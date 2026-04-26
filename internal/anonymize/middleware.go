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
// Go-level errors (JSON-RPC -32603) bypass the scrubber and propagate unchanged
// because they carry no tool content for the LLM to read.
func WrapHandler(tool server.ServerTool, a Anonymizer) server.ServerTool {
	orig := tool.Handler
	tool.Handler = func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		result, err := orig(ctx, req)
		if err != nil {
			return nil, err
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
