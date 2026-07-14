package provider_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/bse/notifyd/internal/provider"
)

func TestValidateHTTPSDestinationURL(t *testing.T) {
	t.Run("accepts an ordinary https URL", func(t *testing.T) {
		err := provider.ValidateHTTPSDestinationURL("https://example.com/hooks/notifyd")
		assert.NoError(t, err)
	})

	t.Run("rejects a plain http URL", func(t *testing.T) {
		err := provider.ValidateHTTPSDestinationURL("http://example.com/hooks/notifyd")
		assert.Error(t, err)
	})

	t.Run("rejects an empty URL", func(t *testing.T) {
		err := provider.ValidateHTTPSDestinationURL("")
		assert.Error(t, err)
	})

	t.Run("rejects an unparseable URL", func(t *testing.T) {
		err := provider.ValidateHTTPSDestinationURL("https://[::1")
		assert.Error(t, err)
	})

	t.Run("rejects a literal loopback IPv4 host", func(t *testing.T) {
		err := provider.ValidateHTTPSDestinationURL("https://127.0.0.1/hooks")
		assert.Error(t, err)
	})

	t.Run("rejects a literal private IPv4 host", func(t *testing.T) {
		err := provider.ValidateHTTPSDestinationURL("https://10.0.0.5/hooks")
		assert.Error(t, err)
	})

	t.Run("rejects a literal loopback IPv6 host", func(t *testing.T) {
		err := provider.ValidateHTTPSDestinationURL("https://[::1]/hooks")
		assert.Error(t, err)
	})

	t.Run("accepts a literal public IPv4 host", func(t *testing.T) {
		err := provider.ValidateHTTPSDestinationURL("https://8.8.8.8/hooks")
		assert.NoError(t, err)
	})

	t.Run("accepts an ordinary hostname without resolving it", func(t *testing.T) {
		// A hostname that merely resolves to a private address (e.g. via
		// attacker-controlled DNS) cannot be caught here — that TOCTOU gap
		// is exactly why the real defense is guardedDialContext validating
		// the resolved address at dial time. This check only catches literal
		// IP addresses used directly as the host.
		err := provider.ValidateHTTPSDestinationURL("https://internal.example.com/hooks")
		assert.NoError(t, err)
	})
}
