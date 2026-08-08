package gcp

// Minimal Kubernetes API decode-target types. Only fields needed by the
// GKE workload tools are present — this is not a full K8s client library.
// All types are unexported; they exist solely as JSON decode targets for
// the k8sClient REST methods.

type k8sMeta struct {
	Name        string            `json:"name"`
	Namespace   string            `json:"namespace"`
	Labels      map[string]string `json:"labels"`
	Annotations map[string]string `json:"annotations"`
}

// k8sWorkloadList covers Deployment, StatefulSet, DaemonSet, CronJob, and Job
// list responses. All share the same items[*].spec.template.spec structure.
type k8sWorkloadList struct {
	Items []k8sWorkload `json:"items"`
}

type k8sWorkload struct {
	Metadata k8sMeta           `json:"metadata"`
	Spec     k8sWorkloadSpec   `json:"spec"`
	Status   k8sWorkloadStatus `json:"status"`
}

type k8sWorkloadSpec struct {
	Replicas           *int32         `json:"replicas"`           // pointer: absent for DaemonSet
	ServiceAccountName string         `json:"serviceAccountName"` // top-level (Job/CronJob)
	Template           k8sPodTemplate `json:"template"`
	// CronJob wraps the pod template one level deeper.
	JobTemplate *k8sCronJobTemplate `json:"jobTemplate"`
}

type k8sCronJobTemplate struct {
	Spec struct {
		Template k8sPodTemplate `json:"template"`
	} `json:"spec"`
}

type k8sPodTemplate struct {
	Metadata k8sMeta    `json:"metadata"`
	Spec     k8sPodSpec `json:"spec"`
}

type k8sPodSpec struct {
	ServiceAccountName           string            `json:"serviceAccountName"`
	AutomountServiceAccountToken *bool             `json:"automountServiceAccountToken"`
	Containers                   []k8sContainer    `json:"containers"`
	InitContainers               []k8sContainer    `json:"initContainers"`
	Volumes                      []k8sVolume       `json:"volumes"`
	NodeSelector                 map[string]string `json:"nodeSelector"`
	Tolerations                  []k8sToleration   `json:"tolerations"`
}

type k8sContainer struct {
	Name      string             `json:"name"`
	Image     string             `json:"image"`
	Ports     []k8sContainerPort `json:"ports"`
	Env       []k8sEnvVar        `json:"env"`
	EnvFrom   []k8sEnvFrom       `json:"envFrom"`
	Resources k8sResources       `json:"resources"`
}

type k8sContainerPort struct {
	ContainerPort int32  `json:"containerPort"`
	Protocol      string `json:"protocol"`
}

type k8sEnvVar struct {
	Name      string           `json:"name"`
	Value     string           `json:"value"`
	ValueFrom *k8sEnvVarSource `json:"valueFrom"`
}

type k8sEnvVarSource struct {
	SecretKeyRef *k8sSecretKeySelector `json:"secretKeyRef"`
}

type k8sSecretKeySelector struct {
	Name string `json:"name"`
	Key  string `json:"key"`
}

type k8sEnvFrom struct {
	SecretRef *k8sSecretEnvSource `json:"secretRef"`
}

type k8sSecretEnvSource struct {
	Name string `json:"name"`
}

type k8sVolume struct {
	Name   string                 `json:"name"`
	Secret *k8sSecretVolumeSource `json:"secret"`
}

type k8sSecretVolumeSource struct {
	SecretName string `json:"secretName"`
}

type k8sToleration struct {
	Key      string `json:"key"`
	Operator string `json:"operator"`
	Value    string `json:"value"`
}

type k8sResources struct {
	Requests map[string]string `json:"requests"`
	Limits   map[string]string `json:"limits"`
}

type k8sWorkloadStatus struct {
	ReadyReplicas     int32 `json:"readyReplicas"`
	AvailableReplicas int32 `json:"availableReplicas"`
}

// --- Services ---

type k8sServiceList struct {
	Items []k8sService `json:"items"`
}

type k8sService struct {
	Metadata k8sMeta          `json:"metadata"`
	Spec     k8sServiceSpec   `json:"spec"`
	Status   k8sServiceStatus `json:"status"`
}

type k8sServiceSpec struct {
	Type                     string            `json:"type"`
	ClusterIP                string            `json:"clusterIP"`
	ExternalIPs              []string          `json:"externalIPs"`
	Selector                 map[string]string `json:"selector"`
	Ports                    []k8sServicePort  `json:"ports"`
	LoadBalancerSourceRanges []string          `json:"loadBalancerSourceRanges"`
	LoadBalancerClass        string            `json:"loadBalancerClass"`
	ExternalTrafficPolicy    string            `json:"externalTrafficPolicy"`
}

type k8sLoadBalancerIngress struct {
	IP       string `json:"ip"`
	Hostname string `json:"hostname"`
}

type k8sServiceStatus struct {
	LoadBalancer struct {
		Ingress []k8sLoadBalancerIngress `json:"ingress"`
	} `json:"loadBalancer"`
}

type k8sServicePort struct {
	Name       string `json:"name"`
	Port       int32  `json:"port"`
	TargetPort any    `json:"targetPort"` // can be int or string
	Protocol   string `json:"protocol"`
	NodePort   int32  `json:"nodePort"`
}

// --- Ingresses ---

type k8sIngressList struct {
	Items []k8sIngress `json:"items"`
}

type k8sIngress struct {
	Metadata k8sMeta          `json:"metadata"`
	Spec     k8sIngressSpec   `json:"spec"`
	Status   k8sIngressStatus `json:"status"`
}

type k8sIngressSpec struct {
	Rules            []k8sIngressRule   `json:"rules"`
	TLS              []k8sIngressTLS    `json:"tls"`
	IngressClassName string             `json:"ingressClassName"`
	DefaultBackend   *k8sIngressBackend `json:"defaultBackend"`
}

type k8sIngressStatus struct {
	LoadBalancer struct {
		Ingress []k8sLoadBalancerIngress `json:"ingress"`
	} `json:"loadBalancer"`
}

type k8sIngressRule struct {
	Host string                   `json:"host"`
	HTTP *k8sHTTPIngressRuleValue `json:"http"`
}

type k8sHTTPIngressRuleValue struct {
	Paths []k8sHTTPIngressPath `json:"paths"`
}

type k8sHTTPIngressPath struct {
	Path     string            `json:"path"`
	PathType string            `json:"pathType"`
	Backend  k8sIngressBackend `json:"backend"`
}

type k8sIngressBackend struct {
	Service *k8sIngressServiceBackend `json:"service"`
}

type k8sIngressServiceBackend struct {
	Name string                `json:"name"`
	Port k8sServiceBackendPort `json:"port"`
}

type k8sServiceBackendPort struct {
	Number int32  `json:"number"`
	Name   string `json:"name"`
}

type k8sIngressTLS struct {
	Hosts      []string `json:"hosts"`
	SecretName string   `json:"secretName"`
}

// --- HTTPRoutes (Gateway API) ---

type k8sHTTPRouteList struct {
	Items []k8sHTTPRoute `json:"items"`
}

type k8sHTTPRoute struct {
	Metadata k8sMeta          `json:"metadata"`
	Spec     k8sHTTPRouteSpec `json:"spec"`
}

type k8sHTTPRouteSpec struct {
	Hostnames  []string             `json:"hostnames"`
	ParentRefs []k8sParentReference `json:"parentRefs"`
	Rules      []k8sHTTPRouteRule   `json:"rules"`
}

type k8sParentReference struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Kind      string `json:"kind"`
}

type k8sHTTPRouteRule struct {
	BackendRefs []k8sHTTPBackendRef `json:"backendRefs"`
}

type k8sHTTPBackendRef struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Port      int32  `json:"port"`
}

// --- Gateway API ---

type k8sGatewayList struct {
	Items []k8sGateway `json:"items"`
}

type k8sGateway struct {
	Metadata k8sMeta          `json:"metadata"`
	Spec     k8sGatewaySpec   `json:"spec"`
	Status   k8sGatewayStatus `json:"status"`
}

type k8sGatewaySpec struct {
	GatewayClassName string               `json:"gatewayClassName"`
	Addresses        []k8sGatewayAddress  `json:"addresses"`
	Listeners        []k8sGatewayListener `json:"listeners"`
}

type k8sGatewayAddress struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

type k8sGatewayListener struct {
	Name     string `json:"name"`
	Hostname string `json:"hostname"`
	Port     int32  `json:"port"`
	Protocol string `json:"protocol"`
	TLS      *struct {
		Mode string `json:"mode"`
	} `json:"tls"`
}

type k8sGatewayStatus struct {
	Addresses []k8sGatewayAddress `json:"addresses"`
}

// --- Kubernetes ServiceAccounts ---

type k8sServiceAccountList struct {
	Items []k8sServiceAccount `json:"items"`
}

type k8sServiceAccount struct {
	Metadata                     k8sMeta `json:"metadata"`
	AutomountServiceAccountToken *bool   `json:"automountServiceAccountToken"`
}

// --- NetworkPolicies ---

type k8sNetworkPolicyList struct {
	Items []k8sNetworkPolicy `json:"items"`
}

type k8sNetworkPolicy struct {
	Metadata k8sMeta              `json:"metadata"`
	Spec     k8sNetworkPolicySpec `json:"spec"`
}

type k8sNetworkPolicySpec struct {
	PodSelector k8sLabelSelector              `json:"podSelector"`
	Ingress     []k8sNetworkPolicyIngressRule `json:"ingress"`
	Egress      []k8sNetworkPolicyEgressRule  `json:"egress"`
	PolicyTypes []string                      `json:"policyTypes"`
}

type k8sLabelSelector struct {
	MatchLabels      map[string]string             `json:"matchLabels"`
	MatchExpressions []k8sLabelSelectorRequirement `json:"matchExpressions"`
}

type k8sLabelSelectorRequirement struct {
	Key      string   `json:"key"`
	Operator string   `json:"operator"`
	Values   []string `json:"values"`
}

type k8sNetworkPolicyIngressRule struct {
	From  []k8sNetworkPolicyPeer `json:"from"`
	Ports []k8sNetworkPolicyPort `json:"ports"`
}

type k8sNetworkPolicyEgressRule struct {
	To    []k8sNetworkPolicyPeer `json:"to"`
	Ports []k8sNetworkPolicyPort `json:"ports"`
}

type k8sNetworkPolicyPeer struct {
	PodSelector       *k8sLabelSelector `json:"podSelector"`
	NamespaceSelector *k8sLabelSelector `json:"namespaceSelector"`
}

type k8sNetworkPolicyPort struct {
	Port     any    `json:"port"` // int or string
	Protocol string `json:"protocol"`
}
