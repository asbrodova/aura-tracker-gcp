package gcp

import (
	"context"
	"strings"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

func (a *gcpAdapter) ListArtifactRegistryRepos(ctx context.Context, req models.ListArtifactRegistryReposRequest) (models.ListArtifactRegistryReposResponse, error) {
	if err := a.rateWait(ctx, "artifactregistry.ListRepos"); err != nil {
		return models.ListArtifactRegistryReposResponse{}, err
	}
	if a.artifactRegistrySvc == nil {
		return models.ListArtifactRegistryReposResponse{Repositories: []models.ArtifactRegistryRepoSummary{}}, nil
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	loc := req.Location
	if loc == "" {
		loc = "-"
	}
	parent := "projects/" + req.ProjectID + "/locations/" + loc

	resp, err := a.artifactRegistrySvc.Projects.Locations.Repositories.List(parent).Context(ctx).Do()
	if err != nil {
		return models.ListArtifactRegistryReposResponse{}, wrapGCPError("artifactregistry.ListRepos", err)
	}

	repos := make([]models.ArtifactRegistryRepoSummary, 0, len(resp.Repositories))
	for _, r := range resp.Repositories {
		repos = append(repos, models.ArtifactRegistryRepoSummary{
			Name:        r.Name,
			Format:      r.Format,
			Location:    locationFromName(r.Name),
			Description: r.Description,
			CreateTime:  r.CreateTime,
			SizeBytes:   r.SizeBytes,
		})
	}
	return models.ListArtifactRegistryReposResponse{Repositories: repos}, nil
}

func (a *gcpAdapter) ListArtifactRegistryImages(ctx context.Context, req models.ListArtifactRegistryImagesRequest) (models.ListArtifactRegistryImagesResponse, error) {
	if err := a.rateWait(ctx, "artifactregistry.ListImages"); err != nil {
		return models.ListArtifactRegistryImagesResponse{}, err
	}
	if a.artifactRegistrySvc == nil {
		return models.ListArtifactRegistryImagesResponse{Images: []models.ArtifactRegistryImageSummary{}}, nil
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	parent := "projects/" + req.ProjectID + "/locations/" + req.Location + "/repositories/" + req.Repository

	resp, err := a.artifactRegistrySvc.Projects.Locations.Repositories.DockerImages.List(parent).Context(ctx).Do()
	if err != nil {
		return models.ListArtifactRegistryImagesResponse{}, wrapGCPError("artifactregistry.ListImages", err)
	}

	images := make([]models.ArtifactRegistryImageSummary, 0, len(resp.DockerImages))
	for _, img := range resp.DockerImages {
		images = append(images, models.ArtifactRegistryImageSummary{
			Name:       img.Name,
			URI:        img.Uri,
			Tags:       img.Tags,
			BuildTime:  img.BuildTime,
			UploadTime: img.UploadTime,
			SizeBytes:  img.ImageSizeBytes,
		})
	}
	return models.ListArtifactRegistryImagesResponse{Images: images}, nil
}

func (a *gcpAdapter) ListCloudBuildTriggers(ctx context.Context, req models.ListCloudBuildTriggersRequest) (models.ListCloudBuildTriggersResponse, error) {
	if err := a.rateWait(ctx, "cloudbuild.ListTriggers"); err != nil {
		return models.ListCloudBuildTriggersResponse{}, err
	}
	if a.cloudBuildSvc == nil {
		return models.ListCloudBuildTriggersResponse{Triggers: []models.CloudBuildTriggerSummary{}}, nil
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	region := req.Region
	if region == "" {
		region = "-"
	}
	parent := "projects/" + req.ProjectID + "/locations/" + region

	resp, err := a.cloudBuildSvc.Projects.Locations.Triggers.List(parent).Context(ctx).Do()
	if err != nil {
		return models.ListCloudBuildTriggersResponse{}, wrapGCPError("cloudbuild.ListTriggers", err)
	}

	triggers := make([]models.CloudBuildTriggerSummary, 0, len(resp.Triggers))
	for _, t := range resp.Triggers {
		eventType := ""
		switch {
		case t.Github != nil:
			eventType = "github"
		case t.PubsubConfig != nil:
			eventType = "pubsub"
		case t.WebhookConfig != nil:
			eventType = "webhook"
		case t.SourceToBuild != nil:
			eventType = "manual"
		}
		triggers = append(triggers, models.CloudBuildTriggerSummary{
			ID:          t.Id,
			Name:        t.Name,
			Description: t.Description,
			EventType:   eventType,
			Disabled:    t.Disabled,
			Tags:        t.Tags,
			CreateTime:  t.CreateTime,
			Filename:    t.Filename,
		})
	}
	return models.ListCloudBuildTriggersResponse{Triggers: triggers}, nil
}

func (a *gcpAdapter) ListServiceDirectoryNamespaces(ctx context.Context, req models.ListServiceDirectoryNamespacesRequest) (models.ListServiceDirectoryNamespacesResponse, error) {
	if err := a.rateWait(ctx, "servicedirectory.ListNamespaces"); err != nil {
		return models.ListServiceDirectoryNamespacesResponse{}, err
	}
	if a.serviceDirectorySvc == nil {
		return models.ListServiceDirectoryNamespacesResponse{Namespaces: []models.ServiceDirectoryNamespaceSummary{}}, nil
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	loc := req.Location
	if loc == "" {
		loc = "-"
	}
	parent := "projects/" + req.ProjectID + "/locations/" + loc

	resp, err := a.serviceDirectorySvc.Projects.Locations.Namespaces.List(parent).Context(ctx).Do()
	if err != nil {
		return models.ListServiceDirectoryNamespacesResponse{}, wrapGCPError("servicedirectory.ListNamespaces", err)
	}

	namespaces := make([]models.ServiceDirectoryNamespaceSummary, 0, len(resp.Namespaces))
	for _, ns := range resp.Namespaces {
		svcResp, err := a.serviceDirectorySvc.Projects.Locations.Namespaces.Services.List(ns.Name).Context(ctx).Do()
		if err != nil {
			namespaces = append(namespaces, models.ServiceDirectoryNamespaceSummary{
				Name:     ns.Name,
				Location: locationFromName(ns.Name),
			})
			continue
		}
		svcs := make([]models.ServiceDirectoryService, 0, len(svcResp.Services))
		for _, s := range svcResp.Services {
			svcs = append(svcs, models.ServiceDirectoryService{Name: s.Name})
		}
		namespaces = append(namespaces, models.ServiceDirectoryNamespaceSummary{
			Name:     ns.Name,
			Location: locationFromName(ns.Name),
			Services: svcs,
		})
	}
	return models.ListServiceDirectoryNamespacesResponse{Namespaces: namespaces}, nil
}

// locationFromName extracts the location segment from a resource name of the form
// projects/{p}/locations/{loc}/...
func locationFromName(name string) string {
	parts := strings.Split(name, "/")
	for i, p := range parts {
		if p == "locations" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}
