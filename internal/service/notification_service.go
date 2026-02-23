package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"golang.org/x/sync/errgroup"

	"github.com/bse/notifyd/internal/domain"
	ntask "github.com/bse/notifyd/internal/worker"
)

type NotificationService struct {
	notifRepo   domain.NotificationRepository
	channelRepo domain.ChannelConfigRepository
	asynqClient *asynq.Client
	maxRetries  int
}

func NewNotificationService(
	notifRepo domain.NotificationRepository,
	channelRepo domain.ChannelConfigRepository,
	asynqClient *asynq.Client,
	maxRetries int,
) *NotificationService {
	return &NotificationService{
		notifRepo:   notifRepo,
		channelRepo: channelRepo,
		asynqClient: asynqClient,
		maxRetries:  maxRetries,
	}
}

func (s *NotificationService) Send(ctx context.Context, tenantID uuid.UUID, input domain.SendNotificationInput) (*domain.Notification, error) {
	channelCfg, err := s.channelRepo.GetByID(ctx, input.ChannelConfigID)
	if err != nil {
		return nil, fmt.Errorf("%w: channel config not found: %w", domain.ErrNotFound, err)
	}
	if channelCfg.TenantID != tenantID {
		return nil, fmt.Errorf("%w: channel config does not belong to this tenant", domain.ErrValidationFailed)
	}
	if !channelCfg.IsActive {
		return nil, fmt.Errorf("%w: channel config is disabled", domain.ErrValidationFailed)
	}

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
		MaxRetries:      s.maxRetries,
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
	}, asynq.MaxRetry(s.maxRetries))
	if err != nil {
		return nil, fmt.Errorf("create task: %w", err)
	}

	info, err := s.asynqClient.EnqueueContext(ctx, task)
	if err != nil {
		return nil, fmt.Errorf("enqueue task: %w", err)
	}

	if err := s.notifRepo.SetAsynqTaskID(ctx, notif.ID, info.ID); err != nil {
		// Non-fatal: notification is enqueued, but asynq_task_id won't be set for observability.
		// The notification will still be delivered; we just lose task correlation.
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
	g.Wait()
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
