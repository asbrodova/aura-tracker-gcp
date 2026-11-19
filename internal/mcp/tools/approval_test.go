package tools

import (
	"context"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/asbrodova/aura-tracker-gcp/internal/requestmeta"
	"github.com/asbrodova/aura-tracker-gcp/internal/safety"
	"github.com/asbrodova/aura-tracker-gcp/internal/testutil"
	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

// scaleFixture is a node-pool scale tool behind the real safety decorator,
// counting how often the change actually executes.
type scaleFixture struct {
	tool     server.ServerTool
	executed atomic.Int32
}

func newScaleFixture(t *testing.T, decorate bool) *scaleFixture {
	t.Helper()
	f := &scaleFixture{}
	fake := &testutil.FakeGCPService{
		ScaleDeploymentFunc: func(_ context.Context, req models.ScaleDeploymentRequest) (models.ScaleDeploymentResponse, error) {
			resp := models.ScaleDeploymentResponse{DryRun: req.DryRun, NodePoolName: req.NodePoolName, PreviousCount: 2, RequestedCount: req.NodeCount, Zones: 1, CurrentTotalNodes: 2}
			if req.NodeCount == 2 {
				resp.NoChangeNeeded = true
				return resp, nil
			}
			if !req.DryRun {
				f.executed.Add(1)
				resp.OperationName = "operation-1"
			}
			return resp, nil
		},
	}
	log := slog.New(slog.DiscardHandler)
	gke := NewGKETools(fake, log)
	if decorate {
		gke = NewGKETools(safety.NewSafetyDecorator(fake, log), log)
	} else {
		gke.DisableMutationApproval()
	}
	f.tool = gke.ScaleDeployment()
	return f
}

// modernClient is a request context from a 2026-07-28 client that declared
// elicitation support, in a given session.
func modernClient(session string, elicitation bool) context.Context {
	caps := &mcp.ClientCapabilities{}
	if elicitation {
		caps.Elicitation = &mcp.ElicitationCapability{}
	}
	ctx := requestmeta.WithSessionID(context.Background(), session)
	return server.WithRequestProtocolInfo(ctx, &server.RequestProtocolInfo{
		Modern: true, ProtocolVersion: mcp.ProtocolVersion20260728, ClientCapabilities: caps,
	})
}

func scaleCall(args map[string]any) mcp.CallToolRequest {
	base := map[string]any{"project_id": "p", "location": "us-central1", "cluster_name": "c", "node_pool_name": "pool", "node_count": 5}
	for key, value := range args {
		base[key] = value
	}
	return mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "gcp_gke_scale_deployment", Arguments: base}}
}

func withAnswer(req mcp.CallToolRequest, state string, action mcp.ElicitationResponseAction, content any) mcp.CallToolRequest {
	req.Params.RequestState = state
	req.Params.InputResponses = mcp.InputResponses{
		approvalInputID: mcp.NewElicitationInputResponse(mcp.ElicitationResult{
			ElicitationResponse: mcp.ElicitationResponse{Action: action, Content: content},
		}),
	}
	return req
}

func call(t *testing.T, tool server.ServerTool, ctx context.Context, req mcp.CallToolRequest) *mcp.CallToolResult {
	t.Helper()
	result, err := tool.Handler(ctx, req)
	if err != nil {
		t.Fatalf("handler returned a Go error: %v", err)
	}
	return result
}

// askApproval makes the first call and returns the approval prompt state.
func askApproval(t *testing.T, f *scaleFixture, ctx context.Context) (string, string) {
	t.Helper()
	result := call(t, f.tool, ctx, scaleCall(nil))
	if result.ResultType != mcp.ResultTypeInputRequired {
		t.Fatalf("first call did not ask for approval: %+v", result)
	}
	request, ok := result.InputRequests[approvalInputID]
	if !ok || request.Elicitation == nil {
		t.Fatalf("no approval elicitation in %+v", result.InputRequests)
	}
	if result.RequestState == "" {
		t.Fatal("approval request carries no plan state")
	}
	if f.executed.Load() != 0 {
		t.Fatal("change executed before approval")
	}
	return result.RequestState, request.Elicitation.Message
}

func TestMutationExecutesOnlyAfterUserApproval(t *testing.T) {
	f := newScaleFixture(t, true)
	ctx := modernClient("session-a", true)

	state, message := askApproval(t, f, ctx)
	for _, want := range []string{`node pool "pool"`, "from 2 to 5 nodes per zone"} {
		if !strings.Contains(message, want) {
			t.Errorf("approval message %q does not contain %q", message, want)
		}
	}

	result := call(t, f.tool, ctx, withAnswer(scaleCall(nil), state, mcp.ElicitationResponseActionAccept, map[string]any{"approve": true}))
	if result.IsError || f.executed.Load() != 1 {
		t.Fatalf("approved change did not execute exactly once: executed=%d result=%+v", f.executed.Load(), result)
	}

	// The same answer replayed must not execute again.
	replay := call(t, f.tool, ctx, withAnswer(scaleCall(nil), state, mcp.ElicitationResponseActionAccept, map[string]any{"approve": true}))
	if !replay.IsError || f.executed.Load() != 1 {
		t.Fatalf("replayed approval executed again: executed=%d result=%+v", f.executed.Load(), replay)
	}
}

func TestMutationIsNotExecutedWithoutAcceptance(t *testing.T) {
	for name, answer := range map[string]struct {
		action  mcp.ElicitationResponseAction
		content any
	}{
		"declined":          {mcp.ElicitationResponseActionDecline, nil},
		"cancelled":         {mcp.ElicitationResponseActionCancel, nil},
		"accepted unticked": {mcp.ElicitationResponseActionAccept, map[string]any{"approve": false}},
		"approve not bool":  {mcp.ElicitationResponseActionAccept, map[string]any{"approve": "yes"}},
	} {
		t.Run(name, func(t *testing.T) {
			f := newScaleFixture(t, true)
			ctx := modernClient("session-a", true)
			state, _ := askApproval(t, f, ctx)
			result := call(t, f.tool, ctx, withAnswer(scaleCall(nil), state, answer.action, answer.content))
			if f.executed.Load() != 0 {
				t.Fatal("change executed without acceptance")
			}
			if text := resultText(result); !strings.Contains(text, "did not approve") {
				t.Fatalf("result does not report the refusal: %s", text)
			}
		})
	}
}

func TestMutationRejectsMissingOrForeignApprovalState(t *testing.T) {
	f := newScaleFixture(t, true)
	ctx := modernClient("session-a", true)
	state, _ := askApproval(t, f, ctx)

	// An answer without the echoed state is not an approval.
	noState := call(t, f.tool, ctx, withAnswer(scaleCall(nil), "", mcp.ElicitationResponseActionAccept, nil))
	if f.executed.Load() != 0 || !strings.Contains(resultText(noState), "did not approve") {
		t.Fatalf("answer without state was treated as approval: %+v", noState)
	}

	// A made-up plan ID is refused by the safety decorator.
	forged := call(t, f.tool, ctx, withAnswer(scaleCall(nil), "forged-plan", mcp.ElicitationResponseActionAccept, nil))
	if !forged.IsError || f.executed.Load() != 0 {
		t.Fatalf("forged plan state executed: %+v", forged)
	}

	// The real plan answered from another session is refused too.
	other := call(t, f.tool, modernClient("session-b", true), withAnswer(scaleCall(nil), state, mcp.ElicitationResponseActionAccept, nil))
	if !other.IsError || f.executed.Load() != 0 {
		t.Fatalf("plan approved from another session executed: %+v", other)
	}
}

func TestModelCannotConfirmOrBypassApproval(t *testing.T) {
	f := newScaleFixture(t, true)
	ctx := modernClient("session-a", true)
	state, _ := askApproval(t, f, ctx)

	result := call(t, f.tool, ctx, scaleCall(map[string]any{"confirm_plan_id": state}))
	if !result.IsError || !strings.Contains(resultText(result), "confirm_plan_id is not accepted") || f.executed.Load() != 0 {
		t.Fatalf("model-supplied confirm_plan_id was not refused: %+v", result)
	}
	if _, exists := f.tool.Tool.InputSchema.Properties["confirm_plan_id"]; exists {
		t.Fatal("confirm_plan_id is still advertised in the tool schema")
	}
}

func TestMutationFailsClosedWithoutApprovalSupport(t *testing.T) {
	f := newScaleFixture(t, true)
	cases := map[string]context.Context{
		"modern client without elicitation":             modernClient("session-a", false),
		"no session":                                    context.Background(),
		"legacy session on a transport that cannot ask": server.NewMCPServer("t", "1").WithContext(context.Background(), &legacySession{elicitation: true}),
		"legacy client without elicitation":             server.NewMCPServer("t", "1").WithContext(context.Background(), &legacySession{}),
	}
	for name, ctx := range cases {
		t.Run(name, func(t *testing.T) {
			result := call(t, f.tool, ctx, scaleCall(nil))
			if !result.IsError || result.ResultType == mcp.ResultTypeInputRequired || f.executed.Load() != 0 {
				t.Fatalf("mutation was not refused: %+v", result)
			}
			if !strings.Contains(resultText(result), "needs the user's approval") {
				t.Fatalf("refusal does not explain why: %s", resultText(result))
			}
		})
	}
}

func TestPreviewOnlyNeverAsksOrExecutes(t *testing.T) {
	f := newScaleFixture(t, true)
	result := call(t, f.tool, context.Background(), scaleCall(map[string]any{"dry_run": true}))
	if result.IsError || result.ResultType == mcp.ResultTypeInputRequired || f.executed.Load() != 0 {
		t.Fatalf("preview asked or executed: %+v", result)
	}
	text := resultText(result)
	if strings.Contains(text, "plan_id") || !strings.Contains(text, "PREVIEW ONLY") {
		t.Fatalf("preview exposes a plan or lacks the preview label: %s", text)
	}
}

func TestNoChangeNeededSkipsApproval(t *testing.T) {
	f := newScaleFixture(t, true)
	result := call(t, f.tool, modernClient("session-a", true), scaleCall(map[string]any{"node_count": 2}))
	if result.ResultType == mcp.ResultTypeInputRequired || !strings.Contains(resultText(result), `"no_change_needed":true`) {
		t.Fatalf("no-op change asked for approval: %+v", result)
	}
}

func TestSafetyDisabledExecutesDirectly(t *testing.T) {
	f := newScaleFixture(t, false)
	result := call(t, f.tool, context.Background(), scaleCall(nil))
	if result.IsError || f.executed.Load() != 1 {
		t.Fatalf("SAFETY_ENABLED=false did not execute directly: %+v", result)
	}
}

func TestOtherMutationToolsUseApproval(t *testing.T) {
	log := slog.New(slog.DiscardHandler)
	var trafficExecuted, exportExecuted atomic.Int32
	fake := &testutil.FakeGCPService{
		UpdateTrafficFunc: func(_ context.Context, req models.UpdateTrafficRequest) (models.UpdateTrafficResponse, error) {
			if !req.DryRun {
				trafficExecuted.Add(1)
			}
			return models.UpdateTrafficResponse{
				ServiceName: req.ServiceName,
				Before:      []models.TrafficTarget{{Revision: "api-1", Percent: 100}},
				After:       []models.TrafficTarget{{Revision: "api-2", Percent: 100}},
			}, nil
		},
		ExportRecommendationsToBQFunc: func(_ context.Context, req models.ExportRecommendationsToBQRequest) (models.ExportRecommendationsToBQResponse, error) {
			if !req.DryRun {
				exportExecuted.Add(1)
			}
			return models.ExportRecommendationsToBQResponse{RowsPlanned: 7, Table: "p.recs.gcp_recommendations"}, nil
		},
	}
	svc := safety.NewSafetyDecorator(fake, log)
	for _, tc := range []struct {
		tool     server.ServerTool
		args     map[string]any
		want     string
		executed *atomic.Int32
	}{
		{NewCloudRunTools(svc, log).UpdateTraffic(), map[string]any{"project_id": "p", "region": "europe-west1", "service_name": "api", "traffic": []any{map[string]any{"revision": "api-2", "percent": 100}}}, "from 100% api-1 to 100% api-2", &trafficExecuted},
		{NewRecommenderExportTools(svc, log, "recs").ExportRecommendationsToBQ(), map[string]any{"project_id": "p"}, "Insert 7 active Recommender recommendations", &exportExecuted},
	} {
		t.Run(tc.tool.Tool.Name, func(t *testing.T) {
			if _, exists := tc.tool.Tool.InputSchema.Properties["confirm_plan_id"]; exists {
				t.Fatal("confirm_plan_id is still advertised")
			}
			ctx := modernClient("session-a", true)
			req := mcp.CallToolRequest{Params: mcp.CallToolParams{Name: tc.tool.Tool.Name, Arguments: tc.args}}
			first := call(t, tc.tool, ctx, req)
			prompt, ok := first.InputRequests[approvalInputID]
			if first.ResultType != mcp.ResultTypeInputRequired || !ok || !strings.Contains(prompt.Elicitation.Message, tc.want) {
				t.Fatalf("first call did not ask for approval with %q: %+v", tc.want, first)
			}
			if tc.executed.Load() != 0 {
				t.Fatal("executed before approval")
			}
			second := call(t, tc.tool, ctx, withAnswer(req, first.RequestState, mcp.ElicitationResponseActionAccept, nil))
			if second.IsError || tc.executed.Load() != 1 {
				t.Fatalf("approved change did not execute once: executed=%d result=%+v", tc.executed.Load(), second)
			}
		})
	}
}

// legacySession is a pre-2026-07-28 session on a transport that cannot send
// server-initiated requests (like mcp-go's SSE session): it records client
// capabilities but does not implement server.SessionWithElicitation.
type legacySession struct {
	elicitation bool
}

func (s *legacySession) Initialize()       {}
func (s *legacySession) Initialized() bool { return true }
func (s *legacySession) NotificationChannel() chan<- mcp.JSONRPCNotification {
	return make(chan mcp.JSONRPCNotification, 1)
}
func (s *legacySession) SessionID() string                            { return "legacy" }
func (s *legacySession) GetClientInfo() mcp.Implementation            { return mcp.Implementation{} }
func (s *legacySession) SetClientInfo(mcp.Implementation)             {}
func (s *legacySession) SetClientCapabilities(mcp.ClientCapabilities) {}
func (s *legacySession) GetClientCapabilities() mcp.ClientCapabilities {
	if s.elicitation {
		return mcp.ClientCapabilities{Elicitation: &mcp.ElicitationCapability{}}
	}
	return mcp.ClientCapabilities{}
}

func resultText(result *mcp.CallToolResult) string {
	var builder strings.Builder
	for _, content := range result.Content {
		if text, ok := content.(mcp.TextContent); ok {
			builder.WriteString(text.Text)
		}
	}
	return builder.String()
}
