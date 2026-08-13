package gcp

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	_ "time/tzdata"

	"github.com/googleapis/gax-go/v2/apierror"
	"google.golang.org/api/googleapi"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/asbrodova/aura-tracker-gcp/ports"
)

const (
	recommenderQuotaWindowMinute  = "minute"
	recommenderQuotaWindowDaily   = "daily"
	recommenderQuotaWindowUnknown = "unknown"
)

var (
	recommenderPacificLocation = mustLoadRecommenderLocation("America/Los_Angeles")
	unknownQuotaBackoffs       = [...]time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute, 2 * time.Hour}
)

type recommenderQuotaBlock struct {
	until           time.Time
	window          string
	unknownFailures int
	generation      uint64
}

// recommenderQuotaGate is an in-process, per-recommender circuit breaker.
// The zero value is ready for use. Expiry is lazy so a sleeping Cloud Run
// instance needs no timer or lifecycle hook to resume after a quota reset.
type recommenderQuotaGate struct {
	mu     sync.Mutex
	blocks map[string]recommenderQuotaBlock
	now    func() time.Time
}

func (g *recommenderQuotaGate) currentTime() time.Time {
	if g.now != nil {
		return g.now().UTC()
	}
	return time.Now().UTC()
}

// check returns a typed error while recommenderID is blocked. The bool reports
// that an expired block was reopened by this call, allowing transition-only
// logging without a background goroutine.
func (g *recommenderQuotaGate) check(op, recommenderID string) (*ports.RecommenderQuotaExhaustedError, *ports.RecommenderQuotaExhaustedError, uint64) {
	now := g.currentTime()
	g.mu.Lock()
	defer g.mu.Unlock()
	block, ok := g.blocks[recommenderID]
	if !ok || block.until.IsZero() {
		return nil, nil, block.generation
	}
	if now.Before(block.until) {
		return recommenderQuotaError(op, recommenderID, block), nil, block.generation
	}
	// Retain the unknown-failure count until a complete request succeeds. This
	// gives unclassified 429s bounded backoff without repeatedly starting at 1m.
	reopened := recommenderQuotaError(op, recommenderID, block)
	block.until = time.Time{}
	g.blocks[recommenderID] = block
	return nil, reopened, block.generation
}

func (g *recommenderQuotaGate) trip(op, recommenderID string, cause error) (*ports.RecommenderQuotaExhaustedError, bool) {
	now := g.currentTime()
	window, retryAt := classifyRecommenderQuota(cause, now)

	g.mu.Lock()
	defer g.mu.Unlock()
	if g.blocks == nil {
		g.blocks = make(map[string]recommenderQuotaBlock)
	}
	previous := g.blocks[recommenderID]
	block := recommenderQuotaBlock{window: window, generation: previous.generation + 1}
	if window == recommenderQuotaWindowUnknown {
		block.unknownFailures = previous.unknownFailures + 1
		if retryAt.IsZero() {
			retryAt = now.Add(unknownQuotaBackoff(block.unknownFailures))
			if dailyReset := nextRecommenderDailyReset(now); retryAt.After(dailyReset) {
				retryAt = dailyReset
			}
		}
	}
	block.until = retryAt.UTC()

	changed := true
	if now.Before(previous.until) && !block.until.After(previous.until) {
		// A concurrent/shorter quota response must never shorten an active block.
		block = previous
		changed = false
	}
	g.blocks[recommenderID] = block
	return recommenderQuotaError(op, recommenderID, block), changed
}

func (g *recommenderQuotaGate) succeed(recommenderID string, observedGeneration uint64) {
	g.mu.Lock()
	if block, ok := g.blocks[recommenderID]; ok && block.generation == observedGeneration {
		block.until = time.Time{}
		block.window = ""
		block.unknownFailures = 0
		g.blocks[recommenderID] = block
	}
	g.mu.Unlock()
}

func recommenderQuotaError(op, recommenderID string, block recommenderQuotaBlock) *ports.RecommenderQuotaExhaustedError {
	return &ports.RecommenderQuotaExhaustedError{
		Op:            op,
		RecommenderID: recommenderID,
		RetryAt:       block.until.UTC(),
		Window:        block.window,
	}
}

func unknownQuotaBackoff(failures int) time.Duration {
	if failures <= 0 {
		failures = 1
	}
	if failures > len(unknownQuotaBackoffs) {
		return 24 * time.Hour
	}
	return unknownQuotaBackoffs[failures-1]
}

func nextRecommenderDailyReset(now time.Time) time.Time {
	local := now.In(recommenderPacificLocation)
	return time.Date(local.Year(), local.Month(), local.Day()+1, 0, 0, 0, 0, recommenderPacificLocation).UTC()
}

func mustLoadRecommenderLocation(name string) *time.Location {
	location, err := time.LoadLocation(name)
	if err != nil {
		panic("load Recommender quota timezone: " + err.Error())
	}
	return location
}

// classifyRecommenderQuota returns the best known quota window and retry time.
// It prefers server-provided RetryInfo/Retry-After, then structured/message
// evidence. An unclassified response is handled by the gate's bounded backoff.
func classifyRecommenderQuota(err error, now time.Time) (string, time.Time) {
	window := recommenderQuotaWindowUnknown
	var evidence strings.Builder
	var retryAt time.Time

	if st, ok := status.FromError(err); ok {
		evidence.WriteString(st.Message())
		for _, detail := range st.Details() {
			switch value := detail.(type) {
			case *errdetails.RetryInfo:
				if delay := value.GetRetryDelay(); delay != nil && delay.AsDuration() > 0 {
					retryAt = now.Add(delay.AsDuration())
				}
			case *errdetails.QuotaFailure:
				for _, violation := range value.GetViolations() {
					evidence.WriteByte(' ')
					evidence.WriteString(violation.GetSubject())
					evidence.WriteByte(' ')
					evidence.WriteString(violation.GetDescription())
				}
			case *errdetails.ErrorInfo:
				evidence.WriteByte(' ')
				evidence.WriteString(value.GetReason())
				for key, metadata := range value.GetMetadata() {
					evidence.WriteByte(' ')
					evidence.WriteString(key)
					evidence.WriteByte(' ')
					evidence.WriteString(metadata)
				}
			}
		}
	}

	var restErr *googleapi.Error
	if errors.As(err, &restErr) {
		evidence.WriteByte(' ')
		evidence.WriteString(restErr.Message)
		evidence.WriteByte(' ')
		evidence.WriteString(restErr.Body)
		for _, item := range restErr.Errors {
			evidence.WriteByte(' ')
			evidence.WriteString(item.Reason)
			evidence.WriteByte(' ')
			evidence.WriteString(item.Message)
		}
		if parsed, ok := parseRetryAfter(restErr.Header.Get("Retry-After"), now); ok {
			retryAt = parsed
		}
	}
	var parsedAPIErr *apierror.APIError
	if !errors.As(err, &parsedAPIErr) && restErr != nil {
		parsedAPIErr, _ = apierror.FromError(restErr)
	}
	if parsedAPIErr != nil {
		details := parsedAPIErr.Details()
		if details.RetryInfo != nil {
			if delay := details.RetryInfo.GetRetryDelay(); delay != nil && delay.AsDuration() > 0 {
				retryAt = now.Add(delay.AsDuration())
			}
		}
		if details.QuotaFailure != nil {
			for _, violation := range details.QuotaFailure.GetViolations() {
				evidence.WriteByte(' ')
				evidence.WriteString(violation.GetSubject())
				evidence.WriteByte(' ')
				evidence.WriteString(violation.GetDescription())
			}
		}
		if details.ErrorInfo != nil {
			evidence.WriteByte(' ')
			evidence.WriteString(details.ErrorInfo.GetReason())
			for key, metadata := range details.ErrorInfo.GetMetadata() {
				evidence.WriteByte(' ')
				evidence.WriteString(key)
				evidence.WriteByte(' ')
				evidence.WriteString(metadata)
			}
		}
	}

	normalized := strings.ToLower(evidence.String())
	switch {
	case strings.Contains(normalized, "daily"), strings.Contains(normalized, "per day"), strings.Contains(normalized, "per_day"), strings.Contains(normalized, "per-day"), strings.Contains(normalized, "perday"):
		window = recommenderQuotaWindowDaily
	case strings.Contains(normalized, "per minute"), strings.Contains(normalized, "per_minute"), strings.Contains(normalized, "per-minute"), strings.Contains(normalized, "perminute"), strings.Contains(normalized, "rate limit"), strings.Contains(normalized, "rate_limit"), strings.Contains(normalized, "ratelimit"):
		window = recommenderQuotaWindowMinute
	}
	if !retryAt.IsZero() {
		return window, retryAt.UTC()
	}
	switch window {
	case recommenderQuotaWindowDaily:
		return window, nextRecommenderDailyReset(now)
	case recommenderQuotaWindowMinute:
		return window, now.Add(time.Minute)
	default:
		return window, time.Time{}
	}
}

func parseRetryAfter(value string, now time.Time) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, false
	}
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil && seconds >= 0 {
		return now.Add(time.Duration(seconds) * time.Second).UTC(), true
	}
	parsed, err := http.ParseTime(value)
	if err != nil || !parsed.After(now) {
		return time.Time{}, false
	}
	return parsed.UTC(), true
}

func isRecommenderQuotaError(err error) bool {
	if status.Code(err) == codes.ResourceExhausted {
		return true
	}
	var restErr *googleapi.Error
	return errors.As(err, &restErr) && restErr.Code == http.StatusTooManyRequests
}

func (a *gcpAdapter) checkRecommenderQuota(op, recommenderID string) (uint64, error) {
	quotaErr, reopened, generation := a.recommenderQuota.check(op, recommenderID)
	if reopened != nil && a.log != nil {
		a.log.Info("recommender quota circuit reopened",
			"op", op,
			"recommender_id", recommenderID,
			"window", reopened.Window,
			"retry_at", reopened.RetryAt.Format(time.RFC3339),
		)
	}
	if quotaErr != nil {
		return 0, quotaErr
	}
	return generation, nil
}

func (a *gcpAdapter) tripRecommenderQuota(op, recommenderID string, cause error) error {
	quotaErr, changed := a.recommenderQuota.trip(op, recommenderID, cause)
	if changed && a.log != nil {
		a.log.Warn("recommender quota circuit opened",
			"op", op,
			"recommender_id", recommenderID,
			"window", quotaErr.Window,
			"retry_at", quotaErr.RetryAt.Format(time.RFC3339),
		)
	}
	return quotaErr
}
