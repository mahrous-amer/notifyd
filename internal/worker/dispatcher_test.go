package worker

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/bse/notifyd/internal/domain"
	"github.com/bse/notifyd/internal/domain/mocks"
	"github.com/bse/notifyd/internal/provider"
)

// mockProvider is a hand-written test double for provider.Provider. Using a
// struct with a configurable function avoids the overhead of a generated mock
// for a type that is exercised in very few, simple ways here.
type mockProvider struct {
	sendFunc func(ctx context.Context, cfg json.RawMessage, req provider.SendRequest) (*provider.SendResponse, error)
	typeName string
}

func (m *mockProvider) Type() string { return m.typeName }

func (m *mockProvider) Capabilities() provider.ProviderCapabilities {
	return provider.ProviderCapabilities{}
}

func (m *mockProvider) Send(ctx context.Context, cfg json.RawMessage, req provider.SendRequest) (*provider.SendResponse, error) {
	return m.sendFunc(ctx, cfg, req)
}

func (m *mockProvider) FetchMetrics(_ context.Context, _ json.RawMessage, _ string) (*provider.DeliveryMetrics, error) {
	return nil, domain.ErrMetricsNotSupported
}

func (m *mockProvider) ValidateConfig(_ json.RawMessage) error { return nil }

// dispatcherTestFixture groups the mocks and dispatcher used across test cases
// to reduce boilerplate. Each test constructs its own fixture so mock
// expectations remain isolated.
type dispatcherTestFixture struct {
	ctrl        *gomock.Controller
	notifRepo   *mocks.MockNotificationRepository
	attemptRepo *mocks.MockDeliveryAttemptRepository
	channelRepo *mocks.MockChannelConfigRepository
	metricRepo  *mocks.MockDeliveryMetricRepository
	registry    *provider.Registry
	dispatcher  *Dispatcher
}

func newDispatcherFixture(t *testing.T) *dispatcherTestFixture {
	t.Helper()
	ctrl := gomock.NewController(t)
	f := &dispatcherTestFixture{
		ctrl:        ctrl,
		notifRepo:   mocks.NewMockNotificationRepository(ctrl),
		attemptRepo: mocks.NewMockDeliveryAttemptRepository(ctrl),
		channelRepo: mocks.NewMockChannelConfigRepository(ctrl),
		metricRepo:  mocks.NewMockDeliveryMetricRepository(ctrl),
		registry:    provider.NewRegistry(),
	}
	f.dispatcher = NewDispatcher(
		f.registry,
		f.notifRepo,
		f.attemptRepo,
		f.channelRepo,
		f.metricRepo,
		zerolog.Nop(),
	)
	return f
}

// makeTask serialises a NotificationDeliverPayload into an *asynq.Task the
// same way the dispatcher's handler will receive it in production.
func makeTask(p NotificationDeliverPayload) *asynq.Task {
	payload, _ := json.Marshal(p)
	return asynq.NewTask(TypeNotificationDeliver, payload)
}

// makeChannelConfig returns a minimal ChannelConfig sufficient for dispatcher tests.
func makeChannelConfig(id uuid.UUID) *domain.ChannelConfig {
	return &domain.ChannelConfig{
		ID:       id,
		Channel:  domain.ChannelDiscord,
		Config:   json.RawMessage(`{"webhook_url":"https://discord.example.com"}`),
		IsActive: true,
	}
}

func TestHandleNotificationDeliver_Success(t *testing.T) {
	f := newDispatcherFixture(t)

	notifID := uuid.New()
	channelConfigID := uuid.New()
	providerMsgID := "provider-msg-123"

	channelCfg := makeChannelConfig(channelConfigID)

	prov := &mockProvider{
		typeName: "discord",
		sendFunc: func(_ context.Context, _ json.RawMessage, _ provider.SendRequest) (*provider.SendResponse, error) {
			return &provider.SendResponse{
				Success:       true,
				ProviderMsgID: providerMsgID,
				ProviderData:  json.RawMessage(`{"status":"sent"}`),
			}, nil
		},
	}
	f.registry.Register(prov)

	// Status updated to processing first, then the attempt is created, then
	// the notification is marked delivered and the provider msg ID stored.
	f.notifRepo.EXPECT().
		UpdateStatus(gomock.Any(), notifID, domain.StatusProcessing, gomock.Nil()).
		Return(nil)
	f.channelRepo.EXPECT().
		GetByID(gomock.Any(), channelConfigID).
		Return(channelCfg, nil)
	f.attemptRepo.EXPECT().
		Create(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, a *domain.DeliveryAttempt) error {
			assert.Equal(t, notifID, a.NotificationID)
			assert.Equal(t, domain.AttemptSuccess, a.Status)
			return nil
		})
	f.notifRepo.EXPECT().
		MarkDelivered(gomock.Any(), notifID).
		Return(nil)
	f.notifRepo.EXPECT().
		SetProviderMsgID(gomock.Any(), notifID, providerMsgID).
		Return(nil)
	f.metricRepo.EXPECT().
		Upsert(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, m *domain.DeliveryMetric) error {
			assert.Equal(t, notifID, m.NotificationID)
			assert.Equal(t, providerMsgID, m.ProviderMsgID)
			return nil
		})

	task := makeTask(NotificationDeliverPayload{
		NotificationID:  notifID,
		ChannelType:     "discord",
		ChannelConfigID: channelConfigID,
		Subject:         "Hello",
		Body:            "World",
	})

	err := f.dispatcher.HandleNotificationDeliver(context.Background(), task)

	require.NoError(t, err)
}

func TestHandleNotificationDeliver_ProviderFailure_SetsRetryingStatus(t *testing.T) {
	f := newDispatcherFixture(t)

	notifID := uuid.New()
	channelConfigID := uuid.New()
	channelCfg := makeChannelConfig(channelConfigID)
	providerErrMsg := "upstream rate limit exceeded"

	prov := &mockProvider{
		typeName: "discord",
		sendFunc: func(_ context.Context, _ json.RawMessage, _ provider.SendRequest) (*provider.SendResponse, error) {
			return &provider.SendResponse{
				Success:      false,
				ErrorMessage: providerErrMsg,
			}, nil
		},
	}
	f.registry.Register(prov)

	f.notifRepo.EXPECT().
		UpdateStatus(gomock.Any(), notifID, domain.StatusProcessing, gomock.Nil()).
		Return(nil)
	f.channelRepo.EXPECT().
		GetByID(gomock.Any(), channelConfigID).
		Return(channelCfg, nil)
	f.attemptRepo.EXPECT().
		Create(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, a *domain.DeliveryAttempt) error {
			assert.Equal(t, domain.AttemptFailure, a.Status)
			require.NotNil(t, a.ErrorMessage)
			assert.Equal(t, providerErrMsg, *a.ErrorMessage)
			return nil
		})
	f.notifRepo.EXPECT().
		IncrementRetry(gomock.Any(), notifID, providerErrMsg).
		Return(nil)
	f.notifRepo.EXPECT().
		UpdateStatus(gomock.Any(), notifID, domain.StatusRetrying, gomock.Any()).
		Return(nil)

	task := makeTask(NotificationDeliverPayload{
		NotificationID:  notifID,
		ChannelType:     "discord",
		ChannelConfigID: channelConfigID,
		Body:            "Hello",
	})

	err := f.dispatcher.HandleNotificationDeliver(context.Background(), task)

	// A provider failure returns a non-nil error so asynq will retry the task.
	require.Error(t, err)
	assert.Contains(t, err.Error(), providerErrMsg)
}

func TestHandleNotificationDeliver_PermanentProviderFailure_SetsFailedStatusAndSkipsRetry(t *testing.T) {
	f := newDispatcherFixture(t)

	notifID := uuid.New()
	channelConfigID := uuid.New()
	channelCfg := makeChannelConfig(channelConfigID)
	providerErrMsg := "smtp: authentication failed"

	prov := &mockProvider{
		typeName: "discord",
		sendFunc: func(_ context.Context, _ json.RawMessage, _ provider.SendRequest) (*provider.SendResponse, error) {
			return &provider.SendResponse{
				Success:      false,
				Permanent:    true,
				ErrorMessage: providerErrMsg,
			}, nil
		},
	}
	f.registry.Register(prov)

	f.notifRepo.EXPECT().
		UpdateStatus(gomock.Any(), notifID, domain.StatusProcessing, gomock.Nil()).
		Return(nil)
	f.channelRepo.EXPECT().
		GetByID(gomock.Any(), channelConfigID).
		Return(channelCfg, nil)
	f.attemptRepo.EXPECT().
		Create(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, a *domain.DeliveryAttempt) error {
			assert.Equal(t, domain.AttemptFailure, a.Status)
			require.NotNil(t, a.ErrorMessage)
			assert.Equal(t, providerErrMsg, *a.ErrorMessage)
			return nil
		})
	// A permanent failure goes straight to StatusFailed, never StatusRetrying,
	// but retry_count must still be incremented so it stays consistent with
	// delivery_attempts.attempt_number (both record "one attempt happened"),
	// matching how the retry-exhaustion path keeps the two in sync.
	f.notifRepo.EXPECT().
		IncrementRetry(gomock.Any(), notifID, providerErrMsg).
		Return(nil)
	f.notifRepo.EXPECT().
		UpdateStatus(gomock.Any(), notifID, domain.StatusFailed, gomock.Any()).
		Return(nil)

	task := makeTask(NotificationDeliverPayload{
		NotificationID:  notifID,
		ChannelType:     "discord",
		ChannelConfigID: channelConfigID,
		Body:            "Hello",
	})

	err := f.dispatcher.HandleNotificationDeliver(context.Background(), task)

	require.Error(t, err)
	assert.Contains(t, err.Error(), providerErrMsg)
	assert.True(t, errors.Is(err, asynq.SkipRetry), "permanent provider failures must not be retried")
}

// TestHandleNotificationDeliver_PermanentProviderFailure_IncrementRetryErrorDoesNotBlockStatusUpdate
// verifies that a failure to increment the retry counter is logged and
// swallowed rather than blocking the StatusFailed update, matching the
// log-and-continue style used throughout the rest of the dispatcher.
func TestHandleNotificationDeliver_PermanentProviderFailure_IncrementRetryErrorDoesNotBlockStatusUpdate(t *testing.T) {
	f := newDispatcherFixture(t)

	notifID := uuid.New()
	channelConfigID := uuid.New()
	channelCfg := makeChannelConfig(channelConfigID)
	providerErrMsg := "smtp: mailbox unavailable"

	prov := &mockProvider{
		typeName: "discord",
		sendFunc: func(_ context.Context, _ json.RawMessage, _ provider.SendRequest) (*provider.SendResponse, error) {
			return &provider.SendResponse{
				Success:      false,
				Permanent:    true,
				ErrorMessage: providerErrMsg,
			}, nil
		},
	}
	f.registry.Register(prov)

	f.notifRepo.EXPECT().
		UpdateStatus(gomock.Any(), notifID, domain.StatusProcessing, gomock.Nil()).
		Return(nil)
	f.channelRepo.EXPECT().
		GetByID(gomock.Any(), channelConfigID).
		Return(channelCfg, nil)
	f.attemptRepo.EXPECT().
		Create(gomock.Any(), gomock.Any()).
		Return(nil)
	f.notifRepo.EXPECT().
		IncrementRetry(gomock.Any(), notifID, providerErrMsg).
		Return(errors.New("db connection lost"))
	f.notifRepo.EXPECT().
		UpdateStatus(gomock.Any(), notifID, domain.StatusFailed, gomock.Any()).
		Return(nil)

	task := makeTask(NotificationDeliverPayload{
		NotificationID:  notifID,
		ChannelType:     "discord",
		ChannelConfigID: channelConfigID,
		Body:            "Hello",
	})

	err := f.dispatcher.HandleNotificationDeliver(context.Background(), task)

	require.Error(t, err)
	assert.True(t, errors.Is(err, asynq.SkipRetry))
}

func TestHandleNotificationDeliver_TransportError_IncrementsRetry(t *testing.T) {
	f := newDispatcherFixture(t)

	notifID := uuid.New()
	channelConfigID := uuid.New()
	channelCfg := makeChannelConfig(channelConfigID)
	transportErr := errors.New("connection refused")

	prov := &mockProvider{
		typeName: "discord",
		sendFunc: func(_ context.Context, _ json.RawMessage, _ provider.SendRequest) (*provider.SendResponse, error) {
			return nil, transportErr
		},
	}
	f.registry.Register(prov)

	f.notifRepo.EXPECT().
		UpdateStatus(gomock.Any(), notifID, domain.StatusProcessing, gomock.Nil()).
		Return(nil)
	f.channelRepo.EXPECT().
		GetByID(gomock.Any(), channelConfigID).
		Return(channelCfg, nil)
	f.attemptRepo.EXPECT().
		Create(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, a *domain.DeliveryAttempt) error {
			assert.Equal(t, domain.AttemptFailure, a.Status)
			require.NotNil(t, a.ErrorMessage)
			assert.Equal(t, transportErr.Error(), *a.ErrorMessage)
			return nil
		})
	f.notifRepo.EXPECT().
		IncrementRetry(gomock.Any(), notifID, transportErr.Error()).
		Return(nil)
	f.notifRepo.EXPECT().
		UpdateStatus(gomock.Any(), notifID, domain.StatusRetrying, gomock.Any()).
		Return(nil)

	task := makeTask(NotificationDeliverPayload{
		NotificationID:  notifID,
		ChannelType:     "discord",
		ChannelConfigID: channelConfigID,
		Body:            "Hello",
	})

	err := f.dispatcher.HandleNotificationDeliver(context.Background(), task)

	require.Error(t, err)
	// The transport error is returned as-is so asynq will schedule a retry.
	assert.Equal(t, transportErr, err)
}

func TestHandleNotificationDeliver_InvalidPayload_ReturnsSkipRetry(t *testing.T) {
	f := newDispatcherFixture(t)

	// Craft a task with a payload that cannot unmarshal into NotificationDeliverPayload.
	task := asynq.NewTask(TypeNotificationDeliver, []byte("this is not valid json{{{"))

	err := f.dispatcher.HandleNotificationDeliver(context.Background(), task)

	require.Error(t, err)
	// The dispatcher wraps the unmarshal error with asynq.SkipRetry so that
	// asynq does not retry a permanently broken task.
	assert.True(t, errors.Is(err, asynq.SkipRetry))
}

func TestHandleNotificationDeliver_ChannelConfigNotFound_ReturnsSkipRetry(t *testing.T) {
	f := newDispatcherFixture(t)

	notifID := uuid.New()
	channelConfigID := uuid.New()

	f.notifRepo.EXPECT().
		UpdateStatus(gomock.Any(), notifID, domain.StatusProcessing, gomock.Nil()).
		Return(nil)
	f.channelRepo.EXPECT().
		GetByID(gomock.Any(), channelConfigID).
		Return(nil, domain.ErrNotFound)

	task := makeTask(NotificationDeliverPayload{
		NotificationID:  notifID,
		ChannelType:     "discord",
		ChannelConfigID: channelConfigID,
		Body:            "Hello",
	})

	err := f.dispatcher.HandleNotificationDeliver(context.Background(), task)

	require.Error(t, err)
	// A missing channel config is a permanent failure; retrying cannot fix it.
	assert.True(t, errors.Is(err, asynq.SkipRetry))
}

func TestHandleNotificationDeliver_UnknownProviderType_ReturnsSkipRetry(t *testing.T) {
	f := newDispatcherFixture(t)

	notifID := uuid.New()
	channelConfigID := uuid.New()
	channelCfg := makeChannelConfig(channelConfigID)

	// Register no provider — "discord" will not be found in the registry.
	f.notifRepo.EXPECT().
		UpdateStatus(gomock.Any(), notifID, domain.StatusProcessing, gomock.Nil()).
		Return(nil)
	f.channelRepo.EXPECT().
		GetByID(gomock.Any(), channelConfigID).
		Return(channelCfg, nil)

	task := makeTask(NotificationDeliverPayload{
		NotificationID:  notifID,
		ChannelType:     "discord",
		ChannelConfigID: channelConfigID,
		Body:            "Hello",
	})

	err := f.dispatcher.HandleNotificationDeliver(context.Background(), task)

	require.Error(t, err)
	// An unregistered provider type is a permanent configuration error.
	assert.True(t, errors.Is(err, asynq.SkipRetry))
}

func TestHandleNotificationDeliver_Success_NoProviderMsgID_SkipsMetric(t *testing.T) {
	// When the provider does not return a ProviderMsgID, SetProviderMsgID and
	// the initial metric creation must not be called.
	f := newDispatcherFixture(t)

	notifID := uuid.New()
	channelConfigID := uuid.New()
	channelCfg := makeChannelConfig(channelConfigID)

	prov := &mockProvider{
		typeName: "discord",
		sendFunc: func(_ context.Context, _ json.RawMessage, _ provider.SendRequest) (*provider.SendResponse, error) {
			return &provider.SendResponse{
				Success:       true,
				ProviderMsgID: "", // no provider msg ID
			}, nil
		},
	}
	f.registry.Register(prov)

	f.notifRepo.EXPECT().
		UpdateStatus(gomock.Any(), notifID, domain.StatusProcessing, gomock.Nil()).
		Return(nil)
	f.channelRepo.EXPECT().
		GetByID(gomock.Any(), channelConfigID).
		Return(channelCfg, nil)
	f.attemptRepo.EXPECT().
		Create(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, a *domain.DeliveryAttempt) error {
			assert.Equal(t, domain.AttemptSuccess, a.Status)
			return nil
		})
	f.notifRepo.EXPECT().
		MarkDelivered(gomock.Any(), notifID).
		Return(nil)
	// SetProviderMsgID and metricRepo.Upsert must NOT be called.

	task := makeTask(NotificationDeliverPayload{
		NotificationID:  notifID,
		ChannelType:     "discord",
		ChannelConfigID: channelConfigID,
		Body:            "Hello",
	})

	err := f.dispatcher.HandleNotificationDeliver(context.Background(), task)

	require.NoError(t, err)
}
