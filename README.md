# aura-tracker-gcp

A model-agnostic MCP (Model Context Protocol) server that exposes Google Cloud Platform infrastructure operations as structured tools callable by any LLM — Claude, Gemini, or any other model that supports MCP.

## Tools

| Tool | Description | Mutation | Dry-run |
|------|-------------|----------|---------|
| `gcp_gke_list_clusters` | List GKE clusters in a project/location | No | — |
| `gcp_gke_get_cluster_details` | Describe a cluster: node pools, endpoint, labels | No | — |
| `gcp_gke_get_cluster_bottlenecks` | Aggregate CPU/memory metrics + error logs → severity rating | No | — |
| `gcp_gke_scale_deployment` | Resize a GKE node pool | **Yes** | Yes |
| `gcp_cloudrun_list_services` | List Cloud Run services in a region | No | — |
| `gcp_cloudrun_get_service_details` | Describe a service: traffic, revision, labels | No | — |
| `gcp_cloudrun_update_traffic` | Update traffic split percentages | **Yes** | Yes |
| `gcp_pubsub_list_topics` | List Pub/Sub topics with subscription counts | No | — |
| `gcp_pubsub_inspect_topic_health` | Inspect topic for subscription lag and health issues | No | — |
| `gcp_logging_query_recent` | Fetch recent Cloud Logging entries by severity and resource | No | — |
| `gcp_monitoring_get_metrics` | Fetch Cloud Monitoring time-series metrics | No | — |
| `gcp_iam_test_permissions` | Test which IAM permissions the caller has on a project | No | — |

## Prerequisites

- Go 1.26+
- A GCP project with Application Default Credentials configured
- The service account must have appropriate IAM roles (use `gcp_iam_test_permissions` to verify)

```bash
gcloud auth application-default login
```

## Usage

```bash
# Build
go build -o aura-tracker-gcp ./cmd/aura-tracker-gcp

# Run (requires GCP_PROJECT_ID)
GCP_PROJECT_ID=my-project ./aura-tracker-gcp
```

The server reads JSON-RPC from stdin and writes responses to stdout. Logs go to stderr as structured JSON.

## Using with MCP Clients

### Claude Desktop

Add to `~/Library/Application Support/Claude/claude_desktop_config.json` (macOS) or `%APPDATA%\Claude\claude_desktop_config.json` (Windows):

```json
{
  "mcpServers": {
    "aura-tracker-gcp": {
      "command": "/path/to/aura-tracker-gcp",
      "env": {
        "GCP_PROJECT_ID": "my-project"
      }
    }
  }
}
```

Restart Claude Desktop. The tools will appear automatically in the tool list.

### Claude Code (CLI)

Add to your project's `.claude/settings.json` or global `~/.claude/settings.json`:

```json
{
  "mcpServers": {
    "aura-tracker-gcp": {
      "command": "/path/to/aura-tracker-gcp",
      "env": {
        "GCP_PROJECT_ID": "my-project"
      }
    }
  }
}
```

Or run inline from the repo:

```json
{
  "mcpServers": {
    "aura-tracker-gcp": {
      "command": "go",
      "args": ["run", "./cmd/aura-tracker-gcp"],
      "cwd": "/path/to/aura-tracker-gcp",
      "env": {
        "GCP_PROJECT_ID": "my-project"
      }
    }
  }
}
```

### Any MCP-compatible client

The server speaks JSON-RPC 2.0 over stdio — the transport used by every MCP client. Point any client at the binary and set `GCP_PROJECT_ID`.

### Example prompts

Once connected, you can ask the LLM things like:

> "List all GKE clusters in project my-project across all locations."

> "Are there any bottlenecks in my-cluster in us-central1? Look back 60 minutes."

> "What IAM permissions does the current service account have on project my-project?"

> "Scale the default-pool node pool in my-cluster to 5 nodes — dry run first."

> "Show me the last 50 ERROR logs from the my-service Cloud Run service."

## Environment Variables

| Variable | Required | Description |
|----------|----------|-------------|
| `GCP_PROJECT_ID` | Yes | Default GCP project used when initialising SDK clients |
| `GOOGLE_APPLICATION_CREDENTIALS` | No | Path to service account JSON key (optional if ADC is configured via `gcloud`) |

## Security Model

The server runs under a specific service account (Application Default Credentials) and implements least-privilege by design:

- **Permission-denied errors** are surfaced to the LLM as tool errors with clear remediation guidance, not server crashes
- **Rate limiting** is applied at the port boundary: 10 requests/second, burst 20 — configurable at startup
- **Mutation tools** (`gcp_gke_scale_deployment`, `gcp_cloudrun_update_traffic`) always support `dry_run: true`
- **Idempotency**: scaling to the current replica count returns `no_change_needed: true` without issuing an API call

## Architecture

The server uses **Hexagonal Architecture** (Ports and Adapters) to ensure the MCP protocol layer is completely decoupled from the Google Cloud SDK. Swap the GCP adapter for a mock or another cloud without touching a single tool handler.

```
┌─────────────────────────────────────────────────────────────────┐
│                    LLM (Claude / any model)                      │
│               calls tools via JSON-RPC over stdio                │
└─────────────────────────────┬───────────────────────────────────┘
                              │ mcp-go StdioServer
┌─────────────────────────────▼───────────────────────────────────┐
│              internal/mcp/   (MCP Protocol Layer)                │
│   server.go — tool registration                                  │
│   tools/  gke · cloudrun · pubsub · logging · monitoring · iam  │
└─────────────────────────────┬───────────────────────────────────┘
                              │ calls only ▼
┌─────────────────────────────▼───────────────────────────────────┐
│           ports/gcp_service.go   (Hexagon Boundary)              │
│                    GCPService interface                           │
└─────────────────────────────┬───────────────────────────────────┘
                              │ implements ▼
┌─────────────────────────────▼───────────────────────────────────┐
│              internal/gcp/   (GCP Adapter Layer)                 │
│   client.go — SDK factory, rate limiter (10 rps), 30s timeout    │
│   gke · gke_bottleneck · cloudrun · pubsub                       │
│   logging · monitoring · iam · errors                            │
└─────────────────────────────┬───────────────────────────────────┘
                              │ Google Cloud Go SDK (gRPC)
                          GCP APIs
```

**Dependency rule:** `internal/mcp` never imports `internal/gcp`. Both depend only on `ports/`. The model sees only tool names and JSON schemas.

## Development

```bash
# Build
go build ./...

# Test (always with race detector)
go test -race ./...

# Vet
go vet ./...

# Run against real GCP
GCP_PROJECT_ID=my-project go run ./cmd/aura-tracker-gcp

# Smoke-test tools/list via stdin
echo '{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}' \
  | GCP_PROJECT_ID=my-project go run ./cmd/aura-tracker-gcp
```

## Project Layout

```
aura-tracker-gcp/
├── cmd/aura-tracker-gcp/main.go   # entry point: wires adapter + server
├── internal/
│   ├── gcp/                       # GCP SDK adapter (secondary port)
│   │   ├── client.go              # gcpAdapter, New(), rate limiter, timeout
│   │   ├── errors.go              # PermissionDeniedError, NotFoundError
│   │   ├── gke.go                 # ListClusters, GetClusterDetails, ScaleDeployment
│   │   ├── gke_bottleneck.go      # GetClusterBottlenecks (errgroup fan-out)
│   │   ├── cloudrun.go            # Cloud Run adapter
│   │   ├── pubsub.go              # Pub/Sub adapter
│   │   ├── logging.go             # Cloud Logging adapter
│   │   ├── monitoring.go          # Cloud Monitoring adapter
│   │   ├── iam.go                 # IAM adapter
│   │   └── util.go                # isIteratorDone, isGRPCNotFound helpers
│   └── mcp/                       # MCP protocol layer (primary port)
│       ├── server.go              # tool registration
│       └── tools/                 # one file per GCP domain
├── pkg/models/                    # shared input/output structs (no GCP deps)
└── ports/gcp_service.go           # GCPService interface (hexagon boundary)
```
