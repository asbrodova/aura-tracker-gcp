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
| `ANONYMIZE_ENABLED` | No | Set `true` to enable PII scrubbing on all tool outputs (overrides YAML `enabled`) |
| `ANONYMIZE_CONFIG_PATH` | No | Path to YAML config file for the anonymization engine |

## README Hygiene

Always check `README.md` is up to date **before committing**. Update it whenever you:
- Add, remove, or rename a tool (update the count in the intro, the Tools table, the architecture diagram, the Project Layout section, and example prompts)
- Change environment variables, prerequisites, or architecture

Stage README changes in the same commit as the code that required them.

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

## Anonymization Engine

PII/credential scrubbing runs as middleware on every tool result, applied before the LLM sees the output. Off by default.

### Packages

| File | Purpose |
|------|---------|
| `internal/anonymize/anonymize.go` | `Anonymizer` interface, `AuditReport`/`Finding` types, `NoopAnonymizer` |
| `internal/anonymize/config.go` | `Config` struct, `LoadConfig()` (YAML + env-var override) |
| `internal/anonymize/local.go` | `LocalScrubber`: built-in regexes, JSON walker, per-call token registry |
| `internal/anonymize/middleware.go` | `WrapHandler(tool, a)` — wraps any `server.ServerTool` handler |
| `internal/anonymize/dlp.go` | `DLPAnonymizer` skeleton (Phase 2; compile-time interface check only) |
| `ports/dlp_service.go` | `DLPService` secondary port interface |
| `internal/gcp/dlp.go` | `dlpAdapter` skeleton (Phase 2) |

### Enabling

```bash
# Minimal: enable with defaults (local mode, masking on)
ANONYMIZE_ENABLED=true GCP_PROJECT_ID=my-project go run ./cmd/aura-tracker-gcp

# With config file
ANONYMIZE_ENABLED=true ANONYMIZE_CONFIG_PATH=/path/to/anonymize.yaml \
  GCP_PROJECT_ID=my-project go run ./cmd/aura-tracker-gcp
```

### Audit / Dry-Run Mode

Set `audit_only: true` in the YAML config (or add it to the file and point `ANONYMIZE_CONFIG_PATH` at it). Every tool result is replaced with an `AuditReport` JSON showing matched patterns and JSON paths — no actual masking. Use this to tune patterns before turning on real scrubbing.

### Built-in Patterns

`internal_ip` · `public_ip` · `email` · `service_account` · `gcp_api_key`

Custom patterns are appended via the `patterns:` list in the YAML config.

### Adding a New Anonymizer Backend

1. Implement `Anonymizer` in `internal/anonymize/<name>.go`
2. If it needs a GCP service, add a port to `ports/<name>_service.go` and an adapter to `internal/gcp/<name>.go`
3. Wire the constructor in the `switch anonCfg.Mode` block in `cmd/aura-tracker-gcp/main.go`

## Phase 2 Work (Not Yet Implemented)

`gcp_gke_scale_deployment` currently resizes GKE **node pools** via the GKE management API.
Scaling individual **Kubernetes Deployments** requires `k8s.io/client-go` (significant dep addition):
1. `container.GetCluster` to fetch endpoint + CA cert
2. `golang.org/x/oauth2/google` for access token
3. `k8s.io/client-go/kubernetes` for `AppsV1().Deployments().Patch()`
