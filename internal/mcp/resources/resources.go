// Package resources implements MCP Resource handlers for GCP services.
// It imports ports.GCPService only — never internal/gcp — preserving hexagonal isolation.
package resources

import (
	"log/slog"

	"github.com/asbrodova/aura-tracker-gcp/ports"
)

// BigQueryResources serves resources under gcp://{project}/bigquery/...
type BigQueryResources struct {
	svc ports.GCPService
	log *slog.Logger
}

func NewBigQueryResources(svc ports.GCPService, log *slog.Logger) *BigQueryResources {
	return &BigQueryResources{svc: svc, log: log}
}

// CloudRunResources serves resources under gcp://{project}/cloudrun/...
type CloudRunResources struct {
	svc ports.GCPService
	log *slog.Logger
}

func NewCloudRunResources(svc ports.GCPService, log *slog.Logger) *CloudRunResources {
	return &CloudRunResources{svc: svc, log: log}
}

// StorageResources serves resources under gcp://{project}/storage/...
type StorageResources struct {
	svc ports.GCPService
	log *slog.Logger
}

func NewStorageResources(svc ports.GCPService, log *slog.Logger) *StorageResources {
	return &StorageResources{svc: svc, log: log}
}

// IAMResources serves resources under gcp://{project}/iam/...
type IAMResources struct {
	svc ports.GCPService
	log *slog.Logger
}

func NewIAMResources(svc ports.GCPService, log *slog.Logger) *IAMResources {
	return &IAMResources{svc: svc, log: log}
}
