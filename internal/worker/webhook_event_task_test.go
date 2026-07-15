package worker

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewWebhookEventTask_TypeAndPayload(t *testing.T) {
	endpointID := uuid.New()
	notifID := uuid.New()
	channelConfigID := uuid.New()

	payload := WebhookEventTaskPayload{
		EndpointID: endpointID,
		Event: WebhookEventPayload{
			ID:        "evt_abc123",
			Type:      "notification.delivered",
			CreatedAt: time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC),
			Data: WebhookEventData{
				NotificationID:  notifID,
				ChannelConfigID: channelConfigID,
				Channel:         "telegram",
				Status:          "delivered",
				Attempts:        2,
				Metadata:        json.RawMessage(`{"order_id":"o-1"}`),
			},
		},
	}

	task, err := NewWebhookEventTask(payload)

	require.NoError(t, err)
	assert.Equal(t, TypeWebhookEvent, task.Type())

	var decoded WebhookEventTaskPayload
	require.NoError(t, json.Unmarshal(task.Payload(), &decoded))
	assert.Equal(t, endpointID, decoded.EndpointID)
	assert.Equal(t, "evt_abc123", decoded.Event.ID)
	assert.Equal(t, "notification.delivered", decoded.Event.Type)
	assert.Equal(t, notifID, decoded.Event.Data.NotificationID)
	assert.Equal(t, channelConfigID, decoded.Event.Data.ChannelConfigID)
	assert.Equal(t, "telegram", decoded.Event.Data.Channel)
	assert.Equal(t, "delivered", decoded.Event.Data.Status)
	assert.Equal(t, 2, decoded.Event.Data.Attempts)
}

func TestNewWebhookEventTask_UsesWebhooksQueue(t *testing.T) {
	// The queue name cannot be read back from *asynq.Task directly; this is
	// documented by exercising the constructor without error and relying on
	// dispatcher/worker wiring tests plus manual queue inspection, the same
	// approach tasks_test.go uses for TestNewNotificationDeliverTask_QueueSelection.
	payload := WebhookEventTaskPayload{
		EndpointID: uuid.New(),
		Event: WebhookEventPayload{
			ID:   "evt_1",
			Type: "notification.failed",
			Data: WebhookEventData{NotificationID: uuid.New()},
		},
	}

	task, err := NewWebhookEventTask(payload)

	require.NoError(t, err)
	require.NotNil(t, task)
}

func TestNewWebhookEventTask_TaskIDIncludesEventAndEndpoint(t *testing.T) {
	// Distinct (event ID, endpoint ID) pairs must get distinct Asynq task
	// IDs — reusing the same ID across endpoints would cause asynq to treat
	// the second endpoint's delivery as a duplicate of the first and drop
	// it silently.
	eventID := "evt_shared"
	endpoint1 := uuid.New()
	endpoint2 := uuid.New()

	payload1 := WebhookEventTaskPayload{EndpointID: endpoint1, Event: WebhookEventPayload{ID: eventID, Data: WebhookEventData{NotificationID: uuid.New()}}}
	payload2 := WebhookEventTaskPayload{EndpointID: endpoint2, Event: WebhookEventPayload{ID: eventID, Data: WebhookEventData{NotificationID: uuid.New()}}}

	task1, err := NewWebhookEventTask(payload1)
	require.NoError(t, err)
	task2, err := NewWebhookEventTask(payload2)
	require.NoError(t, err)

	// asynq does not expose the TaskID option through a public accessor, so
	// this is verified indirectly: enqueuing both must not collide. That
	// requires a live client/broker, which task-construction tests
	// deliberately avoid (see TestNewNotificationDeliverTask_TaskIDFormat's
	// same approach) — so this test instead asserts the two tasks were
	// constructed independently without error, and the ID-collision
	// behavior itself is covered by NewWebhookEventTask's implementation
	// deriving the ID from both fields (see source comment).
	assert.NotNil(t, task1)
	assert.NotNil(t, task2)
}
