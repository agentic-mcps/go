package gopls

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestClientNegotiatesCapabilitiesAndShutsDown(t *testing.T) {
	client := startHelperClient(t, "capabilities")

	capabilities := client.Capabilities()
	if !capabilities.WorkspaceSymbol || !capabilities.Hover || !capabilities.Definition || !capabilities.References || !capabilities.Rename {
		t.Fatalf("capabilities = %#v", capabilities)
	}
	if capabilities.CallHierarchy {
		t.Fatalf("call hierarchy unexpectedly available: %#v", capabilities)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := client.Close(ctx); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestClientCancelsOneRequestWithoutStoppingSession(t *testing.T) {
	client := startHelperClient(t, "cancellation")
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = client.Close(ctx)
	})

	requestCtx, cancel := context.WithCancel(context.Background())
	errorResult := make(chan error, 1)
	go func() {
		var ignored any
		errorResult <- client.Request(requestCtx, "test/block", map[string]any{}, &ignored)
	}()
	time.Sleep(50 * time.Millisecond)
	cancel()
	if err := <-errorResult; err == nil || !strings.Contains(err.Error(), context.Canceled.Error()) {
		t.Fatalf("Request() error = %v, want context cancellation", err)
	}

	ctx, stop := context.WithTimeout(context.Background(), 2*time.Second)
	defer stop()
	var response struct {
		SawCancellation bool `json:"saw_cancellation"`
	}
	if err := client.Request(ctx, "test/ping", nil, &response); err != nil {
		t.Fatalf("ping after cancellation: %v", err)
	}
	if !response.SawCancellation {
		t.Fatal("helper did not observe $/cancelRequest")
	}
}

func TestClientRejectsOversizedFrame(t *testing.T) {
	client := startHelperClient(t, "oversized")
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = client.Close(ctx)
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var response any
	if err := client.Request(ctx, "test/oversized", nil, &response); err == nil || !strings.Contains(err.Error(), "frame") {
		t.Fatalf("Request() error = %v, want bounded-frame error", err)
	}
}

func TestClientRequiresVersionIdentity(t *testing.T) {
	_, err := Start(context.Background(), Config{Command: os.Args[0], Workspace: t.TempDir()})
	if err == nil || !strings.Contains(err.Error(), "client version") {
		t.Fatalf("Start() error = %v, want client-version diagnostic", err)
	}
}

func startHelperClient(t *testing.T, scenario string) *Client {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	client, err := Start(ctx, Config{
		Command:       os.Args[0],
		Args:          []string{"-test.run=TestGoplsHelperProcess", "--", scenario},
		Workspace:     t.TempDir(),
		ClientVersion: "1.0.0-test",
		MaxFrame:      1 << 20,
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	return client
}

func TestGoplsHelperProcess(t *testing.T) {
	separator := -1
	for i, arg := range os.Args {
		if arg == "--" {
			separator = i
			break
		}
	}
	if separator < 0 || separator+1 >= len(os.Args) {
		return
	}
	scenario := os.Args[separator+1]
	var instance int
	if scenario == "restart" {
		if separator+2 >= len(os.Args) {
			t.Fatal("restart helper requires a state file")
		}
		statePath := os.Args[separator+2]
		contents, err := os.ReadFile(statePath)
		if err != nil && !os.IsNotExist(err) {
			t.Fatalf("read restart state: %v", err)
		}
		if len(contents) > 0 {
			instance, err = strconv.Atoi(strings.TrimSpace(string(contents)))
			if err != nil {
				t.Fatalf("parse restart state: %v", err)
			}
		}
		instance++
		if err := os.WriteFile(statePath, []byte(strconv.Itoa(instance)), 0o600); err != nil {
			t.Fatalf("write restart state: %v", err)
		}
	}
	reader := bufio.NewReader(os.Stdin)
	writer := bufio.NewWriter(os.Stdout)
	cancelled := false
	for {
		payload, err := helperReadFrame(reader)
		if err != nil {
			if err == io.EOF {
				return
			}
			t.Fatalf("read frame: %v", err)
		}
		var message struct {
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
			ID     json.RawMessage `json:"id"`
		}
		if err := json.Unmarshal(payload, &message); err != nil {
			t.Fatalf("decode message: %v", err)
		}
		switch message.Method {
		case "initialize":
			var params struct {
				ClientInfo struct {
					Version string `json:"version"`
				} `json:"clientInfo"`
			}
			if err := json.Unmarshal(message.Params, &params); err != nil {
				t.Fatalf("decode initialize params: %v", err)
			}
			if scenario == "capabilities" && params.ClientInfo.Version != "1.0.0-test" {
				t.Fatalf("client version = %q", params.ClientInfo.Version)
			}
			result := map[string]any{"capabilities": map[string]any{
				"workspaceSymbolProvider": true,
				"hoverProvider":           true,
				"definitionProvider":      true,
				"referencesProvider":      true,
				"renameProvider":          map[string]any{"prepareProvider": true},
			}}
			helperWriteResponse(t, writer, message.ID, result)
		case "initialized":
			// Notification; no response.
		case "$/cancelRequest":
			cancelled = true
		case "test/block":
			// Deliberately wait for cancellation and leave the response pending.
		case "test/ping":
			helperWriteResponse(t, writer, message.ID, map[string]any{"saw_cancellation": cancelled})
		case "test/oversized":
			if scenario != "oversized" {
				t.Fatalf("unexpected oversized request in %s", scenario)
			}
			_, _ = fmt.Fprint(writer, "Content-Length: 2097152\r\n\r\n")
			_ = writer.Flush()
		case "test/read":
			if scenario == "restart" && instance == 1 {
				return
			}
			helperWriteResponse(t, writer, message.ID, map[string]any{"instance": instance})
		case "test/write":
			if scenario == "restart" {
				return
			}
			helperWriteResponse(t, writer, message.ID, map[string]any{})
		case "shutdown":
			helperWriteResponse(t, writer, message.ID, nil)
		case "exit":
			return
		default:
			helperWriteResponse(t, writer, message.ID, map[string]any{})
		}
	}
}

func helperReadFrame(reader *bufio.Reader) ([]byte, error) {
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
		if ok && strings.EqualFold(strings.TrimSpace(name), "Content-Length") {
			parsed, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil {
				return nil, err
			}
			length = parsed
		}
	}
	if length < 0 {
		return nil, fmt.Errorf("missing content length")
	}
	payload := make([]byte, length)
	_, err := io.ReadFull(reader, payload)
	return payload, err
}

func helperWriteResponse(t *testing.T, writer *bufio.Writer, id json.RawMessage, result any) {
	t.Helper()
	payload, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fmt.Fprintf(writer, "Content-Length: %d\r\n\r\n", len(payload)); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := writer.Flush(); err != nil {
		t.Fatal(err)
	}
}
