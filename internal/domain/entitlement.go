package domain

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Entitlements is notifyd's local copy of what the billing service granted a
// tenant. Billing is the source of truth; this row is the last-known state and
// keeps delivery working even when billing is unreachable.
type Entitlements struct {
	TenantID        uuid.UUID     `json:"tenant_id"`
	PlanCode        string        `json:"plan_code"`
	MessageLimit    int64         `json:"message_limit"`
	AllowedChannels []ChannelType `json:"allowed_channels"`
	APIKeyLimit     int           `json:"api_key_limit"`
	RetentionDays   int           `json:"retention_days"`
	PeriodStart     time.Time     `json:"period_start"`
	PeriodEnd       time.Time     `json:"period_end"`
	UpdatedAt       time.Time     `json:"updated_at"`
}

func (e *Entitlements) AllowsChannel(ct ChannelType) bool {
	for _, c := range e.AllowedChannels {
		if c == ct {
			return true
		}
	}
	return false
}

type EntitlementRepository interface {
	Upsert(ctx context.Context, e *Entitlements) error
	GetByTenantID(ctx context.Context, tenantID uuid.UUID) (*Entitlements, error)
	ListAll(ctx context.Context) ([]*Entitlements, error)
}
