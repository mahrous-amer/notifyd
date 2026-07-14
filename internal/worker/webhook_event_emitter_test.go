package worker

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
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

// TestWebhookEventEmitter_Emit_SameTransition_ProducesTheSameEventID verifies
// the fix for crash-redelivery duplicates: a worker crash between
// MarkDelivered/UpdateStatus and the emit call causes asynq to redeliver the
// notification:deliver task, which re-runs the dispatcher and calls Emit a
// second time for the SAME logical (notification, event type) transition. If
// the event ID were random per call (the old uuid.New() behavior), the two
// emissions would carry different X-Notifyd-Event-Id values and a receiver
// could never collapse them into one logical delivery. Deriving the ID from
// (notification_id, event_type) makes both emissions produce byte-identical
// IDs, so the header the README promises as a dedup key actually works.
func TestWebhookEventEmitter_Emit_SameTransition_ProducesTheSameEventID(t *testing.T) {
	_, endpointRepo, _, _ := newEmitterFixture(t)
	tenantID := uuid.New()
	notifID := uuid.New()
	endpoint := activeEndpoint(tenantID, "notification.delivered")

	endpointRepo.EXPECT().
		ListActiveByTenantAndEvent(gomock.Any(), tenantID, domain.WebhookEventDelivered).
		Return([]*domain.WebhookEndpoint{endpoint}, nil).
		Times(2)

	var firstEventID, secondEventID string
	captureEnqueuer := enqueuerFunc(func(p WebhookEventTaskPayload) error {
		if firstEventID == "" {
			firstEventID = p.Event.ID
		} else {
			secondEventID = p.Event.ID
		}
		return nil
	})
	emitter := NewWebhookEventEmitter(endpointRepo, captureEnqueuer, zerolog.Nop())

	params := EmitParams{
		TenantID:        tenantID,
		NotificationID:  notifID,
		ChannelConfigID: uuid.New(),
		Channel:         "telegram",
		EventType:       domain.WebhookEventDelivered,
		Attempts:        1,
	}

	require.NoError(t, emitter.Emit(context.Background(), params))
	require.NoError(t, emitter.Emit(context.Background(), params))

	assert.NotEmpty(t, firstEventID)
	assert.Equal(t, firstEventID, secondEventID, "re-emitting the same transition must produce the identical event ID")
}

// TestWebhookEventEmitter_Emit_DifferentEventType_ProducesADifferentEventID
// verifies the ID is not simply a function of the notification alone: a
// notification.delivered and a notification.failed event for the same
// notification (which cannot both be real, but the ID derivation must not
// silently collide two conceptually different events) get different IDs.
func TestWebhookEventEmitter_Emit_DifferentEventType_ProducesADifferentEventID(t *testing.T) {
	_, endpointRepo, _, _ := newEmitterFixture(t)
	tenantID := uuid.New()
	notifID := uuid.New()
	endpoint := activeEndpoint(tenantID, "notification.delivered", "notification.failed")

	endpointRepo.EXPECT().
		ListActiveByTenantAndEvent(gomock.Any(), tenantID, gomock.Any()).
		Return([]*domain.WebhookEndpoint{endpoint}, nil).
		Times(2)

	var deliveredEventID, failedEventID string
	captureEnqueuer := enqueuerFunc(func(p WebhookEventTaskPayload) error {
		if p.Event.Type == "notification.delivered" {
			deliveredEventID = p.Event.ID
		} else {
			failedEventID = p.Event.ID
		}
		return nil
	})
	emitter := NewWebhookEventEmitter(endpointRepo, captureEnqueuer, zerolog.Nop())

	base := EmitParams{
		TenantID:        tenantID,
		NotificationID:  notifID,
		ChannelConfigID: uuid.New(),
		Channel:         "telegram",
		Attempts:        1,
	}

	delivered := base
	delivered.EventType = domain.WebhookEventDelivered
	failed := base
	failed.EventType = domain.WebhookEventFailed

	require.NoError(t, emitter.Emit(context.Background(), delivered))
	require.NoError(t, emitter.Emit(context.Background(), failed))

	assert.NotEmpty(t, deliveredEventID)
	assert.NotEmpty(t, failedEventID)
	assert.NotEqual(t, deliveredEventID, failedEventID)
}

// TestWebhookEventEmitter_Emit_DifferentNotification_ProducesADifferentEventID
// guards against a derivation that ignores the notification ID entirely.
func TestWebhookEventEmitter_Emit_DifferentNotification_ProducesADifferentEventID(t *testing.T) {
	_, endpointRepo, _, _ := newEmitterFixture(t)
	tenantID := uuid.New()
	endpoint := activeEndpoint(tenantID, "notification.delivered")

	endpointRepo.EXPECT().
		ListActiveByTenantAndEvent(gomock.Any(), tenantID, domain.WebhookEventDelivered).
		Return([]*domain.WebhookEndpoint{endpoint}, nil).
		Times(2)

	var ids []string
	captureEnqueuer := enqueuerFunc(func(p WebhookEventTaskPayload) error {
		ids = append(ids, p.Event.ID)
		return nil
	})
	emitter := NewWebhookEventEmitter(endpointRepo, captureEnqueuer, zerolog.Nop())

	base := EmitParams{
		TenantID:        tenantID,
		ChannelConfigID: uuid.New(),
		Channel:         "telegram",
		EventType:       domain.WebhookEventDelivered,
		Attempts:        1,
	}

	first := base
	first.NotificationID = uuid.New()
	second := base
	second.NotificationID = uuid.New()

	require.NoError(t, emitter.Emit(context.Background(), first))
	require.NoError(t, emitter.Emit(context.Background(), second))

	require.Len(t, ids, 2)
	assert.NotEqual(t, ids[0], ids[1])
}

// TestWebhookEventEmitter_Emit_EventIDHasEvtPrefix documents the ID shape
// the design doc specifies ("evt_…") is preserved by the deterministic
// derivation, not just its uniqueness/stability properties.
func TestWebhookEventEmitter_Emit_EventIDHasEvtPrefix(t *testing.T) {
	_, endpointRepo, _, _ := newEmitterFixture(t)
	tenantID := uuid.New()
	endpoint := activeEndpoint(tenantID, "notification.delivered")

	endpointRepo.EXPECT().
		ListActiveByTenantAndEvent(gomock.Any(), tenantID, domain.WebhookEventDelivered).
		Return([]*domain.WebhookEndpoint{endpoint}, nil)

	var eventID string
	captureEnqueuer := enqueuerFunc(func(p WebhookEventTaskPayload) error {
		eventID = p.Event.ID
		return nil
	})
	emitter := NewWebhookEventEmitter(endpointRepo, captureEnqueuer, zerolog.Nop())

	err := emitter.Emit(context.Background(), EmitParams{
		TenantID:        tenantID,
		NotificationID:  uuid.New(),
		ChannelConfigID: uuid.New(),
		Channel:         "telegram",
		EventType:       domain.WebhookEventDelivered,
		Attempts:        1,
	})

	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(eventID, "evt_"), "event ID must keep the evt_ prefix: %s", eventID)
}
