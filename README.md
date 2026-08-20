# aura-tracker-gcp

[![CI](https://github.com/asbrodova/aura-tracker-gcp/actions/workflows/ci.yaml/badge.svg)](https://github.com/asbrodova/aura-tracker-gcp/actions/workflows/ci.yaml)
[![Go Version](https://img.shields.io/github/go-mod/go-version/asbrodova/aura-tracker-gcp)](go.mod)
[![Go Report Card](https://goreportcard.com/badge/github.com/asbrodova/aura-tracker-gcp)](https://goreportcard.com/report/github.com/asbrodova/aura-tracker-gcp)
[![GoDoc](https://pkg.go.dev/badge/github.com/asbrodova/aura-tracker-gcp.svg)](https://pkg.go.dev/github.com/asbrodova/aura-tracker-gcp)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

<!-- Add your social preview image here: -->
<!-- ![aura-tracker-gcp banner](docs/banner.png) -->

**Give your LLM the full aura of your GCP infrastructure — and the confidence to act on it safely.**

`aura-tracker-gcp` is an open-source, model-agnostic [Model Context Protocol (MCP)](https://modelcontextprotocol.io) server written in Go. It connects Claude (or any MCP-compatible LLM) to live Google Cloud Platform state — error rates, memory pressure, IAM gaps, cross-service dependency graphs, cost anomalies, and composite health scores — all queryable in plain English, with a two-step preview/confirmation protocol before infrastructure changes when safety is enabled.

**73 Tools (+1 opt-in cost reasoning) · 10 Resources · 3 Prompts · 30 Modules**

> For architecture deep-dives, per-service guides, and full configuration reference, see the **[GitHub Wiki](https://github.com/asbrodova/aura-tracker-gcp/wiki)**.

<!-- Add a demo GIF here: -->
<!-- ![Demo: Claude calling gcp_project_aura_summary](docs/demo.gif) -->

**[→ Jump to Quick Start](#quick-start)**

---

## Why Aura Tracker GCP?

### Safety First, by Default

Cost-bearing integrations and sensitive-data features have explicit controls:

| Feature | Env var | Default |
|---|---|---|
| Cloud Recommender API integration | `RECOMMENDER_ENABLED` | **on** — disable with `=false` |
| Recommender BigQuery export tool | `RECOMMENDER_BQ_EXPORT_ENABLED` | off |
| Cost reasoning over detailed billing export | `COST_REASONING_ENABLED` | off |
| PII / credential scrubbing | `ANONYMIZE_ENABLED` | off |
| Two-step mutation confirmation | `SAFETY_ENABLED` | **on** — the one default that is on by design |

Cloud Recommender is on by default and can be disabled with `RECOMMENDER_ENABLED=false`; its API and IAM access still require explicit project setup. Cost reasoning is off by default because it runs chargeable BigQuery queries. Two base tools mutate GCP state (`gcp_gke_scale_deployment`, `gcp_cloudrun_update_traffic`); the optional BigQuery recommendation export is also a mutation. With safety enabled, all mutations require a preview and a second call containing the generated plan ID.

### Two-Step Confirmation for Mutations

No GCP resource can be modified without first generating a preview plan, then explicitly confirming it:

1. Call with `dry_run: true` → receive a `plan_id` and a before/after preview. The plan expires in **10 minutes** and is **single-use**.
2. Call again with `confirm_plan_id: <plan_id>` → the change executes using the exact parameters locked at dry-run time.

Calling a mutation tool without a plan returns a `confirmation required` error with step-by-step instructions. Every confirmed execution is audit-logged to Cloud Logging (`jsonPayload.msg="safety: mutation confirmed"`). The service enforces this two-call protocol at the port boundary when safety is enabled, but it does not independently prove that a distinct human approved the second call; mandatory human review depends on the MCP client or deployment policy.

> **Local development override:** Set `SAFETY_ENABLED=false` to skip the plan/confirm flow over stdio. The server refuses this override with the SSE transport.

### Token Efficiency: Load Only What You Need

All 73 default tools are registered when `--modules` is omitted. The cost tool is registered only when its feature flag and export configuration are present. Use `--modules` to load only the services relevant to your workflow—each excluded module also skips its GCP client connections at startup:

```json
"args": ["--modules=cloudrun,aura,monitoring,iam"]
```

| Workflow | `--modules` | Tools |
|---|---|---|
| Production incident diagnosis | `incident` | 1 |
| Project security audit | `security` | 1 |
| Environment drift detection | `drift` | 1 |
| Cloud Run + functions | `cloudrun,functions,eventarc,scheduler,aura,monitoring,iam` | 14 |
| GKE cluster health | `gke,aura,monitoring,logging` | 8 |
| Serverless event graph | `serverlessgraph,cloudrun,functions,eventarc,scheduler,workflows,tasks,pubsub,secretmanager,vpcaccess,cloudsql` | 20 |
| Data / logging | `logging,monitoring,iam` | 4 |
| Storage inspection | `storage` | 3 |
| Production architecture diagram | `archgraph` | 2 |
| Cost investigation | `cost` | 1 (requires `COST_REASONING_ENABLED=true`) |
| Full toolkit | *(omit flag)* | 73 (74 if cost reasoning is enabled) |

See the [full module list](#step-3--wire-it-into-claude-desktop) in Quick Start.

---

## Quick Start

### Step 1 — Install

**Homebrew (macOS / Linux) — recommended**

```bash
brew install asbrodova/tap/aura-tracker-gcp
```

> **macOS Gatekeeper:** On first run macOS may block the binary with a "cannot be opened" warning. Fix with either:
> - `xattr -d com.apple.quarantine $(which aura-tracker-gcp)` in your terminal, then restart Claude Desktop.
> - **System Settings → Privacy & Security** → scroll down to `aura-tracker-gcp` → click **Allow Anyway**, then restart Claude Desktop.

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

For automation, use an attached service account, Workload Identity, or service-account impersonation.
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

#### Optional: reduce context usage with `--modules`

By default all 73 base tools are registered. Use the `--modules` flag to load only the services you work with—each excluded module also skips its GCP client connection at startup. The opt-in cost tool is additional.

```json
{
  "mcpServers": {
    "aura-tracker-gcp": {
      "command": "aura-tracker-gcp",
      "args": ["--modules=cloudrun,aura,monitoring,iam"],
      "env": {
        "GCP_PROJECT_ID": "my-project"
      }
    }
  }
}
```

Available module names: `gke` · `cloudrun` · `functions` · `eventarc` · `scheduler` · `workflows` · `tasks` · `pubsub` · `secretmanager` · `vpcaccess` · `cloudsql` · `logging` · `monitoring` · `iam` · `security` · `topology` · `aura` · `storage` · `serverlessgraph` · `gke_workloads` · `gke_mesh` · `networking` · `datastores` · `supplychain` · `coverage` · `archgraph` · `tagging` · `incident` · `drift` · `cost`. The `cost` module also requires `COST_REASONING_ENABLED=true`. Use `--modules=none` to register zero tools. Resources remain available; module-specific prompts are registered only when their module is enabled. Omit the flag to keep the default all-tools behaviour.

| Workflow | Suggested `--modules` | Tools loaded |
|---|---|---|
| Production incident diagnosis | `incident` | 1 |
| Project security audit | `security` | 1 |
| Environment drift detection | `drift` | 1 |
| Cloud Run + functions | `cloudrun,functions,eventarc,scheduler,aura,monitoring,iam` | 14 |
| Serverless event graph | `serverlessgraph,cloudrun,functions,eventarc,scheduler,workflows,tasks,pubsub,secretmanager,vpcaccess,cloudsql` | 20 |
| GKE cluster health | `gke,aura,monitoring,logging` | 8 |
| Storage inspection | `storage` | 3 |
| Production architecture diagram | `archgraph` | 2 |
| Data / logging | `logging,monitoring,iam` | 4 |
| Cost investigation | `cost` | 1 (opt-in) |
| Full toolkit | *(omit flag)* | 73 (74 if cost reasoning is enabled) |

Restart Claude Desktop. Tools, Resources, and Prompts appear automatically. Now ask:

> "Are there any bottlenecks in my-cluster in us-central1? Look back 60 minutes."

### Compare environments for configuration drift

Configure at least two aliased environments, then use the read-only `drift` module:

```yaml
environments:
  - project_id: my-company-dev
    alias: dev
    default: true
  - project_id: my-company-prod
    alias: prod
```

`gcp_compare_environments` compares all supported components when `components` is omitted, or only the services named by the user. It treats both environments symmetrically—neither is a baseline. Every result uses the exact alias: for example, `missing_in: dev`, `present_in: prod`, or a field value for each environment. Collection coverage is reported separately, so an API error or partial scan is never mistaken for a clean match or a missing resource.

Supported components are BigQuery, Cloud Run, Cloud SQL, data stores, Eventarc, Cloud Functions, GKE clusters and workloads, IAM service accounts, Monitoring, networking, Pub/Sub, Scheduler, Secret Manager, Storage, supply chain, Cloud Tasks, VPC Access, and Workflows. Optional filters narrow the comparison by resource name, location, or Kubernetes namespace.

Try:

> "Find drifts between dev and prod."
>
> "Find diffs between prod Cloud Run and dev Cloud Run."
>
> "What is the difference between dev GKE setup and prod GKE setup?"
>
> "Compare networking and Pub/Sub in dev and prod; show detailed field differences."

---

## Getting Started by Service

Each snippet below is a minimal `mcpServers` config for that service. Paste it into your `claude_desktop_config.json` or `.claude/settings.json`, replace `my-project`, and restart.

### GKE

**Modules:** `gke` (cluster health) · add `gke_workloads` + `gke_mesh` for workload / mesh introspection  
**IAM role:** `roles/container.viewer` · add `roles/container.admin` for node pool scaling

```json
{
  "mcpServers": {
    "aura-tracker-gcp": {
      "command": "aura-tracker-gcp",
      "args": ["--modules=gke,aura,monitoring,logging"],
      "env": { "GCP_PROJECT_ID": "my-project" }
    }
  }
}
```

**Try it:**
> "List all GKE clusters in my-project across all locations."  
> "Are there any bottlenecks in my-cluster in us-central1? Look back 60 minutes."  
> "Give me a deep Aura Score for my-cluster — include per-node-pool autoscaling audit."  
> "Scale the default-pool node pool in my-cluster to 5 nodes — show me a plan first."

The last prompt triggers the two-step confirmation flow: the client calls `gcp_gke_scale_deployment` with `dry_run: true` first and receives a single-use plan. A client that requires human review can show that plan before sending the confirmation call.

Workload listing is paginated with `page_size` (default 500, maximum 1,000) and an opaque `next_page_token`. Workload details never serialize literal environment-variable values or their internal comparison fingerprints. Secret and ConfigMap references expose names only; arbitrary annotation values are withheld and counted in `annotations_omitted`.

---

### Cloud Run

**Modules:** `cloudrun` · add `incident` for correlated production diagnosis · add `functions`, `eventarc` for the full serverless tier
**IAM role:** `roles/run.viewer` · add `roles/run.admin` for traffic split mutations

```json
{
  "mcpServers": {
    "aura-tracker-gcp": {
      "command": "aura-tracker-gcp",
      "args": ["--modules=cloudrun,incident,functions,eventarc,aura,monitoring,iam"],
      "env": { "GCP_PROJECT_ID": "my-project" }
    }
  }
}
```

**Try it:**
> "List all Cloud Run services in my-project."  
> "What's the traffic split for the api-gateway service in us-central1?"  
> "Show me the last 50 ERROR logs from my-service."  
> "Production is failing. Diagnose the most likely cause and show me the evidence."
> "Update the api-gateway traffic split to send 10% to revision-2 — show me a plan first."

---

### Production Incident Diagnosis

**Module:** `incident`

**IAM roles:** `roles/run.viewer`, `roles/logging.viewer`, `roles/monitoring.viewer`, `roles/pubsub.viewer`, `roles/cloudsql.viewer`, `roles/vpcaccess.viewer`.

> [!IMPORTANT]
> **Platform-health correlation is optional and off by default.** To use it, the project must have billing enabled, the Service Health API must be enabled, and the runtime identity must have both `roles/logging.viewer` and `roles/servicehealth.viewer`. The tool reads Google's documented `servicehealth.googleapis.com/activity` log stream only when `include_platform_health=true`. Without these optional prerequisites, the rest of the incident diagnosis still runs and reports platform health as skipped or a coverage gap.

```json
{
  "mcpServers": {
    "aura-tracker-gcp": {
      "command": "aura-tracker-gcp",
      "args": ["--modules=incident"],
      "env": { "GCP_PROJECT_ID": "my-project" }
    }
  }
}
```

`gcp_incident_diagnose` is one bounded, read-only call. It discovers a production scope from `env`, `environment`, or `stage` labels when a service is omitted, then correlates the active window with a baseline across deployments, Cloud Run revisions and traffic, 5xx rate, p99 latency, error fingerprints, Admin Activity/IAM changes, dependency state, and optional Personalized Service Health events. Collector failures are reported as coverage gaps instead of discarding completed evidence.

The response contains ranked hypotheses with independent 0–100 evidence scores, supporting and contradicting evidence, a timeline, and suggested read-only investigations. Scores are confidence aids, not probabilities and do not need to sum to 100.

#### Enable optional platform-health correlation

Run the idempotent admin setup command. It works whether the `aura-tracker-mcp` service account is new or already exists, enables the Service Health API, and reconciles the optional viewer role:

```bash
export PROJECT_ID=my-project
SERVICE_HEALTH_ENABLED=true bash scripts/setup-iam.sh
```

`roles/logging.viewer` is already part of the core incident-diagnosis role set. If Aura Tracker runs with your user Application Default Credentials instead, grant `roles/servicehealth.viewer` to that user rather than to the service account.

The equivalent manual commands are:

```bash
SA_EMAIL="aura-tracker-mcp@${PROJECT_ID}.iam.gserviceaccount.com"

gcloud services enable servicehealth.googleapis.com \
  --project="${PROJECT_ID}"

gcloud projects add-iam-policy-binding "${PROJECT_ID}" \
  --member="serviceAccount:${SA_EMAIL}" \
  --role="roles/servicehealth.viewer" \
  --condition=None
```

Finally, opt in on the diagnosis call:

```json
{
  "project_id": "my-project",
  "include_platform_health": true
}
```

Verify that the API is enabled and that the identity can query the stream:

```bash
gcloud services list \
  --enabled \
  --project="${PROJECT_ID}" \
  --filter='config.name:servicehealth.googleapis.com'

gcloud logging read \
  "logName=\"projects/${PROJECT_ID}/logs/servicehealth.googleapis.com%2Factivity\"" \
  --project="${PROJECT_ID}" \
  --freshness=30d \
  --limit=5
```

An empty log query can simply mean there were no matching Service Health events. Google notes that new events can take a few hours to appear after API enablement. See Google's [Service Health logs](https://cloud.google.com/service-health/docs/logs) and [Service Health access control](https://cloud.google.com/service-health/docs/manage-access) documentation.

---

### Project Security Posture

**Module:** `security`
**Mutation:** none — the complete audit is read-only

`gcp_project_security_audit` turns broad requests such as “I need a project audit,” “is my GCP setup secure?”, or “give me a security score” into one deterministic assessment. It covers inherited organization/folder/project IAM and deny policies, service-account keys, Secret Manager, public serverless and GKE endpoints, effective classic/hierarchical/network firewall policies, KSA/GSA Workload Identity mappings, and Active Assist recommendations.

The response always contains Score, Critical, High, Medium, Low, prioritized recommendations, and coverage gaps. Missing permissions reduce coverage instead of being treated as a clean result.

```bash
PROJECT_ID=my-project SECURITY_AUDIT_ENABLED=true bash scripts/setup-iam.sh
```

**Try it:**
> "I need a project audit."
>
> "Is my GCP project secure? Show the score and highest-risk findings."
>
> "Check IAM, secrets, public services, service accounts, firewalls, and Workload Identity."

See the [Project Security Posture guide](https://github.com/asbrodova/aura-tracker-gcp/wiki/Security-Posture) for scoring, permissions, limitations, and the architecture diagram.

---

### Pub/Sub

**Module:** `pubsub`  
**IAM role:** `roles/pubsub.viewer`

```json
{
  "mcpServers": {
    "aura-tracker-gcp": {
      "command": "aura-tracker-gcp",
      "args": ["--modules=pubsub,monitoring,iam"],
      "env": { "GCP_PROJECT_ID": "my-project" }
    }
  }
}
```

**Try it:**
> "List all Pub/Sub topics in my-project with their subscription counts."  
> "Is there any subscription lag on the orders-topic?"  
> "Show me the dead-letter configuration for the payment-events subscription."

---

### BigQuery

> **BigQuery uses MCP Resources, not tools.** There is no `--modules=bigquery` flag. Resources (`gcp://…/bigquery/…`) are always available regardless of which modules are loaded.

**IAM role:** `roles/bigquery.metadataViewer`

```json
{
  "mcpServers": {
    "aura-tracker-gcp": {
      "command": "aura-tracker-gcp",
      "args": ["--modules=monitoring,iam"],
      "env": { "GCP_PROJECT_ID": "my-project" }
    }
  }
}
```

The LLM reads BigQuery state through three resource URIs before generating SQL or analysis:

1. `gcp://{project}/bigquery/datasets` — discover all datasets
2. `gcp://{project}/bigquery/{dataset}/tables` — list tables with row counts and sizes
3. `gcp://{project}/bigquery/{dataset}/{table}/schema` — full field definitions

This three-step read prevents hallucinated column names — the model knows actual column types before writing SQL.

**Try it:**
> "What BigQuery datasets exist in my-project?"  
> "Show me the schema for the orders table in the analytics dataset."  
> "What are the five largest tables in the events dataset by storage size?"  
> "Use the optimize-bigquery-costs prompt on my-project to identify partitioning opportunities."

---

### Cost Reasoning

**Module:** `cost` (opt-in)

**Required data:** Cloud Billing **Detailed usage cost** export in BigQuery

**Required IAM:** `roles/bigquery.jobUser` on the query project, `roles/bigquery.dataViewer` on the billing-export dataset, and `roles/cloudasset.viewer` plus `roles/serviceusage.serviceUsageConsumer` on the workload project. Add `roles/recommender.viewer` for idle-resource findings.

`gcp_cost_explain` answers more than “what did we spend?” It compares complete calendar-day windows and returns:

- current, baseline, delta, percentage change, daily history, and billing-export freshness;
- top spenders and top increases by service, SKU, and resource;
- ranked, reconciling drivers such as new resources, usage growth, traffic spikes, effective-rate changes, credit changes, and unattributed residuals;
- newly billed resources, upgraded to confirmed-new when Cloud Asset Inventory shows a matching creation time;
- active idle-resource recommendations with estimated monthly savings;
- traffic-like billing increases, with high confidence only when Cloud Monitoring confirms the same increase across the current and baseline windows;
- explicit coverage checks and warnings when optional evidence, resource attribution, export history, or freshness is incomplete.

Enable the [detailed billing export](https://cloud.google.com/billing/docs/how-to/export-data-bigquery-setup), then configure the MCP server:

```json
{
  "mcpServers": {
    "aura-tracker-gcp": {
      "command": "aura-tracker-gcp",
      "args": ["--modules=cost"],
      "env": {
        "GCP_PROJECT_ID": "workload-project",
        "COST_REASONING_ENABLED": "true",
        "COST_QUERY_PROJECT_ID": "finops-project",
        "BILLING_EXPORT_PROJECT_ID": "finops-project",
        "BILLING_EXPORT_DATASET": "cloud_billing",
        "COST_REASONING_TIMEZONE": "UTC",
        "COST_QUERY_MAX_BYTES": "5368709120"
      }
    }
  }
}
```

`BILLING_EXPORT_TABLE` is optional. When omitted, startup accepts the dataset and the first analysis discovers the sole table named `gcp_billing_export_resource_v1_*`; if multiple detailed exports exist, configure the table explicitly.

For the repository service account, the setup script enables the APIs and reconciles the project-level roles:

```bash
PROJECT_ID=workload-project \
COST_REASONING_ENABLED=true \
COST_QUERY_PROJECT_ID=finops-project \
BILLING_EXPORT_PROJECT_ID=finops-project \
BILLING_EXPORT_DATASET=cloud_billing \
RECOMMENDER_ENABLED=true \
bash scripts/setup-iam.sh
```

For tighter production access, grant `roles/bigquery.dataViewer` only on each billing dataset instead of its whole export project. The single Aura identity needs workload viewer permissions in every mapped environment, `roles/bigquery.jobUser` in every unique query project, and data-viewer access to every export dataset. The current setup script remains single-source; apply additional project and dataset grants manually for multi-environment routing.

The query path is bounded: every statement is dry-run first, rejected above `COST_QUERY_MAX_BYTES`, then executed with the same BigQuery maximum-bytes limit. BigQuery performs aggregation and ranking before rows reach the process; responses are cached for 15 minutes. The default limit is 5 GiB per statement, and one uncached analysis normally executes two statements.

The comparison uses regular net usage cost (`cost + credits`, excluding tax and adjustment rows) and excludes the incomplete current local day. Version 1 supports equal-length `previous_period` comparisons for the last 7 complete days, last 30 complete days, month-to-date, or a custom inclusive date range of at most 366 days. Currency conversion is not attempted; if the selected export contains multiple currencies, the response warns that its aggregated amounts are not directly comparable.

> [!NOTE]
> Detailed-export resource identifiers are service-dependent, GKE resource detail requires GKE cost allocation, and billing exports have no delivery-latency guarantee. “Newly billed” means first seen in the available export history; only a matching Asset Inventory creation time is labeled “confirmed new.” These limitations are surfaced in `coverage` and `warnings` rather than hidden.

**Try it:**
> "Why did GCP costs increase in the last 7 complete days? Show the top offenders and evidence."
> "Which resources appeared for the first time this month, and how much did they cost?"
> "Find idle resources and unexpected traffic behind this month's increase."
> "Compare the last 30 complete days with the previous 30 and explain usage versus rate effects."

---

### Firebase / Firestore

Firestore is available through the `datastores` module (Phase 2), which also covers Cloud Spanner, AlloyDB, and Memorystore for Redis. It lists Firestore databases in both **Native** and **Datastore** mode — it does not read collection or document data.

**Module:** `datastores`  
**IAM role:** `roles/datastore.viewer`

```json
{
  "mcpServers": {
    "aura-tracker-gcp": {
      "command": "aura-tracker-gcp",
      "args": ["--modules=datastores,iam"],
      "env": { "GCP_PROJECT_ID": "my-project" }
    }
  }
}
```

**Try it:**
> "List all Firestore databases in my-project and tell me which are in Native mode."  
> "What data stores does my-project use? Show me Spanner, AlloyDB, Firestore, and Redis."

---

### Cloud SQL

**Module:** `cloudsql`  
**IAM role:** `roles/cloudsql.viewer`  
Recommender signals are included by default. Successful results are cached for 12 hours. Valid cache hits remain usable during a quota block; uncached reads stop until the server-provided retry time, the minute window, or the next Pacific-midnight daily reset. Disable with `RECOMMENDER_ENABLED=false`.

```json
{
  "mcpServers": {
    "aura-tracker-gcp": {
      "command": "aura-tracker-gcp",
      "args": ["--modules=cloudsql,aura,monitoring,iam"],
      "env": {
        "GCP_PROJECT_ID": "my-project"
      }
    }
  }
}
```

**Try it:**
> "List all Cloud SQL instances in my-project with their machine tier and state."  
> "Give me an Aura Score for the main-db Cloud SQL instance."  
> "Show me the Aura Score summary for all resources in my-project — are any SQL instances idle?"

---

## Tools

73 base tools plus one opt-in cost-reasoning tool across 30 module flags. Load only what you need with `--modules`.

| Module | Flag | Tools | Mutation? |
|---|---|---|---|
| GKE | `gke` | 4 | node pool scale |
| GKE Workloads | `gke_workloads` | 5 | — |
| GKE Service Mesh | `gke_mesh` | 1 | — |
| Networking | `networking` | 7 | — |
| Cloud Run | `cloudrun` | 6 | traffic split |
| Cloud Functions | `functions` | 2 | — |
| Eventarc | `eventarc` | 2 | — |
| Cloud Scheduler | `scheduler` | 1 | — |
| Workflows | `workflows` | 2 | — |
| Cloud Tasks | `tasks` | 1 | — |
| Pub/Sub | `pubsub` | 3 | — |
| Secret Manager | `secretmanager` | 1 | — |
| Serverless VPC Access | `vpcaccess` | 1 | — |
| Cloud SQL | `cloudsql` | 1 | — |
| Cloud Logging | `logging` | 1 | — |
| Cloud Monitoring | `monitoring` | 8 | — |
| IAM | `iam` | 3 | — |
| Project Security Posture | `security` | 1 | — |
| Topology & Aura Score | `topology`, `aura` | 5 | — |
| Cloud Storage | `storage` | 3 | — |
| Data Stores | `datastores` | 4 | — |
| Supply Chain | `supplychain` | 4 | — |
| Serverless Graph | `serverlessgraph` | 1 | — |
| Architecture Graph & Diagrams | `archgraph` | 2 | — |
| Observability Coverage | `coverage` | 1 | — |
| Resource Tagging | `tagging` | 1 | — |
| Incident Diagnosis | `incident` | 1 | — |
| Environment Drift | `drift` | 1 | — |
| Cost Reasoning | `cost` | 1 (opt-in) | — |

→ Full tool reference with parameters and descriptions: [Module Reference](https://github.com/asbrodova/aura-tracker-gcp/wiki/Module-Reference)

---

## What You Can Ask

### Health & Incident Response
> "Production is failing. Diagnose likely root causes and show the evidence."
> "Are there any bottlenecks in my-cluster in us-central1? Look back 60 minutes."  
> "What's the Aura Score for all resources in my-project? Show me the worst first."  
> "Show me ERROR logs from the api-gateway Cloud Run service in the last hour."  
> "Which Cloud Run services have an error rate above 1% right now?"

### Infrastructure Exploration
> "Show my production architecture."
> "Show my production architecture as Graphviz."
> "Generate an SVG diagram of the whole project, grouped by region."
> "What does the payment-service depend on? Show me its full dependency graph."  
> "List all Pub/Sub topics with subscription lag above 10,000 unacked messages."  
> "What IAM permissions does my current service account have — and what's missing?"  
> "Export a full serverless event graph for my-project."

### Environment Drift

> "Find drifts between dev and prod."
>
> "Compare only Cloud Run and GKE between dev and prod."
>
> "Which resources are missing in dev, and which are missing in prod?"
>
> "Show detailed configuration differences for the api service in dev and prod."

### Automatic Architecture Diagrams

`gcp_generate_architecture_diagram` turns the live project graph into Mermaid (default), Graphviz DOT, or SVG. Production scope is inferred only from `env`, `environment`, or `stage` labels with `prod`/`production` values and exact `prod`/`production` GKE namespaces. Connected entrypoints and dependencies are included; explicitly labelled cross-environment resources are surfaced with a warning.

Mermaid and Graphviz DOT require no extra runtime. SVG uses the Graphviz `dot` executable: it is included in the official container, auto-discovered on `PATH` for local installs, or can be configured with `GRAPHVIZ_DOT_PATH`. If no production scope can be inferred, the tool returns `needs_scope` with discovered environment candidates instead of silently diagramming the entire project.

### Cost & Efficiency
> "Why did costs increase in the last 7 complete days? Rank the drivers and show the evidence."
> "Show top spenders, newly billed resources, idle resources, and unexpected traffic."
> "Compare this month to the previous equal period and separate usage growth from rate or credit changes."
> "Which Cloud SQL instances are idle or over-provisioned? Estimate monthly savings."  
> "Show me GCS buckets without lifecycle rules or public access prevention."  
> "Give me an Aura Score for the legacy-db Cloud SQL instance."

### Safe Mutations (two-step confirmation required)
> "Scale the default-pool node pool in my-cluster to 5 nodes — show me a plan first."  
> "Update the api-gateway traffic split to send 10% to revision-2 — dry run first."

---

## Resources

10 MCP Resources expose GCP state as browsable context the LLM reads **before** deciding which tool to call — preventing hallucinated BigQuery column names, IAM surprises, and wasted tool calls.

Covers BigQuery datasets / table schemas, Cloud Run service snapshots and revisions, Cloud Storage bucket metadata, and IAM permission splits. All URIs follow the `gcp://{project}/{service}/{resource-path}` pattern.

→ Full resource catalog, URI patterns, token budget, and schema discovery workflow: [MCP Resources Reference](https://github.com/asbrodova/aura-tracker-gcp/wiki/MCP-Resources-Reference)

---

## Prompts

Prompt Templates pre-configure the AI for complex multi-step GCP workflows. Invoke via `prompts/get` in any MCP client, or by asking Claude to "use the incident-response-helper prompt".

| Prompt | Arguments | What it does |
|--------|-----------|-------------|
| `audit-security-posture` | `project_id` or environment alias (optional; default environment), `focus` (iam\|network\|all) | Calls `gcp_project_security_audit` once and presents its score, severity-ranked evidence, remediation, and coverage gaps |
| `optimize-bigquery-costs` | `project_id` or environment alias (optional; default environment), `dataset_id` (optional) | Reads schemas + slot usage metrics; recommends partitioning, clustering, expiration, and slot rightsizing |
| `incident-response-helper` | `project_id` or environment alias (optional; default environment); `service_name`, `region`, `environment` (optional) | Calls `gcp_incident_diagnose` once and presents ranked causes, evidence, timeline, investigations, and coverage gaps without executing a mutation |

---

## Aura Score

The Aura Score is a composite 0–100 metric that tells an LLM not just that a resource *exists*, but whether it is **healthy** and **cost-effective** — two things that previously required separate queries to Cloud Monitoring, the Recommender API, and manual judgment to combine.

Two tools expose it:
- **`gcp_get_aura_score`** — scores a single named resource on demand (Cloud Run, Cloud SQL, BigQuery, GKE cluster, or GCS bucket)
- **`gcp_project_aura_summary`** — auto-discovers all Cloud Run services, Cloud SQL instances, BigQuery datasets, and GKE clusters in a project, scores each, and returns them sorted worst-first with a pre-formatted block the LLM can read at a glance
- **`gcp_gke_get_aura_score`** — deep GKE analysis: 5 health signals including version drift against the release channel, plus a per-node-pool autoscaling audit
- **`gcp_gcs_get_aura_score`** — security-posture and cost-efficiency score for a GCS bucket, including PAP enforcement, UBLA status, lifecycle rule presence, and storage class fit

### Example output

```
🔴 Cloud SQL: legacy-db     | Aura: 28  (Idle Resource (GCP Recommender))
🟡 Cloud Run: api-gateway   | Aura: 62  (Healthy, Over-provisioned)
🟢 Cloud Run: auth-service  | Aura: 91  (Healthy & Scaled)
```

Each resource also returns a `reasons` array the LLM uses to suggest concrete actions:

```json
{
  "display": "🔴 Cloud SQL: legacy-db | Aura: 28 (Idle Resource (GCP Recommender))",
  "score": 28,
  "health_score": 62,
  "efficiency_score": 20,
  "health_signals": [
    { "name": "cpu_util",    "value": 0.04, "score": 60, "label": "Warning" },
    { "name": "memory_util", "value": 0.12, "score": 60, "label": "Warning" },
    { "name": "disk_util",   "value": 0.31, "score": 100,"label": "OK"      },
    { "name": "recommender_idle", "value": 43.20, "score": 20, "label": "Warning" }
  ],
  "reasons": [
    "GCP Recommender: resource is idle — estimated $43.20/mo savings if deleted or stopped",
    "CPU at 4% — instance may be idle; consider a smaller machine type"
  ]
}
```

**Band mapping:** 🟢 80–100 Healthy · 🟡 50–79 Warning · 🔴 0–49 Critical

Scores are cached in-process for **5 minutes**. Recommender signals (idle/overprovisioned + estimated monthly USD savings) are **active by default** with a 12-hour result cache. Quota gates are maintained per recommender: cached signals remain available, uncached reads stop until the reported retry time, and requests resume automatically afterward. Set `RECOMMENDER_ENABLED=false` to disable.

→ Scoring formula, signal weights per resource type, GCP Recommender integration, and quota details: [Aura Score](https://github.com/asbrodova/aura-tracker-gcp/wiki/Aura-Score)

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

> "Scale the default-pool node pool in my-cluster to 5 nodes — show me a plan first, then I'll confirm."

> "Show me the last 50 ERROR logs from the my-service Cloud Run service."

> "What does my-api depend on? Show me its full dependency graph at depth 2."

> "Give me an Aura Score for the auth-service Cloud Run service in us-central1."

> "Show me the Aura Score summary for all resources in my-project — sorted by worst health first."

> "What BigQuery datasets exist in my-project? Then show me the schema for the orders table in the analytics dataset."

> "Check my IAM permissions and tell me which GCP tools you can and cannot use in this project."

> "I need a project audit. Show Critical, High, Medium, Low, recommendations, and the security score."

> "Use the incident-response-helper prompt: production is failing in my-project."

> "Run the audit-security-posture prompt on my-project and focus on network exposure."

---

## Prerequisites

- Go 1.26+ (or Docker / Homebrew — no Go required)
- A GCP project with [Application Default Credentials](https://cloud.google.com/docs/authentication/application-default-credentials) configured
- IAM roles for the modules you use — run the setup script below or check what you have with `gcp_iam_test_permissions`

The idempotent `scripts/setup-iam.sh` script creates or updates the `aura-tracker-mcp` service account with least-privilege roles:

```bash
PROJECT_ID=my-project bash scripts/setup-iam.sh

# Provision only a matching subset of tool modules (resources stay available)
PROJECT_ID=my-project MODULES=cloudrun,monitoring bash scripts/setup-iam.sh

# Also authorize a developer for keyless local service-account impersonation
PROJECT_ID=my-project LOCAL_IMPERSONATION_PRINCIPAL=user:developer@example.com \
  bash scripts/setup-iam.sh

# Reconcile mutation roles (node pool scale, traffic split)
PROJECT_ID=my-project MUTATION_ROLES=true bash scripts/setup-iam.sh

# Enable Recommender API and reconcile its viewer role
PROJECT_ID=my-project RECOMMENDER_ENABLED=true bash scripts/setup-iam.sh

# Enable the read-only project security posture collectors
PROJECT_ID=my-project SECURITY_AUDIT_ENABLED=true bash scripts/setup-iam.sh

# Enable Service Health API and reconcile its viewer role
PROJECT_ID=my-project SERVICE_HEALTH_ENABLED=true bash scripts/setup-iam.sh

# Enable cost reasoning (detailed billing export must already exist)
PROJECT_ID=my-project COST_REASONING_ENABLED=true \
  BILLING_EXPORT_DATASET=cloud_billing bash scripts/setup-iam.sh
```

Re-running the script is safe: it keeps an existing service account and reconciles the APIs and read-only roles required by `MODULES` (`all` by default, matching the server default; `none` keeps only the always-on resources). This includes Storage Bucket Viewer for bucket enumeration in addition to Storage Object Viewer. It always enables the IAM Service Account Credentials API so its printed impersonated-ADC command works. `LOCAL_IMPERSONATION_PRINCIPAL` optionally grants `roles/iam.serviceAccountTokenCreator` on this service account only; it accepts `user:`, `group:`, and `serviceAccount:` principals. Setup flags are additive—setting a flag to `false` or omitting it does not disable an API or revoke a previously granted role. Run it as a team admin with permission to change project IAM; API-enabling options also require permission to enable services.

→ Full per-module IAM role table: [Getting Started — IAM Setup](https://github.com/asbrodova/aura-tracker-gcp/wiki/Getting-Started#iam-setup)

## Environment Variables

| Variable | Required | Description |
|----------|----------|-------------|
| `GCP_PROJECT_ID` | Yes* | Legacy single-project configuration. Can be omitted when `project_id`, `environments`, or `GCP_ENVIRONMENTS_JSON` is configured. |
| `GCP_ENVIRONMENTS_JSON` | No | JSON array of `{project_id, alias, default}` entries for container/Cloud Run multi-environment deployments. Cannot be combined with `GCP_PROJECT_ID` or YAML project settings. |
| `GOOGLE_APPLICATION_CREDENTIALS` | No | Path to an external ADC credential file when keyless discovery is unavailable. Prefer attached service accounts, Workload Identity, or impersonation; never store credential files in the repository. |
| `ANONYMIZE_ENABLED` | No | Set `true` to enable PII/credential scrubbing on all tool outputs |
| `ANONYMIZE_CONFIG_PATH` | No | Path to a YAML config file for the anonymization engine (custom patterns, whitelist, audit mode) |
| `ANONYMIZE_PROJECT_ID` | No | Set `true` to mask an unaliased project ID as `[GCP_PROJECT_ID]`. Aliased projects are always returned as their aliases regardless of this setting. |
| `RECOMMENDER_ENABLED` | No | Set `false` to disable Cloud Recommender API integration. **On by default.** Aura Scores include idle/over-provisioned signals with estimated monthly USD savings. Successful results are cached for 12 h. Per-recommender quota gates preserve valid cache hits, block uncached reads until an RFC3339 `retry_at`, and resume automatically. Gate state is process-local. Requires the Recommender Viewer role. |
| `RECOMMENDER_BQ_EXPORT_ENABLED` | No | Set `true` to register the `gcp_export_recommendations_to_bq` tool. Off by default. |
| `RECOMMENDER_BQ_EXPORT_DATASET` | No | BigQuery dataset name used by `gcp_export_recommendations_to_bq`. Overrides `recommender_export.dataset` in `~/.aura-tracker.yaml`. |
| `COST_REASONING_ENABLED` | No | Set `true` to register `gcp_cost_explain`. Off by default; configure either `COST_REASONING_SOURCES_JSON` or the legacy billing-export settings. |
| `COST_REASONING_SOURCES_JSON` | No | Strict JSON array mapping environment aliases/project IDs to billing sources. Cannot be combined with YAML `sources` or legacy source fields/environment variables. |
| `COST_QUERY_PROJECT_ID` | No | Project that owns chargeable BigQuery query jobs. Defaults to `GCP_PROJECT_ID`. |
| `BILLING_EXPORT_PROJECT_ID` | No | Project containing the detailed Cloud Billing export dataset. Defaults to `COST_QUERY_PROJECT_ID`. |
| `BILLING_EXPORT_DATASET` | When cost reasoning is enabled | Dataset containing `gcp_billing_export_resource_v1_*`. |
| `BILLING_EXPORT_TABLE` | No | Exact detailed-export table ID. Omit when the dataset contains exactly one matching table. |
| `COST_REASONING_TIMEZONE` | No | IANA timezone used for complete calendar-day windows. Default: `UTC`. |
| `COST_REASONING_HISTORY_DAYS` | No | History queried for freshness, trends, and first-seen classification; `14`–`366`. Default: `90`. |
| `COST_QUERY_MAX_BYTES` | No | Positive per-statement BigQuery maximum bytes billed. Default: `5368709120` (5 GiB). |
| `SAFETY_ENABLED` | No | Set `false` to bypass the two-step confirmation protocol for local stdio development. Rejected when `MCP_TRANSPORT=sse`. Safety is on by default; the protocol itself does not prove a distinct human approved the second call. |
| `TRACE_BACKEND` | No | Backend for `gcp_trace_list_services`: `trace` (Cloud Trace v1 REST, default) or `monitoring` (Monitoring metric proxy). Use `monitoring` if Cloud Trace API is disabled but OpenCensus/OpenTelemetry metrics exist. |
| `GRAPH_TIMEOUT_SECONDS` | No | Outer context timeout in seconds for graph exports and diagram discovery. Default: `120`. Individual sub-calls still use `callTimeout` (30 s). |
| `GRAPHVIZ_DOT_PATH` | No | Path to Graphviz `dot` for SVG diagram generation. Auto-discovered on `PATH`; set to `/usr/bin/dot` in the official container. Mermaid and Graphviz DOT source do not require it. |
| `MCP_TRANSPORT` | No | Transport mode: `stdio` (default, for Claude Desktop) or `sse` (for Cloud Run / web-based MCP clients) |
| `PORT` | No | HTTP port when `MCP_TRANSPORT=sse`. Cloud Run sets this automatically. Defaults to `8080`. |
| `MCP_BASE_URL` | No | Public HTTPS URL of the SSE server, optionally including its preserved route prefix (e.g. `https://my-service-xyz.run.app/mcp`). Non-loopback URLs must use HTTPS. Defaults to `http://localhost:PORT` for local testing. |
| `MCP_AUTH_MODE` | No | SSE authentication mode: `required` (default) or `disabled`. Disabled mode is accepted only for a loopback base URL and binds the listener to loopback. |
| `MCP_AUTH_AUDIENCE` | No | Expected Google ID-token audience. Defaults to `MCP_BASE_URL` without a trailing slash. |
| `MCP_AUTH_ALLOWED_EMAILS` | No | Comma-separated allowlist of verified email claims. Required for a public SSE endpoint unless the broad-access override below is explicitly enabled. |
| `MCP_AUTH_ALLOW_ANY_VALID_TOKEN` | No | Set `true` to explicitly allow any valid Google identity token for the configured audience on a public SSE endpoint. Defaults to `false`. |

## User Config File

Configure one or more GCP environments in `~/.aura-tracker.yaml`. With multiple environments, every entry needs an alias and exactly one entry must be marked as the default:

```yaml
environments:
  - project_id: my-company-123
    alias: dev
    default: true
  - project_id: my-company-345
    alias: prod
```

Aliases are matched case-insensitively, so `dev`, `DEV`, and `DeV` select the same project. A configured project ID is also accepted as input. When an alias exists, all MCP-generated output—including tool results, errors, prompts, resources, URIs, and diagrams—uses the alias and never returns that project ID. If the user omits a selector, the default environment is used.

For example, both “check logs on PROD” and “check logs on my-company-345” query the `prod` environment, and the answer refers only to `prod`.

With two or more environments, requests such as “find drifts between dev and prod” invoke one symmetric comparison. The response names aliases directly (`missing in dev`, `missing in prod`) rather than referring to a baseline or target. Omit component names for a whole-environment scan, or name one or more components to narrow it.

A single project may omit both alias and default, in which case its project ID may be shown:

```yaml
project_id: my-gcp-project-id

cost_reasoning:
  enabled: false
  query_project_id: finops-project
  export_project_id: finops-project
  dataset: cloud_billing
  table: ""                    # auto-discover the sole detailed export
  timezone: UTC
  history_days: 90
  max_bytes_billed: 5368709120

# Optional accepted risks. Every suppression requires a reason and expiry.
security_audit:
  kubernetes_access: auto       # auto, direct, connect_gateway, or disabled
  fleet_project_id: platform-fleet
  cluster_concurrency: 4
  per_cluster_timeout_seconds: 20
  max_resources_per_kind: 2000
  suppressions:
    - rule_id: PUB-001
      resource: "//run.googleapis.com/projects/my-gcp-project-id/locations/*/services/public-api"
      reason: "Approved public API behind the external application load balancer"
      owner: "platform-security@example.com"
      expires_at: "2026-12-01T00:00:00Z"
```

The legacy `GCP_PROJECT_ID` environment variable takes precedence over the legacy YAML `project_id`. The new `environments` list cannot be combined with either legacy setting, so ambiguous configuration fails at startup. Unknown YAML fields and invalid anonymization modes or boolean environment values are rejected rather than silently ignored.

Cost reasoning can route each environment to a different detailed billing export. Every configured environment must appear exactly once; selectors may be aliases (case-insensitive) or configured project IDs:

```yaml
cost_reasoning:
  enabled: true
  timezone: Asia/Makassar
  history_days: 90
  max_bytes_billed: 5368709120
  sources:
    - environments: [dev]
      query_project_id: finops-dev
      export_project_id: billing-dev
      dataset: cloud_billing
    - environments: [prod]
      query_project_id: finops-prod
      export_project_id: billing-prod
      dataset: cloud_billing
```

One central export is expressed as a shared source with `environments: [dev, prod]`. The query still filters `project.id` using the selected workload project, so environments do not share results. If `sources` is omitted, the legacy scalar fields and environment variables remain supported and define one source shared by all configured environments. Aura never falls back to another environment's source when a mapping is absent.

For container deployments, `COST_REASONING_SOURCES_JSON` accepts the same source objects as a strict JSON array:

```json
[{"environments":["dev"],"query_project_id":"finops-dev","export_project_id":"billing-dev","dataset":"cloud_billing"},{"environments":["prod"],"query_project_id":"finops-prod","export_project_id":"billing-prod","dataset":"cloud_billing"}]
```

Container deployments can provide the same list as JSON:

```bash
GCP_ENVIRONMENTS_JSON='[{"project_id":"my-company-123","alias":"dev","default":true},{"project_id":"my-company-345","alias":"prod"}]' \
  aura-tracker-gcp
```

Security suppression resources support `*` and `?` wildcards. Matches are excluded from the active score only until expiry and remain visible in the audit report.

## IAM Setup

`scripts/setup-iam.sh` is an **idempotent team-admin setup**. It creates the `aura-tracker-mcp` service account when missing and reconciles core plus requested optional roles on every run. This makes it safe to enable an integration later without recreating the account.

```bash
# Create the account or reconcile its core roles (team admin only)
PROJECT_ID=my-project bash scripts/setup-iam.sh

# Reconcile the narrow role required for keyless local impersonated ADC
PROJECT_ID=my-project LOCAL_IMPERSONATION_PRINCIPAL=user:developer@example.com \
  bash scripts/setup-iam.sh

# Reconcile mutation roles (gcp_gke_scale_deployment, gcp_cloudrun_update_traffic)
PROJECT_ID=my-project MUTATION_ROLES=true bash scripts/setup-iam.sh

# Match a restricted --modules deployment; always-on resources are included
PROJECT_ID=my-project MODULES=cloudrun,monitoring bash scripts/setup-iam.sh

# Enable Recommender API and reconcile its viewer role
PROJECT_ID=my-project RECOMMENDER_ENABLED=true bash scripts/setup-iam.sh

# Enable the read-only project security posture collectors
PROJECT_ID=my-project SECURITY_AUDIT_ENABLED=true bash scripts/setup-iam.sh

# Enable Service Health API and reconcile its viewer role
PROJECT_ID=my-project SERVICE_HEALTH_ENABLED=true bash scripts/setup-iam.sh

# Enable cost reasoning and grant BigQuery/Asset Inventory access
PROJECT_ID=my-project COST_REASONING_ENABLED=true \
  COST_QUERY_PROJECT_ID=finops-project \
  BILLING_EXPORT_PROJECT_ID=finops-project \
  BILLING_EXPORT_DATASET=cloud_billing \
  bash scripts/setup-iam.sh
```

The optional flags can be combined in one invocation and also work when the service account already exists. `MODULES` accepts the same comma-separated module names as `--modules`; omitted/`all` provisions the default server surface, while `none` provisions only the four always-on resources. The script always enables `iamcredentials.googleapis.com`; when `LOCAL_IMPERSONATION_PRINCIPAL` is set, it grants that principal Service Account Token Creator on this service account only so `gcloud auth application-default login --impersonate-service-account=...` can succeed without a key file. The default `security` module receives its project-level read-only dependencies automatically. `SECURITY_AUDIT_ENABLED=true` additionally reconciles a distinct fleet project and optional organization IAM/deny readers; Kubernetes object collection also needs the RBAC template in `deploy/security-audit-rbac.yaml.tmpl`. `RECOMMENDER_ENABLED=true` forces Recommender setup even for a module subset, and `SERVICE_HEALTH_ENABLED=true` adds its API and viewer role; `MUTATION_ROLES=true` grants mutation roles. `COST_REASONING_ENABLED=true` enables BigQuery and Cloud Asset Inventory, grants BigQuery Job User on the query project, grants Cloud Asset Viewer on the workload project, and grants BigQuery Data Viewer on the export project. The script uses a project-wide Data Viewer binding for idempotent setup; prefer a dataset-level binding in tightly scoped production environments. These setup switches are additive: `false` does not disable APIs or revoke roles. Disable Recommender at runtime with the server's `RECOMMENDER_ENABLED=false`; platform-health collection is controlled per diagnosis with `include_platform_health=false`.

**For developers joining the team:** you do not need to run the admin setup. Ask an admin to set `LOCAL_IMPERSONATION_PRINCIPAL=user:YOUR_EMAIL` when running it, then use the printed impersonated-ADC command. Direct user ADC also works if your own account already has every required project role.

**Why not `roles/owner`?** Primitive roles grant write access to every GCP service in the project — a compromised MCP session could delete databases, modify firewall rules, or exfiltrate secrets. The read-only set grants only `list`/`get` on the specific services this server calls. Mutation roles (`roles/container.admin`, `roles/run.admin`) are opt-in and gated by the two-step confirmation protocol when safety is enabled.

**Audit granted roles:**

```bash
gcloud projects get-iam-policy PROJECT_ID \
  --flatten='bindings[].members' \
  --filter="bindings.members:aura-tracker-mcp@PROJECT_ID.iam.gserviceaccount.com" \
  --format='table(bindings.role)'
```

---

## Security Model

The server runs under a specific service account (Application Default Credentials) and implements least-privilege by design:

- **Permission-denied errors** are surfaced to the LLM as tool errors with clear remediation guidance, not server crashes
- **Rate limiting** is applied at the port boundary: 10 requests/second, burst 20 — configurable at startup
- **Two-step mutation confirmation**: when safety is enabled, mutations require a preview plan (`dry_run: true` → `plan_id`) followed by a confirmation call (`confirm_plan_id: <id>`). Plans expire after 10 minutes and are single-use, and confirmed execution is audit-logged. The server does not independently verify that the second call came from a distinct human; `SAFETY_ENABLED=false` also remains available for local stdio development.
- **Idempotency**: scaling to the current replica count returns `no_change_needed: true` without generating a plan or issuing an API call
- **Read-only guarantees**: all 70 default non-mutation tools—and the opt-in cost-reasoning tool—are strictly read-only and never modify resource state. The two base mutation tools (`gcp_gke_scale_deployment`, `gcp_cloudrun_update_traffic`) and the opt-in recommendation export use the two-step confirmation flow when safety is enabled.
- **Secret Manager safety**: `gcp_secretmanager_list` returns secret *metadata only* (name, labels, create time, replication). The `secretmanager.projects.secrets.versions.access` API is **never called** — secret values cannot be read or returned by any tool in this server. The required role (`roles/secretmanager.viewer`) does not grant `secretAccessor` permissions.
- **MCP annotations**: all registered tools (73 by default, 74 with cost reasoning) carry standard `readOnlyHint` / `destructiveHint` / `idempotentHint` annotations—clients like Claude Desktop use these to decide whether to present a confirmation UI before calling a tool
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

To persist the setting, add `"ANONYMIZE_ENABLED": "true"` to the `env` block in your MCP config.

Three modes are available: `local` (fast regex, no extra API calls), `dlp` (GCP Data Loss Prevention API, higher recall, billed), and `both` (local first, then DLP). An `audit_only` flag previews what would be masked without modifying output — useful for tuning patterns before enabling real scrubbing.

→ YAML config schema, built-in patterns, DLP mode, JSON key whitelist, and audit mode: [Configure Your Environment — PII Anonymization](https://github.com/asbrodova/aura-tracker-gcp/wiki/Configure-Your-Environment#pii-anonymization)

---

## Troubleshooting

### Cloud Monitoring: Empty Results or "No Data" for a Cloud Run Service

**Symptom:** `gcp_monitoring_get_metrics` returns an empty `points` array even though the service is running.

**Cause 1 — Wrong metric type name.**

Metric type strings must exactly match the Cloud Monitoring descriptor. Common mistakes:

| Wrong | Correct |
|---|---|
| `cloudrun.googleapis.com/request_count` | `run.googleapis.com/request_count` |
| `cloudrun.googleapis.com/container/cpu` | `run.googleapis.com/container/cpu/utilizations` |
| `container.googleapis.com/cpu` | `kubernetes.io/container/cpu/request_utilization` |

Use `gcp_monitoring_list_metric_descriptors` to discover exact type strings for any service:

> "List all metric descriptors in my-project with the prefix `run.googleapis.com`."

**Cause 2 — Region scoping.**

Cloud Run metrics require the resource label `location` to match the region where the service runs. Pass `resource_labels` explicitly to `gcp_monitoring_get_metrics`:

```json
{
  "metric_type": "run.googleapis.com/request_count",
  "resource_labels": {
    "service_name": "my-service",
    "location": "us-central1"
  }
}
```

**Cause 3 — Distribution metric without a percentile aligner.**

Some Cloud Run metrics are `DELTA DISTRIBUTION` types (`run.googleapis.com/request_latencies`, `run.googleapis.com/container/cpu/utilizations`). Calling `gcp_monitoring_get_metrics` on them directly may return:

```
The per-series aligner ALIGN_MEAN is not compatible with the value type DISTRIBUTION
```

Pass `per_series_aligner="ALIGN_PERCENTILE_95"` or `"ALIGN_PERCENTILE_99"`. The tool now preserves every labeled time series in `series`; `points` remains as a backward-compatible view of the first series. Use `cross_series_reducer` and `group_by_fields` only when you intentionally want to combine series.

---

### Cloud Logging: Zero Results for a Cloud Run Service

**Symptom:** `gcp_logging_query_recent` returns no entries for a Cloud Run service.

The tool accepts either a native Cloud Logging `filter` or structured `resource_type` / `resource_labels` selectors. For Cloud Run, use:

```json
{
  "resource_type": "cloud_run_revision",
  "resource_labels": {
    "service_name": "payments-api",
    "location": "us-central1"
  },
  "min_severity": "ERROR",
  "lookback_minutes": 60
}
```

Common monitored-resource types:

| Resource | Correct `resource_type` |
|---|---|
| Cloud Run service | `cloud_run_revision` |
| GKE cluster | `k8s_cluster` |
| GKE container | `k8s_container` |
| Cloud Function | `cloud_function` |

Using `cloud_run_service` (which looks correct) returns zero results — the actual monitored resource type is `cloud_run_revision`.

**Severity values must be uppercase.** The valid values are `DEBUG`, `INFO`, `NOTICE`, `WARNING`, `ERROR`, `CRITICAL`, `ALERT`, `EMERGENCY`. Lowercase values like `"error"` are silently treated as the zero value by the Logging API, returning all severities regardless of the filter.

**Short lookback windows.** If `lookback_minutes` is less than `15` for an infrequently accessed service, no logs may exist in the window. Start with `60` and narrow down.

---

### Permission Errors on Monitoring or Logging API Calls

**Symptom:** Tools return `PermissionDeniedError` on Monitoring or Logging calls.

Use `gcp_iam_test_permissions` to identify exactly which roles are missing before diagnosing further:

> "Test my IAM permissions on project my-project and tell me what GCP tools you can and cannot call."

Commonly missing roles:

| Missing role | Affected tools |
|---|---|
| `roles/monitoring.viewer` | `gcp_monitoring_get_metrics`, `gcp_monitoring_list_metric_descriptors`, all Aura Score tools |
| `roles/cloudtrace.user` | `gcp_trace_list_services`, `gcp_trace_list_dependency_edges` |
| `roles/logging.viewer` | `gcp_logging_query_recent` |
| `roles/vpcaccess.viewer` | Incident dependency checks for Serverless VPC Access connectors |
| `roles/servicehealth.viewer` | Optional Personalized Service Health correlation in `gcp_incident_diagnose` |
| `roles/recommender.viewer` | Aura Score efficiency signals (Recommender integration is on by default; set `RECOMMENDER_ENABLED=false` to disable) |

Run `scripts/setup-iam.sh` to provision all least-privilege roles automatically:

```bash
PROJECT_ID=my-project bash scripts/setup-iam.sh

# Enable the API and reconcile roles/servicehealth.viewer, including for an existing SA:
PROJECT_ID=my-project SERVICE_HEALTH_ENABLED=true bash scripts/setup-iam.sh
```

See [Enable optional platform-health correlation](#enable-optional-platform-health-correlation) for the complete setup and verification commands.

---

### Server Fails to Start: Missing GCP Credentials

```
aura-tracker-gcp: no GCP credentials found.

Run:  gcloud auth application-default login

For automation, use an attached service account, Workload Identity, or service-account impersonation.
```

For Cloud Run deployments (`MCP_TRANSPORT=sse`), attach a least-privilege runtime service account. For GKE-hosted deployments, use Workload Identity Federation for GKE. Avoid long-lived service-account keys.

---

## Architecture

The server uses **Hexagonal Architecture** (Ports and Adapters) to ensure the MCP protocol layer is completely decoupled from the Google Cloud SDK. Swap the GCP adapter for a mock or another cloud without touching a single tool handler.

```
┌─────────────────────────────────────────────────────────────────┐
│                    LLM (Claude / any model)                      │
│   reads Resources · invokes Prompts · calls Tools via stdio      │
└─────────────────────────────┬───────────────────────────────────┘
                              │ mcp-go StdioServer
┌─────────────────────────────▼───────────────────────────────────┐
│              internal/mcp/   (MCP Protocol Layer)                │
│   server.go — tool + resource + prompt registration              │
│   tools/     gke · cloudrun · functions · eventarc · scheduler    │
│              workflows · tasks · pubsub · secretmanager           │
│              vpcaccess · cloudsql · logging · monitoring          │
│              iam · topology · aura · storage · serverlessgraph    │
│              archgraph · incident · drift · cost                  │
│   resources/ bigquery · cloudrun · storage · iam                 │
│   prompts/   audit-security · optimize-bq · incident-response    │
└─────────────────────────────┬───────────────────────────────────┘
                              │ correlation / reasoning paths ▼
┌─────────────────────────────▼───────────────────────────────────┐
│ diagnostics · costreasoning · diagram · drift                     │
│ scopes · collectors · evidence scoring · deterministic renderers │
│ partial failures become explicit coverage gaps                  │
└─────────────────────────────┬───────────────────────────────────┘
                              │ narrow DataSource; other tools call ▼
┌─────────────────────────────▼───────────────────────────────────┐
│           ports/gcp_service.go   (Hexagon Boundary)              │
│                    composite GCPService interfaces                │
└─────────────────────────────┬───────────────────────────────────┘
                              │ implements ▼
┌─────────────────────────────▼───────────────────────────────────┐
│              internal/safety/   (Safety Decorator)               │
│   decorator.go — two-step confirmation for mutations             │
│   store.go     — TTL plan store, single-use UUID plan IDs        │
│   (read-only methods pass through unchanged)                     │
└─────────────────────────────┬───────────────────────────────────┘
                              │ wraps ▼
┌─────────────────────────────▼───────────────────────────────────┐
│              internal/gcp/   (GCP Adapter Layer)                 │
│   client.go — SDK factory, rate limiter (10 rps), 30s timeout    │
│   gke · gke_bottleneck · cloudrun · functions · eventarc          │
│   scheduler · workflows · tasks · secretmanager · vpcaccess       │
│   cloudsql · pubsub · logging · monitoring · iam                  │
│   topology · aura · bigquery · storage · serverlessgraph          │
│   cost_billing · cost_assets · cost_recommendations               │
│   regions (discoverRegions helper, 3-tier fallback + 10-min TTL) │
└─────────────────────────────┬───────────────────────────────────┘
                              │ Google Cloud Go SDK (gRPC / REST)
                          GCP APIs
```

**Dependency rule:** `internal/mcp` never imports `internal/gcp`. Ordinary tool modules depend on `ports/`; incident diagnosis, cost reasoning, diagrams, and environment drift delegate to deterministic application-layer engines whose narrow `DataSource` interfaces are satisfied by the same `GCPService`. `internal/safety` sits at the port boundary—it implements `GCPService` and wraps the real adapter, wired exclusively in `cmd/`. The model sees only tool names and JSON schemas.

Set `MCP_TRANSPORT=sse` to switch from stdio to HTTP/SSE for Cloud Run deployments. SSE requires a Google Accounts-issued ID token in `Authorization: Bearer <token>` by default; validation covers signature, issuer, expiry, audience, subject, and the optional verified-email allowlist. Public base URLs must use HTTPS. Session capabilities are bound to the token issuer and subject, so a refreshed token for the same identity can continue a session but a different identity cannot inject messages into it. The SDK keeps SSE sessions in process memory; run a single server instance unless the deployment provides session-affine routing to the same instance for the initial stream and every message. Bearer-authenticated SSE currently targets same-origin or non-browser MCP clients that can set the `Authorization` header; native cross-origin `EventSource` clients are not supported. The MCP protocol layer is otherwise identical in both modes.

→ Architectural decisions, hexagonal boundary rules, SSE deployment, and contributor guide: [Architecture & Contributing](https://github.com/asbrodova/aura-tracker-gcp/wiki/Architecture-and-Contributing)

## Development

```bash
make build        # compile the binary
make test         # run all tests with race detector
make lint         # golangci-lint (auto-installs on first run)
make smoke        # stdio round-trip — prints "OK — N tools registered"
make test-cover   # generate coverage.html
```

See `CONTRIBUTING.md` for the full contribution guide and architectural rules.

Raw commands (if you prefer not to use `make`):

```bash
go build ./...
go test -race ./...
go vet ./...

# Smoke-test tools/list via stdin
echo '{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}' \
  | GCP_PROJECT_ID=my-project go run ./cmd/aura-tracker-gcp
```

## Project Layout

The repository follows hexagonal architecture: `internal/mcp` (MCP protocol layer) and `internal/gcp` (GCP SDK adapter) remain decoupled. `internal/diagnostics` owns incident correlation; `internal/costreasoning` owns complete-day windows and deterministic cost attribution; `internal/diagram` owns environment scoping and deterministic Mermaid, Graphviz, and SVG rendering; `internal/drift` owns configuration collection, normalization, matching, comparison, and coverage reporting. None imports MCP or the GCP SDK. `internal/safety` wraps the adapter at the port boundary to enforce two-step mutation confirmation. `pkg/models` holds all input/output structs with zero GCP dependencies.

→ Full annotated project layout and contributor guide: [Architecture & Contributing](https://github.com/asbrodova/aura-tracker-gcp/wiki/Architecture-and-Contributing)
