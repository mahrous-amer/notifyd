package worker

import (
	"errors"
	"testing"
	"time"

	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/assert"
)

func TestWebhookEventRetryDelay_SumOverEightAttempts_IsRoughlySixHours(t *testing.T) {
	// The design doc calls for "up to 8 attempts over ~6h". This asserts the
	// worst case (no jitter) sum is in a broad band around six hours rather
	// than pinning an exact formula, so the backoff curve can be tuned later
	// without this test becoming a maintenance burden.
	task := asynq.NewTask(TypeWebhookEvent, nil)
	var total time.Duration
	for attempt := 0; attempt < webhookEventMaxRetry; attempt++ {
		total += WebhookEventRetryDelay(attempt, errors.New("x"), task)
	}

	assert.Greater(t, total, 3*time.Hour, "backoff should span multiple hours, not minutes")
	assert.Less(t, total, 10*time.Hour, "backoff should not run past roughly a day")
}

func TestWebhookEventRetryDelay_IsMonotonicallyNonDecreasing(t *testing.T) {
	task := asynq.NewTask(TypeWebhookEvent, nil)
	var previous time.Duration
	for attempt := 0; attempt < webhookEventMaxRetry; attempt++ {
		delay := WebhookEventRetryDelay(attempt, errors.New("x"), task)
		assert.GreaterOrEqual(t, delay, previous, "attempt %d should not have a shorter delay than the previous attempt", attempt)
		previous = delay
	}
}

func TestWebhookEventRetryDelay_NeverExceedsTheConfiguredCap(t *testing.T) {
	task := asynq.NewTask(TypeWebhookEvent, nil)
	for attempt := 0; attempt < 20; attempt++ {
		delay := WebhookEventRetryDelay(attempt, errors.New("x"), task)
		assert.LessOrEqual(t, delay, webhookEventMaxRetryDelay)
	}
}

func TestRetryDelayForTask_WebhookEventTask_UsesWebhookCurve(t *testing.T) {
	defaultDelay := func(int, error, *asynq.Task) time.Duration { return time.Hour }
	dispatched := RetryDelayForTask(defaultDelay)

	task := asynq.NewTask(TypeWebhookEvent, nil)
	delay := dispatched(0, errors.New("x"), task)

	assert.Less(t, delay, time.Hour, "the first webhook:event retry should use webhookEventMinRetryDelay, not the unrelated default curve")
}

func TestRetryDelayForTask_OtherTaskTypes_UseTheDefaultCurve(t *testing.T) {
	const sentinelDelay = 42 * time.Second
	defaultDelay := func(int, error, *asynq.Task) time.Duration { return sentinelDelay }
	dispatched := RetryDelayForTask(defaultDelay)

	task := asynq.NewTask(TypeNotificationDeliver, nil)
	delay := dispatched(0, errors.New("x"), task)

	assert.Equal(t, sentinelDelay, delay)
}

// TestJitter_ZeroBase_DoesNotPanic guards against the panic landmine in
// rand.Int64N: it panics if called with an argument <= 0. WebhookEventRetryDelay
// can never actually pass 0 today (webhookEventMinRetryDelay is 2 minutes,
// so base is always positive), but a future tuner lowering that constant
// toward zero — or extending this backoff curve to a task type with a
// smaller starting delay — could reintroduce the crash. jitter must floor
// its input rather than trust the caller's base is always large enough.
func TestJitter_ZeroBase_DoesNotPanic(t *testing.T) {
	assert.NotPanics(t, func() {
		jitter(0)
	})
}

func TestJitter_NegativeBase_DoesNotPanic(t *testing.T) {
	assert.NotPanics(t, func() {
		jitter(-1 * time.Second)
	})
}

func TestJitter_PositiveBase_StaysWithinOneFifth(t *testing.T) {
	base := 10 * time.Second
	for i := 0; i < 100; i++ {
		j := jitter(base)
		assert.GreaterOrEqual(t, j, time.Duration(0))
		assert.Less(t, j, base/5)
	}
}
