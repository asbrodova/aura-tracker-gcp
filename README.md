# aura-tracker-gcp

[![CI](https://github.com/asbrodova/aura-tracker-gcp/actions/workflows/ci.yaml/badge.svg)](https://github.com/asbrodova/aura-tracker-gcp/actions/workflows/ci.yaml)

<!-- Add your social preview image here: -->
<!-- ![aura-tracker-gcp banner](docs/banner.png) -->

**Talk to your GCP infrastructure in plain English.**

Manually checking GKE cluster health, IAM permissions, or Cloud Run traffic splits via the console or CLI is slow. `aura-tracker-gcp` is a [Model Context Protocol (MCP)](https://modelcontextprotocol.io) server that exposes **65 Tools**, **10 Resources**, and **3 Prompts** — so you can ask Claude (or any LLM) to do it for you, in natural language, with built-in two-step HITL confirmation for every mutation.

The AI **browses** your GCP state via Resources first (BigQuery schemas, Cloud Run config, IAM permissions, GCS buckets), then **acts** via Tools — avoiding costly mistakes and hallucinated SQL from unknown column types.

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

#### Optional: reduce context usage with `--modules`

By default all 65 tools are registered. Use the `--modules` flag to load only the services you work with — each excluded module also skips its GCP client connection at startup.

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

Available module names: `gke` · `cloudrun` · `functions` · `eventarc` · `scheduler` · `workflows` · `tasks` · `pubsub` · `secretmanager` · `vpcaccess` · `cloudsql` · `logging` · `monitoring` · `iam` · `topology` · `aura` · `storage` · `serverlessgraph` · `gke_workloads` · `gke_mesh` · `networking` · `datastores` · `supplychain` · `coverage` · `archgraph`. Use `--modules=none` to register zero tools (resources and prompts are always available). Omit the flag to keep the default all-tools behaviour.

| Workflow | Suggested `--modules` | Tools loaded |
|---|---|---|
| Cloud Run + functions | `cloudrun,functions,eventarc,scheduler,aura,monitoring,iam` | 14 |
| Serverless event graph | `serverlessgraph,cloudrun,functions,eventarc,scheduler,workflows,tasks,pubsub,secretmanager,vpcaccess,cloudsql` | 20 |
| GKE cluster health | `gke,aura,monitoring,logging` | 8 |
| Storage inspection | `storage` | 2 |
| Data / logging | `logging,monitoring,iam` | 4 |
| Full toolkit | *(omit flag)* | 35 |

Restart Claude Desktop. Tools, Resources, and Prompts appear automatically. Now ask:

> "Are there any bottlenecks in my-cluster in us-central1? Look back 60 minutes."

---

## Tools

### GKE

| Tool | Description | Mutation |
|------|-------------|----------|
| `gcp_gke_list_clusters` | List GKE clusters in a project/location | No |
| `gcp_gke_get_cluster_details` | Describe a cluster: node pools, endpoint, labels | No |
| `gcp_gke_get_cluster_bottlenecks` | Aggregate CPU/memory metrics + error logs → severity rating | No |
| `gcp_gke_scale_deployment` | Resize a GKE node pool | **Yes**¹ |

### GKE Workloads

| Tool | Description | Mutation |
|------|-------------|----------|
| `gcp_gke_list_workloads` | List Deployments, StatefulSets, DaemonSets, CronJobs, and Jobs in a GKE cluster; includes image, replica count, SA, secret refs, and OTel detection | No |
| `gcp_gke_get_workload_details` | Full spec for a single workload: all containers, env vars (secret refs masked), resource limits, node selector, tolerations, annotations | No |
| `gcp_gke_list_services` | List Kubernetes Services; returns type, selector, ports, and NEG annotation linking to GCP Load Balancers | No |
| `gcp_gke_list_ingresses` | List Ingress resources and Gateway API HTTPRoutes; returns hosts, TLS status, routing rules, and linked GCP LB name | No |
| `gcp_gke_list_network_policies` | List NetworkPolicies; returns pod selector, ingress/egress rule counts, and policy types | No |

### GKE Service Mesh

| Tool | Description | Mutation |
|------|-------------|----------|
| `gcp_gke_get_mesh_topology` | Get service-to-service traffic edges from Anthos Service Mesh (Istio) telemetry. Primary: Cloud Monitoring `istio.io/service/server/request_count` metric. Falls back to Istio proxy access logs when ASM is not installed. Returns caller/callee workload names, namespace, and requests-per-minute. | No |

### Networking

| Tool | Description | Mutation |
|------|-------------|----------|
| `gcp_compute_list_loadbalancers` | List Compute Engine forwarding rules (global and regional LBs); returns IP, protocol, ports, load-balancing scheme, and target proxy name | No |
| `gcp_compute_list_url_maps` | List Compute Engine URL maps (global and regional); returns name, default backend service, and host rule count | No |
| `gcp_compute_list_negs` | List Network Endpoint Groups across all zones; includes type, size, and linked Cloud Run service for SERVERLESS NEGs | No |
| `gcp_apigateway_list` | List API Gateway instances; returns state, API config ID, and default hostname | No |
| `gcp_vpc_list_networks` | List VPC networks; returns auto-create-subnets mode, subnet count, peering count, and MTU | No |
| `gcp_vpc_list_subnets` | List VPC subnets across all regions; filterable by network and region; returns CIDR, Private Google Access, purpose | No |
| `gcp_psc_list_endpoints` | List Private Service Connect consumer endpoints (forwarding rules with purpose=PRIVATE_SERVICE_CONNECT) | No |

### Cloud Run

| Tool | Description | Mutation |
|------|-------------|----------|
| `gcp_cloudrun_list_services` | List Cloud Run services (excludes Gen 2 function wrappers) | No |
| `gcp_cloudrun_get_service_details` | Describe a service: traffic, revision, labels | No |
| `gcp_cloudrun_update_traffic` | Update traffic split percentages | **Yes**¹ |
| `gcp_cloudrun_list_jobs` | List Cloud Run Jobs in a project (`region="-"` fans out to all regions) | No |
| `gcp_cloudrun_get_job_details` | Describe a job: image, parallelism, SA, latest execution | No |
| `gcp_cloudrun_list_job_executions` | List recent executions of a Cloud Run Job | No |

### Cloud Functions

| Tool | Description | Mutation |
|------|-------------|----------|
| `gcp_functions_list` | List Cloud Functions Gen 1 and/or Gen 2 (`generation=both` by default) | No |
| `gcp_functions_get_details` | Full config for a function: entry point, memory, timeout, SA, VPC connector | No |

### Eventarc

| Tool | Description | Mutation |
|------|-------------|----------|
| `gcp_eventarc_list_triggers` | List Eventarc triggers with destination kind, SA, and transport topic | No |
| `gcp_eventarc_get_trigger` | Full trigger details including event filters | No |

### Cloud Scheduler

| Tool | Description | Mutation |
|------|-------------|----------|
| `gcp_scheduler_list_jobs` | List Scheduler jobs with target kind (HTTP / Pub/Sub) and URI | No |

### Workflows

| Tool | Description | Mutation |
|------|-------------|----------|
| `gcp_workflows_list` | List Cloud Workflows | No |
| `gcp_workflows_list_executions` | List recent executions of a workflow | No |

### Cloud Tasks

| Tool | Description | Mutation |
|------|-------------|----------|
| `gcp_tasks_list_queues` | List Cloud Tasks queues | No |

### Pub/Sub

| Tool | Description | Mutation |
|------|-------------|----------|
| `gcp_pubsub_list_topics` | List topics with subscription counts | No |
| `gcp_pubsub_inspect_topic_health` | Subscription lag, push endpoints, dead-letter topics | No |
| `gcp_pubsub_list_subscriptions` | List subscriptions with push endpoint and dead-letter details | No |

### Secret Manager

| Tool | Description | Mutation |
|------|-------------|----------|
| `gcp_secretmanager_list` | List secrets (metadata only — values are **never** read). Pass `include_references=true` to annotate each secret with the Cloud Run services that reference it. | No |

### Serverless VPC Access

| Tool | Description | Mutation |
|------|-------------|----------|
| `gcp_vpc_list_connectors` | List Serverless VPC Access connectors | No |

### Cloud SQL

| Tool | Description | Mutation |
|------|-------------|----------|
| `gcp_cloudsql_list_instances` | List Cloud SQL instances (uses SQL Admin API, not Monitoring) | No |

### Cloud Logging

| Tool | Description | Mutation |
|------|-------------|----------|
| `gcp_logging_query_recent` | Fetch recent Cloud Logging entries by severity and resource | No |

### Cloud Monitoring

| Tool | Description | Mutation |
|------|-------------|----------|
| `gcp_monitoring_get_metrics` | Fetch Cloud Monitoring time-series metrics | No |
| `gcp_monitoring_list_metric_descriptors` | List metric descriptors, optionally filtered by prefix | No |
| `gcp_trace_list_services` | List services that have sent traces to Cloud Trace (configurable backend) | No |
| `gcp_monitoring_list_alert_policies` | List Cloud Monitoring alert policies with enabled status, severity, and condition names | No |
| `gcp_monitoring_list_uptime_checks` | List Cloud Monitoring uptime check configurations with period, timeout, and checker type | No |
| `gcp_monitoring_list_slos` | List Service Level Objectives (SLOs) for monitoring services, with goal and calendar period | No |
| `gcp_monitoring_list_dashboards` | List Cloud Monitoring dashboards with display name and etag | No |
| `gcp_trace_list_dependency_edges` | Infer service-to-service dependency edges from Cloud Trace span parent/child relationships (up to 2000 traces, default 7-day lookback) | No |
| `gcp_observability_coverage` | Roll up observability signal coverage (metrics, traces, logs, alerts) for Cloud Run services; returns per-service scores and gap recommendations | No |
| `gcp_export_architecture_graph` | Export a full project-wide architecture graph (Phase 1 + Phase 2): GKE workloads, networking, data stores, supply chain, IAM, and observability. Infers service-to-service edges via K8s spec, IAM bindings, mesh telemetry, and Cloud Trace. Results cached 5 minutes. | No |

### IAM

| Tool | Description | Mutation |
|------|-------------|----------|
| `gcp_iam_test_permissions` | Test which IAM permissions the caller has on a project | No |
| `gcp_iam_get_resource_bindings` | Get IAM role→members bindings for a resource URN (storage_bucket, project) | No |
| `gcp_iam_list_service_accounts` | List IAM service accounts in a project with email, display name, and disabled status | No |

### Topology & Aura Score

| Tool | Description | Mutation |
|------|-------------|----------|
| `gcp_get_service_topology` | Infer Cloud Run service dependencies: Cloud SQL, Pub/Sub, VPC, secrets. Supports `depth=1` and `depth=2`. | No |
| `gcp_get_aura_score` | Composite 0–100 health + efficiency score for a single resource. Cached 5 min. | No |
| `gcp_project_aura_summary` | Auto-discover and score all Cloud Run, Cloud SQL, BigQuery, and GKE resources. Sorted worst-first with 🟢/🟡/🔴 display. | No |

### Cloud Storage

| Tool | Description | Mutation |
|------|-------------|----------|
| `gcp_storage_list_buckets` | List all GCS buckets with location, storage class, labels | No |
| `gcp_storage_get_bucket_metadata` | Versioning, lifecycle rules, uniform access, public access prevention | No |

### Data Stores

| Tool | Description | Mutation |
|------|-------------|----------|
| `gcp_spanner_list_instances` | List Cloud Spanner instances with state, node count, processing units, and edition | No |
| `gcp_alloydb_list_clusters` | List AlloyDB clusters with state, PostgreSQL version, and cluster type (primary/secondary) | No |
| `gcp_firestore_list_databases` | List Firestore databases with type (Native/Datastore), location, concurrency mode, and edition | No |
| `gcp_memorystore_list_instances` | List Memorystore for Redis instances with tier, memory size, Redis version, and host endpoint | No |

### Supply Chain

| Tool | Description | Mutation |
|------|-------------|----------|
| `gcp_artifactregistry_list_repos` | List Artifact Registry repositories; returns format (DOCKER, MAVEN, NPM, etc.), location, description, and size | No |
| `gcp_artifactregistry_list_images` | List Docker images in an Artifact Registry repository; returns URI, tags, build time, upload time, and size | No |
| `gcp_cloudbuild_list_triggers` | List Cloud Build triggers; returns event type (github, pubsub, webhook, manual), enabled status, tags, and build config filename | No |
| `gcp_servicedirectory_list` | List Service Directory namespaces and their registered services; omit location to scan all locations | No |

### Serverless Graph Export

| Tool | Description | Mutation |
|------|-------------|----------|
| `gcp_export_serverless_graph` | Export a full serverless/event-driven resource graph: nodes (35 kinds), typed edges (`triggers`, `routes_to`, `dead_letters_to`, `reads_secret`, …), and region/project groups. Optional `region` filter and `max_nodes` cap. | No |

> ¹ **Two-step confirmation (HITL):** mutation tools require an explicit approval before any change is made.
> 1. Call with `dry_run: true` → receive a `plan_id` and a before/after preview of the change (valid for 10 minutes).
> 2. Call again with `confirm_plan_id: <plan_id>` → the change executes using the exact parameters from step 1. The plan is single-use; each confirmation is audit-logged to Cloud Logging.
>
> Attempting a mutation without first obtaining a `plan_id` returns a `confirmation required` error with instructions. Set `SAFETY_ENABLED=false` to bypass this protocol (development only).

---

## Resources

Resources expose GCP state as browsable context the AI reads *before* deciding which tool to call. All URIs follow `gcp://{project}/{service}/{resource-path}`.

### Static resources (always listed)

| URI | Name | Description |
|-----|------|-------------|
| `gcp://{project}/bigquery/datasets` | BigQuery Datasets | All datasets — start here before writing SQL |
| `gcp://{project}/cloudrun/services` | Cloud Run Services | All services across all regions |
| `gcp://{project}/storage/buckets` | Cloud Storage Buckets | All GCS buckets |
| `gcp://{project}/iam/my-permissions` | My IAM Permissions | Granted vs denied permissions split — AI explains missing roles before attempting tool calls |

### Resource templates (resolved at read time)

| URI Template | Name | Description |
|---|---|---|
| `gcp://{project}/bigquery/{dataset}/tables` | BigQuery Tables | Table index with row counts and sizes |
| `gcp://{project}/bigquery/{dataset}/{table}/schema` | BigQuery Table Schema | Full field definitions — **read before writing SQL** |
| `gcp://{project}/cloudrun/{region}/{service}` | Cloud Run Service Snapshot | URL, traffic splits, latest revision, labels |
| `gcp://{project}/cloudrun/{region}/{service}/revisions` | Cloud Run Service Revisions | Latest revision and traffic allocation |
| `gcp://{project}/storage/{bucket}` | Cloud Storage Bucket Metadata | Versioning, lifecycle, uniform access, public access prevention |
| `gcp://{project}/storage/{bucket}/objects` | Cloud Storage Objects | Bucket config summary for object browsing context |

### Schema Discovery Workflow

When an AI is asked to query a BigQuery table, it should:
1. Read `gcp://{project}/bigquery/datasets` to discover dataset names
2. Read `gcp://{project}/bigquery/{dataset}/tables` to find the target table
3. Read `gcp://{project}/bigquery/{dataset}/{table}/schema` to learn column names and types
4. **Then** call `gcp_monitoring_get_metrics` or generate a SQL query — with correct column types

This prevents hallucinated column names and type errors that would otherwise require iterative correction.

### Token budget

Resources use three-tier lazy loading to stay within LLM context limits:

| Tier | Content | Approx. tokens |
|------|---------|---------------|
| Dataset list | ID + location + labels | ~2K |
| Table index | ID + type + row count + size GB | ~3K |
| Table schema | Field definitions (hard cap: 500 fields) | ~5K |

---

## Prompts

Prompt Templates pre-configure the AI for complex multi-step GCP workflows. Invoke via `prompts/get` in any MCP client, or by asking Claude to "use the incident-response-helper prompt".

| Prompt | Arguments | What it does |
|--------|-----------|-------------|
| `audit-security-posture` | `project_id` (required), `focus` (iam\|network\|all) | Reads IAM permissions + Cloud Run ingress config; returns severity-ranked findings with gcloud remediation commands |
| `optimize-bigquery-costs` | `project_id` (required), `dataset_id` (optional) | Reads schemas + slot usage metrics; recommends partitioning, clustering, expiration, and slot rightsizing |
| `incident-response-helper` | `project_id`, `service_name`, `region` (all required) | Pulls error logs + request metrics + Aura score; synthesises a structured incident report with rollback commands |

---

## Aura Score

The Aura Score is a composite 0–100 metric that tells an LLM not just that a resource *exists*, but whether it is **healthy** and **cost-effective** — two things that previously required separate queries to Cloud Monitoring, the Recommender API, and manual judgment to combine.

Two tools expose it:
- **`gcp_get_aura_score`** — scores a single named resource on demand (Cloud Run, Cloud SQL, BigQuery, or GKE cluster)
- **`gcp_project_aura_summary`** — auto-discovers all Cloud Run services, Cloud SQL instances, BigQuery datasets, and GKE clusters in a project, scores each, and returns them sorted worst-first with a pre-formatted block the LLM can read at a glance

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

### How the score is calculated

The score combines two independent sub-scores with a fixed weighting:

```
Final score = (health_score × 0.6) + (efficiency_score × 0.4)
```

**Health score** is derived from Cloud Monitoring golden signals — the same API already used by `gcp_monitoring_get_metrics`. Signals and weights vary by resource type:

| Resource | Signal | Weight | Thresholds (score) |
|----------|--------|--------|--------------------|
| Cloud Run | Error rate (5xx / total) | 50% | 0%→100, <1%→85, <3%→60, <5%→30, ≥5%→0 |
| Cloud Run | CPU utilization | 30% | <5%→55⚠, 5–50%→100, 50–80%→80, 80–90%→50, ≥90%→20 |
| Cloud Run | Latency p99 | 20% | <200ms→100, <500ms→80, <1s→60, <3s→30, ≥3s→0 |
| Cloud SQL | CPU utilization | 40% | <10%→60⚠, 10–70%→100, 70–85%→75, 85–95%→40, ≥95%→0 |
| Cloud SQL | Memory utilization | 30% | same thresholds as CPU |
| Cloud SQL | Disk utilization | 30% | same thresholds as CPU |
| BigQuery | Job failure rate | 60% | 0%→100, <2%→80, <5%→55, <10%→30, ≥10%→0 |
| BigQuery | Slot utilization | 20% | <10 slots→60⚠, 10–500→100, ≥500→80 |
| BigQuery | Billable storage | 20% | <10 GB→100, <100 GB→85, <1 TB→65, ≥1 TB→40 |
| GKE | Node CPU allocatable utilization | 35% | <10%→50⚠, 10–75%→100, 75–90%→70, 90–95%→40, ≥95%→10 |
| GKE | Node memory allocatable utilization | 35% | same thresholds as node CPU |
| GKE | Container restart rate (per second) | 20% | 0→100, <0.005→70, ≥0.005→20 |
| GKE | Control plane health (GKE API) | 10% | RUNNING→100, RECONCILING→60, ERROR/DEGRADED→0 |

⚠ A low value here indicates under-utilization (idle / over-provisioned), which also penalises the efficiency score.

**Efficiency score** starts from the same metrics — e.g. Cloud Run CPU below 5% while receiving traffic flags over-provisioning — and is then overridden by the Cloud Recommender API when `RECOMMENDER_ENABLED=true` (see below).

**Band mapping:**

| Score | Band | Indicator |
|-------|------|-----------|
| 80–100 | Healthy | 🟢 |
| 50–79 | Warning | 🟡 |
| 0–49 | Critical | 🔴 |

Scores are cached in-process for **5 minutes**. Repeated LLM calls within the cache window return in under 1 ms with no GCP API calls.

---

## Cloud Recommender Integration

> **Opt-in.** Set `RECOMMENDER_ENABLED=true` to enable. Off by default.

Without the Recommender, efficiency is estimated from Cloud Monitoring metrics alone — for example, a Cloud Run service with CPU below 5% is flagged as over-provisioned. This heuristic works, but metrics can't tell you that GCP itself has already analyzed the resource and concluded it should be deleted or right-sized.

With `RECOMMENDER_ENABLED=true`, each Aura Score fetch also queries the [Cloud Recommender API](https://cloud.google.com/recommender) for pre-computed, GCP-authoritative recommendations:

| Recommender | Target | What it detects |
|-------------|--------|-----------------|
| `google.run.service.IdentifyIdleService` | Cloud Run | Services receiving zero traffic for an extended period |
| `google.cloudsql.instance.IdleRecommender` | Cloud SQL | Instances with no connections or queries |
| `google.cloudsql.instance.OverprovisionedInstanceRecommender` | Cloud SQL | Instances with CPU/memory headroom that warrant a smaller tier |

### What changes in the score

When an active Recommender recommendation is found for a resource, the efficiency score is overridden — it no longer relies on the metric heuristic:

| Recommendation type | Efficiency score override |
|---------------------|--------------------------|
| Idle | Capped at **20** (regardless of metrics) |
| Over-provisioned | Capped at **45** (regardless of metrics) |

The display label and reasons are also updated to surface the GCP-authoritative finding first:

```
Without Recommender:  🟡 Cloud SQL: main-db | Aura: 62  (Idle, Consider Downsize)
With Recommender:     🔴 Cloud SQL: main-db | Aura: 28  (Idle Resource (GCP Recommender))
```

The `reasons` field includes the estimated monthly USD savings from GCP's cost projection, giving the LLM a concrete number to include in its recommendation:

```
"GCP Recommender: resource is idle — estimated $43.20/mo savings if deleted or stopped"
```

If the Recommender API is unavailable or returns a permission error, the Aura Score falls back silently to the metric-based efficiency — no error is surfaced, and the score is still computed from Cloud Monitoring data.

### Enabling

```bash
RECOMMENDER_ENABLED=true GCP_PROJECT_ID=my-project aura-tracker-gcp
```

Or in your MCP client config:

```json
"env": {
  "GCP_PROJECT_ID": "my-project",
  "RECOMMENDER_ENABLED": "true"
}
```

### Permissions

Add these to your service account in addition to the base Aura Score permissions:

| Permission | IAM Role |
|-----------|----------|
| `recommender.cloudsqlInstanceIdleRecommendations.list` | Recommender Viewer |
| `recommender.cloudsqlInstanceOverprovisionedRecommendations.list` | Recommender Viewer |
| `recommender.runServiceIdleRecommendations.list` | Recommender Viewer |

The **Recommender Viewer** role (`roles/recommender.viewer`) grants all three at once.

### Pricing

The Cloud Recommender API is **free** for all three recommenders used here. Google Cloud generates recommendations at no charge; the only paid recommender is Firewall Insights, which is not used by this tool.

The practical constraint is the default **API quota**:

| Support plan | Reads/day |
|---|---|
| None / Basic | 100 |
| Standard / Enhanced / Premium | 1,000,000 |

On the default 100 reads/day quota, each `gcp_get_aura_score` call for Cloud Run consumes 1 read, and each Cloud SQL call consumes 2 (idle + over-provisioned). The 5-minute in-process cache means a cache miss per resource is the worst case. If you run `gcp_project_aura_summary` frequently against many resources on a basic support plan, you may hit this limit — in which case the Aura Score degrades gracefully to metric-only efficiency with no error.

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

> "Use the incident-response-helper prompt for the api-gateway service in us-central1."

> "Run the audit-security-posture prompt on my-project and focus on network exposure."

---

## Prerequisites

- Go 1.26+
- A GCP project with Application Default Credentials configured
- The service account must have appropriate IAM roles (use `gcp_iam_test_permissions` to verify)

**Permissions required for Aura Score tools:**

| Permission | IAM Role | Used by |
|-----------|----------|---------|
| `monitoring.timeSeries.list` | Monitoring Viewer | all aura tools (already required by `gcp_monitoring_get_metrics`) |
| `run.services.list` | Cloud Run Viewer | `gcp_project_aura_summary` (already required) |
| `cloudsql.instances.list` | Cloud SQL Viewer | `gcp_project_aura_summary` Cloud SQL discovery |
| `bigquery.datasets.list` | BigQuery Data Viewer | `gcp_project_aura_summary` BigQuery discovery |

**Permissions required for Phase 1 serverless/event-driven tools:**

| IAM Role | Module | What it grants |
|---------|--------|----------------|
| `roles/run.viewer` | `cloudrun` | Cloud Run services and jobs (list/get) |
| `roles/cloudfunctions.viewer` | `functions` | Cloud Functions Gen 1 list/get |
| `roles/eventarc.viewer` | `eventarc` | Eventarc trigger list/get (includes filters, destination, transport) |
| `roles/cloudscheduler.viewer` | `scheduler` | Scheduler job list (includes target type and URI) |
| `roles/workflows.viewer` | `workflows` | Workflow list and execution history |
| `roles/cloudtasks.viewer` | `tasks` | Cloud Tasks queue list |
| `roles/secretmanager.viewer` | `secretmanager` | Secret **metadata** list only — names, labels, create/update times. Does **not** grant secret value access. `roles/secretmanager.secretAccessor` is not required and not requested. |
| `roles/vpcaccess.viewer` | `vpcaccess` | Serverless VPC Access connector list |
| `roles/cloudsql.viewer` | `cloudsql` | SQL Admin API instance list |
| `roles/monitoring.viewer` | `monitoring` | Metric descriptors and time-series list |
| `roles/cloudtrace.user` | `monitoring` | Cloud Trace service enumeration (for `gcp_trace_list_services`) |
| `roles/pubsub.viewer` | `pubsub` | Topic and subscription list/get |

**Permissions required for Resources:**

| Permission | IAM Role | Used by |
|-----------|----------|---------|
| `bigquery.datasets.get` | BigQuery Metadata Viewer | `gcp://.../bigquery/datasets` |
| `bigquery.tables.list` | BigQuery Metadata Viewer | `gcp://.../bigquery/{dataset}/tables` |
| `bigquery.tables.get` | BigQuery Metadata Viewer | `gcp://.../bigquery/{dataset}/{table}/schema` |
| `storage.buckets.list` | Storage Object Viewer | `gcp://.../storage/buckets` |
| `storage.buckets.get` | Storage Object Viewer | `gcp://.../storage/{bucket}` |
| `resourcemanager.projects.testIamPermissions` | (included in most roles) | `gcp://.../iam/my-permissions` |

## Environment Variables

| Variable | Required | Description |
|----------|----------|-------------|
| `GCP_PROJECT_ID` | Yes | Default GCP project used when initialising SDK clients |
| `GOOGLE_APPLICATION_CREDENTIALS` | No | Path to service account JSON key (optional if ADC is configured via `gcloud`) |
| `ANONYMIZE_ENABLED` | No | Set `true` to enable PII/credential scrubbing on all tool outputs |
| `ANONYMIZE_CONFIG_PATH` | No | Path to a YAML config file for the anonymization engine (custom patterns, whitelist, audit mode) |
| `RECOMMENDER_ENABLED` | No | Set `true` to enable the Cloud Recommender API integration. When enabled, Aura Scores include idle/over-provisioned flags with estimated monthly savings from GCP's pre-computed recommendations. Requires the Recommender Viewer role. Off by default. |
| `SAFETY_ENABLED` | No | Set `false` to bypass the two-step HITL confirmation protocol for mutation tools. **Development/testing only** — safety is on by default. |
| `TRACE_BACKEND` | No | Backend for `gcp_trace_list_services`: `trace` (Cloud Trace v1 REST, default) or `monitoring` (Monitoring metric proxy). Use `monitoring` if Cloud Trace API is disabled but OpenCensus/OpenTelemetry metrics exist. |
| `GRAPH_TIMEOUT_SECONDS` | No | Outer context timeout in seconds for `gcp_export_serverless_graph`. Default: `120`. Individual sub-calls still use `callTimeout` (30 s). |
| `MCP_TRANSPORT` | No | Transport mode: `stdio` (default, for Claude Desktop) or `sse` (for Cloud Run / web-based MCP clients) |
| `PORT` | No | HTTP port when `MCP_TRANSPORT=sse`. Cloud Run sets this automatically. Defaults to `8080`. |
| `MCP_BASE_URL` | No | Public HTTPS URL of the SSE server (e.g. `https://my-service-xyz.run.app`). Required for correct SSE endpoint URLs when deployed to Cloud Run. Defaults to `http://localhost:PORT` for local testing. |

## IAM Setup

`scripts/setup-iam.sh` is a **one-time team-admin setup**. Run it once per GCP project to create the `aura-tracker-mcp` service account with least-privilege roles. If the SA already exists, the script prints onboarding instructions for other team members and exits without making any changes.

```bash
# First-time setup (team admin only)
PROJECT_ID=my-project bash scripts/setup-iam.sh

# With mutation tools (gcp_gke_scale_deployment, gcp_cloudrun_update_traffic)
PROJECT_ID=my-project MUTATION_ROLES=true bash scripts/setup-iam.sh

# With Recommender API
PROJECT_ID=my-project RECOMMENDER_ENABLED=true bash scripts/setup-iam.sh
```

**For developers joining the team:** the SA already exists. Either ask your admin for a key file (`GOOGLE_APPLICATION_CREDENTIALS=/path/to/sa-key.json`) or use `gcloud auth application-default login` with your own account if it already has the required roles.

**Why not `roles/owner`?** Primitive roles grant write access to every GCP service in the project — a compromised MCP session could delete databases, modify firewall rules, or exfiltrate secrets. The read-only set grants only `list`/`get` on the specific services this server calls. Mutation roles (`roles/container.admin`, `roles/run.admin`) are opt-in and always gated by the two-step HITL confirmation protocol.

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
- **Two-step HITL confirmation** for mutation tools: no GCP resource can be changed without first generating a preview plan (`dry_run: true` → `plan_id`) and then confirming it (`confirm_plan_id: <id>`). Plans expire after 10 minutes and are single-use — replay attacks are prevented at the store level. Every confirmed execution is audit-logged to Cloud Logging (`jsonPayload.msg="safety: mutation confirmed"`)
- **Idempotency**: scaling to the current replica count returns `no_change_needed: true` without generating a plan or issuing an API call
- **Read-only guarantees**: all 33 non-mutation tools are strictly read-only — they call only `list`/`get` GCP API methods and never modify any resource state. The two mutation tools (`gcp_gke_scale_deployment`, `gcp_cloudrun_update_traffic`) require the two-step HITL confirmation flow described above.
- **Secret Manager safety**: `gcp_secretmanager_list` returns secret *metadata only* (name, labels, create time, replication). The `secretmanager.projects.secrets.versions.access` API is **never called** — secret values cannot be read or returned by any tool in this server. The required role (`roles/secretmanager.viewer`) does not grant `secretAccessor` permissions.
- **MCP annotations**: all 65 tools carry standard `readOnlyHint` / `destructiveHint` / `idempotentHint` annotations — clients like Claude Desktop use these to decide whether to present a confirmation UI before calling a tool
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
│   resources/ bigquery · cloudrun · storage · iam                 │
│   prompts/   audit-security · optimize-bq · incident-response    │
└─────────────────────────────┬───────────────────────────────────┘
                              │ calls only ▼
┌─────────────────────────────▼───────────────────────────────────┐
│           ports/gcp_service.go   (Hexagon Boundary)              │
│                    GCPService interface (38 methods)              │
└─────────────────────────────┬───────────────────────────────────┘
                              │ implements ▼
┌─────────────────────────────▼───────────────────────────────────┐
│              internal/safety/   (Safety Decorator)               │
│   decorator.go — two-step HITL confirmation for mutations        │
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
│   regions (discoverRegions helper, 3-tier fallback + 10-min TTL) │
└─────────────────────────────┬───────────────────────────────────┘
                              │ Google Cloud Go SDK (gRPC / REST)
                          GCP APIs
```

**Dependency rule:** `internal/mcp` never imports `internal/gcp`. Both depend only on `ports/`. `internal/safety` sits at the port boundary — it implements `GCPService` and wraps the real adapter, wired exclusively in `cmd/`. The model sees only tool names and JSON schemas.

### Transport Flows

Set `MCP_TRANSPORT=sse` to switch from stdio to HTTP/SSE for Cloud Run deployments. The MCP protocol layer is identical in both modes.

```
# Local (stdio — default, Claude Desktop)
Claude Desktop ──stdio──► cmd/main.go ──► MCPServer ──► ports.GCPService
                                                               │
                                           gcpAdapter ◄────────┘
                                                │ ADC: gcloud user creds / SA key file
                                                ▼ GCP APIs

# Cloud Run (MCP_TRANSPORT=sse)
MCP Client ──HTTPS──► Cloud Run (PORT=$PORT)
                           │
           bearerAuthMiddleware: validates Bearer token → injects caller email into ctx (audit log)
                           │
                       MCPServer ──► ports.GCPService
                                          │
                          gcpAdapter ◄────┘
                               │ ADC: Workload Identity (GCE metadata server)
                               ▼ GCP APIs
```

> **Auth model for SSE:** GCP API calls always use the server's Workload Identity SA (or local ADC). The `Authorization: Bearer <token>` header is used only for MCP-layer audit logging — it identifies *who* made the request, not which GCP identity is used for API calls.

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
│   ├── safety/                    # Port-level safety decorator (HITL confirmation)
│   │   ├── decorator.go           # SafetyDecorator: implements GCPService, enforces plan/confirm flow
│   │   └── store.go               # PlanStore: sync.Mutex TTL map, single-use UUID plan IDs
│   ├── gcp/                       # GCP SDK adapter (secondary port)
│   │   ├── client.go              # gcpAdapter, New(), rate limiter, timeout, all client fields
│   │   ├── modules.go             # moduleClientDeps map, neededClients(), clientKey constants
│   │   ├── errors.go              # PermissionDeniedError, NotFoundError, ConfirmationRequiredError
│   │   ├── regions.go             # discoverRegions() — 3-tier fallback, 10-min TTL cache
│   │   ├── gke.go                 # ListClusters, GetClusterDetails, ScaleDeployment
│   │   ├── gke_bottleneck.go      # GetClusterBottlenecks (errgroup fan-out)
│   │   ├── cloudrun.go            # ListServices, GetServiceDetails, UpdateTraffic, ListJobs, GetJobDetails, ListJobExecutions
│   │   ├── functions.go           # ListFunctions (Gen1+Gen2), GetFunctionDetails
│   │   ├── eventarc.go            # ListTriggers, GetTrigger (multi-region fan-out)
│   │   ├── scheduler.go           # ListSchedulerJobs (multi-region fan-out)
│   │   ├── workflows.go           # ListWorkflows, ListWorkflowExecutions
│   │   ├── tasks.go               # ListTaskQueues
│   │   ├── secretmanager.go       # ListSecrets (metadata only, no values read)
│   │   ├── vpcaccess.go           # ListVPCConnectors
│   │   ├── cloudsql.go            # ListSQLInstances
│   │   ├── pubsub.go              # ListTopics, InspectTopicHealth, ListSubscriptions
│   │   ├── logging.go             # QueryRecentLogs
│   │   ├── monitoring.go          # GetMetrics, ListMetricDescriptors, ListTraceServices, ListAlertPolicies, ListUptimeChecks, ListSLOs, ListDashboards
│   │   ├── iam.go                 # TestPermissions
│   │   ├── topology.go            # GetServiceTopology (dependency graph inference)
│   │   ├── aura.go                # GetAuraScore, GetProjectAuraSummary (health + efficiency scoring)
│   │   ├── serverlessgraph.go     # ExportServerlessGraph (parallel fan-out + edge stitching)
│   │   ├── bigquery.go            # ListDatasets, ListTables, GetTableSchema
│   │   ├── storage.go             # ListBuckets, GetBucketMetadata
│   │   ├── cache.go               # Generic TTL cache (5-min aura scores, 10-min regions)
│   │   ├── dlp.go                 # DLPAdapter: GCP DLP client, InspectText
│   │   └── util.go                # isIteratorDone, isGRPCNotFound helpers
│   └── mcp/                       # MCP protocol layer (primary port)
│       ├── server.go              # tool + resource + prompt registration
│       ├── registry.go            # module constants, AllModules, FilteredRegistry
│       ├── tools/                 # one file per GCP domain (65 tools)
│       ├── resources/             # MCP Resource handlers (10 resources)
│       │   ├── resources.go       # constructor types
│       │   ├── bigquery.go        # gcp://{p}/bigquery/... (datasets, tables, schema)
│       │   ├── cloudrun.go        # gcp://{p}/cloudrun/... (services, snapshot, revisions)
│       │   ├── storage.go         # gcp://{p}/storage/... (buckets, metadata, objects)
│       │   └── iam.go             # gcp://{p}/iam/my-permissions
│       └── prompts/               # MCP Prompt Templates (3 prompts)
│           └── prompts.go         # audit-security · optimize-bq · incident-response
├── pkg/models/                    # shared input/output structs (no GCP deps)
│   ├── graph.go                   # GraphNode, GraphEdge, GraphGroup, ServerlessGraph, PartialResult
│   ├── errors.go                  # ToolError (structured error envelope)
│   └── ...                        # one file per GCP domain
└── ports/
    ├── gcp_service.go             # GCPService interface (hexagon boundary, 38 methods)
    └── dlp_service.go             # DLPService interface (secondary hexagon port)
```
