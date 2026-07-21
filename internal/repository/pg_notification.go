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
		       asynq_task_id, retry_count, max_retries, last_error, delivered_at, provider_msg_id, created_at, updated_at
		FROM notifications WHERE id = $1`
	n := &domain.Notification{}
	err := r.pool.QueryRow(ctx, query, id).Scan(
		&n.ID, &n.TenantID, &n.ChannelConfigID, &n.Channel, &n.Subject, &n.Body, &n.Metadata, &n.Status,
		&n.AsynqTaskID, &n.RetryCount, &n.MaxRetries, &n.LastError, &n.DeliveredAt, &n.ProviderMsgID, &n.CreatedAt, &n.UpdatedAt)
	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("%w: notification not found", domain.ErrNotFound)
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
		return fmt.Errorf("%w: notification not found", domain.ErrNotFound)
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
		return fmt.Errorf("%w: notification not found", domain.ErrNotFound)
	}
	return nil
}

func (r *PgNotificationRepo) SetProviderMsgID(ctx context.Context, id uuid.UUID, providerMsgID string) error {
	query := `UPDATE notifications SET provider_msg_id = $2, updated_at = $3 WHERE id = $1`
	ct, err := r.pool.Exec(ctx, query, id, providerMsgID, time.Now())
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("%w: notification not found", domain.ErrNotFound)
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
		return fmt.Errorf("%w: notification not found", domain.ErrNotFound)
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
		return fmt.Errorf("%w: notification not found", domain.ErrNotFound)
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
		       asynq_task_id, retry_count, max_retries, last_error, delivered_at, provider_msg_id, created_at, updated_at
		FROM notifications WHERE %s ORDER BY created_at DESC LIMIT $%d OFFSET $%d`, where, argIdx, argIdx+1)
	args = append(args, limit, offset)

	rows, err := r.pool.Query(ctx, dataQuery, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	notifications := make([]*domain.Notification, 0)
	for rows.Next() {
		n := &domain.Notification{}
		err := rows.Scan(
			&n.ID, &n.TenantID, &n.ChannelConfigID, &n.Channel, &n.Subject, &n.Body, &n.Metadata, &n.Status,
			&n.AsynqTaskID, &n.RetryCount, &n.MaxRetries, &n.LastError, &n.DeliveredAt, &n.ProviderMsgID, &n.CreatedAt, &n.UpdatedAt)
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

func (r *PgNotificationRepo) UsageByTenant(ctx context.Context, tenantID uuid.UUID, from, to time.Time) (*domain.UsageReport, error) {
	report := &domain.UsageReport{ByChannel: map[string]int64{}}

	err := r.pool.QueryRow(ctx, `
		SELECT COUNT(*),
		       COUNT(*) FILTER (WHERE n.status = 'delivered'),
		       COUNT(*) FILTER (WHERE n.status = 'failed')
		FROM notifications n
		WHERE n.tenant_id = $1 AND n.created_at >= $2 AND n.created_at < $3`,
		tenantID, from, to).Scan(&report.Sent, &report.Delivered, &report.Failed)
	if err != nil {
		return nil, err
	}

	rows, err := r.pool.Query(ctx, `
		SELECT c.channel, COUNT(*)
		FROM notifications n
		JOIN channel_configs c ON c.id = n.channel_config_id
		WHERE n.tenant_id = $1 AND n.created_at >= $2 AND n.created_at < $3
		GROUP BY c.channel`, tenantID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var channel string
		var count int64
		if err := rows.Scan(&channel, &count); err != nil {
			return nil, err
		}
		report.ByChannel[channel] = count
	}
	return report, rows.Err()
}

// DeleteOlderThan removes notifications and their associated delivery records
// for a given tenant that were created before the cutoff time. Child rows in
// delivery_attempts and delivery_metrics are deleted first so this works
// regardless of FK cascade settings on the older migrations.
func (r *PgNotificationRepo) DeleteOlderThan(ctx context.Context, tenantID uuid.UUID, cutoff time.Time) (int64, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if _, err := tx.Exec(ctx, `
		DELETE FROM delivery_attempts WHERE notification_id IN
			(SELECT id FROM notifications WHERE tenant_id = $1 AND created_at < $2)`, tenantID, cutoff); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM delivery_metrics WHERE notification_id IN
			(SELECT id FROM notifications WHERE tenant_id = $1 AND created_at < $2)`, tenantID, cutoff); err != nil {
		return 0, err
	}
	tag, err := tx.Exec(ctx,
		`DELETE FROM notifications WHERE tenant_id = $1 AND created_at < $2`, tenantID, cutoff)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), tx.Commit(ctx)
}

// CountByStatus returns a map of notification status to count across all tenants.
// It issues a single aggregation query rather than N per-tenant queries.
func (r *PgNotificationRepo) CountByStatus(ctx context.Context) (map[domain.NotificationStatus]int, error) {
	query := `SELECT status, COUNT(*) FROM notifications GROUP BY status`
	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	counts := make(map[domain.NotificationStatus]int)
	for rows.Next() {
		var status domain.NotificationStatus
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return nil, err
		}
		counts[status] = count
	}
	return counts, rows.Err()
}
