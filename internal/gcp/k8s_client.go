package gcp

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

// k8sClient is a minimal read-only Kubernetes API REST client.
// It authenticates using a GCP bearer token (GKE API servers accept GCP tokens).
// No k8s.io/client-go dependency — the full informer machinery is not needed
// for the read-only workload listing this tool performs.
type k8sClient struct {
	baseURL    string
	httpClient *http.Client
}

const maxK8sResponseBytes = 16 << 20

// tokenRoundTripper injects a fresh GCP bearer token on every request.
type tokenRoundTripper struct {
	base     http.RoundTripper
	tokenSrc oauth2.TokenSource
}

func (t *tokenRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	tok, err := t.tokenSrc.Token()
	if err != nil {
		return nil, fmt.Errorf("k8s: acquire token: %w", err)
	}
	req2 := req.Clone(req.Context())
	req2.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	return t.base.RoundTrip(req2)
}

// dialCluster builds a k8sClient for a GKE cluster, using Application Default
// Credentials for authentication. caCertBase64 is the base64-encoded PEM CA
// certificate from Cluster.MasterAuth.ClusterCaCertificate.
func dialCluster(ctx context.Context, endpoint, caCertBase64 string) (*k8sClient, error) {
	tokenSrc, err := google.DefaultTokenSource(ctx, "https://www.googleapis.com/auth/cloud-platform")
	if err != nil {
		return nil, fmt.Errorf("k8s: get token source: %w", err)
	}
	return dialClusterWithTokenSource(endpoint, caCertBase64, tokenSrc)
}

// dialClusterWithTokenSource builds a k8sClient with an explicit token source.
// Used by tests to inject a static token without real ADC.
func dialClusterWithTokenSource(endpoint, caCertBase64 string, tokenSrc oauth2.TokenSource) (*k8sClient, error) {
	caPEM, err := base64.StdEncoding.DecodeString(caCertBase64)
	if err != nil {
		return nil, fmt.Errorf("k8s: decode CA cert: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("k8s: no valid CA certificates found in cluster CA cert")
	}
	transport := &tokenRoundTripper{
		base: &http.Transport{
			TLSClientConfig: &tls.Config{RootCAs: pool},
		},
		tokenSrc: tokenSrc,
	}
	return &k8sClient{
		baseURL:    "https://" + strings.TrimPrefix(endpoint, "https://"),
		httpClient: &http.Client{Transport: transport},
	}, nil
}

func dialGatewayWithTokenSource(endpoint string, tokenSrc oauth2.TokenSource) *k8sClient {
	return &k8sClient{
		baseURL: strings.TrimSuffix(endpoint, "/"),
		httpClient: &http.Client{Transport: &tokenRoundTripper{
			base: http.DefaultTransport, tokenSrc: tokenSrc,
		}},
	}
}

// get performs a GET request to the K8s API and decodes the JSON response into out.
func (c *k8sClient) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("k8s: build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("k8s: GET %s: %w", path, err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("k8s: GET %s: HTTP %d", path, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxK8sResponseBytes+1))
	if err != nil {
		return fmt.Errorf("k8s: read response from %s: %w", path, err)
	}
	if len(body) > maxK8sResponseBytes {
		return fmt.Errorf("k8s: response from %s exceeded %d bytes", path, maxK8sResponseBytes)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("k8s: decode response from %s: %w", path, err)
	}
	return nil
}

// resourcePath builds the K8s API path for a namespaced or cluster-scoped list.
//
//	apiBase = "apis/apps/v1" or "api/v1" etc.
//	ns      = namespace name, or "" for all namespaces
//	resource = "deployments", "services", etc.
func resourcePath(apiBase, ns, resource string) string {
	if ns == "" {
		return "/" + apiBase + "/" + resource
	}
	return "/" + apiBase + "/namespaces/" + ns + "/" + resource
}

// listWorkloads returns workload summaries for the given namespace across all
// standard workload kinds (Deployment, StatefulSet, DaemonSet, CronJob, Job).
// kind filters to a specific kind when non-empty (e.g. "Deployment").
type workloadListKindConfig struct {
	apiBase  string
	resource string
	kindName string
}

var workloadListKinds = []workloadListKindConfig{
	{"apis/apps/v1", "deployments", "Deployment"},
	{"apis/apps/v1", "statefulsets", "StatefulSet"},
	{"apis/apps/v1", "daemonsets", "DaemonSet"},
	{"apis/batch/v1", "cronjobs", "CronJob"},
	{"apis/batch/v1", "jobs", "Job"},
}

func canonicalWorkloadKind(kind string, allowEmpty bool) (string, error) {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "":
		if allowEmpty {
			return "", nil
		}
	case "deployment", strings.ToLower(models.KindGKEDeployment):
		return "Deployment", nil
	case "statefulset", strings.ToLower(models.KindGKEStatefulSet):
		return "StatefulSet", nil
	case "daemonset", strings.ToLower(models.KindGKEDaemonSet):
		return "DaemonSet", nil
	case "cronjob", strings.ToLower(models.KindGKECronJob):
		return "CronJob", nil
	case "job", strings.ToLower(models.KindGKEJob):
		return "Job", nil
	}
	return "", fmt.Errorf("k8s: unsupported workload kind %q; expected Deployment, StatefulSet, DaemonSet, CronJob, or Job", kind)
}

func (c *k8sClient) listWorkloads(ctx context.Context, ns, kind string, pageSize int, pageToken string) ([]models.GKEWorkloadSummary, string, error) {
	canonicalKind, err := canonicalWorkloadKind(kind, true)
	if err != nil {
		return nil, "", err
	}
	if pageSize < 1 {
		return nil, "", fmt.Errorf("k8s: page_size must be positive")
	}
	startKind, continueToken, err := decodeWorkloadPageToken(pageToken)
	if err != nil {
		return nil, "", err
	}
	if startKind != "" {
		startKind, err = canonicalWorkloadKind(startKind, false)
		if err != nil {
			return nil, "", fmt.Errorf("k8s: page token references unsupported workload kind")
		}
	}
	if canonicalKind != "" && startKind != "" && canonicalKind != startKind {
		return nil, "", fmt.Errorf("k8s: page token belongs to workload kind %q", startKind)
	}
	started := startKind == ""
	var result []models.GKEWorkloadSummary
	for kindIndex, kc := range workloadListKinds {
		if canonicalKind != "" && canonicalKind != kc.kindName {
			continue
		}
		if !started {
			if !strings.EqualFold(startKind, kc.kindName) {
				continue
			}
			started = true
		}
		remaining := pageSize - len(result)
		if remaining <= 0 {
			return result, encodeWorkloadPageToken(kc.kindName, continueToken), nil
		}
		var list k8sWorkloadList
		path := workloadListPath(resourcePath(kc.apiBase, ns, kc.resource), remaining, continueToken)
		if err := c.get(ctx, path, &list); err != nil {
			return nil, "", fmt.Errorf("k8s: list %s: %w", kc.kindName, err)
		}
		if len(list.Items) > remaining {
			return nil, "", fmt.Errorf("k8s: list %s returned %d items for a limit of %d", kc.kindName, len(list.Items), remaining)
		}
		for _, w := range list.Items {
			result = append(result, toWorkloadSummary(w, kc.kindName))
		}
		if list.Metadata.Continue != "" {
			return result, encodeWorkloadPageToken(kc.kindName, list.Metadata.Continue), nil
		}
		continueToken = ""
		if len(result) >= pageSize {
			if nextKind := nextSelectedWorkloadKind(workloadListKinds, kindIndex+1, canonicalKind); nextKind != "" {
				return result, encodeWorkloadPageToken(nextKind, ""), nil
			}
			return result, "", nil
		}
	}
	if !started {
		return nil, "", fmt.Errorf("k8s: page token references unsupported workload kind %q", startKind)
	}
	return result, "", nil
}

type workloadPageToken struct {
	Kind     string `json:"kind"`
	Continue string `json:"continue,omitempty"`
}

func encodeWorkloadPageToken(kind, continueToken string) string {
	encoded, _ := json.Marshal(workloadPageToken{Kind: kind, Continue: continueToken})
	return base64.RawURLEncoding.EncodeToString(encoded)
}

func decodeWorkloadPageToken(token string) (string, string, error) {
	if token == "" {
		return "", "", nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return "", "", fmt.Errorf("k8s: invalid page token")
	}
	var value workloadPageToken
	if err := json.Unmarshal(decoded, &value); err != nil || value.Kind == "" {
		return "", "", fmt.Errorf("k8s: invalid page token")
	}
	return value.Kind, value.Continue, nil
}

func workloadListPath(path string, limit int, continueToken string) string {
	query := url.Values{"limit": []string{strconv.Itoa(limit)}}
	if continueToken != "" {
		query.Set("continue", continueToken)
	}
	return path + "?" + query.Encode()
}

func nextSelectedWorkloadKind(kinds []workloadListKindConfig, start int, filter string) string {
	for _, candidate := range kinds[start:] {
		if filter == "" || strings.EqualFold(filter, candidate.kindName) {
			return candidate.kindName
		}
	}
	return ""
}

// getWorkload fetches a single workload's full spec and returns WorkloadDetails.
func (c *k8sClient) getWorkload(ctx context.Context, ns, name, kind string) (models.GKEWorkloadDetails, error) {
	kc, err := kindConfig(kind)
	if err != nil {
		return models.GKEWorkloadDetails{}, err
	}
	path := resourcePath(kc.apiBase, ns, kc.resource) + "/" + name
	var w k8sWorkload
	if err := c.get(ctx, path, &w); err != nil {
		return models.GKEWorkloadDetails{}, err
	}
	return toWorkloadDetails(w, kind), nil
}

type k8sKindConfig struct {
	apiBase  string
	resource string
}

func kindConfig(kind string) (k8sKindConfig, error) {
	canonicalKind, err := canonicalWorkloadKind(kind, false)
	if err != nil {
		return k8sKindConfig{}, err
	}
	switch canonicalKind {
	case "Deployment":
		return k8sKindConfig{"apis/apps/v1", "deployments"}, nil
	case "StatefulSet":
		return k8sKindConfig{"apis/apps/v1", "statefulsets"}, nil
	case "DaemonSet":
		return k8sKindConfig{"apis/apps/v1", "daemonsets"}, nil
	case "CronJob":
		return k8sKindConfig{"apis/batch/v1", "cronjobs"}, nil
	case "Job":
		return k8sKindConfig{"apis/batch/v1", "jobs"}, nil
	default:
		return k8sKindConfig{}, fmt.Errorf("k8s: unsupported workload kind %q", canonicalKind)
	}
}

// listServices returns Kubernetes Service summaries.
func (c *k8sClient) listServices(ctx context.Context, ns string) ([]models.GKEServiceSummary, error) {
	var list k8sServiceList
	if err := c.get(ctx, resourcePath("api/v1", ns, "services"), &list); err != nil {
		return nil, err
	}
	result := make([]models.GKEServiceSummary, 0, len(list.Items))
	for _, s := range list.Items {
		result = append(result, toServiceSummary(s))
	}
	return result, nil
}

// listIngresses returns Kubernetes Ingress and Gateway API HTTPRoute summaries.
func (c *k8sClient) listIngresses(ctx context.Context, ns string) ([]models.GKEIngressSummary, error) {
	var result []models.GKEIngressSummary

	var iList k8sIngressList
	if err := c.get(ctx, resourcePath("apis/networking.k8s.io/v1", ns, "ingresses"), &iList); err != nil {
		return nil, fmt.Errorf("k8s: list ingresses: %w", err)
	}
	for _, ing := range iList.Items {
		result = append(result, toIngressSummary(ing))
	}

	// Gateway API HTTPRoutes — silently skip if the CRD is not installed (404).
	var hrList k8sHTTPRouteList
	if err := c.get(ctx, resourcePath("apis/gateway.networking.k8s.io/v1", ns, "httproutes"), &hrList); err != nil {
		if !strings.Contains(err.Error(), "HTTP 404") && !strings.Contains(err.Error(), "HTTP 405") {
			return nil, fmt.Errorf("k8s: list httproutes: %w", err)
		}
	} else {
		for _, hr := range hrList.Items {
			result = append(result, toHTTPRouteSummary(hr))
		}
	}

	return result, nil
}

func (c *k8sClient) listGateways(ctx context.Context, ns string) ([]k8sGateway, error) {
	var list k8sGatewayList
	if err := c.get(ctx, resourcePath("apis/gateway.networking.k8s.io/v1", ns, "gateways"), &list); err != nil {
		return nil, err
	}
	return list.Items, nil
}

func (c *k8sClient) listKubernetesServiceAccounts(ctx context.Context, ns string) ([]k8sServiceAccount, error) {
	var list k8sServiceAccountList
	if err := c.get(ctx, resourcePath("api/v1", ns, "serviceaccounts"), &list); err != nil {
		return nil, err
	}
	return list.Items, nil
}

// listNetworkPolicies returns Kubernetes NetworkPolicy summaries.
func (c *k8sClient) listNetworkPolicies(ctx context.Context, ns string) ([]models.GKENetworkPolicySummary, error) {
	var list k8sNetworkPolicyList
	if err := c.get(ctx, resourcePath("apis/networking.k8s.io/v1", ns, "networkpolicies"), &list); err != nil {
		return nil, err
	}
	result := make([]models.GKENetworkPolicySummary, 0, len(list.Items))
	for _, np := range list.Items {
		result = append(result, toNetworkPolicySummary(np))
	}
	return result, nil
}

// --- Conversion helpers ---

var otelContainerRE = regexp.MustCompile(`^(opentelemetry-collector|otel-.+)$`)

// detectOtelSidecar returns true if pod-level signals indicate OTel
// instrumentation: a sidecar container name match, the inject annotation, or
// OTEL_EXPORTER_OTLP_ENDPOINT in any container's env.
func detectOtelSidecar(podSpec k8sPodSpec, podMeta k8sMeta) bool {
	// Annotation: sidecar.opentelemetry.io/inject = "true"
	if podMeta.Annotations["sidecar.opentelemetry.io/inject"] == "true" {
		return true
	}
	// Label: instrumentation.opentelemetry.io/inject-*
	for k := range podMeta.Labels {
		if strings.HasPrefix(k, "instrumentation.opentelemetry.io/") {
			return true
		}
	}
	allContainers := append(podSpec.Containers, podSpec.InitContainers...)
	for _, c := range allContainers {
		if otelContainerRE.MatchString(c.Name) {
			return true
		}
		for _, env := range c.Env {
			if strings.HasPrefix(env.Name, "OTEL_") {
				return true
			}
		}
	}
	return false
}

// extractSecretRefs collects all Secret names referenced by a pod spec
// (via secretKeyRef env vars, envFrom secretRef, and secret volumes).
func extractSecretRefs(spec k8sPodSpec) []string {
	seen := make(map[string]bool)
	addSecret := func(name string) {
		if name != "" {
			seen[name] = true
		}
	}
	for _, c := range append(spec.Containers, spec.InitContainers...) {
		for _, env := range c.Env {
			if env.ValueFrom != nil && env.ValueFrom.SecretKeyRef != nil {
				addSecret(env.ValueFrom.SecretKeyRef.Name)
			}
		}
		for _, ef := range c.EnvFrom {
			if ef.SecretRef != nil {
				addSecret(ef.SecretRef.Name)
			}
		}
	}
	for _, v := range spec.Volumes {
		if v.Secret != nil {
			addSecret(v.Secret.SecretName)
		}
	}
	refs := make([]string, 0, len(seen))
	for name := range seen {
		refs = append(refs, name)
	}
	return refs
}

// podSpec returns the effective pod spec and pod template metadata for any
// workload kind, including the CronJob double-nesting.
func effectivePodSpec(w k8sWorkload) (k8sPodSpec, k8sMeta) {
	if w.Spec.JobTemplate != nil {
		return w.Spec.JobTemplate.Spec.Template.Spec, w.Spec.JobTemplate.Spec.Template.Metadata
	}
	return w.Spec.Template.Spec, w.Spec.Template.Metadata
}

func toWorkloadSummary(w k8sWorkload, kind string) models.GKEWorkloadSummary {
	podSpec, podMeta := effectivePodSpec(w)

	var replicas int32
	if w.Spec.Replicas != nil {
		replicas = *w.Spec.Replicas
	}

	sa := podSpec.ServiceAccountName
	if sa == "" {
		sa = w.Spec.ServiceAccountName
	}

	image := ""
	if len(podSpec.Containers) > 0 {
		image = podSpec.Containers[0].Image
	}

	kindConst := workloadKindConst(kind)

	return models.GKEWorkloadSummary{
		Name:                         w.Metadata.Name,
		Namespace:                    w.Metadata.Namespace,
		Kind:                         kindConst,
		Replicas:                     replicas,
		ReadyReplicas:                w.Status.ReadyReplicas,
		Image:                        image,
		ServiceAccount:               sa,
		AutomountServiceAccountToken: podSpec.AutomountServiceAccountToken,
		SecretRefs:                   extractSecretRefs(podSpec),
		Labels:                       w.Metadata.Labels,
		OtelSidecar:                  detectOtelSidecar(podSpec, podMeta),
	}
}

func toWorkloadDetails(w k8sWorkload, kind string) models.GKEWorkloadDetails {
	summary := toWorkloadSummary(w, kind)
	podSpec, _ := effectivePodSpec(w)

	allContainers := append(podSpec.Containers, podSpec.InitContainers...)
	containers := make([]models.GKEContainerSummary, 0, len(allContainers))
	isInit := make(map[string]bool, len(podSpec.InitContainers))
	for _, ic := range podSpec.InitContainers {
		isInit[ic.Name] = true
	}
	for _, c := range allContainers {
		ports := make([]int32, 0, len(c.Ports))
		for _, p := range c.Ports {
			ports = append(ports, p.ContainerPort)
		}
		envVars := make([]models.GKEEnvVar, 0, len(c.Env))
		for _, e := range c.Env {
			ev := safeGKEEnvVar(e)
			envVars = append(envVars, ev)
		}
		containers = append(containers, models.GKEContainerSummary{
			Name:    c.Name,
			Image:   c.Image,
			Ports:   ports,
			EnvVars: envVars,
			Resources: models.GKEResourceRequirements{
				CPURequest:    c.Resources.Requests["cpu"],
				CPULimit:      c.Resources.Limits["cpu"],
				MemoryRequest: c.Resources.Requests["memory"],
				MemoryLimit:   c.Resources.Limits["memory"],
			},
			IsInit: isInit[c.Name],
		})
	}

	tolerations := make([]string, 0, len(podSpec.Tolerations))
	for _, t := range podSpec.Tolerations {
		tolerations = append(tolerations, t.Key+"="+t.Value)
	}

	annotations, annotationsOmitted, omittedAnnotationFingerprints := safeWorkloadAnnotations(w.Metadata.Annotations)
	return models.GKEWorkloadDetails{
		GKEWorkloadSummary:            summary,
		Containers:                    containers,
		NodeSelector:                  podSpec.NodeSelector,
		Tolerations:                   tolerations,
		Annotations:                   annotations,
		AnnotationsOmitted:            annotationsOmitted,
		OmittedAnnotationFingerprints: omittedAnnotationFingerprints,
	}
}

func safeGKEEnvVar(env k8sEnvVar) models.GKEEnvVar {
	result := models.GKEEnvVar{Name: env.Name, Source: "unknown"}
	if env.ValueFrom == nil {
		result.Source = "literal"
		result.HasLiteralValue = env.Value != ""
		if env.Value != "" {
			digest := sha256.Sum256([]byte("gke-env-v1\x00" + env.Value))
			result.ValueFingerprint = fmt.Sprintf("sha256:%x", digest)
		}
		return result
	}
	if env.ValueFrom.SecretKeyRef != nil {
		result.Source = "secret_key_ref"
		result.SecretRef = env.ValueFrom.SecretKeyRef.Name
		return result
	}
	if env.ValueFrom.ConfigMapKeyRef != nil {
		result.Source = "config_map_key_ref"
		result.ConfigMapRef = env.ValueFrom.ConfigMapKeyRef.Name
		return result
	}
	if env.ValueFrom.FieldRef != nil {
		result.Source = "field_ref"
		result.FieldRef = env.ValueFrom.FieldRef.FieldPath
		return result
	}
	if env.ValueFrom.ResourceFieldRef != nil {
		result.Source = "resource_field_ref"
		result.FieldRef = env.ValueFrom.ResourceFieldRef.Resource
	}
	return result
}

var safeBooleanWorkloadAnnotations = map[string]bool{
	"sidecar.opentelemetry.io/inject":                true,
	"instrumentation.opentelemetry.io/inject-java":   true,
	"instrumentation.opentelemetry.io/inject-nodejs": true,
	"instrumentation.opentelemetry.io/inject-python": true,
	"instrumentation.opentelemetry.io/inject-dotnet": true,
	"instrumentation.opentelemetry.io/inject-go":     true,
	"prometheus.io/scrape":                           true,
}

func safeWorkloadAnnotations(annotations map[string]string) (map[string]string, int, []string) {
	if len(annotations) == 0 {
		return nil, 0, nil
	}
	safe := make(map[string]string)
	omittedFingerprints := make([]string, 0, len(annotations))
	for key, value := range annotations {
		if safeBooleanWorkloadAnnotations[key] {
			switch strings.ToLower(strings.TrimSpace(value)) {
			case "true":
				safe[key] = "true"
				continue
			case "false":
				safe[key] = "false"
				continue
			}
		}
		digest := sha256.Sum256([]byte("gke-annotation-v1\x00" + key + "\x00" + value))
		omittedFingerprints = append(omittedFingerprints, fmt.Sprintf("sha256:%x", digest))
	}
	if len(safe) == 0 {
		safe = nil
	}
	sort.Strings(omittedFingerprints)
	return safe, len(omittedFingerprints), omittedFingerprints
}

func toServiceSummary(s k8sService) models.GKEServiceSummary {
	ports := make([]models.GKEServicePort, 0, len(s.Spec.Ports))
	for _, p := range s.Spec.Ports {
		tp := ""
		switch v := p.TargetPort.(type) {
		case float64:
			tp = fmt.Sprintf("%d", int(v))
		case string:
			tp = v
		}
		ports = append(ports, models.GKEServicePort{
			Name:       p.Name,
			Port:       p.Port,
			TargetPort: tp,
			Protocol:   p.Protocol,
			NodePort:   p.NodePort,
		})
	}
	addresses := append([]string(nil), s.Spec.ExternalIPs...)
	for _, ingress := range s.Status.LoadBalancer.Ingress {
		addresses = appendUnique(addresses, firstNonEmpty(ingress.IP, ingress.Hostname))
	}
	internal := strings.EqualFold(s.Metadata.Annotations["networking.gke.io/load-balancer-type"], "Internal") ||
		strings.EqualFold(s.Metadata.Annotations["cloud.google.com/load-balancer-type"], "Internal")
	return models.GKEServiceSummary{
		Name:                     s.Metadata.Name,
		Namespace:                s.Metadata.Namespace,
		Type:                     s.Spec.Type,
		ClusterIP:                s.Spec.ClusterIP,
		ExternalIPs:              s.Spec.ExternalIPs,
		Ports:                    ports,
		Selector:                 s.Spec.Selector,
		NEGAnnotation:            s.Metadata.Annotations["cloud.google.com/neg"],
		Labels:                   s.Metadata.Labels,
		LoadBalancerAddresses:    addresses,
		LoadBalancerSourceRanges: append([]string(nil), s.Spec.LoadBalancerSourceRanges...),
		LoadBalancerClass:        s.Spec.LoadBalancerClass,
		ExternalTrafficPolicy:    s.Spec.ExternalTrafficPolicy,
		Internal:                 internal,
	}
}

func toIngressSummary(ing k8sIngress) models.GKEIngressSummary {
	hosts := make([]string, 0)
	rules := make([]models.GKEIngressRule, 0, len(ing.Spec.Rules))
	for _, r := range ing.Spec.Rules {
		if r.Host != "" {
			hosts = append(hosts, r.Host)
		}
		ir := models.GKEIngressRule{Host: r.Host}
		if r.HTTP != nil {
			for _, p := range r.HTTP.Paths {
				path := models.GKEIngressPath{Path: p.Path, PathType: p.PathType}
				if p.Backend.Service != nil {
					path.BackendName = p.Backend.Service.Name
					path.BackendPort = p.Backend.Service.Port.Number
				}
				ir.Paths = append(ir.Paths, path)
			}
		}
		rules = append(rules, ir)
	}

	// GCP LB name from annotation: kubernetes.io/ingress.class or the managed-cert annotation.
	gcpLB := ing.Metadata.Annotations["kubernetes.io/ingress.global-static-ip-name"]
	if gcpLB == "" {
		gcpLB = ing.Metadata.Annotations["ingress.gcp.kubernetes.io/pre-shared-cert"]
	}

	addresses := make([]string, 0, len(ing.Status.LoadBalancer.Ingress))
	for _, address := range ing.Status.LoadBalancer.Ingress {
		addresses = appendUnique(addresses, firstNonEmpty(address.IP, address.Hostname))
	}
	ingressClass := firstNonEmpty(ing.Spec.IngressClassName, ing.Metadata.Annotations["kubernetes.io/ingress.class"])
	internal := strings.Contains(strings.ToLower(ingressClass), "internal")
	defaultBackend := ""
	if ing.Spec.DefaultBackend != nil && ing.Spec.DefaultBackend.Service != nil {
		defaultBackend = ing.Spec.DefaultBackend.Service.Name
	}
	return models.GKEIngressSummary{
		Name:             ing.Metadata.Name,
		Namespace:        ing.Metadata.Namespace,
		Kind:             "Ingress",
		Hosts:            hosts,
		TLSEnabled:       len(ing.Spec.TLS) > 0,
		PlaintextEnabled: !strings.EqualFold(ing.Metadata.Annotations["kubernetes.io/ingress.allow-http"], "false"),
		Rules:            rules,
		GCPLBName:        gcpLB,
		Labels:           ing.Metadata.Labels,
		Addresses:        addresses,
		IngressClass:     ingressClass,
		DefaultBackend:   defaultBackend,
		Internal:         internal,
	}
}

func toHTTPRouteSummary(hr k8sHTTPRoute) models.GKEIngressSummary {
	hosts := make([]string, 0, len(hr.Spec.Hostnames))
	for _, h := range hr.Spec.Hostnames {
		hosts = append(hosts, h)
	}
	parents := make([]string, 0, len(hr.Spec.ParentRefs))
	for _, parent := range hr.Spec.ParentRefs {
		if parent.Kind != "" && !strings.EqualFold(parent.Kind, "Gateway") {
			continue
		}
		namespace := parent.Namespace
		if namespace == "" {
			namespace = hr.Metadata.Namespace
		}
		parents = appendUnique(parents, namespace+"/"+parent.Name)
	}
	rules := make([]models.GKEIngressRule, 0, len(hr.Spec.Rules))
	for _, r := range hr.Spec.Rules {
		ir := models.GKEIngressRule{}
		for _, br := range r.BackendRefs {
			ir.Paths = append(ir.Paths, models.GKEIngressPath{
				BackendName: br.Name,
				BackendPort: br.Port,
			})
		}
		rules = append(rules, ir)
	}
	return models.GKEIngressSummary{
		Name:           hr.Metadata.Name,
		Namespace:      hr.Metadata.Namespace,
		Kind:           "HTTPRoute",
		Hosts:          hosts,
		Rules:          rules,
		Labels:         hr.Metadata.Labels,
		ParentGateways: parents,
	}
}

func toNetworkPolicySummary(np k8sNetworkPolicy) models.GKENetworkPolicySummary {
	return models.GKENetworkPolicySummary{
		Name:             np.Metadata.Name,
		Namespace:        np.Metadata.Namespace,
		PodSelector:      np.Spec.PodSelector.MatchLabels,
		IngressRuleCount: len(np.Spec.Ingress),
		EgressRuleCount:  len(np.Spec.Egress),
		PolicyTypes:      np.Spec.PolicyTypes,
		Labels:           np.Metadata.Labels,
	}
}

// workloadKindConst maps the K8s Kind string to the models.Kind* constant.
func workloadKindConst(kind string) string {
	switch strings.ToLower(kind) {
	case "deployment":
		return models.KindGKEDeployment
	case "statefulset":
		return models.KindGKEStatefulSet
	case "daemonset":
		return models.KindGKEDaemonSet
	case "cronjob":
		return models.KindGKECronJob
	case "job":
		return models.KindGKEJob
	default:
		return strings.ToLower(kind)
	}
}
