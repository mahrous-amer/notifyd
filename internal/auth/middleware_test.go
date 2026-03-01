package auth_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bse/notifyd/internal/auth"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMiddleware(t *testing.T) {
	manager := auth.NewJWTManager(testSigningKey, testIssuer, testExpiration)
	tenantID := uuid.New()
	tenantSlug := "acme-corp"

	successHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middlewareUnderTest := auth.Middleware(manager)

	t.Run("missing Authorization header returns 401", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()

		middlewareUnderTest(successHandler).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("Authorization header without Bearer scheme returns 401", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "Basic dXNlcjpwYXNz")
		rec := httptest.NewRecorder()

		middlewareUnderTest(successHandler).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("malformed token returns 401", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "Bearer this.is.not.a.valid.jwt")
		rec := httptest.NewRecorder()

		middlewareUnderTest(successHandler).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("expired token returns 401", func(t *testing.T) {
		expiredManager := auth.NewJWTManager(testSigningKey, testIssuer, -time.Minute)
		tokenString, err := expiredManager.GenerateToken(tenantID, tenantSlug, false)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "Bearer "+tokenString)
		rec := httptest.NewRecorder()

		middlewareUnderTest(successHandler).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("valid token passes through and claims are available in context", func(t *testing.T) {
		tokenString, err := manager.GenerateToken(tenantID, tenantSlug, false)
		require.NoError(t, err)

		var capturedClaims *auth.TenantClaims
		claimCapturingHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			capturedClaims = auth.GetClaims(r.Context())
			w.WriteHeader(http.StatusOK)
		})

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "Bearer "+tokenString)
		rec := httptest.NewRecorder()

		middlewareUnderTest(claimCapturingHandler).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		require.NotNil(t, capturedClaims)
		assert.Equal(t, tenantID, capturedClaims.TenantID)
		assert.Equal(t, tenantSlug, capturedClaims.TenantSlug)
		assert.False(t, capturedClaims.IsAdmin)
	})

	t.Run("valid admin token passes through with IsAdmin true in context", func(t *testing.T) {
		tokenString, err := manager.GenerateToken(tenantID, tenantSlug, true)
		require.NoError(t, err)

		var capturedClaims *auth.TenantClaims
		claimCapturingHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			capturedClaims = auth.GetClaims(r.Context())
			w.WriteHeader(http.StatusOK)
		})

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "Bearer "+tokenString)
		rec := httptest.NewRecorder()

		middlewareUnderTest(claimCapturingHandler).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		require.NotNil(t, capturedClaims)
		assert.True(t, capturedClaims.IsAdmin)
	})
}

func TestGetClaims(t *testing.T) {
	t.Run("returns nil when context has no claims", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		claims := auth.GetClaims(req.Context())
		assert.Nil(t, claims)
	})
}
