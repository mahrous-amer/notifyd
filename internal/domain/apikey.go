package domain

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type APIKey struct {
	ID            uuid.UUID  `json:"id"`
	TenantID      uuid.UUID  `json:"tenant_id"`
	APIKey        string     `json:"api_key"`
	APISecretHash string     `json:"-"`
	Label         string     `json:"label"`
	CreatedAt     time.Time  `json:"created_at"`
	RevokedAt     *time.Time `json:"revoked_at,omitempty"`
}

type APIKeyRepository interface {
	Create(ctx context.Context, k *APIKey) error
	// CreateWithinLimit atomically creates k if the tenant's active key count
	// is below limit, returning ErrKeyLimitReached otherwise.
	CreateWithinLimit(ctx context.Context, k *APIKey, limit int) error
	GetByAPIKey(ctx context.Context, apiKey string) (*APIKey, error)
	ListByTenant(ctx context.Context, tenantID uuid.UUID) ([]*APIKey, error)
	Revoke(ctx context.Context, id, tenantID uuid.UUID) error
}
