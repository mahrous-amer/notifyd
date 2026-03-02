package worker

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"

	"github.com/bse/notifyd/internal/domain"
)

const TypeNotificationDeliver = "notification:deliver"

// NotificationDeliverPayload carries the data the worker needs to attempt
// delivery of a single notification. Channel config secrets are intentionally
// excluded — the dispatcher fetches them fresh from the database to avoid
// storing credentials in Redis.
type NotificationDeliverPayload struct {
	NotificationID uuid.UUID                    `json:"notification_id"`
	TenantID       uuid.UUID                    `json:"tenant_id"`
	ChannelType    string                       `json:"channel_type"`
	ChannelConfigID uuid.UUID                   `json:"channel_config_id"`
	Subject        string                       `json:"subject"`
	Body           string                       `json:"body"`
	Metadata       json.RawMessage              `json:"metadata"`
	DeliveryPrefs  *domain.DeliveryPreferences  `json:"delivery_prefs,omitempty"`
}

// queueForPriority maps the human-readable priority string from DeliveryPreferences
// to the corresponding asynq queue name.
func queueForPriority(priority string) string {
	switch priority {
	case "critical":
		return "critical"
	case "low":
		return "low"
	default:
		return "notifications"
	}
}

func NewNotificationDeliverTask(p NotificationDeliverPayload, opts ...asynq.Option) (*asynq.Task, error) {
	payload, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("marshal payload: %w", err)
	}

	queue := "notifications"
	maxRetry := 5
	if p.DeliveryPrefs != nil {
		if p.DeliveryPrefs.Priority != "" {
			queue = queueForPriority(p.DeliveryPrefs.Priority)
		}
		if p.DeliveryPrefs.MaxRetries != nil {
			maxRetry = *p.DeliveryPrefs.MaxRetries
		}
	}

	defaultOpts := []asynq.Option{
		asynq.Queue(queue),
		asynq.MaxRetry(maxRetry),
		asynq.TaskID(fmt.Sprintf("notif:%s", p.NotificationID.String())),
		asynq.Timeout(30 * time.Second),
	}

	// Caller-supplied opts are appended last so they can override defaults when
	// needed (e.g. the service overrides MaxRetry from the channel config).
	return asynq.NewTask(
		TypeNotificationDeliver,
		payload,
		append(defaultOpts, opts...)...,
	), nil
}
