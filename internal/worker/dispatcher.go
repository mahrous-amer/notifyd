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

type Dispatcher struct {
	registry    *provider.Registry
	notifRepo   domain.NotificationRepository
	attemptRepo domain.DeliveryAttemptRepository
	channelRepo domain.ChannelConfigRepository
	metricRepo  domain.DeliveryMetricRepository
	logger      zerolog.Logger
}

func NewDispatcher(
	registry *provider.Registry,
	notifRepo domain.NotificationRepository,
	attemptRepo domain.DeliveryAttemptRepository,
	channelRepo domain.ChannelConfigRepository,
	metricRepo domain.DeliveryMetricRepository,
	logger zerolog.Logger,
) *Dispatcher {
	return &Dispatcher{
		registry:    registry,
		notifRepo:   notifRepo,
		attemptRepo: attemptRepo,
		channelRepo: channelRepo,
		metricRepo:  metricRepo,
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
		return d.handleProviderFailure(ctx, log, attempt, p.NotificationID, resp, durationMs)
	}

	d.recordSuccessfulDelivery(ctx, log, attempt, p.NotificationID, resp, durationMs)
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
	notifID uuid.UUID,
	resp *provider.SendResponse,
	durationMs int,
) error {
	attempt.Status = domain.AttemptFailure
	attempt.ErrorMessage = &resp.ErrorMessage
	attempt.ProviderResponse = resp.ProviderData

	if dbErr := d.attemptRepo.Create(ctx, attempt); dbErr != nil {
		log.Error().Err(dbErr).Msg("failed to record delivery attempt")
	}

	if resp.Permanent {
		return d.handlePermanentProviderFailure(ctx, log, notifID, resp, durationMs)
	}

	if dbErr := d.notifRepo.IncrementRetry(ctx, notifID, resp.ErrorMessage); dbErr != nil {
		log.Error().Err(dbErr).Msg("failed to increment retry count")
	}
	if dbErr := d.notifRepo.UpdateStatus(ctx, notifID, domain.StatusRetrying, &resp.ErrorMessage); dbErr != nil {
		log.Error().Err(dbErr).Msg("failed to update notification status to retrying")
	}

	log.Warn().Str("error", resp.ErrorMessage).Int("duration_ms", durationMs).Msg("delivery failed (provider)")
	return fmt.Errorf("provider error: %s", resp.ErrorMessage)
}

// handlePermanentProviderFailure marks a notification as permanently failed
// and signals asynq to stop retrying. Used for provider errors that retrying
// cannot fix, such as SMTP authentication failures or rejected recipients.
func (d *Dispatcher) handlePermanentProviderFailure(
	ctx context.Context,
	log zerolog.Logger,
	notifID uuid.UUID,
	resp *provider.SendResponse,
	durationMs int,
) error {
	// Still counts as one attempt even though it will never be retried, so
	// retry_count must be incremented here too — otherwise a first-attempt
	// permanent failure leaves retry_count at 0 while delivery_attempts
	// already has attempt_number 1, the same bookkeeping the retrying path
	// keeps in sync.
	if dbErr := d.notifRepo.IncrementRetry(ctx, notifID, resp.ErrorMessage); dbErr != nil {
		log.Error().Err(dbErr).Msg("failed to increment retry count")
	}
	if dbErr := d.notifRepo.UpdateStatus(ctx, notifID, domain.StatusFailed, &resp.ErrorMessage); dbErr != nil {
		log.Error().Err(dbErr).Msg("failed to update notification status to failed")
	}

	log.Warn().Str("error", resp.ErrorMessage).Int("duration_ms", durationMs).Msg("delivery failed permanently (provider)")
	return fmt.Errorf("provider error: %s: %w", resp.ErrorMessage, asynq.SkipRetry)
}

func (d *Dispatcher) recordSuccessfulDelivery(
	ctx context.Context,
	log zerolog.Logger,
	attempt *domain.DeliveryAttempt,
	notifID uuid.UUID,
	resp *provider.SendResponse,
	durationMs int,
) {
	attempt.Status = domain.AttemptSuccess
	attempt.ProviderResponse = resp.ProviderData

	if dbErr := d.attemptRepo.Create(ctx, attempt); dbErr != nil {
		log.Error().Err(dbErr).Msg("failed to record delivery attempt")
	}
	if dbErr := d.notifRepo.MarkDelivered(ctx, notifID); dbErr != nil {
		log.Error().Err(dbErr).Msg("failed to mark notification as delivered")
	}

	if resp.ProviderMsgID != "" {
		if dbErr := d.notifRepo.SetProviderMsgID(ctx, notifID, resp.ProviderMsgID); dbErr != nil {
			log.Error().Err(dbErr).Msg("failed to store provider message ID")
		}
		d.createInitialDeliveryMetric(ctx, log, notifID, resp.ProviderMsgID)
	}

	log.Info().Int("duration_ms", durationMs).Msg("notification delivered")
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
