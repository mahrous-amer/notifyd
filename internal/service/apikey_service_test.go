package service_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"github.com/bse/notifyd/internal/domain"
	"github.com/bse/notifyd/internal/service"
)

// fakeAPIKeyRepo is a hand-rolled test double for domain.APIKeyRepository.
// It stores keys in memory and enforces limits in CreateWithinLimit so the
// service's plan-limit path can be exercised without a database.
type fakeAPIKeyRepo struct {
	keys []*domain.APIKey
}

func (f *fakeAPIKeyRepo) Create(_ context.Context, k *domain.APIKey) error {
	f.keys = append(f.keys, k)
	return nil
}

func (f *fakeAPIKeyRepo) CreateWithinLimit(_ context.Context, k *domain.APIKey, limit int) error {
	active := 0
	for _, existing := range f.keys {
		if existing.RevokedAt == nil {
			active++
		}
	}
	if active >= limit {
		return domain.ErrKeyLimitReached
	}
	f.keys = append(f.keys, k)
	return nil
}

func (f *fakeAPIKeyRepo) GetByAPIKey(_ context.Context, key string) (*domain.APIKey, error) {
	for _, k := range f.keys {
		if k.APIKey == key {
			return k, nil
		}
	}
	return nil, domain.ErrNotFound
}

func (f *fakeAPIKeyRepo) ListByTenant(_ context.Context, _ uuid.UUID) ([]*domain.APIKey, error) {
	return f.keys, nil
}

func (f *fakeAPIKeyRepo) Revoke(_ context.Context, _, _ uuid.UUID) error { return nil }

func TestAPIKeyService_Create_ReturnsRawSecretOnce(t *testing.T) {
	repo := &fakeAPIKeyRepo{}
	svc := service.NewAPIKeyService(repo, &fakeEntitlementRepo{})

	key, rawSecret, err := svc.Create(context.Background(), uuid.New(), "ci")
	require.NoError(t, err)
	assert.NotEmpty(t, rawSecret)
	assert.Len(t, key.APIKey, 64, "32 random bytes hex-encoded")
	assert.NoError(t, bcrypt.CompareHashAndPassword([]byte(key.APISecretHash), []byte(rawSecret)))
}

func TestAPIKeyService_Create_EnforcesPlanLimit(t *testing.T) {
	// Seed one key so the Free-plan limit of 1 is already reached.
	repo := &fakeAPIKeyRepo{keys: []*domain.APIKey{{ID: uuid.New()}}}
	svc := service.NewAPIKeyService(repo, &fakeEntitlementRepo{})

	_, _, err := svc.Create(context.Background(), uuid.New(), "second")
	assert.ErrorIs(t, err, domain.ErrKeyLimitReached)
}
