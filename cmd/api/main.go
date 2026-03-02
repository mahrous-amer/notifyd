package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"

	"github.com/bse/notifyd/internal/auth"
	"github.com/bse/notifyd/internal/config"
	"github.com/bse/notifyd/internal/handler"
	"github.com/bse/notifyd/internal/provider"
	"github.com/bse/notifyd/internal/repository"
	"github.com/bse/notifyd/internal/router"
	"github.com/bse/notifyd/internal/service"
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

	redisCli := redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	})
	defer redisCli.Close()

	asynqClient := asynq.NewClient(asynq.RedisClientOpt{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	})
	defer asynqClient.Close()

	jwtMgr := auth.NewJWTManager(cfg.JWTSigningKey, cfg.JWTIssuer, cfg.JWTExpiration)

	tenantRepo := repository.NewPgTenantRepo(dbPool)
	channelRepo := repository.NewPgChannelConfigRepo(dbPool)
	notifRepo := repository.NewPgNotificationRepo(dbPool)
	attemptRepo := repository.NewPgDeliveryAttemptRepo(dbPool)
	metricRepo := repository.NewPgDeliveryMetricRepo(dbPool)

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

	tenantSvc := service.NewTenantService(tenantRepo)
	channelSvc := service.NewChannelService(channelRepo, registry, logger)
	notifSvc := service.NewNotificationService(notifRepo, channelRepo, asynqClient, cfg.MaxRetries, logger)

	tenantH := handler.NewTenantHandler(tenantSvc)
	channelH := handler.NewChannelHandler(channelSvc)
	notifH := handler.NewNotificationHandler(notifSvc, attemptRepo, metricRepo)
	authH := handler.NewAuthHandler(tenantRepo, jwtMgr, cfg.AdminAPIKey, cfg.AdminAPISecret)
	healthH := handler.NewHealthHandler(dbPool, redisCli)

	r := router.New(jwtMgr, tenantH, channelH, notifH, authH, healthH)

	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.APIPort),
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info().Int("port", cfg.APIPort).Msg("API server starting")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	select {
	case err := <-errCh:
		logger.Error().Err(err).Msg("server failed to start")
		return
	case <-quit:
		logger.Info().Msg("shutting down server...")
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		logger.Fatal().Err(err).Msg("server forced to shutdown")
	}
	logger.Info().Msg("server stopped")
}
