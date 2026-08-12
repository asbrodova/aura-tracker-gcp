package gcp

import (
	"context"
	"fmt"
	"strings"
	"time"

	"cloud.google.com/go/pubsub/v2/apiv1/pubsubpb"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
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

	// List subscriptions and fetch their ack deadline as a proxy for health.
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

		ageStr := ""
		if sub.AckDeadlineSeconds > 0 {
			ageStr = formatDuration(time.Duration(sub.AckDeadlineSeconds) * time.Second)
		}

		lags = append(lags, models.SubscriptionLag{
			SubscriptionName: subName,
			OldestUnackedAge: ageStr,
		})
	}
	if subscriptionScanTruncated {
		issues = append(issues, fmt.Sprintf("subscription inspection truncated at %d items", maxUnpagedInventoryItems))
	}

	healthy := len(issues) == 0
	return models.TopicHealthReport{
		TopicName:     req.TopicName,
		Exists:        true,
		Subscriptions: lags,
		Healthy:       healthy,
		Issues:        issues,
	}, nil
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
