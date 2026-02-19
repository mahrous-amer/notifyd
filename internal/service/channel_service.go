package service

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/bse/notifyd/internal/domain"
	"github.com/bse/notifyd/internal/provider"
)

type ChannelService struct {
	repo     domain.ChannelConfigRepository
	registry *provider.Registry
}

func NewChannelService(repo domain.ChannelConfigRepository, registry *provider.Registry) *ChannelService {
	return &ChannelService{repo: repo, registry: registry}
}

func (s *ChannelService) Create(ctx context.Context, tenantID uuid.UUID, input domain.CreateChannelConfigInput) (*domain.ChannelConfig, error) {
	if !domain.IsValidChannelType(input.Channel) {
		return nil, fmt.Errorf("invalid channel type: %s", input.Channel)
	}
	if input.Name == "" {
		return nil, fmt.Errorf("name is required")
	}

	prov, err := s.registry.Get(string(input.Channel))
	if err != nil {
		return nil, fmt.Errorf("unsupported channel: %w", err)
	}
	if err := prov.ValidateConfig(input.Config); err != nil {
		return nil, fmt.Errorf("invalid channel config: %w", err)
	}

	now := time.Now()
	cfg := &domain.ChannelConfig{
		ID:        uuid.New(),
		TenantID:  tenantID,
		Channel:   input.Channel,
		Name:      input.Name,
		Config:    input.Config,
		IsActive:  true,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := s.repo.Create(ctx, cfg); err != nil {
		return nil, fmt.Errorf("create channel config: %w", err)
	}
	return cfg, nil
}

func (s *ChannelService) GetByID(ctx context.Context, id uuid.UUID) (*domain.ChannelConfig, error) {
	return s.repo.GetByID(ctx, id)
}

func (s *ChannelService) ListByTenant(ctx context.Context, tenantID uuid.UUID) ([]*domain.ChannelConfig, error) {
	return s.repo.ListByTenant(ctx, tenantID)
}

func (s *ChannelService) Update(ctx context.Context, id uuid.UUID, tenantID uuid.UUID, input domain.UpdateChannelConfigInput) (*domain.ChannelConfig, error) {
	if input.Config != nil {
		existing, err := s.repo.GetByID(ctx, id)
		if err != nil {
			return nil, err
		}
		prov, err := s.registry.Get(string(existing.Channel))
		if err != nil {
			return nil, fmt.Errorf("unsupported channel: %w", err)
		}
		if err := prov.ValidateConfig(*input.Config); err != nil {
			return nil, fmt.Errorf("invalid channel config: %w", err)
		}
	}
	return s.repo.Update(ctx, id, tenantID, input)
}

func (s *ChannelService) Delete(ctx context.Context, id uuid.UUID, tenantID uuid.UUID) error {
	return s.repo.Delete(ctx, id, tenantID)
}
