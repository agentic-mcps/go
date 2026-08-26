package tools

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/ashwingopalsamy/agentic-go/internal/intelligence"
	"github.com/ashwingopalsamy/agentic-go/internal/verification"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	capabilitiesURI          = "agentic-go://capabilities"
	changeContractCurrentURI = "agentic-go://change-contract/current"
	verificationLatestURI    = "agentic-go://verification/latest"
)

type intelligenceResourceService interface {
	Capabilities() intelligence.Capabilities
	ReadArtifact(context.Context, string, int64) (intelligence.ArtifactChunk, error)
	CurrentChangeContract(context.Context) (intelligence.ChangeContract, error)
	CurrentVerification(context.Context) (verification.Report, error)
}

// RegisterIntelligenceResources publishes effective semantic capabilities and
// snapshot-bound Context Pack detail artifacts.
func RegisterIntelligenceResources(server *mcp.Server, runtime *Runtime) {
	server.AddResource(&mcp.Resource{Name: "capabilities", Description: "Effective pinned-gopls capabilities and compact Context Pack limits.", URI: capabilitiesURI, MIMEType: "application/json"}, runtime.capabilitiesResource)
	server.AddResource(&mcp.Resource{Name: "current-change-contract", Description: "The active private Change Contract for the current workspace.", URI: changeContractCurrentURI, MIMEType: "application/json"}, runtime.currentChangeContractResource)
	server.AddResource(&mcp.Resource{Name: "latest-verification", Description: "The latest private finalized verification report for the current workspace.", URI: verificationLatestURI, MIMEType: "application/json"}, runtime.latestVerificationResource)
	server.AddResourceTemplate(&mcp.ResourceTemplate{
		Name: "artifact", Description: "Reads one bounded chunk using the opaque cursor returned by a Context Pack.",
		URITemplate: "agentic-go://artifact/{id}", MIMEType: "application/json",
	}, runtime.artifactResource)
}

func (r *Runtime) latestVerificationResource(ctx context.Context, request *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	if err := resourceURI(request, verificationLatestURI); err != nil {
		return nil, err
	}
	service, err := r.requireIntelligenceResources()
	if err != nil {
		return nil, err
	}
	report, err := service.CurrentVerification(ctx)
	if err != nil {
		return nil, fmt.Errorf("reading latest verification: %w", err)
	}
	return jsonResource(verificationLatestURI, report)
}

func (r *Runtime) currentChangeContractResource(ctx context.Context, request *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	if err := resourceURI(request, changeContractCurrentURI); err != nil {
		return nil, err
	}
	service, err := r.requireIntelligenceResources()
	if err != nil {
		return nil, err
	}
	contract, err := service.CurrentChangeContract(ctx)
	if err != nil {
		return nil, fmt.Errorf("reading current change contract: %w", err)
	}
	return jsonResource(changeContractCurrentURI, contract)
}

func (r *Runtime) requireIntelligenceResources() (intelligenceResourceService, error) {
	service, ok := r.intelligence.(intelligenceResourceService)
	if !ok || service == nil {
		return nil, fmt.Errorf("intelligence resources are unavailable")
	}
	return service, nil
}

func (r *Runtime) capabilitiesResource(_ context.Context, request *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	if err := resourceURI(request, capabilitiesURI); err != nil {
		return nil, err
	}
	service, err := r.requireIntelligenceResources()
	if err != nil {
		return nil, err
	}
	return jsonResource(capabilitiesURI, service.Capabilities())
}

func (r *Runtime) artifactResource(ctx context.Context, request *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	if request == nil || request.Params == nil {
		return nil, fmt.Errorf("artifact resource request is missing")
	}
	parsed, err := url.Parse(request.Params.URI)
	if err != nil || parsed.Scheme != "agentic-go" || parsed.Host != "artifact" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("artifact resource URI is invalid")
	}
	cursor := strings.TrimPrefix(parsed.EscapedPath(), "/")
	if cursor == "" || strings.Contains(cursor, "/") {
		return nil, fmt.Errorf("artifact cursor is required")
	}
	cursor, err = url.PathUnescape(cursor)
	if err != nil {
		return nil, fmt.Errorf("decoding artifact cursor: %w", err)
	}
	service, err := r.requireIntelligenceResources()
	if err != nil {
		return nil, err
	}
	chunk, err := service.ReadArtifact(ctx, cursor, 0)
	if err != nil {
		return nil, fmt.Errorf("reading context artifact: %w", err)
	}
	return jsonResource(request.Params.URI, chunk)
}
