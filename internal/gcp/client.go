// Package gcp implements the ports.GCPService interface using the Google Cloud
// Go SDK. This is the only package in the module that imports GCP SDK types.
package gcp

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"cloud.google.com/go/bigquery"
	cloudtasks "cloud.google.com/go/cloudtasks/apiv2"
	container "cloud.google.com/go/container/apiv1"
	eventarc "cloud.google.com/go/eventarc/apiv1"
	"cloud.google.com/go/logging/logadmin"
	monitoring "cloud.google.com/go/monitoring/apiv3/v2"
	"cloud.google.com/go/pubsub/v2"
	recommender "cloud.google.com/go/recommender/apiv1"
	run "cloud.google.com/go/run/apiv2"
	scheduler "cloud.google.com/go/scheduler/apiv1"
	secretmanager "cloud.google.com/go/secretmanager/apiv1"
	"cloud.google.com/go/storage"
	workflows "cloud.google.com/go/workflows/apiv1"
	workflowexecutions "cloud.google.com/go/workflows/executions/apiv1"
	"golang.org/x/time/rate"
	"google.golang.org/api/alloydb/v1"
	"google.golang.org/api/apigateway/v1beta"
	"google.golang.org/api/artifactregistry/v1"
	"google.golang.org/api/cloudbuild/v1"
	"google.golang.org/api/cloudfunctions/v1"
	"google.golang.org/api/cloudresourcemanager/v1"
	crmv3 "google.golang.org/api/cloudresourcemanager/v3"
	"google.golang.org/api/cloudtrace/v1"
	"google.golang.org/api/compute/v1"
	"google.golang.org/api/firestore/v1"
	"google.golang.org/api/iam/v1"
	monitoringv1 "google.golang.org/api/monitoring/v1"
	monitoringv3 "google.golang.org/api/monitoring/v3"
	"google.golang.org/api/option"
	"google.golang.org/api/redis/v1"
	"google.golang.org/api/servicedirectory/v1"
	"google.golang.org/api/spanner/v1"
	"google.golang.org/api/sqladmin/v1"
	"google.golang.org/api/vpcaccess/v1"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
	"github.com/asbrodova/aura-tracker-gcp/ports"
)

// Option configures a gcpAdapter created by New.
type Option func(*gcpAdapter)

// WithRateLimit sets the token-bucket rate limiter.
// Default: 10 requests/second, burst 20.
func WithRateLimit(rps float64, burst int) Option {
	return func(a *gcpAdapter) {
		a.limiter = rate.NewLimiter(rate.Limit(rps), burst)
	}
}

// WithCallTimeout sets the per-call context timeout applied inside every adapter method.
// Default: 30 seconds.
func WithCallTimeout(d time.Duration) Option {
	return func(a *gcpAdapter) { a.callTimeout = d }
}

// WithLogger sets the structured logger used by the adapter.
func WithLogger(l *slog.Logger) Option {
	return func(a *gcpAdapter) { a.log = l }
}

// WithClientOptions passes extra google.golang.org/api/option.ClientOption values
// (e.g. WithEndpoint for emulator testing).
func WithClientOptions(opts ...option.ClientOption) Option {
	return func(a *gcpAdapter) { a.clientOpts = opts }
}

// WithRecommender enables the Cloud Recommender API integration used to enrich
// Aura Score efficiency signals with GCP's pre-computed idle / over-provisioned
// recommendations and their estimated monthly cost savings.
//
// On by default. Set RECOMMENDER_ENABLED=false to disable. Requires the service
// account to have the Recommender Viewer role (or individual recommender.*.list
// permissions listed in the README).
func WithRecommender() Option {
	return func(a *gcpAdapter) { a.enableRecommender = true }
}

// WithModules restricts GCP client initialization to only the clients required
// by the named modules. A nil or empty map initializes all clients (default behavior).
func WithModules(modules map[string]bool) Option {
	return func(a *gcpAdapter) { a.enabledModules = modules }
}

// WithTraceBackend selects the backend used by gcp_trace_list_services.
// Valid values: "trace" (Cloud Trace v1 REST, default) or "monitoring" (Monitoring metric proxy).
func WithTraceBackend(backend string) Option {
	return func(a *gcpAdapter) { a.traceBackend = backend }
}

// WithGraphTimeout sets the outer context timeout for gcp_export_serverless_graph.
// Each individual sub-call still uses the per-call callTimeout (default 30s).
// Default: 120 seconds.
func WithGraphTimeout(d time.Duration) Option {
	return func(a *gcpAdapter) { a.graphTimeout = d }
}

// gcpAdapter is the single concrete implementation of ports.GCPService.
// SDK clients are lazily initialised based on the enabledModules set.
//
// Field order: pointer/interface-sized types first (8 bytes each on amd64),
// then time.Duration (8 bytes), then slices/maps/string (24/16/16 bytes),
// then bool fields last to avoid padding between same-size groups.
type gcpAdapter struct {
	// --- GCP SDK clients (pointer-sized, 8 bytes each) ---
	clusterMgr          *container.ClusterManagerClient
	runSvc              *run.ServicesClient
	runJobs             *run.JobsClient
	runExecs            *run.ExecutionsClient
	fnGen1              *cloudfunctions.Service
	eventarcClient      *eventarc.Client
	schedulerClient     *scheduler.CloudSchedulerClient
	workflowsClient     *workflows.Client
	workflowExecClient  *workflowexecutions.Client
	tasksClient         *cloudtasks.Client
	secretMgrClient     *secretmanager.Client
	vpcAccess           *vpcaccess.Service
	sqlAdmin            *sqladmin.Service
	computeSvc          *compute.Service
	apiGatewaySvc       *apigateway.Service
	artifactRegistrySvc *artifactregistry.Service
	cloudBuildSvc       *cloudbuild.Service
	serviceDirectorySvc *servicedirectory.APIService
	traceClient         *cloudtrace.Service
	pubsub              *pubsub.Client
	logAdmin            *logadmin.Client
	metric              *monitoring.MetricClient
	crm                 *cloudresourcemanager.Service
	bq                  *bigquery.Client
	gcs                 *storage.Client
	spannerSvc          *spanner.Service
	alloydbSvc          *alloydb.Service
	firestoreSvc        *firestore.Service
	redisSvc            *redis.Service
	iamAdminSvc         *iam.Service
	monitoringSvc       *monitoringv3.Service
	monitoringV1Svc     *monitoringv1.Service
	crmV3Svc            *crmv3.Service
	rec                 *recommender.Client
	limiter             *rate.Limiter
	log                 *slog.Logger
	auraCache                 *ttlCache[models.AuraReport]
	regionsCache              *ttlCache[[]string]
	graphCache                *ttlCache[models.ServerlessGraph]
	recommenderCache          *ttlCache[[]recommenderInsight]
	recommenderQuotaExhausted atomic.Bool
	// --- time.Duration (8 bytes each) ---
	callTimeout  time.Duration
	graphTimeout time.Duration
	// --- slice / map / string (16–24 bytes each) ---
	clientOpts     []option.ClientOption
	enabledModules map[string]bool
	traceBackend   string
	// --- bool (1 byte, grouped at end to minimise padding) ---
	enableRecommender bool
}

// Compile-time assertion that gcpAdapter satisfies the full composite port.
var _ ports.GCPService = (*gcpAdapter)(nil)

// New creates a gcpAdapter, initialises the required GCP SDK clients using
// Application Default Credentials, and returns it as a ports.GCPService.
// When WithModules is provided, only the clients needed by those modules are opened.
//
// Call Close() when done to release gRPC connections.
func New(ctx context.Context, projectID string, opts ...Option) (*gcpAdapter, error) {
	a := &gcpAdapter{
		limiter:      rate.NewLimiter(10, 20),
		callTimeout:  30 * time.Second,
		graphTimeout: 120 * time.Second,
		traceBackend: "trace",
		log:          slog.Default(),
		auraCache:    newTTLCache[models.AuraReport](auraCacheTTL),
		regionsCache: newTTLCache[[]string](10 * time.Minute),
		graphCache:   newTTLCache[models.ServerlessGraph](archGraphCacheTTL),
	}
	for _, o := range opts {
		o(a)
	}

	needed := neededClients(a.enabledModules)
	var err error

	if needed[clientClusterMgr] {
		a.clusterMgr, err = container.NewClusterManagerClient(ctx, a.clientOpts...)
		if err != nil {
			return nil, fmt.Errorf("gcp: create container client: %w", err)
		}
	}

	if needed[clientRunSvc] {
		a.runSvc, err = run.NewServicesClient(ctx, a.clientOpts...)
		if err != nil {
			return nil, fmt.Errorf("gcp: create run services client: %w", err)
		}
	}

	if needed[clientRunJobs] {
		a.runJobs, err = run.NewJobsClient(ctx, a.clientOpts...)
		if err != nil {
			return nil, fmt.Errorf("gcp: create run jobs client: %w", err)
		}
		a.runExecs, err = run.NewExecutionsClient(ctx, a.clientOpts...)
		if err != nil {
			return nil, fmt.Errorf("gcp: create run executions client: %w", err)
		}
	}

	if needed[clientFunctionsV1] {
		a.fnGen1, err = cloudfunctions.NewService(ctx, a.clientOpts...)
		if err != nil {
			return nil, fmt.Errorf("gcp: create cloudfunctions v1 client: %w", err)
		}
	}

	if needed[clientEventarc] {
		a.eventarcClient, err = eventarc.NewClient(ctx, a.clientOpts...)
		if err != nil {
			return nil, fmt.Errorf("gcp: create eventarc client: %w", err)
		}
	}

	if needed[clientScheduler] {
		a.schedulerClient, err = scheduler.NewCloudSchedulerClient(ctx, a.clientOpts...)
		if err != nil {
			return nil, fmt.Errorf("gcp: create scheduler client: %w", err)
		}
	}

	if needed[clientWorkflows] {
		a.workflowsClient, err = workflows.NewClient(ctx, a.clientOpts...)
		if err != nil {
			return nil, fmt.Errorf("gcp: create workflows client: %w", err)
		}
	}

	if needed[clientWorkflowExec] {
		a.workflowExecClient, err = workflowexecutions.NewClient(ctx, a.clientOpts...)
		if err != nil {
			return nil, fmt.Errorf("gcp: create workflow executions client: %w", err)
		}
	}

	if needed[clientTasks] {
		a.tasksClient, err = cloudtasks.NewClient(ctx, a.clientOpts...)
		if err != nil {
			return nil, fmt.Errorf("gcp: create cloudtasks client: %w", err)
		}
	}

	if needed[clientSecretMgr] {
		a.secretMgrClient, err = secretmanager.NewClient(ctx, a.clientOpts...)
		if err != nil {
			return nil, fmt.Errorf("gcp: create secretmanager client: %w", err)
		}
	}

	if needed[clientTrace] {
		a.traceClient, err = cloudtrace.NewService(ctx, a.clientOpts...)
		if err != nil {
			return nil, fmt.Errorf("gcp: create cloudtrace client: %w", err)
		}
	}

	if needed[clientVPCAccess] {
		a.vpcAccess, err = vpcaccess.NewService(ctx, a.clientOpts...)
		if err != nil {
			return nil, fmt.Errorf("gcp: create vpcaccess client: %w", err)
		}
	}

	if needed[clientCompute] {
		a.computeSvc, err = compute.NewService(ctx, a.clientOpts...)
		if err != nil {
			return nil, fmt.Errorf("gcp: create compute client: %w", err)
		}
	}

	if needed[clientAPIGateway] {
		a.apiGatewaySvc, err = apigateway.NewService(ctx, a.clientOpts...)
		if err != nil {
			return nil, fmt.Errorf("gcp: create apigateway client: %w", err)
		}
	}

	if needed[clientArtifactRegistry] {
		a.artifactRegistrySvc, err = artifactregistry.NewService(ctx, a.clientOpts...)
		if err != nil {
			return nil, fmt.Errorf("gcp: create artifactregistry client: %w", err)
		}
	}

	if needed[clientCloudBuild] {
		a.cloudBuildSvc, err = cloudbuild.NewService(ctx, a.clientOpts...)
		if err != nil {
			return nil, fmt.Errorf("gcp: create cloudbuild client: %w", err)
		}
	}

	if needed[clientServiceDirectory] {
		a.serviceDirectorySvc, err = servicedirectory.NewService(ctx, a.clientOpts...)
		if err != nil {
			return nil, fmt.Errorf("gcp: create servicedirectory client: %w", err)
		}
	}

	if needed[clientMonitoringV3] {
		a.monitoringSvc, err = monitoringv3.NewService(ctx, a.clientOpts...)
		if err != nil {
			return nil, fmt.Errorf("gcp: create monitoring v3 client: %w", err)
		}
	}

	if needed[clientMonitoringV1] {
		a.monitoringV1Svc, err = monitoringv1.NewService(ctx, a.clientOpts...)
		if err != nil {
			return nil, fmt.Errorf("gcp: create monitoring v1 client: %w", err)
		}
	}

	if needed[clientIAMAdmin] {
		a.iamAdminSvc, err = iam.NewService(ctx, a.clientOpts...)
		if err != nil {
			return nil, fmt.Errorf("gcp: create iam client: %w", err)
		}
	}

	if needed[clientSQLAdmin] {
		a.sqlAdmin, err = sqladmin.NewService(ctx, a.clientOpts...)
		if err != nil {
			return nil, fmt.Errorf("gcp: create sqladmin client: %w", err)
		}
	}

	if needed[clientPubSub] {
		a.pubsub, err = pubsub.NewClient(ctx, projectID, a.clientOpts...)
		if err != nil {
			return nil, fmt.Errorf("gcp: create pubsub client: %w", err)
		}
	}

	if needed[clientLogAdmin] {
		a.logAdmin, err = logadmin.NewClient(ctx, projectID, a.clientOpts...)
		if err != nil {
			return nil, fmt.Errorf("gcp: create log admin client: %w", err)
		}
	}

	if needed[clientMetric] {
		a.metric, err = monitoring.NewMetricClient(ctx, a.clientOpts...)
		if err != nil {
			return nil, fmt.Errorf("gcp: create metric client: %w", err)
		}
	}

	if a.enableRecommender {
		a.rec, err = recommender.NewClient(ctx, a.clientOpts...)
		if err != nil {
			return nil, fmt.Errorf("gcp: create recommender client: %w", err)
		}
		a.recommenderCache = newTTLCache[[]recommenderInsight](12 * time.Hour)
	}

	if needed[clientCRM] {
		httpOpts := make([]option.ClientOption, len(a.clientOpts))
		copy(httpOpts, a.clientOpts)
		a.crm, err = cloudresourcemanager.NewService(ctx, httpOpts...)
		if err != nil {
			return nil, fmt.Errorf("gcp: create cloudresourcemanager client: %w", err)
		}
	}

	if needed[clientCRMv3] {
		httpOpts := make([]option.ClientOption, len(a.clientOpts))
		copy(httpOpts, a.clientOpts)
		a.crmV3Svc, err = crmv3.NewService(ctx, httpOpts...)
		if err != nil {
			return nil, fmt.Errorf("gcp: create cloudresourcemanager/v3 client: %w", err)
		}
	}

	if needed[clientBQ] {
		a.bq, err = bigquery.NewClient(ctx, projectID, a.clientOpts...)
		if err != nil {
			return nil, fmt.Errorf("gcp: create bigquery client: %w", err)
		}
	}

	if needed[clientGCS] {
		a.gcs, err = storage.NewClient(ctx, a.clientOpts...)
		if err != nil {
			return nil, fmt.Errorf("gcp: create storage client: %w", err)
		}
	}

	if needed[clientSpanner] {
		a.spannerSvc, err = spanner.NewService(ctx, a.clientOpts...)
		if err != nil {
			return nil, fmt.Errorf("gcp: create spanner client: %w", err)
		}
	}

	if needed[clientAlloyDB] {
		a.alloydbSvc, err = alloydb.NewService(ctx, a.clientOpts...)
		if err != nil {
			return nil, fmt.Errorf("gcp: create alloydb client: %w", err)
		}
	}

	if needed[clientFirestore] {
		a.firestoreSvc, err = firestore.NewService(ctx, a.clientOpts...)
		if err != nil {
			return nil, fmt.Errorf("gcp: create firestore client: %w", err)
		}
	}

	if needed[clientMemorystore] {
		a.redisSvc, err = redis.NewService(ctx, a.clientOpts...)
		if err != nil {
			return nil, fmt.Errorf("gcp: create memorystore client: %w", err)
		}
	}

	return a, nil
}

// Close releases all underlying connections. Nil-safe: only closes clients that were initialized.
func (a *gcpAdapter) Close() error {
	var errs []error
	if a.clusterMgr != nil {
		if err := a.clusterMgr.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close container client: %w", err))
		}
	}
	if a.runSvc != nil {
		if err := a.runSvc.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close run client: %w", err))
		}
	}
	if a.runJobs != nil {
		if err := a.runJobs.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close run jobs client: %w", err))
		}
	}
	if a.runExecs != nil {
		if err := a.runExecs.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close run executions client: %w", err))
		}
	}
	if a.eventarcClient != nil {
		if err := a.eventarcClient.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close eventarc client: %w", err))
		}
	}
	if a.schedulerClient != nil {
		if err := a.schedulerClient.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close scheduler client: %w", err))
		}
	}
	if a.workflowsClient != nil {
		if err := a.workflowsClient.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close workflows client: %w", err))
		}
	}
	if a.workflowExecClient != nil {
		if err := a.workflowExecClient.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close workflow executions client: %w", err))
		}
	}
	if a.tasksClient != nil {
		if err := a.tasksClient.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close cloudtasks client: %w", err))
		}
	}
	if a.secretMgrClient != nil {
		if err := a.secretMgrClient.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close secretmanager client: %w", err))
		}
	}
	if a.pubsub != nil {
		if err := a.pubsub.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close pubsub client: %w", err))
		}
	}
	if a.logAdmin != nil {
		if err := a.logAdmin.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close log admin client: %w", err))
		}
	}
	if a.metric != nil {
		if err := a.metric.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close metric client: %w", err))
		}
	}
	if a.rec != nil {
		if err := a.rec.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close recommender client: %w", err))
		}
	}
	if a.bq != nil {
		if err := a.bq.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close bigquery client: %w", err))
		}
	}
	if a.gcs != nil {
		if err := a.gcs.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close storage client: %w", err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("gcp adapter close: %v", errs)
	}
	return nil
}

// withTimeout wraps ctx with a.callTimeout. Caller must always defer cancel().
func (a *gcpAdapter) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, a.callTimeout)
}

// rateWait waits for the token bucket before a GCP call. Returns an error if
// ctx is cancelled while waiting.
func (a *gcpAdapter) rateWait(ctx context.Context, op string) error {
	if err := a.limiter.Wait(ctx); err != nil {
		return fmt.Errorf("%s: rate limiter: %w", op, err)
	}
	return nil
}
