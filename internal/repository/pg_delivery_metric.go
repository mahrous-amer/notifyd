package repository

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bse/notifyd/internal/domain"
)

type PgDeliveryMetricRepo struct {
	pool *pgxpool.Pool
}

func NewPgDeliveryMetricRepo(pool *pgxpool.Pool) *PgDeliveryMetricRepo {
	return &PgDeliveryMetricRepo{pool: pool}
}

// Upsert inserts or replaces the delivery metric for a notification. The
// unique constraint on notification_id ensures only one record per notification.
func (r *PgDeliveryMetricRepo) Upsert(ctx context.Context, m *domain.DeliveryMetric) error {
	query := `
		INSERT INTO delivery_metrics (id, notification_id, provider_msg_id, delivered_at, read_at, interactions, collected_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (notification_id) DO UPDATE
		SET provider_msg_id = EXCLUDED.provider_msg_id,
		    delivered_at    = EXCLUDED.delivered_at,
		    read_at         = EXCLUDED.read_at,
		    interactions    = EXCLUDED.interactions,
		    collected_at    = EXCLUDED.collected_at`
	_, err := r.pool.Exec(ctx, query,
		m.ID, m.NotificationID, m.ProviderMsgID, m.DeliveredAt, m.ReadAt, m.Interactions, m.CollectedAt)
	if err != nil {
		return fmt.Errorf("upsert delivery metric: %w", err)
	}
	return nil
}

func (r *PgDeliveryMetricRepo) GetByNotificationID(ctx context.Context, notificationID uuid.UUID) (*domain.DeliveryMetric, error) {
	query := `
		SELECT id, notification_id, provider_msg_id, delivered_at, read_at, interactions, collected_at
		FROM delivery_metrics
		WHERE notification_id = $1`

	m := &domain.DeliveryMetric{}
	err := r.pool.QueryRow(ctx, query, notificationID).Scan(
		&m.ID, &m.NotificationID, &m.ProviderMsgID, &m.DeliveredAt, &m.ReadAt, &m.Interactions, &m.CollectedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("%w: delivery metric not found", domain.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("get delivery metric: %w", err)
	}
	return m, nil
}
