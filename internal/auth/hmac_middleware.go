package auth

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"

	"github.com/bse/notifyd/pkg/response"
)

// HMACMiddleware authenticates service-to-service calls (billing -> notifyd).
// The caller signs the raw request body with HMAC-SHA256 and sends the hex
// digest in X-Service-Signature. An empty secret fails closed: the deployment
// is misconfigured and internal endpoints must not be reachable.
func HMACMiddleware(secret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if secret == "" {
				response.Error(w, http.StatusServiceUnavailable, "service auth not configured")
				return
			}
			body, err := io.ReadAll(r.Body)
			if err != nil {
				response.Error(w, http.StatusBadRequest, "failed to read body")
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(body))

			mac := hmac.New(sha256.New, []byte(secret))
			mac.Write(body)
			expected := hex.EncodeToString(mac.Sum(nil))

			provided := r.Header.Get("X-Service-Signature")
			if provided == "" || !hmac.Equal([]byte(expected), []byte(provided)) {
				response.Error(w, http.StatusUnauthorized, "invalid service signature")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
