package auth

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

type JWTManager struct {
	signingKey []byte
	issuer     string
	expiration time.Duration
}

func NewJWTManager(signingKey string, issuer string, expiration time.Duration) *JWTManager {
	return &JWTManager{
		signingKey: []byte(signingKey),
		issuer:     issuer,
		expiration: expiration,
	}
}

func (m *JWTManager) GenerateToken(tenantID uuid.UUID, tenantSlug string, isAdmin bool) (string, error) {
	now := time.Now()
	claims := TenantClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    m.issuer,
			Subject:   tenantID.String(),
			ExpiresAt: jwt.NewNumericDate(now.Add(m.expiration)),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ID:        uuid.New().String(),
		},
		TenantID:   tenantID,
		TenantSlug: tenantSlug,
		IsAdmin:    isAdmin,
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(m.signingKey)
}

func (m *JWTManager) Expiration() time.Duration {
	return m.expiration
}

func (m *JWTManager) ValidateToken(tokenString string) (*TenantClaims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &TenantClaims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return m.signingKey, nil
	})
	if err != nil {
		return nil, fmt.Errorf("invalid token: %w", err)
	}
	claims, ok := token.Claims.(*TenantClaims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token claims")
	}
	return claims, nil
}
