// Package gopls manages the pinned semantic-intelligence sidecar over LSP.
package gopls

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	defaultMaxFrame          = 16 << 20
	defaultInitializeTimeout = 10 * time.Second
)

// Config describes one managed gopls process.
type Config struct {
	Command           string
	Workspace         string
	ClientVersion     string
	Args              []string
	MaxFrame          int
	InitializeTimeout time.Duration
}

// Capabilities is the normalized subset of negotiated gopls features used by
// agentic-go. It records the initialize response rather than assuming support.
type Capabilities struct {
	WorkspaceSymbol bool `json:"workspace_symbol"`
	Hover           bool `json:"hover"`
	Definition      bool `json:"definition"`
	TypeDefinition  bool `json:"type_definition"`
	References      bool `json:"references"`
	Implementation  bool `json:"implementation"`
	DocumentSymbol  bool `json:"document_symbol"`
	CallHierarchy   bool `json:"call_hierarchy"`
	Diagnostics     bool `json:"diagnostics"`
	Rename          bool `json:"rename"`
	Formatting      bool `json:"formatting"`
	CodeAction      bool `json:"code_action"`
}

type callResult struct {
	err    error
	result json.RawMessage
}

// Client owns one long-lived stdio LSP process.
type Client struct {
	stdin      io.WriteCloser
	terminal   error
	cmd        *exec.Cmd
	done       chan struct{}
	stderr     *boundedBuffer
	pending    map[string]chan callResult
	maxFrame   int
	nextID     atomic.Uint64
	closeOnce  sync.Once
	failOnce   sync.Once
	terminalMu sync.Mutex
	pendingMu  sync.Mutex
	writeMu    sync.Mutex
	caps       Capabilities
}

// Start launches and initializes one gopls LSP session.
func Start(ctx context.Context, config Config) (*Client, error) {
	if strings.TrimSpace(config.Command) == "" {
		return nil, fmt.Errorf("gopls command is empty")
	}
	if strings.TrimSpace(config.ClientVersion) == "" {
		return nil, fmt.Errorf("gopls client version is empty")
	}
	workspace, err := filepath.Abs(config.Workspace)
	if err != nil {
		return nil, fmt.Errorf("resolving gopls workspace: %w", err)
	}
	info, err := os.Stat(workspace)
	if err != nil {
		return nil, fmt.Errorf("inspecting gopls workspace: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("gopls workspace is not a directory")
	}
	if config.MaxFrame == 0 {
		config.MaxFrame = defaultMaxFrame
	}
	if config.MaxFrame < 1 {
		return nil, fmt.Errorf("gopls maximum frame must be positive")
	}
	if config.InitializeTimeout == 0 {
		config.InitializeTimeout = defaultInitializeTimeout
	}
	if config.InitializeTimeout < 0 {
		return nil, fmt.Errorf("gopls initialize timeout must be positive")
	}

	command := exec.CommandContext(ctx, config.Command, config.Args...)
	command.Dir = workspace
	command.Env = replaceEnv(os.Environ(), "GOTOOLCHAIN", "local")
	command.Env = replaceEnv(command.Env, "GOTELEMETRY", "off")
	command.Env = replaceEnv(command.Env, "GOWORK", "auto")
	stdin, err := command.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("opening gopls stdin: %w", err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("opening gopls stdout: %w", err)
	}
	stderr, err := command.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("opening gopls stderr: %w", err)
	}
	if err := command.Start(); err != nil {
		return nil, fmt.Errorf("starting gopls: %w", err)
	}

	client := &Client{
		cmd:      command,
		stdin:    stdin,
		maxFrame: config.MaxFrame,
		done:     make(chan struct{}),
		pending:  make(map[string]chan callResult),
		stderr:   &boundedBuffer{limit: 1 << 20},
	}
	go func() { _, _ = io.Copy(client.stderr, stderr) }()
	go client.readLoop(stdout)
	go func() {
		err := command.Wait()
		if err != nil {
			client.fail(fmt.Errorf("gopls process exited: %w: %s", err, client.stderr.String()))
		} else {
			client.fail(io.EOF)
		}
	}()

	rootURI := (&url.URL{Scheme: "file", Path: workspace}).String()
	params := map[string]any{
		"processId":        nil,
		"clientInfo":       map[string]any{"name": "agentic-go", "version": config.ClientVersion},
		"rootUri":          rootURI,
		"workspaceFolders": []map[string]any{{"uri": rootURI, "name": filepath.Base(workspace)}},
		"capabilities": map[string]any{
			"workspace": map[string]any{
				"configuration":         true,
				"workspaceFolders":      true,
				"didChangeWatchedFiles": map[string]any{"dynamicRegistration": true, "relativePatternSupport": true},
			},
			"window": map[string]any{"workDoneProgress": true},
			"textDocument": map[string]any{
				"hover":          map[string]any{"contentFormat": []string{"markdown", "plaintext"}},
				"documentSymbol": map[string]any{"hierarchicalDocumentSymbolSupport": true},
				"rename":         map[string]any{"prepareSupport": true},
				"diagnostic":     map[string]any{"dynamicRegistration": false},
				"codeAction": map[string]any{
					"dataSupport":              true,
					"resolveSupport":           map[string]any{"properties": []string{"edit"}},
					"codeActionLiteralSupport": map[string]any{"codeActionKind": map[string]any{"valueSet": []string{"", "quickfix", "source.fixAll", "source.organizeImports"}}},
				},
			},
		},
		"initializationOptions": map[string]any{"symbolMatcher": "fuzzy", "pullDiagnostics": true},
	}
	var initialized struct {
		Capabilities map[string]json.RawMessage `json:"capabilities"`
	}
	initializeCtx, cancelInitialize := context.WithTimeout(ctx, config.InitializeTimeout)
	defer cancelInitialize()
	if err := client.Request(initializeCtx, "initialize", params, &initialized); err != nil {
		_ = command.Process.Kill()
		return nil, fmt.Errorf("initializing gopls: %w", err)
	}
	client.caps = normalizeCapabilities(initialized.Capabilities)
	if err := client.Notify("initialized", map[string]any{}); err != nil {
		_ = command.Process.Kill()
		return nil, fmt.Errorf("notifying gopls initialization: %w", err)
	}
	return client, nil
}

// Capabilities returns the immutable negotiated capability manifest.
func (c *Client) Capabilities() Capabilities { return c.caps }

// Terminated reports whether the sidecar session can no longer serve requests.
func (c *Client) Terminated() bool {
	select {
	case <-c.done:
		return true
	default:
		return false
	}
}

// Request sends one cancellable JSON-RPC request.
func (c *Client) Request(ctx context.Context, method string, params, result any) error {
	if strings.TrimSpace(method) == "" {
		return fmt.Errorf("gopls method is empty")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	id := c.nextID.Add(1)
	key := strconv.FormatUint(id, 10)
	response := make(chan callResult, 1)
	c.pendingMu.Lock()
	select {
	case <-c.done:
		c.pendingMu.Unlock()
		return c.terminalError()
	default:
		c.pending[key] = response
	}
	c.pendingMu.Unlock()

	message := map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}
	if err := c.write(message); err != nil {
		c.removePending(key)
		return err
	}

	select {
	case reply := <-response:
		if reply.err != nil {
			return reply.err
		}
		if result == nil || bytes.Equal(reply.result, []byte("null")) || len(reply.result) == 0 {
			return nil
		}
		if err := json.Unmarshal(reply.result, result); err != nil {
			return fmt.Errorf("decoding gopls %s result: %w", method, err)
		}
		return nil
	case <-ctx.Done():
		c.removePending(key)
		_ = c.Notify("$/cancelRequest", map[string]any{"id": id})
		return ctx.Err()
	case <-c.done:
		c.removePending(key)
		return c.terminalError()
	}
}

// Notify sends one JSON-RPC notification.
func (c *Client) Notify(method string, params any) error {
	if strings.TrimSpace(method) == "" {
		return fmt.Errorf("gopls notification method is empty")
	}
	return c.write(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

// Close performs the LSP shutdown sequence once.
func (c *Client) Close(ctx context.Context) error {
	var closeErr error
	c.closeOnce.Do(func() {
		if err := c.Request(ctx, "shutdown", nil, nil); err != nil && !errors.Is(err, io.EOF) {
			closeErr = err
			_ = c.cmd.Process.Kill()
			return
		}
		if err := c.Notify("exit", nil); err != nil {
			closeErr = err
			_ = c.cmd.Process.Kill()
			return
		}
		_ = c.stdin.Close()
		select {
		case <-c.done:
		case <-ctx.Done():
			closeErr = ctx.Err()
			_ = c.cmd.Process.Kill()
		}
	})
	return closeErr
}

func (c *Client) readLoop(reader io.Reader) {
	buffered := bufio.NewReader(reader)
	for {
		payload, err := readFrame(buffered, c.maxFrame)
		if err != nil {
			c.fail(fmt.Errorf("reading gopls frame: %w", err))
			return
		}
		var message wireMessage
		if err := json.Unmarshal(payload, &message); err != nil {
			c.fail(fmt.Errorf("decoding gopls frame: %w", err))
			return
		}
		if len(message.ID) > 0 && message.Method == "" {
			c.deliver(message)
			continue
		}
		if len(message.ID) > 0 && message.Method != "" {
			c.handleServerRequest(message)
		}
	}
}

func (c *Client) deliver(message wireMessage) {
	key := strings.TrimSpace(string(message.ID))
	c.pendingMu.Lock()
	response := c.pending[key]
	delete(c.pending, key)
	c.pendingMu.Unlock()
	if response == nil {
		return
	}
	if message.Error != nil {
		response <- callResult{err: fmt.Errorf("gopls RPC %d: %s", message.Error.Code, message.Error.Message)}
		return
	}
	response <- callResult{result: message.Result}
}

func (c *Client) handleServerRequest(message wireMessage) {
	var result any
	switch message.Method {
	case "workspace/configuration":
		var params struct {
			Items []any `json:"items"`
		}
		_ = json.Unmarshal(message.Params, &params)
		result = make([]any, len(params.Items))
	case "client/registerCapability", "window/workDoneProgress/create":
		result = nil
	case "workspace/applyEdit":
		result = map[string]any{"applied": false, "failureReason": "agentic-go requires a guarded refactor plan"}
	default:
		_ = c.write(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(message.ID), "error": map[string]any{"code": -32601, "message": "method not supported"}})
		return
	}
	_ = c.write(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(message.ID), "result": result})
}

func (c *Client) write(message any) error {
	payload, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("encoding gopls message: %w", err)
	}
	if len(payload) > c.maxFrame {
		return fmt.Errorf("gopls frame exceeds %d bytes", c.maxFrame)
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	select {
	case <-c.done:
		return c.terminalError()
	default:
	}
	if _, err := fmt.Fprintf(c.stdin, "Content-Length: %d\r\n\r\n", len(payload)); err != nil {
		return fmt.Errorf("writing gopls frame header: %w", err)
	}
	if _, err := c.stdin.Write(payload); err != nil {
		return fmt.Errorf("writing gopls frame payload: %w", err)
	}
	return nil
}

func (c *Client) removePending(key string) {
	c.pendingMu.Lock()
	delete(c.pending, key)
	c.pendingMu.Unlock()
}

func (c *Client) fail(err error) {
	c.failOnce.Do(func() {
		c.terminalMu.Lock()
		c.terminal = err
		c.terminalMu.Unlock()
		close(c.done)
		c.pendingMu.Lock()
		for key, pending := range c.pending {
			pending <- callResult{err: err}
			delete(c.pending, key)
		}
		c.pendingMu.Unlock()
	})
}

func (c *Client) terminalError() error {
	c.terminalMu.Lock()
	defer c.terminalMu.Unlock()
	if c.terminal == nil {
		return io.EOF
	}
	return c.terminal
}

type rpcFailure struct {
	Message string `json:"message"`
	Code    int    `json:"code"`
}

type wireMessage struct {
	Error   *rpcFailure     `json:"error"`
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	ID      json.RawMessage `json:"id"`
	Params  json.RawMessage `json:"params"`
	Result  json.RawMessage `json:"result"`
}

func normalizeCapabilities(values map[string]json.RawMessage) Capabilities {
	return Capabilities{
		WorkspaceSymbol: capability(values["workspaceSymbolProvider"]),
		Hover:           capability(values["hoverProvider"]),
		Definition:      capability(values["definitionProvider"]),
		TypeDefinition:  capability(values["typeDefinitionProvider"]),
		References:      capability(values["referencesProvider"]),
		Implementation:  capability(values["implementationProvider"]),
		DocumentSymbol:  capability(values["documentSymbolProvider"]),
		CallHierarchy:   capability(values["callHierarchyProvider"]),
		Diagnostics:     capability(values["diagnosticProvider"]),
		Rename:          capability(values["renameProvider"]),
		Formatting:      capability(values["documentFormattingProvider"]),
		CodeAction:      capability(values["codeActionProvider"]),
	}
}

func capability(raw json.RawMessage) bool {
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) || bytes.Equal(raw, []byte("false")) {
		return false
	}
	return json.Valid(raw)
}

func readFrame(reader *bufio.Reader, maximum int) ([]byte, error) {
	length := -1
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
		if line == "" {
			break
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			return nil, fmt.Errorf("malformed frame header")
		}
		if strings.EqualFold(strings.TrimSpace(name), "Content-Length") {
			parsed, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil || parsed < 0 {
				return nil, fmt.Errorf("invalid frame content length")
			}
			length = parsed
		}
	}
	if length < 0 {
		return nil, fmt.Errorf("frame content length is missing")
	}
	if length > maximum {
		return nil, fmt.Errorf("frame content length %d exceeds %d", length, maximum)
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(reader, payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func replaceEnv(environment []string, key, value string) []string {
	prefix := key + "="
	result := make([]string, 0, len(environment)+1)
	for _, entry := range environment {
		if !strings.HasPrefix(entry, prefix) {
			result = append(result, entry)
		}
	}
	return append(result, prefix+value)
}

type boundedBuffer struct {
	data  []byte
	limit int
	mu    sync.Mutex
}

func (b *boundedBuffer) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	remaining := b.limit - len(b.data)
	if remaining > 0 {
		b.data = append(b.data, data[:min(len(data), remaining)]...)
	}
	return len(data), nil
}

func (b *boundedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.TrimSpace(string(b.data))
}
