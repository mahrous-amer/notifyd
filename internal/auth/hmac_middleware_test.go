package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// sign builds the canonical request string and returns its hex-encoded HMAC-SHA256.
// canonical = METHOD + "\n" + requestURI + "\n" + timestamp + "\n" + hex(sha256(body))
func sign(method, requestURI, timestamp, body, secret string) string {
	bodyHash := sha256.Sum256([]byte(body))
	bodyHashHex := hex.EncodeToString(bodyHash[:])
	canonical := method + "\n" + requestURI + "\n" + timestamp + "\n" + bodyHashHex
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(canonical))
	return hex.EncodeToString(mac.Sum(nil))
}

func freshTS() string {
	return time.Now().UTC().Format(time.RFC3339)
}

func TestHMACMiddleware_ValidSignature(t *testing.T) {
	var gotBody string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
	})
	body := `{"plan_code":"pro"}`
	ts := freshTS()
	req := httptest.NewRequest(http.MethodPut, "/x", strings.NewReader(body))
	req.Header.Set("X-Service-Timestamp", ts)
	req.Header.Set("X-Service-Signature", sign(http.MethodPut, "/x", ts, body, "s3cret"))
	rec := httptest.NewRecorder()

	HMACMiddleware("s3cret")(next).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, body, gotBody, "body must be restored for the handler")
}

func TestHMACMiddleware_InvalidSignature(t *testing.T) {
	ts := freshTS()
	req := httptest.NewRequest(http.MethodPut, "/x", strings.NewReader(`{}`))
	req.Header.Set("X-Service-Timestamp", ts)
	req.Header.Set("X-Service-Signature", "deadbeef")
	rec := httptest.NewRecorder()

	HMACMiddleware("s3cret")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler must not be called")
	})).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestHMACMiddleware_NoSecretFailsClosed(t *testing.T) {
	req := httptest.NewRequest(http.MethodPut, "/x", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()

	HMACMiddleware("")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler must not be called")
	})).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

func TestHMACMiddleware_MissingSignatureHeader(t *testing.T) {
	ts := freshTS()
	req := httptest.NewRequest(http.MethodPut, "/x", strings.NewReader(`{}`))
	req.Header.Set("X-Service-Timestamp", ts)
	// No X-Service-Signature header
	rec := httptest.NewRecorder()

	HMACMiddleware("s3cret")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler must not be called")
	})).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestHMACMiddleware_MissingTimestamp(t *testing.T) {
	req := httptest.NewRequest(http.MethodPut, "/x", strings.NewReader(`{}`))
	req.Header.Set("X-Service-Signature", "somevalue")
	// No X-Service-Timestamp header
	rec := httptest.NewRecorder()

	HMACMiddleware("s3cret")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler must not be called")
	})).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestHMACMiddleware_StaleTimestamp(t *testing.T) {
	staleTS := time.Now().UTC().Add(-10 * time.Minute).Format(time.RFC3339)
	body := `{}`
	req := httptest.NewRequest(http.MethodPut, "/x", strings.NewReader(body))
	req.Header.Set("X-Service-Timestamp", staleTS)
	req.Header.Set("X-Service-Signature", sign(http.MethodPut, "/x", staleTS, body, "s3cret"))
	rec := httptest.NewRecorder()

	HMACMiddleware("s3cret")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler must not be called")
	})).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestHMACMiddleware_TamperedPath(t *testing.T) {
	ts := freshTS()
	body := `{}`
	// Signed for tenant "aaa" path, but request is sent to tenant "bbb" path
	sig := sign(http.MethodPut, "/admin/tenants/aaa/entitlements", ts, body, "s3cret")
	req := httptest.NewRequest(http.MethodPut, "/admin/tenants/bbb/entitlements", strings.NewReader(body))
	req.Header.Set("X-Service-Timestamp", ts)
	req.Header.Set("X-Service-Signature", sig)
	rec := httptest.NewRecorder()

	HMACMiddleware("s3cret")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler must not be called")
	})).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestHMACMiddleware_TamperedQuery(t *testing.T) {
	ts := freshTS()
	body := `{}`
	// Signed for one period_start value, but request uses a different period_start
	sig := sign(http.MethodGet, "/admin/tenants/abc/usage?period_start=2026-01-01T00:00:00Z", ts, body, "s3cret")
	req := httptest.NewRequest(http.MethodGet, "/admin/tenants/abc/usage?period_start=2026-02-01T00:00:00Z", strings.NewReader(body))
	req.Header.Set("X-Service-Timestamp", ts)
	req.Header.Set("X-Service-Signature", sig)
	rec := httptest.NewRecorder()

	HMACMiddleware("s3cret")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler must not be called")
	})).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestHMACMiddleware_FractionalSecondTimestamp(t *testing.T) {
	var gotBody string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
	})

	// Create a timestamp with fractional seconds (millisecond precision) using RFC3339Nano
	ts := time.Now().UTC().Format(time.RFC3339Nano)

	body := `{"plan_code":"pro"}`
	req := httptest.NewRequest(http.MethodPut, "/x", strings.NewReader(body))
	req.Header.Set("X-Service-Timestamp", ts)
	req.Header.Set("X-Service-Signature", sign(http.MethodPut, "/x", ts, body, "s3cret"))
	rec := httptest.NewRecorder()

	HMACMiddleware("s3cret")(next).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, body, gotBody, "body must be restored for the handler")
}
