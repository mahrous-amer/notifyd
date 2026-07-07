package quota

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/bse/notifyd/internal/auth"
	"github.com/bse/notifyd/pkg/response"
)

// ReserverRefunder is the quota service interface required by Middleware.
type ReserverRefunder interface {
	Reserve(ctx context.Context, tenantID uuid.UUID, n int64) (*Decision, error)
	Refund(ctx context.Context, tenantID uuid.UUID, n int64) error
}

// statusRecorder wraps http.ResponseWriter and captures the HTTP status code
// written by the downstream handler (defaults to 200 if WriteHeader is never
// called).
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (sr *statusRecorder) WriteHeader(code int) {
	sr.status = code
	sr.ResponseWriter.WriteHeader(code)
}

// Middleware enforces the tenant's message quota on send endpoints. For
// send-multi the reservation size equals the number of requested channels;
// the body is restored for the downstream handler.
func Middleware(svc ReserverRefunder, upgradeURL string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			claims := auth.GetClaims(ctx)
			if claims == nil {
				response.Error(w, http.StatusUnauthorized, "unauthorized")
				return
			}

			n := int64(1)
			if strings.HasSuffix(r.URL.Path, "/send-multi") {
				body, err := io.ReadAll(r.Body)
				if err != nil {
					response.Error(w, http.StatusBadRequest, "failed to read body")
					return
				}
				r.Body = io.NopCloser(bytes.NewReader(body))
				var probe struct {
					Channels []json.RawMessage `json:"channels"`
				}
				if err := json.Unmarshal(body, &probe); err == nil && len(probe.Channels) > 0 {
					n = int64(len(probe.Channels))
				}
			}

			decision, err := svc.Reserve(ctx, claims.TenantID, n)
			if err != nil {
				response.Error(w, http.StatusInternalServerError, "internal server error")
				return
			}
			if !decision.Allowed {
				response.JSON(w, http.StatusTooManyRequests, map[string]string{
					"error":       "QUOTA_EXCEEDED",
					"upgrade_url": upgradeURL,
				})
				return
			}

			recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(recorder, r)

			if recorder.status >= 400 {
				// NOTE: threshold webhooks fired during Reserve before this reject was known;
				// a refunded reject may have already emitted a webhook. Acceptable while billing
				// is not consuming webhooks.
				svc.Refund(ctx, claims.TenantID, n) //nolint:errcheck
			}
		})
	}
}
