package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/bse/notifyd/internal/domain"
	"github.com/bse/notifyd/internal/domain/mocks"
	"github.com/bse/notifyd/internal/provider"
	"github.com/bse/notifyd/internal/service"
)

// buildChannelServiceFixture constructs a ChannelService with a mocked
// repository and a real provider.Registry that has a DiscordProvider registered.
func buildChannelServiceFixture(t *testing.T) (*service.ChannelService, *mocks.MockChannelConfigRepository) {
	t.Helper()

	ctrl := gomock.NewController(t)
	repo := mocks.NewMockChannelConfigRepository(ctrl)

	registry := provider.NewRegistry()
	registry.Register(provider.NewDiscordProvider(http.DefaultClient))

	logger := zerolog.Nop()
	svc := service.NewChannelService(repo, registry, logger)

	return svc, repo
}

// validDiscordConfig returns a minimal JSON config that the DiscordProvider
// will accept without error.
func validDiscordConfig() json.RawMessage {
	return json.RawMessage(`{"webhook_url":"https://discord.com/api/webhooks/test"}`)
}

// invalidDiscordConfig is missing the required webhook_url field.
func invalidDiscordConfig() json.RawMessage {
	return json.RawMessage(`{"webhook_url":""}`)
}

// validCreateInput returns a CreateChannelConfigInput that passes all
// service-level and provider-level validation.
func validCreateInput() domain.CreateChannelConfigInput {
	return domain.CreateChannelConfigInput{
		Channel:       domain.ChannelDiscord,
		Name:          "My Discord Channel",
		Config:        validDiscordConfig(),
		DeliveryPrefs: nil,
	}
}

// TestChannelService_Create_ValidInput verifies that a well-formed input
// causes the service to call repo.Create and return the new ChannelConfig.
func TestChannelService_Create_ValidInput(t *testing.T) {
	svc, repo := buildChannelServiceFixture(t)
	ctx := context.Background()
	tenantID := uuid.New()
	input := validCreateInput()

	repo.EXPECT().
		Create(ctx, gomock.Any()).
		Return(nil)

	cfg, err := svc.Create(ctx, tenantID, input)

	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Equal(t, tenantID, cfg.TenantID)
	assert.Equal(t, domain.ChannelDiscord, cfg.Channel)
	assert.Equal(t, "My Discord Channel", cfg.Name)
	assert.True(t, cfg.IsActive)
	assert.NotEqual(t, uuid.Nil, cfg.ID)
}

// TestChannelService_Create_InvalidChannelType verifies that an unrecognised
// channel type is rejected before any repository interaction.
func TestChannelService_Create_InvalidChannelType(t *testing.T) {
	svc, _ := buildChannelServiceFixture(t)
	ctx := context.Background()

	input := validCreateInput()
	input.Channel = "smoke_signals"

	cfg, err := svc.Create(ctx, uuid.New(), input)

	require.Error(t, err)
	assert.Nil(t, cfg)
	assert.ErrorIs(t, err, domain.ErrValidationFailed)
}

// TestChannelService_Create_EmptyName verifies that a missing name is rejected.
func TestChannelService_Create_EmptyName(t *testing.T) {
	svc, _ := buildChannelServiceFixture(t)
	ctx := context.Background()

	input := validCreateInput()
	input.Name = ""

	cfg, err := svc.Create(ctx, uuid.New(), input)

	require.Error(t, err)
	assert.Nil(t, cfg)
	assert.ErrorIs(t, err, domain.ErrValidationFailed)
}

// TestChannelService_Create_InvalidProviderConfig verifies that a config
// rejected by the provider's ValidateConfig method causes ErrValidationFailed.
func TestChannelService_Create_InvalidProviderConfig(t *testing.T) {
	svc, _ := buildChannelServiceFixture(t)
	ctx := context.Background()

	input := validCreateInput()
	input.Config = invalidDiscordConfig()

	cfg, err := svc.Create(ctx, uuid.New(), input)

	require.Error(t, err)
	assert.Nil(t, cfg)
	assert.ErrorIs(t, err, domain.ErrValidationFailed)
}

// TestChannelService_Create_InvalidDeliveryPrefs verifies that a bad priority
// value in DeliveryPreferences is caught before any repository call.
func TestChannelService_Create_InvalidDeliveryPrefs(t *testing.T) {
	svc, _ := buildChannelServiceFixture(t)
	ctx := context.Background()

	input := validCreateInput()
	input.DeliveryPrefs = &domain.DeliveryPreferences{
		Priority: "turbo", // not a valid priority
	}

	cfg, err := svc.Create(ctx, uuid.New(), input)

	require.Error(t, err)
	assert.Nil(t, cfg)
	assert.ErrorIs(t, err, domain.ErrValidationFailed)
}

// TestChannelService_Create_RepoError verifies that a repository error is
// propagated unchanged to the caller.
func TestChannelService_Create_RepoError(t *testing.T) {
	svc, repo := buildChannelServiceFixture(t)
	ctx := context.Background()
	repoErr := errors.New("database unavailable")

	repo.EXPECT().
		Create(ctx, gomock.Any()).
		Return(repoErr)

	cfg, err := svc.Create(ctx, uuid.New(), validCreateInput())

	require.Error(t, err)
	assert.Nil(t, cfg)
	assert.ErrorIs(t, err, repoErr)
}

// TestChannelService_GetByID_DelegatesToRepo verifies that GetByID passes the
// id directly to the repository and returns whatever the repository returns.
func TestChannelService_GetByID_DelegatesToRepo(t *testing.T) {
	svc, repo := buildChannelServiceFixture(t)
	ctx := context.Background()
	id := uuid.New()

	expected := &domain.ChannelConfig{ID: id, Name: "alpha"}
	repo.EXPECT().GetByID(ctx, id).Return(expected, nil)

	got, err := svc.GetByID(ctx, id)

	require.NoError(t, err)
	assert.Equal(t, expected, got)
}

// TestChannelService_ListByTenant_DelegatesToRepo verifies that ListByTenant
// passes the tenantID directly to the repository.
func TestChannelService_ListByTenant_DelegatesToRepo(t *testing.T) {
	svc, repo := buildChannelServiceFixture(t)
	ctx := context.Background()
	tenantID := uuid.New()

	expected := []*domain.ChannelConfig{
		{ID: uuid.New(), TenantID: tenantID, Name: "beta"},
	}
	repo.EXPECT().ListByTenant(ctx, tenantID).Return(expected, nil)

	got, err := svc.ListByTenant(ctx, tenantID)

	require.NoError(t, err)
	assert.Equal(t, expected, got)
}

// TestChannelService_Update_ValidatesConfigWhenProvided verifies that Update
// calls GetByID to retrieve the channel type, validates the new config via the
// provider, and then calls repo.Update when everything is valid.
func TestChannelService_Update_ValidatesConfigWhenProvided(t *testing.T) {
	svc, repo := buildChannelServiceFixture(t)
	ctx := context.Background()
	id := uuid.New()
	tenantID := uuid.New()

	existing := &domain.ChannelConfig{
		ID:       id,
		TenantID: tenantID,
		Channel:  domain.ChannelDiscord,
	}

	newConfig := validDiscordConfig()
	input := domain.UpdateChannelConfigInput{Config: &newConfig}

	updated := &domain.ChannelConfig{ID: id, TenantID: tenantID, Name: "updated"}

	repo.EXPECT().GetByID(ctx, id).Return(existing, nil)
	repo.EXPECT().Update(ctx, id, tenantID, input).Return(updated, nil)

	got, err := svc.Update(ctx, id, tenantID, input)

	require.NoError(t, err)
	assert.Equal(t, updated, got)
}

// TestChannelService_Update_RejectsInvalidDeliveryPrefs verifies that a bad
// priority in the updated DeliveryPreferences is rejected before any repo call.
func TestChannelService_Update_RejectsInvalidDeliveryPrefs(t *testing.T) {
	svc, _ := buildChannelServiceFixture(t)
	ctx := context.Background()

	input := domain.UpdateChannelConfigInput{
		DeliveryPrefs: &domain.DeliveryPreferences{
			Priority: "ludicrous", // invalid
		},
	}

	got, err := svc.Update(ctx, uuid.New(), uuid.New(), input)

	require.Error(t, err)
	assert.Nil(t, got)
	assert.ErrorIs(t, err, domain.ErrValidationFailed)
}

// TestChannelService_Delete_DelegatesToRepo verifies that Delete forwards
// both the channel config id and the tenant id to the repository.
func TestChannelService_Delete_DelegatesToRepo(t *testing.T) {
	svc, repo := buildChannelServiceFixture(t)
	ctx := context.Background()
	id := uuid.New()
	tenantID := uuid.New()

	repo.EXPECT().Delete(ctx, id, tenantID).Return(nil)

	err := svc.Delete(ctx, id, tenantID)

	require.NoError(t, err)
}
