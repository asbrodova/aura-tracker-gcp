package gcp

import (
	"context"
	"fmt"
	"strings"

	containerpb "cloud.google.com/go/container/apiv1/containerpb"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
	"github.com/asbrodova/aura-tracker-gcp/ports"
)

func (a *gcpAdapter) ListClusters(ctx context.Context, req models.ListClustersRequest) (models.ListClustersResponse, error) {
	if err := a.rateWait(ctx, "gke.ListClusters"); err != nil {
		return models.ListClustersResponse{}, err
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	parent := fmt.Sprintf("projects/%s/locations/%s", req.ProjectID, req.Location)
	if a.clusterMgr == nil {
		return models.ListClustersResponse{}, errClientNotInitialized("gke.ListClusters", clientClusterMgr)
	}
	resp, err := a.clusterMgr.ListClusters(ctx, &containerpb.ListClustersRequest{Parent: parent})
	if err != nil {
		return models.ListClustersResponse{}, wrapGCPError("gke.ListClusters", err)
	}

	result := models.ListClustersResponse{
		Clusters: make([]models.ClusterSummary, 0, len(resp.Clusters)),
	}
	for _, c := range resp.Clusters {
		result.Clusters = append(result.Clusters, models.ClusterSummary{
			Name:           c.Name,
			Location:       c.Location,
			Status:         c.Status.String(),
			NodeCount:      c.CurrentNodeCount,
			K8sVersion:     c.CurrentMasterVersion,
			ResourceLabels: c.ResourceLabels,
		})
	}
	return result, nil
}

func (a *gcpAdapter) GetClusterDetails(ctx context.Context, req models.GetClusterDetailsRequest) (models.ClusterDetails, error) {
	if err := a.rateWait(ctx, "gke.GetClusterDetails"); err != nil {
		return models.ClusterDetails{}, err
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	name := fmt.Sprintf("projects/%s/locations/%s/clusters/%s", req.ProjectID, req.Location, req.ClusterName)
	if a.clusterMgr == nil {
		return models.ClusterDetails{}, errClientNotInitialized("gke.GetClusterDetails", clientClusterMgr)
	}
	c, err := a.clusterMgr.GetCluster(ctx, &containerpb.GetClusterRequest{Name: name})
	if err != nil {
		return models.ClusterDetails{}, wrapGCPError("gke.GetClusterDetails", err)
	}

	pools := make([]models.NodePoolSummary, 0, len(c.NodePools))
	for _, np := range c.NodePools {
		pool := models.NodePoolSummary{
			Name: np.Name, NodeCount: np.InitialNodeCount, Status: np.Status.String(),
			Version: np.Version, Locations: np.Locations,
		}
		if np.Config != nil {
			pool.MachineType = np.Config.MachineType
			pool.DiskType = np.Config.DiskType
			pool.DiskSizeGB = np.Config.DiskSizeGb
			pool.ImageType = np.Config.ImageType
			pool.ServiceAccount = np.Config.ServiceAccount
			pool.Labels = np.Config.Labels
			pool.ResourceLabels = np.Config.ResourceLabels
			pool.Tags = np.Config.Tags
			pool.Preemptible = np.Config.Preemptible
			pool.Spot = np.Config.Spot
			for _, taint := range np.Config.Taints {
				pool.Taints = append(pool.Taints, fmt.Sprintf("%s=%s:%s", taint.Key, taint.Value, taint.Effect.String()))
			}
		}
		if np.Autoscaling != nil {
			pool.AutoscalingEnabled = np.Autoscaling.Enabled
			pool.MinNodeCount = np.Autoscaling.MinNodeCount
			pool.MaxNodeCount = np.Autoscaling.MaxNodeCount
		}
		if np.Management != nil {
			pool.AutoUpgrade = np.Management.AutoUpgrade
			pool.AutoRepair = np.Management.AutoRepair
		}
		if np.MaxPodsConstraint != nil {
			pool.MaxPodsPerNode = np.MaxPodsConstraint.MaxPodsPerNode
		}
		pools = append(pools, pool)
	}
	details := models.ClusterDetails{
		ClusterSummary: models.ClusterSummary{
			Name: c.Name, Location: c.Location, Status: c.Status.String(),
			NodeCount: c.CurrentNodeCount, K8sVersion: c.CurrentMasterVersion, ResourceLabels: c.ResourceLabels,
		},
		NodePools: pools, Endpoint: c.Endpoint, CreateTime: c.CreateTime,
		Description: c.Description, Network: c.Network, Subnetwork: c.Subnetwork,
		NodeLocations: c.Locations, LoggingService: c.LoggingService,
		MonitoringService: c.MonitoringService, InitialClusterVersion: c.InitialClusterVersion,
	}
	if c.NetworkConfig != nil {
		details.DataplaneProvider = c.NetworkConfig.DatapathProvider.String()
	}
	if c.ReleaseChannel != nil {
		details.ReleaseChannel = c.ReleaseChannel.Channel.String()
	}
	if c.WorkloadIdentityConfig != nil {
		details.WorkloadIdentityPool = c.WorkloadIdentityConfig.WorkloadPool
	}
	if c.PrivateClusterConfig != nil {
		details.PrivateNodes = c.PrivateClusterConfig.EnablePrivateNodes
		details.PrivateEndpoint = c.PrivateClusterConfig.EnablePrivateEndpoint
		details.MasterIPv4CIDR = c.PrivateClusterConfig.MasterIpv4CidrBlock
	}
	if c.MasterAuthorizedNetworksConfig != nil {
		for _, block := range c.MasterAuthorizedNetworksConfig.CidrBlocks {
			details.MasterAuthorizedNetworks = append(details.MasterAuthorizedNetworks, block.CidrBlock)
		}
	}
	if c.NetworkPolicy != nil {
		details.NetworkPolicyEnabled = c.NetworkPolicy.Enabled
		details.NetworkPolicyProvider = c.NetworkPolicy.Provider.String()
	}
	if c.BinaryAuthorization != nil {
		details.BinaryAuthorizationMode = c.BinaryAuthorization.EvaluationMode.String()
	}
	if c.DatabaseEncryption != nil {
		details.DatabaseEncryptionState = c.DatabaseEncryption.State.String()
	}
	if c.ShieldedNodes != nil {
		details.ShieldedNodesEnabled = c.ShieldedNodes.Enabled
	}
	if c.VerticalPodAutoscaling != nil {
		details.VerticalPodAutoscaling = c.VerticalPodAutoscaling.Enabled
	}
	if c.Autoscaling != nil {
		details.NodeAutoprovisioning = c.Autoscaling.EnableNodeAutoprovisioning
		details.AutoscalingProfile = c.Autoscaling.AutoscalingProfile.String()
	}
	if c.Autopilot != nil {
		details.AutopilotEnabled = c.Autopilot.Enabled
	}
	if c.CostManagementConfig != nil {
		details.CostManagementEnabled = c.CostManagementConfig.Enabled
	}
	if c.AddonsConfig != nil {
		if c.AddonsConfig.HttpLoadBalancing != nil {
			details.HTTPLoadBalancingDisabled = c.AddonsConfig.HttpLoadBalancing.Disabled
		}
		if c.AddonsConfig.HorizontalPodAutoscaling != nil {
			details.HorizontalAutoscalingDisabled = c.AddonsConfig.HorizontalPodAutoscaling.Disabled
		}
		if c.AddonsConfig.NetworkPolicyConfig != nil {
			details.NetworkPolicyAddonDisabled = c.AddonsConfig.NetworkPolicyConfig.Disabled
		}
		if c.AddonsConfig.DnsCacheConfig != nil {
			details.DNSCacheEnabled = c.AddonsConfig.DnsCacheConfig.Enabled
		}
	}

	return details, nil
}

// ScaleDeployment resizes a GKE node pool to the requested node count through
// the GKE management API. Despite the name it does not scale Kubernetes
// Deployments; the method name is kept for client compatibility.
func (a *gcpAdapter) ScaleDeployment(ctx context.Context, req models.ScaleDeploymentRequest) (models.ScaleDeploymentResponse, error) {
	if err := a.rateWait(ctx, "gke.ScaleDeployment"); err != nil {
		return models.ScaleDeploymentResponse{}, err
	}
	if req.NodeCount < 0 {
		return models.ScaleDeploymentResponse{}, fmt.Errorf("gke.ScaleDeployment: node_count must be non-negative")
	}

	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	// Fetch current node count for idempotency check and before/after reporting.
	npName := fmt.Sprintf(
		"projects/%s/locations/%s/clusters/%s/nodePools/%s",
		req.ProjectID, req.Location, req.ClusterName, req.NodePoolName,
	)
	if a.clusterMgr == nil {
		return models.ScaleDeploymentResponse{}, errClientNotInitialized("gke.ScaleDeployment", clientClusterMgr)
	}
	np, err := a.clusterMgr.GetNodePool(ctx, &containerpb.GetNodePoolRequest{Name: npName})
	if err != nil {
		return models.ScaleDeploymentResponse{}, wrapGCPError("gke.ScaleDeployment.get", err)
	}

	size := a.currentNodePoolSize(ctx, req.ProjectID, np)
	currentCount := size.perZone
	if req.ExpectedCount != nil && currentCount != *req.ExpectedCount {
		return models.ScaleDeploymentResponse{}, &ports.ConfirmationRequiredError{
			Op:      "gke.ScaleDeployment",
			Message: fmt.Sprintf("node pool size changed from %d to %d nodes per zone after the preview; request the change again", *req.ExpectedCount, currentCount),
		}
	}
	base := models.ScaleDeploymentResponse{
		DryRun:             req.DryRun,
		NodePoolName:       req.NodePoolName,
		PreviousCount:      currentCount,
		RequestedCount:     req.NodeCount,
		Zones:              size.zones,
		CurrentTotalNodes:  size.total,
		ZoneSizes:          size.zoneSizes,
		SizeSource:         size.source,
		AutoscalingEnabled: np.GetAutoscaling().GetEnabled(),
		Warnings:           size.warnings,
	}
	if base.AutoscalingEnabled {
		base.Warnings = append(base.Warnings, "cluster autoscaler is enabled for this node pool and may change the size again after the resize")
	}
	if currentCount == req.NodeCount && !size.uneven {
		base.NoChangeNeeded = true
		base.Description = fmt.Sprintf("node pool %q already has %d nodes per zone — no change needed", req.NodePoolName, req.NodeCount)
		return base, nil
	}
	if req.DryRun {
		base.Description = fmt.Sprintf(
			"DRY RUN: would resize node pool %q in cluster %q from %d to %d nodes per zone",
			req.NodePoolName, req.ClusterName, currentCount, req.NodeCount,
		)
		return base, nil
	}

	op, err := a.clusterMgr.SetNodePoolSize(ctx, &containerpb.SetNodePoolSizeRequest{
		Name:      npName,
		NodeCount: req.NodeCount,
	})
	if err != nil {
		return models.ScaleDeploymentResponse{}, wrapGCPError("gke.ScaleDeployment.resize", err)
	}

	base.OperationName = op.Name
	base.Description = fmt.Sprintf(
		"resizing node pool %q from %d to %d nodes per zone — operation submitted",
		req.NodePoolName, currentCount, req.NodeCount,
	)
	return base, nil
}

// nodePoolSize is a node pool's current size as far as it can be read.
type nodePoolSize struct {
	perZone   int32 // per-zone count compared with node_count (the largest zone when uneven)
	zones     int
	total     int32
	zoneSizes map[string]int32
	uneven    bool
	source    string
	warnings  []string
}

// currentNodePoolSize reads the pool's real size from its managed instance
// groups (one per zone) when the Compute client is available. Otherwise it
// falls back to InitialNodeCount, the size the pool was created with, and says
// so: autoscaling and earlier resizes make that number stale.
func (a *gcpAdapter) currentNodePoolSize(ctx context.Context, projectID string, np *containerpb.NodePool) nodePoolSize {
	zones := len(np.GetLocations())
	if zones == 0 {
		zones = 1
	}
	fallback := func(reason string) nodePoolSize {
		initial := np.GetInitialNodeCount()
		return nodePoolSize{
			perZone: initial,
			zones:   zones,
			total:   initial * int32(zones),
			source:  "initial_node_count",
			warnings: []string{
				"could not read the current node pool size (" + reason + "); counts show the size the pool was created with, which may be out of date",
			},
		}
	}
	urls := np.GetInstanceGroupUrls()
	if a.computeSvc == nil {
		return fallback("the Compute Engine client is not enabled for the selected modules")
	}
	if len(urls) == 0 {
		return fallback("the node pool reports no instance groups")
	}
	size := nodePoolSize{zones: len(urls), zoneSizes: make(map[string]int32, len(urls)), source: "instance_groups"}
	for _, url := range urls {
		project, zone, name, ok := parseInstanceGroupManagerURL(url)
		if !ok {
			return fallback("unrecognized instance group URL")
		}
		if project == "" {
			project = projectID
		}
		igm, err := a.computeSvc.InstanceGroupManagers.Get(project, zone, name).Context(ctx).Do()
		if err != nil {
			return fallback("instance group lookup failed")
		}
		count := int32(igm.TargetSize)
		size.zoneSizes[zone] += count
		size.total += count
	}
	first := true
	for _, count := range size.zoneSizes {
		if first {
			size.perZone, first = count, false
			continue
		}
		if count != size.perZone {
			size.uneven = true
		}
		if count > size.perZone {
			size.perZone = count
		}
	}
	size.zones = len(size.zoneSizes)
	if size.uneven {
		size.warnings = append(size.warnings, fmt.Sprintf("zones currently have different sizes; the resize sets every zone to the same count (largest zone today: %d)", size.perZone))
	}
	return size
}

// parseInstanceGroupManagerURL extracts project, zone and name from a node
// pool instance group URL such as
// https://www.googleapis.com/compute/v1/projects/p/zones/us-central1-a/instanceGroupManagers/gke-x-grp.
func parseInstanceGroupManagerURL(url string) (project, zone, name string, ok bool) {
	parts := strings.Split(strings.TrimSuffix(url, "/"), "/")
	for i := 0; i+1 < len(parts); i++ {
		switch parts[i] {
		case "projects":
			project = parts[i+1]
		case "zones":
			zone = parts[i+1]
		case "instanceGroupManagers", "instanceGroups":
			name = parts[i+1]
		}
	}
	return project, zone, name, zone != "" && name != ""
}
