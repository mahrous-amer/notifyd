package worker

import (
	"context"

	"github.com/hibiken/asynq"
)

// AsynqWebhookEventEnqueuer adapts *asynq.Client to the taskEnqueuer
// interface WebhookEventEmitter depends on. This is the production
// implementation; tests substitute a recording stub instead (see
// fakeWebhookEmitter and enqueuerFunc in the *_test.go files).
type AsynqWebhookEventEnqueuer struct {
	client *asynq.Client
}

func NewAsynqWebhookEventEnqueuer(client *asynq.Client) *AsynqWebhookEventEnqueuer {
	return &AsynqWebhookEventEnqueuer{client: client}
}

func (e *AsynqWebhookEventEnqueuer) Enqueue(p WebhookEventTaskPayload) error {
	task, err := NewWebhookEventTask(p)
	if err != nil {
		return err
	}
	_, err = e.client.EnqueueContext(context.Background(), task)
	return err
}
