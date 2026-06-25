package config

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config holds user-level defaults read from ~/.aura-tracker.yaml.
type Config struct {
	ProjectID string `yaml:"project_id"`
}

// Load reads ~/.aura-tracker.yaml. A missing file is not an error; it returns
// a zero Config so callers always fall back to environment variables.
func Load() (Config, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Config{}, err
	}
	data, err := os.ReadFile(filepath.Join(home, ".aura-tracker.yaml"))
	if os.IsNotExist(err) {
		return Config{}, nil
	}
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	return cfg, yaml.Unmarshal(data, &cfg)
}
