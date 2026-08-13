// Package resources implements MCP Resource handlers for GCP services.
// It imports ports.GCPService only — never internal/gcp — preserving hexagonal isolation.
package resources

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/asbrodova/aura-tracker-gcp/internal/environments"
	"github.com/asbrodova/aura-tracker-gcp/ports"
)

// BigQueryResources serves resources under gcp://{project}/bigquery/...
type BigQueryResources struct {
	svc          ports.BigQueryService
	log          *slog.Logger
	environments *environments.Registry
	placeholder  string
}

func NewBigQueryResources(svc ports.BigQueryService, log *slog.Logger, registry *environments.Registry, placeholder string) *BigQueryResources {
	return &BigQueryResources{svc: svc, log: log, environments: registry, placeholder: placeholder}
}

// CloudRunResources serves resources under gcp://{project}/cloudrun/...
type CloudRunResources struct {
	svc          ports.CloudRunService
	log          *slog.Logger
	environments *environments.Registry
	placeholder  string
}

func NewCloudRunResources(svc ports.CloudRunService, log *slog.Logger, registry *environments.Registry, placeholder string) *CloudRunResources {
	return &CloudRunResources{svc: svc, log: log, environments: registry, placeholder: placeholder}
}

// StorageResources serves resources under gcp://{project}/storage/...
type StorageResources struct {
	svc          ports.StorageService
	log          *slog.Logger
	environments *environments.Registry
	placeholder  string
}

func NewStorageResources(svc ports.StorageService, log *slog.Logger, registry *environments.Registry, placeholder string) *StorageResources {
	return &StorageResources{svc: svc, log: log, environments: registry, placeholder: placeholder}
}

// IAMResources serves resources under gcp://{project}/iam/...
type IAMResources struct {
	svc          ports.IAMService
	log          *slog.Logger
	environments *environments.Registry
	placeholder  string
	permissions  []string
}

func NewIAMResources(svc ports.IAMService, log *slog.Logger, registry *environments.Registry, placeholder string, permissions []string) *IAMResources {
	return &IAMResources{svc: svc, log: log, environments: registry, placeholder: placeholder, permissions: append([]string(nil), permissions...)}
}

func resolveEnvironment(registry *environments.Registry, selector, placeholder string) (environments.Environment, error) {
	if registry == nil {
		return environments.Environment{ProjectID: selector}, nil
	}
	if placeholder != "" && selector == placeholder {
		return registry.Default(), nil
	}
	environment, err := registry.Resolve(selector)
	if err != nil {
		return environments.Environment{}, fmt.Errorf(
			"unknown environment; available environments: %s",
			strings.Join(registry.DisplayNames(), ", "),
		)
	}
	return environment, nil
}
