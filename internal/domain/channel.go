package domain

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type ChannelType string

const (
	ChannelDiscord  ChannelType = "discord"
	ChannelTelegram ChannelType = "telegram"
	ChannelWhatsApp ChannelType = "whatsapp"
)

func ValidChannelTypes() []ChannelType {
	return []ChannelType{ChannelDiscord, ChannelTelegram, ChannelWhatsApp}
}

func IsValidChannelType(ct ChannelType) bool {
	for _, valid := range ValidChannelTypes() {
		if ct == valid {
			return true
		}
	}
	return false
}

type ChannelConfig struct {
	ID        uuid.UUID       `json:"id"`
	TenantID  uuid.UUID       `json:"tenant_id"`
	Channel   ChannelType     `json:"channel"`
	Name      string          `json:"name"`
	Config    json.RawMessage `json:"config"`
	IsActive  bool            `json:"is_active"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

type CreateChannelConfigInput struct {
	Channel ChannelType     `json:"channel"`
	Name    string          `json:"name"`
	Config  json.RawMessage `json:"config"`
}

type UpdateChannelConfigInput struct {
	Name     *string          `json:"name,omitempty"`
	Config   *json.RawMessage `json:"config,omitempty"`
	IsActive *bool            `json:"is_active,omitempty"`
}

type ChannelConfigRepository interface {
	Create(ctx context.Context, cfg *ChannelConfig) error
	GetByID(ctx context.Context, id uuid.UUID) (*ChannelConfig, error)
	ListByTenant(ctx context.Context, tenantID uuid.UUID) ([]*ChannelConfig, error)
	ListByTenantAndChannel(ctx context.Context, tenantID uuid.UUID, ch ChannelType) ([]*ChannelConfig, error)
	Update(ctx context.Context, id uuid.UUID, tenantID uuid.UUID, input UpdateChannelConfigInput) (*ChannelConfig, error)
	Delete(ctx context.Context, id uuid.UUID, tenantID uuid.UUID) error
}
