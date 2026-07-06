package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/bse/notifyd/internal/domain"
)

type TenantService struct {
	repo    domain.TenantRepository
	keyRepo domain.APIKeyRepository
}

func NewTenantService(repo domain.TenantRepository, keyRepo domain.APIKeyRepository) *TenantService {
	return &TenantService{repo: repo, keyRepo: keyRepo}
}

type CreateTenantResult struct {
	Tenant    *domain.Tenant `json:"tenant"`
	APIKey    string         `json:"api_key"`
	APISecret string         `json:"api_secret"`
}

func (s *TenantService) Create(ctx context.Context, input domain.CreateTenantInput) (*CreateTenantResult, error) {
	if input.Name == "" {
		return nil, fmt.Errorf("%w: name is required", domain.ErrValidationFailed)
	}
	if input.Slug == "" {
		return nil, fmt.Errorf("%w: slug is required", domain.ErrValidationFailed)
	}

	apiKey, err := generateRandomHex(32)
	if err != nil {
		return nil, fmt.Errorf("generate api key: %w", err)
	}
	rawSecret, err := generateRandomHex(32)
	if err != nil {
		return nil, fmt.Errorf("generate api secret: %w", err)
	}
	hashedSecret, err := bcrypt.GenerateFromPassword([]byte(rawSecret), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("hash api secret: %w", err)
	}

	now := time.Now()
	tenant := &domain.Tenant{
		ID:        uuid.New(),
		Name:      input.Name,
		Slug:      input.Slug,
		APIKey:    apiKey,
		APISecret: string(hashedSecret),
		IsActive:  true,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := s.repo.Create(ctx, tenant); err != nil {
		return nil, err
	}

	// Write the tenant's initial key row so auth lookup works via api_keys.
	// The tenants.api_key / api_secret columns keep the same values for
	// backward compatibility; auth no longer reads from there.
	if err := s.keyRepo.Create(ctx, &domain.APIKey{
		ID:            uuid.New(),
		TenantID:      tenant.ID,
		APIKey:        apiKey,
		APISecretHash: string(hashedSecret),
		Label:         "default",
		CreatedAt:     now,
	}); err != nil {
		return nil, err
	}

	return &CreateTenantResult{
		Tenant:    tenant,
		APIKey:    apiKey,
		APISecret: rawSecret,
	}, nil
}

func (s *TenantService) GetByID(ctx context.Context, id uuid.UUID) (*domain.Tenant, error) {
	return s.repo.GetByID(ctx, id)
}

func (s *TenantService) Update(ctx context.Context, id uuid.UUID, input domain.UpdateTenantInput) (*domain.Tenant, error) {
	return s.repo.Update(ctx, id, input)
}

func (s *TenantService) Delete(ctx context.Context, id uuid.UUID) error {
	return s.repo.Delete(ctx, id)
}

func (s *TenantService) List(ctx context.Context, limit, offset int) ([]*domain.Tenant, int, error) {
	if limit <= 0 {
		limit = 20
	}
	return s.repo.List(ctx, limit, offset)
}

func generateRandomHex(bytes int) (string, error) {
	b := make([]byte, bytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
