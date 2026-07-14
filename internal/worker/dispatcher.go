package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/rs/zerolog"

	"github.com/bse/notifyd/internal/domain"
	"github.com/bse/notifyd/internal/provider"
)

// terminalEventEmitter is the slice of *WebhookEventEmitter's behavior the
// dispatcher depends on, defined as an interface so tests can substitute a
// recording fake instead of a real endpoint repository and Asynq client.
type terminalEventEmitter interface {
	Emit(ctx context.Context, params EmitParams) error
}

type Dispatcher struct {
	registry    *provider.Registry
	notifRepo   domain.NotificationRepository
	attemptRepo domain.DeliveryAttemptRepository
	channelRepo domain.ChannelConfigRepository
	metricRepo  domain.DeliveryMetricRepository
	emitter     terminalEventEmitter
	logger      zerolog.Logger
}

func NewDispatcher(
	registry *provider.Registry,
	notifRepo domain.NotificationRepository,
	attemptRepo domain.DeliveryAttemptRepository,
	channelRepo domain.ChannelConfigRepository,
	metricRepo domain.DeliveryMetricRepository,
	emitter terminalEventEmitter,
	logger zerolog.Logger,
) *Dispatcher {
	return &Dispatcher{
		registry:    registry,
		notifRepo:   notifRepo,
		attemptRepo: attemptRepo,
		channelRepo: channelRepo,
		metricRepo:  metricRepo,
		emitter:     emitter,
		logger:      logger,
	}
}

func (d *Dispatcher) HandleNotificationDeliver(ctx context.Context, t *asynq.Task) error {
	var p NotificationDeliverPayload
	if err := json.Unmarshal(t.Payload(), &p); err != nil {
		return fmt.Errorf("unmarshal payload: %w: %w", err, asynq.SkipRetry)
	}

	retryCount, _ := asynq.GetRetryCount(ctx)
	attemptNumber := retryCount + 1

	log := d.logger.With().
		Str("notification_id", p.NotificationID.String()).
		Str("channel", p.ChannelType).
		Int("attempt", attemptNumber).
		Logger()

	if err := d.notifRepo.UpdateStatus(ctx, p.NotificationID, domain.StatusProcessing, nil); err != nil {
		log.Error().Err(err).Msg("failed to update notification status to processing")
	}

	// Fetch channel config from DB (not from payload) to avoid storing secrets in Redis.
	channelCfg, err := d.channelRepo.GetByID(ctx, p.ChannelConfigID)
	if err != nil {
		log.Error().Err(err).Msg("failed to fetch channel config")
		return fmt.Errorf("fetch channel config: %w: %w", err, asynq.SkipRetry)
	}

	prov, err := d.registry.Get(p.ChannelType)
	if err != nil {
		log.Error().Err(err).Msg("no provider for channel type")
		return fmt.Errorf("no provider: %w: %w", err, asynq.SkipRetry)
	}

	sendReq := d.buildSendRequest(p, channelCfg)

	start := time.Now()
	resp, err := prov.Send(ctx, channelCfg.Config, sendReq)
	durationMs := int(time.Since(start).Milliseconds())

	attempt := &domain.DeliveryAttempt{
		ID:             uuid.New(),
		NotificationID: p.NotificationID,
		AttemptNumber:  attemptNumber,
		DurationMs:     durationMs,
		AttemptedAt:    time.Now(),
	}

	if err != nil {
		return d.handleDeliveryError(ctx, log, attempt, p.NotificationID, err, durationMs)
	}

	if !resp.Success {
		return d.handleProviderFailure(ctx, log, attempt, p, resp, attemptNumber, durationMs)
	}

	d.recordSuccessfulDelivery(ctx, log, attempt, p, resp, attemptNumber, durationMs)
	return nil
}

// buildSendRequest constructs the provider send request, applying the format
// mode from delivery preferences when specified.
func (d *Dispatcher) buildSendRequest(p NotificationDeliverPayload, channelCfg *domain.ChannelConfig) provider.SendRequest {
	req := provider.SendRequest{
		NotificationID: p.NotificationID,
		Subject:        p.Subject,
		Body:           p.Body,
		Metadata:       p.Metadata,
	}

	// Merge delivery preferences from the channel config (the authoritative
	// source) over those from the payload. The channel config is fetched fresh
	// from the database, so it always reflects the current tenant settings.
	prefs := channelCfg.DeliveryPrefs
	if prefs == nil {
		prefs = p.DeliveryPrefs
	}

	if prefs != nil && prefs.FormatMode != "" {
		req.FormatMode = prefs.FormatMode
	}

	return req
}

func (d *Dispatcher) handleDeliveryError(
	ctx context.Context,
	log zerolog.Logger,
	attempt *domain.DeliveryAttempt,
	notifID uuid.UUID,
	err error,
	durationMs int,
) error {
	errMsg := err.Error()
	attempt.Status = domain.AttemptFailure
	attempt.ErrorMessage = &errMsg

	if dbErr := d.attemptRepo.Create(ctx, attempt); dbErr != nil {
		log.Error().Err(dbErr).Msg("failed to record delivery attempt")
	}
	if dbErr := d.notifRepo.IncrementRetry(ctx, notifID, errMsg); dbErr != nil {
		log.Error().Err(dbErr).Msg("failed to increment retry count")
	}
	if dbErr := d.notifRepo.UpdateStatus(ctx, notifID, domain.StatusRetrying, &errMsg); dbErr != nil {
		log.Error().Err(dbErr).Msg("failed to update notification status to retrying")
	}

	log.Warn().Err(err).Int("duration_ms", durationMs).Msg("delivery failed (transport)")
	return err
}

func (d *Dispatcher) handleProviderFailure(
	ctx context.Context,
	log zerolog.Logger,
	attempt *domain.DeliveryAttempt,
	p NotificationDeliverPayload,
	resp *provider.SendResponse,
	attemptNumber int,
	durationMs int,
) error {
	attempt.Status = domain.AttemptFailure
	attempt.ErrorMessage = &resp.ErrorMessage
	attempt.ProviderResponse = resp.ProviderData

	if dbErr := d.attemptRepo.Create(ctx, attempt); dbErr != nil {
		log.Error().Err(dbErr).Msg("failed to record delivery attempt")
	}

	if resp.Permanent {
		return d.handlePermanentProviderFailure(ctx, log, p, resp, attemptNumber, durationMs)
	}

	if dbErr := d.notifRepo.IncrementRetry(ctx, p.NotificationID, resp.ErrorMessage); dbErr != nil {
		log.Error().Err(dbErr).Msg("failed to increment retry count")
	}
	if dbErr := d.notifRepo.UpdateStatus(ctx, p.NotificationID, domain.StatusRetrying, &resp.ErrorMessage); dbErr != nil {
		log.Error().Err(dbErr).Msg("failed to update notification status to retrying")
	}

	log.Warn().Str("error", resp.ErrorMessage).Int("duration_ms", durationMs).Msg("delivery failed (provider)")
	return fmt.Errorf("provider error: %s", resp.ErrorMessage)
}

// handlePermanentProviderFailure marks a notification as permanently failed,
// emits the notification.failed webhook event, and signals asynq to stop
// retrying. Used for provider errors that retrying cannot fix, such as SMTP
// authentication failures or rejected recipients.
//
// This is the ONLY site that emits notification.failed for a SkipRetry-classified
// failure — asynq's ErrorHandler (error_handler.go) is invoked for every
// failed task including ones wrapping asynq.SkipRetry, so error_handler.go
// must not also emit here or the same terminal transition would fire twice.
// See error_handler.go's HandleError for the corresponding guard.
func (d *Dispatcher) handlePermanentProviderFailure(
	ctx context.Context,
	log zerolog.Logger,
	p NotificationDeliverPayload,
	resp *provider.SendResponse,
	attemptNumber int,
	durationMs int,
) error {
	// Still counts as one attempt even though it will never be retried, so
	// retry_count must be incremented here too — otherwise a first-attempt
	// permanent failure leaves retry_count at 0 while delivery_attempts
	// already has attempt_number 1, the same bookkeeping the retrying path
	// keeps in sync.
	if dbErr := d.notifRepo.IncrementRetry(ctx, p.NotificationID, resp.ErrorMessage); dbErr != nil {
		log.Error().Err(dbErr).Msg("failed to increment retry count")
	}
	if dbErr := d.notifRepo.UpdateStatus(ctx, p.NotificationID, domain.StatusFailed, &resp.ErrorMessage); dbErr != nil {
		log.Error().Err(dbErr).Msg("failed to update notification status to failed")
	}

	// attemptNumber (asynq's own retry-count-derived counter, incremented
	// once per Send() call including this one) equals notifyd's retry_count
	// after the IncrementRetry call above: both count "how many attempts
	// have happened so far, including this one" the same way. See EmitParams
	// for the general rule this specializes.
	d.emitTerminalEvent(ctx, log, p, domain.WebhookEventFailed, attemptNumber)

	log.Warn().Str("error", resp.ErrorMessage).Int("duration_ms", durationMs).Msg("delivery failed permanently (provider)")
	return fmt.Errorf("provider error: %s: %w", resp.ErrorMessage, asynq.SkipRetry)
}

func (d *Dispatcher) recordSuccessfulDelivery(
	ctx context.Context,
	log zerolog.Logger,
	attempt *domain.DeliveryAttempt,
	p NotificationDeliverPayload,
	resp *provider.SendResponse,
	attemptNumber int,
	durationMs int,
) {
	attempt.Status = domain.AttemptSuccess
	attempt.ProviderResponse = resp.ProviderData

	if dbErr := d.attemptRepo.Create(ctx, attempt); dbErr != nil {
		log.Error().Err(dbErr).Msg("failed to record delivery attempt")
	}
	if dbErr := d.notifRepo.MarkDelivered(ctx, p.NotificationID); dbErr != nil {
		log.Error().Err(dbErr).Msg("failed to mark notification as delivered")
	}

	if resp.ProviderMsgID != "" {
		if dbErr := d.notifRepo.SetProviderMsgID(ctx, p.NotificationID, resp.ProviderMsgID); dbErr != nil {
			log.Error().Err(dbErr).Msg("failed to store provider message ID")
		}
		d.createInitialDeliveryMetric(ctx, log, p.NotificationID, resp.ProviderMsgID)
	}

	// MarkDelivered never increments retry_count (unlike the failure paths),
	// so attemptNumber — already "how many attempts happened, including this
	// one" — is the true attempt count directly, with no adjustment needed.
	d.emitTerminalEvent(ctx, log, p, domain.WebhookEventDelivered, attemptNumber)

	log.Info().Int("duration_ms", durationMs).Msg("notification delivered")
}

// emitTerminalEvent fires the webhook status event for a terminal
// transition. Errors are logged and swallowed, matching this file's
// existing log-and-continue style for every other post-outcome side effect
// (attempt recording, status updates, metric creation) — a webhook delivery
// problem must never fail the notification delivery itself, which already
// succeeded or permanently failed by the time this runs.
func (d *Dispatcher) emitTerminalEvent(
	ctx context.Context,
	log zerolog.Logger,
	p NotificationDeliverPayload,
	eventType domain.WebhookEventType,
	attempts int,
) {
	err := d.emitter.Emit(ctx, EmitParams{
		TenantID:        p.TenantID,
		NotificationID:  p.NotificationID,
		ChannelConfigID: p.ChannelConfigID,
		Channel:         p.ChannelType,
		EventType:       eventType,
		Attempts:        attempts,
		Metadata:        p.Metadata,
	})
	if err != nil {
		log.Error().Err(err).Str("event_type", string(eventType)).Msg("failed to emit webhook status event")
	}
}

// createInitialDeliveryMetric creates the first delivery metric record
// immediately after a successful send. Providers that support richer
// status polling can update this record later via a separate job.
func (d *Dispatcher) createInitialDeliveryMetric(
	ctx context.Context,
	log zerolog.Logger,
	notifID uuid.UUID,
	providerMsgID string,
) {
	now := time.Now()
	metric := &domain.DeliveryMetric{
		ID:             uuid.New(),
		NotificationID: notifID,
		ProviderMsgID:  providerMsgID,
		CollectedAt:    now,
	}
	if err := d.metricRepo.Upsert(ctx, metric); err != nil {
		log.Error().Err(err).Msg("failed to create initial delivery metric")
	}
}
