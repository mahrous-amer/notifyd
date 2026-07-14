package service

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/rs/zerolog"
	"golang.org/x/sync/errgroup"

	"github.com/bse/notifyd/internal/domain"
	ntask "github.com/bse/notifyd/internal/worker"
)

type NotificationService struct {
	notifRepo   domain.NotificationRepository
	channelRepo domain.ChannelConfigRepository
	entRepo     domain.EntitlementRepository
	asynqClient *asynq.Client
	maxRetries  int
	logger      zerolog.Logger
}

func NewNotificationService(
	notifRepo domain.NotificationRepository,
	channelRepo domain.ChannelConfigRepository,
	entRepo domain.EntitlementRepository,
	asynqClient *asynq.Client,
	maxRetries int,
	logger zerolog.Logger,
) *NotificationService {
	return &NotificationService{
		notifRepo:   notifRepo,
		channelRepo: channelRepo,
		entRepo:     entRepo,
		asynqClient: asynqClient,
		maxRetries:  maxRetries,
		logger:      logger,
	}
}

func (s *NotificationService) Send(ctx context.Context, tenantID uuid.UUID, input domain.SendNotificationInput) (*domain.Notification, error) {
	channelCfg, err := s.channelRepo.GetByID(ctx, input.ChannelConfigID)
	if err != nil {
		return nil, fmt.Errorf("get channel config: %w", err)
	}
	if channelCfg.TenantID != tenantID {
		return nil, fmt.Errorf("%w: channel config does not belong to this tenant", domain.ErrValidationFailed)
	}
	if !channelCfg.IsActive {
		return nil, fmt.Errorf("%w: channel config is disabled", domain.ErrValidationFailed)
	}
	if err := validateSubjectForChannel(channelCfg.Channel, input.Subject); err != nil {
		return nil, err
	}

	ent, err := domain.EntitlementsOrFree(ctx, s.entRepo, tenantID)
	if err != nil {
		return nil, err
	}
	if !ent.AllowsChannel(channelCfg.Channel) {
		return nil, fmt.Errorf("%w: %s", domain.ErrChannelNotInPlan, channelCfg.Channel)
	}

	maxRetries := s.effectiveMaxRetries(channelCfg)

	now := time.Now()
	notif := &domain.Notification{
		ID:              uuid.New(),
		TenantID:        tenantID,
		ChannelConfigID: channelCfg.ID,
		Channel:         channelCfg.Channel,
		Subject:         input.Subject,
		Body:            input.Body,
		Metadata:        input.Metadata,
		Status:          domain.StatusPending,
		MaxRetries:      maxRetries,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := s.notifRepo.Create(ctx, notif); err != nil {
		return nil, fmt.Errorf("create notification: %w", err)
	}

	subject := ""
	if notif.Subject != nil {
		subject = *notif.Subject
	}

	task, err := ntask.NewNotificationDeliverTask(ntask.NotificationDeliverPayload{
		NotificationID:  notif.ID,
		TenantID:        tenantID,
		ChannelType:     string(channelCfg.Channel),
		ChannelConfigID: channelCfg.ID,
		Subject:         subject,
		Body:            notif.Body,
		Metadata:        notif.Metadata,
		DeliveryPrefs:   channelCfg.DeliveryPrefs,
	}, asynq.MaxRetry(maxRetries))
	if err != nil {
		return nil, fmt.Errorf("create task: %w", err)
	}

	info, err := s.asynqClient.EnqueueContext(ctx, task)
	if err != nil {
		return nil, fmt.Errorf("enqueue task: %w", err)
	}

	if err := s.notifRepo.SetAsynqTaskID(ctx, notif.ID, info.ID); err != nil {
		// Non-fatal: the notification is enqueued and will be delivered.
		// We only lose the asynq task ID correlation for observability.
		s.logger.Warn().
			Err(err).
			Str("notification_id", notif.ID.String()).
			Msg("failed to store asynq task ID; notification is still enqueued")
	}

	return notif, nil
}

func (s *NotificationService) SendMulti(ctx context.Context, tenantID uuid.UUID, input domain.SendMultiInput) ([]*domain.Notification, []error) {
	var (
		mu      sync.Mutex
		results = make([]*domain.Notification, 0, len(input.Channels))
		errs    []error
	)

	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(5)

	for _, ch := range input.Channels {
		g.Go(func() error {
			notif, err := s.Send(ctx, tenantID, ch)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs = append(errs, err)
			} else {
				results = append(results, notif)
			}
			return nil
		})
	}
	_ = g.Wait()
	return results, errs
}

func (s *NotificationService) GetByID(ctx context.Context, id uuid.UUID) (*domain.Notification, error) {
	return s.notifRepo.GetByID(ctx, id)
}

func (s *NotificationService) List(ctx context.Context, filter domain.NotificationFilter) ([]*domain.Notification, int, error) {
	if filter.Limit <= 0 {
		filter.Limit = 20
	}
	return s.notifRepo.List(ctx, filter)
}

// CountByStatus returns the total count of notifications for each status
// across all tenants. It delegates to a single aggregation query in the
// repository rather than issuing per-tenant queries.
func (s *NotificationService) CountByStatus(ctx context.Context) (map[domain.NotificationStatus]int, error) {
	return s.notifRepo.CountByStatus(ctx)
}

// effectiveMaxRetries returns the max retry count to use for a notification,
// giving precedence to the channel config's delivery preferences over the
// service-level default.
func (s *NotificationService) effectiveMaxRetries(cfg *domain.ChannelConfig) int {
	if cfg.DeliveryPrefs != nil && cfg.DeliveryPrefs.MaxRetries != nil {
		return *cfg.DeliveryPrefs.MaxRetries
	}
	return s.maxRetries
}

// validateSubjectForChannel enforces channel-specific subject requirements.
// Email has no reasonable default subject line the way chat channels do
// (Discord/Telegram/WhatsApp messages read fine without one), so a missing or
// blank subject is rejected here rather than silently sent as "(no subject)".
func validateSubjectForChannel(channel domain.ChannelType, subject *string) error {
	if channel != domain.ChannelEmail {
		return nil
	}
	if subject == nil || strings.TrimSpace(*subject) == "" {
		return fmt.Errorf("%w: subject is required for email notifications", domain.ErrValidationFailed)
	}
	return nil
}
