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
	decision    *Decision
	err         error
	gotN        int64
	refundCalls []int64
}

func (s *stubReserver) Reserve(_ context.Context, _ uuid.UUID, n int64) (*Decision, error) {
	s.gotN = n
	return s.decision, s.err
}

func (s *stubReserver) Refund(_ context.Context, _ uuid.UUID, n int64) error {
	s.refundCalls = append(s.refundCalls, n)
	return nil
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

// TestQuotaMiddleware_Refund_On4xx verifies that a 4xx handler response
// triggers a Refund call with the originally reserved n.
func TestQuotaMiddleware_Refund_On4xx(t *testing.T) {
	res := &stubReserver{decision: &Decision{Allowed: true}}
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	})
	req := withClaims(httptest.NewRequest(http.MethodPost, "/notifications/send", strings.NewReader(`{"body":"hi"}`)))
	rec := httptest.NewRecorder()

	Middleware(res, "u")(next).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Len(t, res.refundCalls, 1, "Refund must be called exactly once on 4xx")
	assert.Equal(t, int64(1), res.refundCalls[0], "Refund must be called with the reserved n")
}

// TestQuotaMiddleware_NoRefund_On2xx verifies that a 2xx handler response does
// NOT trigger a Refund call.
func TestQuotaMiddleware_NoRefund_On2xx(t *testing.T) {
	res := &stubReserver{decision: &Decision{Allowed: true}}
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	})
	req := withClaims(httptest.NewRequest(http.MethodPost, "/notifications/send", strings.NewReader(`{"body":"hi"}`)))
	rec := httptest.NewRecorder()

	Middleware(res, "u")(next).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusAccepted, rec.Code)
	assert.Empty(t, res.refundCalls, "Refund must NOT be called on 2xx")
}

// TestQuotaMiddleware_RejectsWith402OnExpiredPeriod verifies that when Reserve
// returns ErrPeriodExpired the middleware responds 402 with the SUBSCRIPTION_PERIOD_EXPIRED
// error code and the renew URL, and does NOT call the downstream handler.
func TestQuotaMiddleware_RejectsWith402OnExpiredPeriod(t *testing.T) {
	res := &stubReserver{err: ErrPeriodExpired}
	req := withClaims(httptest.NewRequest(http.MethodPost, "/notifications/send", strings.NewReader(`{}`)))
	rec := httptest.NewRecorder()

	Middleware(res, "https://portal.fluxintek.com/billing")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler must not be called when period is expired")
	})).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusPaymentRequired, rec.Code)
	assert.Contains(t, rec.Body.String(), "SUBSCRIPTION_PERIOD_EXPIRED")
	assert.Contains(t, rec.Body.String(), "https://portal.fluxintek.com/billing")
}

// TestQuotaMiddleware_Refund_SendMulti_On4xx verifies that a send-multi
// reservation of n=3 is fully refunded when the handler returns 403.
func TestQuotaMiddleware_Refund_SendMulti_On4xx(t *testing.T) {
	res := &stubReserver{decision: &Decision{Allowed: true}}
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	})
	body := `{"channels":[{"channel_config_id":"a"},{"channel_config_id":"b"},{"channel_config_id":"c"}],"body":"hi"}`
	req := withClaims(httptest.NewRequest(http.MethodPost, "/notifications/send-multi", strings.NewReader(body)))
	rec := httptest.NewRecorder()

	Middleware(res, "u")(next).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Len(t, res.refundCalls, 1, "Refund must be called exactly once")
	assert.Equal(t, int64(3), res.refundCalls[0], "Refund must return all 3 reserved slots")
}
