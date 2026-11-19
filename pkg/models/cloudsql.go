package models

// SQLInstanceSummary is a lightweight view of a Cloud SQL instance.
type SQLInstanceSummary struct {
	Name            string            `json:"name"`
	DatabaseVersion string            `json:"database_version,omitempty"`
	Region          string            `json:"region,omitempty"`
	State           string            `json:"state,omitempty"`
	Tier            string            `json:"tier,omitempty"`
	Labels          map[string]string `json:"labels,omitempty"`
	// Settings are the instance's availability, storage, backup, network,
	// maintenance and flag settings, as environment drift compares them.
	Settings *SQLInstanceSettings `json:"settings,omitempty"`
}

// SQLInstanceSettings is a safe subset of a Cloud SQL instance's settings.
// Authorized networks are summarized (count, open to the internet) rather
// than listed.
type SQLInstanceSettings struct {
	Edition                  string                `json:"edition,omitempty"`
	AvailabilityType         string                `json:"availability_type,omitempty"`
	ActivationPolicy         string                `json:"activation_policy,omitempty"`
	DataDiskType             string                `json:"data_disk_type,omitempty"`
	StorageAutoResize        *bool                 `json:"storage_auto_resize,omitempty"`
	StorageAutoResizeLimitGB int64                 `json:"storage_auto_resize_limit_gb,omitempty"`
	DeletionProtection       bool                  `json:"deletion_protection"`
	ConnectorEnforcement     string                `json:"connector_enforcement,omitempty"`
	TimeZone                 string                `json:"time_zone,omitempty"`
	QueryInsightsEnabled     bool                  `json:"query_insights_enabled,omitempty"`
	Backup                   *SQLBackupSettings    `json:"backup,omitempty"`
	Network                  *SQLNetworkSettings   `json:"network,omitempty"`
	MaintenanceWindow        *SQLMaintenanceWindow `json:"maintenance_window,omitempty"`
	DatabaseFlags            map[string]string     `json:"database_flags,omitempty"`
}

type SQLBackupSettings struct {
	Enabled                     bool   `json:"enabled"`
	PointInTimeRecovery         bool   `json:"point_in_time_recovery"`
	BinaryLogEnabled            bool   `json:"binary_log_enabled,omitempty"`
	StartTime                   string `json:"start_time,omitempty"`
	Location                    string `json:"location,omitempty"`
	TransactionLogRetentionDays int64  `json:"transaction_log_retention_days,omitempty"`
	RetainedBackups             int64  `json:"retained_backups,omitempty"`
	RetentionUnit               string `json:"retention_unit,omitempty"`
}

type SQLNetworkSettings struct {
	PublicIPEnabled        bool   `json:"public_ip_enabled"`
	PrivateNetwork         string `json:"private_network,omitempty"`
	SSLMode                string `json:"ssl_mode,omitempty"`
	RequireSSL             bool   `json:"require_ssl,omitempty"`
	AuthorizedNetworkCount int    `json:"authorized_network_count,omitempty"`
	OpenToInternet         bool   `json:"open_to_internet,omitempty"`
}

type SQLMaintenanceWindow struct {
	Day         int64  `json:"day,omitempty"`
	Hour        int64  `json:"hour,omitempty"`
	UpdateTrack string `json:"update_track,omitempty"`
}

// ListSQLInstancesRequest lists Cloud SQL instances in a project.
type ListSQLInstancesRequest struct {
	ProjectID string `json:"project_id"`
}

// ListSQLInstancesResponse holds the list result.
type ListSQLInstancesResponse struct {
	Instances []SQLInstanceSummary `json:"instances"`
}
