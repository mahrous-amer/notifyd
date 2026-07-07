// internal/worker/retention_test.go
package worker

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bse/notifyd/internal/domain"
)

type fakeMaintEntRepo struct{ ents map[uuid.UUID]*domain.Entitlements }

func (f *fakeMaintEntRepo) Upsert(context.Context, *domain.Entitlements) error { return nil }
func (f *fakeMaintEntRepo) GetByTenantID(_ context.Context, id uuid.UUID) (*domain.Entitlements, error) {
	if e, ok := f.ents[id]; ok {
		return e, nil
	}
	return nil, domain.ErrNotFound
}
func (f *fakeMaintEntRepo) ListAll(context.Context) ([]*domain.Entitlements, error) {
	return nil, nil
}

type fakeMaintTenantRepo struct{ tenants []*domain.Tenant }

func (f *fakeMaintTenantRepo) Create(context.Context, *domain.Tenant) error { return nil }
func (f *fakeMaintTenantRepo) GetByID(context.Context, uuid.UUID) (*domain.Tenant, error) {
	return nil, domain.ErrNotFound
}
func (f *fakeMaintTenantRepo) GetBySlug(context.Context, string) (*domain.Tenant, error) {
	return nil, domain.ErrNotFound
}
func (f *fakeMaintTenantRepo) Update(context.Context, uuid.UUID, domain.UpdateTenantInput) (*domain.Tenant, error) {
	return nil, domain.ErrNotFound
}
func (f *fakeMaintTenantRepo) Delete(context.Context, uuid.UUID) error { return nil }
func (f *fakeMaintTenantRepo) List(_ context.Context, limit, offset int) ([]*domain.Tenant, int, error) {
	if offset >= len(f.tenants) {
		return nil, len(f.tenants), nil
	}
	end := min(offset+limit, len(f.tenants))
	return f.tenants[offset:end], len(f.tenants), nil
}

type fakePurgeRepo struct {
	purged map[uuid.UUID]time.Time
}

func (f *fakePurgeRepo) DeleteOlderThan(_ context.Context, tenantID uuid.UUID, cutoff time.Time) (int64, error) {
	f.purged[tenantID] = cutoff
	return 3, nil
}

func TestRetentionPurge_PurgesAllTenantsIncludingLegacy(t *testing.T) {
	billed, legacy := uuid.New(), uuid.New()
	tenants := &fakeMaintTenantRepo{tenants: []*domain.Tenant{{ID: billed}, {ID: legacy}}}
	ents := &fakeMaintEntRepo{ents: map[uuid.UUID]*domain.Entitlements{
		billed: {TenantID: billed, RetentionDays: 30},
	}}
	purge := &fakePurgeRepo{purged: map[uuid.UUID]time.Time{}}

	h := NewMaintenanceHandler(tenants, ents, purge, nil, nil, zerolog.Nop())
	err := h.HandleRetentionPurge(context.Background(), asynq.NewTask(TypeRetentionPurge, nil))

	require.NoError(t, err)
	require.Len(t, purge.purged, 2, "every tenant is purged, entitlements row or not")
	assert.WithinDuration(t, time.Now().AddDate(0, 0, -30), purge.purged[billed], time.Minute)
	assert.WithinDuration(t, time.Now().AddDate(0, 0, -7), purge.purged[legacy], time.Minute,
		"legacy tenants purge on the Free default retention")
}
