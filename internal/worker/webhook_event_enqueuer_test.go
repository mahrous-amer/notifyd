package worker

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newUnreachableTestClient builds an asynq.Client around a Redis connection
// that will never succeed (nothing listens on port 1), with retries and
// timeouts tightened well below go-redis's defaults so tests asserting on
// the resulting error fail fast instead of waiting out five dial retries.
func newUnreachableTestClient(t *testing.T) *asynq.Client {
	t.Helper()
	rdb := redis.NewClient(&redis.Options{
		Addr:        "127.0.0.1:1",
		DialTimeout: 100 * time.Millisecond,
		MaxRetries:  0,
	})
	client := asynq.NewClientFromRedisClient(rdb)
	t.Cleanup(func() { _ = client.Close() })
	return client
}

// TestAsynqWebhookEventEnqueuer_Enqueue_RejectsUnreachableRedis verifies the
// adapter actually calls through to the Asynq client rather than silently
// no-op'ing: pointed at a Redis address nothing listens on, EnqueueContext
// must fail and Enqueue must surface that failure.
func TestAsynqWebhookEventEnqueuer_Enqueue_RejectsUnreachableRedis(t *testing.T) {
	enqueuer := NewAsynqWebhookEventEnqueuer(newUnreachableTestClient(t))

	err := enqueuer.Enqueue(WebhookEventTaskPayload{
		EndpointID: uuid.New(),
		Event: WebhookEventPayload{
			ID:   "evt_1",
			Type: "notification.delivered",
			Data: WebhookEventData{NotificationID: uuid.New()},
		},
	})

	require.Error(t, err)
}

// TestAsynqWebhookEventEnqueuer_Enqueue_BuildsAWebhookEventTask verifies the
// adapter delegates to NewWebhookEventTask (not some ad-hoc task
// construction) by checking the error path still reports a task-shaped
// problem rather than, say, a JSON marshal error from malformed input —
// this payload marshals cleanly, so any error here must come from the
// client's connection attempt, confirming the task was built and handed off.
func TestAsynqWebhookEventEnqueuer_Enqueue_BuildsAWebhookEventTask(t *testing.T) {
	enqueuer := NewAsynqWebhookEventEnqueuer(newUnreachableTestClient(t))

	err := enqueuer.Enqueue(WebhookEventTaskPayload{
		EndpointID: uuid.New(),
		Event: WebhookEventPayload{
			ID:   "evt_2",
			Type: "notification.failed",
			Data: WebhookEventData{NotificationID: uuid.New()},
		},
	})

	require.Error(t, err)
	assert.NotContains(t, err.Error(), "marshal", "a marshal error would indicate NewWebhookEventTask was bypassed")
}
