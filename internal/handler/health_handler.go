package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/bse/notifyd/pkg/response"
)

type HealthHandler struct {
	dbPool   *pgxpool.Pool
	redisCli *redis.Client
}

func NewHealthHandler(dbPool *pgxpool.Pool, redisCli *redis.Client) *HealthHandler {
	return &HealthHandler{dbPool: dbPool, redisCli: redisCli}
}

func (h *HealthHandler) Health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	status := "ok"
	checks := map[string]string{}

	if err := h.dbPool.Ping(ctx); err != nil {
		status = "degraded"
		checks["postgres"] = "error"
	} else {
		checks["postgres"] = "ok"
	}

	if err := h.redisCli.Ping(ctx).Err(); err != nil {
		status = "degraded"
		checks["redis"] = "error"
	} else {
		checks["redis"] = "ok"
	}

	code := http.StatusOK
	if status != "ok" {
		code = http.StatusServiceUnavailable
	}

	response.JSON(w, code, map[string]interface{}{
		"status": status,
		"checks": checks,
	})
}
