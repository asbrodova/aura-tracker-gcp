package resolution

import (
	"crypto/sha256"
	"fmt"
	"strings"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

// gcpManagedSuffixes lists domain suffixes that are GCP-managed and should NOT
// be minted as external_endpoint nodes.
var gcpManagedSuffixes = []string{
	".googleapis.com",
	".run.app",
	".cloudfunctions.net",
	".appspot.com",
	".google.com",
	".iam.gserviceaccount.com",
	"metadata.google.internal",
}

// isGCPManaged returns true if the URL/hostname belongs to a GCP-managed domain.
func isGCPManaged(urlOrHost string) bool {
	host := extractHost(urlOrHost)
	if host == "" {
		return true // treat unknown as managed to avoid false positives
	}
	for _, suffix := range gcpManagedSuffixes {
		if strings.HasSuffix(host, suffix) || host == strings.TrimPrefix(suffix, ".") {
			return true
		}
	}
	// RFC1918 / link-local addresses are internal, not external SaaS.
	if isPrivateIP(host) {
		return true
	}
	return false
}

// mintExternalEndpoint creates a GraphNode for an external hostname.
// The node ID is deterministic: urn:gcp:external:-:{projectID}:external_endpoint/{hash}.
func mintExternalEndpoint(urlOrHost string, nodes []models.GraphNode) models.GraphNode {
	host := extractHost(urlOrHost)
	// Derive projectID from any existing node in the set.
	projectID := ""
	if len(nodes) > 0 {
		projectID = nodes[0].ProjectID
	}

	// Stable 8-char hash so the ID is deterministic.
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(host)))[:8]
	id := fmt.Sprintf("urn:gcp:external:-:%s:external_endpoint/%s", projectID, hash)

	displayName := host
	if friendly, ok := knownSaaSByDomain[host]; ok {
		displayName = friendly
	} else {
		// Try parent domain (e.g. api.stripe.com → stripe.com).
		parts := strings.SplitN(host, ".", 2)
		if len(parts) == 2 {
			if friendly, ok := knownSaaSByDomain[parts[1]]; ok {
				displayName = friendly + " (" + host + ")"
			}
		}
	}

	return models.GraphNode{
		ID:        id,
		Kind:      models.KindExternalEndpoint,
		Name:      displayName,
		ProjectID: projectID,
		Attributes: map[string]string{
			"hostname": host,
		},
	}
}

// isPrivateIP returns true for RFC1918 and link-local address strings.
func isPrivateIP(host string) bool {
	privateRanges := []string{
		"10.",
		"172.16.", "172.17.", "172.18.", "172.19.", "172.20.", "172.21.", "172.22.", "172.23.",
		"172.24.", "172.25.", "172.26.", "172.27.", "172.28.", "172.29.", "172.30.", "172.31.",
		"192.168.",
		"127.",
		"169.254.",
	}
	for _, prefix := range privateRanges {
		if strings.HasPrefix(host, prefix) {
			return true
		}
	}
	return false
}
