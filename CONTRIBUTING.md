# Contributing to aura-tracker-gcp

Thank you for improving this project. This guide covers the dev loop, architectural rules, and PR expectations.

## Prerequisites

- The Go version declared in `go.mod` (currently Go 1.26.7 or newer; check with `go version`)
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

Follow the six-step checklist in [CLAUDE.md](CLAUDE.md#adding-a-new-tool):

1. Add request/response structs to `pkg/models/<domain>.go`
2. Add the method signature to `ports/gcp_service.go`
3. Implement the method on `gcpAdapter` in `internal/gcp/<domain>.go`
4. Create the tool definition + handler in `internal/mcp/tools/<domain>.go`
5. Register it in `GetTools()` on the domain's `*Tools` struct
6. If adding a new domain: update `internal/gcp/modules.go` and `internal/mcp/registry.go`

After adding a tool, update `README.md` (tool count, Tools table, architecture diagram, example prompts) in the same commit.

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
  → PermissionDeniedError / NotFoundError / RetriableError
  → handleServiceError("tool_name", err)  [internal/mcp/tools/errors.go]
  → mcp.NewToolResultError(json) or mcp.NewToolResultError(msg)
  → LLM receives IsError: true + structured ToolError JSON
```

Avoid returning raw GCP SDK errors to the MCP layer — the LLM cannot act on gRPC status strings.

## Mutation tools

Both mutation tools (`gcp_gke_scale_deployment`, `gcp_cloudrun_update_traffic`) use the two-step HITL safety pattern. Any new mutation tool must:

1. Support `dry_run: true` that returns a description without executing
2. Return `no_change_needed: true` when the resource is already at the requested state

## Submitting a PR

- One logical change per PR; keep diffs reviewable
- Squash commits before merging (GitHub "Squash and merge")
- All CI checks must be green: `test`, `lint`, `goreleaser-check`
- Update `README.md` if you changed tools, env vars, or architecture
