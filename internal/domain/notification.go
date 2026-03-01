package domain

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type NotificationStatus string

const (
	StatusPending    NotificationStatus = "pending"
	StatusProcessing NotificationStatus = "processing"
	StatusDelivered  NotificationStatus = "delivered"
	StatusFailed     NotificationStatus = "failed"
	StatusRetrying   NotificationStatus = "retrying"
)

func IsValidNotificationStatus(s NotificationStatus) bool {
	switch s {
	case StatusPending, StatusProcessing, StatusDelivered, StatusFailed, StatusRetrying:
		return true
	}
	return false
}

type Notification struct {
	ID              uuid.UUID          `json:"id"`
	TenantID        uuid.UUID          `json:"tenant_id"`
	ChannelConfigID uuid.UUID          `json:"channel_config_id"`
	Channel         ChannelType        `json:"channel"`
	Subject         *string            `json:"subject,omitempty"`
	Body            string             `json:"body"`
	Metadata        json.RawMessage    `json:"metadata,omitempty"`
	Status          NotificationStatus `json:"status"`
	AsynqTaskID     *string            `json:"asynq_task_id,omitempty"`
	RetryCount      int                `json:"retry_count"`
	MaxRetries      int                `json:"max_retries"`
	LastError       *string            `json:"last_error,omitempty"`
	DeliveredAt     *time.Time         `json:"delivered_at,omitempty"`
	ProviderMsgID   *string            `json:"provider_msg_id,omitempty"`
	CreatedAt       time.Time          `json:"created_at"`
	UpdatedAt       time.Time          `json:"updated_at"`
}

type SendNotificationInput struct {
	ChannelConfigID uuid.UUID       `json:"channel_config_id"`
	Subject         *string         `json:"subject,omitempty"`
	Body            string          `json:"body"`
	Metadata        json.RawMessage `json:"metadata,omitempty"`
}

type SendMultiInput struct {
	Channels []SendNotificationInput `json:"channels"`
}

type NotificationFilter struct {
	TenantID uuid.UUID
	Status   *NotificationStatus
	Channel  *ChannelType
	Limit    int
	Offset   int
}

type NotificationRepository interface {
	Create(ctx context.Context, n *Notification) error
	GetByID(ctx context.Context, id uuid.UUID) (*Notification, error)
	UpdateStatus(ctx context.Context, id uuid.UUID, status NotificationStatus, lastError *string) error
	SetAsynqTaskID(ctx context.Context, id uuid.UUID, taskID string) error
	SetProviderMsgID(ctx context.Context, id uuid.UUID, providerMsgID string) error
	MarkDelivered(ctx context.Context, id uuid.UUID) error
	IncrementRetry(ctx context.Context, id uuid.UUID, lastError string) error
	List(ctx context.Context, filter NotificationFilter) ([]*Notification, int, error)
	CountByStatus(ctx context.Context) (map[NotificationStatus]int, error)
}
