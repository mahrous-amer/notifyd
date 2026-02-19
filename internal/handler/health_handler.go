package handler

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/bse/notifyd/pkg/response"
)

type HealthHandler struct {
	dbPool    *pgxpool.Pool
	redisCli  *redis.Client
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

func parsePagination(r *http.Request) (int, int) {
	limit := 20
	offset := 0
	if l := r.URL.Query().Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 && v <= 100 {
			limit = v
		}
	}
	if o := r.URL.Query().Get("offset"); o != "" {
		if v, err := strconv.Atoi(o); err == nil && v >= 0 {
			offset = v
		}
	}
	return limit, offset
}
