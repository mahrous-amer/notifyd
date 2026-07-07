package handler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bse/notifyd/internal/domain"
)

type fakeEntRepo struct {
	stored *domain.Entitlements
}

func (f *fakeEntRepo) Upsert(_ context.Context, e *domain.Entitlements) error {
	f.stored = e
	return nil
}
func (f *fakeEntRepo) GetByTenantID(_ context.Context, _ uuid.UUID) (*domain.Entitlements, error) {
	if f.stored == nil {
		return nil, domain.ErrNotFound
	}
	return f.stored, nil
}
func (f *fakeEntRepo) ListAll(_ context.Context) ([]*domain.Entitlements, error) {
	if f.stored == nil {
		return nil, nil
	}
	return []*domain.Entitlements{f.stored}, nil
}

type fakeUsageRepo struct{ report *domain.UsageReport }

func (f *fakeUsageRepo) UsageByTenant(_ context.Context, _ uuid.UUID, _, _ time.Time) (*domain.UsageReport, error) {
	return f.report, nil
}

func newEntRequest(t *testing.T, tenantID uuid.UUID, body string) *http.Request {
	req := httptest.NewRequest(http.MethodPut, "/admin/tenants/"+tenantID.String()+"/entitlements", strings.NewReader(body))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenantID", tenantID.String())
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func TestEntitlementHandler_Put_StoresEntitlements(t *testing.T) {
	repo := &fakeEntRepo{}
	h := NewEntitlementHandler(repo, &fakeUsageRepo{})
	tenantID := uuid.New()
	body := fmt.Sprintf(`{"tenant_id":"%s","plan_code":"pro","message_limit":50000,`+
		`"allowed_channels":["discord","telegram","whatsapp"],`+
		`"api_key_limit":3,"retention_days":90,`+
		`"period_start":"2026-07-01T00:00:00Z","period_end":"2026-08-01T00:00:00Z"}`, tenantID)

	rec := httptest.NewRecorder()
	h.Put(rec, newEntRequest(t, tenantID, body))

	require.Equal(t, http.StatusOK, rec.Code)
	require.NotNil(t, repo.stored)
	assert.Equal(t, "pro", repo.stored.PlanCode)
	assert.Equal(t, int64(50000), repo.stored.MessageLimit)
	assert.Equal(t, tenantID, repo.stored.TenantID)
	assert.Len(t, repo.stored.AllowedChannels, 3)
}

func TestEntitlementHandler_Put_RejectsInvalidChannel(t *testing.T) {
	h := NewEntitlementHandler(&fakeEntRepo{}, &fakeUsageRepo{})
	tenantID := uuid.New()
	body := fmt.Sprintf(`{"tenant_id":"%s","plan_code":"pro","message_limit":1,`+
		`"allowed_channels":["smoke-signal"],`+
		`"api_key_limit":1,"retention_days":7,`+
		`"period_start":"2026-07-01T00:00:00Z","period_end":"2026-08-01T00:00:00Z"}`, tenantID)

	rec := httptest.NewRecorder()
	h.Put(rec, newEntRequest(t, tenantID, body))

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestEntitlementHandler_Put_RejectsMismatchedTenantID(t *testing.T) {
	h := NewEntitlementHandler(&fakeEntRepo{}, &fakeUsageRepo{})
	pathTenantID := uuid.New()
	bodyTenantID := uuid.New() // deliberately different from the path tenant ID
	body := fmt.Sprintf(`{"tenant_id":"%s","plan_code":"pro","message_limit":1,`+
		`"allowed_channels":["discord"],`+
		`"api_key_limit":1,"retention_days":7,`+
		`"period_start":"2026-07-01T00:00:00Z","period_end":"2026-08-01T00:00:00Z"}`, bodyTenantID)

	rec := httptest.NewRecorder()
	h.Put(rec, newEntRequest(t, pathTenantID, body))

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestEntitlementHandler_Usage(t *testing.T) {
	usage := &fakeUsageRepo{report: &domain.UsageReport{Sent: 10, Delivered: 8, Failed: 2, ByChannel: map[string]int64{"discord": 10}}}
	h := NewEntitlementHandler(&fakeEntRepo{}, usage)
	tenantID := uuid.New()

	req := httptest.NewRequest(http.MethodGet,
		"/admin/tenants/"+tenantID.String()+"/usage?period_start=2026-07-01T00:00:00Z&period_end=2026-08-01T00:00:00Z", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenantID", tenantID.String())
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	h.Usage(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"sent":10`)
}
