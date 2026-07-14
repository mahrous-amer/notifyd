package provider

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"net/http"
	"time"
)

// SignHMAC computes the HMAC-SHA256 signature notifyd sends alongside every
// signed outbound HTTP call — both the generic webhook channel (delivering
// notification content) and the status-webhook delivery worker (delivering
// notification.delivered/notification.failed events) share this exact
// contract so a single receiver-side verification snippet works for both.
//
// The signature covers "timestamp.body" rather than just body so a captured
// (timestamp, signature, body) triple cannot be replayed under a different
// timestamp — the receiver is expected to reject requests whose timestamp is
// too old, which only works if the timestamp itself is part of what's signed.
func SignHMAC(secret, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// NewGuardedHTTPClient builds an *http.Client safe to point at an arbitrary
// tenant-supplied HTTPS URL: every dial goes through guardedDialContext (see
// ssrf_guard.go), which validates the resolved address before connecting,
// and redirects are never followed — a legitimate receiver has no reason to
// redirect a POST, so refusing outright is simpler and strictly safer than
// re-validating each hop.
//
// Shared by the generic webhook channel provider and the status-webhook
// delivery worker; requestTimeout is the only parameter that legitimately
// differs between callers (dial timeout is fixed — a target that can't be
// reached quickly is not worth blocking a worker slot for, regardless of
// which caller is dialing).
func NewGuardedHTTPClient(requestTimeout time.Duration) *http.Client {
	const dialTimeout = 10 * time.Second
	dialer := &net.Dialer{Timeout: dialTimeout}
	transport := &http.Transport{
		DialContext: guardedDialContext(dialer),
	}
	return &http.Client{
		Transport: transport,
		Timeout:   requestTimeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}
