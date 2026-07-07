package auth

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/bse/notifyd/pkg/response"
)

const (
	maxBodyBytes    int64         = 1 << 20 // 1 MiB
	timestampWindow time.Duration = 5 * time.Minute
)

// HMACMiddleware authenticates service-to-service calls (billing -> notifyd).
// The caller signs a canonical request string with HMAC-SHA256 and sends the
// hex digest in X-Service-Signature. The canonical string is:
//
//	METHOD + "\n" + requestURI + "\n" + X-Service-Timestamp + "\n" + hex(sha256(body))
//
// X-Service-Timestamp must be RFC3339 and within 5 minutes of the server clock.
// An empty configured secret fails closed (503) — the deployment is misconfigured.
func HMACMiddleware(secret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if secret == "" {
				response.Error(w, http.StatusServiceUnavailable, "service auth not configured")
				return
			}

			// Bounded read — prevents DoS via oversized bodies.
			r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
			body, err := io.ReadAll(r.Body)
			if err != nil {
				response.Error(w, http.StatusBadRequest, "failed to read body")
				return
			}
			// Restore the body so downstream handlers can still read it.
			r.Body = io.NopCloser(bytes.NewReader(body))

			// Validate the timestamp header (fail with a generic message to avoid leaking which check failed).
			tsHeader := r.Header.Get("X-Service-Timestamp")
			if tsHeader == "" {
				response.Error(w, http.StatusUnauthorized, "invalid credentials")
				return
			}
			ts, err := time.Parse(time.RFC3339Nano, tsHeader)
			if err != nil {
				response.Error(w, http.StatusUnauthorized, "invalid credentials")
				return
			}
			diff := time.Since(ts)
			if diff > timestampWindow || diff < -timestampWindow {
				response.Error(w, http.StatusUnauthorized, "invalid credentials")
				return
			}

			// Build the canonical string and compute the expected HMAC.
			bodyHash := sha256.Sum256(body)
			bodyHashHex := hex.EncodeToString(bodyHash[:])
			canonical := fmt.Sprintf("%s\n%s\n%s\n%s",
				r.Method,
				r.URL.RequestURI(),
				tsHeader,
				bodyHashHex,
			)

			mac := hmac.New(sha256.New, []byte(secret))
			mac.Write([]byte(canonical))
			expected := hex.EncodeToString(mac.Sum(nil))

			provided := r.Header.Get("X-Service-Signature")
			if provided == "" || !hmac.Equal([]byte(expected), []byte(provided)) {
				response.Error(w, http.StatusUnauthorized, "invalid credentials")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
