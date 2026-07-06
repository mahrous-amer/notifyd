package repository

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bse/notifyd/internal/domain"
)

type PgEntitlementRepo struct {
	pool *pgxpool.Pool
}

func NewPgEntitlementRepo(pool *pgxpool.Pool) *PgEntitlementRepo {
	return &PgEntitlementRepo{pool: pool}
}

func (r *PgEntitlementRepo) Upsert(ctx context.Context, e *domain.Entitlements) error {
	channels := make([]string, len(e.AllowedChannels))
	for i, c := range e.AllowedChannels {
		channels[i] = string(c)
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO tenant_entitlements
			(tenant_id, plan_code, message_limit, allowed_channels, api_key_limit, retention_days, period_start, period_end, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW())
		ON CONFLICT (tenant_id) DO UPDATE SET
			plan_code = EXCLUDED.plan_code,
			message_limit = EXCLUDED.message_limit,
			allowed_channels = EXCLUDED.allowed_channels,
			api_key_limit = EXCLUDED.api_key_limit,
			retention_days = EXCLUDED.retention_days,
			period_start = EXCLUDED.period_start,
			period_end = EXCLUDED.period_end,
			updated_at = NOW()`,
		e.TenantID, e.PlanCode, e.MessageLimit, channels, e.APIKeyLimit, e.RetentionDays, e.PeriodStart, e.PeriodEnd)
	return err
}

func (r *PgEntitlementRepo) GetByTenantID(ctx context.Context, tenantID uuid.UUID) (*domain.Entitlements, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT tenant_id, plan_code, message_limit, allowed_channels, api_key_limit, retention_days, period_start, period_end, updated_at
		FROM tenant_entitlements WHERE tenant_id = $1`, tenantID)
	e, err := scanEntitlements(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	return e, err
}

func (r *PgEntitlementRepo) ListAll(ctx context.Context) ([]*domain.Entitlements, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT tenant_id, plan_code, message_limit, allowed_channels, api_key_limit, retention_days, period_start, period_end, updated_at
		FROM tenant_entitlements ORDER BY tenant_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.Entitlements
	for rows.Next() {
		e, err := scanEntitlements(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanEntitlements(row rowScanner) (*domain.Entitlements, error) {
	var e domain.Entitlements
	var channels []string
	if err := row.Scan(&e.TenantID, &e.PlanCode, &e.MessageLimit, &channels,
		&e.APIKeyLimit, &e.RetentionDays, &e.PeriodStart, &e.PeriodEnd, &e.UpdatedAt); err != nil {
		return nil, err
	}
	e.AllowedChannels = make([]domain.ChannelType, len(channels))
	for i, c := range channels {
		e.AllowedChannels[i] = domain.ChannelType(c)
	}
	return &e, nil
}
