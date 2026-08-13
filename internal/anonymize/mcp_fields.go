package anonymize

import (
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
)

// prepareScrubbableContent validates that an MCP content value has no opaque
// payload and normalizes legacy values that omitted their protocol discriminator.
// Opaque binary content fails closed because a text anonymizer cannot prove it
// contains no sensitive data.
func prepareScrubbableContent(content mcp.Content) (mcp.Content, error) {
	switch typed := content.(type) {
	case mcp.TextContent:
		if typed.Type == "" {
			typed.Type = mcp.ContentTypeText
		}
		return typed, nil
	case mcp.ResourceLink:
		if typed.Type == "" {
			typed.Type = mcp.ContentTypeLink
		}
		return typed, nil
	case mcp.EmbeddedResource:
		if _, opaque := typed.Resource.(mcp.BlobResourceContents); opaque {
			return nil, fmt.Errorf("anonymize: opaque embedded resource withheld")
		}
		if _, ok := typed.Resource.(mcp.TextResourceContents); !ok {
			return nil, fmt.Errorf("anonymize: unsupported embedded resource %T withheld", typed.Resource)
		}
		if typed.Type == "" {
			typed.Type = mcp.ContentTypeResource
		}
		return typed, nil
	case mcp.ImageContent, mcp.AudioContent:
		return nil, fmt.Errorf("anonymize: opaque content %T withheld", content)
	default:
		return nil, fmt.Errorf("anonymize: unsupported content %T withheld", content)
	}
}

func parseScrubbedContent(encoded []byte) (mcp.Content, error) {
	var raw map[string]any
	if err := json.Unmarshal(encoded, &raw); err != nil {
		return nil, err
	}
	return mcp.ParseContent(raw)
}
