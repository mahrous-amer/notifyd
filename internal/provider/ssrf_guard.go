package provider

import (
	"context"
	"errors"
	"fmt"
	"net"
)

// errBlockedAddress is returned by the guarded dialer when a target address
// resolves to a private, loopback, or link-local range. It is wrapped in the
// error chain so callers can classify the resulting send as a permanent
// failure: retrying can never reach a different address for the same host.
var errBlockedAddress = errors.New("refusing to connect to a private/internal address")

// IsBlockedIP reports whether ip falls in a range the webhook provider must
// never connect to. The generic webhook provider is an HTTP client to
// arbitrary tenant-supplied URLs; without this guard a tenant could use a
// notification channel to probe services on the compose-internal network
// (e.g. postgres:5432, notifyd-api:8080) or on notifyd's own host.
//
// Covers, for both IPv4 and IPv6:
//   - loopback (127.0.0.0/8, ::1)
//   - link-local (169.254.0.0/16, fe80::/10)
//   - private / unique-local (10/8, 172.16/12, 192.168/16, fc00::/7)
//   - unspecified (0.0.0.0, ::) and multicast, which have no legitimate
//     use as a webhook destination and are rejected as a side effect of
//     using net.IP's own classification methods.
//
// Exported for direct table-driven testing of the classification rules,
// independent of DNS resolution or a live dial.
func IsBlockedIP(ip net.IP) bool {
	return ip.IsLoopback() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsPrivate() ||
		ip.IsUnspecified() ||
		ip.IsMulticast()
}

// guardedDialContext returns a DialContext function that validates the
// RESOLVED address before connecting, not just the hostname up front. A
// pre-resolve check (look up the host, decide, then let the standard dialer
// re-resolve and connect) leaves a TOCTOU window: a DNS record can change
// between the check and the connect, and an attacker fully controls DNS for
// any hostname they supply as a webhook URL. This dialer instead performs
// the resolution itself, validates every resulting address, and connects
// only to an address it already approved — the standard library dialer
// never gets a hostname to re-resolve.
func guardedDialContext(base *net.Dialer) func(ctx context.Context, network, addr string) (net.Conn, error) {
	resolver := base.Resolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}

	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, fmt.Errorf("split host/port: %w", err)
		}

		ips, err := resolver.LookupIP(ctx, "ip", host)
		if err != nil {
			return nil, fmt.Errorf("resolve host: %w", err)
		}

		for _, ip := range ips {
			if IsBlockedIP(ip) {
				return nil, fmt.Errorf("%w: %s resolved to %s", errBlockedAddress, host, ip)
			}
		}

		// Dial the specific validated IPs directly (never the original
		// hostname) so nothing between this check and the connect can
		// substitute an address that was never validated.
		var lastErr error
		for _, ip := range ips {
			conn, dialErr := base.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			if dialErr == nil {
				return conn, nil
			}
			lastErr = dialErr
		}
		if lastErr == nil {
			lastErr = fmt.Errorf("no addresses returned for host %s", host)
		}
		return nil, lastErr
	}
}
