package intelligence

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ashwingopalsamy/agentic-go/internal/changeimpact"
	"github.com/ashwingopalsamy/agentic-go/internal/execution"
	"github.com/ashwingopalsamy/agentic-go/internal/gopls"
	"github.com/ashwingopalsamy/agentic-go/internal/verification"
	"github.com/ashwingopalsamy/agentic-go/internal/workspace"
)

func TestExternalSemanticWorkspaces(t *testing.T) {
	sidecar := os.Getenv("AGENTIC_GO_GOPLS")
	workspaces := filepath.SplitList(os.Getenv("AGENTIC_GO_SEMANTIC_WORKSPACES"))
	if sidecar == "" || len(workspaces) == 0 {
		t.Skip("AGENTIC_GO_GOPLS and AGENTIC_GO_SEMANTIC_WORKSPACES are required")
	}
	for _, root := range workspaces {
		if root == "" {
			continue
		}
		t.Run(filepath.Base(root), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()
			ws, err := workspace.Open(ctx, root)
			if err != nil {
				t.Fatal(err)
			}
			runner, err := execution.New(ws, execution.Config{Timeout: 2 * time.Minute})
			if err != nil {
				t.Fatal(err)
			}
			manager, err := gopls.NewManager(ctx, gopls.Config{Command: sidecar, Args: []string{"serve"}, Workspace: ws.Root()})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				closeCtx, stop := context.WithTimeout(context.Background(), 3*time.Second)
				defer stop()
				_ = manager.Close(closeCtx)
			})
			impact, err := changeimpact.New(ws, runner)
			if err != nil {
				t.Fatal(err)
			}
			engine, err := verification.NewEngine(ws, runner, impact, "external-contract")
			if err != nil {
				t.Fatal(err)
			}
			core, err := NewCore(ws, runner, manager, impact, engine)
			if err != nil {
				t.Fatal(err)
			}
			started := time.Now()
			brief, err := core.Brief(ctx, BriefRequest{})
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(brief)
			if err != nil {
				t.Fatal(err)
			}
			if len(encoded) > DefaultBriefBytes || len(brief.Packages) == 0 {
				t.Fatalf("brief bytes=%d packages=%d", len(encoded), len(brief.Packages))
			}
			query := externalSearchQuery(filepath.Base(root))
			searched, err := core.Search(ctx, SearchRequest{Query: query, Limit: 5})
			if err != nil {
				t.Fatal(err)
			}
			if searched.Total == 0 || len(searched.Matches) == 0 {
				t.Fatalf("workspace search returned no %q symbols", query)
			}
			symbol, err := core.Symbol(ctx, SymbolRequest{Ref: searched.Matches[0].Ref})
			if err != nil {
				t.Fatal(err)
			}
			if symbol.Symbol.Ref == "" || symbol.Snapshot.ID != searched.Snapshot.ID || searched.Snapshot.ID != brief.Snapshot.ID {
				t.Fatalf("snapshot lineage brief=%s search=%s symbol=%s", brief.Snapshot.ID, searched.Snapshot.ID, symbol.Snapshot.ID)
			}
			t.Logf("workspace=%s duration=%s packages=%d search_total=%d brief_bytes=%d", filepath.Base(root), time.Since(started), brief.Totals.Packages, searched.Total, len(encoded))
		})
	}
}

func externalSearchQuery(workspaceName string) string {
	switch workspaceName {
	case "cobra":
		return "Command"
	case "gin":
		return "Engine"
	case "grpc-go":
		return "ClientConn"
	default:
		return "New"
	}
}
