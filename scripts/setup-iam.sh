#!/usr/bin/env bash
# setup-iam.sh — Idempotent team-admin setup for the aura-tracker-mcp service account.
#
# Run whenever the required role or optional-feature set changes. The script creates
# the service account when missing and otherwise reconciles its project IAM roles.
# Optional API flags enable the corresponding API as well as granting its viewer role.
# Flags are additive: false/omitted options do not disable APIs or revoke old roles.
#
# Usage:
#   PROJECT_ID=my-project bash scripts/setup-iam.sh
#   PROJECT_ID=my-project MUTATION_ROLES=true bash scripts/setup-iam.sh
#   PROJECT_ID=my-project RECOMMENDER_ENABLED=true bash scripts/setup-iam.sh
#   PROJECT_ID=my-project SERVICE_HEALTH_ENABLED=true bash scripts/setup-iam.sh
#   PROJECT_ID=my-project COST_REASONING_ENABLED=true BILLING_EXPORT_DATASET=cloud_billing bash scripts/setup-iam.sh
set -euo pipefail

PROJECT_ID="${PROJECT_ID:?PROJECT_ID environment variable is required}"
MUTATION_ROLES="${MUTATION_ROLES:-false}"
RECOMMENDER_ENABLED="${RECOMMENDER_ENABLED:-false}"
SERVICE_HEALTH_ENABLED="${SERVICE_HEALTH_ENABLED:-false}"
COST_REASONING_ENABLED="${COST_REASONING_ENABLED:-false}"
COST_QUERY_PROJECT_ID="${COST_QUERY_PROJECT_ID:-$PROJECT_ID}"
BILLING_EXPORT_PROJECT_ID="${BILLING_EXPORT_PROJECT_ID:-$PROJECT_ID}"
BILLING_EXPORT_DATASET="${BILLING_EXPORT_DATASET:-}"

SA_NAME="aura-tracker-mcp"
SA_EMAIL="${SA_NAME}@${PROJECT_ID}.iam.gserviceaccount.com"

validate_boolean() {
  local name="$1"
  local value="$2"
  if [[ "$value" != "true" && "$value" != "false" ]]; then
    echo "ERROR: ${name} must be 'true' or 'false' (got '${value}')." >&2
    exit 2
  fi
}

grant_project_role() {
  local role="$1"
  grant_role_on_project "$PROJECT_ID" "$role"
}

grant_role_on_project() {
  local target_project="$1"
  local role="$2"
  echo "  Reconciling ${role} on ${target_project}"
  gcloud projects add-iam-policy-binding "$target_project" \
    --member="serviceAccount:${SA_EMAIL}" \
    --role="$role" \
    --condition=None \
    --quiet
}

enable_api() {
  local api="$1"
  enable_api_on_project "$PROJECT_ID" "$api"
}

enable_api_on_project() {
  local target_project="$1"
  local api="$2"
  echo "  Enabling ${api}"
  gcloud services enable "$api" \
    --project="$target_project" \
    --quiet
}

validate_boolean MUTATION_ROLES "$MUTATION_ROLES"
validate_boolean RECOMMENDER_ENABLED "$RECOMMENDER_ENABLED"
validate_boolean SERVICE_HEALTH_ENABLED "$SERVICE_HEALTH_ENABLED"
validate_boolean COST_REASONING_ENABLED "$COST_REASONING_ENABLED"

if [[ "$COST_REASONING_ENABLED" == "true" && -z "$BILLING_EXPORT_DATASET" ]]; then
  echo "ERROR: BILLING_EXPORT_DATASET is required when COST_REASONING_ENABLED=true." >&2
  exit 2
fi

# ── Existence check ────────────────────────────────────────────────────────────
describe_output=""
describe_status=0
if describe_output="$(gcloud iam service-accounts describe "$SA_EMAIL" \
     --project="$PROJECT_ID" --format="value(email)" 2>&1)"; then
  echo "INFO: Service account ${SA_EMAIL} already exists in project ${PROJECT_ID}."
  echo "Reconciling requested APIs and IAM roles."
else
  describe_status=$?
  if [[ "$describe_output" == *"NOT_FOUND"* || "$describe_output" == *"does not exist"* ]]; then
    echo "Creating service account: ${SA_EMAIL}"
    gcloud iam service-accounts create "$SA_NAME" \
      --project="$PROJECT_ID" \
      --display-name="Aura Tracker MCP Server" \
      --description="Least-privilege SA for aura-tracker-gcp MCP server"
  else
    echo "ERROR: Unable to determine whether ${SA_EMAIL} exists; refusing to create it." >&2
    echo "$describe_output" >&2
    exit "$describe_status"
  fi
fi

# ── Role reconciliation ────────────────────────────────────────────────────────
echo "Reconciling core read-only roles..."

# Read-only roles — grants only list/get on the specific services this server calls.
# No roles/viewer, roles/editor, or roles/owner are used.
for role in \
  roles/container.viewer \
  roles/run.viewer \
  roles/pubsub.viewer \
  roles/logging.viewer \
  roles/monitoring.viewer \
  roles/bigquery.metadataViewer \
  roles/storage.objectViewer \
  roles/cloudsql.viewer \
  roles/vpcaccess.viewer \
  roles/browser; do
  grant_project_role "$role"
done

# Personalized Service Health is optional. The incident tool queries its Cloud
# Logging event stream only when include_platform_health=true.
if [[ "$SERVICE_HEALTH_ENABLED" == "true" ]]; then
  echo "Configuring optional Personalized Service Health correlation..."
  enable_api servicehealth.googleapis.com
  grant_project_role roles/servicehealth.viewer
fi

# Mutation roles (opt-in) — required only for gcp_gke_scale_deployment and
# gcp_cloudrun_update_traffic. Both tools enforce two-step HITL confirmation.
if [[ "$MUTATION_ROLES" == "true" ]]; then
  echo "Reconciling mutation roles (container.admin, run.admin)..."
  for role in roles/container.admin roles/run.admin; do
    grant_project_role "$role"
  done
  echo "WARNING: Mutation roles granted. Consider scoping to specific resources via IAM Conditions."
fi

# Recommender API and role (opt-in) — required only when RECOMMENDER_ENABLED=true.
if [[ "$RECOMMENDER_ENABLED" == "true" ]]; then
  echo "Configuring optional Cloud Recommender integration..."
  enable_api recommender.googleapis.com
  grant_project_role roles/recommender.viewer
fi

# Cost reasoning is opt-in because it executes chargeable BigQuery queries over
# a detailed billing export. The data-viewer binding is project-scoped for an
# idempotent CLI-only setup; production teams can replace it with the same role
# granted only on BILLING_EXPORT_DATASET.
if [[ "$COST_REASONING_ENABLED" == "true" ]]; then
  echo "Configuring optional cost reasoning..."
  enable_api_on_project "$COST_QUERY_PROJECT_ID" bigquery.googleapis.com
  enable_api cloudasset.googleapis.com
  grant_role_on_project "$COST_QUERY_PROJECT_ID" roles/bigquery.jobUser
  grant_role_on_project "$BILLING_EXPORT_PROJECT_ID" roles/bigquery.dataViewer
  grant_project_role roles/cloudasset.viewer
  grant_project_role roles/serviceusage.serviceUsageConsumer
  echo "INFO: roles/bigquery.dataViewer was granted project-wide on ${BILLING_EXPORT_PROJECT_ID}."
  echo "      For tighter access, grant it only on dataset ${BILLING_EXPORT_DATASET} and remove the project binding."
fi

echo ""
echo "Done. APIs and IAM roles are reconciled for: ${SA_EMAIL}"
echo ""
echo "--- Option A: Key file (local dev) ---"
echo "  gcloud iam service-accounts keys create sa-key.json \\"
echo "    --iam-account=${SA_EMAIL} --project=${PROJECT_ID}"
echo "  export GOOGLE_APPLICATION_CREDENTIALS=\$(pwd)/sa-key.json"
echo "  # Add sa-key.json to .gitignore — never commit key files"
echo ""
echo "--- Option B: Workload Identity (Cloud Run — recommended for production) ---"
echo "  gcloud run services update aura-tracker-gcp \\"
echo "    --service-account=${SA_EMAIL} \\"
echo "    --project=${PROJECT_ID} \\"
echo "    --region=REGION"
echo ""
echo "--- Audit granted roles ---"
echo "  gcloud projects get-iam-policy ${PROJECT_ID} \\"
echo "    --flatten='bindings[].members' \\"
echo "    --filter=\"bindings.members:${SA_EMAIL}\" \\"
echo "    --format='table(bindings.role)'"
