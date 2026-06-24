// Package tools implements MCP tool handlers for each GCP domain.
// Each domain has its own file (gke.go, cloudrun.go, etc.) that defines
// a *Tools struct, a constructor, and handler methods registered via GetTools.
package tools
