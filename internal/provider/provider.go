package provider

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Capability names a feature that a provider may or may not support.
type Capability string

const (
	CapReadReceipts   Capability = "read_receipts"
	CapDeliveryStatus Capability = "delivery_status"
	CapClickTracking  Capability = "click_tracking"
)

// ProviderCapabilities declares what features a provider exposes.
type ProviderCapabilities struct {
	Capabilities []Capability
}

// SendRequest carries the message content to be delivered by a provider.
type SendRequest struct {
	// NotificationID identifies the notification being delivered. Most
	// providers ignore it; the webhook provider includes it in its JSON
	// payload so receivers can deduplicate and correlate deliveries.
	NotificationID uuid.UUID
	Subject        string
	Body           string
	Metadata       json.RawMessage
	FormatMode     string // "plain", "markdown", or "html" — provider may use this to apply formatting
}

// SendResponse carries the outcome of a single delivery attempt.
type SendResponse struct {
	Success       bool
	ProviderMsgID string // provider-assigned message ID, used for metric polling
	ProviderData  json.RawMessage
	ErrorMessage  string
	// Permanent marks a failed send as one that will never succeed on retry
	// (e.g. bad credentials, rejected recipient). The dispatcher skips further
	// retry attempts and moves the notification straight to StatusFailed.
	// Ignored when Success is true.
	Permanent bool
}

// DeliveryMetrics holds the delivery and engagement data fetched from a provider
// after a message has been sent.
type DeliveryMetrics struct {
	ProviderMsgID string          `json:"provider_msg_id"`
	DeliveredAt   *time.Time      `json:"delivered_at,omitempty"`
	ReadAt        *time.Time      `json:"read_at,omitempty"`
	Interactions  json.RawMessage `json:"interactions,omitempty"` // provider-specific interaction data
}

// Provider is the interface that every notification channel backend must satisfy.
type Provider interface {
	// Type returns the canonical channel type name (e.g. "discord", "telegram").
	Type() string

	// Capabilities declares which optional features the provider supports.
	Capabilities() ProviderCapabilities

	// Send delivers a notification and returns the outcome.
	Send(ctx context.Context, channelConfig json.RawMessage, req SendRequest) (*SendResponse, error)

	// FetchMetrics retrieves post-delivery engagement data for a previously sent
	// message. Returns domain.ErrMetricsNotSupported when the provider does not
	// offer this capability.
	FetchMetrics(ctx context.Context, channelConfig json.RawMessage, providerMsgID string) (*DeliveryMetrics, error)

	// ValidateConfig parses and validates the provider-specific channel config.
	ValidateConfig(config json.RawMessage) error
}
