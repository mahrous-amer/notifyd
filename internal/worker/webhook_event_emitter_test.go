package worker

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/bse/notifyd/internal/domain"
	"github.com/bse/notifyd/internal/domain/mocks"
)

// stubTaskEnqueuer is a hand-rolled test double for taskEnqueuer, recording
// every task it was asked to enqueue so tests can assert on the fan-out
// without a live Asynq/Redis connection.
type stubTaskEnqueuer struct {
	enqueued []string // task type per call, in order
	err      error
}

func (s *stubTaskEnqueuer) Enqueue(_ WebhookEventTaskPayload) error {
	if s.err != nil {
		return s.err
	}
	s.enqueued = append(s.enqueued, TypeWebhookEvent)
	return nil
}

func newEmitterFixture(t *testing.T) (*gomock.Controller, *mocks.MockWebhookEndpointRepository, *stubTaskEnqueuer, *WebhookEventEmitter) {
	t.Helper()
	ctrl := gomock.NewController(t)
	endpointRepo := mocks.NewMockWebhookEndpointRepository(ctrl)
	enqueuer := &stubTaskEnqueuer{}
	emitter := NewWebhookEventEmitter(endpointRepo, enqueuer, zerolog.Nop())
	return ctrl, endpointRepo, enqueuer, emitter
}

func activeEndpoint(tenantID uuid.UUID, events ...string) *domain.WebhookEndpoint {
	return &domain.WebhookEndpoint{
		ID:       uuid.New(),
		TenantID: tenantID,
		URL:      "https://example.com/hook",
		Secret:   "s",
		Events:   events,
		IsActive: true,
	}
}

func TestWebhookEventEmitter_Emit_FansOutToEveryMatchingEndpoint(t *testing.T) {
	_, endpointRepo, enqueuer, emitter := newEmitterFixture(t)
	tenantID := uuid.New()

	endpoints := []*domain.WebhookEndpoint{
		activeEndpoint(tenantID, "notification.delivered"),
		activeEndpoint(tenantID, "notification.delivered", "notification.failed"),
	}
	endpointRepo.EXPECT().
		ListActiveByTenantAndEvent(gomock.Any(), tenantID, domain.WebhookEventDelivered).
		Return(endpoints, nil)

	err := emitter.Emit(context.Background(), EmitParams{
		TenantID:        tenantID,
		NotificationID:  uuid.New(),
		ChannelConfigID: uuid.New(),
		Channel:         "telegram",
		EventType:       domain.WebhookEventDelivered,
		Attempts:        1,
	})

	require.NoError(t, err)
	assert.Len(t, enqueuer.enqueued, 2, "one task per matching endpoint")
}

func TestWebhookEventEmitter_Emit_NoMatchingEndpoints_EnqueuesNothing(t *testing.T) {
	_, endpointRepo, enqueuer, emitter := newEmitterFixture(t)
	tenantID := uuid.New()

	endpointRepo.EXPECT().
		ListActiveByTenantAndEvent(gomock.Any(), tenantID, domain.WebhookEventFailed).
		Return(nil, nil)

	err := emitter.Emit(context.Background(), EmitParams{
		TenantID:        tenantID,
		NotificationID:  uuid.New(),
		ChannelConfigID: uuid.New(),
		Channel:         "discord",
		EventType:       domain.WebhookEventFailed,
		Attempts:        3,
	})

	require.NoError(t, err)
	assert.Empty(t, enqueuer.enqueued)
}

func TestWebhookEventEmitter_Emit_RepositoryError_ReturnsError(t *testing.T) {
	_, endpointRepo, _, emitter := newEmitterFixture(t)
	tenantID := uuid.New()
	repoErr := errors.New("database unavailable")

	endpointRepo.EXPECT().
		ListActiveByTenantAndEvent(gomock.Any(), tenantID, domain.WebhookEventDelivered).
		Return(nil, repoErr)

	err := emitter.Emit(context.Background(), EmitParams{
		TenantID:        tenantID,
		NotificationID:  uuid.New(),
		ChannelConfigID: uuid.New(),
		Channel:         "telegram",
		EventType:       domain.WebhookEventDelivered,
		Attempts:        1,
	})

	require.Error(t, err)
}

func TestWebhookEventEmitter_Emit_OneEndpointEnqueueFailure_StillTriesTheRest(t *testing.T) {
	// A failure to enqueue for one endpoint (e.g. a transient Redis error)
	// must not prevent delivery to the tenant's other endpoints — each
	// endpoint's delivery is independent.
	ctrl := gomock.NewController(t)
	endpointRepo := mocks.NewMockWebhookEndpointRepository(ctrl)
	tenantID := uuid.New()

	endpoints := []*domain.WebhookEndpoint{
		activeEndpoint(tenantID, "notification.delivered"),
		activeEndpoint(tenantID, "notification.delivered"),
	}
	endpointRepo.EXPECT().
		ListActiveByTenantAndEvent(gomock.Any(), tenantID, domain.WebhookEventDelivered).
		Return(endpoints, nil)

	callCount := 0
	countingEnqueuer := enqueuerFunc(func(WebhookEventTaskPayload) error {
		callCount++
		if callCount == 1 {
			return errors.New("redis unavailable")
		}
		return nil
	})

	emitter := NewWebhookEventEmitter(endpointRepo, countingEnqueuer, zerolog.Nop())

	err := emitter.Emit(context.Background(), EmitParams{
		TenantID:        tenantID,
		NotificationID:  uuid.New(),
		ChannelConfigID: uuid.New(),
		Channel:         "telegram",
		EventType:       domain.WebhookEventDelivered,
		Attempts:        1,
	})

	assert.Error(t, err, "a partial failure is still reported to the caller for observability")
	assert.Equal(t, 2, callCount, "the second endpoint must still be attempted after the first fails")
}

func TestWebhookEventEmitter_Emit_EventPayloadShape(t *testing.T) {
	_, endpointRepo, _, _ := newEmitterFixture(t)
	tenantID := uuid.New()
	notifID := uuid.New()
	channelConfigID := uuid.New()
	endpoint := activeEndpoint(tenantID, "notification.delivered")

	endpointRepo.EXPECT().
		ListActiveByTenantAndEvent(gomock.Any(), tenantID, domain.WebhookEventDelivered).
		Return([]*domain.WebhookEndpoint{endpoint}, nil)

	var captured WebhookEventTaskPayload
	capturingEnqueuer := enqueuerFunc(func(p WebhookEventTaskPayload) error {
		captured = p
		return nil
	})
	emitter := NewWebhookEventEmitter(endpointRepo, capturingEnqueuer, zerolog.Nop())

	err := emitter.Emit(context.Background(), EmitParams{
		TenantID:        tenantID,
		NotificationID:  notifID,
		ChannelConfigID: channelConfigID,
		Channel:         "telegram",
		EventType:       domain.WebhookEventDelivered,
		Attempts:        4,
		Metadata:        json.RawMessage(`{"order_id":"o-1"}`),
	})

	require.NoError(t, err)
	assert.Equal(t, endpoint.ID, captured.EndpointID)
	assert.Equal(t, "notification.delivered", captured.Event.Type)
	assert.Equal(t, notifID, captured.Event.Data.NotificationID)
	assert.Equal(t, channelConfigID, captured.Event.Data.ChannelConfigID)
	assert.Equal(t, "telegram", captured.Event.Data.Channel)
	assert.Equal(t, "delivered", captured.Event.Data.Status)
	assert.Equal(t, 4, captured.Event.Data.Attempts)
	assert.JSONEq(t, `{"order_id":"o-1"}`, string(captured.Event.Data.Metadata))
	assert.NotEmpty(t, captured.Event.ID, "every event gets a unique ID")
	assert.False(t, captured.Event.CreatedAt.IsZero())
}

// enqueuerFunc adapts a plain function to the taskEnqueuer interface, the
// same "function as test double" style dispatcher_test.go uses for
// mockProvider.
type enqueuerFunc func(WebhookEventTaskPayload) error

func (f enqueuerFunc) Enqueue(p WebhookEventTaskPayload) error { return f(p) }
