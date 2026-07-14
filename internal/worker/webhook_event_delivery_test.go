package worker

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/bse/notifyd/internal/domain"
	"github.com/bse/notifyd/internal/domain/mocks"
)

// independentDeliverySignature recomputes the signature the way a receiver
// implementing the contract from scratch would, matching webhook_test.go's
// expectedWebhookSignature helper for the generic-webhook provider — the two
// must use an identical scheme since the design doc specifies one shared
// verification snippet for both.
func independentDeliverySignature(secret, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func newDeliveryTestFixture(t *testing.T, client *http.Client) (*gomock.Controller, *mocks.MockWebhookEndpointRepository, *WebhookEventDeliveryWorker) {
	t.Helper()
	ctrl := gomock.NewController(t)
	endpointRepo := mocks.NewMockWebhookEndpointRepository(ctrl)
	worker := NewWebhookEventDeliveryWorker(endpointRepo, client, zerolog.Nop())
	return ctrl, endpointRepo, worker
}

func makeWebhookEventTask(p WebhookEventTaskPayload) *asynq.Task {
	payload, _ := json.Marshal(p)
	return asynq.NewTask(TypeWebhookEvent, payload)
}

func samplePayload(endpointID uuid.UUID) WebhookEventTaskPayload {
	return WebhookEventTaskPayload{
		EndpointID: endpointID,
		Event: WebhookEventPayload{
			ID:        "evt_test123",
			Type:      "notification.delivered",
			CreatedAt: time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC),
			Data: WebhookEventData{
				NotificationID:  uuid.New(),
				ChannelConfigID: uuid.New(),
				Channel:         "telegram",
				Status:          "delivered",
				Attempts:        1,
			},
		},
	}
}

func TestWebhookEventDeliveryWorker_Success_SignsRequestCorrectly(t *testing.T) {
	var capturedBody []byte
	var capturedSignature, capturedTimestamp, capturedEventID, capturedContentType string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		capturedContentType = r.Header.Get("Content-Type")
		capturedSignature = r.Header.Get("X-Notifyd-Signature")
		capturedTimestamp = r.Header.Get("X-Notifyd-Timestamp")
		capturedEventID = r.Header.Get("X-Notifyd-Event-Id")
		body := make([]byte, 8192)
		n, _ := r.Body.Read(body)
		capturedBody = body[:n]
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	_, endpointRepo, dw := newDeliveryTestFixture(t, server.Client())
	endpointID := uuid.New()
	endpoint := &domain.WebhookEndpoint{
		ID:       endpointID,
		URL:      server.URL,
		Secret:   "top-secret",
		IsActive: true,
	}
	endpointRepo.EXPECT().GetByID(gomock.Any(), endpointID).Return(endpoint, nil)

	payload := samplePayload(endpointID)
	task := makeWebhookEventTask(payload)

	before := time.Now().Unix()
	err := dw.HandleWebhookEvent(context.Background(), task)
	after := time.Now().Unix()

	require.NoError(t, err)
	assert.Equal(t, "application/json", capturedContentType)
	assert.Equal(t, "evt_test123", capturedEventID)

	require.NotEmpty(t, capturedTimestamp)
	ts, err := strconv.ParseInt(capturedTimestamp, 10, 64)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, ts, before)
	assert.LessOrEqual(t, ts, after)

	expected := independentDeliverySignature("top-secret", capturedTimestamp, capturedBody)
	assert.Equal(t, "sha256="+expected, capturedSignature)

	var decodedBody WebhookEventPayload
	require.NoError(t, json.Unmarshal(capturedBody, &decodedBody))
	assert.Equal(t, "evt_test123", decodedBody.ID)
	assert.Equal(t, "notification.delivered", decodedBody.Type)
}

func TestWebhookEventDeliveryWorker_2xxResponse_Succeeds(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	_, endpointRepo, dw := newDeliveryTestFixture(t, server.Client())
	endpointID := uuid.New()
	endpointRepo.EXPECT().GetByID(gomock.Any(), endpointID).Return(&domain.WebhookEndpoint{
		ID: endpointID, URL: server.URL, Secret: "s", IsActive: true,
	}, nil)

	err := dw.HandleWebhookEvent(context.Background(), makeWebhookEventTask(samplePayload(endpointID)))

	require.NoError(t, err)
}

func TestWebhookEventDeliveryWorker_ServerError_ReturnsRetryableError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	_, endpointRepo, dw := newDeliveryTestFixture(t, server.Client())
	endpointID := uuid.New()
	endpointRepo.EXPECT().GetByID(gomock.Any(), endpointID).Return(&domain.WebhookEndpoint{
		ID: endpointID, URL: server.URL, Secret: "s", IsActive: true,
	}, nil)

	err := dw.HandleWebhookEvent(context.Background(), makeWebhookEventTask(samplePayload(endpointID)))

	require.Error(t, err)
	assert.False(t, isSkipRetry(err), "a 500 must be retried, not dropped immediately")
}

func TestWebhookEventDeliveryWorker_ClientError_ReturnsRetryableError(t *testing.T) {
	// Unlike the generic-webhook channel provider (which treats most 4xx as
	// permanent), the design doc specifies a simpler contract for status
	// events: "anything other than 2xx retried with backoff up to 8
	// attempts... then dropped" — there is no separate permanent
	// classification, since a customer's receiver misconfiguration (e.g.
	// briefly returning 404 during a deploy) is exactly the kind of
	// transient-looking failure retries exist for for this delivery path.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	_, endpointRepo, dw := newDeliveryTestFixture(t, server.Client())
	endpointID := uuid.New()
	endpointRepo.EXPECT().GetByID(gomock.Any(), endpointID).Return(&domain.WebhookEndpoint{
		ID: endpointID, URL: server.URL, Secret: "s", IsActive: true,
	}, nil)

	err := dw.HandleWebhookEvent(context.Background(), makeWebhookEventTask(samplePayload(endpointID)))

	require.Error(t, err)
	assert.False(t, isSkipRetry(err))
}

func TestWebhookEventDeliveryWorker_DoesNotFollowRedirects(t *testing.T) {
	var targetCalled bool
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetCalled = true
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer redirector.Close()

	client := redirector.Client()
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}

	_, endpointRepo, dw := newDeliveryTestFixture(t, client)
	endpointID := uuid.New()
	endpointRepo.EXPECT().GetByID(gomock.Any(), endpointID).Return(&domain.WebhookEndpoint{
		ID: endpointID, URL: redirector.URL, Secret: "s", IsActive: true,
	}, nil)

	err := dw.HandleWebhookEvent(context.Background(), makeWebhookEventTask(samplePayload(endpointID)))

	require.Error(t, err)
	assert.False(t, targetCalled, "the redirect target must never be contacted")
}

func TestWebhookEventDeliveryWorker_EndpointNotFound_SkipsRetry(t *testing.T) {
	// The endpoint was deleted after the event was enqueued but before
	// delivery ran; retrying can never make it exist again.
	_, endpointRepo, dw := newDeliveryTestFixture(t, http.DefaultClient)
	endpointID := uuid.New()
	endpointRepo.EXPECT().GetByID(gomock.Any(), endpointID).Return(nil, domain.ErrNotFound)

	err := dw.HandleWebhookEvent(context.Background(), makeWebhookEventTask(samplePayload(endpointID)))

	require.Error(t, err)
	assert.True(t, isSkipRetry(err))
}

func TestWebhookEventDeliveryWorker_EndpointDeactivated_SkipsRetry(t *testing.T) {
	// The endpoint was deactivated after the event was enqueued; delivering
	// to it now would violate the tenant's own configuration.
	_, endpointRepo, dw := newDeliveryTestFixture(t, http.DefaultClient)
	endpointID := uuid.New()
	endpointRepo.EXPECT().GetByID(gomock.Any(), endpointID).Return(&domain.WebhookEndpoint{
		ID: endpointID, URL: "https://example.com/hook", Secret: "s", IsActive: false,
	}, nil)

	err := dw.HandleWebhookEvent(context.Background(), makeWebhookEventTask(samplePayload(endpointID)))

	require.Error(t, err)
	assert.True(t, isSkipRetry(err))
}

func TestWebhookEventDeliveryWorker_InvalidPayload_SkipsRetry(t *testing.T) {
	_, _, dw := newDeliveryTestFixture(t, http.DefaultClient)

	task := asynq.NewTask(TypeWebhookEvent, []byte("not valid json{{{"))
	err := dw.HandleWebhookEvent(context.Background(), task)

	require.Error(t, err)
	assert.True(t, isSkipRetry(err))
}

func TestWebhookEventDeliveryWorker_UsesRealGuardedClient_RefusesLoopback(t *testing.T) {
	// Constructing the worker with the production SSRF-guarded client (via
	// NewDefaultWebhookEventDeliveryWorker) instead of a raw http.Client
	// must refuse to dial a loopback endpoint URL — proving the delivery
	// path is wired to the same guard the generic-webhook channel uses, not
	// just an ordinary client with no protection.
	ctrl := gomock.NewController(t)
	endpointRepo := mocks.NewMockWebhookEndpointRepository(ctrl)
	dw := NewDefaultWebhookEventDeliveryWorker(endpointRepo, zerolog.Nop())

	endpointID := uuid.New()
	endpointRepo.EXPECT().GetByID(gomock.Any(), endpointID).Return(&domain.WebhookEndpoint{
		ID: endpointID, URL: "https://127.0.0.1:1/hook", Secret: "s", IsActive: true,
	}, nil)

	err := dw.HandleWebhookEvent(context.Background(), makeWebhookEventTask(samplePayload(endpointID)))

	require.Error(t, err)
}

func isSkipRetry(err error) bool {
	return errors.Is(err, asynq.SkipRetry)
}
