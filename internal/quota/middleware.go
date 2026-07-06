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

type Reserver interface {
	Reserve(ctx context.Context, tenantID uuid.UUID, n int64) (*Decision, error)
}

// Middleware enforces the tenant's message quota on send endpoints. For
// send-multi the reservation size equals the number of requested channels;
// the body is restored for the downstream handler.
func Middleware(svc Reserver, upgradeURL string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims := auth.GetClaims(r.Context())
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

			decision, err := svc.Reserve(r.Context(), claims.TenantID, n)
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
			next.ServeHTTP(w, r)
		})
	}
}
