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

// AuditSecurityPosture presents the deterministic project security audit.
func (p *GCPPrompts) AuditSecurityPosture() server.ServerPrompt {
	return server.ServerPrompt{
		Prompt: mcp.NewPrompt("audit-security-posture",
			mcp.WithPromptDescription("Run one comprehensive project security audit and present its severity-ranked findings, score, remediation, and coverage gaps"),
			mcp.WithArgument("project_id",
				mcp.ArgumentDescription("GCP project ID to audit"),
				mcp.RequiredArgument(),
			),
			mcp.WithArgument("focus",
				mcp.ArgumentDescription("Optional presentation emphasis: 'iam', 'network', or 'all'. The tool still performs a full audit so its score remains comparable."),
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

	instructions := fmt.Sprintf(`Run the deterministic security posture audit for GCP project %q.

Call gcp_project_security_audit exactly once with project_id=%q and refresh=false.

Present the returned summary in this order:
1. Score, score status, and coverage
2. Critical findings
3. High findings
4. Medium findings
5. Low findings
6. Prioritized recommendations
7. Coverage gaps

Preserve finding IDs, observed evidence, severity, and affected resources. Do not invent findings or describe an unassessed category as secure. Do not execute remediation or any mutation. If a score is unavailable, explain which coverage gap prevented a defensible score.

Presentation focus: %s. A focus other than "all" changes emphasis only; retain the full report and project-wide score.`, project, project, focus)

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

// IncidentResponseHelper invokes the incident correlation engine and asks the
// model to present its evidence without inventing causal certainty.
func (p *GCPPrompts) IncidentResponseHelper() server.ServerPrompt {
	return server.ServerPrompt{
		Prompt: mcp.NewPrompt("incident-response-helper",
			mcp.WithPromptDescription("Correlate deployments, metrics, revisions, IAM changes, logs, dependencies, and platform health for an active production incident"),
			mcp.WithArgument("project_id",
				mcp.ArgumentDescription("GCP project ID to diagnose"),
				mcp.RequiredArgument(),
			),
			mcp.WithArgument("service_name",
				mcp.ArgumentDescription("Cloud Run service name (optional; production services are inferred from labels when omitted)"),
			),
			mcp.WithArgument("region",
				mcp.ArgumentDescription("Cloud Run region (optional; discovered when omitted)"),
			),
			mcp.WithArgument("environment",
				mcp.ArgumentDescription("Environment label to infer (default: production)"),
			),
		),
		Handler: p.incidentResponseHelperHandler,
	}
}

func (p *GCPPrompts) incidentResponseHelperHandler(_ context.Context, req mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	project := req.Params.Arguments["project_id"]
	service := req.Params.Arguments["service_name"]
	region := req.Params.Arguments["region"]
	environment := req.Params.Arguments["environment"]

	if project == "" {
		return nil, fmt.Errorf("project_id is required")
	}
	if environment == "" {
		environment = "production"
	}

	instructions := fmt.Sprintf(`You are the incident commander for GCP project %q.

Call gcp_incident_diagnose exactly once with:
  project_id=%q
  environment=%q
  service_name=%q
  region=%q
  lookback_minutes=60
  baseline_minutes=240
  include_platform_health=true

If service_name or region is empty, omit that argument. The tool will discover a safely labelled production scope; if it returns needs_scope, show the candidates and ask the operator to select one.

Present the tool result as:
1. Status and scope
2. Possible root causes in returned rank order, preserving each likelihood score and band
3. Evidence and contradicting evidence for every hypothesis
4. Timeline
5. Suggested investigation in priority order
6. Coverage gaps and warnings

Do not describe a hypothesis as confirmed unless the evidence says so. Do not execute mutations or rollbacks; the diagnosis and its suggested investigations are read-only.`, project, project, environment, service, region)

	return &mcp.GetPromptResult{
		Description: fmt.Sprintf("Production incident diagnosis for project %q", project),
		Messages: []mcp.PromptMessage{
			mcp.NewPromptMessage(mcp.RoleUser, mcp.NewTextContent(instructions)),
		},
	}, nil
}
