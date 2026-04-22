package gcp

import (
	"context"
	"fmt"
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
		Project: project,
	})

	var topics []models.TopicSummary
	for {
		t, err := it.Next()
		if isIteratorDone(err) {
			break
		}
		if err != nil {
			return models.ListTopicsResponse{}, wrapGCPError("pubsub.ListTopics", err)
		}

		// Count subscriptions for this topic (best-effort).
		subCount := 0
		subIt := a.pubsub.TopicAdminClient.ListTopicSubscriptions(ctx, &pubsubpb.ListTopicSubscriptionsRequest{
			Topic: t.Name,
		})
		for {
			_, err := subIt.Next()
			if isIteratorDone(err) {
				break
			}
			if err != nil {
				break // best-effort; do not fail the whole list
			}
			subCount++
		}

		topics = append(topics, models.TopicSummary{
			Name:              t.Name,
			Labels:            t.Labels,
			SubscriptionCount: subCount,
		})
	}
	if topics == nil {
		topics = []models.TopicSummary{}
	}
	return models.ListTopicsResponse{Topics: topics}, nil
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
		Topic: topicName,
	})

	var lags []models.SubscriptionLag
	var issues []string

	for {
		subName, err := subIt.Next()
		if isIteratorDone(err) {
			break
		}
		if err != nil {
			issues = append(issues, fmt.Sprintf("error listing subscriptions: %v", err))
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
