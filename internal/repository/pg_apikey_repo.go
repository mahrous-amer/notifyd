package repository

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bse/notifyd/internal/domain"
)

type PgAPIKeyRepo struct {
	pool *pgxpool.Pool
}

func NewPgAPIKeyRepo(pool *pgxpool.Pool) *PgAPIKeyRepo {
	return &PgAPIKeyRepo{pool: pool}
}

func (r *PgAPIKeyRepo) Create(ctx context.Context, k *domain.APIKey) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO api_keys (id, tenant_id, api_key, api_secret_hash, label, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		k.ID, k.TenantID, k.APIKey, k.APISecretHash, k.Label, k.CreatedAt)
	return err
}

func (r *PgAPIKeyRepo) GetByAPIKey(ctx context.Context, apiKey string) (*domain.APIKey, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, tenant_id, api_key, api_secret_hash, label, created_at, revoked_at
		FROM api_keys WHERE api_key = $1`, apiKey)
	var k domain.APIKey
	err := row.Scan(&k.ID, &k.TenantID, &k.APIKey, &k.APISecretHash, &k.Label, &k.CreatedAt, &k.RevokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &k, nil
}

func (r *PgAPIKeyRepo) ListByTenant(ctx context.Context, tenantID uuid.UUID) ([]*domain.APIKey, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, api_key, api_secret_hash, label, created_at, revoked_at
		FROM api_keys WHERE tenant_id = $1 ORDER BY created_at`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.APIKey
	for rows.Next() {
		var k domain.APIKey
		if err := rows.Scan(&k.ID, &k.TenantID, &k.APIKey, &k.APISecretHash, &k.Label, &k.CreatedAt, &k.RevokedAt); err != nil {
			return nil, err
		}
		out = append(out, &k)
	}
	return out, rows.Err()
}

func (r *PgAPIKeyRepo) Revoke(ctx context.Context, id, tenantID uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE api_keys SET revoked_at = NOW()
		WHERE id = $1 AND tenant_id = $2 AND revoked_at IS NULL`, id, tenantID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *PgAPIKeyRepo) CountActiveByTenant(ctx context.Context, tenantID uuid.UUID) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM api_keys WHERE tenant_id = $1 AND revoked_at IS NULL`, tenantID).Scan(&n)
	return n, err
}
