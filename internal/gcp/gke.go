package gcp

import (
	"context"
	"fmt"

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

// ScaleDeployment scales a GKE node pool to the requested node count.
// Note: scaling individual Kubernetes Deployments requires k8s.io/client-go
// (see CLAUDE.md Phase 2). This implementation uses the GKE management API
// to resize a node pool, which is the most common infrastructure-level scaling.
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
	np, err := a.clusterMgr.GetNodePool(ctx, &containerpb.GetNodePoolRequest{Name: npName})
	if err != nil {
		return models.ScaleDeploymentResponse{}, wrapGCPError("gke.ScaleDeployment.get", err)
	}

	currentCount := np.InitialNodeCount
	if req.ExpectedCount != nil && currentCount != *req.ExpectedCount {
		return models.ScaleDeploymentResponse{}, &ports.ConfirmationRequiredError{
			Op:      "gke.ScaleDeployment",
			Message: fmt.Sprintf("node pool size changed from %d to %d after preview; run dry_run=true again before confirming", *req.ExpectedCount, currentCount),
		}
	}
	if currentCount == req.NodeCount {
		return models.ScaleDeploymentResponse{
			DryRun:         req.DryRun,
			NodePoolName:   req.NodePoolName,
			PreviousCount:  currentCount,
			RequestedCount: req.NodeCount,
			NoChangeNeeded: true,
			Description:    fmt.Sprintf("node pool %q already has %d nodes — no change needed", req.NodePoolName, req.NodeCount),
		}, nil
	}
	if req.DryRun {
		return models.ScaleDeploymentResponse{
			DryRun:         true,
			NodePoolName:   req.NodePoolName,
			PreviousCount:  currentCount,
			RequestedCount: req.NodeCount,
			Description: fmt.Sprintf(
				"DRY RUN: would resize node pool %q in cluster %q from %d to %d nodes",
				req.NodePoolName, req.ClusterName, currentCount, req.NodeCount,
			),
		}, nil
	}

	op, err := a.clusterMgr.SetNodePoolSize(ctx, &containerpb.SetNodePoolSizeRequest{
		Name:      npName,
		NodeCount: req.NodeCount,
	})
	if err != nil {
		return models.ScaleDeploymentResponse{}, wrapGCPError("gke.ScaleDeployment.resize", err)
	}

	return models.ScaleDeploymentResponse{
		DryRun:         false,
		NodePoolName:   req.NodePoolName,
		PreviousCount:  currentCount,
		RequestedCount: req.NodeCount,
		NoChangeNeeded: false,
		OperationName:  op.Name,
		Description: fmt.Sprintf(
			"resizing node pool %q from %d to %d nodes — operation submitted",
			req.NodePoolName, currentCount, req.NodeCount,
		),
	}, nil
}
