package worker_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bse/notifyd/internal/domain"
	"github.com/bse/notifyd/internal/provider"
	"github.com/bse/notifyd/internal/worker"
)

// inMemoryNotificationRepo is a minimal, concurrency-safe fake of
// domain.NotificationRepository backed by a map. Unlike the gomock-based
// fakes the rest of this package uses (which assert on individual expected
// calls), this test needs a repository that actually accumulates
// retry_count across however many attempts a real asynq.Server drives —
// exactly the value asynq's unexported context keys make impossible to
// inject directly in a unit test, which is why this integration test exists.
type inMemoryNotificationRepo struct {
	mu   sync.Mutex
	byID map[uuid.UUID]*domain.Notification
}

func newInMemoryNotificationRepo() *inMemoryNotificationRepo {
	return &inMemoryNotificationRepo{byID: make(map[uuid.UUID]*domain.Notification)}
}

func (r *inMemoryNotificationRepo) put(n *domain.Notification) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byID[n.ID] = n
}

func (r *inMemoryNotificationRepo) Create(_ context.Context, n *domain.Notification) error {
	r.put(n)
	return nil
}

func (r *inMemoryNotificationRepo) GetByID(_ context.Context, id uuid.UUID) (*domain.Notification, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	n, ok := r.byID[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	copied := *n
	return &copied, nil
}

func (r *inMemoryNotificationRepo) UpdateStatus(_ context.Context, id uuid.UUID, status domain.NotificationStatus, lastError *string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	n, ok := r.byID[id]
	if !ok {
		return domain.ErrNotFound
	}
	n.Status = status
	n.LastError = lastError
	return nil
}

func (r *inMemoryNotificationRepo) SetAsynqTaskID(_ context.Context, id uuid.UUID, taskID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if n, ok := r.byID[id]; ok {
		n.AsynqTaskID = &taskID
	}
	return nil
}

func (r *inMemoryNotificationRepo) SetProviderMsgID(_ context.Context, id uuid.UUID, providerMsgID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if n, ok := r.byID[id]; ok {
		n.ProviderMsgID = &providerMsgID
	}
	return nil
}

func (r *inMemoryNotificationRepo) MarkDelivered(_ context.Context, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if n, ok := r.byID[id]; ok {
		n.Status = domain.StatusDelivered
	}
	return nil
}

// IncrementRetry is the one method this test actually cares about: it must
// behave exactly like the real Postgres-backed implementation (increment by
// one per call) so retry_count after N real dispatcher failures matches
// what the production repository would have recorded.
func (r *inMemoryNotificationRepo) IncrementRetry(_ context.Context, id uuid.UUID, lastError string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	n, ok := r.byID[id]
	if !ok {
		return domain.ErrNotFound
	}
	n.RetryCount++
	n.LastError = &lastError
	return nil
}

func (r *inMemoryNotificationRepo) List(context.Context, domain.NotificationFilter) ([]*domain.Notification, int, error) {
	return nil, 0, nil
}

func (r *inMemoryNotificationRepo) CountByStatus(context.Context) (map[domain.NotificationStatus]int, error) {
	return nil, nil
}

func (r *inMemoryNotificationRepo) UsageByTenant(context.Context, uuid.UUID, time.Time, time.Time) (*domain.UsageReport, error) {
	return nil, nil
}

func (r *inMemoryNotificationRepo) DeleteOlderThan(context.Context, uuid.UUID, time.Time) (int64, error) {
	return 0, nil
}

func (r *inMemoryNotificationRepo) retryCount(id uuid.UUID) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.byID[id].RetryCount
}

// noopDeliveryAttemptRepo and noopDeliveryMetricRepo satisfy the dispatcher's
// remaining dependencies with do-nothing implementations: this test only
// verifies retry_count and the emitted attempts value, not delivery-attempt
// history or metrics.
type noopDeliveryAttemptRepo struct{}

func (noopDeliveryAttemptRepo) Create(context.Context, *domain.DeliveryAttempt) error { return nil }
func (noopDeliveryAttemptRepo) ListByNotification(context.Context, uuid.UUID) ([]*domain.DeliveryAttempt, error) {
	return nil, nil
}

type noopDeliveryMetricRepo struct{}

func (noopDeliveryMetricRepo) Upsert(context.Context, *domain.DeliveryMetric) error { return nil }
func (noopDeliveryMetricRepo) GetByNotificationID(context.Context, uuid.UUID) (*domain.DeliveryMetric, error) {
	return nil, nil
}

// singleChannelConfigRepo always returns the one channel config it was
// constructed with, regardless of the requested ID — sufficient for a test
// that only ever dispatches one notification.
type singleChannelConfigRepo struct {
	cfg *domain.ChannelConfig
}

func (r singleChannelConfigRepo) GetByID(context.Context, uuid.UUID) (*domain.ChannelConfig, error) {
	return r.cfg, nil
}
func (r singleChannelConfigRepo) Create(context.Context, *domain.ChannelConfig) error { return nil }
func (r singleChannelConfigRepo) ListByTenant(context.Context, uuid.UUID) ([]*domain.ChannelConfig, error) {
	return nil, nil
}
func (r singleChannelConfigRepo) ListByTenantAndChannel(context.Context, uuid.UUID, domain.ChannelType) ([]*domain.ChannelConfig, error) {
	return nil, nil
}
func (r singleChannelConfigRepo) Update(context.Context, uuid.UUID, uuid.UUID, domain.UpdateChannelConfigInput) (*domain.ChannelConfig, error) {
	return nil, nil
}
func (r singleChannelConfigRepo) Delete(context.Context, uuid.UUID, uuid.UUID) error { return nil }

// alwaysFailProvider simulates a channel whose every send attempt fails
// transiently (Success: false, Permanent: false) — the only outcome that
// keeps a task retrying all the way to genuine exhaustion, as opposed to
// the SkipRetry path a permanent failure would take instead.
type alwaysFailProvider struct{ typeName string }

func (p alwaysFailProvider) Type() string { return p.typeName }
func (p alwaysFailProvider) Capabilities() provider.ProviderCapabilities {
	return provider.ProviderCapabilities{}
}
func (p alwaysFailProvider) Send(context.Context, json.RawMessage, provider.SendRequest) (*provider.SendResponse, error) {
	return &provider.SendResponse{Success: false, ErrorMessage: "simulated transient failure"}, nil
}
func (p alwaysFailProvider) FetchMetrics(context.Context, json.RawMessage, string) (*provider.DeliveryMetrics, error) {
	return nil, domain.ErrMetricsNotSupported
}
func (p alwaysFailProvider) ValidateConfig(json.RawMessage) error { return nil }

// capturingEmitter records every EmitParams it receives, the same
// observation point production code uses (via terminalEventEmitter) for the
// dispatcher and error handler — but here backed by a real asynq.Server
// instead of a hand-fed context.
type capturingEmitter struct {
	mu    sync.Mutex
	calls []worker.EmitParams
}

func (e *capturingEmitter) Emit(_ context.Context, params worker.EmitParams) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls = append(e.calls, params)
	return nil
}

func (e *capturingEmitter) firstCall() (worker.EmitParams, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.calls) == 0 {
		return worker.EmitParams{}, false
	}
	return e.calls[0], true
}

func (e *capturingEmitter) callCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.calls)
}

// TestRetryExhaustion_EmittedAttempts_MatchesFinalRetryCount drives a real
// asynq.Server, backed by miniredis, through genuine retry exhaustion of a
// notification:deliver task whose provider always fails transiently. It
// asserts the "attempts" value NotifyErrorHandler emits on exhaustion
// (retriedCount(ctx)+1, internal to error_handler.go) equals the
// notification's actual final retry_count in the repository — the one
// number in this codebase that unit tests cannot verify directly, since
// asynq stores retry/max-retry in the context via unexported keys with no
// public constructor (see error_handler_test.go's extensive comment on
// exactly this limitation).
func TestRetryExhaustion_EmittedAttempts_MatchesFinalRetryCount(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	// Registered first so it runs LAST during t.Cleanup's LIFO unwind: the
	// asynq.Server must finish its graceful shutdown (which still talks to
	// Redis to persist state) before miniredis stops listening, or
	// srv.Shutdown() hangs retrying a now-closed connection indefinitely.
	t.Cleanup(mr.Close)

	redisOpt := asynq.RedisClientOpt{Addr: mr.Addr()}
	client := asynq.NewClient(redisOpt)
	t.Cleanup(func() { _ = client.Close() })

	const maxRetry = 2 // total attempts = maxRetry + 1 = 3

	notifID := uuid.New()
	tenantID := uuid.New()
	channelConfigID := uuid.New()

	notifRepo := newInMemoryNotificationRepo()
	notifRepo.put(&domain.Notification{
		ID:         notifID,
		TenantID:   tenantID,
		Status:     domain.StatusPending,
		MaxRetries: maxRetry,
	})

	channelCfg := &domain.ChannelConfig{
		ID:       channelConfigID,
		Channel:  domain.ChannelDiscord,
		Config:   json.RawMessage(`{}`),
		IsActive: true,
	}

	registry := provider.NewRegistry()
	registry.Register(alwaysFailProvider{typeName: "discord"})

	emitter := &capturingEmitter{}

	dispatcher := worker.NewDispatcher(
		registry,
		notifRepo,
		noopDeliveryAttemptRepo{},
		singleChannelConfigRepo{cfg: channelCfg},
		noopDeliveryMetricRepo{},
		emitter,
		zerolog.Nop(),
	)
	errorHandler := worker.NewNotifyErrorHandler(notifRepo, emitter, zerolog.Nop())

	exhausted := make(chan struct{})
	var closeOnce sync.Once
	wrappedErrorHandler := asynq.ErrorHandlerFunc(func(ctx context.Context, task *asynq.Task, err error) {
		errorHandler.HandleError(ctx, task, err)
		retried, _ := asynq.GetRetryCount(ctx)
		maxRetrySeen, _ := asynq.GetMaxRetry(ctx)
		if retried >= maxRetrySeen {
			closeOnce.Do(func() { close(exhausted) })
		}
	})

	srv := asynq.NewServer(redisOpt, asynq.Config{
		Concurrency: 1,
		Queues:      map[string]int{"notifications": 1},
		RetryDelayFunc: func(int, error, *asynq.Task) time.Duration {
			return time.Millisecond
		},
		// asynq's defaults (1s / 5s) are tuned for production, not test
		// wall-clock budgets: the forwarder that moves a scheduled retry
		// back onto the active queue only polls on DelayedTaskCheckInterval,
		// so the default 5s dominates this test's runtime far more than the
		// millisecond-scale RetryDelayFunc above does.
		TaskCheckInterval:        10 * time.Millisecond,
		DelayedTaskCheckInterval: 50 * time.Millisecond,
		ErrorHandler:             wrappedErrorHandler,
	})

	mux := asynq.NewServeMux()
	mux.HandleFunc(worker.TypeNotificationDeliver, dispatcher.HandleNotificationDeliver)

	// Start (not Run) is the embeddable half of asynq's API: Run additionally
	// blocks on an OS signal before calling Shutdown, which never arrives in
	// a test process and leaves srv.Run's goroutine parked forever even
	// after this test's own Shutdown() call completes.
	require.NoError(t, srv.Start(mux))
	t.Cleanup(srv.Shutdown)

	task, err := worker.NewNotificationDeliverTask(worker.NotificationDeliverPayload{
		NotificationID:  notifID,
		TenantID:        tenantID,
		ChannelType:     "discord",
		ChannelConfigID: channelConfigID,
		Body:            "integration test body",
	}, asynq.MaxRetry(maxRetry))
	require.NoError(t, err)

	_, err = client.Enqueue(task)
	require.NoError(t, err)

	// miniredis's internal clock only advances on FastForward; asynq's
	// scheduler/recoverer poll on wall-clock timers regardless, so this
	// loop nudges the virtual clock forward on every tick until either the
	// task exhausts its retries or the test's own timeout fires.
	deadline := time.After(10 * time.Second)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-exhausted:
			goto exhaustedReached
		case <-deadline:
			t.Fatal("timed out waiting for retry exhaustion")
		case <-ticker.C:
			mr.FastForward(time.Second)
		}
	}

exhaustedReached:
	// With MaxRetry(2), asynq allows attempts at retried=0,1,2 — three total
	// attempts — before treating the task as exhausted. Every attempt is a
	// transient provider failure, and handleProviderFailure's non-permanent
	// branch calls IncrementRetry on every single attempt (unlike the
	// success path, which never calls it), so retry_count ends up at 3: one
	// increment per attempt, including the final exhausting one.
	const wantFinalRetryCount = maxRetry + 1
	finalRetryCount := notifRepo.retryCount(notifID)
	require.Equal(t, wantFinalRetryCount, finalRetryCount,
		"sanity check: IncrementRetry must have run once per attempt (3 attempts for MaxRetry(2))")

	emitted, ok := emitter.firstCall()
	require.True(t, ok, "the error handler must have emitted exactly one notification.failed event")
	assert.Equal(t, 1, emitter.callCount(), "retry exhaustion is a single terminal transition; it must emit exactly once")
	assert.Equal(t, domain.WebhookEventFailed, emitted.EventType)
	assert.Equal(t, notifID, emitted.NotificationID)
	// The exhausting attempt's own IncrementRetry call already ran (inside
	// the dispatcher) before HandleError observes it, so retry_count itself
	// — not retry_count+1 — is already the true attempt count on this path;
	// this differs from the *delivered* case (EmitParams' doc comment)
	// precisely because every failure increments while success never does.
	assert.Equal(t, finalRetryCount, emitted.Attempts,
		fmt.Sprintf("emitted attempts (%d) must equal the notification's true final retry_count (%d)",
			emitted.Attempts, finalRetryCount))
}
