// Package prompts implements MCP Prompt Templates for complex GCP workflows.
// It imports ports.GCPService only — never internal/gcp.
package prompts

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/asbrodova/aura-tracker-gcp/ports"
)

// GCPPrompts holds the three built-in prompt templates.
type GCPPrompts struct {
	svc ports.GCPService
	log *slog.Logger
}

func NewGCPPrompts(svc ports.GCPService, log *slog.Logger) *GCPPrompts {
	return &GCPPrompts{svc: svc, log: log}
}

// AuditSecurityPosture guides the AI through a structured IAM + network security audit.
func (p *GCPPrompts) AuditSecurityPosture() server.ServerPrompt {
	return server.ServerPrompt{
		Prompt: mcp.NewPrompt("audit-security-posture",
			mcp.WithPromptDescription("Gather IAM bindings and Cloud Run ingress configuration to surface security vulnerabilities"),
			mcp.WithArgument("project_id",
				mcp.ArgumentDescription("GCP project ID to audit"),
				mcp.RequiredArgument(),
			),
			mcp.WithArgument("focus",
				mcp.ArgumentDescription("Audit scope: 'iam', 'network', or 'all' (default: all)"),
			),
		),
		Handler: p.auditSecurityPostureHandler,
	}
}

func (p *GCPPrompts) auditSecurityPostureHandler(_ context.Context, req mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	project := req.Params.Arguments["project_id"]
	if project == "" {
		return nil, fmt.Errorf("project_id is required")
	}
	focus := req.Params.Arguments["focus"]
	if focus == "" {
		focus = "all"
	}

	instructions := fmt.Sprintf(`You are a GCP security auditor for project %q. Follow these steps in order:

Step 1 — Check server capabilities:
  Read resource gcp://%s/iam/my-permissions
  Note any denied permissions that may limit the audit scope.

Step 2 — Audit IAM (if focus is 'iam' or 'all'):
  Call tool gcp_iam_test_permissions with project_id=%q.
  Look for: Owner/Editor primitive roles on non-admin principals, service accounts with excessive scope,
  allUsers or allAuthenticatedUsers bindings.

Step 3 — Audit Cloud Run ingress (if focus is 'network' or 'all'):
  Call tool gcp_cloudrun_list_services with project_id=%q and region="-".
  For each service, flag those where ingress allows all traffic (not 'internal-and-cloud-load-balancing').

Step 4 — Synthesise and report:
  Return a structured report grouped by severity:
  - CRITICAL: public + unauthenticated exposure, primitive Owner/Editor on external identities
  - HIGH: public ingress without IAP, overly broad service account roles
  - MEDIUM: missing resource labels, unused service accounts, no org policy constraints
  For each finding include: affected resource, risk description, and the exact gcloud or Terraform remediation.

Audit focus: %s`, project, project, project, project, focus)

	return &mcp.GetPromptResult{
		Description: fmt.Sprintf("Security posture audit for project %q (focus: %s)", project, focus),
		Messages: []mcp.PromptMessage{
			mcp.NewPromptMessage(mcp.RoleUser, mcp.NewTextContent(instructions)),
		},
	}, nil
}

// OptimizeBigQueryCosts guides the AI through schema-aware cost optimisation analysis.
func (p *GCPPrompts) OptimizeBigQueryCosts() server.ServerPrompt {
	return server.ServerPrompt{
		Prompt: mcp.NewPrompt("optimize-bigquery-costs",
			mcp.WithPromptDescription("Read BigQuery schemas and slot usage to recommend partitioning, clustering, and expiration policies"),
			mcp.WithArgument("project_id",
				mcp.ArgumentDescription("GCP project ID"),
				mcp.RequiredArgument(),
			),
			mcp.WithArgument("dataset_id",
				mcp.ArgumentDescription("Specific dataset to analyse (optional — analyses all datasets if omitted)"),
			),
		),
		Handler: p.optimizeBigQueryCostsHandler,
	}
}

func (p *GCPPrompts) optimizeBigQueryCostsHandler(_ context.Context, req mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	project := req.Params.Arguments["project_id"]
	if project == "" {
		return nil, fmt.Errorf("project_id is required")
	}
	dataset := req.Params.Arguments["dataset_id"]

	var discoveryStep string
	if dataset != "" {
		discoveryStep = fmt.Sprintf(
			"Read resource gcp://%s/bigquery/%s/tables to list tables.\n"+
				"  For each table, read gcp://%s/bigquery/%s/{table}/schema to inspect field types.",
			project, dataset, project, dataset,
		)
	} else {
		discoveryStep = fmt.Sprintf(
			"Read resource gcp://%s/bigquery/datasets to discover datasets.\n"+
				"  For each dataset, read gcp://%s/bigquery/{dataset}/tables to find the largest tables.\n"+
				"  For the top 5 largest tables (by size_gb), read gcp://%s/bigquery/{dataset}/{table}/schema.",
			project, project, project,
		)
	}

	instructions := fmt.Sprintf(`You are a BigQuery cost optimisation expert for project %q.

Step 1 — Discover schemas:
  %s

Step 2 — Fetch slot usage (7-day window):
  Call tool gcp_monitoring_get_metrics with:
    project_id=%q
    metric_type=bigquery.googleapis.com/slots/allocated_for_project
    hours=168

Step 3 — Analyse and recommend for each table:
  a) Partitioning: if schema contains DATE or TIMESTAMP fields, recommend partitioning on that column
     and set partition_expiration_days (90 for logs, 365 for analytics, 730 for compliance data).
  b) Clustering: identify STRING/INTEGER columns likely used in WHERE or GROUP BY clauses
     (e.g. user_id, region, status, event_type) and suggest clustering on up to 4 columns.
  c) Table expiration: if the table name contains _tmp, _staging, _bak, or _raw, recommend
     setting a table expiration of 7–30 days.
  d) Slot rightsizing: if peak slot usage < 50%% of allocated slots, recommend switching to
     on-demand pricing or reducing flat-rate slot commitments.
  e) Materialized views: if you see repeated aggregate patterns in column names (e.g. daily_count,
     monthly_revenue), suggest a materialized view to avoid full-table scans.

Return a prioritised table ordered by estimated monthly savings (highest first).`, project, discoveryStep, project)

	return &mcp.GetPromptResult{
		Description: fmt.Sprintf("BigQuery cost optimisation for project %q", project),
		Messages: []mcp.PromptMessage{
			mcp.NewPromptMessage(mcp.RoleUser, mcp.NewTextContent(instructions)),
		},
	}, nil
}

// IncidentResponseHelper guides the AI through a structured incident triage for a Cloud Run service.
func (p *GCPPrompts) IncidentResponseHelper() server.ServerPrompt {
	return server.ServerPrompt{
		Prompt: mcp.NewPrompt("incident-response-helper",
			mcp.WithPromptDescription("Pull error logs, key metrics, and Aura score for a Cloud Run service to diagnose an active incident"),
			mcp.WithArgument("project_id",
				mcp.ArgumentDescription("GCP project ID"),
				mcp.RequiredArgument(),
			),
			mcp.WithArgument("service_name",
				mcp.ArgumentDescription("Cloud Run service name"),
				mcp.RequiredArgument(),
			),
			mcp.WithArgument("region",
				mcp.ArgumentDescription("GCP region where the service is deployed"),
				mcp.RequiredArgument(),
			),
		),
		Handler: p.incidentResponseHelperHandler,
	}
}

func (p *GCPPrompts) incidentResponseHelperHandler(_ context.Context, req mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	project := req.Params.Arguments["project_id"]
	service := req.Params.Arguments["service_name"]
	region := req.Params.Arguments["region"]

	if project == "" || service == "" || region == "" {
		return nil, fmt.Errorf("project_id, service_name, and region are required")
	}

	instructions := fmt.Sprintf(`You are an SRE responding to an active incident for Cloud Run service %q in project %q (region: %s).
Work through these steps quickly and systematically:

Step 1 — Read current service state:
  Read resource gcp://%s/cloudrun/%s/%s
  Note: current URL, traffic splits, latest revision name, and any labels that indicate environment/version.

Step 2 — Pull error logs (last 2 hours):
  Call tool gcp_logging_query_recent with:
    project_id=%q
    filter=resource.type="cloud_run_revision" AND resource.labels.service_name="%s" AND severity>=ERROR
    hours=2
    limit=50
  Identify: most frequent error messages, first occurrence timestamp, affected revisions.

Step 3 — Pull request metrics (last 30 minutes):
  Call tool gcp_monitoring_get_metrics with project_id=%q, metric_type=run.googleapis.com/request/count, hours=1
  Then call again with metric_type=run.googleapis.com/request/latencies
  Identify: request rate drop, latency spike, or error rate increase.

Step 4 — Get Aura health score:
  Call tool gcp_get_aura_score with:
    project_id=%q
    resource_kind=cloud_run
    region=%q
    resource_name=%q

Step 5 — Synthesise incident report:
  Produce a structured report containing:
  1. Status: CRITICAL / DEGRADED / RECOVERING / HEALTHY (based on error rate and Aura score)
  2. Timeline: when errors started, correlation with recent deployments (check latest_revision vs traffic)
  3. Root cause hypothesis: the most probable cause based on error patterns
  4. Immediate actions (in priority order):
     - If latest revision is causing errors: call gcp_cloudrun_update_traffic to roll back traffic to previous revision
     - Include the exact arguments needed for the rollback call
  5. Follow-up: monitoring thresholds to set, code fixes to investigate`, service, project, region, project, region, service, project, service, project, project, region, service)

	return &mcp.GetPromptResult{
		Description: fmt.Sprintf("Incident response for Cloud Run service %q in project %q", service, project),
		Messages: []mcp.PromptMessage{
			mcp.NewPromptMessage(mcp.RoleUser, mcp.NewTextContent(instructions)),
		},
	}, nil
}
