package service

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/bse/notifyd/internal/domain"
	"github.com/bse/notifyd/internal/provider"
)

type ChannelService struct {
	repo     domain.ChannelConfigRepository
	entRepo  domain.EntitlementRepository
	registry *provider.Registry
	logger   zerolog.Logger
}

func NewChannelService(repo domain.ChannelConfigRepository, entRepo domain.EntitlementRepository, registry *provider.Registry, logger zerolog.Logger) *ChannelService {
	return &ChannelService{repo: repo, entRepo: entRepo, registry: registry, logger: logger}
}

func (s *ChannelService) Create(ctx context.Context, tenantID uuid.UUID, input domain.CreateChannelConfigInput) (*domain.ChannelConfig, error) {
	if !domain.IsValidChannelType(input.Channel) {
		return nil, fmt.Errorf("%w: invalid channel type: %s", domain.ErrValidationFailed, input.Channel)
	}
	if input.Name == "" {
		return nil, fmt.Errorf("%w: name is required", domain.ErrValidationFailed)
	}

	ent, err := domain.EntitlementsOrFree(ctx, s.entRepo, tenantID)
	if err != nil {
		return nil, err
	}
	if !ent.AllowsChannel(input.Channel) {
		return nil, fmt.Errorf("%w: %s", domain.ErrChannelNotInPlan, input.Channel)
	}

	prov, err := s.registry.Get(string(input.Channel))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", domain.ErrUnsupportedChannel, err)
	}
	if err := prov.ValidateConfig(input.Config); err != nil {
		return nil, fmt.Errorf("%w: invalid channel config: %w", domain.ErrValidationFailed, err)
	}
	if err := input.DeliveryPrefs.Validate(); err != nil {
		return nil, err
	}

	now := time.Now()
	cfg := &domain.ChannelConfig{
		ID:            uuid.New(),
		TenantID:      tenantID,
		Channel:       input.Channel,
		Name:          input.Name,
		Config:        input.Config,
		IsActive:      true,
		DeliveryPrefs: input.DeliveryPrefs,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	if err := s.repo.Create(ctx, cfg); err != nil {
		return nil, err
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
			return nil, fmt.Errorf("%w: %w", domain.ErrUnsupportedChannel, err)
		}
		if err := prov.ValidateConfig(*input.Config); err != nil {
			return nil, fmt.Errorf("%w: invalid channel config: %w", domain.ErrValidationFailed, err)
		}
	}
	if err := input.DeliveryPrefs.Validate(); err != nil {
		return nil, err
	}
	return s.repo.Update(ctx, id, tenantID, input)
}

func (s *ChannelService) Delete(ctx context.Context, id uuid.UUID, tenantID uuid.UUID) error {
	return s.repo.Delete(ctx, id, tenantID)
}
