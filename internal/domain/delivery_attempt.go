package domain

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type AttemptStatus string

const (
	AttemptSuccess AttemptStatus = "success"
	AttemptFailure AttemptStatus = "failure"
)

type DeliveryAttempt struct {
	ID               uuid.UUID       `json:"id"`
	NotificationID   uuid.UUID       `json:"notification_id"`
	AttemptNumber    int             `json:"attempt_number"`
	Status           AttemptStatus   `json:"status"`
	ProviderResponse json.RawMessage `json:"provider_response,omitempty"`
	ErrorMessage     *string         `json:"error_message,omitempty"`
	DurationMs       int             `json:"duration_ms"`
	AttemptedAt      time.Time       `json:"attempted_at"`
}

type DeliveryAttemptRepository interface {
	Create(ctx context.Context, a *DeliveryAttempt) error
	ListByNotification(ctx context.Context, notificationID uuid.UUID) ([]*DeliveryAttempt, error)
}
