// internal/worker/retention.go
package worker

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"

	"github.com/bse/notifyd/internal/domain"
)

const (
	TypeRetentionPurge = "retention:purge"
	TypeUsageReconcile = "usage:reconcile"
)

// Purger is the slice of NotificationRepository maintenance needs.
type Purger interface {
	DeleteOlderThan(ctx context.Context, tenantID uuid.UUID, cutoff time.Time) (int64, error)
}

// UsageCounter counts billable notifications for reconciliation.
type UsageCounter interface {
	UsageByTenant(ctx context.Context, tenantID uuid.UUID, from, to time.Time) (*domain.UsageReport, error)
}

type MaintenanceHandler struct {
	tenantRepo domain.TenantRepository
	entRepo    domain.EntitlementRepository
	purger     Purger
	counter    UsageCounter
	rdb        *redis.Client
	logger     zerolog.Logger
}

func NewMaintenanceHandler(tenantRepo domain.TenantRepository, entRepo domain.EntitlementRepository, purger Purger, counter UsageCounter, rdb *redis.Client, logger zerolog.Logger) *MaintenanceHandler {
	return &MaintenanceHandler{tenantRepo: tenantRepo, entRepo: entRepo, purger: purger, counter: counter, rdb: rdb, logger: logger}
}

// forEachTenant pages through every tenant and applies fn with the tenant's
// entitlements (Free defaults when no row exists).
func (h *MaintenanceHandler) forEachTenant(ctx context.Context, fn func(*domain.Tenant, *domain.Entitlements)) error {
	const pageSize = 500
	for offset := 0; ; offset += pageSize {
		tenants, total, err := h.tenantRepo.List(ctx, pageSize, offset)
		if err != nil {
			return fmt.Errorf("list tenants: %w", err)
		}
		for _, t := range tenants {
			ent, err := domain.EntitlementsOrFree(ctx, h.entRepo, t.ID)
			if err != nil {
				h.logger.Error().Err(err).Str("tenant", t.ID.String()).Msg("load entitlements failed")
				continue
			}
			fn(t, ent)
		}
		if offset+pageSize >= total || len(tenants) == 0 {
			return nil
		}
	}
}

// HandleRetentionPurge deletes notification history past each tenant's
// retention window. Tenants without an entitlements row purge on the Free
// default (7 days) — explicit owner decision, 2026-07-07.
func (h *MaintenanceHandler) HandleRetentionPurge(ctx context.Context, _ *asynq.Task) error {
	return h.forEachTenant(ctx, func(t *domain.Tenant, ent *domain.Entitlements) {
		cutoff := time.Now().AddDate(0, 0, -ent.RetentionDays)
		n, err := h.purger.DeleteOlderThan(ctx, t.ID, cutoff)
		if err != nil {
			h.logger.Error().Err(err).Str("tenant", t.ID.String()).Msg("retention purge failed")
			return
		}
		if n > 0 {
			h.logger.Info().Str("tenant", t.ID.String()).Int64("deleted", n).Msg("retention purge")
		}
	})
}

// HandleUsageReconcile overwrites each tenant's Redis usage counter with the
// database truth for the current period.
func (h *MaintenanceHandler) HandleUsageReconcile(ctx context.Context, _ *asynq.Task) error {
	return h.forEachTenant(ctx, func(t *domain.Tenant, ent *domain.Entitlements) {
		report, err := h.counter.UsageByTenant(ctx, t.ID, ent.PeriodStart, ent.PeriodEnd)
		if err != nil {
			h.logger.Error().Err(err).Str("tenant", t.ID.String()).Msg("usage reconcile failed")
			return
		}
		key := fmt.Sprintf("usage:%s:%d", t.ID, ent.PeriodStart.Unix())
		if err := h.rdb.Set(ctx, key, report.Sent, 45*24*time.Hour).Err(); err != nil {
			h.logger.Error().Err(err).Str("tenant", t.ID.String()).Msg("usage counter write failed")
		}
	})
}
