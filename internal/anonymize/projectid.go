package anonymize

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
)

type projectIDReplacement struct {
	projectID   string
	replacement string
}

// ProjectIDMasker replaces configured project IDs in every string-bearing MCP
// result field. Replacements are applied longest-first to avoid partial matches.
type ProjectIDMasker struct {
	replacements []projectIDReplacement
}

// NewProjectIDMasker preserves the legacy single-project placeholder behavior.
func NewProjectIDMasker(projectID string) *ProjectIDMasker {
	return NewProjectIDReplacer(map[string]string{projectID: "[GCP_PROJECT_ID]"})
}

// NewProjectIDReplacer creates a masker for project ID to display-name mappings.
func NewProjectIDReplacer(replacements map[string]string) *ProjectIDMasker {
	pairs := make([]projectIDReplacement, 0, len(replacements))
	for projectID, replacement := range replacements {
		if projectID == "" || projectID == replacement {
			continue
		}
		pairs = append(pairs, projectIDReplacement{projectID: projectID, replacement: replacement})
	}
	sort.Slice(pairs, func(i, j int) bool {
		return len(pairs[i].projectID) > len(pairs[j].projectID)
	})
	return &ProjectIDMasker{replacements: pairs}
}

// Empty reports whether no project IDs need replacement.
func (m *ProjectIDMasker) Empty() bool { return m == nil || len(m.replacements) == 0 }

// ReplaceString maps every configured project ID to its public display value.
func (m *ProjectIDMasker) ReplaceString(value string) string {
	if m == nil {
		return value
	}
	for _, pair := range m.replacements {
		value = strings.ReplaceAll(value, pair.projectID, pair.replacement)
	}
	return value
}

// Scrub replaces project IDs in a tool result. Opaque binary content is
// withheld in alias mode because it cannot be inspected safely.
func (m *ProjectIDMasker) Scrub(_ context.Context, result *mcp.CallToolResult) (*mcp.CallToolResult, error) {
	if result == nil || m.Empty() {
		return result, nil
	}
	out := *result
	meta, err := m.scrubMeta(result.Meta)
	if err != nil {
		return nil, err
	}
	out.Meta = meta
	out.Content = make([]mcp.Content, len(result.Content))
	for i, content := range result.Content {
		scrubbed, scrubErr := m.scrubContent(content)
		if scrubErr != nil {
			return nil, scrubErr
		}
		out.Content[i] = scrubbed
	}
	if result.StructuredContent != nil {
		out.StructuredContent, err = m.scrubJSONValue(result.StructuredContent)
		if err != nil {
			return nil, fmt.Errorf("project ID scrub structured content: %w", err)
		}
	}
	return &out, nil
}

func (m *ProjectIDMasker) scrubContent(content mcp.Content) (mcp.Content, error) {
	switch typed := content.(type) {
	case mcp.TextContent:
		typed.Text = m.ReplaceString(typed.Text)
		meta, err := m.scrubMeta(typed.Meta)
		if err != nil {
			return nil, err
		}
		typed.Meta = meta
		return typed, nil
	case mcp.ResourceLink:
		typed.URI = m.ReplaceString(typed.URI)
		typed.Name = m.ReplaceString(typed.Name)
		typed.Description = m.ReplaceString(typed.Description)
		return typed, nil
	case mcp.EmbeddedResource:
		meta, err := m.scrubMeta(typed.Meta)
		if err != nil {
			return nil, err
		}
		typed.Meta = meta
		switch resource := typed.Resource.(type) {
		case mcp.TextResourceContents:
			resource.URI = m.ReplaceString(resource.URI)
			resource.Text = m.ReplaceString(resource.Text)
			scrubbedMeta, scrubErr := m.scrubMap(resource.Meta)
			if scrubErr != nil {
				return nil, scrubErr
			}
			resource.Meta = scrubbedMeta
			typed.Resource = resource
			return typed, nil
		case mcp.BlobResourceContents:
			return nil, errors.New("project ID scrub: opaque blob content withheld")
		default:
			return nil, fmt.Errorf("project ID scrub: unsupported embedded resource %T", typed.Resource)
		}
	case mcp.ImageContent, mcp.AudioContent:
		return nil, fmt.Errorf("project ID scrub: opaque content %T withheld", content)
	default:
		return nil, fmt.Errorf("project ID scrub: unsupported content %T", content)
	}
}

// ScrubResourceContents replaces project IDs in MCP resource responses.
func (m *ProjectIDMasker) ScrubResourceContents(contents []mcp.ResourceContents) ([]mcp.ResourceContents, error) {
	if m.Empty() {
		return contents, nil
	}
	out := make([]mcp.ResourceContents, len(contents))
	for i, content := range contents {
		switch typed := content.(type) {
		case mcp.TextResourceContents:
			typed.URI = m.ReplaceString(typed.URI)
			typed.Text = m.ReplaceString(typed.Text)
			meta, err := m.scrubMap(typed.Meta)
			if err != nil {
				return nil, err
			}
			typed.Meta = meta
			out[i] = typed
		case mcp.BlobResourceContents:
			return nil, errors.New("project ID scrub: opaque resource blob withheld")
		default:
			return nil, fmt.Errorf("project ID scrub: unsupported resource content %T", content)
		}
	}
	return out, nil
}

// ScrubPromptResult replaces project IDs in generated prompt content.
func (m *ProjectIDMasker) ScrubPromptResult(result *mcp.GetPromptResult) (*mcp.GetPromptResult, error) {
	if result == nil || m.Empty() {
		return result, nil
	}
	out := *result
	out.Description = m.ReplaceString(result.Description)
	meta, err := m.scrubMeta(result.Meta)
	if err != nil {
		return nil, err
	}
	out.Meta = meta
	out.Messages = make([]mcp.PromptMessage, len(result.Messages))
	for i, message := range result.Messages {
		content, scrubErr := m.scrubContent(message.Content)
		if scrubErr != nil {
			return nil, scrubErr
		}
		out.Messages[i] = mcp.PromptMessage{Role: message.Role, Content: content}
	}
	return &out, nil
}

func (m *ProjectIDMasker) scrubMeta(meta *mcp.Meta) (*mcp.Meta, error) {
	if meta == nil {
		return nil, nil
	}
	out := *meta
	additional, err := m.scrubMap(meta.AdditionalFields)
	if err != nil {
		return nil, err
	}
	out.AdditionalFields = additional
	if value, ok := meta.ProgressToken.(string); ok {
		out.ProgressToken = m.ReplaceString(value)
	}
	return &out, nil
}

func (m *ProjectIDMasker) scrubMap(input map[string]any) (map[string]any, error) {
	if input == nil {
		return nil, nil
	}
	value, err := m.scrubJSONValue(input)
	if err != nil {
		return nil, err
	}
	out, ok := value.(map[string]any)
	if !ok {
		return nil, errors.New("project ID scrub: metadata changed type")
	}
	return out, nil
}

func (m *ProjectIDMasker) scrubJSONValue(input any) (any, error) {
	encoded, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	encoded = []byte(m.ReplaceString(string(encoded)))
	var output any
	if err := json.Unmarshal(encoded, &output); err != nil {
		return nil, err
	}
	return output, nil
}
