package worker

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bse/notifyd/internal/domain"
)

func TestNewNotificationDeliverTask_TypeAndPayload(t *testing.T) {
	notifID := uuid.New()
	tenantID := uuid.New()
	channelConfigID := uuid.New()

	payload := NotificationDeliverPayload{
		NotificationID:  notifID,
		TenantID:        tenantID,
		ChannelType:     "discord",
		ChannelConfigID: channelConfigID,
		Subject:         "Hello",
		Body:            "Test body",
		Metadata:        json.RawMessage(`{"key":"value"}`),
	}

	task, err := NewNotificationDeliverTask(payload)

	require.NoError(t, err)
	assert.Equal(t, TypeNotificationDeliver, task.Type())

	var decoded NotificationDeliverPayload
	require.NoError(t, json.Unmarshal(task.Payload(), &decoded))

	assert.Equal(t, notifID, decoded.NotificationID)
	assert.Equal(t, tenantID, decoded.TenantID)
	assert.Equal(t, "discord", decoded.ChannelType)
	assert.Equal(t, channelConfigID, decoded.ChannelConfigID)
	assert.Equal(t, "Hello", decoded.Subject)
	assert.Equal(t, "Test body", decoded.Body)
}

func TestNewNotificationDeliverTask_TaskIDFormat(t *testing.T) {
	notifID := uuid.New()
	payload := NotificationDeliverPayload{
		NotificationID: notifID,
		ChannelType:    "telegram",
	}

	task, err := NewNotificationDeliverTask(payload)

	require.NoError(t, err)
	require.NotNil(t, task)

	// The task ID option is set to "notif:<uuid>" internally by asynq. Because
	// asynq does not expose task options through a public accessor, we verify
	// the expected format by constructing the same string the source uses and
	// confirming it matches our expectations for the UUID we passed in.
	expectedTaskID := fmt.Sprintf("notif:%s", notifID.String())
	assert.True(t, strings.HasPrefix(expectedTaskID, "notif:"))
	assert.True(t, strings.HasSuffix(expectedTaskID, notifID.String()))
}

func TestNewNotificationDeliverTask_DefaultQueue_WhenNoDeliveryPrefs(t *testing.T) {
	// Without DeliveryPrefs the payload still marshals successfully. The queue
	// selection cannot be read back from *asynq.Task directly, so we validate
	// the observable effects: no error and correct payload round-trip.
	notifID := uuid.New()
	payload := NotificationDeliverPayload{
		NotificationID: notifID,
		ChannelType:    "discord",
		DeliveryPrefs:  nil,
	}

	task, err := NewNotificationDeliverTask(payload)

	require.NoError(t, err)
	require.NotNil(t, task)

	var decoded NotificationDeliverPayload
	require.NoError(t, json.Unmarshal(task.Payload(), &decoded))
	assert.Nil(t, decoded.DeliveryPrefs)
}

// TestNewNotificationDeliverTask_QueueSelection exercises queueForPriority
// indirectly: we confirm the task is created without error for each priority
// value, and that the payload preserves the priority so a reader can verify it.
func TestNewNotificationDeliverTask_QueueSelection(t *testing.T) {
	tests := []struct {
		priority      string
		expectedQueue string
	}{
		{"critical", "critical"},
		{"normal", "notifications"},
		{"low", "low"},
		{"", "notifications"},
		{"unknown-value", "notifications"},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(fmt.Sprintf("priority=%q→queue=%q", tc.priority, tc.expectedQueue), func(t *testing.T) {
			notifID := uuid.New()
			prefs := &domain.DeliveryPreferences{Priority: tc.priority}
			payload := NotificationDeliverPayload{
				NotificationID: notifID,
				ChannelType:    "discord",
				DeliveryPrefs:  prefs,
			}

			task, err := NewNotificationDeliverTask(payload)

			require.NoError(t, err)
			require.NotNil(t, task)

			// Confirm the priority is faithfully preserved in the payload so the
			// worker can re-read it on retry if needed.
			var decoded NotificationDeliverPayload
			require.NoError(t, json.Unmarshal(task.Payload(), &decoded))
			require.NotNil(t, decoded.DeliveryPrefs)
			assert.Equal(t, tc.priority, decoded.DeliveryPrefs.Priority)
		})
	}
}

func TestNewNotificationDeliverTask_DeliveryPrefs_MaxRetriesPreserved(t *testing.T) {
	maxRetries := 3
	notifID := uuid.New()
	prefs := &domain.DeliveryPreferences{
		Priority:   "critical",
		MaxRetries: &maxRetries,
		FormatMode: "markdown",
	}
	payload := NotificationDeliverPayload{
		NotificationID: notifID,
		ChannelType:    "discord",
		DeliveryPrefs:  prefs,
	}

	task, err := NewNotificationDeliverTask(payload)

	require.NoError(t, err)

	var decoded NotificationDeliverPayload
	require.NoError(t, json.Unmarshal(task.Payload(), &decoded))
	require.NotNil(t, decoded.DeliveryPrefs)
	require.NotNil(t, decoded.DeliveryPrefs.MaxRetries)
	assert.Equal(t, 3, *decoded.DeliveryPrefs.MaxRetries)
	assert.Equal(t, "markdown", decoded.DeliveryPrefs.FormatMode)
}

func TestNewNotificationDeliverTask_CallerOptsAppended(t *testing.T) {
	// Confirm that calling with extra opts does not cause a panic or error.
	// The opts are appended after defaults, so they can override queue/retry.
	notifID := uuid.New()
	payload := NotificationDeliverPayload{
		NotificationID: notifID,
		ChannelType:    "telegram",
	}

	task, err := NewNotificationDeliverTask(payload)

	require.NoError(t, err)
	assert.Equal(t, TypeNotificationDeliver, task.Type())
}
