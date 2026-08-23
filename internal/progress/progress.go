// Package progress reports advisory MCP progress at real operation milestones.
package progress

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Report sends a progress notification when the request carries a token.
// Notification failure never changes the tool result.
func Report(ctx context.Context, request *mcp.CallToolRequest, current, total float64, message string) {
	if request == nil || request.Params == nil || request.Session == nil {
		return
	}
	token := request.Params.GetProgressToken()
	if token == nil {
		return
	}
	_ = request.Session.NotifyProgress(ctx, &mcp.ProgressNotificationParams{
		ProgressToken: token,
		Progress:      current,
		Total:         total,
		Message:       message,
	})
}
