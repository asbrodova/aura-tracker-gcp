package drift

import (
	"context"
	"fmt"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

func (c *GCPCollector) collectPubSub(ctx context.Context, req CollectionRequest) (CollectionResult, error) {
	topics, err := c.source.ListTopics(ctx, models.ListTopicsRequest{ProjectID: req.ProjectID})
	if err != nil {
		return CollectionResult{}, err
	}
	result := CollectionResult{Resources: []Resource{}}
	for _, topic := range topics.Topics {
		if includeResource(req, topic.Name, "") {
			result.Resources = append(result.Resources, resource("pubsub", "pubsub.topic", topic.Name, "", "", topic))
		}
	}
	subscriptions, err := c.source.ListSubscriptions(ctx, models.ListSubscriptionsRequest{ProjectID: req.ProjectID})
	if err != nil {
		result.Partial = true
		result.Warnings = append(result.Warnings, "subscriptions: "+err.Error())
		return result, nil
	}
	for _, subscription := range subscriptions.Subscriptions {
		if includeResource(req, subscription.Name, "") {
			result.Resources = append(result.Resources, resource("pubsub", "pubsub.subscription", subscription.Name, "", "", subscription))
		}
	}
	return result, nil
}

func (c *GCPCollector) collectStorage(ctx context.Context, req CollectionRequest) (CollectionResult, error) {
	buckets, err := c.source.ListBuckets(ctx, models.ListBucketsRequest{ProjectID: req.ProjectID})
	if err != nil {
		return CollectionResult{}, err
	}
	result := CollectionResult{Resources: []Resource{}}
	for _, bucket := range buckets.Buckets {
		if !includeResource(req, bucket.Name, bucket.Location) {
			continue
		}
		metadata, metadataErr := c.source.GetBucketMetadata(ctx, models.GetBucketMetadataRequest{ProjectID: req.ProjectID, BucketName: bucket.Name})
		if metadataErr != nil {
			result.Partial = true
			result.Warnings = append(result.Warnings, fmt.Sprintf("bucket %s metadata: %v", bucket.Name, metadataErr))
			result.Resources = append(result.Resources, resource("storage", "storage.bucket", bucket.Name, bucket.Location, "", bucket))
			continue
		}
		result.Resources = append(result.Resources, resource("storage", "storage.bucket", bucket.Name, bucket.Location, "", metadata))
	}
	return result, nil
}

func (c *GCPCollector) collectBigQuery(ctx context.Context, req CollectionRequest) (CollectionResult, error) {
	datasets, err := c.source.ListDatasets(ctx, models.ListDatasetsRequest{ProjectID: req.ProjectID})
	if err != nil {
		return CollectionResult{}, err
	}
	result := CollectionResult{Resources: []Resource{}}
	for _, dataset := range datasets.Datasets {
		if len(req.Locations) > 0 && !containsFold(req.Locations, dataset.Location) {
			continue
		}
		if len(req.ResourceNames) == 0 || containsFold(req.ResourceNames, dataset.ID) {
			result.Resources = append(result.Resources, resource("bigquery", "bigquery.dataset", dataset.ID, dataset.Location, "", dataset))
		}
		tables, tableErr := c.source.ListTables(ctx, models.ListTablesRequest{ProjectID: req.ProjectID, DatasetID: dataset.ID})
		if tableErr != nil {
			result.Partial = true
			result.Warnings = append(result.Warnings, fmt.Sprintf("dataset %s tables: %v", dataset.ID, tableErr))
			continue
		}
		for _, table := range tables.Tables {
			if len(req.ResourceNames) > 0 && !containsFold(req.ResourceNames, table.ID) {
				continue
			}
			schema, schemaErr := c.source.GetTableSchema(ctx, models.GetTableSchemaRequest{ProjectID: req.ProjectID, DatasetID: dataset.ID, TableID: table.ID})
			configuration := map[string]any{"table": configMap(table)}
			if schemaErr != nil {
				result.Partial = true
				result.Warnings = append(result.Warnings, fmt.Sprintf("table %s.%s schema: %v", dataset.ID, table.ID, schemaErr))
			} else {
				configuration["schema"] = configMap(schema)
				if schema.Truncated {
					result.Partial = true
					result.Warnings = append(result.Warnings, fmt.Sprintf("table %s.%s schema was truncated", dataset.ID, table.ID))
				}
			}
			result.Resources = append(result.Resources, resource("bigquery", "bigquery.table", table.ID, dataset.Location, dataset.ID, configuration))
			if len(result.Resources) >= maxResourcesPerComponent {
				result.Partial = true
				result.Warnings = append(result.Warnings, fmt.Sprintf("resource limit of %d reached", maxResourcesPerComponent))
				return result, nil
			}
		}
	}
	return result, nil
}

func (c *GCPCollector) collectDatastores(ctx context.Context, req CollectionRequest) (CollectionResult, error) {
	result := CollectionResult{Resources: []Resource{}}
	spanner, err := c.source.ListSpannerInstances(ctx, models.ListSpannerInstancesRequest{ProjectID: req.ProjectID})
	if err != nil {
		result.Partial = true
		result.Warnings = append(result.Warnings, "Spanner: "+err.Error())
	} else {
		for _, value := range spanner.Instances {
			if includeResource(req, value.Name, "") {
				result.Resources = append(result.Resources, resource("datastores", "spanner.instance", value.Name, "", "", value))
			}
		}
	}
	alloy, err := c.source.ListAlloyDBClusters(ctx, models.ListAlloyDBClustersRequest{ProjectID: req.ProjectID})
	if err != nil {
		result.Partial = true
		result.Warnings = append(result.Warnings, "AlloyDB: "+err.Error())
	} else {
		for _, value := range alloy.Clusters {
			if includeResource(req, value.Name, value.Location) {
				result.Resources = append(result.Resources, resource("datastores", "alloydb.cluster", value.Name, value.Location, "", value))
			}
		}
	}
	firestore, err := c.source.ListFirestoreDatabases(ctx, models.ListFirestoreDatabasesRequest{ProjectID: req.ProjectID})
	if err != nil {
		result.Partial = true
		result.Warnings = append(result.Warnings, "Firestore: "+err.Error())
	} else {
		for _, value := range firestore.Databases {
			if includeResource(req, value.Name, value.LocationID) {
				result.Resources = append(result.Resources, resource("datastores", "firestore.database", value.Name, value.LocationID, "", value))
			}
		}
	}
	redis, err := c.source.ListMemorystoreInstances(ctx, models.ListMemorystoreInstancesRequest{ProjectID: req.ProjectID})
	if err != nil {
		result.Partial = true
		result.Warnings = append(result.Warnings, "Memorystore: "+err.Error())
	} else {
		for _, value := range redis.Instances {
			if includeResource(req, value.Name, value.LocationID) {
				result.Resources = append(result.Resources, resource("datastores", "memorystore.instance", value.Name, value.LocationID, "", value))
			}
		}
	}
	return result, nil
}

func (c *GCPCollector) collectSupplyChain(ctx context.Context, req CollectionRequest) (CollectionResult, error) {
	result := CollectionResult{Resources: []Resource{}}
	repositories, err := c.source.ListArtifactRegistryRepos(ctx, models.ListArtifactRegistryReposRequest{ProjectID: req.ProjectID})
	if err != nil {
		result.Partial = true
		result.Warnings = append(result.Warnings, "Artifact Registry: "+err.Error())
	} else {
		for _, value := range repositories.Repositories {
			if includeResource(req, value.Name, value.Location) {
				result.Resources = append(result.Resources, resource("supplychain", "artifactregistry.repository", value.Name, value.Location, "", value))
			}
		}
	}
	triggers, err := c.source.ListCloudBuildTriggers(ctx, models.ListCloudBuildTriggersRequest{ProjectID: req.ProjectID})
	if err != nil {
		result.Partial = true
		result.Warnings = append(result.Warnings, "Cloud Build triggers: "+err.Error())
	} else {
		for _, value := range triggers.Triggers {
			name := value.Name
			if name == "" {
				name = value.ID
			}
			if includeResource(req, name, "") {
				result.Resources = append(result.Resources, resource("supplychain", "cloudbuild.trigger", name, "", "", value))
			}
		}
	}
	namespaces, err := c.source.ListServiceDirectoryNamespaces(ctx, models.ListServiceDirectoryNamespacesRequest{ProjectID: req.ProjectID})
	if err != nil {
		result.Partial = true
		result.Warnings = append(result.Warnings, "Service Directory: "+err.Error())
	} else {
		for _, value := range namespaces.Namespaces {
			if includeResource(req, value.Name, value.Location) {
				result.Resources = append(result.Resources, resource("supplychain", "servicedirectory.namespace", value.Name, value.Location, "", value))
			}
		}
	}
	return result, nil
}

func (c *GCPCollector) collectMonitoring(ctx context.Context, req CollectionRequest) (CollectionResult, error) {
	result := CollectionResult{Resources: []Resource{}}
	policies, err := c.source.ListAlertPolicies(ctx, models.ListAlertPoliciesRequest{ProjectID: req.ProjectID})
	if err != nil {
		result.Partial = true
		result.Warnings = append(result.Warnings, "alert policies: "+err.Error())
	} else {
		for _, value := range policies.Policies {
			name := value.DisplayName
			if name == "" {
				name = value.Name
			}
			if includeResource(req, name, "") {
				result.Resources = append(result.Resources, resource("monitoring", "monitoring.alert_policy", name, "", "", value))
			}
		}
	}
	uptime, err := c.source.ListUptimeChecks(ctx, models.ListUptimeChecksRequest{ProjectID: req.ProjectID})
	if err != nil {
		result.Partial = true
		result.Warnings = append(result.Warnings, "uptime checks: "+err.Error())
	} else {
		for _, value := range uptime.UptimeChecks {
			name := value.DisplayName
			if name == "" {
				name = value.Name
			}
			if includeResource(req, name, "") {
				result.Resources = append(result.Resources, resource("monitoring", "monitoring.uptime_check", name, "", "", value))
			}
		}
	}
	slos, err := c.source.ListSLOs(ctx, models.ListSLOsRequest{ProjectID: req.ProjectID})
	if err != nil {
		result.Partial = true
		result.Warnings = append(result.Warnings, "SLOs: "+err.Error())
	} else {
		for _, value := range slos.SLOs {
			name := value.DisplayName
			if name == "" {
				name = value.Name
			}
			if includeResource(req, name, "") {
				result.Resources = append(result.Resources, resource("monitoring", "monitoring.slo", name, "", "", value))
			}
		}
	}
	dashboards, err := c.source.ListDashboards(ctx, models.ListDashboardsRequest{ProjectID: req.ProjectID})
	if err != nil {
		result.Partial = true
		result.Warnings = append(result.Warnings, "dashboards: "+err.Error())
	} else {
		for _, value := range dashboards.Dashboards {
			name := value.DisplayName
			if name == "" {
				name = value.Name
			}
			if includeResource(req, name, "") {
				result.Resources = append(result.Resources, resource("monitoring", "monitoring.dashboard", name, "", "", value))
			}
		}
	}
	return result, nil
}

func (c *GCPCollector) collectIAM(ctx context.Context, req CollectionRequest) (CollectionResult, error) {
	accounts, err := c.source.ListServiceAccounts(ctx, models.ListServiceAccountsRequest{ProjectID: req.ProjectID})
	if err != nil {
		return CollectionResult{}, err
	}
	result := CollectionResult{Resources: []Resource{}, Partial: true, Warnings: []string{"service-account configuration is compared; project and resource IAM bindings are not yet included"}}
	for _, value := range accounts.ServiceAccounts {
		name := value.Email
		if value.Name != "" {
			name = bareName(value.Name)
		}
		if includeResource(req, name, "") {
			result.Resources = append(result.Resources, resource("iam", "iam.service_account", name, "", "", value))
		}
	}
	return result, nil
}
