#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SETUP_SCRIPT="${SCRIPT_DIR}/setup-iam.sh"

fail() {
  echo "FAIL: $*" >&2
  exit 1
}

assert_contains() {
  local output="$1"
  local expected="$2"
  [[ "$output" == *"$expected"* ]] || fail "expected output to contain: ${expected}"
}

assert_not_contains() {
  local output="$1"
  local unexpected="$2"
  [[ "$output" != *"$unexpected"* ]] || fail "expected output not to contain: ${unexpected}"
}

run_setup() {
  local account_exists="$1"
  shift

  ACCOUNT_EXISTS="$account_exists" \
    PROJECT_ID=test-project \
    "$@" \
    bash -c '
      gcloud() {
        if [[ "$1" == "iam" && "$2" == "service-accounts" && "$3" == "describe" ]]; then
          case "$ACCOUNT_EXISTS" in
            true)
              printf "aura-tracker-mcp@test-project.iam.gserviceaccount.com\n"
              return 0
              ;;
            false)
              printf "ERROR: NOT_FOUND: Unknown service account\n" >&2
              return 1
              ;;
            error)
              printf "ERROR: PERMISSION_DENIED: iam.serviceAccounts.get\n" >&2
              return 1
              ;;
          esac
        fi
        printf "GCLOUD"
        printf " %q" "$@"
        printf "\n"
      }
      export -f gcloud
      bash "$1"
    ' _ "$SETUP_SCRIPT"
}

test_existing_account_reconciles_service_health() {
  local output
  output="$(run_setup true env SERVICE_HEALTH_ENABLED=true)"

  assert_contains "$output" "already exists"
  assert_not_contains "$output" "iam service-accounts create"
  assert_contains "$output" "services enable servicehealth.googleapis.com"
  assert_contains "$output" "--role=roles/servicehealth.viewer"
  assert_contains "$output" "--role=roles/logging.viewer"
}

test_missing_account_is_created() {
  local output
  output="$(run_setup false env)"

  assert_contains "$output" "iam service-accounts create aura-tracker-mcp"
  assert_contains "$output" "--role=roles/run.viewer"
	assert_contains "$output" "--role=roles/storage.bucketViewer"
	assert_contains "$output" "services enable cloudfunctions.googleapis.com"
	assert_contains "$output" "services enable artifactregistry.googleapis.com"
	assert_contains "$output" "services enable servicedirectory.googleapis.com"
  assert_not_contains "$output" "services enable servicehealth.googleapis.com"
	assert_contains "$output" "services enable recommender.googleapis.com"
}

test_modules_none_provisions_only_resources() {
	local output
	output="$(run_setup true env MODULES=none)"

	assert_contains "$output" "services enable bigquery.googleapis.com"
	assert_contains "$output" "services enable storage.googleapis.com"
	assert_contains "$output" "services enable run.googleapis.com"
	assert_contains "$output" "--role=roles/storage.bucketViewer"
	assert_not_contains "$output" "services enable container.googleapis.com"
	assert_not_contains "$output" "services enable recommender.googleapis.com"
	assert_not_contains "$output" "--role=roles/compute.viewer"
}

test_selected_modules_scope_capabilities() {
	local output
	output="$(run_setup true env MODULES=cloudrun,monitoring)"

	assert_contains "$output" "services enable monitoring.googleapis.com"
	assert_contains "$output" "services enable cloudtrace.googleapis.com"
	assert_contains "$output" "--role=roles/monitoring.viewer"
	assert_contains "$output" "--role=roles/cloudtrace.user"
	assert_not_contains "$output" "services enable compute.googleapis.com"
	assert_not_contains "$output" "services enable cloudfunctions.googleapis.com"
}

test_unknown_module_is_rejected() {
	local output
	local status

	set +e
	output="$(run_setup true env MODULES=cloudrun,typo 2>&1)"
	status=$?
	set -e

	[[ "$status" -eq 2 ]] || fail "expected unknown module to exit 2, got ${status}"
	assert_contains "$output" "unknown MODULES entry 'typo'"
	assert_not_contains "$output" "GCLOUD"
}

test_optional_flags_work_for_existing_account() {
  local output
  output="$(run_setup true env RECOMMENDER_ENABLED=true MUTATION_ROLES=true)"

  assert_contains "$output" "services enable recommender.googleapis.com"
  assert_contains "$output" "--role=roles/recommender.viewer"
  assert_contains "$output" "--role=roles/container.admin"
  assert_contains "$output" "--role=roles/run.admin"
}

test_security_audit_setup_is_read_only() {
  local output
  output="$(run_setup true env SECURITY_AUDIT_ENABLED=true)"

  assert_contains "$output" "services enable cloudasset.googleapis.com"
  assert_contains "$output" "services enable secretmanager.googleapis.com"
	assert_contains "$output" "services enable gkehub.googleapis.com"
	assert_contains "$output" "services enable connectgateway.googleapis.com"
  assert_contains "$output" "--role=roles/cloudasset.viewer"
  assert_contains "$output" "--role=roles/iam.serviceAccountViewer"
  assert_contains "$output" "--role=roles/secretmanager.viewer"
  assert_contains "$output" "--role=roles/compute.viewer"
  assert_contains "$output" "--role=roles/recommender.iamViewer"
	assert_contains "$output" "--role=roles/gkehub.viewer"
	assert_contains "$output" "--role=roles/gkehub.gatewayReader"
  assert_not_contains "$output" "roles/secretmanager.secretAccessor"
  assert_not_contains "$output" "roles/owner"
}

test_security_audit_organization_roles_are_opt_in() {
  local output
  output="$(run_setup true env SECURITY_AUDIT_ENABLED=true SECURITY_AUDIT_ORGANIZATION_ID=123456789)"

  assert_contains "$output" "organizations add-iam-policy-binding 123456789"
  assert_contains "$output" "--role=roles/iam.securityReviewer"
  assert_contains "$output" "--role=roles/iam.denyReviewer"
  assert_contains "$output" "--role=roles/browser"
}

test_invalid_boolean_is_rejected() {
  local output
  local status

  set +e
  output="$(run_setup true env SERVICE_HEALTH_ENABLED=yes 2>&1)"
  status=$?
  set -e

  [[ "$status" -eq 2 ]] || fail "expected invalid boolean to exit 2, got ${status}"
  assert_contains "$output" "SERVICE_HEALTH_ENABLED must be 'true' or 'false'"
  assert_not_contains "$output" "GCLOUD"
}

test_lookup_error_does_not_attempt_creation() {
  local output
  local status

  set +e
  output="$(run_setup error env 2>&1)"
  status=$?
  set -e

  [[ "$status" -ne 0 ]] || fail "expected lookup error to fail"
  assert_contains "$output" "refusing to create it"
  assert_contains "$output" "PERMISSION_DENIED"
  assert_not_contains "$output" "iam service-accounts create"
}

test_local_auth_guidance_is_keyless() {
  local output
  output="$(run_setup true env)"

	assert_contains "$output" "application-default login"
	assert_contains "$output" "--impersonate-service-account=aura-tracker-mcp@test-project.iam.gserviceaccount.com"
	assert_contains "$output" "services enable iamcredentials.googleapis.com"
	assert_contains "$output" "LOCAL_IMPERSONATION_PRINCIPAL=user:YOUR_EMAIL"
	assert_not_contains "$output" "service-accounts keys create"
	assert_not_contains "$output" "sa-key.json"
	assert_contains "$output" "--max-instances=1"
}

test_local_impersonation_principal_reconciles_token_creator() {
	local output
	output="$(run_setup true env LOCAL_IMPERSONATION_PRINCIPAL=user:developer@example.com)"

	assert_contains "$output" "services enable iamcredentials.googleapis.com"
	assert_contains "$output" "service-accounts add-iam-policy-binding"
	assert_contains "$output" "--member=user:developer@example.com"
	assert_contains "$output" "--role=roles/iam.serviceAccountTokenCreator"
	assert_contains "$output" "Token Creator was reconciled"
}

test_invalid_local_impersonation_principal_is_rejected() {
	local output
	local status

	set +e
	output="$(run_setup true env LOCAL_IMPERSONATION_PRINCIPAL=developer@example.com 2>&1)"
	status=$?
	set -e

	[[ "$status" -eq 2 ]] || fail "expected invalid principal to exit 2, got ${status}"
	assert_contains "$output" "must start with user:, group:, or serviceAccount:"
	assert_not_contains "$output" "GCLOUD"
}

test_existing_account_reconciles_service_health
test_missing_account_is_created
test_modules_none_provisions_only_resources
test_selected_modules_scope_capabilities
test_unknown_module_is_rejected
test_optional_flags_work_for_existing_account
test_security_audit_setup_is_read_only
test_security_audit_organization_roles_are_opt_in
test_invalid_boolean_is_rejected
test_lookup_error_does_not_attempt_creation
test_local_auth_guidance_is_keyless
test_local_impersonation_principal_reconciles_token_creator
test_invalid_local_impersonation_principal_is_rejected

echo "PASS: setup-iam.sh"
