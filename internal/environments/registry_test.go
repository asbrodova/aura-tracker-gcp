package environments

import (
	"errors"
	"strings"
	"testing"
)

func TestRegistryResolvesAliasesProjectIDsAndDefault(t *testing.T) {
	registry, err := NewRegistry([]Environment{
		{ProjectID: "company-dev-123", Alias: "Dev", Default: true},
		{ProjectID: "company-prod-345", Alias: "prod"},
	})
	if err != nil {
		t.Fatal(err)
	}
	for selector, want := range map[string]string{
		"":                 "company-dev-123",
		"dev":              "company-dev-123",
		"DEV":              "company-dev-123",
		"DeV":              "company-dev-123",
		"prod":             "company-prod-345",
		"company-prod-345": "company-prod-345",
	} {
		got, resolveErr := registry.Resolve(selector)
		if resolveErr != nil {
			t.Fatalf("Resolve(%q): %v", selector, resolveErr)
		}
		if got.ProjectID != want {
			t.Errorf("Resolve(%q) project = %q, want %q", selector, got.ProjectID, want)
		}
	}
	if registry.DisplayName("company-dev-123") != "Dev" {
		t.Fatal("configured alias was not preserved for display")
	}
	if replacements := registry.ReplacementMap(); replacements["company-prod-345"] != "prod" {
		t.Fatalf("replacement map = %#v", replacements)
	}
}

func TestRegistrySingleEnvironmentDefaultsAndMayBeUnaliased(t *testing.T) {
	registry, err := NewRegistry([]Environment{{ProjectID: "solo-project"}})
	if err != nil {
		t.Fatal(err)
	}
	if !registry.Default().Default || registry.Default().DisplayName() != "solo-project" {
		t.Fatalf("default = %+v", registry.Default())
	}
	if len(registry.ReplacementMap()) != 0 {
		t.Fatal("unaliased project must remain visible")
	}
}

func TestRegistryRejectsInvalidMultiEnvironmentConfiguration(t *testing.T) {
	tests := []struct {
		name  string
		items []Environment
	}{
		{"missing alias", []Environment{{ProjectID: "one", Alias: "dev", Default: true}, {ProjectID: "two"}}},
		{"missing default", []Environment{{ProjectID: "one", Alias: "dev"}, {ProjectID: "two", Alias: "prod"}}},
		{"multiple defaults", []Environment{{ProjectID: "one", Alias: "dev", Default: true}, {ProjectID: "two", Alias: "prod", Default: true}}},
		{"duplicate alias", []Environment{{ProjectID: "one", Alias: "Dev", Default: true}, {ProjectID: "two", Alias: "dEV"}}},
		{"duplicate project", []Environment{{ProjectID: "one", Alias: "dev", Default: true}, {ProjectID: "one", Alias: "prod"}}},
		{"alias project collision", []Environment{{ProjectID: "dev-project", Alias: "prod-project", Default: true}, {ProjectID: "prod-project", Alias: "prod"}}},
		{"unsafe alias", []Environment{{ProjectID: "one", Alias: "dev team", Default: true}, {ProjectID: "two", Alias: "prod"}}},
		{"invalid project ID", []Environment{{ProjectID: "UPPER_case"}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewRegistry(test.items); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestRegistryUnknownSelectorDoesNotEchoInput(t *testing.T) {
	registry, err := NewRegistry([]Environment{{ProjectID: "private-project", Alias: "dev"}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = registry.Resolve("secret-unknown-project")
	if !errors.Is(err, ErrUnknownEnvironment) {
		t.Fatalf("Resolve error = %v", err)
	}
	if strings.Contains(err.Error(), "secret-unknown-project") {
		t.Fatal("unknown selector was echoed")
	}
}
