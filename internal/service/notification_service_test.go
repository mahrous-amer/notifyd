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
	svc := service.NewNotificationService(notifRepo, channelRepo, &fakeEntitlementRepo{}, asynqClient, 3, logger)

	return notificationServiceFixture{
		svc:         svc,
		notifRepo:   notifRepo,
		channelRepo: channelRepo,
	}
}

// TestNotificationService_Send_EmailChannelWithEmptySubject_ReturnsValidationError
// verifies that sending through an email channel config without a subject is
// rejected before any notification row is created or task enqueued. Email
// requires a subject; other channels do not.
func TestNotificationService_Send_EmailChannelWithEmptySubject_ReturnsValidationError(t *testing.T) {
	f := buildNotificationServiceFixture(t)
	ctx := context.Background()
	tenantID := uuid.New()
	channelConfigID := uuid.New()

	emailChannel := &domain.ChannelConfig{
		ID:       channelConfigID,
		TenantID: tenantID,
		Channel:  domain.ChannelEmail,
		IsActive: true,
	}
	f.channelRepo.EXPECT().GetByID(ctx, channelConfigID).Return(emailChannel, nil)

	input := domain.SendNotificationInput{
		ChannelConfigID: channelConfigID,
		Body:            "Body without a subject",
	}

	notif, err := f.svc.Send(ctx, tenantID, input)

	assert.Nil(t, notif)
	require.Error(t, err)
	assert.ErrorIs(t, err, domain.ErrValidationFailed)
	assert.Contains(t, err.Error(), "subject is required")
}

// TestNotificationService_Send_EmailChannelWithBlankSubject_ReturnsValidationError
// verifies that a whitespace-only subject is treated the same as an absent one.
func TestNotificationService_Send_EmailChannelWithBlankSubject_ReturnsValidationError(t *testing.T) {
	f := buildNotificationServiceFixture(t)
	ctx := context.Background()
	tenantID := uuid.New()
	channelConfigID := uuid.New()

	emailChannel := &domain.ChannelConfig{
		ID:       channelConfigID,
		TenantID: tenantID,
		Channel:  domain.ChannelEmail,
		IsActive: true,
	}
	f.channelRepo.EXPECT().GetByID(ctx, channelConfigID).Return(emailChannel, nil)

	blankSubject := "   "
	input := domain.SendNotificationInput{
		ChannelConfigID: channelConfigID,
		Subject:         &blankSubject,
		Body:            "Body with a blank subject",
	}

	notif, err := f.svc.Send(ctx, tenantID, input)

	assert.Nil(t, notif)
	require.Error(t, err)
	assert.ErrorIs(t, err, domain.ErrValidationFailed)
}

// TestNotificationService_Send_NonEmailChannelWithEmptySubject_SkipsSubjectValidation
// verifies the subject requirement is specific to email: chat channels
// (Discord here) proceed past validation with no subject at all. The
// repository's Create call is forced to fail so the test observes that
// validation was *not* the reason Send returned an error — proving the
// subject check never ran for this channel type.
func TestNotificationService_Send_NonEmailChannelWithEmptySubject_SkipsSubjectValidation(t *testing.T) {
	f := buildNotificationServiceFixture(t)
	ctx := context.Background()
	tenantID := uuid.New()
	channelConfigID := uuid.New()

	discordChannel := &domain.ChannelConfig{
		ID:       channelConfigID,
		TenantID: tenantID,
		Channel:  domain.ChannelDiscord,
		IsActive: true,
	}
	f.channelRepo.EXPECT().GetByID(ctx, channelConfigID).Return(discordChannel, nil)

	createErr := errors.New("stop before reaching the asynq enqueue call")
	f.notifRepo.EXPECT().Create(ctx, gomock.Any()).Return(createErr)

	input := domain.SendNotificationInput{
		ChannelConfigID: channelConfigID,
		Body:            "Body without a subject",
	}

	notif, err := f.svc.Send(ctx, tenantID, input)

	assert.Nil(t, notif)
	require.Error(t, err)
	assert.NotErrorIs(t, err, domain.ErrValidationFailed)
	assert.ErrorIs(t, err, createErr)
}

// TestNotificationService_Send_EmailChannelWithSubject_PassesValidation verifies
// that a populated subject clears the email subject check and Send proceeds to
// create the notification row.
func TestNotificationService_Send_EmailChannelWithSubject_PassesValidation(t *testing.T) {
	f := buildNotificationServiceFixture(t)
	ctx := context.Background()
	tenantID := uuid.New()
	channelConfigID := uuid.New()

	emailChannel := &domain.ChannelConfig{
		ID:       channelConfigID,
		TenantID: tenantID,
		Channel:  domain.ChannelEmail,
		IsActive: true,
	}
	f.channelRepo.EXPECT().GetByID(ctx, channelConfigID).Return(emailChannel, nil)

	createErr := errors.New("stop before reaching the asynq enqueue call")
	f.notifRepo.EXPECT().Create(ctx, gomock.Any()).Return(createErr)

	subject := "Deployment complete"
	input := domain.SendNotificationInput{
		ChannelConfigID: channelConfigID,
		Subject:         &subject,
		Body:            "Body with a subject",
	}

	notif, err := f.svc.Send(ctx, tenantID, input)

	assert.Nil(t, notif)
	require.Error(t, err)
	assert.NotErrorIs(t, err, domain.ErrValidationFailed)
	assert.ErrorIs(t, err, createErr)
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
