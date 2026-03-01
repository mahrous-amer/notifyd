package auth_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bse/notifyd/internal/auth"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestAdminMiddleware(t *testing.T) {
	successHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	adminMiddleware := auth.AdminMiddleware()

	t.Run("missing claims in context returns 403", func(t *testing.T) {
		// No JWT middleware ran, so no claims are set in the context.
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()

		adminMiddleware(successHandler).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusForbidden, rec.Code)
	})

	t.Run("non-admin claims returns 403", func(t *testing.T) {
		nonAdminClaims := &auth.TenantClaims{
			TenantID:   uuid.New(),
			TenantSlug: "regular-tenant",
			IsAdmin:    false,
		}

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		ctx := context.WithValue(req.Context(), auth.TenantClaimsKey, nonAdminClaims)
		req = req.WithContext(ctx)
		rec := httptest.NewRecorder()

		adminMiddleware(successHandler).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusForbidden, rec.Code)
	})

	t.Run("admin claims passes through to handler", func(t *testing.T) {
		adminClaims := &auth.TenantClaims{
			TenantID:   uuid.New(),
			TenantSlug: "admin-tenant",
			IsAdmin:    true,
		}

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		ctx := context.WithValue(req.Context(), auth.TenantClaimsKey, adminClaims)
		req = req.WithContext(ctx)
		rec := httptest.NewRecorder()

		adminMiddleware(successHandler).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
	})
}
