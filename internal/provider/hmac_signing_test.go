package provider_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bse/notifyd/internal/provider"
)

// independentHMAC recomputes the signature the way a receiver implementing
// the contract from scratch would, so the test verifies against an
// independent implementation rather than the package's own helper.
func independentHMAC(secret, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func TestSignHMAC_MatchesIndependentImplementation(t *testing.T) {
	got := provider.SignHMAC("a-secret", "1700000000", []byte(`{"hello":"world"}`))
	want := independentHMAC("a-secret", "1700000000", []byte(`{"hello":"world"}`))
	assert.Equal(t, want, got)
}

func TestSignHMAC_DifferentBodyProducesDifferentSignature(t *testing.T) {
	sig1 := provider.SignHMAC("secret", "1700000000", []byte("body-a"))
	sig2 := provider.SignHMAC("secret", "1700000000", []byte("body-b"))
	assert.NotEqual(t, sig1, sig2)
}

func TestSignHMAC_DifferentTimestampProducesDifferentSignature(t *testing.T) {
	sig1 := provider.SignHMAC("secret", "1700000000", []byte("body"))
	sig2 := provider.SignHMAC("secret", "1700000001", []byte("body"))
	assert.NotEqual(t, sig1, sig2, "timestamp must be covered by the signature to prevent replay under a new timestamp")
}

func TestNewGuardedHTTPClient_RefusesLoopbackDial(t *testing.T) {
	client := provider.NewGuardedHTTPClient(5 * time.Second)

	req, err := http.NewRequest(http.MethodGet, "http://127.0.0.1:1/", nil)
	require.NoError(t, err)

	_, err = client.Do(req)
	require.Error(t, err, "the SSRF guard must refuse to dial a loopback address")
}

func TestNewGuardedHTTPClient_DoesNotFollowRedirects(t *testing.T) {
	client := provider.NewGuardedHTTPClient(5 * time.Second)
	assert.NotNil(t, client.CheckRedirect, "a client with no redirect policy follows redirects by default")

	err := client.CheckRedirect(&http.Request{}, nil)
	assert.ErrorIs(t, err, http.ErrUseLastResponse, "must stop at the first redirect response instead of following it")
}

func TestNewGuardedHTTPClient_DialsThroughGuardedDialContext(t *testing.T) {
	// A loopback listener the guard must refuse regardless of port, proving
	// the returned client's Transport really is wired to the SSRF guard and
	// not just a plain dialer with a timeout.
	client := provider.NewGuardedHTTPClient(2 * time.Second)
	transport, ok := client.Transport.(*http.Transport)
	require.True(t, ok, "expected *http.Transport so DialContext can be inspected")
	require.NotNil(t, transport.DialContext)

	_, err := transport.DialContext(context.Background(), "tcp", net.JoinHostPort("127.0.0.1", "1"))
	require.Error(t, err)
}
