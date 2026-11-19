package tools

import (
	"context"
	"errors"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// Human approval for mutations.
//
// A mutation tool never executes on the model's say-so. The first call
// previews the change and stores a single-use plan in the safety decorator
// (bound to the caller and session, 10 minute TTL). The tool then returns an
// MCP elicitation that the client shows to the person, not the model. Only an
// "accept" answer, delivered by the client on the retry together with the
// plan ID as opaque request state, executes the stored plan.
//
// Clients on the 2026-07-28 protocol receive an input_required result and
// retry the call themselves; for older clients mcp-go sends elicitation/create
// and re-invokes the handler. The same handler serves both.

const approvalInputID = "approval"

var errConfirmPlanIDNotAccepted = errors.New("confirm_plan_id is not accepted: the change is approved by the user through the client's approval prompt")

// mutation describes one approval-gated tool call. Resp is the service
// response type of the mutation.
type mutation[Resp any] struct {
	tool string
	// previewOnly is the model's dry_run=true: show the change, never ask
	// for approval, never execute.
	previewOnly bool
	// confirmPlanID is the confirm_plan_id argument, which the model may no
	// longer supply.
	confirmPlanID string
	// skipApproval is set only when SAFETY_ENABLED=false (local stdio): the
	// service is the raw adapter and executes directly.
	skipApproval bool

	preview func(context.Context) (Resp, error)             // service call with DryRun=true
	confirm func(context.Context, string) (Resp, error)     // service call with ConfirmPlanID
	execute func(context.Context) (Resp, error)             // service call with DryRun=false (no safety layer)
	plan    func(Resp) (planID string, noChangeNeeded bool) // plan created by the preview
	strip   func(*Resp)                                     // remove plan fields from a preview-only response
	prompt  func(Resp) string                               // what the user is asked to approve
}

func runMutation[Resp any](ctx context.Context, req mcp.CallToolRequest, m mutation[Resp]) (*mcp.CallToolResult, error) {
	if m.confirmPlanID != "" {
		return mcp.NewToolResultError(errConfirmPlanIDNotAccepted.Error()), nil
	}
	if m.skipApproval {
		run := m.execute
		if m.previewOnly {
			run = m.preview
		}
		resp, err := run(ctx)
		if err != nil {
			return handleServiceError(m.tool, err)
		}
		return jsonResult(m.tool, resp)
	}

	// Retry carrying the user's answer.
	if planID, answered := approvalState(req); answered {
		if !approvalGranted(req) {
			return jsonResult(m.tool, map[string]any{
				"approved":    false,
				"description": "The user did not approve this change. Nothing was modified.",
			})
		}
		resp, err := m.confirm(ctx, planID)
		if err != nil {
			return handleServiceError(m.tool, err)
		}
		return jsonResult(m.tool, resp)
	}

	resp, err := m.preview(ctx)
	if err != nil {
		return handleServiceError(m.tool, err)
	}
	planID, noChange := m.plan(resp)
	if noChange {
		return jsonResult(m.tool, resp)
	}
	if m.previewOnly {
		m.strip(&resp)
		return jsonResult(m.tool, resp)
	}
	// Checked after the preview so a request that needs no change succeeds
	// with any client. A refused request leaves its plan to expire unused.
	if err := approvalSupported(ctx); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf(
			"%s changes resources and needs the user's approval, but %v. Nothing was modified. "+
				"Use dry_run=true to preview the change, or use an MCP client that supports elicitation.",
			m.tool, err)), nil
	}
	if planID == "" {
		// The safety decorator always returns a plan for a needed change.
		// Without one there is nothing safe to approve.
		return mcp.NewToolResultError(m.tool + ": no approval plan was created; nothing was modified"), nil
	}
	return approvalRequest(planID, m.prompt(resp)), nil
}

// approvalRequest asks the client to show the user a single approve/decline
// prompt. The plan ID travels as opaque request state that only the client
// echoes back; the safety decorator accepts it only from the same caller and
// session, once, within its TTL.
func approvalRequest(planID, message string) *mcp.CallToolResult {
	return server.NewInputRequestBuilder(planID).Elicit(approvalInputID, mcp.ElicitationParams{
		Mode:    mcp.ElicitationModeForm,
		Message: message,
		RequestedSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"approve": map[string]any{
					"type":        "boolean",
					"title":       "Approve this change",
					"description": "Leave checked and accept to apply the change. Decline or cancel to leave everything as it is.",
					"default":     true,
				},
			},
		},
	}).ToolResult()
}

// approvalState returns the plan ID echoed back by the client and whether the
// call is a retry carrying the user's answer.
func approvalState(req mcp.CallToolRequest) (string, bool) {
	state := req.Params.RequestState
	return state, state != "" || len(req.Params.InputResponses) > 0
}

// approvalGranted is true only for an explicit accept that does not set
// approve=false. A missing answer counts as not approved.
func approvalGranted(req mcp.CallToolRequest) bool {
	if req.Params.RequestState == "" {
		return false
	}
	answer := server.ElicitationResponse(req.Params.InputResponses, approvalInputID)
	if answer == nil || answer.Action != mcp.ElicitationResponseActionAccept {
		return false
	}
	if content, ok := answer.Content.(map[string]any); ok {
		if approve, present := content["approve"]; present {
			value, isBool := approve.(bool)
			return isBool && value
		}
	}
	return true
}

// approvalSupported reports why the current client cannot show an approval
// prompt, or nil when it can. It fails closed: an unknown client, a client
// that did not declare elicitation, or a legacy session on a transport that
// cannot send server-initiated requests (mcp-go's SSE transport) all refuse.
func approvalSupported(ctx context.Context) error {
	if info := server.RequestProtocolInfoFromContext(ctx); info != nil && info.Modern {
		if info.ClientCapabilities == nil || info.ClientCapabilities.Elicitation == nil {
			return errors.New("the MCP client did not declare elicitation support")
		}
		return nil
	}
	session := server.ClientSessionFromContext(ctx)
	if session == nil {
		return errors.New("there is no MCP client session to ask")
	}
	withInfo, ok := session.(server.SessionWithClientInfo)
	if !ok || withInfo.GetClientCapabilities().Elicitation == nil {
		return errors.New("the MCP client did not declare elicitation support")
	}
	if _, ok := session.(server.SessionWithElicitation); !ok {
		return errors.New("this transport cannot send approval prompts to this client version")
	}
	return nil
}

func jsonResult(tool string, value any) (*mcp.CallToolResult, error) {
	result, err := mcp.NewToolResultJSON(value)
	if err != nil {
		return nil, fmt.Errorf("%s: marshal: %w", tool, err)
	}
	return result, nil
}
