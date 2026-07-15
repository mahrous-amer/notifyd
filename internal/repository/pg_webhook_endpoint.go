package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bse/notifyd/internal/domain"
)

type PgWebhookEndpointRepo struct {
	pool *pgxpool.Pool
}

func NewPgWebhookEndpointRepo(pool *pgxpool.Pool) *PgWebhookEndpointRepo {
	return &PgWebhookEndpointRepo{pool: pool}
}

// CreateWithinLimit atomically creates e if the tenant's endpoint count is
// below limit, returning ErrWebhookLimitReached otherwise. Mirrors
// PgAPIKeyRepo.CreateWithinLimit: locking the tenant row serializes concurrent
// creates for the same tenant so the count-then-insert cannot race.
//
// The FOR UPDATE lock is taken on the same tenants row PgAPIKeyRepo's
// CreateWithinLimit locks for API key creation — a deliberate coupling, not
// an accident: both are "check a per-tenant cap, then insert" operations
// serialized by locking the same parent row, so a concurrent webhook-endpoint
// create and API-key create for the same tenant simply queue behind each
// other rather than racing. This is fine at human-driven creation rates
// (a tenant clicking "add endpoint" a few times a day); it would become a
// contention point only if either operation started happening at a rate
// this design never anticipated.
func (r *PgWebhookEndpointRepo) CreateWithinLimit(ctx context.Context, e *domain.WebhookEndpoint, limit int) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var tenantID uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT id FROM tenants WHERE id = $1 FOR UPDATE`, e.TenantID).Scan(&tenantID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrNotFound
		}
		return err
	}

	var count int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM webhook_endpoints WHERE tenant_id = $1`, e.TenantID).Scan(&count); err != nil {
		return err
	}
	if count >= limit {
		return domain.ErrWebhookLimitReached
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO webhook_endpoints (id, tenant_id, url, secret, events, is_active, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		e.ID, e.TenantID, e.URL, e.Secret, e.Events, e.IsActive, e.CreatedAt); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *PgWebhookEndpointRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.WebhookEndpoint, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, tenant_id, url, secret, events, is_active, created_at
		FROM webhook_endpoints WHERE id = $1`, id)
	return scanWebhookEndpoint(row)
}

func (r *PgWebhookEndpointRepo) ListByTenant(ctx context.Context, tenantID uuid.UUID) ([]*domain.WebhookEndpoint, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, url, secret, events, is_active, created_at
		FROM webhook_endpoints WHERE tenant_id = $1 ORDER BY created_at`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectWebhookEndpoints(rows)
}

// ListActiveByTenantAndEvent returns active endpoints subscribed to
// eventType. The events filter uses Postgres's array containment operator
// (@>) so the check happens in the query rather than after fetching every
// endpoint for the tenant.
func (r *PgWebhookEndpointRepo) ListActiveByTenantAndEvent(ctx context.Context, tenantID uuid.UUID, eventType domain.WebhookEventType) ([]*domain.WebhookEndpoint, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, url, secret, events, is_active, created_at
		FROM webhook_endpoints
		WHERE tenant_id = $1 AND is_active = true AND events @> ARRAY[$2::text]`,
		tenantID, string(eventType))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectWebhookEndpoints(rows)
}

func (r *PgWebhookEndpointRepo) Update(ctx context.Context, id, tenantID uuid.UUID, input domain.UpdateWebhookEndpointInput) (*domain.WebhookEndpoint, error) {
	var eventsParam interface{}
	if input.Events != nil {
		eventsParam = input.Events
	}

	row := r.pool.QueryRow(ctx, `
		UPDATE webhook_endpoints
		SET url        = COALESCE($3, url),
		    events     = COALESCE($4, events),
		    is_active  = COALESCE($5, is_active)
		WHERE id = $1 AND tenant_id = $2
		RETURNING id, tenant_id, url, secret, events, is_active, created_at`,
		id, tenantID, input.URL, eventsParam, input.IsActive)

	endpoint, err := scanWebhookEndpoint(row)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, fmt.Errorf("%w: webhook endpoint not found", domain.ErrNotFound)
		}
		return nil, err
	}
	return endpoint, nil
}

func (r *PgWebhookEndpointRepo) Delete(ctx context.Context, id, tenantID uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM webhook_endpoints WHERE id = $1 AND tenant_id = $2`, id, tenantID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: webhook endpoint not found", domain.ErrNotFound)
	}
	return nil
}

func scanWebhookEndpoint(row pgx.Row) (*domain.WebhookEndpoint, error) {
	e := &domain.WebhookEndpoint{}
	err := row.Scan(&e.ID, &e.TenantID, &e.URL, &e.Secret, &e.Events, &e.IsActive, &e.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("%w: webhook endpoint not found", domain.ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	return e, nil
}

func collectWebhookEndpoints(rows pgx.Rows) ([]*domain.WebhookEndpoint, error) {
	var out []*domain.WebhookEndpoint
	for rows.Next() {
		e := &domain.WebhookEndpoint{}
		if err := rows.Scan(&e.ID, &e.TenantID, &e.URL, &e.Secret, &e.Events, &e.IsActive, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
