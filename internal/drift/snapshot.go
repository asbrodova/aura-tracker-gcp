package drift

import "context"

// Collector returns a safe, normalized-at-the-port configuration snapshot for
// one project and component. The engine performs cross-environment
// normalization and comparison afterward.
type Collector interface {
	SupportedComponents() []string
	Collect(context.Context, CollectionRequest) (CollectionResult, error)
}

type CollectionRequest struct {
	ProjectID     string
	Component     string
	ResourceNames []string
	Locations     []string
	Namespaces    []string
}

type CollectionResult struct {
	Resources []Resource
	Partial   bool
	Warnings  []string
}

// Resource is internal so configuration fields that should never be returned
// directly remain behind the comparison boundary.
type Resource struct {
	Component    string
	ResourceType string
	Name         string
	Location     string
	Qualifier    string
	Config       map[string]any
}

func (r Resource) identity() string {
	return r.ResourceType + "\x00" + r.Name
}

func (r Resource) exactIdentity() string {
	return r.identity() + "\x00" + r.Location + "\x00" + r.Qualifier
}
