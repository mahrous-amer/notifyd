package service_test

import (
	"context"
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
	"github.com/bse/notifyd/internal/service"
)

// notificationServiceFixture groups the service under test with its mocked
// dependencies so each test can set expectations without boilerplate.
type notificationServiceFixture struct {
	svc         *service.NotificationService
	notifRepo   *mocks.MockNotificationRepository
	channelRepo *mocks.MockChannelConfigRepository
}

// buildNotificationServiceFixture constructs a NotificationService whose
// asynq.Client points at an invalid Redis address. The client is never invoked
// by the read-path methods that these tests cover (GetByID, List,
// CountByStatus), so connection failures are not triggered.
func buildNotificationServiceFixture(t *testing.T) notificationServiceFixture {
	t.Helper()

	ctrl := gomock.NewController(t)
	notifRepo := mocks.NewMockNotificationRepository(ctrl)
	channelRepo := mocks.NewMockChannelConfigRepository(ctrl)

	// asynq.NewClient does not dial Redis eagerly, so a placeholder address is
	// safe for tests that never exercise the Send code path.
	asynqClient := asynq.NewClient(asynq.RedisClientOpt{Addr: "localhost:0"})
	t.Cleanup(func() { _ = asynqClient.Close() })

	logger := zerolog.Nop()
	svc := service.NewNotificationService(notifRepo, channelRepo, asynqClient, 3, logger)

	return notificationServiceFixture{
		svc:         svc,
		notifRepo:   notifRepo,
		channelRepo: channelRepo,
	}
}

// TestNotificationService_GetByID_DelegatesToRepo verifies that GetByID
// forwards the notification id to the repository and returns its response.
func TestNotificationService_GetByID_DelegatesToRepo(t *testing.T) {
	f := buildNotificationServiceFixture(t)
	ctx := context.Background()
	id := uuid.New()

	expected := &domain.Notification{
		ID:     id,
		Status: domain.StatusDelivered,
	}
	f.notifRepo.EXPECT().GetByID(ctx, id).Return(expected, nil)

	got, err := f.svc.GetByID(ctx, id)

	require.NoError(t, err)
	assert.Equal(t, expected, got)
}

// TestNotificationService_GetByID_PropagatesRepoError verifies that a
// repository error is returned unchanged.
func TestNotificationService_GetByID_PropagatesRepoError(t *testing.T) {
	f := buildNotificationServiceFixture(t)
	ctx := context.Background()
	id := uuid.New()
	repoErr := errors.New("row not found")

	f.notifRepo.EXPECT().GetByID(ctx, id).Return(nil, repoErr)

	got, err := f.svc.GetByID(ctx, id)

	require.Error(t, err)
	assert.Nil(t, got)
	assert.ErrorIs(t, err, repoErr)
}

// TestNotificationService_List_DefaultLimitApplied verifies that a
// non-positive Limit in the filter is replaced with 20 before the repository
// is called.
func TestNotificationService_List_DefaultLimitApplied(t *testing.T) {
	f := buildNotificationServiceFixture(t)
	ctx := context.Background()
	tenantID := uuid.New()

	// The filter supplied by the caller has Limit=0 (zero value).
	inputFilter := domain.NotificationFilter{
		TenantID: tenantID,
		Limit:    0,
		Offset:   0,
	}

	// The service must normalise the limit to 20 before delegating.
	expectedFilter := domain.NotificationFilter{
		TenantID: tenantID,
		Limit:    20,
		Offset:   0,
	}

	expected := []*domain.Notification{{ID: uuid.New(), Status: domain.StatusPending}}
	f.notifRepo.EXPECT().List(ctx, expectedFilter).Return(expected, 1, nil)

	got, total, err := f.svc.List(ctx, inputFilter)

	require.NoError(t, err)
	assert.Equal(t, 1, total)
	assert.Equal(t, expected, got)
}

// TestNotificationService_List_DelegatesToRepoWithFilter verifies that a fully
// populated filter (including status and channel) is passed through to the
// repository after the limit is validated.
func TestNotificationService_List_DelegatesToRepoWithFilter(t *testing.T) {
	f := buildNotificationServiceFixture(t)
	ctx := context.Background()
	tenantID := uuid.New()

	status := domain.StatusFailed
	channel := domain.ChannelDiscord

	filter := domain.NotificationFilter{
		TenantID: tenantID,
		Status:   &status,
		Channel:  &channel,
		Limit:    10,
		Offset:   5,
	}

	expected := []*domain.Notification{
		{ID: uuid.New(), Status: domain.StatusFailed, Channel: domain.ChannelDiscord},
	}
	f.notifRepo.EXPECT().List(ctx, filter).Return(expected, 1, nil)

	got, total, err := f.svc.List(ctx, filter)

	require.NoError(t, err)
	assert.Equal(t, 1, total)
	assert.Equal(t, expected, got)
}

// TestNotificationService_CountByStatus_DelegatesToRepo verifies that
// CountByStatus delegates directly to the repository and returns the result.
func TestNotificationService_CountByStatus_DelegatesToRepo(t *testing.T) {
	f := buildNotificationServiceFixture(t)
	ctx := context.Background()

	expected := map[domain.NotificationStatus]int{
		domain.StatusPending:   3,
		domain.StatusDelivered: 42,
		domain.StatusFailed:    1,
	}
	f.notifRepo.EXPECT().CountByStatus(ctx).Return(expected, nil)

	got, err := f.svc.CountByStatus(ctx)

	require.NoError(t, err)
	assert.Equal(t, expected, got)
}

// TestNotificationService_CountByStatus_PropagatesRepoError verifies that a
// repository error is returned unchanged.
func TestNotificationService_CountByStatus_PropagatesRepoError(t *testing.T) {
	f := buildNotificationServiceFixture(t)
	ctx := context.Background()
	repoErr := errors.New("query timeout")

	f.notifRepo.EXPECT().CountByStatus(ctx).Return(nil, repoErr)

	got, err := f.svc.CountByStatus(ctx)

	require.Error(t, err)
	assert.Nil(t, got)
	assert.ErrorIs(t, err, repoErr)
}
