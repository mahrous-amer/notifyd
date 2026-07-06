package quota

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"

	"github.com/bse/notifyd/internal/domain"
)

// Free-plan defaults applied to tenants that have no entitlements row yet
// (e.g. tenants created before billing existed).
var (
	FreeMessageLimit int64 = 1000
	FreeChannels           = []domain.ChannelType{domain.ChannelDiscord, domain.ChannelTelegram}
)

const counterTTL = 45 * 24 * time.Hour // outlives any billing period; reconciled nightly

type Decision struct {
	Allowed bool
	Used    int64
	Limit   int64
}

type Service struct {
	rdb        *redis.Client
	entRepo    domain.EntitlementRepository
	webhookURL string
	httpClient *http.Client
	logger     zerolog.Logger
}

func NewService(rdb *redis.Client, entRepo domain.EntitlementRepository, webhookURL string, httpClient *http.Client, logger zerolog.Logger) *Service {
	return &Service{rdb: rdb, entRepo: entRepo, webhookURL: webhookURL, httpClient: httpClient, logger: logger}
}

// EntitlementsFor returns the tenant's entitlements, falling back to
// Free-plan defaults with a calendar-month period when no row exists.
func (s *Service) EntitlementsFor(ctx context.Context, tenantID uuid.UUID) (*domain.Entitlements, error) {
	ent, err := s.entRepo.GetByTenantID(ctx, tenantID)
	if err == nil {
		return ent, nil
	}
	if !errors.Is(err, domain.ErrNotFound) {
		return nil, err
	}
	now := time.Now().UTC()
	start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	return &domain.Entitlements{
		TenantID:        tenantID,
		PlanCode:        "free",
		MessageLimit:    FreeMessageLimit,
		AllowedChannels: FreeChannels,
		APIKeyLimit:     1,
		RetentionDays:   7,
		PeriodStart:     start,
		PeriodEnd:       start.AddDate(0, 1, 0),
	}, nil
}

func usageKey(tenantID uuid.UUID, periodStart time.Time) string {
	return fmt.Sprintf("usage:%s:%d", tenantID, periodStart.Unix())
}

// Reserve atomically claims n message slots. When the claim would exceed the
// limit it is rolled back and Allowed=false is returned. Crossing the 80% or
// 100% threshold fires the billing webhook once per threshold per period.
func (s *Service) Reserve(ctx context.Context, tenantID uuid.UUID, n int64) (*Decision, error) {
	ent, err := s.EntitlementsFor(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	key := usageKey(tenantID, ent.PeriodStart)

	used, err := s.rdb.IncrBy(ctx, key, n).Result()
	if err != nil {
		return nil, err
	}
	if used == n { // first write for this period
		if err := s.rdb.Expire(ctx, key, counterTTL).Err(); err != nil {
			s.logger.Error().Err(err).Str("tenant", tenantID.String()).Msg("failed to set usage key TTL")
		}
	}

	if used > ent.MessageLimit {
		rolledBack, derr := s.rdb.DecrBy(ctx, key, n).Result()
		if derr != nil {
			s.logger.Error().Err(derr).Str("tenant", tenantID.String()).Msg("quota rollback failed")
			rolledBack = used - n
		}
		s.notifyThreshold(tenantID, ent, rolledBack, 100)
		return &Decision{Allowed: false, Used: rolledBack, Limit: ent.MessageLimit}, nil
	}

	prev := used - n
	if crossed(prev, used, ent.MessageLimit, 80) {
		s.notifyThreshold(tenantID, ent, used, 80)
	}
	if used == ent.MessageLimit {
		s.notifyThreshold(tenantID, ent, used, 100)
	}
	return &Decision{Allowed: true, Used: used, Limit: ent.MessageLimit}, nil
}

func crossed(prev, now, limit int64, pct int64) bool {
	threshold := limit * pct / 100
	return prev < threshold && now >= threshold
}

func (s *Service) notifyThreshold(tenantID uuid.UUID, ent *domain.Entitlements, used int64, pct int) {
	if s.webhookURL == "" {
		return
	}
	// Dedup: only the first crossing per threshold per period fires.
	dedupKey := fmt.Sprintf("quota-alert:%s:%d:%d", tenantID, ent.PeriodStart.Unix(), pct)
	ok, err := s.rdb.SetNX(context.Background(), dedupKey, 1, counterTTL).Result()
	if err != nil || !ok {
		return
	}
	payload, _ := json.Marshal(map[string]any{
		"tenant_id":    tenantID,
		"period_start": ent.PeriodStart,
		"usage":        used,
		"limit":        ent.MessageLimit,
		"threshold":    pct,
	})
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.webhookURL, bytes.NewReader(payload))
		if err != nil {
			return
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := s.httpClient.Do(req)
		if err != nil {
			s.logger.Warn().Err(err).Msg("quota alert webhook failed")
			return
		}
		resp.Body.Close() //nolint:errcheck
	}()
}
