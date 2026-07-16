package notifyd

import "time"

// ChannelType is one of the delivery channels notifyd supports.
type ChannelType string

const (
	ChannelDiscord  ChannelType = "discord"
	ChannelTelegram ChannelType = "telegram"
	ChannelWhatsApp ChannelType = "whatsapp"
	ChannelEmail    ChannelType = "email"
	ChannelSlack    ChannelType = "slack"
	ChannelWebhook  ChannelType = "webhook"
)

// NotificationStatus is the delivery lifecycle state of a Notification.
type NotificationStatus string

const (
	StatusPending    NotificationStatus = "pending"
	StatusProcessing NotificationStatus = "processing"
	StatusDelivered  NotificationStatus = "delivered"
	StatusFailed     NotificationStatus = "failed"
	StatusRetrying   NotificationStatus = "retrying"
)

// AttemptStatus is the outcome of a single delivery attempt.
type AttemptStatus string

const (
	AttemptSuccess AttemptStatus = "success"
	AttemptFailure AttemptStatus = "failure"
)

// FormatMode controls how a notification body is rendered by the receiving
// channel, when that channel supports rich formatting.
type FormatMode string

const (
	FormatPlain    FormatMode = "plain"
	FormatMarkdown FormatMode = "markdown"
	FormatHTML     FormatMode = "html"
)

// APIError is the shape of every non-2xx JSON response body. Some endpoints
// return richer error shapes (QuotaExceededError, SubscriptionExpiredError)
// that embed this same "error" field plus extra context; callers who need
// that extra context can inspect the HTTP status code on *RequestError.
type APIError struct {
	Error string `json:"error"`
}

// DeliveryPreferences controls queueing priority, retry count, and message
// formatting for notifications sent through a channel config.
type DeliveryPreferences struct {
	Priority   string `json:"priority,omitempty"`
	MaxRetries *int   `json:"max_retries,omitempty"`
	FormatMode string `json:"format_mode,omitempty"`
}

// ChannelConfig is a tenant's configured destination for one channel type
// (a specific Discord webhook, Telegram bot+chat, SMTP mailbox, etc).
type ChannelConfig struct {
	ID             string               `json:"id"`
	TenantID       string               `json:"tenant_id"`
	Channel        ChannelType          `json:"channel"`
	Name           string               `json:"name"`
	Config         map[string]any       `json:"config"`
	IsActive       bool                 `json:"is_active"`
	DeliveryPrefs  *DeliveryPreferences `json:"delivery_prefs,omitempty"`
	CreatedAt      time.Time            `json:"created_at"`
	UpdatedAt      time.Time            `json:"updated_at"`
}

// CreateChannelInput is the request body for creating a ChannelConfig.
// Config's shape depends on Channel — see the openapi spec's
// EmailChannelConfig / SlackChannelConfig / WebhookChannelConfig schemas.
type CreateChannelInput struct {
	Channel       ChannelType          `json:"channel"`
	Name          string               `json:"name"`
	Config        map[string]any       `json:"config"`
	DeliveryPrefs *DeliveryPreferences `json:"delivery_prefs,omitempty"`
}

// UpdateChannelInput is the request body for updating a ChannelConfig. Only
// non-nil fields are sent, so omitted fields leave the existing value alone.
type UpdateChannelInput struct {
	Name          *string              `json:"name,omitempty"`
	Config        map[string]any       `json:"config,omitempty"`
	IsActive      *bool                `json:"is_active,omitempty"`
	DeliveryPrefs *DeliveryPreferences `json:"delivery_prefs,omitempty"`
}

// Notification is a single message enqueued for delivery through one
// channel config.
type Notification struct {
	ID              string             `json:"id"`
	TenantID        string             `json:"tenant_id"`
	ChannelConfigID string             `json:"channel_config_id"`
	Channel         ChannelType        `json:"channel"`
	Subject         *string            `json:"subject"`
	Body            string             `json:"body"`
	Metadata        map[string]any     `json:"metadata,omitempty"`
	Status          NotificationStatus `json:"status"`
	RetryCount      int                `json:"retry_count"`
	MaxRetries      int                `json:"max_retries"`
	LastError       *string            `json:"last_error"`
	DeliveredAt     *time.Time         `json:"delivered_at"`
	ProviderMsgID   *string            `json:"provider_msg_id"`
	CreatedAt       time.Time          `json:"created_at"`
	UpdatedAt       time.Time          `json:"updated_at"`
}

// SendInput is the request body for Client.Send.
type SendInput struct {
	ChannelConfigID string         `json:"channel_config_id"`
	// Subject is optional for chat/webhook channels but required and
	// non-blank when the target channel is email.
	Subject  string         `json:"subject,omitempty"`
	Body     string         `json:"body"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// SendMultiResponse is the response body for Client.SendMulti: notifications
// that enqueued successfully, plus per-item error messages for any that
// didn't (partial success is expected, not exceptional).
type SendMultiResponse struct {
	Sent   []Notification `json:"sent"`
	Errors []string       `json:"errors"`
}

// NotificationList is a page of notifications from Client.ListNotifications.
type NotificationList struct {
	Data   []Notification `json:"data"`
	Total  int            `json:"total"`
	Limit  int            `json:"limit"`
	Offset int            `json:"offset"`
}

// ListNotificationsParams filters and paginates Client.ListNotifications.
// All fields are optional; the zero value lists the first page unfiltered.
type ListNotificationsParams struct {
	Limit   int
	Offset  int
	Status  NotificationStatus
	Channel ChannelType
}

// DeliveryAttempt is one delivery attempt record for a Notification.
type DeliveryAttempt struct {
	ID               string         `json:"id"`
	NotificationID   string         `json:"notification_id"`
	AttemptNumber    int            `json:"attempt_number"`
	Status           AttemptStatus  `json:"status"`
	ProviderResponse map[string]any `json:"provider_response,omitempty"`
	ErrorMessage     *string        `json:"error_message"`
	DurationMs       int            `json:"duration_ms"`
	AttemptedAt      time.Time      `json:"attempted_at"`
}

// DeliveryMetric holds provider-reported engagement data for a delivered
// notification (read receipts, link clicks, reactions — shape varies by
// provider).
type DeliveryMetric struct {
	ID             string         `json:"id"`
	NotificationID string         `json:"notification_id"`
	ProviderMsgID  string         `json:"provider_msg_id"`
	DeliveredAt    *time.Time     `json:"delivered_at"`
	ReadAt         *time.Time     `json:"read_at"`
	Interactions   map[string]any `json:"interactions,omitempty"`
	CollectedAt    time.Time      `json:"collected_at"`
}

// APIKey is a tenant's API key record (never includes the secret — that's
// returned once, at creation time, in CreateAPIKeyResponse).
type APIKey struct {
	ID        string     `json:"id"`
	TenantID  string     `json:"tenant_id"`
	APIKey    string     `json:"api_key"`
	Label     string     `json:"label"`
	CreatedAt time.Time  `json:"created_at"`
	RevokedAt *time.Time `json:"revoked_at"`
}

// CreateAPIKeyResponse is returned once on key creation; api_secret cannot
// be retrieved again afterward.
type CreateAPIKeyResponse struct {
	Key       APIKey `json:"key"`
	APISecret string `json:"api_secret"`
}

// WebhookEventType is the set of status events a WebhookEndpoint may
// subscribe to.
type WebhookEventType string

const (
	EventNotificationDelivered WebhookEventType = "notification.delivered"
	EventNotificationFailed    WebhookEventType = "notification.failed"
)

// WebhookEndpoint is a tenant-configured destination for delivery-status
// events. The signing secret is never included here — see
// WebhookEndpointCreated for the one response that does.
type WebhookEndpoint struct {
	ID        string             `json:"id"`
	TenantID  string             `json:"tenant_id"`
	URL       string             `json:"url"`
	Events    []WebhookEventType `json:"events"`
	IsActive  bool               `json:"is_active"`
	CreatedAt time.Time          `json:"created_at"`
}

// WebhookEndpointCreated is the POST /webhooks response: a WebhookEndpoint
// plus the plaintext signing secret, shown this one time only.
type WebhookEndpointCreated struct {
	WebhookEndpoint
	Secret string `json:"secret"`
}

// CreateWebhookInput is the request body for creating a WebhookEndpoint.
type CreateWebhookInput struct {
	URL    string             `json:"url"`
	Events []WebhookEventType `json:"events"`
}

// UpdateWebhookInput is the request body for updating a WebhookEndpoint.
// Only non-nil fields are sent, so omitted fields leave the existing value
// alone.
type UpdateWebhookInput struct {
	URL      *string            `json:"url,omitempty"`
	Events   []WebhookEventType `json:"events,omitempty"`
	IsActive *bool              `json:"is_active,omitempty"`
}
