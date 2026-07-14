package service

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/bse/notifyd/internal/domain"
	"github.com/bse/notifyd/internal/provider"
)

// maxWebhookEndpointsPerTenant caps how many status-webhook destinations a
// tenant may configure. A constant for now; the design doc defers making
// this an entitlement to a future increment if a customer asks for more.
const maxWebhookEndpointsPerTenant = 3

// webhookEndpointSecretBytes is the amount of random data hex-encoded into
// the per-endpoint signing secret, matching the API key/secret generation
// in tenant_service.go and apikey_service.go.
const webhookEndpointSecretBytes = 32

type WebhookEndpointService struct {
	repo domain.WebhookEndpointRepository
}

func NewWebhookEndpointService(repo domain.WebhookEndpointRepository) *WebhookEndpointService {
	return &WebhookEndpointService{repo: repo}
}

// Create validates and persists a new webhook endpoint, returning the
// created record alongside the plaintext secret. The plaintext secret is
// never stored or logged anywhere else and this is the only point in the
// endpoint's lifecycle where it is available — callers (the handler) must
// return it to the client immediately and then discard it.
func (s *WebhookEndpointService) Create(ctx context.Context, tenantID uuid.UUID, input domain.CreateWebhookEndpointInput) (*domain.WebhookEndpoint, string, error) {
	if err := validateWebhookEndpointURL(input.URL); err != nil {
		return nil, "", err
	}
	if err := domain.ValidateWebhookEvents(input.Events); err != nil {
		return nil, "", err
	}

	secret, err := generateRandomHex(webhookEndpointSecretBytes)
	if err != nil {
		return nil, "", fmt.Errorf("generate webhook secret: %w", err)
	}

	endpoint := &domain.WebhookEndpoint{
		ID:        uuid.New(),
		TenantID:  tenantID,
		URL:       input.URL,
		Secret:    secret,
		Events:    input.Events,
		IsActive:  true,
		CreatedAt: time.Now(),
	}

	if err := s.repo.CreateWithinLimit(ctx, endpoint, maxWebhookEndpointsPerTenant); err != nil {
		return nil, "", err
	}
	return endpoint, secret, nil
}

func (s *WebhookEndpointService) GetByID(ctx context.Context, id uuid.UUID) (*domain.WebhookEndpoint, error) {
	return s.repo.GetByID(ctx, id)
}

func (s *WebhookEndpointService) ListByTenant(ctx context.Context, tenantID uuid.UUID) ([]*domain.WebhookEndpoint, error) {
	return s.repo.ListByTenant(ctx, tenantID)
}

func (s *WebhookEndpointService) Update(ctx context.Context, id, tenantID uuid.UUID, input domain.UpdateWebhookEndpointInput) (*domain.WebhookEndpoint, error) {
	if input.URL != nil {
		if err := validateWebhookEndpointURL(*input.URL); err != nil {
			return nil, err
		}
	}
	if input.Events != nil {
		if err := domain.ValidateWebhookEvents(input.Events); err != nil {
			return nil, err
		}
	}
	return s.repo.Update(ctx, id, tenantID, input)
}

func (s *WebhookEndpointService) Delete(ctx context.Context, id, tenantID uuid.UUID) error {
	return s.repo.Delete(ctx, id, tenantID)
}

// validateWebhookEndpointURL wraps provider.ValidateHTTPSDestinationURL,
// translating its plain error into domain.ErrValidationFailed so handlers
// can classify it the same way as every other input validation failure.
func validateWebhookEndpointURL(rawURL string) error {
	if err := provider.ValidateHTTPSDestinationURL(rawURL); err != nil {
		return fmt.Errorf("%w: %w", domain.ErrValidationFailed, err)
	}
	return nil
}
