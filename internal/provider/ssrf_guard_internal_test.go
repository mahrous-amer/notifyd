package provider

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubResolver is a minimal ipLookuper that returns a fixed, test-controlled
// answer for any host, so DNS behavior that's rare or slow to reproduce
// against real DNS (multiple A records, one of them private) can be tested
// deterministically.
type stubResolver struct {
	ips []net.IP
	err error
}

func (s stubResolver) LookupIP(_ context.Context, _, _ string) ([]net.IP, error) {
	return s.ips, s.err
}

// TestNewGuardedDialer_ValidatesAllAddressesBeforeDialingAny is a regression
// test for the "validate every resolved IP before dialing any of them"
// property. A hostname can resolve to multiple A/AAAA records; if the guard
// validated addresses one at a time inside the dial loop instead of up
// front, a public address listed before a private one in the same DNS
// answer could be dialed and connected successfully before the private
// address was ever inspected — silently reopening the SSRF hole this guard
// exists to close. This test fails if a future refactor reintroduces that
// per-address-inside-the-loop pattern, because it asserts zero dial
// attempts happened at all, not just that the overall call failed.
func TestNewGuardedDialer_ValidatesAllAddressesBeforeDialingAny(t *testing.T) {
	publicIP := net.ParseIP("8.8.8.8")
	privateIP := net.ParseIP("10.0.0.1")
	resolver := stubResolver{ips: []net.IP{publicIP, privateIP}}

	dialAttempts := 0
	dial := func(_ context.Context, _, _ string) (net.Conn, error) {
		dialAttempts++
		return nil, errors.New("dial must never be called")
	}

	dialContext := newGuardedDialer(resolver, dial)
	conn, err := dialContext(context.Background(), "tcp", "example.com:443")

	require.Nil(t, conn)
	require.Error(t, err)
	assert.ErrorIs(t, err, errBlockedAddress)
	assert.Equal(t, 0, dialAttempts, "no address may be dialed once any resolved address is blocked")
}

// TestNewGuardedDialer_DialsFirstValidatedAddressWhenAllPublic is the
// counterpart happy-path check: when every resolved address passes, the
// dialer proceeds and does attempt a connection.
func TestNewGuardedDialer_DialsFirstValidatedAddressWhenAllPublic(t *testing.T) {
	resolver := stubResolver{ips: []net.IP{net.ParseIP("8.8.8.8"), net.ParseIP("1.1.1.1")}}

	var dialedAddrs []string
	dial := func(_ context.Context, _, addr string) (net.Conn, error) {
		dialedAddrs = append(dialedAddrs, addr)
		return nil, errors.New("stub: no real connection in this test")
	}

	dialContext := newGuardedDialer(resolver, dial)
	_, err := dialContext(context.Background(), "tcp", "example.com:443")

	require.Error(t, err)
	assert.NotErrorIs(t, err, errBlockedAddress, "an all-public answer must reach the dial step, not the guard")
	assert.Equal(t, []string{"8.8.8.8:443", "1.1.1.1:443"}, dialedAddrs)
}
