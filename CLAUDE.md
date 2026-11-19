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

# Make targets (all of build, test, lint and smoke must pass before a PR)
make build        # ./aura-tracker-gcp with the version stamped in
make test         # go test -race ./...
make lint         # golangci-lint at the pinned version; reinstalls on version mismatch
make smoke        # protocol compliance + "N tools registered", no GCP credentials needed
make test-cover   # coverage.out + coverage.html

# Hexagon boundary check: must print nothing
go list -deps ./internal/mcp/... | grep cloud.google.com
```

The Go version is the one in `go.mod`; the Dockerfile builder must match its major.minor and be pinned by `@sha256` digest (CI enforces both).

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

`cmd/` wraps the adapter in `internal/safety.SafetyDecorator` (the plan store behind mutation approval) before handing it to the MCP server. The repository's wiki is a separate git repo, checked out at `wiki/` and git-ignored here.

### Panic containment

- Every tool, resource and prompt handler is wrapped by `internal/mcp/recovery.go`. A panic becomes a generic error for that one request; the panic value and stack go to the server log only, because a recovered panic bypasses the project-ID privacy filters.
- Goroutines go through `internal/safego`: `safego.WithContext` instead of `errgroup.WithContext`, and `defer safego.Recover(func(err error) { ... })` as the first defer of a raw `go func()`. `internal/safego/usage_test.go` fails on a direct `errgroup` import or an unprotected goroutine under `internal/`.

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
| `SAFETY_ENABLED` | No | Default `true`: mutations run only after the user approves them in the MCP client. `false` executes them without asking and is rejected when `MCP_TRANSPORT=sse`. |
| `TRACE_BACKEND` | No | `trace` (default) or `monitoring`: source for trace-based dependency edges. |
| `GRAPH_TIMEOUT_SECONDS` | No | Budget for architecture/serverless graph collection, 1–3600. Default 120. |
| `MCP_TRANSPORT` | No | Empty for stdio (default) or `sse` for the HTTP/SSE server. |
| `PORT` | SSE only | Listen port, default 8080 (Cloud Run sets it). |
| `MCP_BASE_URL` | SSE only | Public absolute URL of the server. Defaults to `http://localhost:$PORT` with a warning; non-loopback URLs must be `https`. |
| `MCP_AUTH_MODE` | SSE only | `required` (default): Google ID token on every request. `disabled` is allowed only with a loopback `MCP_BASE_URL` and then binds to 127.0.0.1. |
| `MCP_AUTH_AUDIENCE` | SSE only | Expected token audience. Defaults to the origin (plus base path) of `MCP_BASE_URL`. |
| `MCP_AUTH_ALLOWED_EMAILS` | SSE only | Comma-separated allowlist of verified caller emails. A public URL needs this or `MCP_AUTH_ALLOW_ANY_VALID_TOKEN=true`. |
| `MCP_AUTH_ALLOW_ANY_VALID_TOKEN` | SSE only | `true` accepts any valid Google ID token for the audience. Explicit opt-in. |

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
| `--modules` | *(all)* | Comma-separated list of tool modules to enable. Serverless and core: `gke`, `cloudrun`, `pubsub`, `logging`, `monitoring`, `iam`, `topology`, `aura`, `storage`, `functions`, `eventarc`, `scheduler`, `workflows`, `tasks`, `secretmanager`, `vpcaccess`, `cloudsql`, `serverlessgraph`. Platform: `gke_workloads`, `gke_mesh`, `networking`, `datastores`, `supplychain`, `coverage`, `archgraph`, `tagging`. Correlation/reasoning: `incident`, `security`, `drift`, `cost`; `cost` also requires `COST_REASONING_ENABLED=true`. Opt-in only (not part of the default set): `recommender_export`, which also needs `RECOMMENDER_BQ_EXPORT_ENABLED=true`. Use `none` for zero tools (resources and non-module prompts remain). Each excluded module also skips its GCP client connection at startup. |
| `--version` | — | Print version and exit. |

## README Hygiene

Always check `README.md` is up to date **before committing**. Update it whenever you:
- Add, remove, or rename a tool (update the count in the intro, the Tools table, the architecture diagram, the Project Layout section, and example prompts)
- Change environment variables, prerequisites, or architecture

Stage README changes in the same commit as the code that required them.

## Adding a New Tool

1. **Models**: request/response structs in `pkg/models/<domain>.go` (JSON tags, no SDK types).
2. **Port**: method on the domain sub-interface in `ports/gcp_service.go`.
3. **Adapter**: implement on `gcpAdapter` in `internal/gcp/<domain>.go`:
   - `a.rateWait(ctx, "domain.Method")` first, then `ctx, cancel := a.withTimeout(ctx); defer cancel()`;
   - return `errClientNotInitialized(op, clientX)` if the SDK client is nil (modules construct only the clients they declare);
   - exhaust pagination or expose an opaque `next_page_token`; bound fan-out and report partial coverage explicitly;
   - fan out with `safego.WithContext`, never a bare `errgroup` or `go func()`;
   - wrap errors with `wrapGCPError("domain.Method", err)`.
4. **Safety decorator**: add the method to `internal/safety/decorator.go`. Reads are one-line pass-throughs; a request with a `DryRun` field is a mutation and needs the full plan logic (`decorator_guard_test.go` enforces both), and its tool handler must go through `runMutation` so the user approves it.
5. **Test fake**: add `<Method>Func` to `internal/testutil/FakeGCPService` and have the method call it (`fake_gcp_service_test.go` enforces this); update `internal/mcp/tools/mock_test.go` if handler tests use it.
6. **MCP tool**: definition and handler in `internal/mcp/tools/<domain>.go`:
   - `mcp.NewTypedToolHandler(t.handlerFunc)` for typed argument binding;
   - declare `mcp.Required()` parameters before optional ones (mcp-go keeps declaration order in the JSON Schema, so required fields come first in the model's context);
   - put bounds in the schema and re-validate in the adapter; set `ReadOnlyHint` / `DestructiveHint` / `IdempotentHint`;
   - on error `return handleServiceError(toolName, err)`; return structured JSON.
7. **Register**: add it to `GetTools()` on the domain's `*Tools` struct.
8. **New domain only**: `internal/gcp/modules.go` (`moduleClientDeps`: every module needs an entry, or `--modules=<name>` starts without its clients), `internal/mcp/registry.go` (constant + `AllModules`), `internal/mcp/server.go` (`allModules`), `internal/mcp/permissions.go` (`modulePermissions`), `scripts/setup-iam.sh` and `setup-iam_test.sh`. Consider whether `archgraph`, `drift` or `security` should collect it.
9. **Tests**: adapter helpers, handler, schema (`read_schema_test.go` / `mutation_schema_test.go`), routing and error behaviour. The module-matrix test (`cmd/aura-tracker-gcp/module_matrix_test.go`) exercises every tool with only its module enabled.
10. **Docs in the same PR**: README (counts, tables, example prompts) and the wiki (per-module parameter table mirroring every `mcp.With*`, `Module-Reference`, counts on `Home`, `_Sidebar.md`, IAM requirements).

## Error Handling Rules

`wrapGCPError` (`internal/gcp/errors.go`) maps SDK errors to typed port errors; `handleServiceError` (`internal/mcp/tools/errors.go`) maps those to tool results.

| Adapter error | LLM sees |
|---------------|----------|
| `*PermissionDeniedError` (REST 401/403, gRPC PermissionDenied/Unauthenticated) | `IsError: true` + `ToolError` JSON with `missing_iam_permissions` |
| `*RecommenderQuotaExhaustedError` | `IsError: true` + `ToolError` JSON, `retriable: true`, `retry_at` |
| `*RetriableError` (REST 408/429/5xx, transient gRPC) | `IsError: true` + `ToolError` JSON, `retriable: true` |
| `*NotFoundError` | `IsError: true` + message |
| `*ConfirmationRequiredError` (safety decorator) | `IsError: true` + why the plan was refused (expired, used, other session, resource changed) |
| `*CostSourceNotConfiguredError` | `IsError: true` + message |
| Any other error | JSON-RPC -32603, message passed through the project-ID masker |

## Rate Limiting & Timeouts

- One token bucket per adapter, 10 rps with burst 20 (`gcpadapter.WithRateLimit(10, 20)` in `cmd/`; not configurable at runtime).
- It is enforced on every outgoing request, not per adapter method: `internal/gcp/transport.go` wraps the shared HTTP client (`rateLimitedRoundTripper`) and adds unary and stream gRPC interceptors, so pagination, retries and nested collectors each count. `a.rateWait` only checks for a cancelled context.
- Call timeout: 30s per adapter method via `a.withTimeout(ctx)`. Graph collection has its own budget (`GRAPH_TIMEOUT_SECONDS`, default 120s).

## Mutation Tools (Human Approval)

Three tools change state: `gcp_gke_scale_deployment` (resizes a GKE **node pool**), `gcp_cloudrun_update_traffic`, and the opt-in `gcp_export_recommendations_to_bq`. The model cannot execute any of them on its own:

1. The tool handler (`runMutation` in `internal/mcp/tools/approval.go`) previews the change through `SafetyDecorator`, which stores a plan: TTL 10 minutes, bound to the caller identity and session, single-use. A request already at the target state returns `no_change_needed: true` with no plan.
2. The handler returns an MCP elicitation (`server.NewInputRequestBuilder`) asking the **user** to approve, with the plan ID as opaque request state. Clients on protocol 2026-07-28 retry the call with the answer; for older clients mcp-go sends `elicitation/create` and re-invokes the handler.
3. Only an `accept` answer (without `approve: false`) executes the stored plan, not the retry's arguments. Scale and traffic changes carry a state guard (the previewed per-zone node count or the service ETag), so a resource that changed since the preview is refused; exports carry the plan ID as their execution ID.
4. `confirm_plan_id` is not in the tool schemas and is refused if the model sends it. `dry_run: true` only previews (no plan ID, no prompt).
5. Fail closed: a client without elicitation support, or a legacy session on mcp-go's SSE transport (which cannot send server-initiated requests), is refused.

Approval prompts pass through the project-ID masker (`maskInputRequests` in `server.go`). `SAFETY_ENABLED=false` (local stdio only) executes without a prompt.

## Anonymization Engine

PII/credential scrubbing runs as middleware on every tool result, applied before the LLM sees the output. Off by default.

### Packages

| File | Purpose |
|------|---------|
| `internal/anonymize/anonymize.go` | `Anonymizer` interface, `AuditReport`/`Finding` types, `NoopAnonymizer` |
| `internal/anonymize/config.go` | `Config` struct, `LoadConfig()` (YAML + env-var override) |
| `internal/anonymize/local.go` | `LocalScrubber`: built-in regexes, JSON walker, per-call token registry |
| `internal/anonymize/middleware.go` | `WrapHandler(tool, a)` — wraps any `server.ServerTool` handler |
| `internal/anonymize/dlp.go` | `DLPAnonymizer`: Cloud DLP inspection in chunks of at most 500 KB, offset-based masking |
| `internal/anonymize/chain.go` | `ChainedAnonymizer`: local regexes first, then DLP (`mode: both`) |
| `internal/anonymize/projectid.go` | Project-ID masker that enforces environment aliases on every MCP surface (always on, independent of `ANONYMIZE_ENABLED`) |
| `ports/dlp_service.go` | `DLPService` secondary port interface |
| `internal/gcp/dlp.go` | `dlpAdapter`: rate-limited DLP `InspectContent` client |

### Modes

`mode: local` (default, regex only, no GCP call), `mode: dlp` (Cloud DLP only), `mode: both` (local, then DLP). DLP settings live under `dlp:` in the YAML config: `project_id` (defaults to the server project) and `info_types`.

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

`gcp_gke_scale_deployment` resizes GKE **node pools** via the GKE management API, despite its name. Scaling individual Kubernetes Deployments is not implemented.
