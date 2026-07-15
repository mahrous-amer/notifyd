package worker

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/hibiken/asynq"
	"github.com/rs/zerolog"

	"github.com/bse/notifyd/internal/domain"
)

type NotifyErrorHandler struct {
	notifRepo domain.NotificationRepository
	emitter   terminalEventEmitter
	logger    zerolog.Logger
}

func NewNotifyErrorHandler(notifRepo domain.NotificationRepository, emitter terminalEventEmitter, logger zerolog.Logger) *NotifyErrorHandler {
	return &NotifyErrorHandler{notifRepo: notifRepo, emitter: emitter, logger: logger}
}

func (h *NotifyErrorHandler) HandleError(ctx context.Context, task *asynq.Task, err error) {
	retried, _ := asynq.GetRetryCount(ctx)
	maxRetry, _ := asynq.GetMaxRetry(ctx)

	h.logger.Error().
		Str("task_type", task.Type()).
		Int("retry", retried).
		Int("max_retry", maxRetry).
		Err(err).
		Msg("task processing error")

	if retried < maxRetry {
		return
	}

	switch task.Type() {
	case TypeNotificationDeliver:
		h.handleNotificationRetriesExhausted(ctx, task, err)
	case TypeWebhookEvent:
		h.handleWebhookEventRetriesExhausted(ctx, task, err)
	}
}

// handleWebhookEventRetriesExhausted logs the permanent drop of a webhook
// status event once its own delivery retries are exhausted, per the design
// doc's "then dropped (recorded in logs)". There is deliberately no
// notification-state or emitter interaction here: a webhook:event task's
// payload is a WebhookEventTaskPayload, not a NotificationDeliverPayload,
// and this task type never represents a notification's own delivery — it
// represents delivery of an *event about* a notification that already
// reached a terminal state.
func (h *NotifyErrorHandler) handleWebhookEventRetriesExhausted(_ context.Context, task *asynq.Task, err error) {
	var p WebhookEventTaskPayload
	if jsonErr := json.Unmarshal(task.Payload(), &p); jsonErr != nil {
		return
	}
	h.logger.Error().
		Str("event_id", p.Event.ID).
		Str("event_type", p.Event.Type).
		Str("endpoint_id", p.EndpointID.String()).
		Err(err).
		Msg("webhook event delivery permanently failed — dropped")
}

// handleNotificationRetriesExhausted marks a notification permanently
// failed once its task has run out of retries. asynq invokes HandleError
// for every failed task unconditionally — including ones whose error wraps
// asynq.SkipRetry, which dispatcher.go's handlePermanentProviderFailure
// returns after already marking the notification StatusFailed and emitting
// notification.failed itself. Re-running that here would both redundantly
// rewrite the same status and, without the SkipRetry check below,
// double-emit the webhook event for a single terminal transition — so a
// SkipRetry error skips emission (the UpdateStatus call below is harmless
// to repeat since it writes the same status, but is also naturally rare
// here since handlePermanentProviderFailure's SkipRetry fires almost always
// on an early attempt, well before retried >= maxRetry).
func (h *NotifyErrorHandler) handleNotificationRetriesExhausted(ctx context.Context, task *asynq.Task, err error) {
	var p NotificationDeliverPayload
	if jsonErr := json.Unmarshal(task.Payload(), &p); jsonErr != nil {
		return
	}

	errMsg := err.Error()
	if dbErr := h.notifRepo.UpdateStatus(ctx, p.NotificationID, domain.StatusFailed, &errMsg); dbErr != nil {
		h.logger.Error().Err(dbErr).
			Str("notification_id", p.NotificationID.String()).
			Msg("failed to mark notification as permanently failed")
	}
	h.logger.Error().
		Str("notification_id", p.NotificationID.String()).
		Msg("notification permanently failed — moved to dead letter")

	if errors.Is(err, asynq.SkipRetry) {
		// dispatcher.go's handlePermanentProviderFailure already emitted
		// notification.failed for this transition before returning this
		// error; emitting again here would fire the event twice.
		return
	}

	emitErr := h.emitter.Emit(ctx, EmitParams{
		TenantID:        p.TenantID,
		NotificationID:  p.NotificationID,
		ChannelConfigID: p.ChannelConfigID,
		Channel:         p.ChannelType,
		EventType:       domain.WebhookEventFailed,
		// retried >= maxRetry means this is the attempt that triggered
		// exhaustion; dispatcher.go's IncrementRetry already ran for it
		// (inside handleDeliveryError/handleProviderFailure) before this
		// error reached HandleError, so retried+1 — the same "how many
		// attempts happened, including this one" count dispatcher.go's
		// attemptNumber uses — is the true attempt count.
		Attempts: retriedCount(ctx) + 1,
		Metadata: p.Metadata,
	})
	if emitErr != nil {
		h.logger.Error().Err(emitErr).
			Str("notification_id", p.NotificationID.String()).
			Msg("failed to emit webhook status event")
	}
}

func retriedCount(ctx context.Context) int {
	retried, _ := asynq.GetRetryCount(ctx)
	return retried
}
