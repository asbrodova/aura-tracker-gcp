package gcp

import (
	"context"
	"fmt"
	"net/http"

	"golang.org/x/time/rate"
	"google.golang.org/api/option"
	htransport "google.golang.org/api/transport/http"
	"google.golang.org/grpc"
)

const cloudPlatformScope = "https://www.googleapis.com/auth/cloud-platform"

// rateLimitedRoundTripper applies the adapter-wide budget at the real HTTP
// request boundary. Pagination, polling, retries, and nested collectors are
// therefore counted individually rather than once per public adapter method.
type rateLimitedRoundTripper struct {
	base    http.RoundTripper
	limiter *rate.Limiter
}

func (t *rateLimitedRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if req == nil {
		return nil, fmt.Errorf("gcp transport: nil HTTP request")
	}
	if t == nil || t.limiter == nil {
		return nil, fmt.Errorf("gcp transport: rate limiter is unavailable")
	}
	if err := t.limiter.Wait(req.Context()); err != nil {
		return nil, fmt.Errorf("gcp transport: rate limiter: %w", err)
	}
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(req)
}

func newRateLimitedHTTPClient(ctx context.Context, limiter *rate.Limiter, opts []option.ClientOption) (*http.Client, string, error) {
	// Generated REST clients supply their own default scopes. This bootstrap
	// client needs one explicit broad scope because it is shared by those clients.
	bootstrapOpts := make([]option.ClientOption, 0, len(opts)+1)
	bootstrapOpts = append(bootstrapOpts, option.WithScopes(cloudPlatformScope))
	bootstrapOpts = append(bootstrapOpts, opts...)
	client, endpoint, err := htransport.NewClient(ctx, bootstrapOpts...)
	if err != nil {
		return nil, "", err
	}
	clone := *client
	clone.Transport = &rateLimitedRoundTripper{base: client.Transport, limiter: limiter}
	return &clone, endpoint, nil
}

func rateLimitedHTTPOptions(client *http.Client, endpoint string) []option.ClientOption {
	opts := []option.ClientOption{option.WithHTTPClient(client)}
	if endpoint != "" {
		opts = append(opts, option.WithEndpoint(endpoint))
	}
	return opts
}

func rateLimitedGRPCOptions(base []option.ClientOption, limiter *rate.Limiter) []option.ClientOption {
	opts := append([]option.ClientOption(nil), base...)
	return append(opts,
		option.WithGRPCDialOption(grpc.WithChainUnaryInterceptor(unaryRateLimitInterceptor(limiter))),
		option.WithGRPCDialOption(grpc.WithChainStreamInterceptor(streamRateLimitInterceptor(limiter))),
	)
}

func unaryRateLimitInterceptor(limiter *rate.Limiter) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, connection *grpc.ClientConn, invoke grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		if limiter == nil {
			return fmt.Errorf("%s: rate limiter is unavailable", method)
		}
		if err := limiter.Wait(ctx); err != nil {
			return fmt.Errorf("%s: rate limiter: %w", method, err)
		}
		return invoke(ctx, method, req, reply, connection, opts...)
	}
}

func streamRateLimitInterceptor(limiter *rate.Limiter) grpc.StreamClientInterceptor {
	return func(ctx context.Context, description *grpc.StreamDesc, connection *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		if limiter == nil {
			return nil, fmt.Errorf("%s: rate limiter is unavailable", method)
		}
		if err := limiter.Wait(ctx); err != nil {
			return nil, fmt.Errorf("%s: rate limiter: %w", method, err)
		}
		return streamer(ctx, description, connection, method, opts...)
	}
}
