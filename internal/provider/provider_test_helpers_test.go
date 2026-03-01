package provider_test

import (
	"net/http"
	"net/url"
)

// hostOverrideTransport rewrites every outgoing request to target a fixed
// origin while preserving the original path and query string. This allows
// providers that hard-code their API host (e.g. api.telegram.org,
// graph.facebook.com) to be transparently redirected to a local
// httptest.Server in tests.
type hostOverrideTransport struct {
	base         http.RoundTripper
	targetOrigin string
}

func (h *hostOverrideTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	target, err := url.Parse(h.targetOrigin)
	if err != nil {
		return nil, err
	}

	// Clone the request to avoid mutating the original.
	cloned := req.Clone(req.Context())
	cloned.URL.Scheme = target.Scheme
	cloned.URL.Host = target.Host
	cloned.Host = target.Host

	return h.base.RoundTrip(cloned)
}
