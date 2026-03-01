package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"github.com/bse/notifyd/internal/auth"
	"github.com/bse/notifyd/internal/domain"
)

// stubTenantRepo satisfies domain.TenantRepository using a function field so
// each test can inject its own behaviour without a separate type per test.
type stubTenantRepo struct {
	domain.TenantRepository
	getByAPIKeyFn func(ctx context.Context, apiKey string) (*domain.Tenant, error)
}

func (s *stubTenantRepo) GetByAPIKey(ctx context.Context, apiKey string) (*domain.Tenant, error) {
	return s.getByAPIKeyFn(ctx, apiKey)
}

// authHandlerFixture bundles everything a test needs to exercise IssueToken.
type authHandlerFixture struct {
	handler        *AuthHandler
	jwtMgr         *auth.JWTManager
	hashedSecret   []byte
	tenantID       uuid.UUID
	tenantAPIKey   string
	tenantRawSecret string
}

func newAuthHandlerFixture(t *testing.T, repo domain.TenantRepository) *authHandlerFixture {
	t.Helper()

	jwtMgr := auth.NewJWTManager("test-signing-key", "notifyd-test", time.Hour)
	handler := NewAuthHandler(repo, jwtMgr, "admin-key", "admin-secret")

	hashedSecret, err := bcrypt.GenerateFromPassword([]byte("test-secret"), bcrypt.MinCost)
	require.NoError(t, err, "bcrypt.GenerateFromPassword must not fail in test setup")

	return &authHandlerFixture{
		handler:        handler,
		jwtMgr:         jwtMgr,
		hashedSecret:   hashedSecret,
		tenantID:       uuid.New(),
		tenantAPIKey:   "tenant-api-key",
		tenantRawSecret: "test-secret",
	}
}

func issueTokenRequest(t *testing.T, handler *AuthHandler, apiKey, apiSecret string) *httptest.ResponseRecorder {
	t.Helper()

	body, err := json.Marshal(map[string]string{
		"api_key":    apiKey,
		"api_secret": apiSecret,
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/auth/token", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.IssueToken(rec, req)
	return rec
}

func TestIssueToken_ValidTenantCredentials_Returns200WithToken(t *testing.T) {
	hashedSecret, err := bcrypt.GenerateFromPassword([]byte("test-secret"), bcrypt.MinCost)
	require.NoError(t, err)

	tenantID := uuid.New()
	repo := &stubTenantRepo{
		getByAPIKeyFn: func(_ context.Context, _ string) (*domain.Tenant, error) {
			return &domain.Tenant{
				ID:        tenantID,
				Slug:      "acme",
				APIKey:    "tenant-api-key",
				APISecret: string(hashedSecret),
				IsActive:  true,
			}, nil
		},
	}
	f := newAuthHandlerFixture(t, repo)

	rec := issueTokenRequest(t, f.handler, "tenant-api-key", "test-secret")

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp tokenResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.NotEmpty(t, resp.Token)
	assert.NotEmpty(t, resp.ExpiresIn)
}

func TestIssueToken_InvalidAPIKey_Returns401(t *testing.T) {
	repo := &stubTenantRepo{
		getByAPIKeyFn: func(_ context.Context, _ string) (*domain.Tenant, error) {
			return nil, domain.ErrNotFound
		},
	}
	f := newAuthHandlerFixture(t, repo)

	rec := issueTokenRequest(t, f.handler, "wrong-key", "any-secret")

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestIssueToken_DisabledTenant_Returns403(t *testing.T) {
	hashedSecret, err := bcrypt.GenerateFromPassword([]byte("test-secret"), bcrypt.MinCost)
	require.NoError(t, err)

	repo := &stubTenantRepo{
		getByAPIKeyFn: func(_ context.Context, _ string) (*domain.Tenant, error) {
			return &domain.Tenant{
				ID:        uuid.New(),
				APIKey:    "tenant-api-key",
				APISecret: string(hashedSecret),
				IsActive:  false,
			}, nil
		},
	}
	f := newAuthHandlerFixture(t, repo)

	rec := issueTokenRequest(t, f.handler, "tenant-api-key", "test-secret")

	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestIssueToken_WrongTenantSecret_Returns401(t *testing.T) {
	hashedSecret, err := bcrypt.GenerateFromPassword([]byte("correct-secret"), bcrypt.MinCost)
	require.NoError(t, err)

	repo := &stubTenantRepo{
		getByAPIKeyFn: func(_ context.Context, _ string) (*domain.Tenant, error) {
			return &domain.Tenant{
				ID:        uuid.New(),
				APIKey:    "tenant-api-key",
				APISecret: string(hashedSecret),
				IsActive:  true,
			}, nil
		},
	}
	f := newAuthHandlerFixture(t, repo)

	rec := issueTokenRequest(t, f.handler, "tenant-api-key", "wrong-secret")

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestIssueToken_ValidAdminCredentials_Returns200WithAdminToken(t *testing.T) {
	// The repo should never be called when admin credentials match.
	repo := &stubTenantRepo{
		getByAPIKeyFn: func(_ context.Context, _ string) (*domain.Tenant, error) {
			t.Fatal("GetByAPIKey must not be called for admin authentication")
			return nil, nil
		},
	}
	f := newAuthHandlerFixture(t, repo)

	rec := issueTokenRequest(t, f.handler, "admin-key", "admin-secret")

	require.Equal(t, http.StatusOK, rec.Code)

	var resp tokenResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.NotEmpty(t, resp.Token)

	// Verify the issued token carries the admin flag.
	claims, err := f.jwtMgr.ValidateToken(resp.Token)
	require.NoError(t, err)
	assert.True(t, claims.IsAdmin)
}

func TestIssueToken_WrongAdminSecret_Returns401(t *testing.T) {
	repo := &stubTenantRepo{
		getByAPIKeyFn: func(_ context.Context, _ string) (*domain.Tenant, error) {
			return nil, domain.ErrNotFound
		},
	}
	f := newAuthHandlerFixture(t, repo)

	rec := issueTokenRequest(t, f.handler, "admin-key", "not-the-admin-secret")

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestIssueToken_EmptyBody_Returns400(t *testing.T) {
	f := newAuthHandlerFixture(t, &stubTenantRepo{})

	req := httptest.NewRequest(http.MethodPost, "/auth/token", bytes.NewReader([]byte{}))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	f.handler.IssueToken(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestIssueToken_MissingAPIKey_Returns400(t *testing.T) {
	f := newAuthHandlerFixture(t, &stubTenantRepo{})

	body, err := json.Marshal(map[string]string{"api_secret": "some-secret"})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/auth/token", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	f.handler.IssueToken(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestIssueToken_MissingAPISecret_Returns400(t *testing.T) {
	f := newAuthHandlerFixture(t, &stubTenantRepo{})

	body, err := json.Marshal(map[string]string{"api_key": "some-key"})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/auth/token", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	f.handler.IssueToken(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}
