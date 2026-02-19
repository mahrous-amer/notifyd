package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bse/notifyd/internal/domain"
)

type PgDeliveryAttemptRepo struct {
	pool *pgxpool.Pool
}

func NewPgDeliveryAttemptRepo(pool *pgxpool.Pool) *PgDeliveryAttemptRepo {
	return &PgDeliveryAttemptRepo{pool: pool}
}

func (r *PgDeliveryAttemptRepo) Create(ctx context.Context, a *domain.DeliveryAttempt) error {
	query := `
		INSERT INTO delivery_attempts (id, notification_id, attempt_number, status, provider_response, error_message, duration_ms, attempted_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`
	_, err := r.pool.Exec(ctx, query,
		a.ID, a.NotificationID, a.AttemptNumber, a.Status, a.ProviderResponse, a.ErrorMessage, a.DurationMs, a.AttemptedAt)
	return err
}

func (r *PgDeliveryAttemptRepo) ListByNotification(ctx context.Context, notificationID uuid.UUID) ([]*domain.DeliveryAttempt, error) {
	query := `
		SELECT id, notification_id, attempt_number, status, provider_response, error_message, duration_ms, attempted_at
		FROM delivery_attempts WHERE notification_id = $1 ORDER BY attempt_number ASC`
	rows, err := r.pool.Query(ctx, query, notificationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var attempts []*domain.DeliveryAttempt
	for rows.Next() {
		a := &domain.DeliveryAttempt{}
		err := rows.Scan(&a.ID, &a.NotificationID, &a.AttemptNumber, &a.Status, &a.ProviderResponse, &a.ErrorMessage, &a.DurationMs, &a.AttemptedAt)
		if err != nil {
			return nil, err
		}
		attempts = append(attempts, a)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return attempts, nil
}
