// Package environments defines the configured GCP environments exposed through MCP.
// Real project IDs stay inside the server; aliases are the display names returned to clients.
package environments

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

var (
	aliasPattern          = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	projectIDPattern      = regexp.MustCompile(`^[a-z][a-z0-9-]{4,28}[a-z0-9]$`)
	ErrUnknownEnvironment = errors.New("unknown environment")
)

// Environment maps one configured GCP project to its optional public alias.
type Environment struct {
	ProjectID string
	Alias     string
	Default   bool
}

// DisplayName returns the value safe to expose through MCP. An unaliased
// single-project configuration intentionally exposes its project ID.
func (e Environment) DisplayName() string {
	if e.Alias != "" {
		return e.Alias
	}
	return e.ProjectID
}

// Registry validates and resolves the server's configured environments.
// It is immutable after construction and safe for concurrent use.
type Registry struct {
	environments []Environment
	byAlias      map[string]int
	byProjectID  map[string]int
	defaultIndex int
}

// NewRegistry validates environments and creates a resolver.
func NewRegistry(input []Environment) (*Registry, error) {
	if len(input) == 0 {
		return nil, errors.New("at least one environment is required")
	}

	r := &Registry{
		environments: make([]Environment, len(input)),
		byAlias:      make(map[string]int, len(input)),
		byProjectID:  make(map[string]int, len(input)),
		defaultIndex: -1,
	}
	defaultCount := 0
	for i, candidate := range input {
		candidate.ProjectID = strings.TrimSpace(candidate.ProjectID)
		candidate.Alias = strings.TrimSpace(candidate.Alias)
		if candidate.ProjectID == "" {
			return nil, fmt.Errorf("environment %d: project_id is required", i+1)
		}
		if !projectIDPattern.MatchString(candidate.ProjectID) {
			return nil, fmt.Errorf("environment %d: invalid GCP project_id %q", i+1, candidate.ProjectID)
		}
		if _, exists := r.byProjectID[candidate.ProjectID]; exists {
			return nil, fmt.Errorf("environment %d: duplicate project_id", i+1)
		}
		if len(input) > 1 && candidate.Alias == "" {
			return nil, fmt.Errorf("environment %d: alias is required when multiple environments are configured", i+1)
		}
		if candidate.Alias != "" {
			if !aliasPattern.MatchString(candidate.Alias) {
				return nil, fmt.Errorf("environment %d: alias must contain only letters, digits, '.', '_' or '-'", i+1)
			}
			canonical := strings.ToLower(candidate.Alias)
			if _, exists := r.byAlias[canonical]; exists {
				return nil, fmt.Errorf("environment %d: duplicate alias (aliases are case-insensitive)", i+1)
			}
			r.byAlias[canonical] = i
		}
		if candidate.Default {
			defaultCount++
			r.defaultIndex = i
		}
		r.byProjectID[candidate.ProjectID] = i
		r.environments[i] = candidate
	}

	for i, environment := range r.environments {
		if environment.Alias == "" {
			continue
		}
		for _, other := range r.environments {
			if strings.EqualFold(environment.Alias, other.ProjectID) {
				return nil, fmt.Errorf("environment %d: alias must not equal a configured project_id", i+1)
			}
		}
	}

	if len(r.environments) == 1 {
		r.defaultIndex = 0
		r.environments[0].Default = true
	} else if defaultCount != 1 {
		return nil, fmt.Errorf("multiple environments require exactly one default; found %d", defaultCount)
	}
	return r, nil
}

// Environments returns a copy in configuration order.
func (r *Registry) Environments() []Environment {
	if r == nil {
		return nil
	}
	out := make([]Environment, len(r.environments))
	copy(out, r.environments)
	return out
}

// Default returns the environment selected when the caller omits a selector.
func (r *Registry) Default() Environment {
	return r.environments[r.defaultIndex]
}

// Resolve accepts an alias (case-insensitively), a configured project ID, or
// an empty string for the default environment.
func (r *Registry) Resolve(selector string) (Environment, error) {
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return r.Default(), nil
	}
	if index, ok := r.byAlias[strings.ToLower(selector)]; ok {
		return r.environments[index], nil
	}
	if index, ok := r.byProjectID[selector]; ok {
		return r.environments[index], nil
	}
	return Environment{}, ErrUnknownEnvironment
}

// DisplayName returns the alias for an aliased project and the project ID for
// an unaliased project. Unknown IDs are returned unchanged.
func (r *Registry) DisplayName(projectID string) string {
	if index, ok := r.byProjectID[projectID]; ok {
		return r.environments[index].DisplayName()
	}
	return projectID
}

// DisplayNames returns configured public names with the default first.
func (r *Registry) DisplayNames() []string {
	defaultName := r.Default().DisplayName()
	names := make([]string, 0, len(r.environments))
	for _, environment := range r.environments {
		if environment.DisplayName() != defaultName {
			names = append(names, environment.DisplayName())
		}
	}
	sort.Strings(names)
	return append([]string{defaultName}, names...)
}

// SelectorDescription returns schema-safe guidance without exposing aliased project IDs.
func (r *Registry) SelectorDescription() string {
	names := r.DisplayNames()
	if len(names) == 1 && r.Default().Alias == "" {
		return fmt.Sprintf("Configured GCP project ID. Omit to use the server default (%s).", names[0])
	}
	return fmt.Sprintf(
		"Configured environment alias or project ID. Aliases are case-insensitive. Omit to use %s. Available aliases: %s.",
		r.Default().DisplayName(), strings.Join(names, ", "),
	)
}

// ReplacementMap returns only aliased project IDs. Unaliased projects remain
// visible by design.
func (r *Registry) ReplacementMap() map[string]string {
	replacements := make(map[string]string)
	for _, environment := range r.environments {
		if environment.Alias != "" {
			replacements[environment.ProjectID] = environment.Alias
		}
	}
	return replacements
}
