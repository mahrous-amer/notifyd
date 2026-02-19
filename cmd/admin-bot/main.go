package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"github.com/bse/notifyd/internal/bot"
	"github.com/bse/notifyd/internal/config"
	"github.com/bse/notifyd/internal/repository"
	"github.com/bse/notifyd/internal/service"
)

func main() {
	logger := zerolog.New(os.Stdout).With().Timestamp().Logger()

	cfg, err := config.Load()
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to load config")
	}

	if cfg.TelegramBotToken == "" {
		logger.Fatal().Msg("TELEGRAM_BOT_TOKEN is required")
	}
	if cfg.TelegramAdminChat == 0 {
		logger.Fatal().Msg("TELEGRAM_ADMIN_CHAT is required")
	}

	dbPool, err := buildDBPool(cfg)
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to connect to database")
	}
	defer dbPool.Close()

	if err := dbPool.Ping(context.Background()); err != nil {
		logger.Fatal().Err(err).Msg("failed to ping database")
	}
	logger.Info().Msg("connected to PostgreSQL")

	tenantRepo := repository.NewPgTenantRepo(dbPool)
	notifRepo := repository.NewPgNotificationRepo(dbPool)
	channelRepo := repository.NewPgChannelConfigRepo(dbPool)

	tenantSvc := service.NewTenantService(tenantRepo)
	// NotificationService requires an asynq client for Send, but the bot only
	// calls GetByID and List, so we pass nil — send paths are never exercised
	// by the admin bot. If Send were ever called, it would panic, which would
	// surface the misconfiguration immediately rather than silently failing.
	notifSvc := service.NewNotificationService(notifRepo, channelRepo, nil, cfg.MaxRetries)

	adminBot, err := bot.New(bot.BotConfig{
		Token:       cfg.TelegramBotToken,
		AdminChatID: cfg.TelegramAdminChat,
		TenantSvc:   tenantSvc,
		NotifSvc:    notifSvc,
		Logger:      logger,
	})
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to create bot")
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	logger.Info().Msg("admin bot starting")
	if err := adminBot.Start(ctx); err != nil {
		logger.Fatal().Err(err).Msg("bot exited with error")
	}
	logger.Info().Msg("admin bot stopped")
}

func buildDBPool(cfg *config.Config) (*pgxpool.Pool, error) {
	poolCfg, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}

	poolCfg.MaxConns = cfg.DBMaxConns
	poolCfg.MinConns = cfg.DBMinConns
	poolCfg.MaxConnLifetime = cfg.DBMaxConnLifetime
	poolCfg.MaxConnIdleTime = cfg.DBMaxConnIdleTime
	poolCfg.HealthCheckPeriod = cfg.DBHealthCheckPeriod

	return pgxpool.NewWithConfig(context.Background(), poolCfg)
}
