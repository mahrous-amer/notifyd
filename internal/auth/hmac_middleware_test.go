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

	"github.com/stretchr/testify/assert"
)

func sign(body, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	return hex.EncodeToString(mac.Sum(nil))
}

func TestHMACMiddleware_ValidSignature(t *testing.T) {
	var gotBody string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
	})
	body := `{"plan_code":"pro"}`
	req := httptest.NewRequest(http.MethodPut, "/x", strings.NewReader(body))
	req.Header.Set("X-Service-Signature", sign(body, "s3cret"))
	rec := httptest.NewRecorder()

	HMACMiddleware("s3cret")(next).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, body, gotBody, "body must be restored for the handler")
}

func TestHMACMiddleware_InvalidSignature(t *testing.T) {
	req := httptest.NewRequest(http.MethodPut, "/x", strings.NewReader(`{}`))
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
