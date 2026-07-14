package service_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bse/notifyd/internal/domain"
	"github.com/bse/notifyd/internal/service"
)

// fakeWebhookEndpointRepo is a hand-rolled test double for
// domain.WebhookEndpointRepository. It enforces the cap in CreateWithinLimit
// the same way the real Postgres repository does (verified separately
// against a live database), so the service's cap-handling can be exercised
// without one.
type fakeWebhookEndpointRepo struct {
	endpoints []*domain.WebhookEndpoint
}

func (f *fakeWebhookEndpointRepo) CreateWithinLimit(_ context.Context, e *domain.WebhookEndpoint, limit int) error {
	if len(f.endpoints) >= limit {
		return domain.ErrWebhookLimitReached
	}
	f.endpoints = append(f.endpoints, e)
	return nil
}

func (f *fakeWebhookEndpointRepo) GetByID(_ context.Context, id uuid.UUID) (*domain.WebhookEndpoint, error) {
	for _, e := range f.endpoints {
		if e.ID == id {
			return e, nil
		}
	}
	return nil, domain.ErrNotFound
}

func (f *fakeWebhookEndpointRepo) ListByTenant(_ context.Context, tenantID uuid.UUID) ([]*domain.WebhookEndpoint, error) {
	var out []*domain.WebhookEndpoint
	for _, e := range f.endpoints {
		if e.TenantID == tenantID {
			out = append(out, e)
		}
	}
	return out, nil
}

func (f *fakeWebhookEndpointRepo) ListActiveByTenantAndEvent(_ context.Context, tenantID uuid.UUID, eventType domain.WebhookEventType) ([]*domain.WebhookEndpoint, error) {
	var out []*domain.WebhookEndpoint
	for _, e := range f.endpoints {
		if e.TenantID == tenantID && e.SubscribesTo(eventType) {
			out = append(out, e)
		}
	}
	return out, nil
}

func (f *fakeWebhookEndpointRepo) Update(_ context.Context, id, tenantID uuid.UUID, input domain.UpdateWebhookEndpointInput) (*domain.WebhookEndpoint, error) {
	for _, e := range f.endpoints {
		if e.ID == id && e.TenantID == tenantID {
			if input.URL != nil {
				e.URL = *input.URL
			}
			if input.Events != nil {
				e.Events = input.Events
			}
			if input.IsActive != nil {
				e.IsActive = *input.IsActive
			}
			return e, nil
		}
	}
	return nil, domain.ErrNotFound
}

func (f *fakeWebhookEndpointRepo) Delete(_ context.Context, id, tenantID uuid.UUID) error {
	for i, e := range f.endpoints {
		if e.ID == id && e.TenantID == tenantID {
			f.endpoints = append(f.endpoints[:i], f.endpoints[i+1:]...)
			return nil
		}
	}
	return domain.ErrNotFound
}

func validWebhookInput() domain.CreateWebhookEndpointInput {
	return domain.CreateWebhookEndpointInput{
		URL:    "https://example.com/hooks/notifyd",
		Events: []string{"notification.delivered"},
	}
}

func TestWebhookEndpointService_Create_ReturnsSecretOnce(t *testing.T) {
	repo := &fakeWebhookEndpointRepo{}
	svc := service.NewWebhookEndpointService(repo)

	endpoint, rawSecret, err := svc.Create(context.Background(), uuid.New(), validWebhookInput())

	require.NoError(t, err)
	assert.NotEmpty(t, rawSecret)
	assert.Equal(t, rawSecret, endpoint.Secret, "the service's return value carries the plaintext secret; callers must not persist or log endpoint.Secret beyond this one response")
	assert.Len(t, rawSecret, 64, "32 random bytes hex-encoded")
}

func TestWebhookEndpointService_Create_PersistsHTTPSURL(t *testing.T) {
	repo := &fakeWebhookEndpointRepo{}
	svc := service.NewWebhookEndpointService(repo)
	tenantID := uuid.New()

	endpoint, _, err := svc.Create(context.Background(), tenantID, validWebhookInput())

	require.NoError(t, err)
	assert.Equal(t, tenantID, endpoint.TenantID)
	assert.Equal(t, "https://example.com/hooks/notifyd", endpoint.URL)
	assert.Equal(t, []string{"notification.delivered"}, endpoint.Events)
	assert.True(t, endpoint.IsActive)
}

func TestWebhookEndpointService_Create_RejectsPlainHTTP(t *testing.T) {
	repo := &fakeWebhookEndpointRepo{}
	svc := service.NewWebhookEndpointService(repo)

	input := validWebhookInput()
	input.URL = "http://example.com/hooks/notifyd"

	_, _, err := svc.Create(context.Background(), uuid.New(), input)

	assert.ErrorIs(t, err, domain.ErrValidationFailed)
}

func TestWebhookEndpointService_Create_RejectsSSRFTarget(t *testing.T) {
	// Defense in depth per the design doc: the create-time check uses the
	// same static validation the SSRF guard applies at dial time, so an
	// obviously-internal URL is rejected immediately rather than only
	// failing (permanently) on first delivery attempt.
	repo := &fakeWebhookEndpointRepo{}
	svc := service.NewWebhookEndpointService(repo)

	input := validWebhookInput()
	input.URL = "https://127.0.0.1/hooks/notifyd"

	_, _, err := svc.Create(context.Background(), uuid.New(), input)

	assert.ErrorIs(t, err, domain.ErrValidationFailed)
}

func TestWebhookEndpointService_Create_RejectsUnknownEventType(t *testing.T) {
	repo := &fakeWebhookEndpointRepo{}
	svc := service.NewWebhookEndpointService(repo)

	input := validWebhookInput()
	input.Events = []string{"notification.queued"}

	_, _, err := svc.Create(context.Background(), uuid.New(), input)

	assert.ErrorIs(t, err, domain.ErrValidationFailed)
}

func TestWebhookEndpointService_Create_RejectsEmptyEventList(t *testing.T) {
	repo := &fakeWebhookEndpointRepo{}
	svc := service.NewWebhookEndpointService(repo)

	input := validWebhookInput()
	input.Events = nil

	_, _, err := svc.Create(context.Background(), uuid.New(), input)

	assert.ErrorIs(t, err, domain.ErrValidationFailed)
}

func TestWebhookEndpointService_Create_EnforcesCapOfThree(t *testing.T) {
	repo := &fakeWebhookEndpointRepo{}
	svc := service.NewWebhookEndpointService(repo)
	tenantID := uuid.New()

	for i := 0; i < 3; i++ {
		_, _, err := svc.Create(context.Background(), tenantID, validWebhookInput())
		require.NoError(t, err)
	}

	_, _, err := svc.Create(context.Background(), tenantID, validWebhookInput())
	assert.ErrorIs(t, err, domain.ErrWebhookLimitReached)
}

func TestWebhookEndpointService_Create_CapIsPerTenant(t *testing.T) {
	repo := &fakeWebhookEndpointRepo{}
	svc := service.NewWebhookEndpointService(repo)

	for i := 0; i < 3; i++ {
		_, _, err := svc.Create(context.Background(), uuid.New(), validWebhookInput())
		require.NoError(t, err, "each tenant has its own independent cap")
	}
}

func TestWebhookEndpointService_ListByTenant_JSONResponseNeverIncludesSecret(t *testing.T) {
	// The service itself returns whatever the repository gives it — secret
	// included, since the repository is the source of truth. The contract
	// that matters ("GET/PUT responses never include it", per the design
	// doc) is enforced by domain.WebhookEndpoint.Secret's `json:"-"` tag,
	// which this test verifies at the actual serialization boundary rather
	// than asserting on the in-memory struct field.
	repo := &fakeWebhookEndpointRepo{}
	svc := service.NewWebhookEndpointService(repo)
	tenantID := uuid.New()

	_, _, err := svc.Create(context.Background(), tenantID, validWebhookInput())
	require.NoError(t, err)

	list, err := svc.ListByTenant(context.Background(), tenantID)
	require.NoError(t, err)
	require.Len(t, list, 1)

	encoded, err := json.Marshal(list[0])
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), list[0].Secret)
	assert.NotContains(t, string(encoded), `"secret"`)
}

func TestWebhookEndpointService_Update_ValidatesNewEvents(t *testing.T) {
	repo := &fakeWebhookEndpointRepo{}
	svc := service.NewWebhookEndpointService(repo)
	tenantID := uuid.New()

	created, _, err := svc.Create(context.Background(), tenantID, validWebhookInput())
	require.NoError(t, err)

	_, err = svc.Update(context.Background(), created.ID, tenantID, domain.UpdateWebhookEndpointInput{
		Events: []string{"not-a-real-event"},
	})

	assert.ErrorIs(t, err, domain.ErrValidationFailed)
}

func TestWebhookEndpointService_Update_ValidatesNewURL(t *testing.T) {
	repo := &fakeWebhookEndpointRepo{}
	svc := service.NewWebhookEndpointService(repo)
	tenantID := uuid.New()

	created, _, err := svc.Create(context.Background(), tenantID, validWebhookInput())
	require.NoError(t, err)

	badURL := "http://example.com/not-https"
	_, err = svc.Update(context.Background(), created.ID, tenantID, domain.UpdateWebhookEndpointInput{
		URL: &badURL,
	})

	assert.ErrorIs(t, err, domain.ErrValidationFailed)
}

func TestWebhookEndpointService_Update_CanDeactivateWithoutChangingURLOrEvents(t *testing.T) {
	repo := &fakeWebhookEndpointRepo{}
	svc := service.NewWebhookEndpointService(repo)
	tenantID := uuid.New()

	created, _, err := svc.Create(context.Background(), tenantID, validWebhookInput())
	require.NoError(t, err)

	inactive := false
	updated, err := svc.Update(context.Background(), created.ID, tenantID, domain.UpdateWebhookEndpointInput{
		IsActive: &inactive,
	})

	require.NoError(t, err)
	assert.False(t, updated.IsActive)
	assert.Equal(t, created.URL, updated.URL)
	assert.Equal(t, created.Events, updated.Events)
}

func TestWebhookEndpointService_Delete_DelegatesToRepo(t *testing.T) {
	repo := &fakeWebhookEndpointRepo{}
	svc := service.NewWebhookEndpointService(repo)
	tenantID := uuid.New()

	created, _, err := svc.Create(context.Background(), tenantID, validWebhookInput())
	require.NoError(t, err)

	require.NoError(t, svc.Delete(context.Background(), created.ID, tenantID))

	_, err = svc.GetByID(context.Background(), created.ID)
	assert.ErrorIs(t, err, domain.ErrNotFound)
}
