package worker

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
)

const TypeWebhookEvent = "webhook:event"

// queueWebhooks is a dedicated, lowest-priority Asynq queue for status-event
// delivery. Kept separate from "notifications"/"critical"/"low" so a slow or
// unreachable customer webhook endpoint competes with other webhook
// deliveries for worker time, never with actual notification delivery — see
// cmd/worker/main.go's Queues weight map.
const queueWebhooks = "webhooks"

// webhookEventMaxRetry is chosen so 8 attempts, spaced by
// webhookEventRetryDelay's backoff curve, span roughly six hours before the
// delivery worker gives up and drops the event (see error_handler.go's
// generic retry-exhaustion logging — no special-casing is needed there
// since it already logs and returns for any task type).
const webhookEventMaxRetry = 8

// WebhookEventData is the "data" object inside a delivered webhook-event
// payload. Field shapes mirror the design doc's example payload exactly.
type WebhookEventData struct {
	NotificationID  uuid.UUID       `json:"notification_id"`
	ChannelConfigID uuid.UUID       `json:"channel_config_id"`
	Channel         string          `json:"channel"`
	Status          string          `json:"status"`
	Attempts        int             `json:"attempts"`
	Metadata        json.RawMessage `json:"metadata,omitempty"`
}

// WebhookEventPayload is the exact JSON body POSTed to a tenant's webhook
// endpoint.
type WebhookEventPayload struct {
	ID        string           `json:"id"`
	Type      string           `json:"type"`
	CreatedAt time.Time        `json:"created_at"`
	Data      WebhookEventData `json:"data"`
}

// WebhookEventTaskPayload carries one (endpoint, event) pairing through the
// "webhooks" Asynq queue. EndpointID is looked up fresh from the database by
// the delivery worker (not embedded here) so the endpoint's URL and secret
// are never stored in Redis, matching NotificationDeliverPayload's existing
// rationale for keeping channel config secrets out of task payloads.
type WebhookEventTaskPayload struct {
	EndpointID uuid.UUID           `json:"endpoint_id"`
	Event      WebhookEventPayload `json:"event"`
}

// NewWebhookEventTask builds the Asynq task the dispatcher enqueues once per
// matching active endpoint on a terminal notification transition.
//
// The task ID combines the event ID and endpoint ID so that, for a single
// terminal transition fanning out to N endpoints, each endpoint gets an
// independent Asynq task instead of colliding on a shared ID (which would
// make asynq silently treat every endpoint after the first as a duplicate
// enqueue and drop it).
func NewWebhookEventTask(p WebhookEventTaskPayload) (*asynq.Task, error) {
	payload, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("marshal payload: %w", err)
	}

	return asynq.NewTask(
		TypeWebhookEvent,
		payload,
		asynq.Queue(queueWebhooks),
		asynq.MaxRetry(webhookEventMaxRetry),
		asynq.TaskID(fmt.Sprintf("webhookevent:%s:%s", p.Event.ID, p.EndpointID.String())),
		asynq.Timeout(30*time.Second),
	), nil
}
