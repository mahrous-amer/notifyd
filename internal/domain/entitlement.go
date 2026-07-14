package domain

import (
	"context"
	"errors"
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

// FreeDefaults returns the entitlements applied to tenants that predate
// billing (no tenant_entitlements row): Free plan, calendar-month period.
func FreeDefaults(tenantID uuid.UUID, now time.Time) *Entitlements {
	start := time.Date(now.UTC().Year(), now.UTC().Month(), 1, 0, 0, 0, 0, time.UTC)
	return &Entitlements{
		TenantID:        tenantID,
		PlanCode:        "free",
		MessageLimit:    1000,
		AllowedChannels: []ChannelType{ChannelDiscord, ChannelTelegram, ChannelEmail, ChannelSlack, ChannelWebhook},
		APIKeyLimit:     1,
		RetentionDays:   7,
		PeriodStart:     start,
		PeriodEnd:       start.AddDate(0, 1, 0),
	}
}

// EntitlementsOrFree loads a tenant's entitlements, falling back to FreeDefaults.
func EntitlementsOrFree(ctx context.Context, repo EntitlementRepository, tenantID uuid.UUID) (*Entitlements, error) {
	ent, err := repo.GetByTenantID(ctx, tenantID)
	if err == nil {
		return ent, nil
	}
	if errors.Is(err, ErrNotFound) {
		return FreeDefaults(tenantID, time.Now()), nil
	}
	return nil, err
}
