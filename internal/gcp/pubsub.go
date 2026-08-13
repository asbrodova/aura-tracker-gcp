package gcp

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"cloud.google.com/go/pubsub/v2/apiv1/pubsubpb"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

const (
	pubsubHealthMetricLookback = 10 * time.Minute
	pubsubBacklogThreshold     = 10000
	pubsubOldestAgeThreshold   = 5 * time.Minute
)

func (a *gcpAdapter) ListTopics(ctx context.Context, req models.ListTopicsRequest) (models.ListTopicsResponse, error) {
	if err := a.rateWait(ctx, "pubsub.ListTopics"); err != nil {
		return models.ListTopicsResponse{}, err
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	project := fmt.Sprintf("projects/%s", req.ProjectID)
	it := a.pubsub.TopicAdminClient.ListTopics(ctx, &pubsubpb.ListTopicsRequest{
		Project: project, PageSize: maxUnpagedInventoryItems,
	})

	// Build subscription counts with one bounded project-wide scan. The former
	// per-topic scan could issue up to one million iterator calls for a large
	// project (1,000 topics x 1,000 subscriptions each).
	subscriptionCounts := make(map[string]int)
	subscriptionCountsTruncated := false
	subIt := a.pubsub.SubscriptionAdminClient.ListSubscriptions(ctx, &pubsubpb.ListSubscriptionsRequest{
		Project: project, PageSize: maxUnpagedInventoryItems,
	})
	for scanned := 0; ; scanned++ {
		subscription, err := subIt.Next()
		if isIteratorDone(err) {
			break
		}
		if err != nil {
			subscriptionCountsTruncated = true
			break
		}
		if scanned >= maxUnpagedInventoryItems {
			subscriptionCountsTruncated = true
			break
		}
		subscriptionCounts[subscription.Topic]++
	}

	var topics []models.TopicSummary
	truncated := false
	for {
		t, err := it.Next()
		if isIteratorDone(err) {
			break
		}
		if err != nil {
			return models.ListTopicsResponse{}, wrapGCPError("pubsub.ListTopics", err)
		}
		if len(topics) >= maxUnpagedInventoryItems {
			truncated = true
			break
		}

		topics = append(topics, models.TopicSummary{
			Name:                       t.Name,
			Labels:                     t.Labels,
			SubscriptionCount:          subscriptionCounts[t.Name],
			SubscriptionCountTruncated: subscriptionCountsTruncated,
		})
	}
	if topics == nil {
		topics = []models.TopicSummary{}
	}
	return models.ListTopicsResponse{Topics: topics, Truncated: truncated}, nil
}

func (a *gcpAdapter) InspectTopicHealth(ctx context.Context, req models.InspectTopicHealthRequest) (models.TopicHealthReport, error) {
	if err := a.rateWait(ctx, "pubsub.InspectTopicHealth"); err != nil {
		return models.TopicHealthReport{}, err
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	topicName := fmt.Sprintf("projects/%s/topics/%s", req.ProjectID, req.TopicName)

	_, err := a.pubsub.TopicAdminClient.GetTopic(ctx, &pubsubpb.GetTopicRequest{Topic: topicName})
	if err != nil {
		if isGRPCNotFound(err) {
			return models.TopicHealthReport{
				TopicName: req.TopicName,
				Exists:    false,
				Healthy:   false,
				Issues:    []string{fmt.Sprintf("topic %q does not exist", req.TopicName)},
			}, nil
		}
		return models.TopicHealthReport{}, wrapGCPError("pubsub.InspectTopicHealth.getTopic", err)
	}

	// List subscriptions and retain the configured ack deadline separately from
	// runtime lag. AckDeadlineSeconds is a delivery setting, not the age of the
	// oldest unacknowledged message.
	subIt := a.pubsub.TopicAdminClient.ListTopicSubscriptions(ctx, &pubsubpb.ListTopicSubscriptionsRequest{
		Topic: topicName, PageSize: maxUnpagedInventoryItems,
	})

	var lags []models.SubscriptionLag
	var issues []string

	subscriptionScanTruncated := false
	for scanned := 0; ; scanned++ {
		subName, err := subIt.Next()
		if isIteratorDone(err) {
			break
		}
		if err != nil {
			issues = append(issues, fmt.Sprintf("error listing subscriptions: %v", err))
			break
		}
		if scanned >= maxUnpagedInventoryItems {
			subscriptionScanTruncated = true
			break
		}

		sub, err := a.pubsub.SubscriptionAdminClient.GetSubscription(ctx, &pubsubpb.GetSubscriptionRequest{
			Subscription: subName,
		})
		if err != nil {
			issues = append(issues, fmt.Sprintf("error fetching subscription %q: %v", subName, err))
			continue
		}

		lags = append(lags, models.SubscriptionLag{
			SubscriptionName:   subName,
			AckDeadlineSeconds: sub.AckDeadlineSeconds,
		})
	}
	if subscriptionScanTruncated {
		issues = append(issues, fmt.Sprintf("subscription inspection truncated at %d items", maxUnpagedInventoryItems))
	}
	metricIssues := a.populateSubscriptionLagMetrics(ctx, req.ProjectID, lags)
	issues = append(issues, metricIssues...)

	healthy := len(issues) == 0
	return models.TopicHealthReport{
		TopicName:     req.TopicName,
		Exists:        true,
		Subscriptions: lags,
		Healthy:       healthy,
		Issues:        issues,
	}, nil
}

type subscriptionGaugeObservation struct {
	value     float64
	timestamp time.Time
}

func (a *gcpAdapter) populateSubscriptionLagMetrics(ctx context.Context, projectID string, lags []models.SubscriptionLag) []string {
	if len(lags) == 0 {
		return nil
	}
	if a.metric == nil {
		return []string{"subscription lag metrics unavailable: Monitoring client is not configured"}
	}

	end := time.Now().UTC().Truncate(time.Second)
	start := end.Add(-pubsubHealthMetricLookback)
	query := func(metricType string) (models.GetMetricsResponse, error) {
		return a.GetMetrics(ctx, models.GetMetricsRequest{
			ProjectID:              projectID,
			MetricType:             metricType,
			StartTime:              start.Format(time.RFC3339),
			EndTime:                end.Format(time.RFC3339),
			LookbackMinutes:        int(pubsubHealthMetricLookback / time.Minute),
			AlignmentPeriodSeconds: 60,
			PerSeriesAligner:       "ALIGN_MAX",
			MaxTimeSeries:          maxUnpagedInventoryItems,
		})
	}

	backlogResponse, backlogErr := query("pubsub.googleapis.com/subscription/num_undelivered_messages")
	ageResponse, ageErr := query("pubsub.googleapis.com/subscription/oldest_unacked_message_age")
	backlogBySubscription := latestSubscriptionGaugeValues(backlogResponse, start, end)
	ageBySubscription := latestSubscriptionGaugeValues(ageResponse, start, end)

	issues := make([]string, 0)
	if backlogErr != nil {
		issues = append(issues, "subscription backlog metrics unavailable: "+backlogErr.Error())
	} else if backlogResponse.Truncated {
		issues = append(issues, "subscription backlog metric inventory was truncated")
	}
	if ageErr != nil {
		issues = append(issues, "oldest-unacked metrics unavailable: "+ageErr.Error())
	} else if ageResponse.Truncated {
		issues = append(issues, "oldest-unacked metric inventory was truncated")
	}

	for i := range lags {
		subscriptionID := parseSubResourceName(lags[i].SubscriptionName)
		backlog, hasBacklog := backlogBySubscription[subscriptionID]
		age, hasAge := ageBySubscription[subscriptionID]
		lags[i].MetricsAvailable = hasBacklog && hasAge

		if hasBacklog {
			lags[i].UndeliveredMessages = int64(math.Round(math.Max(0, backlog.value)))
			lags[i].MetricsObservedAt = backlog.timestamp.Format(time.RFC3339)
			if backlog.value >= pubsubBacklogThreshold {
				issues = append(issues, fmt.Sprintf("subscription %q has %.0f undelivered messages", subscriptionID, backlog.value))
			}
		} else if backlogErr == nil {
			issues = append(issues, fmt.Sprintf("subscription %q has no recent backlog metric", subscriptionID))
		}

		if hasAge {
			ageDuration := time.Duration(math.Max(0, age.value) * float64(time.Second))
			lags[i].OldestUnackedAge = formatDuration(ageDuration)
			if lags[i].MetricsObservedAt == "" || age.timestamp.After(backlog.timestamp) {
				lags[i].MetricsObservedAt = age.timestamp.Format(time.RFC3339)
			}
			if ageDuration >= pubsubOldestAgeThreshold {
				issues = append(issues, fmt.Sprintf("subscription %q oldest unacked message is %.0f seconds old", subscriptionID, age.value))
			}
		} else if ageErr == nil {
			issues = append(issues, fmt.Sprintf("subscription %q has no recent oldest-unacked metric", subscriptionID))
		}
	}
	return issues
}

func latestSubscriptionGaugeValues(response models.GetMetricsResponse, start, end time.Time) map[string]subscriptionGaugeObservation {
	values := make(map[string]subscriptionGaugeObservation)
	for _, series := range response.Series {
		subscriptionID := parseSubResourceName(series.ResourceLabels["subscription_id"])
		if subscriptionID == "" {
			continue
		}
		for _, point := range series.Points {
			timestamp, err := time.Parse(time.RFC3339Nano, point.Timestamp)
			if err != nil || timestamp.Before(start) || timestamp.After(end) {
				continue
			}
			current, ok := values[subscriptionID]
			if !ok || timestamp.After(current.timestamp) || (timestamp.Equal(current.timestamp) && point.Value > current.value) {
				values[subscriptionID] = subscriptionGaugeObservation{value: point.Value, timestamp: timestamp}
			}
		}
	}
	return values
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
}

func (a *gcpAdapter) ListSubscriptions(ctx context.Context, req models.ListSubscriptionsRequest) (models.ListSubscriptionsResponse, error) {
	if err := a.rateWait(ctx, "pubsub.ListSubscriptions"); err != nil {
		return models.ListSubscriptionsResponse{}, err
	}
	if a.pubsub == nil {
		return models.ListSubscriptionsResponse{Subscriptions: []models.SubscriptionSummary{}}, nil
	}
	ctx, cancel := a.withTimeout(ctx)
	defer cancel()

	project := fmt.Sprintf("projects/%s", req.ProjectID)

	var subs []models.SubscriptionSummary
	truncated := false

	if req.TopicName != "" {
		// Filter by topic: list subscriptions on a specific topic.
		topic := fmt.Sprintf("projects/%s/topics/%s", req.ProjectID, req.TopicName)
		it := a.pubsub.TopicAdminClient.ListTopicSubscriptions(ctx, &pubsubpb.ListTopicSubscriptionsRequest{
			Topic: topic, PageSize: maxUnpagedInventoryItems,
		})
		for scanned := 0; ; scanned++ {
			subName, err := it.Next()
			if err != nil {
				if isIteratorDone(err) {
					break
				}
				return models.ListSubscriptionsResponse{}, wrapGCPError("pubsub.ListSubscriptions", err)
			}
			if scanned >= maxUnpagedInventoryItems {
				truncated = true
				break
			}
			sub, err := a.pubsub.SubscriptionAdminClient.GetSubscription(ctx, &pubsubpb.GetSubscriptionRequest{
				Subscription: subName,
			})
			if err != nil {
				truncated = true
				continue
			}
			subs = append(subs, subscriptionToSummary(sub))
		}
	} else {
		it := a.pubsub.SubscriptionAdminClient.ListSubscriptions(ctx, &pubsubpb.ListSubscriptionsRequest{
			Project: project, PageSize: maxUnpagedInventoryItems,
		})
		for {
			sub, err := it.Next()
			if err != nil {
				if isIteratorDone(err) {
					break
				}
				return models.ListSubscriptionsResponse{}, wrapGCPError("pubsub.ListSubscriptions", err)
			}
			if len(subs) >= maxUnpagedInventoryItems {
				truncated = true
				break
			}
			subs = append(subs, subscriptionToSummary(sub))
		}
	}

	if subs == nil {
		subs = []models.SubscriptionSummary{}
	}
	return models.ListSubscriptionsResponse{Subscriptions: subs, Truncated: truncated}, nil
}

func subscriptionToSummary(sub *pubsubpb.Subscription) models.SubscriptionSummary {
	pushEndpoint := ""
	if sub.PushConfig != nil {
		pushEndpoint = sub.PushConfig.PushEndpoint
	}
	deadLetterTopic := ""
	if sub.DeadLetterPolicy != nil {
		deadLetterTopic = parseSubResourceName(sub.DeadLetterPolicy.DeadLetterTopic)
	}
	return models.SubscriptionSummary{
		Name:            parseSubResourceName(sub.Name),
		Topic:           parseSubResourceName(sub.Topic),
		PushEndpoint:    pushEndpoint,
		DeadLetterTopic: deadLetterTopic,
		Filter:          sub.Filter,
	}
}

// parseSubResourceName extracts the bare resource name from a full path
// like "projects/P/subscriptions/NAME" or "projects/P/topics/NAME".
func parseSubResourceName(fullName string) string {
	parts := strings.Split(fullName, "/")
	if len(parts) >= 4 {
		return parts[len(parts)-1]
	}
	return fullName
}
