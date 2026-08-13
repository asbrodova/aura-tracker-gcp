package gcp

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/time/rate"
	"google.golang.org/api/option"
	"google.golang.org/grpc"
)

type countingRoundTripper struct {
	calls atomic.Int32
}

func (t *countingRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	t.calls.Add(1)
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader("ok")),
		Header:     make(http.Header),
	}, nil
}

func TestRateLimitedRoundTripperEnforcesAtRequestBoundary(t *testing.T) {
	base := &countingRoundTripper{}
	limiter := rate.NewLimiter(rate.Every(time.Hour), 1)
	if !limiter.Allow() {
		t.Fatal("could not drain initial limiter token")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://example.test", nil)
	if err != nil {
		t.Fatal(err)
	}
	transport := &rateLimitedRoundTripper{base: base, limiter: limiter}
	resp, err := transport.RoundTrip(req)
	if resp != nil {
		_ = resp.Body.Close()
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("RoundTrip error = %v, want context cancellation", err)
	}
	if got := base.calls.Load(); got != 0 {
		t.Fatalf("base transport was called %d times after limiter rejection", got)
	}
}

func TestNewRateLimitedHTTPClientPreservesConfiguredClientAndEndpoint(t *testing.T) {
	base := &countingRoundTripper{}
	client, endpoint, err := newRateLimitedHTTPClient(context.Background(), rate.NewLimiter(rate.Inf, 1), []option.ClientOption{
		option.WithHTTPClient(&http.Client{Transport: base}),
		option.WithEndpoint("https://emulator.example/v1/"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if endpoint != "https://emulator.example/v1/" {
		t.Fatalf("endpoint = %q", endpoint)
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://emulator.example/v1/resources", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if got := base.calls.Load(); got != 1 {
		t.Fatalf("base transport calls = %d, want 1", got)
	}
}

func TestGRPCRateLimitInterceptorsRejectBeforeInvocation(t *testing.T) {
	limiter := rate.NewLimiter(rate.Every(time.Hour), 1)
	if !limiter.Allow() {
		t.Fatal("could not drain initial limiter token")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	unaryCalled := false
	unary := unaryRateLimitInterceptor(limiter)
	err := unary(ctx, "/test.Service/Get", nil, nil, nil, func(context.Context, string, any, any, *grpc.ClientConn, ...grpc.CallOption) error {
		unaryCalled = true
		return nil
	})
	if !errors.Is(err, context.Canceled) || unaryCalled {
		t.Fatalf("unary error=%v called=%v", err, unaryCalled)
	}

	streamCalled := false
	stream := streamRateLimitInterceptor(limiter)
	_, err = stream(ctx, &grpc.StreamDesc{}, nil, "/test.Service/List", func(context.Context, *grpc.StreamDesc, *grpc.ClientConn, string, ...grpc.CallOption) (grpc.ClientStream, error) {
		streamCalled = true
		return nil, nil
	})
	if !errors.Is(err, context.Canceled) || streamCalled {
		t.Fatalf("stream error=%v called=%v", err, streamCalled)
	}
}
