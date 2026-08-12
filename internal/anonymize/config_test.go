package anonymize

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeAnonymizeConfig(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "anonymize.yaml")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadConfigRejectsUnknownFieldsAndModes(t *testing.T) {
	for name, contents := range map[string]string{
		"unknown field": "enabled: true\nunknown_option: true\n",
		"unknown mode":  "enabled: true\nmode: remote\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("ANONYMIZE_CONFIG_PATH", writeAnonymizeConfig(t, contents))
			t.Setenv("ANONYMIZE_ENABLED", "")
			if _, err := LoadConfig(); err == nil {
				t.Fatalf("invalid config accepted: %s", contents)
			}
		})
	}
}

func TestLoadConfigUsesStrictBooleanEnvironmentOverride(t *testing.T) {
	t.Setenv("ANONYMIZE_CONFIG_PATH", "")
	t.Setenv("ANONYMIZE_ENABLED", "yes")
	if _, err := LoadConfig(); err == nil || !strings.Contains(err.Error(), "true") {
		t.Fatalf("ambiguous boolean error = %v", err)
	}

	t.Setenv("ANONYMIZE_ENABLED", "true")
	cfg, err := LoadConfig()
	if err != nil || !cfg.Enabled || cfg.Mode != ModeLocal {
		t.Fatalf("strict true override = %+v, %v", cfg, err)
	}
}
