package resolution

// knownSaaSByDomain maps well-known external hostnames and domains to
// human-friendly display names used when minting external_endpoint nodes.
var knownSaaSByDomain = map[string]string{
	// Payments
	"stripe.com":           "Stripe",
	"api.stripe.com":       "Stripe",
	"paypal.com":           "PayPal",
	"braintreegateway.com": "Braintree",
	"square.com":           "Square",

	// Communication
	"sendgrid.net":     "SendGrid",
	"api.sendgrid.com": "SendGrid",
	"mailgun.org":      "Mailgun",
	"postmarkapp.com":  "Postmark",
	"twilio.com":       "Twilio",
	"api.twilio.com":   "Twilio",
	"slack.com":        "Slack",
	"hooks.slack.com":  "Slack",

	// Auth / identity
	"auth0.com":                 "Auth0",
	"okta.com":                  "Okta",
	"cognito-idp.amazonaws.com": "AWS Cognito",

	// AI / ML
	"api.openai.com":    "OpenAI",
	"openai.com":        "OpenAI",
	"api.anthropic.com": "Anthropic",
	"anthropic.com":     "Anthropic",

	// Monitoring / APM
	"api.pagerduty.com": "PagerDuty",
	"pagerduty.com":     "PagerDuty",
	"api.datadoghq.com": "Datadog",
	"datadoghq.com":     "Datadog",
	"sentry.io":         "Sentry",

	// Storage / CDN
	"storage.googleapis.com": "GCS",
	"s3.amazonaws.com":       "AWS S3",
	"cloudflare.com":         "Cloudflare",
	"fastly.com":             "Fastly",

	// CRM / business
	"salesforce.com": "Salesforce",
	"hubspot.com":    "HubSpot",
	"intercom.io":    "Intercom",

	// Data / analytics
	"segment.io":     "Segment",
	"api.segment.io": "Segment",
	"amplitude.com":  "Amplitude",
	"mixpanel.com":   "Mixpanel",

	// GitHub / source control
	"api.github.com": "GitHub",
	"github.com":     "GitHub",
	"gitlab.com":     "GitLab",

	// Other cloud
	"execute-api.amazonaws.com": "AWS API Gateway",
	"lambda.amazonaws.com":      "AWS Lambda",
	"azure.com":                 "Azure",
}
