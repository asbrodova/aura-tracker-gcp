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

Application-layer engines live between MCP and the port: `internal/diagnostics` for incidents, `internal/costreasoning` for cost explanations, `internal/securityaudit` for deterministic security posture findings and scoring, `internal/diagram` for architecture scoping and deterministic rendering, and `internal/drift` for symmetric cross-environment configuration comparison. They depend on narrow interfaces satisfied by `ports.GCPService` and never import GCP SDK packages.

## Environment Variables

| Variable | Required | Description |
|----------|----------|-------------|
| `GCP_PROJECT_ID` | Yes* | Legacy single-project setting. Omit when using YAML `environments` or `GCP_ENVIRONMENTS_JSON`. |
| `GCP_ENVIRONMENTS_JSON` | No | JSON array of `{project_id, alias, default}` entries. Multiple entries require aliases and exactly one default. |
| `GOOGLE_APPLICATION_CREDENTIALS` | No | Service account key path (optional with ADC) |
| `ANONYMIZE_ENABLED` | No | Set `true` to enable PII scrubbing on all tool outputs (overrides YAML `enabled`) |
| `ANONYMIZE_CONFIG_PATH` | No | Path to YAML config file for the anonymization engine |
| `ANONYMIZE_PROJECT_ID` | No | Masks an unaliased project as `[GCP_PROJECT_ID]`. Configured aliases are always enforced on MCP output. |
| `RECOMMENDER_ENABLED` | No | Set `false` to disable Cloud Recommender API integration. **On by default.** Enriches Aura Scores with idle/overprovisioned signals; 12h cache prevents quota exhaustion. |
| `RECOMMENDER_BQ_EXPORT_ENABLED` | No | Set `true` to enable the `gcp_export_recommendations_to_bq` MCP tool. Off by default. |
| `RECOMMENDER_BQ_EXPORT_DATASET` | No | BigQuery dataset name for the recommendations export (overrides `recommender_export.dataset` in YAML). |
| `COST_REASONING_ENABLED` | No | Set `true` to register the read-only `gcp_cost_explain` tool. Off by default. |
| `COST_REASONING_SOURCES_JSON` | No | Strict JSON array mapping configured environments to query/export projects and billing datasets. Mutually exclusive with YAML `sources` and legacy source settings. |
| `COST_QUERY_PROJECT_ID` | No | Project that owns BigQuery query jobs. Defaults to `GCP_PROJECT_ID`. |
| `BILLING_EXPORT_PROJECT_ID` | No | Project containing the detailed billing export. Defaults to the query project. |
| `BILLING_EXPORT_DATASET` | Cost only | Required dataset containing `gcp_billing_export_resource_v1_*`. |
| `BILLING_EXPORT_TABLE` | No | Optional exact detailed billing table; otherwise the sole matching table is discovered. |
| `COST_REASONING_TIMEZONE` | No | IANA timezone for complete-day comparisons. Default `UTC`. |
| `COST_REASONING_HISTORY_DAYS` | No | Billing history window, 14–366 days. Default 90. |
| `COST_QUERY_MAX_BYTES` | No | Per-statement BigQuery bytes-billed ceiling. Default 5 GiB. |
| `GRAPHVIZ_DOT_PATH` | No | Path to Graphviz `dot` for SVG architecture diagrams. Mermaid and Graphviz DOT source do not require it. |

## User Config File

Optional per-user defaults in `~/.aura-tracker.yaml`. Legacy scalar settings are overridden by their environment variables; environment lists must use exactly one configuration source.

Multiple environments use chat-safe aliases:

```yaml
environments:
  - project_id: my-company-123
    alias: dev
    default: true
  - project_id: my-company-345
    alias: prod
```

Aliases are case-insensitive inputs. Configured project IDs are also accepted as inputs, but an aliased project ID is never returned in MCP results, errors, prompts, resources, or diagrams. Missing selectors use the default environment. Do not combine `environments` with legacy `project_id` or `GCP_PROJECT_ID`.

```yaml
project_id: my-gcp-project-id   # fallback when GCP_PROJECT_ID is not set

recommender_export:
  enabled: false      # set true to enable gcp_export_recommendations_to_bq
  dataset: ""         # BigQuery dataset name

cost_reasoning:
  enabled: false
  query_project_id: finops-project
  export_project_id: finops-project
  dataset: cloud_billing
  table: ""
  timezone: UTC
  history_days: 90
  max_bytes_billed: 5368709120

security_audit:
  kubernetes_access: auto
  fleet_project_id: platform-fleet
  cluster_concurrency: 4
  per_cluster_timeout_seconds: 20
  max_resources_per_kind: 2000
  suppressions:
    - rule_id: PUB-001
      resource: "//run.googleapis.com/projects/my-gcp-project-id/locations/*/services/public-api"
      reason: "Approved public API"
      owner: "platform-security@example.com"
      expires_at: "2026-12-01T00:00:00Z"
```

## CLI Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--modules` | *(all)* | Comma-separated list of tool modules to enable. Phase 1: `gke`, `cloudrun`, `pubsub`, `logging`, `monitoring`, `iam`, `topology`, `aura`, `storage`, `functions`, `eventarc`, `scheduler`, `workflows`, `tasks`, `secretmanager`, `vpcaccess`, `cloudsql`, `serverlessgraph`. Phase 2: `gke_workloads`, `gke_mesh`, `networking`, `datastores`, `supplychain`, `coverage`, `archgraph`, `tagging`. Correlation/reasoning: `incident`, `security`, `drift`, `cost`; `cost` also requires `COST_REASONING_ENABLED=true`. Use `none` for zero tools (resources and non-module prompts remain). Each excluded module also skips its GCP client connection at startup. |
| `--version` | — | Print version and exit. |

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
   - Declare `mcp.Required()` parameters before optional ones in `mcp.NewTool(...)` — the mcp-go library preserves declaration order in the JSON Schema, so required fields appear first in the LLM's context window
5. Add the tool to `GetTools()` on the domain's `*Tools` struct (in the same file)
6. If adding a new domain, also add it to `internal/gcp/modules.go` (`moduleClientDeps`) and `internal/mcp/registry.go` (`AllModules`)

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

## Notes

`gcp_gke_scale_deployment` resizes GKE **node pools** via the GKE management API.
Scaling individual **Kubernetes Deployments** requires `k8s.io/client-go` (significant dep addition) and is not currently implemented.
