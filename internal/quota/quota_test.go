package quota

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bse/notifyd/internal/domain"
)

type stubEntRepo struct{ ent *domain.Entitlements }

func (s *stubEntRepo) Upsert(context.Context, *domain.Entitlements) error { return nil }
func (s *stubEntRepo) ListAll(context.Context) ([]*domain.Entitlements, error) {
	return nil, nil
}
func (s *stubEntRepo) GetByTenantID(context.Context, uuid.UUID) (*domain.Entitlements, error) {
	if s.ent == nil {
		return nil, domain.ErrNotFound
	}
	return s.ent, nil
}

func testRedis(t *testing.T) *redis.Client {
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		t.Skip("REDIS_ADDR not set; skipping quota integration test")
	}
	return redis.NewClient(&redis.Options{Addr: addr, DB: 9})
}

func TestReserve_AllowsUnderLimit_RejectsOver(t *testing.T) {
	rdb := testRedis(t)
	t.Cleanup(func() { rdb.FlushDB(context.Background()); rdb.Close() })

	ent := &domain.Entitlements{
		MessageLimit: 2,
		PeriodStart:  time.Now().Add(-time.Hour),
		PeriodEnd:    time.Now().Add(time.Hour),
	}
	svc := NewService(rdb, &stubEntRepo{ent: ent}, "", &http.Client{}, zerolog.Nop())
	tenantID := uuid.New()

	d1, err := svc.Reserve(context.Background(), tenantID, 1)
	require.NoError(t, err)
	assert.True(t, d1.Allowed)

	d2, err := svc.Reserve(context.Background(), tenantID, 1)
	require.NoError(t, err)
	assert.True(t, d2.Allowed)
	assert.Equal(t, int64(2), d2.Used)

	d3, err := svc.Reserve(context.Background(), tenantID, 1)
	require.NoError(t, err)
	assert.False(t, d3.Allowed, "third message exceeds limit of 2")
	assert.Equal(t, int64(2), d3.Used, "rejected reservation must roll back the counter")
}

func TestReserve_DefaultsToFreePlanWhenNoEntitlements(t *testing.T) {
	rdb := testRedis(t)
	t.Cleanup(func() { rdb.FlushDB(context.Background()); rdb.Close() })

	svc := NewService(rdb, &stubEntRepo{}, "", &http.Client{}, zerolog.Nop())
	d, err := svc.Reserve(context.Background(), uuid.New(), 1)
	require.NoError(t, err)
	assert.True(t, d.Allowed)
	assert.Equal(t, FreeMessageLimit, d.Limit)
}
