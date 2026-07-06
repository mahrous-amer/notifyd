package quota

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	"github.com/bse/notifyd/internal/auth"
)

type stubReserver struct {
	decision *Decision
	gotN     int64
}

func (s *stubReserver) Reserve(_ context.Context, _ uuid.UUID, n int64) (*Decision, error) {
	s.gotN = n
	return s.decision, nil
}

func withClaims(req *http.Request) *http.Request {
	claims := &auth.TenantClaims{TenantID: uuid.New()}
	return req.WithContext(context.WithValue(req.Context(), auth.TenantClaimsKey, claims))
}

func TestQuotaMiddleware_AllowsAndCountsOne(t *testing.T) {
	res := &stubReserver{decision: &Decision{Allowed: true}}
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusAccepted) })
	req := withClaims(httptest.NewRequest(http.MethodPost, "/notifications/send", strings.NewReader(`{"body":"hi"}`)))
	rec := httptest.NewRecorder()

	Middleware(res, "https://portal.fluxintek.com/billing")(next).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusAccepted, rec.Code)
	assert.Equal(t, int64(1), res.gotN)
}

func TestQuotaMiddleware_SendMultiCountsChannels(t *testing.T) {
	res := &stubReserver{decision: &Decision{Allowed: true}}
	var handlerBody string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b := make([]byte, r.ContentLength)
		r.Body.Read(b) //nolint:errcheck
		handlerBody = string(b)
		w.WriteHeader(http.StatusAccepted)
	})
	body := `{"channels":[{"channel_config_id":"a"},{"channel_config_id":"b"},{"channel_config_id":"c"}],"body":"hi"}`
	req := withClaims(httptest.NewRequest(http.MethodPost, "/notifications/send-multi", strings.NewReader(body)))
	rec := httptest.NewRecorder()

	Middleware(res, "u")(next).ServeHTTP(rec, req)

	assert.Equal(t, int64(3), res.gotN)
	assert.Equal(t, body, handlerBody, "body must be restored for the handler")
}

func TestQuotaMiddleware_RejectsWith429(t *testing.T) {
	res := &stubReserver{decision: &Decision{Allowed: false, Used: 1000, Limit: 1000}}
	req := withClaims(httptest.NewRequest(http.MethodPost, "/notifications/send", strings.NewReader(`{}`)))
	rec := httptest.NewRecorder()

	Middleware(res, "https://portal.fluxintek.com/billing")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler must not be called")
	})).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusTooManyRequests, rec.Code)
	assert.Contains(t, rec.Body.String(), "QUOTA_EXCEEDED")
	assert.Contains(t, rec.Body.String(), "https://portal.fluxintek.com/billing")
}
