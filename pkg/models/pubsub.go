package models

type ListTopicsRequest struct {
	ProjectID string `json:"project_id"`
}

type TopicSummary struct {
	Name                       string            `json:"name"`
	Labels                     map[string]string `json:"labels,omitempty"`
	SubscriptionCount          int               `json:"subscription_count"`
	SubscriptionCountTruncated bool              `json:"subscription_count_truncated,omitempty"`
}

type ListTopicsResponse struct {
	Topics    []TopicSummary `json:"topics"`
	Truncated bool           `json:"truncated,omitempty"`
}

type InspectTopicHealthRequest struct {
	ProjectID string `json:"project_id"`
	TopicName string `json:"topic_name"`
}

type SubscriptionLag struct {
	SubscriptionName    string `json:"subscription_name"`
	AckDeadlineSeconds  int32  `json:"ack_deadline_seconds,omitempty"`
	UndeliveredMessages int64  `json:"undelivered_messages"`
	OldestUnackedAge    string `json:"oldest_unacked_age"`
	MetricsObservedAt   string `json:"metrics_observed_at,omitempty"`
	MetricsAvailable    bool   `json:"metrics_available"`
}

type TopicHealthReport struct {
	TopicName     string            `json:"topic_name"`
	Exists        bool              `json:"exists"`
	Subscriptions []SubscriptionLag `json:"subscriptions"`
	Healthy       bool              `json:"healthy"`
	Issues        []string          `json:"issues,omitempty"`
}

// SubscriptionSummary is a lightweight view of a Pub/Sub subscription.
type SubscriptionSummary struct {
	Name            string `json:"name"`
	Topic           string `json:"topic"`
	PushEndpoint    string `json:"push_endpoint,omitempty"`
	DeadLetterTopic string `json:"dead_letter_topic,omitempty"`
	Filter          string `json:"filter,omitempty"`
}

// ListSubscriptionsRequest lists subscriptions in a project.
type ListSubscriptionsRequest struct {
	ProjectID string `json:"project_id"`
	TopicName string `json:"topic_name,omitempty"` // optional: filter by topic
}

// ListSubscriptionsResponse holds the list result.
type ListSubscriptionsResponse struct {
	Subscriptions []SubscriptionSummary `json:"subscriptions"`
	Truncated     bool                  `json:"truncated,omitempty"`
}
