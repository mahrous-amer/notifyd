package service

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/bse/notifyd/internal/domain"
)

type APIKeyService struct {
	keyRepo domain.APIKeyRepository
	entRepo domain.EntitlementRepository
}

func NewAPIKeyService(keyRepo domain.APIKeyRepository, entRepo domain.EntitlementRepository) *APIKeyService {
	return &APIKeyService{keyRepo: keyRepo, entRepo: entRepo}
}

// Create mints a new key pair. The raw secret is returned once and never stored.
func (s *APIKeyService) Create(ctx context.Context, tenantID uuid.UUID, label string) (*domain.APIKey, string, error) {
	ent, err := domain.EntitlementsOrFree(ctx, s.entRepo, tenantID)
	if err != nil {
		return nil, "", err
	}

	active, err := s.keyRepo.CountActiveByTenant(ctx, tenantID)
	if err != nil {
		return nil, "", err
	}
	if active >= ent.APIKeyLimit {
		return nil, "", fmt.Errorf("%w: plan allows %d", domain.ErrKeyLimitReached, ent.APIKeyLimit)
	}

	apiKey, err := generateRandomHex(32)
	if err != nil {
		return nil, "", err
	}
	rawSecret, err := generateRandomHex(32)
	if err != nil {
		return nil, "", err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(rawSecret), bcrypt.DefaultCost)
	if err != nil {
		return nil, "", err
	}

	if label == "" {
		label = "default"
	}

	k := &domain.APIKey{
		ID:            uuid.New(),
		TenantID:      tenantID,
		APIKey:        apiKey,
		APISecretHash: string(hash),
		Label:         label,
		CreatedAt:     time.Now(),
	}
	if err := s.keyRepo.Create(ctx, k); err != nil {
		return nil, "", err
	}
	return k, rawSecret, nil
}

func (s *APIKeyService) ListByTenant(ctx context.Context, tenantID uuid.UUID) ([]*domain.APIKey, error) {
	return s.keyRepo.ListByTenant(ctx, tenantID)
}

func (s *APIKeyService) Revoke(ctx context.Context, id, tenantID uuid.UUID) error {
	return s.keyRepo.Revoke(ctx, id, tenantID)
}
