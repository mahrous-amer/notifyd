package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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
	deliveryPrefsJSON, err := marshalDeliveryPrefs(cfg.DeliveryPrefs)
	if err != nil {
		return err
	}

	query := `
		INSERT INTO channel_configs (id, tenant_id, channel, name, config, is_active, delivery_prefs, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`
	_, err = r.pool.Exec(ctx, query,
		cfg.ID, cfg.TenantID, cfg.Channel, cfg.Name, cfg.Config, cfg.IsActive, deliveryPrefsJSON, cfg.CreatedAt, cfg.UpdatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return fmt.Errorf("%w: %s", domain.ErrValidationFailed, pgErr.ConstraintName)
		}
		return err
	}
	return nil
}

func (r *PgChannelConfigRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.ChannelConfig, error) {
	query := `
		SELECT id, tenant_id, channel, name, config, is_active, delivery_prefs, created_at, updated_at
		FROM channel_configs WHERE id = $1`
	return r.scanConfig(r.pool.QueryRow(ctx, query, id))
}

func (r *PgChannelConfigRepo) ListByTenant(ctx context.Context, tenantID uuid.UUID) ([]*domain.ChannelConfig, error) {
	query := `
		SELECT id, tenant_id, channel, name, config, is_active, delivery_prefs, created_at, updated_at
		FROM channel_configs WHERE tenant_id = $1 ORDER BY created_at DESC`
	return r.queryConfigs(ctx, query, tenantID)
}

func (r *PgChannelConfigRepo) ListByTenantAndChannel(ctx context.Context, tenantID uuid.UUID, ch domain.ChannelType) ([]*domain.ChannelConfig, error) {
	query := `
		SELECT id, tenant_id, channel, name, config, is_active, delivery_prefs, created_at, updated_at
		FROM channel_configs WHERE tenant_id = $1 AND channel = $2 ORDER BY created_at DESC`
	return r.queryConfigs(ctx, query, tenantID, ch)
}

func (r *PgChannelConfigRepo) Update(ctx context.Context, id uuid.UUID, tenantID uuid.UUID, input domain.UpdateChannelConfigInput) (*domain.ChannelConfig, error) {
	deliveryPrefsJSON, err := marshalDeliveryPrefs(input.DeliveryPrefs)
	if err != nil {
		return nil, err
	}

	query := `
		UPDATE channel_configs
		SET name = COALESCE($3, name),
		    config = COALESCE($4, config),
		    is_active = COALESCE($5, is_active),
		    delivery_prefs = COALESCE($6, delivery_prefs),
		    updated_at = NOW()
		WHERE id = $1 AND tenant_id = $2
		RETURNING id, tenant_id, channel, name, config, is_active, delivery_prefs, created_at, updated_at`

	row := r.pool.QueryRow(ctx, query, id, tenantID, input.Name, input.Config, input.IsActive, deliveryPrefsJSON)
	cfg, err := r.scanConfig(row)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, fmt.Errorf("%w: channel config not found", domain.ErrNotFound)
		}
		return nil, err
	}
	return cfg, nil
}

func (r *PgChannelConfigRepo) Delete(ctx context.Context, id uuid.UUID, tenantID uuid.UUID) error {
	query := `DELETE FROM channel_configs WHERE id = $1 AND tenant_id = $2`
	tag, err := r.pool.Exec(ctx, query, id, tenantID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: channel config not found", domain.ErrNotFound)
	}
	return nil
}

func (r *PgChannelConfigRepo) scanConfig(row pgx.Row) (*domain.ChannelConfig, error) {
	cfg := &domain.ChannelConfig{}
	var deliveryPrefsJSON []byte

	err := row.Scan(
		&cfg.ID, &cfg.TenantID, &cfg.Channel, &cfg.Name, &cfg.Config,
		&cfg.IsActive, &deliveryPrefsJSON, &cfg.CreatedAt, &cfg.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("%w: channel config not found", domain.ErrNotFound)
	}
	if err != nil {
		return nil, err
	}

	cfg.DeliveryPrefs = unmarshalDeliveryPrefs(deliveryPrefsJSON)
	return cfg, nil
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
		var deliveryPrefsJSON []byte

		err := rows.Scan(
			&cfg.ID, &cfg.TenantID, &cfg.Channel, &cfg.Name, &cfg.Config,
			&cfg.IsActive, &deliveryPrefsJSON, &cfg.CreatedAt, &cfg.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		cfg.DeliveryPrefs = unmarshalDeliveryPrefs(deliveryPrefsJSON)
		configs = append(configs, cfg)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return configs, nil
}

// marshalDeliveryPrefs converts a DeliveryPreferences pointer to JSON bytes
// suitable for a JSONB column. A nil pointer produces a nil slice, which
// causes the database to keep the existing value in COALESCE expressions.
func marshalDeliveryPrefs(prefs *domain.DeliveryPreferences) ([]byte, error) {
	if prefs == nil {
		return nil, nil
	}
	data, err := json.Marshal(prefs)
	if err != nil {
		return nil, fmt.Errorf("marshal delivery_prefs: %w", err)
	}
	return data, nil
}

// unmarshalDeliveryPrefs parses JSONB bytes from the database. It returns nil
// when the column is NULL or contains an empty JSON object, keeping the domain
// model clean of zero-value structs.
func unmarshalDeliveryPrefs(data []byte) *domain.DeliveryPreferences {
	if len(data) == 0 || string(data) == "{}" || string(data) == "null" {
		return nil
	}
	var prefs domain.DeliveryPreferences
	if err := json.Unmarshal(data, &prefs); err != nil {
		return nil
	}
	return &prefs
}
