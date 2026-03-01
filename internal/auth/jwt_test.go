package auth_test

import (
	"testing"
	"time"

	"github.com/bse/notifyd/internal/auth"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testSigningKey = "super-secret-test-key"
	testIssuer     = "notifyd-test"
	testExpiration = time.Hour
)

func newTestJWTManager() *auth.JWTManager {
	return auth.NewJWTManager(testSigningKey, testIssuer, testExpiration)
}

func TestGenerateToken(t *testing.T) {
	manager := newTestJWTManager()
	tenantID := uuid.New()
	tenantSlug := "acme-corp"

	t.Run("generates valid token for regular tenant", func(t *testing.T) {
		tokenString, err := manager.GenerateToken(tenantID, tenantSlug, false)
		require.NoError(t, err)
		assert.NotEmpty(t, tokenString)

		claims, err := manager.ValidateToken(tokenString)
		require.NoError(t, err)
		assert.Equal(t, tenantID, claims.TenantID)
		assert.Equal(t, tenantSlug, claims.TenantSlug)
		assert.False(t, claims.IsAdmin)
	})

	t.Run("generates valid admin token", func(t *testing.T) {
		tokenString, err := manager.GenerateToken(tenantID, tenantSlug, true)
		require.NoError(t, err)
		assert.NotEmpty(t, tokenString)

		claims, err := manager.ValidateToken(tokenString)
		require.NoError(t, err)
		assert.Equal(t, tenantID, claims.TenantID)
		assert.Equal(t, tenantSlug, claims.TenantSlug)
		assert.True(t, claims.IsAdmin)
	})
}

func TestValidateToken(t *testing.T) {
	manager := newTestJWTManager()
	tenantID := uuid.New()
	tenantSlug := "acme-corp"

	t.Run("valid token parses correctly with correct claims", func(t *testing.T) {
		tokenString, err := manager.GenerateToken(tenantID, tenantSlug, false)
		require.NoError(t, err)

		claims, err := manager.ValidateToken(tokenString)
		require.NoError(t, err)
		assert.Equal(t, tenantID, claims.TenantID)
		assert.Equal(t, tenantSlug, claims.TenantSlug)
		assert.Equal(t, testIssuer, claims.Issuer)
		assert.Equal(t, tenantID.String(), claims.Subject)
		assert.False(t, claims.IsAdmin)
	})

	t.Run("expired token returns error", func(t *testing.T) {
		expiredManager := auth.NewJWTManager(testSigningKey, testIssuer, -time.Minute)
		tokenString, err := expiredManager.GenerateToken(tenantID, tenantSlug, false)
		require.NoError(t, err)

		_, err = manager.ValidateToken(tokenString)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid token")
	})

	t.Run("token signed with wrong key returns error", func(t *testing.T) {
		wrongKeyManager := auth.NewJWTManager("different-secret-key", testIssuer, testExpiration)
		tokenString, err := wrongKeyManager.GenerateToken(tenantID, tenantSlug, false)
		require.NoError(t, err)

		_, err = manager.ValidateToken(tokenString)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid token")
	})

	t.Run("token with unexpected signing algorithm returns error", func(t *testing.T) {
		// Craft a token using RS256 (asymmetric) — the manager expects HS256 (HMAC).
		// jwt.SigningMethodRS256.Sign requires a private key, so we craft the token
		// using the "none" algorithm instead, which requires no key at all.
		unsignedClaims := auth.TenantClaims{
			RegisteredClaims: jwt.RegisteredClaims{
				Issuer:    testIssuer,
				Subject:   tenantID.String(),
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
				IssuedAt:  jwt.NewNumericDate(time.Now()),
			},
			TenantID:   tenantID,
			TenantSlug: tenantSlug,
		}

		// Build a raw token with the "none" alg header manually.
		// jwt.UnsafeAllowNoneSignatureType is the sentinel value for the none method.
		noneToken := jwt.NewWithClaims(jwt.SigningMethodNone, unsignedClaims)
		tokenString, err := noneToken.SignedString(jwt.UnsafeAllowNoneSignatureType)
		require.NoError(t, err)

		_, err = manager.ValidateToken(tokenString)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid token")
	})
}
