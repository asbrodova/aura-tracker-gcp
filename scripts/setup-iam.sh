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
#   PROJECT_ID=my-project SECURITY_AUDIT_ENABLED=true bash scripts/setup-iam.sh
#   PROJECT_ID=my-project SERVICE_HEALTH_ENABLED=true bash scripts/setup-iam.sh
#   PROJECT_ID=my-project COST_REASONING_ENABLED=true BILLING_EXPORT_DATASET=cloud_billing bash scripts/setup-iam.sh
#   PROJECT_ID=my-project MODULES=cloudrun,monitoring bash scripts/setup-iam.sh
#   PROJECT_ID=my-project LOCAL_IMPERSONATION_PRINCIPAL=user:developer@example.com bash scripts/setup-iam.sh
set -euo pipefail

PROJECT_ID="${PROJECT_ID:?PROJECT_ID environment variable is required}"
MUTATION_ROLES="${MUTATION_ROLES:-false}"
RECOMMENDER_ENABLED="${RECOMMENDER_ENABLED:-false}"
SECURITY_AUDIT_ENABLED="${SECURITY_AUDIT_ENABLED:-false}"
SECURITY_AUDIT_FLEET_PROJECT_ID="${SECURITY_AUDIT_FLEET_PROJECT_ID:-$PROJECT_ID}"
SECURITY_AUDIT_ORGANIZATION_ID="${SECURITY_AUDIT_ORGANIZATION_ID:-}"
SERVICE_HEALTH_ENABLED="${SERVICE_HEALTH_ENABLED:-false}"
COST_REASONING_ENABLED="${COST_REASONING_ENABLED:-false}"
COST_QUERY_PROJECT_ID="${COST_QUERY_PROJECT_ID:-$PROJECT_ID}"
BILLING_EXPORT_PROJECT_ID="${BILLING_EXPORT_PROJECT_ID:-$PROJECT_ID}"
BILLING_EXPORT_DATASET="${BILLING_EXPORT_DATASET:-}"
LOCAL_IMPERSONATION_PRINCIPAL="${LOCAL_IMPERSONATION_PRINCIPAL:-}"
MODULES="${MODULES:-all}"

# Keep this list aligned with internal/mcp/registry.go. "all" means the
# zero-config default modules; recommender_export remains explicitly opt-in.
DEFAULT_MODULES="gke,cloudrun,pubsub,logging,monitoring,iam,topology,aura,storage,functions,eventarc,scheduler,workflows,tasks,secretmanager,vpcaccess,cloudsql,serverlessgraph,gke_workloads,gke_mesh,networking,datastores,supplychain,coverage,archgraph,tagging,incident,cost,security,drift"
KNOWN_MODULES="${DEFAULT_MODULES},recommender_export"

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

validate_local_impersonation_principal() {
	local principal="$1"
	if [[ -z "$principal" ]]; then
		return
	fi
	if [[ "$principal" =~ [[:space:]] ]]; then
		echo "ERROR: LOCAL_IMPERSONATION_PRINCIPAL must not contain whitespace." >&2
		exit 2
	fi
	case "$principal" in
		user:*|group:*|serviceAccount:*) ;;
		*)
			echo "ERROR: LOCAL_IMPERSONATION_PRINCIPAL must start with user:, group:, or serviceAccount:." >&2
			exit 2
			;;
	esac
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

grant_role_on_organization() {
  local organization_id="$1"
  local role="$2"
  echo "  Reconciling ${role} on organizations/${organization_id}"
  gcloud organizations add-iam-policy-binding "$organization_id" \
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

is_known_module() {
  local candidate="$1"
  local known
  local old_ifs="$IFS"
  IFS=','
  for known in $KNOWN_MODULES; do
    if [[ "$candidate" == "$known" ]]; then
      IFS="$old_ifs"
      return 0
    fi
  done
  IFS="$old_ifs"
  return 1
}

module_enabled() {
  [[ ",${SELECTED_MODULES}," == *",$1,"* ]]
}

any_module_enabled() {
  local module
  for module in "$@"; do
    if module_enabled "$module"; then
      return 0
    fi
  done
  return 1
}

REQUIRED_APIS=()
REQUIRED_ROLES=()

add_required_api() {
  local value="$1"
  local existing
  for existing in "${REQUIRED_APIS[@]-}"; do
    [[ "$existing" == "$value" ]] && return
  done
  REQUIRED_APIS+=("$value")
}

add_required_role() {
  local value="$1"
  local existing
  for existing in "${REQUIRED_ROLES[@]-}"; do
    [[ "$existing" == "$value" ]] && return
  done
  REQUIRED_ROLES+=("$value")
}

validate_boolean MUTATION_ROLES "$MUTATION_ROLES"
validate_boolean RECOMMENDER_ENABLED "$RECOMMENDER_ENABLED"
validate_boolean SECURITY_AUDIT_ENABLED "$SECURITY_AUDIT_ENABLED"
validate_boolean SERVICE_HEALTH_ENABLED "$SERVICE_HEALTH_ENABLED"
validate_boolean COST_REASONING_ENABLED "$COST_REASONING_ENABLED"
validate_local_impersonation_principal "$LOCAL_IMPERSONATION_PRINCIPAL"

MODULES="${MODULES//[[:space:]]/}"
if [[ "$MODULES" == "all" ]]; then
  SELECTED_MODULES="$DEFAULT_MODULES"
elif [[ "$MODULES" == "none" ]]; then
  SELECTED_MODULES=""
else
  SELECTED_MODULES="$MODULES"
  old_ifs="$IFS"
  IFS=','
  for module in $SELECTED_MODULES; do
    if [[ -z "$module" ]] || ! is_known_module "$module"; then
      echo "ERROR: unknown MODULES entry '${module}'. Valid modules: ${KNOWN_MODULES}." >&2
      exit 2
    fi
  done
  IFS="$old_ifs"
fi

if [[ "$COST_REASONING_ENABLED" == "true" && -z "$BILLING_EXPORT_DATASET" ]]; then
  echo "ERROR: BILLING_EXPORT_DATASET is required when COST_REASONING_ENABLED=true." >&2
  exit 2
fi

# Always-on resources: BigQuery metadata, Cloud Run snapshots, Storage bucket
# metadata/object listings, and the IAM capability report.
for api in iamcredentials.googleapis.com cloudresourcemanager.googleapis.com bigquery.googleapis.com storage.googleapis.com run.googleapis.com; do
  add_required_api "$api"
done
for role in roles/browser roles/bigquery.metadataViewer roles/storage.objectViewer roles/storage.bucketViewer roles/run.viewer; do
  add_required_role "$role"
done

if any_module_enabled gke gke_workloads aura archgraph security drift; then
  add_required_api container.googleapis.com
  add_required_role roles/container.viewer
fi
if any_module_enabled pubsub topology serverlessgraph archgraph incident drift; then
  add_required_api pubsub.googleapis.com
  add_required_role roles/pubsub.viewer
fi
if any_module_enabled logging gke gke_mesh coverage archgraph incident security drift; then
  add_required_api logging.googleapis.com
  add_required_role roles/logging.viewer
fi
if any_module_enabled monitoring gke gke_mesh pubsub aura coverage archgraph incident cost drift; then
  add_required_api monitoring.googleapis.com
  add_required_role roles/monitoring.viewer
fi
if any_module_enabled monitoring coverage archgraph drift; then
  add_required_api cloudtrace.googleapis.com
  add_required_role roles/cloudtrace.user
fi
if any_module_enabled iam archgraph security drift; then
  add_required_api iam.googleapis.com
  add_required_role roles/iam.serviceAccountViewer
fi
if any_module_enabled functions serverlessgraph archgraph security drift; then
  add_required_api cloudfunctions.googleapis.com
  add_required_role roles/cloudfunctions.viewer
fi
if any_module_enabled eventarc serverlessgraph archgraph drift; then
  add_required_api eventarc.googleapis.com
  add_required_role roles/eventarc.viewer
fi
if any_module_enabled scheduler serverlessgraph archgraph drift; then
  add_required_api cloudscheduler.googleapis.com
  add_required_role roles/cloudscheduler.viewer
fi
if any_module_enabled workflows serverlessgraph archgraph drift; then
  add_required_api workflows.googleapis.com
  add_required_api workflowexecutions.googleapis.com
  add_required_role roles/workflows.viewer
fi
if any_module_enabled tasks serverlessgraph archgraph drift; then
  add_required_api cloudtasks.googleapis.com
  add_required_role roles/cloudtasks.viewer
fi
if any_module_enabled secretmanager serverlessgraph archgraph security drift; then
  add_required_api secretmanager.googleapis.com
  add_required_role roles/secretmanager.viewer
fi
if any_module_enabled vpcaccess serverlessgraph archgraph incident drift; then
  add_required_api vpcaccess.googleapis.com
  add_required_role roles/vpcaccess.viewer
fi
if any_module_enabled cloudsql serverlessgraph archgraph incident drift; then
  add_required_api sqladmin.googleapis.com
  add_required_role roles/cloudsql.viewer
fi
if any_module_enabled networking archgraph security drift; then
  add_required_api compute.googleapis.com
  add_required_role roles/compute.viewer
fi
if any_module_enabled networking archgraph drift; then
  add_required_api apigateway.googleapis.com
  add_required_role roles/apigateway.viewer
fi
if any_module_enabled datastores archgraph drift; then
  for api in spanner.googleapis.com alloydb.googleapis.com firestore.googleapis.com redis.googleapis.com; do
    add_required_api "$api"
  done
  for role in roles/spanner.viewer roles/alloydb.viewer roles/datastore.viewer roles/redis.viewer; do
    add_required_role "$role"
  done
fi
if any_module_enabled supplychain archgraph drift; then
  for api in artifactregistry.googleapis.com cloudbuild.googleapis.com servicedirectory.googleapis.com; do
    add_required_api "$api"
  done
  for role in roles/artifactregistry.reader roles/cloudbuild.builds.viewer roles/servicedirectory.viewer; do
    add_required_role "$role"
  done
fi
if any_module_enabled tagging security || [[ "$SECURITY_AUDIT_ENABLED" == "true" ]]; then
  add_required_role roles/resourcemanager.tagViewer
fi
if any_module_enabled cost security || [[ "$COST_REASONING_ENABLED" == "true" || "$SECURITY_AUDIT_ENABLED" == "true" ]]; then
  add_required_api cloudasset.googleapis.com
  add_required_role roles/cloudasset.viewer
  add_required_role roles/serviceusage.serviceUsageConsumer
fi
if any_module_enabled aura cost security recommender_export || [[ "$RECOMMENDER_ENABLED" == "true" || "$SECURITY_AUDIT_ENABLED" == "true" ]]; then
  add_required_api recommender.googleapis.com
  add_required_role roles/recommender.viewer
fi
if module_enabled security || [[ "$SECURITY_AUDIT_ENABLED" == "true" ]]; then
  add_required_api gkehub.googleapis.com
  add_required_api connectgateway.googleapis.com
  add_required_role roles/gkehub.viewer
  add_required_role roles/gkehub.gatewayReader
  add_required_role roles/recommender.iamViewer
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

echo "Enabling APIs required by selected modules and always-on resources..."
for api in "${REQUIRED_APIS[@]}"; do
  enable_api "$api"
done

if [[ -n "$LOCAL_IMPERSONATION_PRINCIPAL" ]]; then
	echo "Granting keyless local impersonation to ${LOCAL_IMPERSONATION_PRINCIPAL}..."
	gcloud iam service-accounts add-iam-policy-binding "$SA_EMAIL" \
		--project="$PROJECT_ID" \
		--member="$LOCAL_IMPERSONATION_PRINCIPAL" \
		--role=roles/iam.serviceAccountTokenCreator \
		--condition=None \
		--quiet
fi

# ── Role reconciliation ────────────────────────────────────────────────────────
echo "Reconciling module-aware read-only roles..."

# Read-only roles — grants only list/get on the specific services this server calls.
# No roles/viewer, roles/editor, or roles/owner are used.
for role in "${REQUIRED_ROLES[@]}"; do
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
# gcp_cloudrun_update_traffic. Both tools use two-step confirmation when safety is enabled.
if [[ "$MUTATION_ROLES" == "true" ]]; then
  echo "Reconciling mutation roles (container.admin, run.admin)..."
  for role in roles/container.admin roles/run.admin; do
    grant_project_role "$role"
  done
  echo "WARNING: Mutation roles granted. Consider scoping to specific resources via IAM Conditions."
fi

# Recommender API and role (opt-in) — required only when RECOMMENDER_ENABLED=true.
if [[ "$RECOMMENDER_ENABLED" == "true" ]]; then
  echo "Cloud Recommender integration was included in the reconciled capability set."
fi

# The default module set already receives project-level read-only security
# capabilities. This flag additionally requests cross-fleet/ancestor setup and
# is useful when MODULES excludes security. Secret values remain inaccessible.
if [[ "$SECURITY_AUDIT_ENABLED" == "true" ]]; then
  echo "Configuring project security posture audit..."
  # The security collector uses read-only Kubernetes API requests. Connect
  # Gateway is the fallback for private/unreachable control planes.
  if [[ "$SECURITY_AUDIT_FLEET_PROJECT_ID" != "$PROJECT_ID" ]]; then
    enable_api_on_project "$SECURITY_AUDIT_FLEET_PROJECT_ID" gkehub.googleapis.com
    enable_api_on_project "$SECURITY_AUDIT_FLEET_PROJECT_ID" connectgateway.googleapis.com
    grant_role_on_project "$SECURITY_AUDIT_FLEET_PROJECT_ID" roles/gkehub.viewer
    grant_role_on_project "$SECURITY_AUDIT_FLEET_PROJECT_ID" roles/gkehub.gatewayReader
  fi

  # Parent policies are not readable from a project-level grant. An
  # organization administrator can opt into these read-only organization
  # bindings so folder/org allow policies and IAM deny policies are complete.
  if [[ -n "$SECURITY_AUDIT_ORGANIZATION_ID" ]]; then
    grant_role_on_organization "$SECURITY_AUDIT_ORGANIZATION_ID" roles/iam.securityReviewer
    grant_role_on_organization "$SECURITY_AUDIT_ORGANIZATION_ID" roles/iam.denyReviewer
    grant_role_on_organization "$SECURITY_AUDIT_ORGANIZATION_ID" roles/browser
  else
    echo "INFO: Set SECURITY_AUDIT_ORGANIZATION_ID to grant read-only ancestor IAM and deny-policy access."
  fi
  echo "INFO: Apply deploy/security-audit-rbac.yaml.tmpl to every audited cluster after replacing AURA_SECURITY_PRINCIPAL."
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
echo "--- Option A: Keyless local development ---"
if [[ -z "$LOCAL_IMPERSONATION_PRINCIPAL" ]]; then
	echo "  # Prerequisite: rerun this script as an admin with"
	echo "  # LOCAL_IMPERSONATION_PRINCIPAL=user:YOUR_EMAIL (or group:/serviceAccount:)."
else
	echo "  # Token Creator was reconciled on this service account for ${LOCAL_IMPERSONATION_PRINCIPAL}."
fi
echo "  gcloud auth application-default login \\"
echo "    --impersonate-service-account=${SA_EMAIL}"
echo "  # No service-account key file is created or stored in the repository."
echo ""
echo "--- Option B: Attached service account (Cloud Run — recommended for production) ---"
echo "  gcloud run services update aura-tracker-gcp \\"
echo "    --service-account=${SA_EMAIL} \\"
echo "    --project=${PROJECT_ID} \\"
echo "    --region=REGION \\"
echo "    --max-instances=1"
echo "  # SSE sessions are process-local; keep one instance unless session-affine routing is guaranteed."
echo ""
echo "--- Audit granted roles ---"
echo "  gcloud projects get-iam-policy ${PROJECT_ID} \\"
echo "    --flatten='bindings[].members' \\"
echo "    --filter=\"bindings.members:${SA_EMAIL}\" \\"
echo "    --format='table(bindings.role)'"
