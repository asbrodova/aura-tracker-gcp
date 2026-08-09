package gcp

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"golang.org/x/sync/errgroup"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

func (a *gcpAdapter) ExportServerlessGraph(ctx context.Context, req models.ExportServerlessGraphRequest) (models.ServerlessGraph, error) {
	if !costProjectIDRE.MatchString(req.ProjectID) {
		return models.ServerlessGraph{}, fmt.Errorf("serverlessgraph: invalid project ID")
	}
	if strings.ContainsAny(req.Region, `/\\`) || len(req.Region) > 63 {
		return models.ServerlessGraph{}, fmt.Errorf("serverlessgraph: invalid region %q", req.Region)
	}
	if req.MaxNodes < 0 || req.MaxNodes > 10000 {
		return models.ServerlessGraph{}, fmt.Errorf("serverlessgraph: max_nodes must be between 0 and 10000")
	}
	outerCtx, cancel := context.WithTimeout(ctx, a.graphTimeout)
	defer cancel()

	// --- Fan out all listers in parallel ---

	type listResult struct {
		services      []models.ServiceSummary
		jobs          []models.JobSummary
		functions     []models.FunctionSummary
		triggers      []models.TriggerSummary
		schedulerJobs []models.SchedulerJobSummary
		workflows     []models.WorkflowSummary
		queues        []models.TaskQueueSummary
		secrets       []models.SecretSummary
		subscriptions []models.SubscriptionSummary
		connectors    []models.VPCConnectorSummary
		sqlInstances  []models.SQLInstanceSummary
		topics        []models.TopicSummary
	}

	var res listResult
	var mu sync.Mutex
	var collectedErrors []models.ToolError

	collectErr := func(api string, err error) {
		if err == nil {
			return
		}
		mu.Lock()
		collectedErrors = append(collectedErrors, models.ToolError{
			FailingAPI: api,
			Message:    err.Error(),
			Retriable:  false,
		})
		mu.Unlock()
	}
	collectToolErrors := func(toolErrors []models.ToolError) {
		if len(toolErrors) == 0 {
			return
		}
		mu.Lock()
		collectedErrors = append(collectedErrors, toolErrors...)
		mu.Unlock()
	}

	regionFilter := req.Region
	g, gctx := errgroup.WithContext(outerCtx)

	g.Go(func() error {
		r, err := a.ListServices(gctx, models.ListServicesRequest{ProjectID: req.ProjectID, Region: regionFilter})
		if err != nil {
			collectErr("run.services.list", err)
			return nil
		}
		mu.Lock()
		res.services = r.Services
		mu.Unlock()
		return nil
	})
	g.Go(func() error {
		r, err := a.ListJobs(gctx, models.ListJobsRequest{ProjectID: req.ProjectID, Region: regionFilter})
		if err != nil {
			collectErr("run.jobs.list", err)
			return nil
		}
		mu.Lock()
		res.jobs = r.Jobs
		mu.Unlock()
		return nil
	})
	g.Go(func() error {
		r, err := a.ListFunctions(gctx, models.ListFunctionsRequest{ProjectID: req.ProjectID, Region: regionFilter, Generation: "both"})
		if err != nil {
			collectErr("cloudfunctions.list", err)
			return nil
		}
		mu.Lock()
		res.functions = r.Functions
		mu.Unlock()
		return nil
	})
	g.Go(func() error {
		r, err := a.ListTriggers(gctx, models.ListTriggersRequest{ProjectID: req.ProjectID, Region: regionFilter})
		if err != nil {
			collectErr("eventarc.triggers.list", err)
			return nil
		}
		collectToolErrors(r.Errors)
		mu.Lock()
		res.triggers = r.Triggers
		mu.Unlock()
		return nil
	})
	g.Go(func() error {
		r, err := a.ListSchedulerJobs(gctx, models.ListSchedulerJobsRequest{ProjectID: req.ProjectID, Region: regionFilter})
		if err != nil {
			collectErr("cloudscheduler.jobs.list", err)
			return nil
		}
		collectToolErrors(r.Errors)
		mu.Lock()
		res.schedulerJobs = r.Jobs
		mu.Unlock()
		return nil
	})
	g.Go(func() error {
		r, err := a.ListWorkflows(gctx, models.ListWorkflowsRequest{ProjectID: req.ProjectID, Region: regionFilter})
		if err != nil {
			collectErr("workflows.list", err)
			return nil
		}
		collectToolErrors(r.Errors)
		mu.Lock()
		res.workflows = r.Workflows
		mu.Unlock()
		return nil
	})
	g.Go(func() error {
		r, err := a.ListTaskQueues(gctx, models.ListTaskQueuesRequest{ProjectID: req.ProjectID, Region: regionFilter})
		if err != nil {
			collectErr("cloudtasks.queues.list", err)
			return nil
		}
		collectToolErrors(r.Errors)
		mu.Lock()
		res.queues = r.Queues
		mu.Unlock()
		return nil
	})
	g.Go(func() error {
		r, err := a.ListSecrets(gctx, models.ListSecretsRequest{ProjectID: req.ProjectID, IncludeReferences: req.IncludeReferences})
		if err != nil {
			collectErr("secretmanager.secrets.list", err)
			return nil
		}
		mu.Lock()
		res.secrets = r.Secrets
		mu.Unlock()
		return nil
	})
	g.Go(func() error {
		r, err := a.ListSubscriptions(gctx, models.ListSubscriptionsRequest{ProjectID: req.ProjectID})
		if err != nil {
			collectErr("pubsub.subscriptions.list", err)
			return nil
		}
		mu.Lock()
		res.subscriptions = r.Subscriptions
		mu.Unlock()
		return nil
	})
	g.Go(func() error {
		r, err := a.ListVPCConnectors(gctx, models.ListVPCConnectorsRequest{ProjectID: req.ProjectID, Region: regionFilter})
		if err != nil {
			collectErr("vpcaccess.connectors.list", err)
			return nil
		}
		collectToolErrors(r.Errors)
		mu.Lock()
		res.connectors = r.Connectors
		mu.Unlock()
		return nil
	})
	g.Go(func() error {
		r, err := a.ListSQLInstances(gctx, models.ListSQLInstancesRequest{ProjectID: req.ProjectID})
		if err != nil {
			collectErr("sqladmin.instances.list", err)
			return nil
		}
		mu.Lock()
		res.sqlInstances = r.Instances
		mu.Unlock()
		return nil
	})
	g.Go(func() error {
		r, err := a.ListTopics(gctx, models.ListTopicsRequest{ProjectID: req.ProjectID})
		if err != nil {
			collectErr("pubsub.topics.list", err)
			return nil
		}
		mu.Lock()
		res.topics = r.Topics
		mu.Unlock()
		return nil
	})

	_ = g.Wait()

	// --- Build nodes ---

	nodeByURN := make(map[string]models.GraphNode)

	addNode := func(n models.GraphNode) {
		nodeByURN[n.ID] = n
	}

	// Gen2 function wrapper URNs keyed by Cloud Run service name for de-duplication.
	gen2ServiceNames := map[string]bool{}

	for _, fn := range res.functions {
		var kind, svc string
		if fn.Generation == 2 {
			kind = "cloud_function_v2"
			svc = "functions"
			// Record bare service name so we can skip the CR service node.
			gen2ServiceNames[fn.Name] = true
		} else {
			kind = "cloud_function_v1"
			svc = "functions"
		}
		urn := graphURN(svc, fn.Region, req.ProjectID, kind, fn.Name)
		addNode(models.GraphNode{
			ID:         urn,
			Kind:       kind,
			Name:       fn.Name,
			Region:     fn.Region,
			ProjectID:  req.ProjectID,
			URL:        fn.URL,
			Generation: fn.Generation,
		})
	}

	for _, svc := range res.services {
		// Skip Gen2 function wrapper services — they appear as cloud_function_v2 nodes.
		if gen2ServiceNames[svc.Name] {
			continue
		}
		urn := graphURN("run", svc.Region, req.ProjectID, "service", svc.Name)
		addNode(models.GraphNode{
			ID:        urn,
			Kind:      "cloud_run_service",
			Name:      svc.Name,
			Region:    svc.Region,
			ProjectID: req.ProjectID,
			URL:       svc.URL,
		})
	}

	for _, job := range res.jobs {
		urn := graphURN("run", job.Region, req.ProjectID, "job", job.Name)
		addNode(models.GraphNode{
			ID:        urn,
			Kind:      "cloud_run_job",
			Name:      job.Name,
			Region:    job.Region,
			ProjectID: req.ProjectID,
			Labels:    job.Labels,
		})
	}

	for _, t := range res.triggers {
		urn := graphURN("eventarc", t.Region, req.ProjectID, "trigger", t.Name)
		addNode(models.GraphNode{
			ID:             urn,
			Kind:           "eventarc_trigger",
			Name:           t.Name,
			Region:         t.Region,
			ProjectID:      req.ProjectID,
			Labels:         t.Labels,
			ServiceAccount: t.ServiceAccount,
		})
	}

	for _, j := range res.schedulerJobs {
		urn := graphURN("scheduler", j.Region, req.ProjectID, "job", j.Name)
		addNode(models.GraphNode{
			ID:        urn,
			Kind:      "scheduler_job",
			Name:      j.Name,
			Region:    j.Region,
			ProjectID: req.ProjectID,
		})
	}

	for _, w := range res.workflows {
		urn := graphURN("workflows", w.Region, req.ProjectID, "workflow", w.Name)
		addNode(models.GraphNode{
			ID:        urn,
			Kind:      "workflow",
			Name:      w.Name,
			Region:    w.Region,
			ProjectID: req.ProjectID,
		})
	}

	for _, q := range res.queues {
		urn := graphURN("tasks", q.Region, req.ProjectID, "queue", q.Name)
		addNode(models.GraphNode{
			ID:        urn,
			Kind:      "tasks_queue",
			Name:      q.Name,
			Region:    q.Region,
			ProjectID: req.ProjectID,
		})
	}

	for _, topic := range res.topics {
		urn := graphURN("pubsub", "-", req.ProjectID, "topic", topic.Name)
		addNode(models.GraphNode{
			ID:            urn,
			Kind:          "pubsub_topic",
			Name:          topic.Name,
			ProjectID:     req.ProjectID,
			EventarcOwned: false,
		})
	}

	for _, sub := range res.subscriptions {
		urn := graphURN("pubsub", "-", req.ProjectID, "subscription", sub.Name)
		addNode(models.GraphNode{
			ID:        urn,
			Kind:      "pubsub_subscription",
			Name:      sub.Name,
			ProjectID: req.ProjectID,
		})
	}

	for _, s := range res.secrets {
		urn := graphURN("secretmanager", "-", req.ProjectID, "secret", s.Name)
		addNode(models.GraphNode{
			ID:        urn,
			Kind:      "secret",
			Name:      s.Name,
			ProjectID: req.ProjectID,
			Labels:    s.Labels,
		})
	}

	for _, c := range res.connectors {
		urn := graphURN("vpcaccess", c.Region, req.ProjectID, "connector", c.Name)
		addNode(models.GraphNode{
			ID:        urn,
			Kind:      "vpc_connector",
			Name:      c.Name,
			Region:    c.Region,
			ProjectID: req.ProjectID,
		})
	}

	for _, inst := range res.sqlInstances {
		urn := graphURN("cloudsql", "-", req.ProjectID, "instance", inst.Name)
		addNode(models.GraphNode{
			ID:        urn,
			Kind:      "cloud_sql_instance",
			Name:      inst.Name,
			ProjectID: req.ProjectID,
		})
	}

	// --- Stitch edges ---

	var edges []models.GraphEdge

	addEdge := func(src, tgt, edgeType, evidence string, confidence float64) {
		if _, ok := nodeByURN[src]; !ok {
			return
		}
		if _, ok := nodeByURN[tgt]; !ok {
			return
		}
		edges = append(edges, models.GraphEdge{
			Source:     src,
			Target:     tgt,
			Type:       edgeType,
			Evidence:   evidence,
			Confidence: confidence,
		})
	}

	// Build lookup maps.
	serviceURLtoURN := map[string]string{}
	for _, svc := range res.services {
		if svc.URL != "" {
			urn := graphURN("run", svc.Region, req.ProjectID, "service", svc.Name)
			serviceURLtoURN[strings.TrimRight(svc.URL, "/")] = urn
		}
	}
	fnURLtoURN := map[string]string{}
	for _, fn := range res.functions {
		if fn.URL != "" {
			var kind string
			if fn.Generation == 2 {
				kind = "cloud_function_v2"
			} else {
				kind = "cloud_function_v1"
			}
			urn := graphURN("functions", fn.Region, req.ProjectID, kind, fn.Name)
			fnURLtoURN[strings.TrimRight(fn.URL, "/")] = urn
		}
	}
	topicNameToURN := map[string]string{}
	for _, t := range res.topics {
		urn := graphURN("pubsub", "-", req.ProjectID, "topic", t.Name)
		topicNameToURN[t.Name] = urn
		// Also index by full resource name: projects/P/topics/NAME
		topicNameToURN[fmt.Sprintf("projects/%s/topics/%s", req.ProjectID, t.Name)] = urn
	}

	// Eventarc trigger edges.
	for _, t := range res.triggers {
		triggerURN := graphURN("eventarc", t.Region, req.ProjectID, "trigger", t.Name)

		// triggers edge: eventarc_trigger → destination
		if t.DestinationURN != "" {
			destURN := t.DestinationURN
			// Ensure destination node exists (may not if from another project).
			if _, ok := nodeByURN[destURN]; ok {
				addEdge(triggerURN, destURN, "triggers", "eventarc_destination", 0.95)
			}
		}

		// routes_to edge: eventarc_trigger → transport pubsub topic
		if t.TransportTopic != "" {
			if topicURN, ok := topicNameToURN[t.TransportTopic]; ok {
				addEdge(triggerURN, topicURN, "routes_to", "eventarc_destination", 0.9)
			}
		}
	}

	// Scheduler job edges.
	for _, j := range res.schedulerJobs {
		jobURN := graphURN("scheduler", j.Region, req.ProjectID, "job", j.Name)

		switch j.TargetKind {
		case "pubsub":
			if topicURN, ok := topicNameToURN[j.TargetRef]; ok {
				addEdge(jobURN, topicURN, "triggers", "scheduler_target", 0.95)
			}
		case "http":
			targetURL := strings.TrimRight(j.TargetRef, "/")
			if svcURN, ok := serviceURLtoURN[targetURL]; ok {
				addEdge(jobURN, svcURN, "triggers", "scheduler_target", 0.85)
			} else if fnURN, ok := fnURLtoURN[targetURL]; ok {
				addEdge(jobURN, fnURN, "triggers", "scheduler_target", 0.85)
			}
		}
	}

	// Pub/Sub subscription edges.
	for _, sub := range res.subscriptions {
		subURN := graphURN("pubsub", "-", req.ProjectID, "subscription", sub.Name)

		// subscribes_to: subscription → topic
		if sub.Topic != "" {
			if topicURN, ok := topicNameToURN[sub.Topic]; ok {
				addEdge(subURN, topicURN, "subscribes_to", "topic_push_endpoint", 0.95)
			}
		}

		// triggers: subscription → push endpoint (Cloud Run service or function)
		if sub.PushEndpoint != "" {
			ep := strings.TrimRight(sub.PushEndpoint, "/")
			// Strip path component for matching service/function base URLs.
			base := ep
			if idx := strings.Index(ep[8:], "/"); idx >= 0 { // skip "https://"
				base = ep[:8+idx]
			}
			if svcURN, ok := serviceURLtoURN[base]; ok {
				addEdge(subURN, svcURN, "triggers", "topic_push_endpoint", 0.9)
			} else if fnURN, ok := fnURLtoURN[base]; ok {
				addEdge(subURN, fnURN, "triggers", "topic_push_endpoint", 0.9)
			}
		}

		// dead_letters_to: subscription → dead letter topic
		if sub.DeadLetterTopic != "" {
			if topicURN, ok := topicNameToURN[sub.DeadLetterTopic]; ok {
				addEdge(subURN, topicURN, "dead_letters_to", "topic_push_endpoint", 0.95)
			}
		}
	}

	// reads_secret: cloud_run_service → secret (via include_references enrichment).
	if req.IncludeReferences {
		secretNameToURN := map[string]string{}
		for _, s := range res.secrets {
			urn := graphURN("secretmanager", "-", req.ProjectID, "secret", s.Name)
			secretNameToURN[s.Name] = urn
		}
		for _, s := range res.secrets {
			secretURN := graphURN("secretmanager", "-", req.ProjectID, "secret", s.Name)
			for _, svcName := range s.ReferencedBy {
				// Find the service URN by name (region unknown from secret metadata).
				for _, svc := range res.services {
					if svc.Name == svcName {
						svcURN := graphURN("run", svc.Region, req.ProjectID, "service", svcName)
						addEdge(svcURN, secretURN, "reads_secret", "env_var", 0.9)
						break
					}
				}
			}
		}
	}

	// --- Collect nodes, apply region filter and max_nodes cap ---

	nodes := make([]models.GraphNode, 0, len(nodeByURN))
	for _, n := range nodeByURN {
		if regionFilter != "" && regionFilter != "-" && n.Region != "" && n.Region != regionFilter {
			continue
		}
		nodes = append(nodes, n)
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })

	if req.MaxNodes > 0 && len(nodes) > req.MaxNodes {
		nodes = nodes[:req.MaxNodes]
		collectedErrors = append(collectedErrors, models.ToolError{
			FailingAPI: "gcp_export_serverless_graph",
			Message:    fmt.Sprintf("result truncated: max_nodes=%d reached; increase max_nodes or filter by region", req.MaxNodes),
			Retriable:  false,
		})
	}
	allowed := nodeIDSet(nodes)
	edges = filterGraphEdges(edges, allowed)
	groups := buildServerlessGroups(req.ProjectID, nodes)
	sort.Slice(collectedErrors, func(i, j int) bool {
		if collectedErrors[i].FailingAPI != collectedErrors[j].FailingAPI {
			return collectedErrors[i].FailingAPI < collectedErrors[j].FailingAPI
		}
		return collectedErrors[i].Message < collectedErrors[j].Message
	})

	return models.ServerlessGraph{
		Nodes:  nodes,
		Edges:  edges,
		Groups: groups,
		Errors: collectedErrors,
	}, nil
}

func buildServerlessGroups(projectID string, nodes []models.GraphNode) []models.GraphGroup {
	projectMembers := make([]string, 0, len(nodes))
	regionMembers := map[string][]string{}
	for _, node := range nodes {
		projectMembers = append(projectMembers, node.ID)
		region := node.Region
		if region == "" {
			region = "-"
		}
		regionMembers[region] = append(regionMembers[region], node.ID)
	}
	groups := []models.GraphGroup{{
		ID: "project:" + projectID, Kind: models.GroupKindProject,
		Label: projectID, Members: projectMembers,
	}}
	for region, members := range regionMembers {
		groups = append(groups, models.GraphGroup{
			ID: "region:" + projectID + "/" + region, Kind: models.GroupKindRegion,
			Label: region, Members: members,
		})
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i].ID < groups[j].ID })
	return groups
}

// graphURN builds a stable URN for a GCP resource.
func graphURN(service, region, projectID, kind, name string) string {
	return fmt.Sprintf("urn:gcp:%s:%s:%s:%s/%s", service, region, projectID, kind, name)
}

func allURNs(m map[string]models.GraphNode) []string {
	urns := make([]string, 0, len(m))
	for k := range m {
		urns = append(urns, k)
	}
	sort.Strings(urns)
	return urns
}
