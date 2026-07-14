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

// extraBlockedRanges lists CIDR blocks that net.IP's own IsLoopback /
// IsLinkLocalUnicast / IsPrivate / IsUnspecified / IsMulticast methods do not
// classify, but which still route to infrastructure a webhook tenant does
// not own, or (for the IETF special-use blocks) have no legitimate use as a
// webhook destination:
//
//   - 100.64.0.0/10  — RFC 6598 carrier-grade NAT (CGNAT). Cloud providers
//     and ISPs use this range for infrastructure shared across tenants;
//     same threat model as RFC 1918 space.
//   - 192.0.0.0/24   — RFC 6890 IETF protocol assignments.
//   - 192.0.2.0/24   — RFC 5737 TEST-NET-1, documentation-only space.
//   - 198.18.0.0/15  — RFC 2544 inter-network benchmarking.
//   - 240.0.0.0/4    — reserved for future use (includes the former
//     255.255.255.255 broadcast-adjacent space).
//   - 64:ff9b::/96   — RFC 6052 NAT64 well-known prefix. A NAT64 gateway
//     translates addresses in this range by embedding an IPv4 address in
//     the low 32 bits (e.g. 64:ff9b::a9fe:a9fe embeds 169.254.169.254), so
//     the prefix itself must be blocked regardless of which IPv4 address it
//     embeds — checking the embedded address separately would require
//     unwrapping NAT64 on every lookup and still miss embedded addresses
//     this list doesn't already cover.
var extraBlockedRanges = mustParseCIDRs(
	"100.64.0.0/10",
	"192.0.0.0/24",
	"192.0.2.0/24",
	"198.18.0.0/15",
	"240.0.0.0/4",
	"64:ff9b::/96",
)

func mustParseCIDRs(cidrs ...string) []*net.IPNet {
	nets := make([]*net.IPNet, len(cidrs))
	for i, cidr := range cidrs {
		_, ipNet, err := net.ParseCIDR(cidr)
		if err != nil {
			// Every entry above is a fixed, compile-time-known literal;
			// a parse failure here means extraBlockedRanges itself is
			// malformed, which is a programming error, not a runtime
			// condition callers can recover from.
			panic(fmt.Sprintf("ssrf guard: invalid CIDR literal %q: %v", cidr, err))
		}
		nets[i] = ipNet
	}
	return nets
}

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
//   - CGNAT, IETF special-use blocks, and the NAT64 well-known prefix —
//     see extraBlockedRanges for the full list and rationale.
//
// Exported for direct table-driven testing of the classification rules,
// independent of DNS resolution or a live dial.
func IsBlockedIP(ip net.IP) bool {
	if ip.IsLoopback() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsPrivate() ||
		ip.IsUnspecified() ||
		ip.IsMulticast() {
		return true
	}
	for _, blocked := range extraBlockedRanges {
		if blocked.Contains(ip) {
			return true
		}
	}
	return false
}

// ipLookuper is the subset of *net.Resolver's interface guardedDialContext
// depends on. Defined as an interface so tests can stub DNS resolution
// (e.g. to simulate a hostname with multiple A/AAAA records, one public and
// one private) without touching the real network.
type ipLookuper interface {
	LookupIP(ctx context.Context, network, host string) ([]net.IP, error)
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
	dial := func(ctx context.Context, network, addr string) (net.Conn, error) {
		return base.DialContext(ctx, network, addr)
	}
	return newGuardedDialer(resolver, dial)
}

// newGuardedDialer builds the DialContext function from an ipLookuper and a
// plain dial function, kept separate from guardedDialContext so tests can
// substitute both: a stub resolver to simulate arbitrary DNS answers (like a
// host with multiple A records, only one of which is private), and a dial
// function that records whether it was ever called, to prove that every
// resolved address is validated up front — before any address is dialed —
// rather than being checked one at a time inside the dial loop. Validating
// inside the loop would let a dial to an earlier, approved address succeed
// and complete before a later, blocked address in the same answer is ever
// inspected, silently reintroducing the SSRF hole this guard exists to
// close.
func newGuardedDialer(
	resolver ipLookuper,
	dial func(ctx context.Context, network, addr string) (net.Conn, error),
) func(ctx context.Context, network, addr string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, fmt.Errorf("split host/port: %w", err)
		}

		ips, err := resolver.LookupIP(ctx, "ip", host)
		if err != nil {
			return nil, fmt.Errorf("resolve host: %w", err)
		}

		// Validate every resolved address before dialing any of them. A
		// hostname can legitimately return multiple A/AAAA records; if
		// even one is blocked, no address gets connected to at all — not
		// even the addresses that come before it in the answer.
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
			conn, dialErr := dial(ctx, network, net.JoinHostPort(ip.String(), port))
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
