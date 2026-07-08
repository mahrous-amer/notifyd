package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/bse/notifyd/internal/domain"
	"github.com/bse/notifyd/internal/domain/mocks"
	"github.com/bse/notifyd/internal/service"
)

func buildTenantHandlerFixture(t *testing.T) (*TenantHandler, *mocks.MockTenantRepository) {
	t.Helper()

	ctrl := gomock.NewController(t)
	repo := mocks.NewMockTenantRepository(ctrl)
	svc := service.NewTenantService(repo, &stubAPIKeyRepo{})

	return NewTenantHandler(svc), repo
}

func newGetBySlugRequest(slug string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/admin/tenants/by-slug/"+slug, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("slug", slug)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

// TestTenantHandler_GetBySlug_Found verifies that a known slug yields
// HTTP 200 and the tenant serialised as JSON.
func TestTenantHandler_GetBySlug_Found(t *testing.T) {
	h, repo := buildTenantHandlerFixture(t)
	slug := "acme-corp"
	tenantID := uuid.New()

	expected := &domain.Tenant{ID: tenantID, Name: "Acme Corp", Slug: slug, IsActive: true}
	repo.EXPECT().GetBySlug(gomock.Any(), slug).Return(expected, nil)

	rec := httptest.NewRecorder()
	h.GetBySlug(rec, newGetBySlugRequest(slug))

	require.Equal(t, http.StatusOK, rec.Code)

	var got domain.Tenant
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&got))
	assert.Equal(t, tenantID, got.ID)
	assert.Equal(t, "Acme Corp", got.Name)
	assert.Equal(t, slug, got.Slug)
}

// TestTenantHandler_GetBySlug_NotFound verifies that an unknown slug yields
// HTTP 404.
func TestTenantHandler_GetBySlug_NotFound(t *testing.T) {
	h, repo := buildTenantHandlerFixture(t)
	slug := "does-not-exist"

	repo.EXPECT().GetBySlug(gomock.Any(), slug).Return(nil, domain.ErrNotFound)

	rec := httptest.NewRecorder()
	h.GetBySlug(rec, newGetBySlugRequest(slug))

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// TestTenantHandler_GetBySlug_EmptySlug verifies that a missing slug URL
// parameter yields HTTP 400 without touching the repository.
func TestTenantHandler_GetBySlug_EmptySlug(t *testing.T) {
	h, _ := buildTenantHandlerFixture(t)

	// No chi URLParam set, so chi.URLParam returns "".
	req := httptest.NewRequest(http.MethodGet, "/admin/tenants/by-slug/", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, chi.NewRouteContext()))

	rec := httptest.NewRecorder()
	h.GetBySlug(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}
