# Contributing to aura-tracker-gcp

Thank you for improving this project. This guide covers the dev loop, architectural rules, and PR expectations.

## Governance and maintainer approval

Contributions are welcome through pull requests.

### Maintainer approval

All pull requests require maintainer review and are merged by [@asbrodova](https://github.com/asbrodova). Passing CI confirms that automated checks succeeded; it does not constitute approval.

### Releases

Version tags and official Aura Tracker GCP releases are created and published exclusively by [@asbrodova](https://github.com/asbrodova).

## Prerequisites

- The Go version declared in `go.mod` (currently Go 1.27.4 or newer; check with `go version`)
- `gcloud` CLI authenticated: `gcloud auth application-default login`
- `GCP_PROJECT_ID` set to a project you control for manual smoke tests

## Dev loop

```bash
make build     # compile
make test      # run all tests with race detector
make lint      # golangci-lint (auto-installs on first run)
make smoke     # stdio round-trip: prints "OK — N tools registered"
```

All four must pass before opening a PR.

## Adding a new tool

Use this client-neutral checklist. See the [architecture and contributing guide](https://github.com/asbrodova/aura-tracker-gcp/wiki/Architecture-and-Contributing) for the full design context.

1. Add request/response structs to `pkg/models/<domain>.go`
2. Add the method signature to `ports/gcp_service.go`
3. Implement the method on `gcpAdapter` in `internal/gcp/<domain>.go`. Return `errClientNotInitialized` when the SDK client is nil, and start goroutines only through `internal/safego`
4. Add the method to `SafetyDecorator` in `internal/safety/decorator.go`: a one-line pass-through for reads, the full plan flow for mutations
5. Add a `<Method>Func` field to `internal/testutil/FakeGCPService` and call it from the fake method
6. Create the tool definition + handler in `internal/mcp/tools/<domain>.go`. Select the project with a `project_id` parameter, and name every duration with its unit (`lookback_minutes`, `lookback_hours`, `_seconds`, `_days`) and state the unit and range in its description; `internal/mcp/parameter_conventions_test.go` checks both
7. Register it in `GetTools()` on the domain's `*Tools` struct
8. Add focused tests for the adapter, handler, schema, routing, and error behavior that changed

Guard tests fail if steps 3–5 are skipped: `internal/safety/decorator_guard_test.go`, `internal/testutil/fake_gcp_service_test.go`, `internal/safego/usage_test.go`, and the per-module matrix in `cmd/aura-tracker-gcp/module_matrix_test.go`.

When adding a new domain, also update every integration surface:

1. `internal/gcp/modules.go` for GCP client construction and module selection (every module needs a `moduleClientDeps` entry)
2. `internal/mcp/registry.go` and `internal/mcp/server.go` for module registration
3. `internal/mcp/permissions.go` for the domain's permission requirements
4. `scripts/setup-iam.sh` for matching API and IAM setup

After adding a tool, update the relevant README and wiki counts, capability descriptions, architecture material, and example prompts in the same pull request.

## Architecture rules

The hexagon boundary is `ports/gcp_service.go`. Breaking it fails CI.

- `internal/gcp/` imports GCP SDKs — never imports `internal/mcp/`
- `internal/mcp/` imports `ports/` only — never imports `internal/gcp/`
- Verify the boundary: `go list -deps ./internal/mcp/... | grep cloud.google.com` must be empty

## Error handling contract

Every GCP error must flow through this chain — do not break it:

```
GCP SDK error
  → wrapGCPError("domain.Method", err)    [internal/gcp/errors.go]
  → PermissionDeniedError / NotFoundError / RetriableError / RecommenderQuotaExhaustedError
  → handleServiceError("tool_name", err)  [internal/mcp/tools/errors.go]
  → mcp.NewToolResultError(json) or mcp.NewToolResultError(msg)
  → LLM receives IsError: true + structured ToolError JSON
```

Avoid returning raw GCP SDK errors to the MCP layer — the LLM cannot act on gRPC status strings.

## Mutation tools

The three mutation tools (`gcp_gke_scale_deployment`, `gcp_cloudrun_update_traffic`, and the opt-in `gcp_export_recommendations_to_bq`) exist only when the server runs with `MUTATIONS_ENABLED=true`, and execute only after the user approves them in the MCP client. `SafetyDecorator` stores the previewed plan; the tool handler asks the user through MCP elicitation. Any new mutation tool must:

1. Have a request type with `DryRun` and `ConfirmPlanID` fields; the decorator guard test treats any request with `DryRun` as a mutation
2. Implement the decorator's preview → stored plan → confirm flow, replaying the stored request with a state guard so a resource that changed since the preview is refused
3. Return `no_change_needed: true` when the resource is already at the requested state
4. Route its handler through `runMutation` (`internal/mcp/tools/approval.go`), keep `confirm_plan_id` out of the tool schema, and describe the change in the approval prompt clearly enough for a person to decide
5. Set `ReadOnlyHint: false` (that is what keeps it off in read-only mode), add it to `knownMutationTools` in `internal/mcp/mutations_switch_test.go`, and put its write permissions in `mutationPermissions`

## Submitting a PR

- One logical change per PR; keep diffs reviewable
- All automated checks must be green: Test, Lint, Docker build, GoReleaser config check, Vulnerability and secret scan, and the configured CodeQL analyses
- Update `README.md` if you changed tools, env vars, or architecture
- Address review feedback and resolve conversations; the maintainer handles the final squash merge
- Leave version tagging and release publication to the maintainer
