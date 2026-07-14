package worker

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/bse/notifyd/internal/domain"
)

// taskEnqueuer is the slice of *asynq.Client's behavior WebhookEventEmitter
// needs, defined as an interface so tests can substitute a recording stub
// instead of a live Asynq/Redis connection — the same pattern dispatcher_test.go
// uses for provider.Provider via mockProvider.
type taskEnqueuer interface {
	Enqueue(p WebhookEventTaskPayload) error
}

// EmitParams describes one terminal notification transition: everything
// WebhookEventEmitter needs to look up matching endpoints and build the
// event payload for each. Grouped into a struct (rather than passed as
// individual parameters) because the field count already exceeds what reads
// cleanly as positional arguments, and because dispatcher.go and
// error_handler.go both construct one from state they already have on hand.
type EmitParams struct {
	TenantID        uuid.UUID
	NotificationID  uuid.UUID
	ChannelConfigID uuid.UUID
	Channel         string
	EventType       domain.WebhookEventType
	// Attempts is the notification's true attempt count. The dispatcher's
	// IncrementRetry is only called on a failed attempt (see dispatcher.go),
	// so retry_count already reflects the failing attempt itself by the time
	// a notification reaches StatusFailed, while a successful delivery never
	// calls IncrementRetry at all — callers must pass retry_count for a
	// failed event and retry_count+1 for a delivered event. See
	// dispatcher.go's emitTerminalEvent and error_handler.go's HandleError
	// for where this is computed.
	Attempts int
	Metadata json.RawMessage
}

// WebhookEventEmitter turns one terminal notification transition into one
// webhook:event Asynq task per matching active endpoint. It is the single
// place that constructs the event payload (including the event ID), so
// calling Emit exactly once per terminal transition is what makes emission
// idempotent — there is no separate database dedup layer to fall back on.
type WebhookEventEmitter struct {
	endpointRepo domain.WebhookEndpointRepository
	enqueuer     taskEnqueuer
	logger       zerolog.Logger
}

func NewWebhookEventEmitter(endpointRepo domain.WebhookEndpointRepository, enqueuer taskEnqueuer, logger zerolog.Logger) *WebhookEventEmitter {
	return &WebhookEventEmitter{endpointRepo: endpointRepo, enqueuer: enqueuer, logger: logger}
}

// Emit looks up every active endpoint the tenant has subscribed to
// params.EventType and enqueues an independent delivery task for each. A
// failure to enqueue for one endpoint is logged and does not stop the
// others from being attempted; the first such failure (if any) is returned
// to the caller so it's visible in the dispatcher/error_handler's own logs,
// but every endpoint is still tried.
func (e *WebhookEventEmitter) Emit(ctx context.Context, params EmitParams) error {
	endpoints, err := e.endpointRepo.ListActiveByTenantAndEvent(ctx, params.TenantID, params.EventType)
	if err != nil {
		return err
	}

	var firstErr error
	for _, endpoint := range endpoints {
		task := buildWebhookEventTaskPayload(endpoint.ID, params)
		if err := e.enqueuer.Enqueue(task); err != nil {
			e.logger.Error().
				Err(err).
				Str("endpoint_id", endpoint.ID.String()).
				Str("notification_id", params.NotificationID.String()).
				Msg("failed to enqueue webhook event delivery")
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

func buildWebhookEventTaskPayload(endpointID uuid.UUID, params EmitParams) WebhookEventTaskPayload {
	return WebhookEventTaskPayload{
		EndpointID: endpointID,
		Event: WebhookEventPayload{
			ID:        webhookEventID(params.NotificationID, params.EventType),
			Type:      string(params.EventType),
			CreatedAt: time.Now().UTC(),
			Data: WebhookEventData{
				NotificationID:  params.NotificationID,
				ChannelConfigID: params.ChannelConfigID,
				Channel:         params.Channel,
				Status:          eventStatusForType(params.EventType),
				Attempts:        params.Attempts,
				Metadata:        defaultMetadata(params.Metadata),
			},
		},
	}
}

// defaultMetadata returns an empty JSON object when metadata is nil, so the
// delivered payload's "metadata" field is always present and always valid
// JSON — matching provider/webhook.go's buildWebhookPayload, which applies
// the identical default for the generic-webhook channel's content payload.
func defaultMetadata(metadata json.RawMessage) json.RawMessage {
	if metadata == nil {
		return json.RawMessage("{}")
	}
	return metadata
}

// webhookEventNamespace is a fixed, arbitrary UUID used as the namespace
// argument to uuid.NewSHA1 below. Its only requirement is stability across
// process restarts and deploys — any fixed value works equally well, since
// what matters is that the same (notification_id, event_type) pair always
// hashes to the same output, not what the namespace itself is.
var webhookEventNamespace = uuid.MustParse("2b5b1e0a-6a8a-4c1e-9c2a-2b6b6a8a4c1e")

// webhookEventID derives a stable event ID from the notification and event
// type it describes, rather than generating a fresh random ID per call.
//
// A worker crash between marking a notification's terminal status and
// calling Emit causes asynq to redeliver the notification:deliver task,
// which re-runs the dispatcher and re-emits for the same logical
// transition. With a random ID, that redelivery would produce a second
// event carrying a different X-Notifyd-Event-Id, and a receiver
// deduplicating on that header (as the README instructs) could never
// collapse the two into one logical delivery. Deriving the ID from
// (notification_id, event_type) with UUIDv5 makes every re-emission of the
// same transition produce the byte-identical ID, so the promised dedup key
// actually works — delivery is at-least-once, and the event ID is what
// makes at-least-once safe to treat as effectively-once on the receiver
// side.
func webhookEventID(notificationID uuid.UUID, eventType domain.WebhookEventType) string {
	data := notificationID.String() + ":" + string(eventType)
	return "evt_" + uuid.NewSHA1(webhookEventNamespace, []byte(data)).String()
}

// eventStatusForType maps the event type to the "status" field in the event
// data. Currently a 1:1 mapping (the event type IS "notification.<status>"),
// kept as its own function so a future event type that doesn't follow that
// naming pattern doesn't require touching buildWebhookEventTaskPayload.
func eventStatusForType(eventType domain.WebhookEventType) string {
	switch eventType {
	case domain.WebhookEventDelivered:
		return "delivered"
	case domain.WebhookEventFailed:
		return "failed"
	default:
		return string(eventType)
	}
}
