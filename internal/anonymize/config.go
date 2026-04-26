package anonymize

import (
	"bytes"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Mode selects which scrubbing backend is active.
type Mode string

const (
	ModeLocal Mode = "local" // regex-only, no GCP call
	ModeDLP   Mode = "dlp"   // GCP DLP API only (Phase 2)
	ModeBoth  Mode = "both"  // local first, then DLP (Phase 2)
)

// Config is the top-level schema loaded from the YAML config file.
type Config struct {
	// Enabled is the master switch. Overridden by ANONYMIZE_ENABLED env var.
	Enabled bool `yaml:"enabled"`
	// Mode selects the backend. Default: "local".
	Mode Mode `yaml:"mode"`
	// AuditOnly makes Scrub return an AuditReport instead of masking.
	AuditOnly bool `yaml:"audit_only"`
	// Patterns are custom rules appended after the built-in set.
	Patterns []PatternConfig `yaml:"patterns"`
	// JSONKeyWhitelist lists exact JSON key names whose values are never masked.
	JSONKeyWhitelist []string `yaml:"json_key_whitelist"`
	// DLP section is only used when Mode is "dlp" or "both".
	DLP DLPConfig `yaml:"dlp"`
}

// PatternConfig defines one user-supplied regex masking rule.
type PatternConfig struct {
	Name string `yaml:"name"`
	// Regex is a Go regexp pattern.
	Regex string `yaml:"regex"`
	// ReplacementTemplate is the replacement string. The literal "${INDEX}"
	// is replaced with an incrementing integer, producing tokens like [EMAIL_1].
	// If empty, defaults to "[<NAME>_${INDEX}]".
	ReplacementTemplate string `yaml:"replacement_template"`
}

// DLPConfig holds GCP DLP backend settings (Phase 2).
type DLPConfig struct {
	// ProjectID to bill DLP requests against. Defaults to GCP_PROJECT_ID.
	ProjectID string `yaml:"project_id"`
	// InfoTypes is the list of DLP infoType detector names.
	InfoTypes []string `yaml:"info_types"`
}

// DefaultConfig returns a safe default: disabled, local mode.
func DefaultConfig() Config {
	return Config{
		Enabled: false,
		Mode:    ModeLocal,
	}
}

// LoadConfig reads the YAML file at ANONYMIZE_CONFIG_PATH (if set), then
// overrides Enabled from ANONYMIZE_ENABLED (if set). Returns DefaultConfig
// when no path is configured.
func LoadConfig() (Config, error) {
	cfg := DefaultConfig()

	path := os.Getenv("ANONYMIZE_CONFIG_PATH")
	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return cfg, fmt.Errorf("anonymize: read config %q: %w", path, err)
		}
		dec := yaml.NewDecoder(bytes.NewReader(data))
			dec.KnownFields(true)
			if err := dec.Decode(&cfg); err != nil {
				return cfg, fmt.Errorf("anonymize: parse config %q: %w", path, err)
			}
	}

	// Env var takes precedence over YAML field.
	if v := os.Getenv("ANONYMIZE_ENABLED"); v != "" {
		cfg.Enabled = v == "1" || v == "true" || v == "yes"
	}

	return cfg, nil
}
