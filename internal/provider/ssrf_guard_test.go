package provider_test

import (
	"context"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bse/notifyd/internal/provider"
)

func TestIsBlockedIP(t *testing.T) {
	tests := []struct {
		name    string
		ip      string
		blocked bool
	}{
		// IPv4 loopback
		{"IPv4 loopback", "127.0.0.1", true},
		{"IPv4 loopback range end", "127.255.255.255", true},
		// IPv4 private (RFC 1918)
		{"IPv4 10/8", "10.0.0.1", true},
		{"IPv4 172.16/12 start", "172.16.0.1", true},
		{"IPv4 172.16/12 end", "172.31.255.255", true},
		{"IPv4 192.168/16", "192.168.1.1", true},
		// IPv4 link-local
		{"IPv4 link-local (169.254/16)", "169.254.1.1", true},
		// IPv4 unspecified
		{"IPv4 unspecified", "0.0.0.0", true},
		// IPv4 public
		{"IPv4 public (Google DNS)", "8.8.8.8", false},
		{"IPv4 public (Cloudflare DNS)", "1.1.1.1", false},
		{"IPv4 just outside 172.16/12", "172.32.0.1", false},

		// IPv6 loopback
		{"IPv6 loopback", "::1", true},
		// IPv6 unique-local (RFC 4193, fc00::/7)
		{"IPv6 unique-local fc00::", "fc00::1", true},
		{"IPv6 unique-local fd00::", "fd12:3456:789a::1", true},
		// IPv6 link-local (fe80::/10)
		{"IPv6 link-local", "fe80::1", true},
		// IPv6 unspecified
		{"IPv6 unspecified", "::", true},
		// IPv6 public
		{"IPv6 public (Google DNS)", "2001:4860:4860::8888", false},

		// IPv4-mapped IPv6 addresses of private ranges must still be caught.
		{"IPv4-mapped IPv6 loopback", "::ffff:127.0.0.1", true},
		{"IPv4-mapped IPv6 private", "::ffff:10.0.0.1", true},

		// CGNAT (RFC 6598, 100.64.0.0/10) — used by cloud providers and ISPs
		// for carrier-grade NAT; routes to infrastructure the tenant does not
		// own, same threat model as RFC 1918 space.
		{"CGNAT 100.64/10 start", "100.64.0.1", true},
		{"CGNAT 100.64/10 end", "100.127.255.255", true},
		{"just below CGNAT range", "100.63.255.255", false},
		{"just above CGNAT range", "100.128.0.1", false},

		// IETF special-use blocks (RFC 5737 docs, RFC 2544 benchmarking,
		// RFC 1122 "this host on this network") that IsPrivate/IsLoopback/
		// IsLinkLocalUnicast do not classify but which have no legitimate use
		// as a webhook destination.
		{"192.0.0.0/24 (IETF protocol assignments)", "192.0.0.1", true},
		{"192.0.2.0/24 (RFC 5737 TEST-NET-1 docs)", "192.0.2.1", true},
		{"198.18.0.0/15 start (RFC 2544 benchmarking)", "198.18.0.1", true},
		{"198.18.0.0/15 end (RFC 2544 benchmarking)", "198.19.255.255", true},
		{"just above 198.18.0.0/15", "198.20.0.1", false},
		{"240.0.0.0/4 (reserved/future use)", "240.0.0.1", true},
		{"240.0.0.0/4 end", "255.255.255.254", true},

		// NAT64 well-known prefix (RFC 6052, 64:ff9b::/96) embeds an IPv4
		// address in the low 32 bits; a NAT64 gateway would translate
		// 64:ff9b::a9fe:a9fe back to the 169.254.169.254 metadata address,
		// so the prefix itself must be blocked regardless of what IPv4
		// address it embeds.
		{"NAT64 well-known prefix embedding private IPv4", "64:ff9b::a9fe:a9fe", true},
		{"NAT64 well-known prefix embedding public IPv4", "64:ff9b::808:808", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip := net.ParseIP(tt.ip)
			require.NotNil(t, ip, "test IP %q must parse", tt.ip)
			assert.Equal(t, tt.blocked, provider.IsBlockedIP(ip))
		})
	}
}

// TestWebhookProvider_Send_BlocksHostnameResolvingToPrivateIP exercises the
// SSRF guard through the real provider and its real (non-test) dialer,
// using "localhost" as a hostname that every OS resolver maps to a
// loopback address (127.0.0.1 or ::1) — exactly the "hostname resolving to
// a private IP" case the guard exists to catch, without depending on any
// external DNS record.
func TestWebhookProvider_Send_BlocksHostnameResolvingToPrivateIP(t *testing.T) {
	p := provider.NewWebhookProvider()
	cfg := newWebhookConfig(t, "https://localhost:1/hook", "")

	resp, err := p.Send(context.Background(), cfg, provider.SendRequest{Body: "test"})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.False(t, resp.Success)
	assert.True(t, resp.Permanent, "an SSRF-blocked address can never succeed on retry")
	assert.Contains(t, resp.ErrorMessage, "private")
}

func TestWebhookProvider_Send_BlocksIPLiteral(t *testing.T) {
	p := provider.NewWebhookProvider()
	cfg := newWebhookConfig(t, "https://127.0.0.1:1/hook", "")

	resp, err := p.Send(context.Background(), cfg, provider.SendRequest{Body: "test"})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.False(t, resp.Success)
	assert.True(t, resp.Permanent)
	assert.Contains(t, resp.ErrorMessage, "private")
}

func TestWebhookProvider_Send_BlocksLinkLocalIPLiteral(t *testing.T) {
	p := provider.NewWebhookProvider()
	// 169.254.169.254 is the well-known cloud metadata endpoint address
	// (AWS/GCP/Azure) — the highest-value SSRF target in practice.
	cfg := newWebhookConfig(t, "https://169.254.169.254/latest/meta-data", "")

	resp, err := p.Send(context.Background(), cfg, provider.SendRequest{Body: "test"})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.False(t, resp.Success)
	assert.True(t, resp.Permanent, "cloud metadata endpoint must be blocked, not merely deprioritized")
}
