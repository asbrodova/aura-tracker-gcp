package gcp

import (
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"google.golang.org/api/googleapi"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/asbrodova/aura-tracker-gcp/ports"
)

func TestNextRecommenderDailyResetUsesPacificCalendar(t *testing.T) {
	tests := []struct {
		name string
		now  time.Time
		want time.Time
	}{
		{
			name: "winter",
			now:  time.Date(2026, time.January, 10, 20, 0, 0, 0, time.UTC),
			want: time.Date(2026, time.January, 11, 8, 0, 0, 0, time.UTC),
		},
		{
			name: "summer",
			now:  time.Date(2026, time.July, 10, 20, 0, 0, 0, time.UTC),
			want: time.Date(2026, time.July, 11, 7, 0, 0, 0, time.UTC),
		},
		{
			name: "spring forward is a 23 hour local day",
			now:  time.Date(2026, time.March, 8, 8, 30, 0, 0, time.UTC),
			want: time.Date(2026, time.March, 9, 7, 0, 0, 0, time.UTC),
		},
		{
			name: "fall back is a 25 hour local day",
			now:  time.Date(2026, time.November, 1, 7, 30, 0, 0, time.UTC),
			want: time.Date(2026, time.November, 2, 8, 0, 0, 0, time.UTC),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := nextRecommenderDailyReset(test.now); !got.Equal(test.want) {
				t.Fatalf("reset = %s, want %s", got, test.want)
			}
		})
	}
}

func TestRecommenderQuotaGateReopensAtDeadlineAndScopesByID(t *testing.T) {
	now := time.Date(2026, time.August, 13, 12, 0, 0, 0, time.UTC)
	gate := recommenderQuotaGate{now: func() time.Time { return now }}
	daily := status.Error(codes.ResourceExhausted, "ListRecommendationsPerDayPerOrganization exceeded")
	blocked, changed := gate.trip("test", "recommender-a", daily)
	if !changed || blocked.Window != recommenderQuotaWindowDaily {
		t.Fatalf("trip = (%+v, %v)", blocked, changed)
	}
	if quotaErr, _, _ := gate.check("test", "recommender-a"); quotaErr == nil {
		t.Fatal("same recommender was not blocked")
	}
	if quotaErr, _, _ := gate.check("test", "recommender-b"); quotaErr != nil {
		t.Fatalf("independent recommender was blocked: %v", quotaErr)
	}

	now = blocked.RetryAt.Add(-time.Nanosecond)
	if quotaErr, _, _ := gate.check("test", "recommender-a"); quotaErr == nil {
		t.Fatal("gate reopened before the deadline")
	}
	now = blocked.RetryAt
	if quotaErr, reopened, generation := gate.check("test", "recommender-a"); quotaErr != nil || reopened == nil || generation == 0 {
		t.Fatalf("at deadline: quota=%v reopened=%v generation=%d", quotaErr, reopened, generation)
	}
}

func TestRecommenderQuotaGateHonorsRetryInfoAndDoesNotShorten(t *testing.T) {
	now := time.Date(2026, time.August, 13, 12, 0, 0, 0, time.UTC)
	gate := recommenderQuotaGate{now: func() time.Time { return now }}
	longStatus, err := status.New(codes.ResourceExhausted, "quota exhausted").WithDetails(
		&errdetails.RetryInfo{RetryDelay: durationpb.New(10 * time.Minute)},
	)
	if err != nil {
		t.Fatal(err)
	}
	longBlock, _ := gate.trip("test", "rec", longStatus.Err())
	shortStatus, err := status.New(codes.ResourceExhausted, "quota exhausted").WithDetails(
		&errdetails.RetryInfo{RetryDelay: durationpb.New(time.Minute)},
	)
	if err != nil {
		t.Fatal(err)
	}
	shortBlock, changed := gate.trip("test", "rec", shortStatus.Err())
	if changed || !shortBlock.RetryAt.Equal(longBlock.RetryAt) {
		t.Fatalf("active block shortened: first=%s second=%s changed=%v", longBlock.RetryAt, shortBlock.RetryAt, changed)
	}
}

func TestRecommenderQuotaGatePreventsSuccessfulIteratorABA(t *testing.T) {
	now := time.Date(2026, time.August, 13, 12, 0, 0, 0, time.UTC)
	gate := recommenderQuotaGate{now: func() time.Time { return now }}
	initial, _ := gate.trip("test", "rec", status.Error(codes.ResourceExhausted, "quota exhausted"))
	now = initial.RetryAt
	_, _, oldGeneration := gate.check("test", "rec")
	gate.succeed("rec", oldGeneration)
	newBlock, _ := gate.trip("test", "rec", status.Error(codes.ResourceExhausted, "quota exhausted"))

	// A late successful item from the old iterator must not clear the new block.
	gate.succeed("rec", oldGeneration)
	quotaErr, _, _ := gate.check("test", "rec")
	if quotaErr == nil || !quotaErr.RetryAt.Equal(newBlock.RetryAt) {
		t.Fatalf("old success cleared newer block: got=%v want retry_at=%s", quotaErr, newBlock.RetryAt)
	}
}

func TestRecommenderQuotaGateConcurrentUse(t *testing.T) {
	now := time.Date(2026, time.August, 13, 12, 0, 0, 0, time.UTC)
	gate := recommenderQuotaGate{now: func() time.Time { return now }}
	cause := status.Error(codes.ResourceExhausted, "quota exhausted")
	var wg sync.WaitGroup
	for worker := 0; worker < 16; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				_, _ = gate.trip("test", "rec", cause)
				quotaErr, _, generation := gate.check("test", "rec")
				if quotaErr == nil {
					gate.succeed("rec", generation)
				}
			}
		}()
	}
	wg.Wait()
}

func TestQuotaGateReturnsTypedError(t *testing.T) {
	gate := recommenderQuotaGate{}
	_, _ = gate.trip("test", "rec", status.Error(codes.ResourceExhausted, "quota exhausted"))
	got, _, _ := gate.check("test", "rec")
	var typed *ports.RecommenderQuotaExhaustedError
	if !errors.As(got, &typed) || typed.RecommenderID != "rec" || typed.RetryAt.IsZero() {
		t.Fatalf("typed error = %+v", got)
	}
}

func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, time.August, 13, 12, 0, 0, 0, time.UTC)
	if got, ok := parseRetryAfter("37", now); !ok || !got.Equal(now.Add(37*time.Second)) {
		t.Fatalf("seconds retry-after = %s, %v", got, ok)
	}
	want := now.Add(2 * time.Minute)
	if got, ok := parseRetryAfter(want.Format(http.TimeFormat), now); !ok || !got.Equal(want) {
		t.Fatalf("date retry-after = %s, %v", got, ok)
	}
}

func TestUnknownQuotaBackoffIsBounded(t *testing.T) {
	now := time.Date(2026, time.August, 13, 6, 59, 30, 0, time.UTC) // 30s before Pacific midnight.
	gate := recommenderQuotaGate{now: func() time.Time { return now }}
	cause := &googleapi.Error{Code: http.StatusTooManyRequests, Message: "quota exhausted"}
	block, _ := gate.trip("test", "rec", cause)
	wantCap := nextRecommenderDailyReset(now)
	if block.Window != recommenderQuotaWindowUnknown || !block.RetryAt.Equal(wantCap) {
		t.Fatalf("unknown block = %+v, want cap %s", block, wantCap)
	}
}

func TestOnlyQuotaErrorsTripRecommenderGate(t *testing.T) {
	tests := []struct {
		err  error
		want bool
	}{
		{status.Error(codes.ResourceExhausted, "quota"), true},
		{&googleapi.Error{Code: http.StatusTooManyRequests}, true},
		{status.Error(codes.PermissionDenied, "denied"), false},
		{status.Error(codes.Unavailable, "unavailable"), false},
		{status.Error(codes.DeadlineExceeded, "deadline"), false},
		{status.Error(codes.NotFound, "missing"), false},
	}
	for _, test := range tests {
		if got := isRecommenderQuotaError(test.err); got != test.want {
			t.Errorf("isRecommenderQuotaError(%v) = %v, want %v", test.err, got, test.want)
		}
	}
}

func TestRateLimitErrorInfoUsesMinuteWindow(t *testing.T) {
	now := time.Date(2026, time.August, 13, 12, 0, 0, 0, time.UTC)
	quotaStatus, err := status.New(codes.ResourceExhausted, "quota exhausted").WithDetails(&errdetails.ErrorInfo{
		Reason: "RATE_LIMIT_EXCEEDED",
		Domain: "googleapis.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	window, retryAt := classifyRecommenderQuota(quotaStatus.Err(), now)
	if window != recommenderQuotaWindowMinute || !retryAt.Equal(now.Add(time.Minute)) {
		t.Fatalf("classification = (%q, %s), want (%q, %s)", window, retryAt, recommenderQuotaWindowMinute, now.Add(time.Minute))
	}
}
