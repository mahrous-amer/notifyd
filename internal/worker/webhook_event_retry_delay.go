package worker

import (
	"math/rand/v2"
	"time"

	"github.com/hibiken/asynq"
)

// WebhookEventRetryDelay computes the backoff delay before the next
// "webhook:event" delivery attempt: doubling from webhookEventMinRetryDelay,
// capped at webhookEventMaxRetryDelay, with up to 20% jitter to avoid
// synchronized retry storms across many endpoints failing at once.
//
// n is the number of attempts already made (0 on the first retry, i.e.
// after the initial attempt failed), matching asynq's RetryDelayFunc
// contract.
func WebhookEventRetryDelay(n int, _ error, _ *asynq.Task) time.Duration {
	base := webhookEventMinRetryDelay * time.Duration(1<<uint(n))
	if base > webhookEventMaxRetryDelay {
		base = webhookEventMaxRetryDelay
	}
	delay := base + jitter(base)
	if delay > webhookEventMaxRetryDelay {
		delay = webhookEventMaxRetryDelay
	}
	return delay
}

// jitter returns a random duration in [0, base/5) to spread out retries that
// would otherwise all fire at the same instant. rand.Int64N panics when
// given an argument <= 0, so any base too small to produce a positive
// one-fifth (zero, negative, or simply tiny) floors to no jitter at all
// rather than crashing the worker process — base itself is still the
// dominant term in the caller's delay either way.
func jitter(base time.Duration) time.Duration {
	fifth := int64(base) / 5
	if fifth <= 0 {
		return 0
	}
	return time.Duration(rand.Int64N(fifth))
}

// RetryDelayForTask dispatches to WebhookEventRetryDelay for "webhook:event"
// tasks and falls back to defaultDelay for every other task type. Used as
// cmd/worker/main.go's single asynq.Config.RetryDelayFunc, since Asynq
// applies one retry-delay function server-wide rather than per task type —
// branching on t.Type() here is what lets "webhook:event" use a backoff
// curve spanning hours while "notification:deliver" keeps its existing,
// much shorter curve unchanged.
func RetryDelayForTask(defaultDelay func(n int, err error, t *asynq.Task) time.Duration) func(n int, err error, t *asynq.Task) time.Duration {
	return func(n int, err error, t *asynq.Task) time.Duration {
		if t.Type() == TypeWebhookEvent {
			return WebhookEventRetryDelay(n, err, t)
		}
		return defaultDelay(n, err, t)
	}
}
