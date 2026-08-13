package mcp

import (
	"context"
	"encoding/json"
	"errors"

	protocol "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// protocolInputGuard prevents mcp-go's pre-dispatch errors from reflecting
// arbitrary caller-controlled identifiers. Handler errors are scrubbed by the
// normal wrappers, but unknown tool/prompt/resource/method errors are generated
// before those wrappers run.
type protocolInputGuard struct {
	tools     map[string]bool
	prompts   map[string]bool
	resources map[string]bool
	templates []*protocol.URITemplate
}

func newProtocolInputGuard() (*protocolInputGuard, *server.Hooks) {
	guard := &protocolInputGuard{
		tools: make(map[string]bool), prompts: make(map[string]bool), resources: make(map[string]bool),
	}
	hooks := &server.Hooks{}
	hooks.AddOnRequestInitialization(guard.validate)
	return guard, hooks
}

func (g *protocolInputGuard) validate(_ context.Context, _ any, message any) error {
	raw, ok := message.(json.RawMessage)
	if !ok {
		if bytes, bytesOK := message.([]byte); bytesOK {
			raw = bytes
		} else {
			return errors.New("request could not be validated")
		}
	}
	var envelope struct {
		Method protocol.MCPMethod `json:"method"`
		Params json.RawMessage    `json:"params"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return errors.New("request could not be validated")
	}
	if !allowedClientMethod(envelope.Method) {
		return errors.New("requested method is unavailable")
	}

	switch envelope.Method {
	case protocol.MethodToolsCall:
		var params struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(envelope.Params, &params) != nil || !g.tools[params.Name] {
			return errors.New("requested tool is unavailable")
		}
	case protocol.MethodPromptsGet:
		var params struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(envelope.Params, &params) != nil || !g.prompts[params.Name] {
			return errors.New("requested prompt is unavailable")
		}
	case protocol.MethodResourcesRead:
		var params struct {
			URI string `json:"uri"`
		}
		if json.Unmarshal(envelope.Params, &params) != nil || !g.resourceAllowed(params.URI) {
			return errors.New("requested resource is unavailable")
		}
	}
	return nil
}

func allowedClientMethod(method protocol.MCPMethod) bool {
	switch method {
	case protocol.MethodInitialize, protocol.MethodPing, protocol.MethodSetLogLevel,
		protocol.MethodResourcesList, protocol.MethodResourcesTemplatesList, protocol.MethodResourcesRead,
		protocol.MethodPromptsList, protocol.MethodPromptsGet,
		protocol.MethodToolsList, protocol.MethodToolsCall,
		protocol.MethodTasksGet, protocol.MethodTasksList, protocol.MethodTasksResult, protocol.MethodTasksCancel,
		protocol.MethodCompletionComplete:
		return true
	default:
		return false
	}
}

func (g *protocolInputGuard) resourceAllowed(uri string) bool {
	if g.resources[uri] {
		return true
	}
	for _, template := range g.templates {
		if template != nil && template.Regexp().MatchString(uri) {
			return true
		}
	}
	return false
}

func (g *protocolInputGuard) addTools(tools []server.ServerTool) {
	for _, tool := range tools {
		g.tools[tool.Tool.Name] = true
	}
}

func (g *protocolInputGuard) addPrompts(prompts []server.ServerPrompt) {
	for _, prompt := range prompts {
		g.prompts[prompt.Prompt.Name] = true
	}
}

func (g *protocolInputGuard) addResources(resources []server.ServerResource) {
	for _, resource := range resources {
		g.resources[resource.Resource.URI] = true
	}
}

func (g *protocolInputGuard) addResourceTemplates(templates []server.ServerResourceTemplate) {
	for _, template := range templates {
		g.templates = append(g.templates, template.Template.URITemplate)
	}
}
