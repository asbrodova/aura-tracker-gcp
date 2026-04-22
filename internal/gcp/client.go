// Package gcp implements the ports.GCPService interface using the Google Cloud
// Go SDK. This is the only package in the module that imports GCP SDK types.
package gcp

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	container "cloud.google.com/go/container/apiv1"
	"cloud.google.com/go/logging/logadmin"
	monitoring "cloud.google.com/go/monitoring/apiv3/v2"
	"cloud.google.com/go/pubsub/v2"
	run "cloud.google.com/go/run/apiv2"
	"golang.org/x/time/rate"
	"google.golang.org/api/cloudresourcemanager/v1"
	"google.golang.org/api/option"
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

// gcpAdapter is the single concrete implementation of ports.GCPService.
// All SDK clients are initialised once at construction time via New().
type gcpAdapter struct {
	clusterMgr *container.ClusterManagerClient
	runSvc     *run.ServicesClient
	pubsub     *pubsub.Client
	logAdmin   *logadmin.Client
	metric     *monitoring.MetricClient
	crm        *cloudresourcemanager.Service
	limiter     *rate.Limiter
	callTimeout time.Duration
	log         *slog.Logger
	clientOpts  []option.ClientOption
}

// New creates a gcpAdapter, initialises all GCP SDK clients using Application
// Default Credentials, and returns it as a ports.GCPService.
//
// Call Close() when done to release gRPC connections.
func New(ctx context.Context, projectID string, opts ...Option) (*gcpAdapter, error) {
	a := &gcpAdapter{
		limiter:     rate.NewLimiter(10, 20),
		callTimeout: 30 * time.Second,
		log:         slog.Default(),
	}
	for _, o := range opts {
		o(a)
	}

	var err error

	a.clusterMgr, err = container.NewClusterManagerClient(ctx, a.clientOpts...)
	if err != nil {
		return nil, fmt.Errorf("gcp: create container client: %w", err)
	}

	a.runSvc, err = run.NewServicesClient(ctx, a.clientOpts...)
	if err != nil {
		return nil, fmt.Errorf("gcp: create run services client: %w", err)
	}

	a.pubsub, err = pubsub.NewClient(ctx, projectID, a.clientOpts...)
	if err != nil {
		return nil, fmt.Errorf("gcp: create pubsub client: %w", err)
	}

	a.logAdmin, err = logadmin.NewClient(ctx, projectID, a.clientOpts...)
	if err != nil {
		return nil, fmt.Errorf("gcp: create log admin client: %w", err)
	}

	a.metric, err = monitoring.NewMetricClient(ctx, a.clientOpts...)
	if err != nil {
		return nil, fmt.Errorf("gcp: create metric client: %w", err)
	}

	httpOpts := make([]option.ClientOption, len(a.clientOpts))
	copy(httpOpts, a.clientOpts)
	a.crm, err = cloudresourcemanager.NewService(ctx, httpOpts...)
	if err != nil {
		return nil, fmt.Errorf("gcp: create cloudresourcemanager client: %w", err)
	}

	return a, nil
}

// Close releases all underlying connections.
func (a *gcpAdapter) Close() error {
	var errs []error
	if err := a.clusterMgr.Close(); err != nil {
		errs = append(errs, fmt.Errorf("close container client: %w", err))
	}
	if err := a.runSvc.Close(); err != nil {
		errs = append(errs, fmt.Errorf("close run client: %w", err))
	}
	if err := a.pubsub.Close(); err != nil {
		errs = append(errs, fmt.Errorf("close pubsub client: %w", err))
	}
	if err := a.logAdmin.Close(); err != nil {
		errs = append(errs, fmt.Errorf("close log admin client: %w", err))
	}
	if err := a.metric.Close(); err != nil {
		errs = append(errs, fmt.Errorf("close metric client: %w", err))
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
