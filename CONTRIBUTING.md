# Contributing to aura-tracker-gcp

Thank you for improving this project. This guide covers the dev loop, architectural rules, and PR expectations.

## Governance and maintainer approval

Contributions are welcome through pull requests. Repository ownership remains with [@asbrodova](https://github.com/asbrodova):

- Contributors may propose changes, review code, and respond to feedback, but they must not merge pull requests.
- Only `@asbrodova`, the repository owner and code owner, decides when a pull request is approved and merges it into `main`.
- Passing CI is required for consideration, but a green pull request is not approval to merge.
- Only `@asbrodova` creates or pushes `v*` tags and publishes Aura Tracker GCP releases.
- Contributors must not create release tags, publish GitHub releases, or update release assets.

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

Use this client-neutral checklist. See the [architecture and contributing guide](https://github.com/asbrodova/aura-tracker-gcp/wiki/Architecture-and-Contributing) for the full design context.

1. Add request/response structs to `pkg/models/<domain>.go`
2. Add the method signature to `ports/gcp_service.go`
3. Implement the method on `gcpAdapter` in `internal/gcp/<domain>.go`
4. Create the tool definition + handler in `internal/mcp/tools/<domain>.go`
5. Register it in `GetTools()` on the domain's `*Tools` struct
6. Add focused tests for the adapter, handler, schema, routing, and error behavior that changed

When adding a new domain, also update every integration surface:

1. `internal/gcp/modules.go` for GCP client construction and module selection
2. `internal/mcp/registry.go` for server registration
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
- All automated checks must be green: Test, Lint, GoReleaser config check, Vulnerability and secret scan, and the configured CodeQL analyses
- Update `README.md` if you changed tools, env vars, or architecture
- Address review feedback and resolve conversations; do not merge the pull request yourself
- The maintainer performs the final squash merge after explicit approval
- Do not create or push a `v*` tag; release tags and releases are maintainer-only
