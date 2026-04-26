# aura-tracker-gcp

<!-- Add your social preview image here: -->
<!-- ![aura-tracker-gcp banner](docs/banner.png) -->

**Talk to your GCP infrastructure in plain English.**

Manually checking GKE cluster health, IAM permissions, or Cloud Run traffic splits via the console or CLI is slow. `aura-tracker-gcp` is a [Model Context Protocol (MCP)](https://modelcontextprotocol.io) server that exposes 13 GCP operations as structured tools — so you can ask Claude (or any LLM) to do it for you, in natural language, with full dry-run safety for mutations.

<!-- Add a demo GIF or screenshot here showing Claude Desktop calling a tool: -->
<!-- ![Demo: Claude Desktop calling gcp_gke_get_cluster_bottlenecks](docs/demo.gif) -->

---

## Quick Start

### Step 1 — Install

**Homebrew (macOS / Linux) — recommended**

```bash
brew install asbrodova/tap/aura-tracker-gcp
```

**Direct binary download (all platforms)**

Download the archive for your platform from the [latest release](https://github.com/asbrodova/aura-tracker-gcp/releases/latest), extract, and place the binary on your `PATH`.

```bash
# macOS Apple Silicon example
curl -L https://github.com/asbrodova/aura-tracker-gcp/releases/latest/download/aura-tracker-gcp_darwin_arm64.tar.gz \
  | tar xz
sudo mv aura-tracker-gcp /usr/local/bin/
```

**Go toolchain**

```bash
go install github.com/asbrodova/aura-tracker-gcp/cmd/aura-tracker-gcp@latest
```

**Docker (Raspberry Pi, hosted environments, or anywhere with a container runtime)**

```bash
docker run --rm \
  -e GCP_PROJECT_ID=my-project \
  -v "$HOME/.config/gcloud/application_default_credentials.json:/creds.json:ro" \
  -e GOOGLE_APPLICATION_CREDENTIALS=/creds.json \
  ghcr.io/asbrodova/aura-tracker-gcp:latest
```

### Step 2 — Authenticate with GCP

```bash
gcloud auth application-default login
```

If credentials are missing the server prints a clear error on startup with the exact command to run:

```
aura-tracker-gcp: no GCP credentials found.

Run:  gcloud auth application-default login

Or set GOOGLE_APPLICATION_CREDENTIALS to a service account key file.
```

### Step 3 — Wire it into Claude Desktop

Add to `~/Library/Application Support/Claude/claude_desktop_config.json` (macOS) or `%APPDATA%\Claude\claude_desktop_config.json` (Windows):

```json
{
  "mcpServers": {
    "aura-tracker-gcp": {
      "command": "aura-tracker-gcp",
      "env": {
        "GCP_PROJECT_ID": "my-project"
      }
    }
  }
}
```

Restart Claude Desktop. The tools appear automatically. Now ask:

> "Are there any bottlenecks in my-cluster in us-central1? Look back 60 minutes."

---

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
| `gcp_get_service_topology` | Infer the dependency graph of a Cloud Run service: Cloud SQL, Pub/Sub, VPC, secrets, and more. Supports `depth=1` (direct deps) and `depth=2` (deps-of-deps) | No | — |

---

## Using with MCP Clients

### Claude Desktop

Add to `~/Library/Application Support/Claude/claude_desktop_config.json` (macOS) or `%APPDATA%\Claude\claude_desktop_config.json` (Windows):

```json
{
  "mcpServers": {
    "aura-tracker-gcp": {
      "command": "aura-tracker-gcp",
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
      "command": "aura-tracker-gcp",
      "env": {
        "GCP_PROJECT_ID": "my-project"
      }
    }
  }
}
```

Or run inline from the repo (no install needed):

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

> "List all GKE clusters in project my-project across all locations."

> "Are there any bottlenecks in my-cluster in us-central1? Look back 60 minutes."

> "What IAM permissions does the current service account have on project my-project?"

> "Scale the default-pool node pool in my-cluster to 5 nodes — dry run first."

> "Show me the last 50 ERROR logs from the my-service Cloud Run service."

> "What does my-api depend on? Show me its full dependency graph at depth 2."

---

## Prerequisites

- Go 1.26+
- A GCP project with Application Default Credentials configured
- The service account must have appropriate IAM roles (use `gcp_iam_test_permissions` to verify)

## Environment Variables

| Variable | Required | Description |
|----------|----------|-------------|
| `GCP_PROJECT_ID` | Yes | Default GCP project used when initialising SDK clients |
| `GOOGLE_APPLICATION_CREDENTIALS` | No | Path to service account JSON key (optional if ADC is configured via `gcloud`) |
| `ANONYMIZE_ENABLED` | No | Set `true` to enable PII/credential scrubbing on all tool outputs |
| `ANONYMIZE_CONFIG_PATH` | No | Path to a YAML config file for the anonymization engine (custom patterns, whitelist, audit mode) |

## Security Model

The server runs under a specific service account (Application Default Credentials) and implements least-privilege by design:

- **Permission-denied errors** are surfaced to the LLM as tool errors with clear remediation guidance, not server crashes
- **Rate limiting** is applied at the port boundary: 10 requests/second, burst 20 — configurable at startup
- **Mutation tools** (`gcp_gke_scale_deployment`, `gcp_cloudrun_update_traffic`) always support `dry_run: true`
- **Idempotency**: scaling to the current replica count returns `no_change_needed: true` without issuing an API call
- **PII anonymization** (opt-in): set `ANONYMIZE_ENABLED=true` to scrub IPs, emails, service account names, and GCP API keys from every tool result before the LLM sees it — see [PII Anonymization](#pii-anonymization) for full configuration options

---

## PII Anonymization

Tool outputs can contain IPs, emails, service account names, and API keys. The anonymization engine scrubs them before the LLM sees the response. It is **off by default** and adds zero overhead when disabled.

### Running with defaults

```bash
ANONYMIZE_ENABLED=true GCP_PROJECT_ID=my-project aura-tracker-gcp
```

**Defaults when enabled:**
- Mode: `local` — fast, regex-based, no extra GCP API calls
- Patterns: `internal_ip`, `public_ip`, `email`, `service_account`, `gcp_api_key`
- Masking on (not audit-only)
- No JSON key whitelist

Matched values are replaced with stable indexed tokens — the same raw value always gets the same token within one tool call, so the LLM can still correlate occurrences:

```
10.0.0.1      →  [INTERNAL_IP_1]
admin@corp.com →  [EMAIL_1]
10.0.0.1      →  [INTERNAL_IP_1]   ← same token, same value
```

To persist the setting in Claude Desktop / Claude Code, add to the `env` block in your MCP config:

```json
"env": {
  "GCP_PROJECT_ID": "my-project",
  "ANONYMIZE_ENABLED": "true"
}
```

### Configuring with a YAML file

```bash
ANONYMIZE_ENABLED=true ANONYMIZE_CONFIG_PATH=/path/to/anonymize.yaml \
  GCP_PROJECT_ID=my-project aura-tracker-gcp
```

> `ANONYMIZE_ENABLED=true` in the environment always overrides the `enabled` field in the file.

### Layer 1 — Local scrubber (default)

Regex patterns walk the JSON tree. No extra API calls, no latency.

```yaml
enabled: true
mode: local            # default

# JSON key names whose values are never masked (exact match)
json_key_whitelist:
  - cluster_name
  - region

# Append custom patterns after the built-ins
patterns:
  - name: ticket_id
    regex: 'TICKET-[0-9]+'
    replacement_template: '[TICKET]'   # fixed string; omit for indexed tokens like [TICKET_ID_1]
```

Built-in patterns (always active in local mode):

| Pattern name | Matches |
|---|---|
| `internal_ip` | RFC-1918 ranges: 10.x, 172.16–31.x, 192.168.x |
| `public_ip` | Any IPv4 address |
| `email` | Email addresses |
| `service_account` | `*@*.iam.gserviceaccount.com` |
| `gcp_api_key` | `AIza…` (35-char GCP API keys) |

### Layer 2 — GCP DLP (higher recall, billed)

Sends each tool result to the [GCP Data Loss Prevention API](https://cloud.google.com/dlp). Catches types the regex layer misses (phone numbers, credit cards, SSNs, etc.). Requires the DLP API to be enabled in your project.

```yaml
enabled: true
mode: dlp

dlp:
  project_id: my-billing-project   # defaults to GCP_PROJECT_ID
  info_types:                       # defaults: EMAIL_ADDRESS, IP_ADDRESS, PHONE_NUMBER,
    - EMAIL_ADDRESS                 #           CREDIT_CARD_NUMBER, US_SOCIAL_SECURITY_NUMBER
    - PHONE_NUMBER
    - CREDIT_CARD_NUMBER
```

### Layer 3 — Both (local first, then DLP)

Local runs first (no extra latency for what regex can catch), then DLP scrubs the already-clean output for anything that slipped through.

```yaml
enabled: true
mode: both

json_key_whitelist:
  - cluster_name

dlp:
  info_types:
    - PHONE_NUMBER
    - CREDIT_CARD_NUMBER
```

> **Note:** In `mode: both` with `audit_only: true`, the audit report reflects only what DLP finds on the already-locally-scrubbed content.

### Audit / dry-run mode

Set `audit_only: true` to see exactly what *would* be masked — no output is modified. Use this to tune patterns and the whitelist before enabling real scrubbing.

```yaml
enabled: true
mode: local    # or dlp / both
audit_only: true
```

Every tool result becomes an `AuditReport` JSON instead of the real output:

```json
{
  "total_matches": 3,
  "patterns_seen": ["email", "internal_ip"],
  "findings": [
    { "pattern_name": "email", "json_path": "pods[0].owner", "content_index": 0, "match_count": 1 },
    { "pattern_name": "internal_ip", "json_path": "endpoint", "content_index": 0, "match_count": 2 }
  ]
}
```

---

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
│   tools/  gke · cloudrun · pubsub · logging · monitoring · iam · topology  │
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
│   logging · monitoring · iam · topology · errors                 │
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
│   ├── anonymize/                 # PII/credential scrubbing middleware (opt-in)
│   │   ├── anonymize.go           # Anonymizer interface, AuditReport, buildAuditResult
│   │   ├── config.go              # Config struct, LoadConfig() (YAML + env-var)
│   │   ├── local.go               # LocalScrubber: regex patterns, JSON walker, token registry
│   │   ├── dlp.go                 # DLPAnonymizer: GCP DLP API backend + maskByOffsets
│   │   ├── chain.go               # ChainedAnonymizer: local → DLP pipeline (mode: both)
│   │   └── middleware.go          # WrapHandler() — wraps any tool handler
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
│   │   ├── topology.go            # GetServiceTopology (dependency graph inference)
│   │   ├── dlp.go                 # DLPAdapter: GCP DLP client, InspectText
│   │   └── util.go                # isIteratorDone, isGRPCNotFound helpers
│   └── mcp/                       # MCP protocol layer (primary port)
│       ├── server.go              # tool registration + anonymizer wiring
│       └── tools/                 # one file per GCP domain
├── pkg/models/                    # shared input/output structs (no GCP deps)
└── ports/
    ├── gcp_service.go             # GCPService interface (hexagon boundary)
    └── dlp_service.go             # DLPService interface (secondary hexagon port)
```
