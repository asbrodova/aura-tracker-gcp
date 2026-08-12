package gcp

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/asbrodova/aura-tracker-gcp/internal/gcp/resolution"
	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

const (
	archGraphCacheTTL = 5 * time.Minute
	batch1Concurrency = 20
	batch2Concurrency = 6
)

func (a *gcpAdapter) ExportArchitectureGraph(ctx context.Context, req models.ExportArchitectureGraphRequest) (models.ServerlessGraph, error) {
	if err := validateArchitectureGraphRequest(req); err != nil {
		return models.ServerlessGraph{}, err
	}
	if req.EnableFlowLogInference {
		return models.ServerlessGraph{}, fmt.Errorf("archgraph: flow-log inference is not implemented")
	}
	cacheKey := archGraphCacheKey(req)
	if cached, ok := a.graphCache.get(cacheKey); ok {
		return applyArchitectureGraphView(cached, req), nil
	}
	value, err, _ := a.graphFlight.Do(cacheKey, func() (any, error) {
		if cached, ok := a.graphCache.get(cacheKey); ok {
			return cached, nil
		}
		return a.collectArchitectureGraph(ctx, req)
	})
	if err != nil {
		return models.ServerlessGraph{}, err
	}
	return applyArchitectureGraphView(value.(models.ServerlessGraph), req), nil
}

func validateArchitectureGraphRequest(req models.ExportArchitectureGraphRequest) error {
	if !costProjectIDRE.MatchString(req.ProjectID) {
		return fmt.Errorf("archgraph: invalid project ID")
	}
	if len(req.Regions) > 100 {
		return fmt.Errorf("archgraph: at most 100 regions may be requested")
	}
	for _, region := range req.Regions {
		if region == "" || len(region) > 63 || strings.ContainsAny(region, `/\\`) {
			return fmt.Errorf("archgraph: invalid region %q", region)
		}
	}
	if req.MaxDepth < 0 || req.MaxDepth > 100 {
		return fmt.Errorf("archgraph: max_depth must be between 0 and 100")
	}
	if req.MaxNodes < 0 || req.MaxNodes > 10000 {
		return fmt.Errorf("archgraph: max_nodes must be between 0 and 10000")
	}
	if req.LookbackHours < 0 || req.LookbackHours > 720 {
		return fmt.Errorf("archgraph: lookback_hours must be between 0 and 720")
	}
	return nil
}

func (a *gcpAdapter) collectArchitectureGraph(ctx context.Context, req models.ExportArchitectureGraphRequest) (models.ServerlessGraph, error) {
	outerCtx, cancel := context.WithTimeout(ctx, a.graphTimeout)
	defer cancel()

	lookbackHours := req.LookbackHours
	if lookbackHours <= 0 {
		lookbackHours = 168
	}

	var (
		mu       sync.Mutex
		allNodes = map[string]models.GraphNode{}
		errs     []models.ToolError

		// For the resolution engine
		subscriptions []models.SubscriptionSummary
		triggers      []models.TriggerSummary
		schedulerJobs []models.SchedulerJobSummary
		topics        []models.TopicSummary
		workloads     []models.GKEWorkloadSummary
		k8sServices   []models.GKEServiceSummary
		ingresses     []models.GKEIngressSummary
		meshEdges     []models.GKEMeshEdge
		traceEdges    []models.TraceDependencyEdge
	)

	addNode := func(n models.GraphNode) {
		mu.Lock()
		allNodes[n.ID] = n
		mu.Unlock()
	}
	collectErr := func(api string, err error) {
		if err == nil {
			return
		}
		mu.Lock()
		errs = append(errs, models.ToolError{FailingAPI: api, Message: err.Error()})
		mu.Unlock()
	}
	collectToolErrors := func(toolErrors []models.ToolError) {
		if len(toolErrors) == 0 {
			return
		}
		mu.Lock()
		errs = append(errs, toolErrors...)
		mu.Unlock()
	}

	region := ""
	if len(req.Regions) == 1 {
		region = req.Regions[0]
	}

	// ────────────────────────────────────────────────────────────────────
	// Batch 1 — Core listing (all resources, parallel)
	// ────────────────────────────────────────────────────────────────────
	b1, b1ctx := errgroup.WithContext(outerCtx)
	b1.SetLimit(batch1Concurrency)

	// Phase 1 listers
	b1.Go(func() error {
		r, err := a.ListServices(b1ctx, models.ListServicesRequest{ProjectID: req.ProjectID, Region: region})
		if err != nil {
			collectErr("run.services.list", err)
			return nil
		}
		for _, s := range r.Services {
			node := models.GraphNode{
				ID:   graphURN("run", s.Region, req.ProjectID, models.KindCloudRunService, s.Name),
				Kind: models.KindCloudRunService, Name: s.Name,
				Region: s.Region, ProjectID: req.ProjectID, URL: s.URL, Labels: s.Labels,
			}
			addNode(node)
		}
		return nil
	})
	b1.Go(func() error {
		r, err := a.ListJobs(b1ctx, models.ListJobsRequest{ProjectID: req.ProjectID, Region: region})
		if err != nil {
			collectErr("run.jobs.list", err)
			return nil
		}
		for _, j := range r.Jobs {
			addNode(models.GraphNode{
				ID:   graphURN("run", j.Region, req.ProjectID, models.KindCloudRunJob, j.Name),
				Kind: models.KindCloudRunJob, Name: j.Name,
				Region: j.Region, ProjectID: req.ProjectID, Labels: j.Labels,
			})
		}
		return nil
	})
	b1.Go(func() error {
		r, err := a.ListFunctions(b1ctx, models.ListFunctionsRequest{ProjectID: req.ProjectID, Region: region, Generation: "both"})
		if err != nil {
			collectErr("cloudfunctions.list", err)
			return nil
		}
		for _, f := range r.Functions {
			kind := models.KindCloudFunctionV1
			if f.Generation == 2 {
				kind = models.KindCloudFunctionV2
			}
			addNode(models.GraphNode{
				ID:   graphURN("cloudfunctions", f.Region, req.ProjectID, kind, f.Name),
				Kind: kind, Name: f.Name,
				Region: f.Region, ProjectID: req.ProjectID, URL: f.URL,
				Generation: f.Generation, Labels: f.Labels,
			})
		}
		return nil
	})
	b1.Go(func() error {
		r, err := a.ListTriggers(b1ctx, models.ListTriggersRequest{ProjectID: req.ProjectID, Region: region})
		if err != nil {
			collectErr("eventarc.triggers.list", err)
			return nil
		}
		collectToolErrors(r.Errors)
		for _, t := range r.Triggers {
			addNode(models.GraphNode{
				ID:   graphURN("eventarc", t.Region, req.ProjectID, models.KindEventarcTrigger, t.Name),
				Kind: models.KindEventarcTrigger, Name: t.Name,
				Region: t.Region, ProjectID: req.ProjectID, Labels: t.Labels,
				ServiceAccount: t.ServiceAccount,
			})
		}
		mu.Lock()
		triggers = r.Triggers
		mu.Unlock()
		return nil
	})
	b1.Go(func() error {
		r, err := a.ListSchedulerJobs(b1ctx, models.ListSchedulerJobsRequest{ProjectID: req.ProjectID, Region: region})
		if err != nil {
			collectErr("cloudscheduler.jobs.list", err)
			return nil
		}
		collectToolErrors(r.Errors)
		for _, j := range r.Jobs {
			addNode(models.GraphNode{
				ID:   graphURN("scheduler", j.Region, req.ProjectID, models.KindSchedulerJob, j.Name),
				Kind: models.KindSchedulerJob, Name: j.Name,
				Region: j.Region, ProjectID: req.ProjectID,
			})
		}
		mu.Lock()
		schedulerJobs = r.Jobs
		mu.Unlock()
		return nil
	})
	b1.Go(func() error {
		r, err := a.ListWorkflows(b1ctx, models.ListWorkflowsRequest{ProjectID: req.ProjectID, Region: region})
		if err != nil {
			collectErr("workflows.list", err)
			return nil
		}
		collectToolErrors(r.Errors)
		for _, w := range r.Workflows {
			addNode(models.GraphNode{
				ID:   graphURN("workflows", w.Region, req.ProjectID, models.KindWorkflow, w.Name),
				Kind: models.KindWorkflow, Name: w.Name,
				Region: w.Region, ProjectID: req.ProjectID, Labels: w.Labels,
			})
		}
		return nil
	})
	b1.Go(func() error {
		r, err := a.ListTaskQueues(b1ctx, models.ListTaskQueuesRequest{ProjectID: req.ProjectID, Region: region})
		if err != nil {
			collectErr("tasks.queues.list", err)
			return nil
		}
		collectToolErrors(r.Errors)
		for _, q := range r.Queues {
			addNode(models.GraphNode{
				ID:   graphURN("tasks", q.Region, req.ProjectID, models.KindTasksQueue, q.Name),
				Kind: models.KindTasksQueue, Name: q.Name,
				Region: q.Region, ProjectID: req.ProjectID,
			})
		}
		return nil
	})
	b1.Go(func() error {
		r, err := a.ListSecrets(b1ctx, models.ListSecretsRequest{ProjectID: req.ProjectID})
		if err != nil {
			collectErr("secretmanager.secrets.list", err)
			return nil
		}
		for _, s := range r.Secrets {
			addNode(models.GraphNode{
				ID:   graphURN("secretmanager", "-", req.ProjectID, models.KindSecret, s.Name),
				Kind: models.KindSecret, Name: s.Name, ProjectID: req.ProjectID, Labels: s.Labels,
			})
		}
		return nil
	})
	b1.Go(func() error {
		r, err := a.ListSubscriptions(b1ctx, models.ListSubscriptionsRequest{ProjectID: req.ProjectID})
		if err != nil {
			collectErr("pubsub.subscriptions.list", err)
			return nil
		}
		for _, s := range r.Subscriptions {
			addNode(models.GraphNode{
				ID:   graphURN("pubsub", "-", req.ProjectID, models.KindPubSubSubscription, s.Name),
				Kind: models.KindPubSubSubscription, Name: s.Name, ProjectID: req.ProjectID,
			})
		}
		mu.Lock()
		subscriptions = r.Subscriptions
		mu.Unlock()
		return nil
	})
	b1.Go(func() error {
		r, err := a.ListVPCConnectors(b1ctx, models.ListVPCConnectorsRequest{ProjectID: req.ProjectID, Region: region})
		if err != nil {
			collectErr("vpcaccess.connectors.list", err)
			return nil
		}
		collectToolErrors(r.Errors)
		for _, c := range r.Connectors {
			addNode(models.GraphNode{
				ID:   graphURN("vpcaccess", c.Region, req.ProjectID, models.KindVPCConnector, c.Name),
				Kind: models.KindVPCConnector, Name: c.Name,
				Region: c.Region, ProjectID: req.ProjectID, Network: c.Network,
			})
		}
		return nil
	})
	b1.Go(func() error {
		r, err := a.ListSQLInstances(b1ctx, models.ListSQLInstancesRequest{ProjectID: req.ProjectID})
		if err != nil {
			collectErr("sqladmin.instances.list", err)
			return nil
		}
		for _, s := range r.Instances {
			addNode(models.GraphNode{
				ID:   graphURN("sqladmin", s.Region, req.ProjectID, models.KindCloudSQLInstance, s.Name),
				Kind: models.KindCloudSQLInstance, Name: s.Name,
				Region: s.Region, ProjectID: req.ProjectID, Labels: s.Labels,
			})
		}
		return nil
	})
	b1.Go(func() error {
		r, err := a.ListTopics(b1ctx, models.ListTopicsRequest{ProjectID: req.ProjectID})
		if err != nil {
			collectErr("pubsub.topics.list", err)
			return nil
		}
		for _, t := range r.Topics {
			addNode(models.GraphNode{
				ID:   graphURN("pubsub", "-", req.ProjectID, models.KindPubSubTopic, t.Name),
				Kind: models.KindPubSubTopic, Name: t.Name, ProjectID: req.ProjectID, Labels: t.Labels,
			})
		}
		mu.Lock()
		topics = r.Topics
		mu.Unlock()
		return nil
	})

	// Phase 2 listers
	b1.Go(func() error {
		r, err := a.ListSpannerInstances(b1ctx, models.ListSpannerInstancesRequest{ProjectID: req.ProjectID})
		if err != nil {
			collectErr("spanner.instances.list", err)
			return nil
		}
		for _, s := range r.Instances {
			addNode(models.GraphNode{
				ID:   graphURN("spanner", "-", req.ProjectID, models.KindSpannerInstance, s.Name),
				Kind: models.KindSpannerInstance, Name: s.Name, ProjectID: req.ProjectID,
			})
		}
		return nil
	})
	b1.Go(func() error {
		r, err := a.ListAlloyDBClusters(b1ctx, models.ListAlloyDBClustersRequest{ProjectID: req.ProjectID})
		if err != nil {
			collectErr("alloydb.clusters.list", err)
			return nil
		}
		for _, c := range r.Clusters {
			addNode(models.GraphNode{
				ID:   graphURN("alloydb", c.Location, req.ProjectID, models.KindAlloyDBCluster, c.Name),
				Kind: models.KindAlloyDBCluster, Name: c.Name,
				Region: c.Location, ProjectID: req.ProjectID,
			})
		}
		return nil
	})
	b1.Go(func() error {
		r, err := a.ListFirestoreDatabases(b1ctx, models.ListFirestoreDatabasesRequest{ProjectID: req.ProjectID})
		if err != nil {
			collectErr("firestore.databases.list", err)
			return nil
		}
		for _, d := range r.Databases {
			addNode(models.GraphNode{
				ID:   graphURN("firestore", d.LocationID, req.ProjectID, models.KindFirestoreDatabase, d.Name),
				Kind: models.KindFirestoreDatabase, Name: d.Name,
				Region: d.LocationID, ProjectID: req.ProjectID,
			})
		}
		return nil
	})
	b1.Go(func() error {
		r, err := a.ListMemorystoreInstances(b1ctx, models.ListMemorystoreInstancesRequest{ProjectID: req.ProjectID})
		if err != nil {
			collectErr("redis.instances.list", err)
			return nil
		}
		for _, m := range r.Instances {
			addNode(models.GraphNode{
				ID:   graphURN("redis", m.LocationID, req.ProjectID, models.KindMemorystoreInstance, m.Name),
				Kind: models.KindMemorystoreInstance, Name: m.Name,
				Region: m.LocationID, ProjectID: req.ProjectID,
			})
		}
		return nil
	})
	b1.Go(func() error {
		r, err := a.ListArtifactRegistryRepos(b1ctx, models.ListArtifactRegistryReposRequest{ProjectID: req.ProjectID})
		if err != nil {
			collectErr("artifactregistry.repositories.list", err)
			return nil
		}
		for _, repo := range r.Repositories {
			addNode(models.GraphNode{
				ID:   graphURN("artifactregistry", repo.Location, req.ProjectID, models.KindArtifactRegistryRepo, repo.Name),
				Kind: models.KindArtifactRegistryRepo, Name: repo.Name,
				Region: repo.Location, ProjectID: req.ProjectID,
			})
		}
		return nil
	})
	b1.Go(func() error {
		r, err := a.ListCloudBuildTriggers(b1ctx, models.ListCloudBuildTriggersRequest{ProjectID: req.ProjectID})
		if err != nil {
			collectErr("cloudbuild.triggers.list", err)
			return nil
		}
		for _, t := range r.Triggers {
			addNode(models.GraphNode{
				ID:   graphURN("cloudbuild", "-", req.ProjectID, models.KindCloudBuildTrigger, t.Name),
				Kind: models.KindCloudBuildTrigger, Name: t.Name, ProjectID: req.ProjectID,
			})
		}
		return nil
	})
	b1.Go(func() error {
		r, err := a.ListServiceDirectoryNamespaces(b1ctx, models.ListServiceDirectoryNamespacesRequest{ProjectID: req.ProjectID})
		if err != nil {
			collectErr("servicedirectory.namespaces.list", err)
			return nil
		}
		for _, ns := range r.Namespaces {
			addNode(models.GraphNode{
				ID:   graphURN("servicedirectory", ns.Location, req.ProjectID, models.KindServiceDirectoryNamespace, ns.Name),
				Kind: models.KindServiceDirectoryNamespace, Name: ns.Name,
				Region: ns.Location, ProjectID: req.ProjectID,
			})
		}
		return nil
	})
	b1.Go(func() error {
		r, err := a.ListLoadBalancers(b1ctx, models.ListLoadBalancersRequest{ProjectID: req.ProjectID})
		if err != nil {
			collectErr("compute.forwardingRules.list", err)
			return nil
		}
		for _, lb := range r.LoadBalancers {
			addNode(models.GraphNode{
				ID:   graphURN("compute", lb.Region, req.ProjectID, models.KindComputeLB, lb.Name),
				Kind: models.KindComputeLB, Name: lb.Name,
				Region: lb.Region, ProjectID: req.ProjectID,
			})
		}
		return nil
	})
	b1.Go(func() error {
		r, err := a.ListURLMaps(b1ctx, models.ListURLMapsRequest{ProjectID: req.ProjectID})
		if err != nil {
			collectErr("compute.urlMaps.list", err)
			return nil
		}
		for _, u := range r.URLMaps {
			addNode(models.GraphNode{
				ID:   graphURN("compute", u.Region, req.ProjectID, models.KindComputeURLMap, u.Name),
				Kind: models.KindComputeURLMap, Name: u.Name,
				Region: u.Region, ProjectID: req.ProjectID,
			})
		}
		return nil
	})
	b1.Go(func() error {
		r, err := a.ListNEGs(b1ctx, models.ListNEGsRequest{ProjectID: req.ProjectID})
		if err != nil {
			collectErr("compute.networkEndpointGroups.list", err)
			return nil
		}
		for _, n := range r.NEGs {
			node := models.GraphNode{
				ID:   graphURN("compute", n.Region, req.ProjectID, models.KindComputeNEG, n.Name),
				Kind: models.KindComputeNEG, Name: n.Name,
				Region: n.Region, ProjectID: req.ProjectID,
			}
			if n.Network != "" {
				node.Network = n.Network
			}
			addNode(node)
		}
		return nil
	})
	b1.Go(func() error {
		r, err := a.ListAPIGateways(b1ctx, models.ListAPIGatewaysRequest{ProjectID: req.ProjectID})
		if err != nil {
			collectErr("apigateway.gateways.list", err)
			return nil
		}
		for _, gw := range r.Gateways {
			addNode(models.GraphNode{
				ID:   graphURN("apigateway", gw.Location, req.ProjectID, models.KindAPIGateway, gw.Name),
				Kind: models.KindAPIGateway, Name: gw.Name,
				Region: gw.Location, ProjectID: req.ProjectID,
			})
		}
		return nil
	})
	b1.Go(func() error {
		r, err := a.ListVPCNetworks(b1ctx, models.ListVPCNetworksRequest{ProjectID: req.ProjectID})
		if err != nil {
			collectErr("compute.networks.list", err)
			return nil
		}
		for _, n := range r.Networks {
			addNode(models.GraphNode{
				ID:   graphURN("compute", "-", req.ProjectID, models.KindVPCNetwork, n.Name),
				Kind: models.KindVPCNetwork, Name: n.Name, ProjectID: req.ProjectID,
				Network: n.SelfLink,
			})
		}
		return nil
	})
	b1.Go(func() error {
		r, err := a.ListVPCSubnets(b1ctx, models.ListVPCSubnetsRequest{ProjectID: req.ProjectID})
		if err != nil {
			collectErr("compute.subnetworks.list", err)
			return nil
		}
		for _, s := range r.Subnets {
			addNode(models.GraphNode{
				ID:   graphURN("compute", s.Region, req.ProjectID, models.KindVPCSubnet, s.Name),
				Kind: models.KindVPCSubnet, Name: s.Name,
				Region: s.Region, ProjectID: req.ProjectID,
				Network: s.Network,
			})
		}
		return nil
	})
	b1.Go(func() error {
		r, err := a.ListPSCEndpoints(b1ctx, models.ListPSCEndpointsRequest{ProjectID: req.ProjectID})
		if err != nil {
			collectErr("compute.forwardingRules.psc.list", err)
			return nil
		}
		for _, p := range r.Endpoints {
			node := models.GraphNode{
				ID:   graphURN("compute", p.Region, req.ProjectID, models.KindPSCEndpoint, p.Name),
				Kind: models.KindPSCEndpoint, Name: p.Name,
				Region: p.Region, ProjectID: req.ProjectID,
			}
			if p.Target != "" {
				node.Attributes = map[string]string{"target_service": p.Target}
			}
			addNode(node)
		}
		return nil
	})
	b1.Go(func() error {
		r, err := a.ListServiceAccounts(b1ctx, models.ListServiceAccountsRequest{ProjectID: req.ProjectID, PageSize: maxInventoryPageSize})
		if err != nil {
			collectErr("iam.serviceAccounts.list", err)
			return nil
		}
		for _, sa := range r.ServiceAccounts {
			addNode(models.GraphNode{
				ID:   graphURN("iam", "-", req.ProjectID, models.KindIAMServiceAccount, sa.Email),
				Kind: models.KindIAMServiceAccount, Name: sa.Email, ProjectID: req.ProjectID,
			})
		}
		if r.Truncated {
			collectErr("iam.serviceAccounts.list", fmt.Errorf("result truncated at %d service accounts", maxInventoryPageSize))
		}
		return nil
	})
	b1.Go(func() error {
		r, err := a.ListClusters(b1ctx, models.ListClustersRequest{ProjectID: req.ProjectID, Location: "-"})
		if err != nil {
			collectErr("container.clusters.list", err)
			return nil
		}
		for _, c := range r.Clusters {
			addNode(models.GraphNode{
				ID:   graphURN("container", c.Location, req.ProjectID, models.KindGKECluster, c.Name),
				Kind: models.KindGKECluster, Name: c.Name,
				Region: c.Location, ProjectID: req.ProjectID, Labels: c.ResourceLabels,
			})
		}
		return nil
	})

	if err := b1.Wait(); err != nil {
		return models.ServerlessGraph{}, fmt.Errorf("archgraph: batch1: %w", err)
	}

	// ────────────────────────────────────────────────────────────────────
	// Batch 2 — Per-cluster GKE workload listing
	// ────────────────────────────────────────────────────────────────────
	var clusters []models.GraphNode
	mu.Lock()
	for _, n := range allNodes {
		if n.Kind == models.KindGKECluster {
			clusters = append(clusters, n)
		}
	}
	mu.Unlock()

	if len(clusters) > 0 {
		b2, b2ctx := errgroup.WithContext(outerCtx)
		b2.SetLimit(batch2Concurrency)

		for _, cl := range clusters {
			cl := cl
			b2.Go(func() error {
				wr, err := a.ListGKEWorkloads(b2ctx, models.ListGKEWorkloadsRequest{
					ProjectID: req.ProjectID, Location: cl.Region, ClusterName: cl.Name,
				})
				if err != nil {
					collectErr("container.workloads.list."+cl.Name, err)
					return nil
				}
				collectToolErrors(wr.Errors)
				annotated := make([]models.GKEWorkloadSummary, 0, len(wr.Workloads))
				for _, w := range wr.Workloads {
					nodeID := graphURN("k8s", cl.Region, req.ProjectID, w.Kind, cl.Name+"/"+w.Namespace+"/"+w.Name)
					w.GraphNodeID = nodeID
					w.ClusterName = cl.Name
					w.ClusterLocation = cl.Region
					annotated = append(annotated, w)
					addNode(models.GraphNode{
						ID:   nodeID,
						Kind: w.Kind, Name: w.Name,
						Region: cl.Region, ProjectID: req.ProjectID,
						Namespace: w.Namespace, ClusterName: cl.Name,
						Image: w.Image, Replicas: w.Replicas,
						ServiceAccount: w.ServiceAccount, Labels: w.Labels,
					})
				}
				mu.Lock()
				workloads = append(workloads, annotated...)
				mu.Unlock()
				return nil
			})
			b2.Go(func() error {
				sr, err := a.ListGKEServices(b2ctx, models.ListGKEServicesRequest{
					ProjectID: req.ProjectID, Location: cl.Region, ClusterName: cl.Name,
				})
				if err != nil {
					collectErr("container.k8sServices.list."+cl.Name, err)
					return nil
				}
				collectToolErrors(sr.Errors)
				annotated := make([]models.GKEServiceSummary, 0, len(sr.Services))
				for _, svc := range sr.Services {
					nodeID := graphURN("k8s", cl.Region, req.ProjectID, models.KindGKEService, cl.Name+"/"+svc.Namespace+"/"+svc.Name)
					svc.GraphNodeID = nodeID
					svc.ClusterName = cl.Name
					svc.ClusterLocation = cl.Region
					annotated = append(annotated, svc)
					addNode(models.GraphNode{
						ID:   nodeID,
						Kind: models.KindGKEService, Name: svc.Name,
						Region: cl.Region, ProjectID: req.ProjectID,
						Namespace: svc.Namespace, ClusterName: cl.Name, Labels: svc.Labels,
					})
				}
				mu.Lock()
				k8sServices = append(k8sServices, annotated...)
				mu.Unlock()
				return nil
			})
			b2.Go(func() error {
				ir, err := a.ListGKEIngresses(b2ctx, models.ListGKEIngressesRequest{
					ProjectID: req.ProjectID, Location: cl.Region, ClusterName: cl.Name,
				})
				if err != nil {
					collectErr("container.ingresses.list."+cl.Name, err)
					return nil
				}
				collectToolErrors(ir.Errors)
				annotated := make([]models.GKEIngressSummary, 0, len(ir.Ingresses))
				for _, ing := range ir.Ingresses {
					kind := architectureIngressKind(ing.Kind)
					nodeID := graphURN("k8s", cl.Region, req.ProjectID, kind, cl.Name+"/"+ing.Namespace+"/"+ing.Name)
					ing.GraphNodeID = nodeID
					ing.ClusterName = cl.Name
					ing.ClusterLocation = cl.Region
					annotated = append(annotated, ing)
					addNode(models.GraphNode{
						ID:   nodeID,
						Kind: kind, Name: ing.Name,
						Region: cl.Region, ProjectID: req.ProjectID,
						Namespace: ing.Namespace, ClusterName: cl.Name, Labels: ing.Labels,
					})
				}
				mu.Lock()
				ingresses = append(ingresses, annotated...)
				mu.Unlock()
				return nil
			})
		}
		if err := b2.Wait(); err != nil {
			return models.ServerlessGraph{}, fmt.Errorf("archgraph: batch2: %w", err)
		}
	}

	// ────────────────────────────────────────────────────────────────────
	// Batch 3 — Observability signals
	// ────────────────────────────────────────────────────────────────────
	b3, b3ctx := errgroup.WithContext(outerCtx)
	b3.SetLimit(batch2Concurrency)

	b3.Go(func() error {
		r, err := a.ListTraceDependencyEdges(b3ctx, models.ListTraceDependencyEdgesRequest{
			ProjectID:     req.ProjectID,
			LookbackHours: lookbackHours,
		})
		if err != nil {
			collectErr("cloudtrace.traces.list", err)
			return nil
		}
		mu.Lock()
		traceEdges = r.Edges
		mu.Unlock()
		return nil
	})

	for _, cl := range clusters {
		cl := cl
		b3.Go(func() error {
			r, err := a.GetGKEMeshTopology(b3ctx, models.GetGKEMeshTopologyRequest{
				ProjectID:     req.ProjectID,
				Location:      cl.Region,
				ClusterName:   cl.Name,
				LookbackHours: lookbackHours,
			})
			if err != nil {
				collectErr("mesh.topology."+cl.Name, err)
				return nil
			}
			annotated := make([]models.GKEMeshEdge, len(r.Edges))
			for i, edge := range r.Edges {
				edge.ClusterName = cl.Name
				edge.ClusterLocation = cl.Region
				annotated[i] = edge
			}
			mu.Lock()
			meshEdges = append(meshEdges, annotated...)
			mu.Unlock()
			return nil
		})
	}

	if err := b3.Wait(); err != nil {
		return models.ServerlessGraph{}, fmt.Errorf("archgraph: batch3: %w", err)
	}

	// ────────────────────────────────────────────────────────────────────
	// Resolution engine
	// ────────────────────────────────────────────────────────────────────
	mu.Lock()
	nodeSlice := make([]models.GraphNode, 0, len(allNodes))
	for _, n := range allNodes {
		nodeSlice = append(nodeSlice, n)
	}
	mu.Unlock()
	sort.Slice(nodeSlice, func(i, j int) bool { return nodeSlice[i].ID < nodeSlice[j].ID })

	resOut := resolution.Resolve(models.ResolutionInput{
		Nodes:           nodeSlice,
		Workloads:       workloads,
		K8sServices:     k8sServices,
		Ingresses:       ingresses,
		Subscriptions:   subscriptions,
		Triggers:        triggers,
		SchedulerJobs:   schedulerJobs,
		Topics:          topics,
		MeshEdges:       meshEdges,
		TraceEdges:      traceEdges,
		IncludeExternal: true,
	})

	// Append minted external endpoint nodes.
	for _, n := range resOut.MintedNodes {
		nodeSlice = append(nodeSlice, n)
	}

	// ────────────────────────────────────────────────────────────────────
	// Build groups
	// ────────────────────────────────────────────────────────────────────
	groups := buildArchGroups(req.ProjectID, nodeSlice)
	sort.Slice(errs, func(i, j int) bool {
		if errs[i].FailingAPI != errs[j].FailingAPI {
			return errs[i].FailingAPI < errs[j].FailingAPI
		}
		return errs[i].Message < errs[j].Message
	})

	graph := models.ServerlessGraph{
		Nodes:         nodeSlice,
		Edges:         resOut.Edges,
		Groups:        groups,
		Errors:        errs,
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339),
		ProjectID:     req.ProjectID,
		LookbackHours: lookbackHours,
		ToolVersion:   "phase2",
	}
	if len(req.Regions) > 0 {
		graph.RegionsScanned = req.Regions
	}

	a.graphCache.set(archGraphCacheKey(req), graph)
	return graph, nil
}

func architectureIngressKind(kind string) string {
	if kind == "HTTPRoute" || kind == models.KindGKEGateway {
		return models.KindGKEGateway
	}
	return models.KindGKEIngress
}

func buildArchGroups(projectID string, nodes []models.GraphNode) []models.GraphGroup {
	projectGroup := models.GraphGroup{
		ID: "urn:gcp:group:project:" + projectID, Kind: models.GroupKindProject,
		Label: projectID,
	}
	regionMembers := map[string][]string{}
	networkMembers := map[string][]string{}
	clusterMembers := map[string][]string{}
	nsMembers := map[string][]string{}

	for _, n := range nodes {
		projectGroup.Members = append(projectGroup.Members, n.ID)
		if n.Region != "" && n.Region != "-" {
			regionMembers[n.Region] = append(regionMembers[n.Region], n.ID)
		}
		if n.Network != "" {
			networkMembers[n.Network] = append(networkMembers[n.Network], n.ID)
		}
		if n.ClusterName != "" {
			clusterKey := n.Region + "/" + n.ClusterName
			clusterMembers[clusterKey] = append(clusterMembers[clusterKey], n.ID)
			if n.Namespace != "" {
				nsKey := clusterKey + "/" + n.Namespace
				nsMembers[nsKey] = append(nsMembers[nsKey], n.ID)
			}
		}
	}

	groups := []models.GraphGroup{projectGroup}
	for r, members := range regionMembers {
		groups = append(groups, models.GraphGroup{
			ID: "urn:gcp:group:region:" + r, Kind: models.GroupKindRegion,
			Label: r, Members: members,
		})
	}
	for net, members := range networkMembers {
		groups = append(groups, models.GraphGroup{
			ID: "urn:gcp:group:network:" + net, Kind: models.GroupKindNetwork,
			Label: net, Members: members,
		})
	}
	for cl, members := range clusterMembers {
		groups = append(groups, models.GraphGroup{
			ID: "urn:gcp:group:cluster:" + cl, Kind: models.GroupKindCluster,
			Label: cl, Members: members,
		})
	}
	for ns, members := range nsMembers {
		groups = append(groups, models.GraphGroup{
			ID: "urn:gcp:group:namespace:" + ns, Kind: models.GroupKindNamespace,
			Label: ns, Members: members,
		})
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i].ID < groups[j].ID })
	return groups
}

func archGraphCacheKey(req models.ExportArchitectureGraphRequest) string {
	regions := make([]string, len(req.Regions))
	copy(regions, req.Regions)
	sort.Strings(regions)
	lookback := req.LookbackHours
	if lookback <= 0 {
		lookback = 168
	}
	key := fmt.Sprintf("%s:lookback=%d", req.ProjectID, lookback)
	for _, r := range regions {
		key += ":" + r
	}
	return "archgraph:" + key
}
