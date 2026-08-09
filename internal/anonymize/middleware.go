package anonymize

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// WrapHandler returns a new ServerTool whose Handler applies the Anonymizer
// after the original handler returns. Both success and IsError=true results
// are scrubbed — error messages can contain IPs, project IDs, or emails.
//
// Go-level errors are converted to tool errors before scrubbing so protocol
// error strings cannot bypass privacy enforcement.
func WrapHandler(tool server.ServerTool, a Anonymizer) server.ServerTool {
	orig := tool.Handler
	tool.Handler = func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		result, err := orig(ctx, req)
		if err != nil {
			result = mcp.NewToolResultError(err.Error())
		}
		scrubbed, scrubErr := a.Scrub(ctx, result)
		if scrubErr != nil {
			// Never surface raw data when scrubbing fails.
			return mcp.NewToolResultError("anonymize: scrub failed; result withheld"), nil
		}
		return scrubbed, nil
	}
	return tool
}
