package config

import (
	"time"

	"github.com/caarlos0/env/v11"
)

type Config struct {
	APIPort         int           `env:"API_PORT" envDefault:"8080"`
	ShutdownTimeout time.Duration `env:"SHUTDOWN_TIMEOUT" envDefault:"15s"`

	DatabaseURL string `env:"DATABASE_URL,required"`

	DBMaxConns          int32         `env:"DB_MAX_CONNS" envDefault:"25"`
	DBMinConns          int32         `env:"DB_MIN_CONNS" envDefault:"5"`
	DBMaxConnLifetime   time.Duration `env:"DB_MAX_CONN_LIFETIME" envDefault:"30m"`
	DBMaxConnIdleTime   time.Duration `env:"DB_MAX_CONN_IDLE_TIME" envDefault:"5m"`
	DBHealthCheckPeriod time.Duration `env:"DB_HEALTH_CHECK_PERIOD" envDefault:"30s"`

	RedisAddr     string `env:"REDIS_ADDR" envDefault:"127.0.0.1:6379"`
	RedisPassword string `env:"REDIS_PASSWORD" envDefault:""`
	RedisDB       int    `env:"REDIS_DB" envDefault:"0"`

	JWTSigningKey string        `env:"JWT_SIGNING_KEY,required"`
	JWTExpiration time.Duration `env:"JWT_EXPIRATION" envDefault:"1h"`
	JWTIssuer     string        `env:"JWT_ISSUER" envDefault:"notifyd"`

	WorkerConcurrency int `env:"WORKER_CONCURRENCY" envDefault:"10"`

	MaxRetries    int           `env:"MAX_RETRIES" envDefault:"5"`
	MinRetryDelay time.Duration `env:"MIN_RETRY_DELAY" envDefault:"15s"`
	MaxRetryDelay time.Duration `env:"MAX_RETRY_DELAY" envDefault:"30m"`

	LogLevel string `env:"LOG_LEVEL" envDefault:"info"`

	TelegramBotToken  string `env:"TELEGRAM_BOT_TOKEN" envDefault:""`
	TelegramAdminChat int64  `env:"TELEGRAM_ADMIN_CHAT" envDefault:"0"`

	AdminAPIKey    string `env:"ADMIN_API_KEY" envDefault:""`
	AdminAPISecret string `env:"ADMIN_API_SECRET" envDefault:""`

	ServiceHMACSecret string `env:"SERVICE_HMAC_SECRET" envDefault:""`
	BillingWebhookURL string `env:"BILLING_WEBHOOK_URL" envDefault:""`
	UpgradeURL        string `env:"UPGRADE_URL" envDefault:"https://portal.fluxintek.com/billing"`
}

func Load() (*Config, error) {
	cfg := &Config{}
	if err := env.Parse(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}
