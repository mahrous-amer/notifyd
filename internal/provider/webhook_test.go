package provider_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bse/notifyd/internal/domain"
	"github.com/bse/notifyd/internal/provider"
)

// newWebhookConfig JSON-encodes a webhook channel config. Encoding is
// assumed to succeed since the input is controlled by the test.
func newWebhookConfig(t *testing.T, url, secret string) json.RawMessage {
	t.Helper()
	cfg := map[string]any{"url": url}
	if secret != "" {
		cfg["secret"] = secret
	}
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)
	return raw
}

// webhookProviderDialingTestServer returns a WebhookProvider wired to the
// given httptest.Server's own client instead of the SSRF-guarded default.
// httptest.Server always listens on a loopback address, which the
// production guard correctly refuses — these tests are verifying Send's
// request construction, signing, and status classification, not the SSRF
// guard itself (see ssrf_guard_test.go for that), so they use the
// server's client the same way the Discord/Slack/Telegram provider tests
// point at an httptest.Server.
func webhookProviderDialingTestServer(t *testing.T, server *httptest.Server) *provider.WebhookProvider {
	t.Helper()
	return provider.NewWebhookProviderWithClient(server.Client())
}

func TestWebhookProvider_Type(t *testing.T) {
	p := provider.NewWebhookProvider()
	assert.Equal(t, "webhook", p.Type())
}

func TestWebhookProvider_Capabilities_ReturnsEmpty(t *testing.T) {
	p := provider.NewWebhookProvider()
	assert.Empty(t, p.Capabilities().Capabilities)
}

func TestWebhookProvider_FetchMetrics_ReturnsErrMetricsNotSupported(t *testing.T) {
	p := provider.NewWebhookProvider()

	metrics, err := p.FetchMetrics(context.Background(), nil, "any-id")

	assert.Nil(t, metrics)
	assert.ErrorIs(t, err, domain.ErrMetricsNotSupported)
}

func TestWebhookProvider_ValidateConfig(t *testing.T) {
	p := provider.NewWebhookProvider()

	t.Run("valid https URL with no secret or headers", func(t *testing.T) {
		err := p.ValidateConfig(newWebhookConfig(t, "https://example.com/hook", ""))
		require.NoError(t, err)
	})

	t.Run("valid https URL with secret", func(t *testing.T) {
		err := p.ValidateConfig(newWebhookConfig(t, "https://example.com/hook", "sekret"))
		require.NoError(t, err)
	})

	t.Run("missing url", func(t *testing.T) {
		err := p.ValidateConfig(newWebhookConfig(t, "", ""))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "url is required")
	})

	t.Run("rejects plain http", func(t *testing.T) {
		err := p.ValidateConfig(newWebhookConfig(t, "http://example.com/hook", ""))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "https")
	})

	t.Run("invalid JSON", func(t *testing.T) {
		err := p.ValidateConfig(json.RawMessage(`{not valid json}`))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid webhook config")
	})

	t.Run("rejects Host header", func(t *testing.T) {
		cfg, _ := json.Marshal(map[string]any{
			"url":     "https://example.com/hook",
			"headers": map[string]string{"Host": "evil.example.com"},
		})
		err := p.ValidateConfig(cfg)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "Host")
	})

	t.Run("rejects Authorization header when secret is set", func(t *testing.T) {
		cfg, _ := json.Marshal(map[string]any{
			"url":     "https://example.com/hook",
			"secret":  "sekret",
			"headers": map[string]string{"Authorization": "Bearer xyz"},
		})
		err := p.ValidateConfig(cfg)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "Authorization")
	})

	t.Run("allows Authorization header when no secret is set", func(t *testing.T) {
		cfg, _ := json.Marshal(map[string]any{
			"url":     "https://example.com/hook",
			"headers": map[string]string{"Authorization": "Bearer xyz"},
		})
		err := p.ValidateConfig(cfg)
		require.NoError(t, err)
	})

	t.Run("rejects X-Notifyd-* headers case-insensitively", func(t *testing.T) {
		for _, name := range []string{"X-Notifyd-Signature", "x-notifyd-timestamp", "X-NOTIFYD-Custom"} {
			cfg, _ := json.Marshal(map[string]any{
				"url":     "https://example.com/hook",
				"headers": map[string]string{name: "anything"},
			})
			err := p.ValidateConfig(cfg)
			require.Error(t, err, "header %q must be rejected", name)
			assert.Contains(t, err.Error(), "reserved")
		}
	})

	t.Run("allows a normal custom header", func(t *testing.T) {
		cfg, _ := json.Marshal(map[string]any{
			"url":     "https://example.com/hook",
			"headers": map[string]string{"X-Tenant-Id": "abc123"},
		})
		err := p.ValidateConfig(cfg)
		require.NoError(t, err)
	})
}

func TestWebhookProvider_Send_Success(t *testing.T) {
	var capturedBody []byte
	var capturedContentType string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		capturedContentType = r.Header.Get("Content-Type")
		body := make([]byte, 4096)
		n, _ := r.Body.Read(body)
		capturedBody = body[:n]
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	p := webhookProviderDialingTestServer(t, server)
	notifID := uuid.New()
	req := provider.SendRequest{
		NotificationID: notifID,
		Subject:        "Hello",
		Body:           "World",
		FormatMode:     "markdown",
		Metadata:       json.RawMessage(`{"order_id":"o-1"}`),
	}

	resp, err := p.Send(context.Background(), newWebhookConfig(t, server.URL, ""), req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Success)
	assert.Equal(t, "application/json", capturedContentType)

	var payload map[string]any
	require.NoError(t, json.Unmarshal(capturedBody, &payload))
	assert.Equal(t, notifID.String(), payload["notification_id"])
	assert.Equal(t, "Hello", payload["subject"])
	assert.Equal(t, "World", payload["body"])
	assert.Equal(t, "markdown", payload["format"])
	assert.Equal(t, map[string]any{"order_id": "o-1"}, payload["metadata"])
	require.Contains(t, payload, "sent_at")
	_, err = time.Parse(time.RFC3339, payload["sent_at"].(string))
	assert.NoError(t, err, "sent_at must be RFC3339")
}

func TestWebhookProvider_Send_NoMetadata_SendsEmptyObject(t *testing.T) {
	var capturedBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, 4096)
		n, _ := r.Body.Read(body)
		capturedBody = body[:n]
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	p := webhookProviderDialingTestServer(t, server)
	req := provider.SendRequest{Body: "no metadata here"}

	_, err := p.Send(context.Background(), newWebhookConfig(t, server.URL, ""), req)
	require.NoError(t, err)

	var payload map[string]any
	require.NoError(t, json.Unmarshal(capturedBody, &payload))
	assert.Equal(t, map[string]any{}, payload["metadata"])
}

func TestWebhookProvider_Send_CustomHeaders_AreSent(t *testing.T) {
	var capturedHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeader = r.Header.Get("X-Tenant-Id")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	p := webhookProviderDialingTestServer(t, server)
	cfg, _ := json.Marshal(map[string]any{
		"url":     server.URL,
		"headers": map[string]string{"X-Tenant-Id": "abc123"},
	})

	_, err := p.Send(context.Background(), cfg, provider.SendRequest{Body: "test"})
	require.NoError(t, err)
	assert.Equal(t, "abc123", capturedHeader)
}

func TestWebhookProvider_Send_SignsPayloadWhenSecretSet(t *testing.T) {
	const secret = "shh-its-a-secret"
	var capturedBody []byte
	var capturedSignature, capturedTimestamp string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedSignature = r.Header.Get("X-Notifyd-Signature")
		capturedTimestamp = r.Header.Get("X-Notifyd-Timestamp")
		body := make([]byte, 4096)
		n, _ := r.Body.Read(body)
		capturedBody = body[:n]
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	p := webhookProviderDialingTestServer(t, server)
	before := time.Now().Unix()

	_, err := p.Send(context.Background(), newWebhookConfig(t, server.URL, secret), provider.SendRequest{Body: "signed message"})
	require.NoError(t, err)

	after := time.Now().Unix()

	require.NotEmpty(t, capturedTimestamp)
	require.NotEmpty(t, capturedSignature)

	ts, err := strconv.ParseInt(capturedTimestamp, 10, 64)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, ts, before)
	assert.LessOrEqual(t, ts, after)

	expected := expectedWebhookSignature(secret, capturedTimestamp, capturedBody)
	assert.Equal(t, "sha256="+expected, capturedSignature)
}

func TestWebhookProvider_Send_NoSecret_OmitsSignatureHeaders(t *testing.T) {
	var sawSignature, sawTimestamp bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawSignature = r.Header.Get("X-Notifyd-Signature") != ""
		sawTimestamp = r.Header.Get("X-Notifyd-Timestamp") != ""
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	p := webhookProviderDialingTestServer(t, server)
	_, err := p.Send(context.Background(), newWebhookConfig(t, server.URL, ""), provider.SendRequest{Body: "unsigned"})
	require.NoError(t, err)

	assert.False(t, sawSignature)
	assert.False(t, sawTimestamp)
}

func TestWebhookProvider_Send_ServerError_IsTransient(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	p := webhookProviderDialingTestServer(t, server)
	resp, err := p.Send(context.Background(), newWebhookConfig(t, server.URL, ""), provider.SendRequest{Body: "test"})

	require.NoError(t, err)
	assert.False(t, resp.Success)
	assert.False(t, resp.Permanent)
	assert.Contains(t, resp.ErrorMessage, "500")
}

func TestWebhookProvider_Send_TooManyRequests_IsTransient(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	p := webhookProviderDialingTestServer(t, server)
	resp, err := p.Send(context.Background(), newWebhookConfig(t, server.URL, ""), provider.SendRequest{Body: "test"})

	require.NoError(t, err)
	assert.False(t, resp.Success)
	assert.False(t, resp.Permanent, "429 must be retried, not treated as permanent")
}

func TestWebhookProvider_Send_BadRequest_IsPermanent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	p := webhookProviderDialingTestServer(t, server)
	resp, err := p.Send(context.Background(), newWebhookConfig(t, server.URL, ""), provider.SendRequest{Body: "test"})

	require.NoError(t, err)
	assert.False(t, resp.Success)
	assert.True(t, resp.Permanent)
	assert.Contains(t, resp.ErrorMessage, "400")
}

func TestWebhookProvider_Send_Unauthorized_IsPermanent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	p := webhookProviderDialingTestServer(t, server)
	resp, err := p.Send(context.Background(), newWebhookConfig(t, server.URL, ""), provider.SendRequest{Body: "test"})

	require.NoError(t, err)
	assert.False(t, resp.Success)
	assert.True(t, resp.Permanent)
}

func TestWebhookProvider_Send_Redirect_IsNotFollowed(t *testing.T) {
	// A malicious or compromised webhook target could 302 to a private
	// address to smuggle a request past a naive "check the URL once"
	// guard. The provider must never follow the redirect at all.
	var targetCalled bool
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetCalled = true
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer redirector.Close()

	// This test needs the provider's real "never follow redirects" policy,
	// which server.Client() (used by the other Send tests in this file)
	// does not have — it follows redirects by default. Build a client that
	// keeps that policy but, like webhookProviderDialingTestServer, skips
	// the SSRF guard so it can reach the loopback-bound test servers.
	client := redirector.Client()
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	p := provider.NewWebhookProviderWithClient(client)
	resp, err := p.Send(context.Background(), newWebhookConfig(t, redirector.URL, ""), provider.SendRequest{Body: "test"})

	require.NoError(t, err)
	assert.False(t, resp.Success)
	assert.True(t, resp.Permanent, "a redirect response can never succeed since it is never followed")
	assert.False(t, targetCalled, "the redirect target must never be contacted")
}

// expectedWebhookSignature recomputes the HMAC the way a receiver
// implementing the same contract independently would, to verify the
// provider's signature against a from-scratch implementation rather than
// its own helper.
func expectedWebhookSignature(secret, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}
