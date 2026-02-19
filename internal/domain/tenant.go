package domain

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type Tenant struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Slug      string    `json:"slug"`
	APIKey    string    `json:"api_key"`
	APISecret string    `json:"-"`
	IsActive  bool      `json:"is_active"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type CreateTenantInput struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
}

type UpdateTenantInput struct {
	Name     *string `json:"name,omitempty"`
	IsActive *bool   `json:"is_active,omitempty"`
}

type TenantRepository interface {
	Create(ctx context.Context, tenant *Tenant) error
	GetByID(ctx context.Context, id uuid.UUID) (*Tenant, error)
	GetBySlug(ctx context.Context, slug string) (*Tenant, error)
	GetByAPIKey(ctx context.Context, apiKey string) (*Tenant, error)
	Update(ctx context.Context, id uuid.UUID, input UpdateTenantInput) (*Tenant, error)
	Delete(ctx context.Context, id uuid.UUID) error
	List(ctx context.Context, limit, offset int) ([]*Tenant, int, error)
}
