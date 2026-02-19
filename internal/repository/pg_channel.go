package repository

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bse/notifyd/internal/domain"
)

type PgChannelConfigRepo struct {
	pool *pgxpool.Pool
}

func NewPgChannelConfigRepo(pool *pgxpool.Pool) *PgChannelConfigRepo {
	return &PgChannelConfigRepo{pool: pool}
}

func (r *PgChannelConfigRepo) Create(ctx context.Context, cfg *domain.ChannelConfig) error {
	query := `
		INSERT INTO channel_configs (id, tenant_id, channel, name, config, is_active, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`
	_, err := r.pool.Exec(ctx, query,
		cfg.ID, cfg.TenantID, cfg.Channel, cfg.Name, cfg.Config, cfg.IsActive, cfg.CreatedAt, cfg.UpdatedAt)
	return err
}

func (r *PgChannelConfigRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.ChannelConfig, error) {
	query := `
		SELECT id, tenant_id, channel, name, config, is_active, created_at, updated_at
		FROM channel_configs WHERE id = $1`
	return r.scanConfig(r.pool.QueryRow(ctx, query, id))
}

func (r *PgChannelConfigRepo) ListByTenant(ctx context.Context, tenantID uuid.UUID) ([]*domain.ChannelConfig, error) {
	query := `
		SELECT id, tenant_id, channel, name, config, is_active, created_at, updated_at
		FROM channel_configs WHERE tenant_id = $1 ORDER BY created_at DESC`
	return r.queryConfigs(ctx, query, tenantID)
}

func (r *PgChannelConfigRepo) ListByTenantAndChannel(ctx context.Context, tenantID uuid.UUID, ch domain.ChannelType) ([]*domain.ChannelConfig, error) {
	query := `
		SELECT id, tenant_id, channel, name, config, is_active, created_at, updated_at
		FROM channel_configs WHERE tenant_id = $1 AND channel = $2 ORDER BY created_at DESC`
	return r.queryConfigs(ctx, query, tenantID, ch)
}

func (r *PgChannelConfigRepo) Update(ctx context.Context, id uuid.UUID, tenantID uuid.UUID, input domain.UpdateChannelConfigInput) (*domain.ChannelConfig, error) {
	query := `
		UPDATE channel_configs
		SET name = COALESCE($3, name),
		    config = COALESCE($4, config),
		    is_active = COALESCE($5, is_active),
		    updated_at = NOW()
		WHERE id = $1 AND tenant_id = $2
		RETURNING id, tenant_id, channel, name, config, is_active, created_at, updated_at`
	cfg := &domain.ChannelConfig{}
	err := r.pool.QueryRow(ctx, query, id, tenantID, input.Name, input.Config, input.IsActive).Scan(
		&cfg.ID, &cfg.TenantID, &cfg.Channel, &cfg.Name, &cfg.Config, &cfg.IsActive, &cfg.CreatedAt, &cfg.UpdatedAt)
	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("channel config not found")
	}
	return cfg, err
}

func (r *PgChannelConfigRepo) Delete(ctx context.Context, id uuid.UUID, tenantID uuid.UUID) error {
	query := `DELETE FROM channel_configs WHERE id = $1 AND tenant_id = $2`
	tag, err := r.pool.Exec(ctx, query, id, tenantID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("channel config not found")
	}
	return nil
}

func (r *PgChannelConfigRepo) scanConfig(row pgx.Row) (*domain.ChannelConfig, error) {
	cfg := &domain.ChannelConfig{}
	err := row.Scan(&cfg.ID, &cfg.TenantID, &cfg.Channel, &cfg.Name, &cfg.Config, &cfg.IsActive, &cfg.CreatedAt, &cfg.UpdatedAt)
	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("channel config not found")
	}
	return cfg, err
}

func (r *PgChannelConfigRepo) queryConfigs(ctx context.Context, query string, args ...interface{}) ([]*domain.ChannelConfig, error) {
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var configs []*domain.ChannelConfig
	for rows.Next() {
		cfg := &domain.ChannelConfig{}
		err := rows.Scan(&cfg.ID, &cfg.TenantID, &cfg.Channel, &cfg.Name, &cfg.Config, &cfg.IsActive, &cfg.CreatedAt, &cfg.UpdatedAt)
		if err != nil {
			return nil, err
		}
		configs = append(configs, cfg)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return configs, nil
}
