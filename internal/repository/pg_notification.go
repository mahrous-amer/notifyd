package repository

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bse/notifyd/internal/domain"
)

type PgNotificationRepo struct {
	pool *pgxpool.Pool
}

func NewPgNotificationRepo(pool *pgxpool.Pool) *PgNotificationRepo {
	return &PgNotificationRepo{pool: pool}
}

func (r *PgNotificationRepo) Create(ctx context.Context, n *domain.Notification) error {
	query := `
		INSERT INTO notifications (id, tenant_id, channel_config_id, channel, subject, body, metadata, status, max_retries, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`
	_, err := r.pool.Exec(ctx, query,
		n.ID, n.TenantID, n.ChannelConfigID, n.Channel, n.Subject, n.Body, n.Metadata, n.Status, n.MaxRetries, n.CreatedAt, n.UpdatedAt)
	return err
}

func (r *PgNotificationRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.Notification, error) {
	query := `
		SELECT id, tenant_id, channel_config_id, channel, subject, body, metadata, status,
		       asynq_task_id, retry_count, max_retries, last_error, delivered_at, created_at, updated_at
		FROM notifications WHERE id = $1`
	n := &domain.Notification{}
	err := r.pool.QueryRow(ctx, query, id).Scan(
		&n.ID, &n.TenantID, &n.ChannelConfigID, &n.Channel, &n.Subject, &n.Body, &n.Metadata, &n.Status,
		&n.AsynqTaskID, &n.RetryCount, &n.MaxRetries, &n.LastError, &n.DeliveredAt, &n.CreatedAt, &n.UpdatedAt)
	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("notification not found")
	}
	return n, err
}

func (r *PgNotificationRepo) UpdateStatus(ctx context.Context, id uuid.UUID, status domain.NotificationStatus, lastError *string) error {
	query := `UPDATE notifications SET status = $2, last_error = $3, updated_at = $4 WHERE id = $1`
	ct, err := r.pool.Exec(ctx, query, id, status, lastError, time.Now())
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("notification not found")
	}
	return nil
}

func (r *PgNotificationRepo) SetAsynqTaskID(ctx context.Context, id uuid.UUID, taskID string) error {
	query := `UPDATE notifications SET asynq_task_id = $2, updated_at = $3 WHERE id = $1`
	ct, err := r.pool.Exec(ctx, query, id, taskID, time.Now())
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("notification not found")
	}
	return nil
}

func (r *PgNotificationRepo) MarkDelivered(ctx context.Context, id uuid.UUID) error {
	now := time.Now()
	query := `UPDATE notifications SET status = 'delivered', delivered_at = $2, updated_at = $2 WHERE id = $1`
	ct, err := r.pool.Exec(ctx, query, id, now)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("notification not found")
	}
	return nil
}

func (r *PgNotificationRepo) IncrementRetry(ctx context.Context, id uuid.UUID, lastError string) error {
	query := `UPDATE notifications SET retry_count = retry_count + 1, last_error = $2, updated_at = $3 WHERE id = $1`
	ct, err := r.pool.Exec(ctx, query, id, lastError, time.Now())
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("notification not found")
	}
	return nil
}

func (r *PgNotificationRepo) List(ctx context.Context, filter domain.NotificationFilter) ([]*domain.Notification, int, error) {
	conditions := []string{"tenant_id = $1"}
	args := []interface{}{filter.TenantID}
	argIdx := 2

	if filter.Status != nil {
		conditions = append(conditions, fmt.Sprintf("status = $%d", argIdx))
		args = append(args, *filter.Status)
		argIdx++
	}
	if filter.Channel != nil {
		conditions = append(conditions, fmt.Sprintf("channel = $%d", argIdx))
		args = append(args, *filter.Channel)
		argIdx++
	}

	where := strings.Join(conditions, " AND ")

	var total int
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM notifications WHERE %s", where)
	err := r.pool.QueryRow(ctx, countQuery, args...).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	limit := filter.Limit
	if limit <= 0 {
		limit = 20
	}
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}

	dataQuery := fmt.Sprintf(`
		SELECT id, tenant_id, channel_config_id, channel, subject, body, metadata, status,
		       asynq_task_id, retry_count, max_retries, last_error, delivered_at, created_at, updated_at
		FROM notifications WHERE %s ORDER BY created_at DESC LIMIT $%d OFFSET $%d`, where, argIdx, argIdx+1)
	args = append(args, limit, offset)

	rows, err := r.pool.Query(ctx, dataQuery, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var notifications []*domain.Notification
	for rows.Next() {
		n := &domain.Notification{}
		err := rows.Scan(
			&n.ID, &n.TenantID, &n.ChannelConfigID, &n.Channel, &n.Subject, &n.Body, &n.Metadata, &n.Status,
			&n.AsynqTaskID, &n.RetryCount, &n.MaxRetries, &n.LastError, &n.DeliveredAt, &n.CreatedAt, &n.UpdatedAt)
		if err != nil {
			return nil, 0, err
		}
		notifications = append(notifications, n)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return notifications, total, nil
}
