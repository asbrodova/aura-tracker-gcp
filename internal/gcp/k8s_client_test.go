package gcp

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"golang.org/x/oauth2"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

// staticTokenSource implements oauth2.TokenSource for testing.
type staticTokenSource struct{ tok *oauth2.Token }

func (s *staticTokenSource) Token() (*oauth2.Token, error) { return s.tok, nil }

// newTestK8sServer creates an httptest.TLSServer with the given handler and
// returns a k8sClient pre-configured to trust the server's certificate.
func newTestK8sServer(t *testing.T, handler http.HandlerFunc) *k8sClient {
	t.Helper()
	srv := httptest.NewTLSServer(handler)
	t.Cleanup(srv.Close)

	cert := srv.TLS.Certificates[0]
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatalf("parse test server cert: %v", err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(leaf)

	transport := &tokenRoundTripper{
		base: &http.Transport{
			TLSClientConfig: &tls.Config{RootCAs: pool},
		},
		tokenSrc: &staticTokenSource{tok: &oauth2.Token{AccessToken: "test-token"}},
	}
	return &k8sClient{
		baseURL:    srv.URL,
		httpClient: &http.Client{Transport: transport},
	}
}

func serveJSON(t *testing.T, v any) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(v); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}
}

// --- dialClusterWithTokenSource ---

func TestDialClusterWithTokenSource_DecodesBase64CA(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"items":[]}`))
	}))
	t.Cleanup(srv.Close)

	cert := srv.TLS.Certificates[0]
	leaf, _ := x509.ParseCertificate(cert.Certificate[0])
	pem := make([]byte, 0, 256)
	pem = append(pem, "-----BEGIN CERTIFICATE-----\n"...)
	raw := base64.StdEncoding.EncodeToString(leaf.Raw)
	for i := 0; i < len(raw); i += 64 {
		end := i + 64
		if end > len(raw) {
			end = len(raw)
		}
		pem = append(pem, raw[i:end]...)
		pem = append(pem, '\n')
	}
	pem = append(pem, "-----END CERTIFICATE-----\n"...)
	caCertBase64 := base64.StdEncoding.EncodeToString(pem)

	ts := &staticTokenSource{tok: &oauth2.Token{AccessToken: "tok"}}
	// Strip scheme — dialClusterWithTokenSource adds https://
	host := srv.URL[len("https://"):]
	client, err := dialClusterWithTokenSource(host, caCertBase64, ts)
	if err != nil {
		t.Fatalf("dialClusterWithTokenSource: %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}
}

// --- listWorkloads ---

func TestListWorkloads_Deployment(t *testing.T) {
	fixture := k8sWorkloadList{
		Items: []k8sWorkload{
			{
				Metadata: k8sMeta{Name: "frontend", Namespace: "default", Labels: map[string]string{"app": "frontend"}},
				Spec: k8sWorkloadSpec{
					Replicas: ptr[int32](3),
					Template: k8sPodTemplate{
						Metadata: k8sMeta{},
						Spec: k8sPodSpec{
							ServiceAccountName: "frontend-sa",
							Containers: []k8sContainer{
								{Name: "app", Image: "gcr.io/my-project/frontend:v1"},
							},
						},
					},
				},
				Status: k8sWorkloadStatus{ReadyReplicas: 3},
			},
		},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/apis/apps/v1/deployments", serveJSON(t, fixture))
	mux.HandleFunc("/apis/apps/v1/statefulsets", serveJSON(t, k8sWorkloadList{}))
	mux.HandleFunc("/apis/apps/v1/daemonsets", serveJSON(t, k8sWorkloadList{}))
	mux.HandleFunc("/apis/batch/v1/cronjobs", serveJSON(t, k8sWorkloadList{}))
	mux.HandleFunc("/apis/batch/v1/jobs", serveJSON(t, k8sWorkloadList{}))

	client := newTestK8sServer(t, mux.ServeHTTP)
	workloads, err := client.listWorkloads(context.Background(), "", "")
	if err != nil {
		t.Fatalf("listWorkloads: %v", err)
	}
	if len(workloads) != 1 {
		t.Fatalf("expected 1 workload, got %d", len(workloads))
	}
	w := workloads[0]
	if w.Name != "frontend" {
		t.Errorf("name = %q, want %q", w.Name, "frontend")
	}
	if w.Kind != models.KindGKEDeployment {
		t.Errorf("kind = %q, want %q", w.Kind, models.KindGKEDeployment)
	}
	if w.Replicas != 3 {
		t.Errorf("replicas = %d, want 3", w.Replicas)
	}
	if w.ServiceAccount != "frontend-sa" {
		t.Errorf("serviceAccount = %q, want %q", w.ServiceAccount, "frontend-sa")
	}
	if w.Image != "gcr.io/my-project/frontend:v1" {
		t.Errorf("image = %q", w.Image)
	}
}

func TestListWorkloads_KindFilter(t *testing.T) {
	fixture := k8sWorkloadList{
		Items: []k8sWorkload{
			{
				Metadata: k8sMeta{Name: "worker", Namespace: "default"},
				Spec: k8sWorkloadSpec{
					Replicas: ptr[int32](1),
					Template: k8sPodTemplate{
						Spec: k8sPodSpec{
							Containers: []k8sContainer{{Name: "worker", Image: "image:latest"}},
						},
					},
				},
			},
		},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/apis/apps/v1/statefulsets", serveJSON(t, fixture))

	client := newTestK8sServer(t, mux.ServeHTTP)
	workloads, err := client.listWorkloads(context.Background(), "", "StatefulSet")
	if err != nil {
		t.Fatalf("listWorkloads: %v", err)
	}
	if len(workloads) != 1 {
		t.Fatalf("expected 1, got %d", len(workloads))
	}
	if workloads[0].Kind != models.KindGKEStatefulSet {
		t.Errorf("kind = %q", workloads[0].Kind)
	}
}

// --- OTel sidecar detection ---

func TestDetectOtelSidecar(t *testing.T) {
	tests := []struct {
		name     string
		spec     k8sPodSpec
		meta     k8sMeta
		expected bool
	}{
		{
			name:     "no otel signals",
			spec:     k8sPodSpec{Containers: []k8sContainer{{Name: "app", Image: "img"}}},
			meta:     k8sMeta{},
			expected: false,
		},
		{
			name: "otel-collector sidecar container",
			spec: k8sPodSpec{Containers: []k8sContainer{
				{Name: "app"},
				{Name: "opentelemetry-collector"},
			}},
			meta:     k8sMeta{},
			expected: true,
		},
		{
			name: "otel- prefix sidecar",
			spec: k8sPodSpec{Containers: []k8sContainer{
				{Name: "app"},
				{Name: "otel-sidecar"},
			}},
			meta:     k8sMeta{},
			expected: true,
		},
		{
			name: "OTEL_ env var",
			spec: k8sPodSpec{Containers: []k8sContainer{
				{Name: "app", Env: []k8sEnvVar{{Name: "OTEL_EXPORTER_OTLP_ENDPOINT", Value: "http://otel:4317"}}},
			}},
			meta:     k8sMeta{},
			expected: true,
		},
		{
			name:     "sidecar inject annotation",
			spec:     k8sPodSpec{Containers: []k8sContainer{{Name: "app"}}},
			meta:     k8sMeta{Annotations: map[string]string{"sidecar.opentelemetry.io/inject": "true"}},
			expected: true,
		},
		{
			name:     "instrumentation inject label",
			spec:     k8sPodSpec{Containers: []k8sContainer{{Name: "app"}}},
			meta:     k8sMeta{Labels: map[string]string{"instrumentation.opentelemetry.io/inject-java": "true"}},
			expected: true,
		},
		{
			name: "OTEL_ in init container",
			spec: k8sPodSpec{
				Containers:     []k8sContainer{{Name: "app"}},
				InitContainers: []k8sContainer{{Name: "init", Env: []k8sEnvVar{{Name: "OTEL_SERVICE_NAME", Value: "svc"}}}},
			},
			meta:     k8sMeta{},
			expected: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := detectOtelSidecar(tc.spec, tc.meta)
			if got != tc.expected {
				t.Errorf("detectOtelSidecar = %v, want %v", got, tc.expected)
			}
		})
	}
}

// --- Secret ref extraction ---

func TestExtractSecretRefs(t *testing.T) {
	spec := k8sPodSpec{
		Containers: []k8sContainer{
			{
				Name: "app",
				Env: []k8sEnvVar{
					{Name: "DB_PASS", ValueFrom: &k8sEnvVarSource{SecretKeyRef: &k8sSecretKeySelector{Name: "db-secret", Key: "password"}}},
					{Name: "PLAIN", Value: "value"},
				},
				EnvFrom: []k8sEnvFrom{
					{SecretRef: &k8sSecretEnvSource{Name: "bulk-secret"}},
				},
			},
		},
		Volumes: []k8sVolume{
			{Name: "certs", Secret: &k8sSecretVolumeSource{SecretName: "tls-secret"}},
			{Name: "empty", Secret: nil},
		},
	}

	refs := extractSecretRefs(spec)
	got := make(map[string]bool, len(refs))
	for _, r := range refs {
		got[r] = true
	}
	for _, want := range []string{"db-secret", "bulk-secret", "tls-secret"} {
		if !got[want] {
			t.Errorf("missing secret ref %q; got %v", want, refs)
		}
	}
	if len(refs) != 3 {
		t.Errorf("expected 3 refs, got %d: %v", len(refs), refs)
	}
}

// --- listServices ---

func TestListServices(t *testing.T) {
	fixture := k8sServiceList{
		Items: []k8sService{
			{
				Metadata: k8sMeta{
					Name:      "my-svc",
					Namespace: "default",
					Labels:    map[string]string{"app": "my-svc"},
					Annotations: map[string]string{
						"cloud.google.com/neg": `{"ingress":true}`,
					},
				},
				Spec: k8sServiceSpec{
					Type:      "LoadBalancer",
					ClusterIP: "10.0.0.5",
					Selector:  map[string]string{"app": "my-svc"},
					Ports: []k8sServicePort{
						{Name: "http", Port: 80, TargetPort: float64(8080), Protocol: "TCP"},
					},
				},
			},
		},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/services", serveJSON(t, fixture))

	client := newTestK8sServer(t, mux.ServeHTTP)
	services, err := client.listServices(context.Background(), "")
	if err != nil {
		t.Fatalf("listServices: %v", err)
	}
	if len(services) != 1 {
		t.Fatalf("expected 1 service, got %d", len(services))
	}
	s := services[0]
	if s.Type != "LoadBalancer" {
		t.Errorf("type = %q", s.Type)
	}
	if s.NEGAnnotation == "" {
		t.Error("expected NEGAnnotation to be set")
	}
	if len(s.Ports) != 1 || s.Ports[0].Port != 80 {
		t.Errorf("ports = %+v", s.Ports)
	}
}

// --- listNetworkPolicies ---

func TestListNetworkPolicies(t *testing.T) {
	fixture := k8sNetworkPolicyList{
		Items: []k8sNetworkPolicy{
			{
				Metadata: k8sMeta{Name: "deny-all", Namespace: "prod"},
				Spec: k8sNetworkPolicySpec{
					PodSelector: k8sLabelSelector{},
					Ingress:     []k8sNetworkPolicyIngressRule{{}, {}},
					Egress:      []k8sNetworkPolicyEgressRule{{}},
					PolicyTypes: []string{"Ingress", "Egress"},
				},
			},
		},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/apis/networking.k8s.io/v1/networkpolicies", serveJSON(t, fixture))

	client := newTestK8sServer(t, mux.ServeHTTP)
	policies, err := client.listNetworkPolicies(context.Background(), "")
	if err != nil {
		t.Fatalf("listNetworkPolicies: %v", err)
	}
	if len(policies) != 1 {
		t.Fatalf("expected 1 policy, got %d", len(policies))
	}
	p := policies[0]
	if p.IngressRuleCount != 2 {
		t.Errorf("ingress rule count = %d, want 2", p.IngressRuleCount)
	}
	if p.EgressRuleCount != 1 {
		t.Errorf("egress rule count = %d, want 1", p.EgressRuleCount)
	}
}

// --- listIngresses ---

func TestListIngresses_SkipsHTTPRoutesOn404(t *testing.T) {
	fixture := k8sIngressList{
		Items: []k8sIngress{
			{
				Metadata: k8sMeta{Name: "main-ingress", Namespace: "default"},
				Spec: k8sIngressSpec{
					Rules: []k8sIngressRule{{Host: "example.com"}},
					TLS:   []k8sIngressTLS{{Hosts: []string{"example.com"}}},
				},
			},
		},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/apis/networking.k8s.io/v1/ingresses", serveJSON(t, fixture))
	mux.HandleFunc("/apis/gateway.networking.k8s.io/v1/httproutes", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	client := newTestK8sServer(t, mux.ServeHTTP)
	ingresses, err := client.listIngresses(context.Background(), "")
	if err != nil {
		t.Fatalf("listIngresses: %v", err)
	}
	if len(ingresses) != 1 {
		t.Fatalf("expected 1 ingress, got %d", len(ingresses))
	}
	if !ingresses[0].TLSEnabled {
		t.Error("expected TLSEnabled = true")
	}
}

// ptr returns a pointer to the given value (test helper).
func ptr[T any](v T) *T { return &v }
