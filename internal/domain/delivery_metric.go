package domain

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// DeliveryMetric persists post-delivery engagement data collected from a
// provider after a notification has been sent. One record exists per
// notification (unique on notification_id).
type DeliveryMetric struct {
	ID             uuid.UUID       `json:"id"`
	NotificationID uuid.UUID       `json:"notification_id"`
	ProviderMsgID  string          `json:"provider_msg_id"`
	DeliveredAt    *time.Time      `json:"delivered_at,omitempty"`
	ReadAt         *time.Time      `json:"read_at,omitempty"`
	Interactions   json.RawMessage `json:"interactions,omitempty"` // provider-specific interaction data
	CollectedAt    time.Time       `json:"collected_at"`
}

// DeliveryMetricRepository persists and retrieves delivery metrics.
type DeliveryMetricRepository interface {
	Upsert(ctx context.Context, m *DeliveryMetric) error
	GetByNotificationID(ctx context.Context, notificationID uuid.UUID) (*DeliveryMetric, error)
}
