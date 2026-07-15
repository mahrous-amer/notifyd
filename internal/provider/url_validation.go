package provider

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// ValidateHTTPSDestinationURL applies the static checks worth doing before
// ever attempting a connection: the scheme must be https, the URL must
// parse, and — when the host is a literal IP address rather than a
// hostname — that address must not be one guardedDialContext would refuse
// to dial.
//
// This is deliberately partial: a hostname that merely resolves to a
// private address (attacker-controlled DNS, or DNS that changes after this
// check runs) cannot be caught by any static check — that gap is exactly
// why guardedDialContext re-validates the resolved address at dial time,
// on every attempt, not just the first. Callers that need real SSRF
// protection must still go through NewGuardedHTTPClient; this function only
// gives immediate feedback for the common case of a caller directly
// pasting an internal IP, so a create-time API call, not a webhook
// delivery attempt, is what surfaces the mistake.
func ValidateHTTPSDestinationURL(rawURL string) error {
	if !strings.HasPrefix(rawURL, "https://") {
		return fmt.Errorf("url must use https")
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid url: %w", err)
	}

	host := parsed.Hostname()
	if host == "" {
		return fmt.Errorf("url must include a host")
	}

	if ip := net.ParseIP(host); ip != nil && IsBlockedIP(ip) {
		return fmt.Errorf("url resolves to a private/internal address")
	}
	return nil
}
