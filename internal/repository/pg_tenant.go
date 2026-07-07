package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bse/notifyd/internal/domain"
)

type PgTenantRepo struct {
	pool *pgxpool.Pool
}

func NewPgTenantRepo(pool *pgxpool.Pool) *PgTenantRepo {
	return &PgTenantRepo{pool: pool}
}

func (r *PgTenantRepo) Create(ctx context.Context, t *domain.Tenant) error {
	query := `
		INSERT INTO tenants (id, name, slug, api_key, api_secret, is_active, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`
	_, err := r.pool.Exec(ctx, query,
		t.ID, t.Name, t.Slug, t.APIKey, t.APISecret, t.IsActive, t.CreatedAt, t.UpdatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return fmt.Errorf("%w: %s", domain.ErrValidationFailed, pgErr.ConstraintName)
		}
		return err
	}
	return nil
}

func (r *PgTenantRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.Tenant, error) {
	query := `
		SELECT id, name, slug, api_key, api_secret, is_active, created_at, updated_at
		FROM tenants WHERE id = $1`
	return r.scanTenant(r.pool.QueryRow(ctx, query, id))
}

func (r *PgTenantRepo) GetBySlug(ctx context.Context, slug string) (*domain.Tenant, error) {
	query := `
		SELECT id, name, slug, api_key, api_secret, is_active, created_at, updated_at
		FROM tenants WHERE slug = $1`
	return r.scanTenant(r.pool.QueryRow(ctx, query, slug))
}

func (r *PgTenantRepo) Update(ctx context.Context, id uuid.UUID, input domain.UpdateTenantInput) (*domain.Tenant, error) {
	query := `
		UPDATE tenants
		SET name = COALESCE($2, name),
		    is_active = COALESCE($3, is_active),
		    updated_at = NOW()
		WHERE id = $1
		RETURNING id, name, slug, api_key, api_secret, is_active, created_at, updated_at`
	t := &domain.Tenant{}
	err := r.pool.QueryRow(ctx, query, id, input.Name, input.IsActive).Scan(
		&t.ID, &t.Name, &t.Slug, &t.APIKey, &t.APISecret, &t.IsActive, &t.CreatedAt, &t.UpdatedAt)
	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("%w: tenant not found", domain.ErrNotFound)
	}
	return t, err
}

func (r *PgTenantRepo) Delete(ctx context.Context, id uuid.UUID) error {
	query := `DELETE FROM tenants WHERE id = $1`
	tag, err := r.pool.Exec(ctx, query, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: tenant not found", domain.ErrNotFound)
	}
	return nil
}

func (r *PgTenantRepo) List(ctx context.Context, limit, offset int) ([]*domain.Tenant, int, error) {
	var total int
	err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM tenants`).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	query := `
		SELECT id, name, slug, api_key, api_secret, is_active, created_at, updated_at
		FROM tenants ORDER BY created_at DESC LIMIT $1 OFFSET $2`
	rows, err := r.pool.Query(ctx, query, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var tenants []*domain.Tenant
	for rows.Next() {
		t, err := r.scanTenantRow(rows)
		if err != nil {
			return nil, 0, err
		}
		tenants = append(tenants, t)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return tenants, total, nil
}

func (r *PgTenantRepo) scanTenant(row pgx.Row) (*domain.Tenant, error) {
	t := &domain.Tenant{}
	err := row.Scan(&t.ID, &t.Name, &t.Slug, &t.APIKey, &t.APISecret, &t.IsActive, &t.CreatedAt, &t.UpdatedAt)
	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("%w: tenant not found", domain.ErrNotFound)
	}
	return t, err
}

func (r *PgTenantRepo) scanTenantRow(rows pgx.Rows) (*domain.Tenant, error) {
	t := &domain.Tenant{}
	err := rows.Scan(&t.ID, &t.Name, &t.Slug, &t.APIKey, &t.APISecret, &t.IsActive, &t.CreatedAt, &t.UpdatedAt)
	return t, err
}
