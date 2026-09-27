package retrievalstudy

import (
	"context"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	managedgopls "github.com/agentic-mcps/go/internal/gopls"
)

type goplsLocation struct {
	URI   string `json:"uri"`
	Range struct {
		Start struct {
			Line int `json:"line"`
		} `json:"start"`
	} `json:"range"`
}

type goplsSymbol struct {
	Name          string        `json:"name"`
	ContainerName string        `json:"containerName"`
	Kind          int           `json:"kind"`
	Location      goplsLocation `json:"location"`
}

type goplsSession struct {
	client *managedgopls.Client
	root   string
}

func startGopls(parent context.Context, binary, workspace string, timeout time.Duration) (*goplsSession, string, float64, error) {
	probeCtx, cancelProbe := context.WithTimeout(parent, timeout)
	installation, err := managedgopls.Locate(probeCtx, "", binary)
	cancelProbe()
	if err != nil {
		return nil, "", 0, err
	}
	start := time.Now()
	client, err := managedgopls.Start(parent, managedgopls.Config{
		Command: installation.Path, Workspace: workspace,
		ClientVersion: "agentic-go-retrievalbench/v1", InitializeTimeout: timeout,
	})
	initializeMS := float64(time.Since(start)) / float64(time.Millisecond)
	if err != nil {
		return nil, installation.Version, initializeMS, err
	}
	if !client.Capabilities().WorkspaceSymbol {
		closeCtx, cancelClose := context.WithTimeout(context.Background(), timeout)
		_ = client.Close(closeCtx)
		cancelClose()
		return nil, installation.Version, initializeMS, fmt.Errorf("gopls did not advertise workspace/symbol")
	}
	return &goplsSession{client: client, root: workspace}, installation.Version, initializeMS, nil
}

func (session *goplsSession) close(timeout time.Duration) {
	if session == nil || session.client == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	_ = session.client.Close(ctx)
}

func (session *goplsSession) search(parent context.Context, timeout time.Duration, query Query, source archivedSource, gold []GoldSpan, repetitions int) (Ranking, Latency, error) {
	queryText := query.GoplsQuery
	if strings.TrimSpace(queryText) == "" {
		queryText = query.Text
	}
	samples := make([]float64, 0, repetitions)
	var candidates []Candidate
	var complete = true
	var reason string
	for repetition := 0; repetition < repetitions; repetition++ {
		if err := parent.Err(); err != nil {
			return Ranking{}, Latency{}, err
		}
		ctx, cancel := context.WithTimeout(parent, timeout)
		start := time.Now()
		var symbols []goplsSymbol
		err := session.client.Request(ctx, "workspace/symbol", map[string]any{"query": queryText}, &symbols)
		duration := time.Since(start)
		cancel()
		samples = append(samples, float64(duration)/float64(time.Millisecond))
		if err != nil {
			complete = false
			reason = "gopls workspace/symbol failed or timed out"
			continue
		}
		candidates = session.mapSymbols(symbols, source)
	}
	ranking := scoreRanking(candidates, len(candidates), gold, complete, reason, "workspace_symbol_anchor", nil)
	if complete {
		ranking.Status = "unknown_completeness"
		ranking.Complete = false
		ranking.MetricsUsable = true
		ranking.CandidateCountComplete = false
		ranking.IncompleteReason = "workspace/symbol provider result cap is not established; scores cover only the returned candidate list"
	}
	if !source.coverage.SourceArchiveComplete {
		ranking.Status = "partial"
		ranking.Complete = false
		ranking.MetricsUsable = false
		ranking.CandidateCountComplete = false
		ranking.IncompleteReason = source.coverage.SourceArchiveIncompleteReason
	}
	if !complete {
		ranking.CandidateCountComplete = false
	}
	return ranking, latency(samples), nil
}

func (session *goplsSession) mapSymbols(symbols []goplsSymbol, source archivedSource) []Candidate {
	result := make([]Candidate, 0, len(symbols))
	seen := make(map[string]struct{}, len(symbols))
	for _, symbol := range symbols {
		parsed, err := url.Parse(symbol.Location.URI)
		if err != nil || parsed.Scheme != "file" || (parsed.Host != "" && parsed.Host != "localhost") {
			continue
		}
		filename := filepath.Clean(filepath.FromSlash(parsed.Path))
		relative, err := filepath.Rel(session.root, filename)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
			continue
		}
		relative = filepath.ToSlash(relative)
		meta, ok := source.meta[relative]
		line := symbol.Location.Range.Start.Line + 1
		if !ok || !meta.goFile || line < 1 || line > meta.lines {
			continue
		}
		key := fmt.Sprintf("%s:%d:%d:%s", relative, line, symbol.Kind, symbol.Name)
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, Candidate{
			Path: relative, Line: line, Name: symbol.Name,
			Container: symbol.ContainerName, Kind: goplsKind(symbol.Kind),
			ProviderKind: symbol.Kind,
		})
	}
	return result
}

func goplsKind(kind int) string {
	switch kind {
	case 1:
		return "file"
	case 2:
		return "module"
	case 3:
		return "namespace"
	case 4:
		return "package"
	case 5:
		return "class"
	case 6:
		return "method"
	case 7:
		return "property"
	case 8:
		return "field"
	case 9:
		return "constructor"
	case 10:
		return "enum"
	case 11:
		return "interface"
	case 12:
		return "function"
	case 13:
		return "variable"
	case 14:
		return "constant"
	case 15:
		return "string"
	case 16:
		return "number"
	case 17:
		return "boolean"
	case 18:
		return "array"
	case 19:
		return "object"
	case 20:
		return "key"
	case 21:
		return "null"
	case 22:
		return "enum_member"
	case 23:
		return "struct"
	case 24:
		return "event"
	case 25:
		return "operator"
	case 26:
		return "type_parameter"
	default:
		return "unknown"
	}
}
