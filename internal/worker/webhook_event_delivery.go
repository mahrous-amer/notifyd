package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/hibiken/asynq"
	"github.com/rs/zerolog"

	"github.com/bse/notifyd/internal/domain"
	"github.com/bse/notifyd/internal/provider"
)

// webhookEventDeliveryTimeout bounds a single delivery attempt end to end,
// matching the generic-webhook channel provider's request timeout (see
// webhookRequestTimeout in provider/webhook.go) since both are posting to
// the same class of tenant-controlled destination.
const webhookEventDeliveryTimeout = 30 * time.Second

// WebhookEventDeliveryWorker POSTs one webhook status event to one endpoint.
// It reuses the generic-webhook channel provider's HMAC signing scheme and
// SSRF-guarded client (see provider.SignHMAC and provider.NewGuardedHTTPClient)
// so a single verification snippet works for both status events and
// channel-delivered content.
type WebhookEventDeliveryWorker struct {
	endpointRepo domain.WebhookEndpointRepository
	client       *http.Client
	logger       zerolog.Logger
}

func NewWebhookEventDeliveryWorker(endpointRepo domain.WebhookEndpointRepository, client *http.Client, logger zerolog.Logger) *WebhookEventDeliveryWorker {
	return &WebhookEventDeliveryWorker{endpointRepo: endpointRepo, client: client, logger: logger}
}

// NewDefaultWebhookEventDeliveryWorker builds a WebhookEventDeliveryWorker
// around the production SSRF-guarded client. cmd/worker/main.go must always
// use this constructor; NewWebhookEventDeliveryWorker's caller-supplied
// client exists only so tests can point at an httptest.Server, which always
// listens on a loopback address the guarded client correctly refuses.
func NewDefaultWebhookEventDeliveryWorker(endpointRepo domain.WebhookEndpointRepository, logger zerolog.Logger) *WebhookEventDeliveryWorker {
	return NewWebhookEventDeliveryWorker(endpointRepo, provider.NewGuardedHTTPClient(webhookEventDeliveryTimeout), logger)
}

// HandleWebhookEvent delivers one (endpoint, event) task. A 2xx response is
// success; any other response — or a network/SSRF-guard failure — is
// retried by asynq up to the task's MaxRetry (see webhookEventMaxRetry in
// webhook_event_task.go). A missing or deactivated endpoint is the only
// case that skips retry outright, since no amount of retrying changes
// either condition.
func (w *WebhookEventDeliveryWorker) HandleWebhookEvent(ctx context.Context, t *asynq.Task) error {
	var p WebhookEventTaskPayload
	if err := json.Unmarshal(t.Payload(), &p); err != nil {
		return fmt.Errorf("unmarshal payload: %w: %w", err, asynq.SkipRetry)
	}

	log := w.logger.With().
		Str("event_id", p.Event.ID).
		Str("event_type", p.Event.Type).
		Str("endpoint_id", p.EndpointID.String()).
		Logger()

	endpoint, err := w.endpointRepo.GetByID(ctx, p.EndpointID)
	if err != nil {
		log.Warn().Err(err).Msg("webhook endpoint not found; dropping event")
		return fmt.Errorf("fetch webhook endpoint: %w: %w", err, asynq.SkipRetry)
	}
	if !endpoint.IsActive {
		log.Info().Msg("webhook endpoint deactivated; dropping event")
		return fmt.Errorf("webhook endpoint is inactive: %w", asynq.SkipRetry)
	}

	if err := w.deliver(ctx, endpoint, p.Event); err != nil {
		log.Warn().Err(err).Msg("webhook event delivery failed")
		return err
	}

	log.Info().Msg("webhook event delivered")
	return nil
}

func (w *WebhookEventDeliveryWorker) deliver(ctx context.Context, endpoint *domain.WebhookEndpoint, event WebhookEventPayload) error {
	body, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal event payload: %w: %w", err, asynq.SkipRetry)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.URL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w: %w", err, asynq.SkipRetry)
	}

	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	signature := provider.SignHMAC(endpoint.Secret, timestamp, body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Notifyd-Signature", "sha256="+signature)
	req.Header.Set("X-Notifyd-Timestamp", timestamp)
	req.Header.Set("X-Notifyd-Event-Id", event.ID)

	resp, err := w.client.Do(req)
	if err != nil {
		return fmt.Errorf("deliver webhook event: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck
	// Drain and discard: the receiver's response body carries no information
	// this worker acts on, but the connection must still be read to
	// completion for it to be reused by the transport's connection pool.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	return fmt.Errorf("webhook endpoint returned %d", resp.StatusCode)
}
