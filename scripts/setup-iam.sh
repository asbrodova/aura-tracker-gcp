#!/usr/bin/env bash
# setup-iam.sh — One-time team-admin setup for the aura-tracker-mcp service account.
#
# Run once per GCP project. If the SA already exists, the script prints onboarding
# instructions for other team members and exits without making any changes.
#
# Usage:
#   PROJECT_ID=my-project bash scripts/setup-iam.sh
#   PROJECT_ID=my-project MUTATION_ROLES=true bash scripts/setup-iam.sh
#   PROJECT_ID=my-project RECOMMENDER_ENABLED=true bash scripts/setup-iam.sh
set -euo pipefail

PROJECT_ID="${PROJECT_ID:?PROJECT_ID environment variable is required}"
MUTATION_ROLES="${MUTATION_ROLES:-false}"
RECOMMENDER_ENABLED="${RECOMMENDER_ENABLED:-false}"

SA_NAME="aura-tracker-mcp"
SA_EMAIL="${SA_NAME}@${PROJECT_ID}.iam.gserviceaccount.com"

# ── Existence check ────────────────────────────────────────────────────────────
# If the SA already exists, print onboarding instructions and exit cleanly.
# This prevents a second team member from accidentally re-running setup.
if gcloud iam service-accounts describe "$SA_EMAIL" \
     --project="$PROJECT_ID" --format="value(email)" &>/dev/null; then
  echo ""
  echo "INFO: Service account ${SA_EMAIL} already exists in project ${PROJECT_ID}."
  echo "      The team admin has already run this setup — you do NOT need to run it again."
  echo ""
  echo "  To develop locally, choose one of:"
  echo ""
  echo "  Option A — your own gcloud credentials (simplest, if your account has the roles):"
  echo "    gcloud auth application-default login"
  echo "    export GCP_PROJECT_ID=${PROJECT_ID}"
  echo ""
  echo "  Option B — shared SA key file (ask a project admin to generate one for you):"
  echo "    gcloud iam service-accounts keys create sa-key.json \\"
  echo "      --iam-account=${SA_EMAIL} --project=${PROJECT_ID}"
  echo "    export GOOGLE_APPLICATION_CREDENTIALS=\$(pwd)/sa-key.json"
  echo "    export GCP_PROJECT_ID=${PROJECT_ID}"
  echo "    # Add sa-key.json to .gitignore — never commit key files"
  echo ""
  echo "  To view currently granted roles:"
  echo "    gcloud projects get-iam-policy ${PROJECT_ID} \\"
  echo "      --flatten='bindings[].members' \\"
  echo "      --filter=\"bindings.members:${SA_EMAIL}\" \\"
  echo "      --format='table(bindings.role)'"
  exit 0
fi

# ── First-time setup ───────────────────────────────────────────────────────────
echo "Creating service account: ${SA_EMAIL}"
gcloud iam service-accounts create "$SA_NAME" \
  --project="$PROJECT_ID" \
  --display-name="Aura Tracker MCP Server" \
  --description="Least-privilege SA for aura-tracker-gcp MCP server"

echo "Granting read-only roles..."

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
  roles/browser; do
  gcloud projects add-iam-policy-binding "$PROJECT_ID" \
    --member="serviceAccount:${SA_EMAIL}" \
    --role="$role" \
    --condition=None \
    --quiet
done

# Mutation roles (opt-in) — required only for gcp_gke_scale_deployment and
# gcp_cloudrun_update_traffic. Both tools enforce two-step HITL confirmation.
if [[ "$MUTATION_ROLES" == "true" ]]; then
  echo "Granting mutation roles (container.admin, run.admin)..."
  for role in roles/container.admin roles/run.admin; do
    gcloud projects add-iam-policy-binding "$PROJECT_ID" \
      --member="serviceAccount:${SA_EMAIL}" \
      --role="$role" \
      --condition=None \
      --quiet
  done
  echo "WARNING: Mutation roles granted. Consider scoping to specific resources via IAM Conditions."
fi

# Recommender role (opt-in) — required only when RECOMMENDER_ENABLED=true.
if [[ "$RECOMMENDER_ENABLED" == "true" ]]; then
  echo "Granting recommender.viewer..."
  gcloud projects add-iam-policy-binding "$PROJECT_ID" \
    --member="serviceAccount:${SA_EMAIL}" \
    --role="roles/recommender.viewer" \
    --condition=None \
    --quiet
fi

echo ""
echo "Done. Service account: ${SA_EMAIL}"
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
