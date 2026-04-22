# CLAUDE.md

## Commands

```bash
# Build
go build ./...

# Test (always use -race)
go test -race ./...

# Single package
go test -race ./internal/gcp/... -run TestAggregateBottlenecks

# Vet
go vet ./...

# Run server
GCP_PROJECT_ID=my-project go run ./cmd/aura-tracker-gcp

# Smoke-test tools/list
echo '{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}' \
  | GCP_PROJECT_ID=my-project go run ./cmd/aura-tracker-gcp
```

## Architecture

Hexagonal Architecture. The hexagon boundary is `ports/gcp_service.go`.

```
cmd/  ──►  internal/mcp/  ──►  ports/GCPService
cmd/  ──►  internal/gcp/  ──►  ports/GCPService
internal/gcp/  ──►  GCP SDK
internal/mcp/  NEVER imports internal/gcp/
```

`go build ./internal/mcp/...` compiles zero GCP SDK code.

## Environment Variables

| Variable | Required | Description |
|----------|----------|-------------|
| `GCP_PROJECT_ID` | Yes | GCP project for SDK client init |
| `GOOGLE_APPLICATION_CREDENTIALS` | No | Service account key path (optional with ADC) |

## Adding a New Tool

1. Add input/output structs to `pkg/models/<domain>.go`
2. Add the method signature to `ports/gcp_service.go`
3. Implement the method on `gcpAdapter` in `internal/gcp/<domain>.go`
   - Call `a.rateWait(ctx, "domain.Method")` first
   - Call `a.withTimeout(ctx)` and `defer cancel()`
   - Wrap errors with `wrapGCPError("domain.Method", err)`
4. Create the tool definition + handler in `internal/mcp/tools/<domain>.go`
   - Use `mcp.NewTypedToolHandler(t.handlerFunc)` for automatic arg binding
   - Call `handleServiceError(toolName, err)` on service errors
5. Register the tool in `internal/mcp/server.go`

## Error Handling Rules

| GCP error | Adapter output | Handler output | LLM sees |
|-----------|---------------|----------------|----------|
| `codes.PermissionDenied` | `*PermissionDeniedError` | `mcp.NewToolResultError(...)` | `IsError: true` + message |
| `codes.NotFound` | `*NotFoundError` | `mcp.NewToolResultError(...)` | `IsError: true` + message |
| Any other error | wrapped `error` | `return nil, err` | JSON-RPC -32603 |

## Rate Limiting & Timeouts

- Rate limiter: 10 rps, burst 20 (token bucket via `golang.org/x/time/rate`)
- Placed at the port boundary in `gcpAdapter` — every GCP API call is throttled here
- Call timeout: 30s per method, applied inside the adapter
- For `GetClusterBottlenecks`: the 30s budget is shared across all 4 fan-out goroutines via `errgroup.WithContext`

## Mutation Tools (Safe-Apply Pattern)

Both mutation tools support `dry_run: true`:
- Returns a description of what WOULD happen without executing
- Idempotent: operation at current state → `no_change_needed: true`

## Phase 2 Work (Not Yet Implemented)

`gcp_gke_scale_deployment` currently resizes GKE **node pools** via the GKE management API.
Scaling individual **Kubernetes Deployments** requires `k8s.io/client-go` (significant dep addition):
1. `container.GetCluster` to fetch endpoint + CA cert
2. `golang.org/x/oauth2/google` for access token
3. `k8s.io/client-go/kubernetes` for `AppsV1().Deployments().Patch()`
