package main

import (
	"context"
	"math/rand/v2"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"

	"github.com/bse/notifyd/internal/config"
	"github.com/bse/notifyd/internal/domain"
	"github.com/bse/notifyd/internal/provider"
	"github.com/bse/notifyd/internal/repository"
	"github.com/bse/notifyd/internal/worker"
)

func main() {
	logger := zerolog.New(os.Stdout).With().Timestamp().Logger()

	cfg, err := config.Load()
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to load config")
	}

	poolCfg, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to parse database config")
	}
	poolCfg.MaxConns = cfg.DBMaxConns
	poolCfg.MinConns = cfg.DBMinConns
	poolCfg.MaxConnLifetime = cfg.DBMaxConnLifetime
	poolCfg.MaxConnIdleTime = cfg.DBMaxConnIdleTime
	poolCfg.HealthCheckPeriod = cfg.DBHealthCheckPeriod
	dbPool, err := pgxpool.NewWithConfig(context.Background(), poolCfg)
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to connect to database")
	}
	defer dbPool.Close()

	if err := dbPool.Ping(context.Background()); err != nil {
		logger.Fatal().Err(err).Msg("failed to ping database")
	}
	logger.Info().Msg("connected to PostgreSQL")

	var notifRepo domain.NotificationRepository = repository.NewPgNotificationRepo(dbPool)
	var attemptRepo domain.DeliveryAttemptRepository = repository.NewPgDeliveryAttemptRepo(dbPool)
	var channelRepo domain.ChannelConfigRepository = repository.NewPgChannelConfigRepo(dbPool)
	var metricRepo domain.DeliveryMetricRepository = repository.NewPgDeliveryMetricRepo(dbPool)

	tenantRepo := repository.NewPgTenantRepo(dbPool)
	entRepo := repository.NewPgEntitlementRepo(dbPool)
	redisCli := redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	})
	defer redisCli.Close() //nolint:errcheck
	maintenance := worker.NewMaintenanceHandler(tenantRepo, entRepo, notifRepo, notifRepo, redisCli, logger)

	httpTransport := &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 20,
		MaxConnsPerHost:     50,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
	}
	httpClient := &http.Client{Timeout: 30 * time.Second, Transport: httpTransport}
	registry := provider.NewRegistry()
	registry.Register(provider.NewDiscordProvider(httpClient))
	registry.Register(provider.NewTelegramProvider(httpClient))
	registry.Register(provider.NewWhatsAppProvider(httpClient))
	registry.Register(provider.NewEmailProvider())
	registry.Register(provider.NewSlackProvider(httpClient))

	dispatcher := worker.NewDispatcher(registry, notifRepo, attemptRepo, channelRepo, metricRepo, logger)

	retryDelay := func(n int, err error, t *asynq.Task) time.Duration {
		base := cfg.MinRetryDelay * time.Duration(1<<uint(n))
		if base > cfg.MaxRetryDelay {
			base = cfg.MaxRetryDelay
		}
		jitter := time.Duration(rand.Int64N(int64(base) / 5))
		return base + jitter
	}

	srv := asynq.NewServer(
		asynq.RedisClientOpt{
			Addr:     cfg.RedisAddr,
			Password: cfg.RedisPassword,
			DB:       cfg.RedisDB,
		},
		asynq.Config{
			Concurrency:     cfg.WorkerConcurrency,
			ShutdownTimeout: cfg.ShutdownTimeout,
			Queues: map[string]int{
				"critical":      6,
				"notifications": 3,
				"low":           1,
			},
			RetryDelayFunc: retryDelay,
			ErrorHandler: asynq.ErrorHandlerFunc(
				worker.NewNotifyErrorHandler(notifRepo, logger).HandleError,
			),
		},
	)

	mux := asynq.NewServeMux()
	mux.HandleFunc(worker.TypeNotificationDeliver, dispatcher.HandleNotificationDeliver)
	mux.HandleFunc(worker.TypeRetentionPurge, maintenance.HandleRetentionPurge)
	mux.HandleFunc(worker.TypeUsageReconcile, maintenance.HandleUsageReconcile)

	scheduler := asynq.NewScheduler(
		asynq.RedisClientOpt{Addr: cfg.RedisAddr, Password: cfg.RedisPassword, DB: cfg.RedisDB},
		&asynq.SchedulerOpts{Location: time.UTC},
	)
	if _, err := scheduler.Register("0 3 * * *", asynq.NewTask(worker.TypeRetentionPurge, nil)); err != nil {
		logger.Fatal().Err(err).Msg("failed to register retention purge schedule")
	}
	if _, err := scheduler.Register("30 3 * * *", asynq.NewTask(worker.TypeUsageReconcile, nil)); err != nil {
		logger.Fatal().Err(err).Msg("failed to register usage reconcile schedule")
	}
	go func() {
		if err := scheduler.Run(); err != nil {
			logger.Error().Err(err).Msg("scheduler stopped")
		}
	}()

	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
		<-sigCh
		srv.Shutdown()
	}()

	logger.Info().Int("concurrency", cfg.WorkerConcurrency).Msg("worker starting")
	if err := srv.Run(mux); err != nil {
		logger.Fatal().Err(err).Msg("failed to run worker")
	}
}
