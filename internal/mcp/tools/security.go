package tools

import (
	"context"
	"log/slog"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

type SecurityAuditor interface {
	Audit(context.Context, models.SecurityAuditRequest) (models.SecurityAuditReport, error)
}

// SecurityTools exposes the deterministic project security posture audit.
type SecurityTools struct {
	auditor SecurityAuditor
	log     *slog.Logger
}

func NewSecurityTools(auditor SecurityAuditor, log *slog.Logger) *SecurityTools {
	return &SecurityTools{auditor: auditor, log: log}
}

func (t *SecurityTools) Name() string { return "security" }

func (t *SecurityTools) GetTools() []server.ServerTool {
	return []server.ServerTool{t.ProjectSecurityAudit()}
}

func (t *SecurityTools) ProjectSecurityAudit() server.ServerTool {
	tool := mcp.NewTool("gcp_project_security_audit",
		mcp.WithDescription(
			"Perform one comprehensive, read-only security posture audit of a GCP project. "+
				"Use for requests to audit, assess, review, scan, or check a project's security, risk, or cloud posture, "+
				"including broad requests such as 'I need a project audit', 'audit my project', 'is my GCP setup secure?', "+
				"'find security risks', or 'give me a security score'. Audits inherited organization/folder/project IAM and deny policies, "+
				"service-account keys, Secret Manager, public serverless and GKE endpoints, effective classic and policy firewalls, "+
				"Kubernetes service-account and GSA Workload Identity mappings, and Google security recommendations. "+
				"Returns Critical, High, Medium, and Low findings, a 0-100 score, prioritized remediation, and explicit coverage gaps. "+
				"Do not use for a cost-only, reliability-only, or architecture-only audit.",
		),
		mcp.WithString("project_id", mcp.Description("GCP project ID. Omit to use the server default.")),
		mcp.WithBoolean("refresh",
			mcp.Description("Bypass the five-minute report cache after remediation. Default false."),
			mcp.DefaultBool(false),
		),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			Title:           "Audit GCP Project Security",
			ReadOnlyHint:    boolPtr(true),
			DestructiveHint: boolPtr(false),
			IdempotentHint:  boolPtr(true),
			OpenWorldHint:   boolPtr(true),
		}),
	)
	return server.ServerTool{Tool: tool, Handler: mcp.NewTypedToolHandler(t.projectSecurityAuditHandler)}
}

func (t *SecurityTools) projectSecurityAuditHandler(ctx context.Context, _ mcp.CallToolRequest, args models.SecurityAuditRequest) (*mcp.CallToolResult, error) {
	t.log.InfoContext(ctx, "gcp_project_security_audit", "project", args.ProjectID, "refresh", args.Refresh)
	report, err := t.auditor.Audit(ctx, args)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultStructured(report, report.SummaryMarkdown), nil
}
