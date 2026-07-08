package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"golang.org/x/crypto/bcrypt"

	"github.com/bse/notifyd/internal/domain"
	"github.com/bse/notifyd/internal/domain/mocks"
	"github.com/bse/notifyd/internal/service"
)

// buildTenantServiceFixture constructs a TenantService with a mocked tenant
// repo and an in-memory fake key repo (so Create can write the initial key row).
func buildTenantServiceFixture(t *testing.T) (*service.TenantService, *mocks.MockTenantRepository) {
	t.Helper()

	ctrl := gomock.NewController(t)
	repo := mocks.NewMockTenantRepository(ctrl)
	svc := service.NewTenantService(repo, &fakeAPIKeyRepo{})

	return svc, repo
}

// TestTenantService_Create_ValidInput verifies that a well-formed input
// generates a random API key and a bcrypt-hashed secret, then persists the
// tenant. The returned result exposes the plaintext secret for the caller.
func TestTenantService_Create_ValidInput(t *testing.T) {
	svc, repo := buildTenantServiceFixture(t)
	ctx := context.Background()

	input := domain.CreateTenantInput{Name: "Acme Corp", Slug: "acme-corp"}

	repo.EXPECT().
		Create(ctx, gomock.Any()).
		Return(nil)

	result, err := svc.Create(ctx, input)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Tenant)

	assert.Equal(t, "Acme Corp", result.Tenant.Name)
	assert.Equal(t, "acme-corp", result.Tenant.Slug)
	assert.True(t, result.Tenant.IsActive)
	assert.NotEqual(t, uuid.Nil, result.Tenant.ID)

	// The API key should be non-empty and be stored on the tenant unchanged.
	assert.NotEmpty(t, result.APIKey)
	assert.Equal(t, result.APIKey, result.Tenant.APIKey)

	// The secret returned to the caller must be the plaintext version.
	// The tenant's stored APISecret must be its bcrypt hash.
	assert.NotEmpty(t, result.APISecret)
	hashErr := bcrypt.CompareHashAndPassword([]byte(result.Tenant.APISecret), []byte(result.APISecret))
	assert.NoError(t, hashErr, "stored APISecret must be a valid bcrypt hash of the returned plaintext secret")
}

// TestTenantService_Create_EmptyName verifies that a missing name is rejected
// before any repository interaction.
func TestTenantService_Create_EmptyName(t *testing.T) {
	svc, _ := buildTenantServiceFixture(t)
	ctx := context.Background()

	input := domain.CreateTenantInput{Name: "", Slug: "some-slug"}

	result, err := svc.Create(ctx, input)

	require.Error(t, err)
	assert.Nil(t, result)
	assert.ErrorIs(t, err, domain.ErrValidationFailed)
}

// TestTenantService_Create_EmptySlug verifies that a missing slug is rejected
// before any repository interaction.
func TestTenantService_Create_EmptySlug(t *testing.T) {
	svc, _ := buildTenantServiceFixture(t)
	ctx := context.Background()

	input := domain.CreateTenantInput{Name: "Acme Corp", Slug: ""}

	result, err := svc.Create(ctx, input)

	require.Error(t, err)
	assert.Nil(t, result)
	assert.ErrorIs(t, err, domain.ErrValidationFailed)
}

// TestTenantService_Create_RepoError verifies that a repository error is
// propagated to the caller without wrapping that conceals the original error.
func TestTenantService_Create_RepoError(t *testing.T) {
	svc, repo := buildTenantServiceFixture(t)
	ctx := context.Background()
	repoErr := errors.New("duplicate key")

	repo.EXPECT().
		Create(ctx, gomock.Any()).
		Return(repoErr)

	result, err := svc.Create(ctx, domain.CreateTenantInput{Name: "X", Slug: "x"})

	require.Error(t, err)
	assert.Nil(t, result)
	assert.ErrorIs(t, err, repoErr)
}

// TestTenantService_GetByID_DelegatesToRepo verifies that GetByID forwards
// the id to the repository and returns whatever the repository returns.
func TestTenantService_GetByID_DelegatesToRepo(t *testing.T) {
	svc, repo := buildTenantServiceFixture(t)
	ctx := context.Background()
	id := uuid.New()

	expected := &domain.Tenant{ID: id, Name: "Acme Corp"}
	repo.EXPECT().GetByID(ctx, id).Return(expected, nil)

	got, err := svc.GetByID(ctx, id)

	require.NoError(t, err)
	assert.Equal(t, expected, got)
}

// TestTenantService_GetBySlug_DelegatesToRepo verifies that GetBySlug forwards
// the slug to the repository and returns whatever the repository returns.
func TestTenantService_GetBySlug_DelegatesToRepo(t *testing.T) {
	svc, repo := buildTenantServiceFixture(t)
	ctx := context.Background()
	slug := "acme-corp"

	expected := &domain.Tenant{ID: uuid.New(), Name: "Acme Corp", Slug: slug}
	repo.EXPECT().GetBySlug(ctx, slug).Return(expected, nil)

	got, err := svc.GetBySlug(ctx, slug)

	require.NoError(t, err)
	assert.Equal(t, expected, got)
}

// TestTenantService_Update_DelegatesToRepo verifies that Update forwards the
// id and input to the repository unchanged.
func TestTenantService_Update_DelegatesToRepo(t *testing.T) {
	svc, repo := buildTenantServiceFixture(t)
	ctx := context.Background()
	id := uuid.New()

	newName := "Renamed Corp"
	input := domain.UpdateTenantInput{Name: &newName}
	expected := &domain.Tenant{ID: id, Name: newName}

	repo.EXPECT().Update(ctx, id, input).Return(expected, nil)

	got, err := svc.Update(ctx, id, input)

	require.NoError(t, err)
	assert.Equal(t, expected, got)
}

// TestTenantService_Delete_DelegatesToRepo verifies that Delete forwards the
// id to the repository.
func TestTenantService_Delete_DelegatesToRepo(t *testing.T) {
	svc, repo := buildTenantServiceFixture(t)
	ctx := context.Background()
	id := uuid.New()

	repo.EXPECT().Delete(ctx, id).Return(nil)

	err := svc.Delete(ctx, id)

	require.NoError(t, err)
}

// TestTenantService_List_DefaultLimitApplied verifies that a non-positive
// limit is replaced with the default of 20 before calling the repository.
func TestTenantService_List_DefaultLimitApplied(t *testing.T) {
	svc, repo := buildTenantServiceFixture(t)
	ctx := context.Background()

	expected := []*domain.Tenant{{ID: uuid.New(), Name: "Acme Corp"}}

	// The service must pass limit=20 when the caller supplies limit=0.
	repo.EXPECT().List(ctx, 20, 0).Return(expected, 1, nil)

	got, total, err := svc.List(ctx, 0, 0)

	require.NoError(t, err)
	assert.Equal(t, 1, total)
	assert.Equal(t, expected, got)
}

// TestTenantService_List_ExplicitLimitPreserved verifies that a positive
// caller-supplied limit is passed through to the repository without alteration.
func TestTenantService_List_ExplicitLimitPreserved(t *testing.T) {
	svc, repo := buildTenantServiceFixture(t)
	ctx := context.Background()

	expected := []*domain.Tenant{{ID: uuid.New(), Name: "Acme Corp"}}

	repo.EXPECT().List(ctx, 5, 10).Return(expected, 1, nil)

	got, total, err := svc.List(ctx, 5, 10)

	require.NoError(t, err)
	assert.Equal(t, 1, total)
	assert.Equal(t, expected, got)
}
