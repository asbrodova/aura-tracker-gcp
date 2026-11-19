package gcp

import (
	"context"

	"google.golang.org/api/sqladmin/v1"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

func (a *gcpAdapter) ListSQLInstances(ctx context.Context, req models.ListSQLInstancesRequest) (models.ListSQLInstancesResponse, error) {
	if err := a.rateWait(ctx, "cloudsql.ListSQLInstances"); err != nil {
		return models.ListSQLInstancesResponse{}, err
	}
	if a.sqlAdmin == nil {
		return models.ListSQLInstancesResponse{Instances: []models.SQLInstanceSummary{}}, nil
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	instances := []models.SQLInstanceSummary{}
	for pageToken := ""; ; {
		call := a.sqlAdmin.Instances.List(req.ProjectID).Context(ctx)
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}
		resp, err := call.Do()
		if err != nil {
			return models.ListSQLInstancesResponse{}, wrapGCPError("cloudsql.ListSQLInstances", err)
		}
		for _, inst := range resp.Items {
			tier := ""
			var labels map[string]string
			if inst.Settings != nil {
				tier = inst.Settings.Tier
				labels = inst.Settings.UserLabels
			}
			instances = append(instances, models.SQLInstanceSummary{
				Name: inst.Name, DatabaseVersion: inst.DatabaseVersion, Region: inst.Region,
				State: inst.State, Tier: tier, Labels: labels, Settings: sqlInstanceSettings(inst.Settings),
			})
		}
		if resp.NextPageToken == "" {
			break
		}
		pageToken = resp.NextPageToken
	}
	return models.ListSQLInstancesResponse{Instances: instances}, nil
}

// sqlInstanceSettings maps the instance settings environment drift compares.
// Disk size is left out because storage auto-resize changes it on its own.
func sqlInstanceSettings(settings *sqladmin.Settings) *models.SQLInstanceSettings {
	if settings == nil {
		return nil
	}
	out := &models.SQLInstanceSettings{
		Edition: settings.Edition, AvailabilityType: settings.AvailabilityType, ActivationPolicy: settings.ActivationPolicy,
		DataDiskType: settings.DataDiskType, StorageAutoResize: settings.StorageAutoResize,
		StorageAutoResizeLimitGB: settings.StorageAutoResizeLimit, DeletionProtection: settings.DeletionProtectionEnabled,
		ConnectorEnforcement: settings.ConnectorEnforcement, TimeZone: settings.TimeZone,
	}
	if insights := settings.InsightsConfig; insights != nil {
		out.QueryInsightsEnabled = insights.QueryInsightsEnabled
	}
	if backup := settings.BackupConfiguration; backup != nil {
		out.Backup = &models.SQLBackupSettings{
			Enabled: backup.Enabled, PointInTimeRecovery: backup.PointInTimeRecoveryEnabled, BinaryLogEnabled: backup.BinaryLogEnabled,
			StartTime: backup.StartTime, Location: backup.Location, TransactionLogRetentionDays: backup.TransactionLogRetentionDays,
		}
		if retention := backup.BackupRetentionSettings; retention != nil {
			out.Backup.RetainedBackups = retention.RetainedBackups
			out.Backup.RetentionUnit = retention.RetentionUnit
		}
	}
	if network := settings.IpConfiguration; network != nil {
		out.Network = &models.SQLNetworkSettings{
			PublicIPEnabled: network.Ipv4Enabled, PrivateNetwork: network.PrivateNetwork,
			SSLMode: network.SslMode, RequireSSL: network.RequireSsl, AuthorizedNetworkCount: len(network.AuthorizedNetworks),
		}
		for _, entry := range network.AuthorizedNetworks {
			if entry != nil && (entry.Value == "0.0.0.0/0" || entry.Value == "::/0") {
				out.Network.OpenToInternet = true
			}
		}
	}
	if window := settings.MaintenanceWindow; window != nil {
		out.MaintenanceWindow = &models.SQLMaintenanceWindow{Day: window.Day, Hour: window.Hour, UpdateTrack: window.UpdateTrack}
	}
	if len(settings.DatabaseFlags) > 0 {
		out.DatabaseFlags = make(map[string]string, len(settings.DatabaseFlags))
		for _, flag := range settings.DatabaseFlags {
			if flag != nil && flag.Name != "" {
				out.DatabaseFlags[flag.Name] = flag.Value
			}
		}
	}
	return out
}
