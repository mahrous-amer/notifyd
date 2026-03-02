package worker

import (
	"context"
	"encoding/json"

	"github.com/hibiken/asynq"
	"github.com/rs/zerolog"

	"github.com/bse/notifyd/internal/domain"
)

type NotifyErrorHandler struct {
	notifRepo domain.NotificationRepository
	logger    zerolog.Logger
}

func NewNotifyErrorHandler(notifRepo domain.NotificationRepository, logger zerolog.Logger) *NotifyErrorHandler {
	return &NotifyErrorHandler{notifRepo: notifRepo, logger: logger}
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

	if retried >= maxRetry {
		var p NotificationDeliverPayload
		if jsonErr := json.Unmarshal(task.Payload(), &p); jsonErr == nil {
			errMsg := err.Error()
			if dbErr := h.notifRepo.UpdateStatus(ctx, p.NotificationID, domain.StatusFailed, &errMsg); dbErr != nil {
				h.logger.Error().Err(dbErr).
					Str("notification_id", p.NotificationID.String()).
					Msg("failed to mark notification as permanently failed")
			}
			h.logger.Error().
				Str("notification_id", p.NotificationID.String()).
				Msg("notification permanently failed — moved to dead letter")
		}
	}
}
