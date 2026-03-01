package auth

import (
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

type TenantClaims struct {
	jwt.RegisteredClaims
	TenantID   uuid.UUID `json:"tenant_id"`
	TenantSlug string    `json:"tenant_slug"`
	IsAdmin    bool      `json:"is_admin,omitempty"`
}
