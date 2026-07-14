package domain

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type ChannelType string

const (
	ChannelDiscord  ChannelType = "discord"
	ChannelTelegram ChannelType = "telegram"
	ChannelWhatsApp ChannelType = "whatsapp"
	ChannelEmail    ChannelType = "email"
	ChannelSlack    ChannelType = "slack"
	ChannelWebhook  ChannelType = "webhook"
)

func ValidChannelTypes() []ChannelType {
	return []ChannelType{ChannelDiscord, ChannelTelegram, ChannelWhatsApp, ChannelEmail, ChannelSlack, ChannelWebhook}
}

func IsValidChannelType(ct ChannelType) bool {
	for _, valid := range ValidChannelTypes() {
		if ct == valid {
			return true
		}
	}
	return false
}

// DeliveryPreferences controls how a notification sent through this channel
// is queued and formatted by the async worker.
type DeliveryPreferences struct {
	// Priority maps to the asynq queue: "critical", "normal", or "low".
	Priority string `json:"priority,omitempty"`
	// MaxRetries overrides the service-level default when set.
	MaxRetries *int `json:"max_retries,omitempty"`
	// FormatMode controls message rendering: "plain", "markdown", or "html".
	FormatMode string `json:"format_mode,omitempty"`
}

// Validate checks that all DeliveryPreferences fields contain only accepted values.
// It returns nil when dp is nil, treating an absent preferences block as valid.
func (dp *DeliveryPreferences) Validate() error {
	if dp == nil {
		return nil
	}
	if dp.Priority != "" {
		switch dp.Priority {
		case "critical", "normal", "low":
		default:
			return fmt.Errorf("%w: invalid priority: %s, must be critical, normal, or low", ErrValidationFailed, dp.Priority)
		}
	}
	if dp.FormatMode != "" {
		switch dp.FormatMode {
		case "plain", "markdown", "html":
		default:
			return fmt.Errorf("%w: invalid format_mode: %s, must be plain, markdown, or html", ErrValidationFailed, dp.FormatMode)
		}
	}
	if dp.MaxRetries != nil && *dp.MaxRetries < 0 {
		return fmt.Errorf("%w: max_retries must be non-negative", ErrValidationFailed)
	}
	return nil
}

type ChannelConfig struct {
	ID            uuid.UUID            `json:"id"`
	TenantID      uuid.UUID            `json:"tenant_id"`
	Channel       ChannelType          `json:"channel"`
	Name          string               `json:"name"`
	Config        json.RawMessage      `json:"config"`
	IsActive      bool                 `json:"is_active"`
	DeliveryPrefs *DeliveryPreferences `json:"delivery_prefs,omitempty"`
	CreatedAt     time.Time            `json:"created_at"`
	UpdatedAt     time.Time            `json:"updated_at"`
}

type CreateChannelConfigInput struct {
	Channel       ChannelType          `json:"channel"`
	Name          string               `json:"name"`
	Config        json.RawMessage      `json:"config"`
	DeliveryPrefs *DeliveryPreferences `json:"delivery_prefs,omitempty"`
}

type UpdateChannelConfigInput struct {
	Name          *string              `json:"name,omitempty"`
	Config        *json.RawMessage     `json:"config,omitempty"`
	IsActive      *bool                `json:"is_active,omitempty"`
	DeliveryPrefs *DeliveryPreferences `json:"delivery_prefs,omitempty"`
}

type ChannelConfigRepository interface {
	Create(ctx context.Context, cfg *ChannelConfig) error
	GetByID(ctx context.Context, id uuid.UUID) (*ChannelConfig, error)
	ListByTenant(ctx context.Context, tenantID uuid.UUID) ([]*ChannelConfig, error)
	ListByTenantAndChannel(ctx context.Context, tenantID uuid.UUID, ch ChannelType) ([]*ChannelConfig, error)
	Update(ctx context.Context, id uuid.UUID, tenantID uuid.UUID, input UpdateChannelConfigInput) (*ChannelConfig, error)
	Delete(ctx context.Context, id uuid.UUID, tenantID uuid.UUID) error
}
