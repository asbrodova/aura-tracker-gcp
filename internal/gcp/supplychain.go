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

	repos := []models.ArtifactRegistryRepoSummary{}
	for pageToken := ""; ; {
		call := a.artifactRegistrySvc.Projects.Locations.Repositories.List(parent).Context(ctx)
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}
		resp, err := call.Do()
		if err != nil {
			return models.ListArtifactRegistryReposResponse{}, wrapGCPError("artifactregistry.ListRepos", err)
		}
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
		if resp.NextPageToken == "" {
			break
		}
		pageToken = resp.NextPageToken
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

	images := []models.ArtifactRegistryImageSummary{}
	for pageToken := ""; ; {
		call := a.artifactRegistrySvc.Projects.Locations.Repositories.DockerImages.List(parent).Context(ctx)
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}
		resp, err := call.Do()
		if err != nil {
			return models.ListArtifactRegistryImagesResponse{}, wrapGCPError("artifactregistry.ListImages", err)
		}
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
		if resp.NextPageToken == "" {
			break
		}
		pageToken = resp.NextPageToken
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

	triggers := []models.CloudBuildTriggerSummary{}
	for pageToken := ""; ; {
		call := a.cloudBuildSvc.Projects.Locations.Triggers.List(parent).Context(ctx)
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}
		resp, err := call.Do()
		if err != nil {
			return models.ListCloudBuildTriggersResponse{}, wrapGCPError("cloudbuild.ListTriggers", err)
		}
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
		if resp.NextPageToken == "" {
			break
		}
		pageToken = resp.NextPageToken
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

	namespaces := []models.ServiceDirectoryNamespaceSummary{}
	for pageToken := ""; ; {
		call := a.serviceDirectorySvc.Projects.Locations.Namespaces.List(parent).Context(ctx)
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}
		resp, err := call.Do()
		if err != nil {
			return models.ListServiceDirectoryNamespacesResponse{}, wrapGCPError("servicedirectory.ListNamespaces", err)
		}
		for _, ns := range resp.Namespaces {
			svcs, err := a.listServiceDirectoryServices(ctx, ns.Name)
			if err != nil {
				namespaces = append(namespaces, models.ServiceDirectoryNamespaceSummary{Name: ns.Name, Location: locationFromName(ns.Name)})
				continue
			}
			namespaces = append(namespaces, models.ServiceDirectoryNamespaceSummary{
				Name: ns.Name, Location: locationFromName(ns.Name), Services: svcs,
			})
		}
		if resp.NextPageToken == "" {
			break
		}
		pageToken = resp.NextPageToken
	}
	return models.ListServiceDirectoryNamespacesResponse{Namespaces: namespaces}, nil
}

func (a *gcpAdapter) listServiceDirectoryServices(ctx context.Context, namespace string) ([]models.ServiceDirectoryService, error) {
	services := []models.ServiceDirectoryService{}
	for pageToken := ""; ; {
		call := a.serviceDirectorySvc.Projects.Locations.Namespaces.Services.List(namespace).Context(ctx)
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}
		resp, err := call.Do()
		if err != nil {
			return nil, err
		}
		for _, service := range resp.Services {
			services = append(services, models.ServiceDirectoryService{Name: service.Name})
		}
		if resp.NextPageToken == "" {
			return services, nil
		}
		pageToken = resp.NextPageToken
	}
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
