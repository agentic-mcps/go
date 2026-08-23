package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const traceSummaryURI = "agentic-go://trace-summary"

// RegisterResources registers the complete v0.1 resource inventory.
func RegisterResources(server *mcp.Server, runtime *Runtime) {
	RegisterWorkspaceResources(server, runtime)
	server.AddResource(&mcp.Resource{
		Name:        "trace-summary",
		Description: "Bounded per-tool aggregates for the current trace run.",
		URI:         traceSummaryURI,
		MIMEType:    "application/json",
	}, runtime.traceSummaryResource)
}

func (r *Runtime) traceSummaryResource(_ context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	if err := resourceURI(req, traceSummaryURI); err != nil {
		return nil, err
	}
	summary, err := r.tracer.Summary()
	if err != nil {
		return nil, err
	}
	return jsonResource(traceSummaryURI, summary)
}
