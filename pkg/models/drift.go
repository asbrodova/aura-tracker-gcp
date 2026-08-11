package models

// CompareEnvironmentsRequest compares the current effective configuration of
// two configured GCP environments. EnvironmentA and EnvironmentB are neutral:
// neither environment is treated as authoritative.
type CompareEnvironmentsRequest struct {
	EnvironmentA     string   `json:"environment_a"`
	EnvironmentB     string   `json:"environment_b"`
	Components       []string `json:"components,omitempty"`
	ResourceNames    []string `json:"resource_names,omitempty"`
	Locations        []string `json:"locations,omitempty"`
	Namespaces       []string `json:"namespaces,omitempty"`
	DetailLevel      string   `json:"detail_level,omitempty"`
	IncludeUnchanged bool     `json:"include_unchanged,omitempty"`
	MaxChanges       int      `json:"max_changes,omitempty"`
}

// DriftSummary contains direction-neutral comparison totals.
type DriftSummary struct {
	ResourcesCompared    int                     `json:"resources_compared"`
	EquivalentResources  int                     `json:"equivalent_resources"`
	DifferentResources   int                     `json:"different_resources"`
	ResourcesOnlyIn      []DriftEnvironmentCount `json:"resources_only_in"`
	UnknownDueToCoverage int                     `json:"unknown_due_to_coverage"`
	FieldDifferences     int                     `json:"field_differences"`
	OnlyInEnvironmentA   int                     `json:"-"`
	OnlyInEnvironmentB   int                     `json:"-"`
}

// DriftEnvironmentCount makes directional totals explicit without calling
// either side a baseline or target.
type DriftEnvironmentCount struct {
	Environment string `json:"environment"`
	Resources   int    `json:"resources"`
}

// DriftEnvironmentValue associates a safe configuration value with its exact
// environment name. Project-alias middleware replaces configured IDs before
// the response reaches the client.
type DriftEnvironmentValue struct {
	Environment string `json:"environment"`
	Value       any    `json:"value,omitempty"`
	Present     bool   `json:"present"`
}

// DriftFieldDifference is one effective configuration difference.
type DriftFieldDifference struct {
	Path       string                  `json:"path"`
	ChangeType string                  `json:"change_type"`
	Category   string                  `json:"category"`
	Importance string                  `json:"importance"`
	Summary    string                  `json:"summary"`
	Values     []DriftEnvironmentValue `json:"values_by_environment"`
}

// ResourceDrift is the comparison result for one logical resource.
type ResourceDrift struct {
	Component        string                 `json:"component"`
	ResourceType     string                 `json:"resource_type"`
	Name             string                 `json:"name"`
	Location         string                 `json:"location,omitempty"`
	Qualifier        string                 `json:"qualifier,omitempty"`
	Status           string                 `json:"status"`
	MissingIn        string                 `json:"missing_in,omitempty"`
	PresentIn        string                 `json:"present_in,omitempty"`
	Summary          string                 `json:"summary"`
	FieldDifferences []DriftFieldDifference `json:"field_differences,omitempty"`
}

// DriftCoverage reports collection completeness independently for each
// component and environment. A collection error is never treated as an empty
// environment.
type DriftCoverage struct {
	Component   string `json:"component"`
	Environment string `json:"environment"`
	Status      string `json:"status"` // complete, partial, error, skipped
	Resources   int    `json:"resources"`
	Message     string `json:"message,omitempty"`
}

// CompareEnvironmentsResponse is deterministic apart from its timestamps and
// comparison ID. The result and coverage status are separate so partial data
// can never be reported as clean parity.
type CompareEnvironmentsResponse struct {
	ComparisonID      string          `json:"comparison_id"`
	GeneratedAt       string          `json:"generated_at"`
	EngineVersion     string          `json:"engine_version"`
	NormalizerVersion string          `json:"normalizer_version"`
	EnvironmentA      string          `json:"environment_a"`
	EnvironmentB      string          `json:"environment_b"`
	Components        []string        `json:"components"`
	Result            string          `json:"result"`          // parity, differences_found, no_differences_observed, no_comparable_resources
	CoverageStatus    string          `json:"coverage_status"` // complete or partial
	SummaryText       string          `json:"summary_text"`
	Summary           DriftSummary    `json:"summary"`
	Highlights        []ResourceDrift `json:"highlights"`
	Resources         []ResourceDrift `json:"resources"`
	Coverage          []DriftCoverage `json:"coverage"`
	Warnings          []string        `json:"warnings,omitempty"`
	Truncated         bool            `json:"truncated"`
}
