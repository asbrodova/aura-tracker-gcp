package gcp

import (
	"context"
	"fmt"

	containerpb "cloud.google.com/go/container/apiv1/containerpb"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
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
			Name:       c.Name,
			Location:   c.Location,
			Status:     c.Status.String(),
			NodeCount:  c.CurrentNodeCount,
			K8sVersion: c.CurrentMasterVersion,
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
		machineType := ""
		if np.Config != nil {
			machineType = np.Config.MachineType
		}
		pools = append(pools, models.NodePoolSummary{
			Name:        np.Name,
			MachineType: machineType,
			NodeCount:   np.InitialNodeCount,
			Status:      np.Status.String(),
		})
	}

	return models.ClusterDetails{
		ClusterSummary: models.ClusterSummary{
			Name:       c.Name,
			Location:   c.Location,
			Status:     c.Status.String(),
			NodeCount:  c.CurrentNodeCount,
			K8sVersion: c.CurrentMasterVersion,
		},
		NodePools:      pools,
		Endpoint:       c.Endpoint,
		CreateTime:     c.CreateTime,
		ResourceLabels: c.ResourceLabels,
	}, nil
}

// ScaleDeployment scales a GKE node pool to the requested node count.
// Note: scaling individual Kubernetes Deployments requires k8s.io/client-go
// (see CLAUDE.md Phase 2). This implementation uses the GKE management API
// to resize a node pool, which is the most common infrastructure-level scaling.
func (a *gcpAdapter) ScaleDeployment(ctx context.Context, req models.ScaleDeploymentRequest) (models.ScaleDeploymentResponse, error) {
	if err := a.rateWait(ctx, "gke.ScaleDeployment"); err != nil {
		return models.ScaleDeploymentResponse{}, err
	}

	if req.DryRun {
		return models.ScaleDeploymentResponse{
			DryRun:         true,
			NodePoolName:   req.NodePoolName,
			RequestedCount: req.NodeCount,
			Description: fmt.Sprintf(
				"DRY RUN: would resize node pool %q in cluster %q to %d nodes",
				req.NodePoolName, req.ClusterName, req.NodeCount,
			),
		}, nil
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
	if currentCount == req.NodeCount {
		return models.ScaleDeploymentResponse{
			DryRun:         false,
			NodePoolName:   req.NodePoolName,
			PreviousCount:  currentCount,
			RequestedCount: req.NodeCount,
			NoChangeNeeded: true,
			Description:    fmt.Sprintf("node pool %q already has %d nodes — no change needed", req.NodePoolName, req.NodeCount),
		}, nil
	}

	_, err = a.clusterMgr.SetNodePoolSize(ctx, &containerpb.SetNodePoolSizeRequest{
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
		Description: fmt.Sprintf(
			"resizing node pool %q from %d to %d nodes — operation submitted",
			req.NodePoolName, currentCount, req.NodeCount,
		),
	}, nil
}
