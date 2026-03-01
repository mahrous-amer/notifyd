package provider_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bse/notifyd/internal/domain"
	"github.com/bse/notifyd/internal/provider"
)

// newDiscordConfig returns a JSON-encoded discordConfig pointing at the given
// webhook URL. Encoding is assumed to succeed since the input is controlled.
func newDiscordConfig(webhookURL string) json.RawMessage {
	raw, _ := json.Marshal(map[string]string{"webhook_url": webhookURL})
	return raw
}

func TestDiscordProvider_Type(t *testing.T) {
	p := provider.NewDiscordProvider(http.DefaultClient)
	assert.Equal(t, "discord", p.Type())
}

func TestDiscordProvider_Capabilities_ReturnsEmpty(t *testing.T) {
	p := provider.NewDiscordProvider(http.DefaultClient)
	caps := p.Capabilities()
	assert.Empty(t, caps.Capabilities)
}

func TestDiscordProvider_FetchMetrics_ReturnsErrMetricsNotSupported(t *testing.T) {
	p := provider.NewDiscordProvider(http.DefaultClient)

	metrics, err := p.FetchMetrics(context.Background(), nil, "any-id")

	assert.Nil(t, metrics)
	assert.True(t, errors.Is(err, domain.ErrMetricsNotSupported))
}

func TestDiscordProvider_ValidateConfig(t *testing.T) {
	p := provider.NewDiscordProvider(http.DefaultClient)

	t.Run("valid config", func(t *testing.T) {
		config := newDiscordConfig("https://discord.com/api/webhooks/123/abc")
		err := p.ValidateConfig(config)
		require.NoError(t, err)
	})

	t.Run("missing webhook_url", func(t *testing.T) {
		config, _ := json.Marshal(map[string]string{"webhook_url": ""})
		err := p.ValidateConfig(config)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "webhook_url is required")
	})

	t.Run("invalid JSON", func(t *testing.T) {
		err := p.ValidateConfig(json.RawMessage(`{not valid json}`))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid discord config")
	})
}

func TestDiscordProvider_Send_Success(t *testing.T) {
	// Discord webhooks return 204 No Content on success.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	p := provider.NewDiscordProvider(server.Client())
	config := newDiscordConfig(server.URL)
	req := provider.SendRequest{Subject: "Hello", Body: "World", FormatMode: "markdown"}

	resp, err := p.Send(context.Background(), config, req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Success)
	// Discord webhooks provide no message ID; ProviderMsgID must be empty.
	assert.Empty(t, resp.ProviderMsgID)
}

func TestDiscordProvider_Send_Success_200Response(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	p := provider.NewDiscordProvider(server.Client())
	config := newDiscordConfig(server.URL)
	req := provider.SendRequest{Body: "A plain message"}

	resp, err := p.Send(context.Background(), config, req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Success)
	assert.Empty(t, resp.ProviderMsgID)
}

func TestDiscordProvider_Send_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message":"Internal Server Error"}`))
	}))
	defer server.Close()

	p := provider.NewDiscordProvider(server.Client())
	config := newDiscordConfig(server.URL)
	req := provider.SendRequest{Body: "test"}

	resp, err := p.Send(context.Background(), config, req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.False(t, resp.Success)
	assert.Contains(t, resp.ErrorMessage, "500")
}

func TestDiscordProvider_Send_NetworkError(t *testing.T) {
	p := provider.NewDiscordProvider(http.DefaultClient)

	// An invalid URL causes the HTTP client to fail at the transport layer.
	config := newDiscordConfig("http://127.0.0.1:0/invalid-endpoint")
	req := provider.SendRequest{Body: "test"}

	resp, err := p.Send(context.Background(), config, req)

	// The implementation swallows network errors into the response rather than
	// returning them as Go errors, matching the fire-and-forget contract.
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.False(t, resp.Success)
	assert.NotEmpty(t, resp.ErrorMessage)
}

func TestBuildDiscordMessage_WithSubject_MarkdownMode(t *testing.T) {
	// buildDiscordMessage is unexported, so we exercise it through Send with a
	// test server that captures the request body.
	var capturedBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 4096)
		n, _ := r.Body.Read(buf)
		capturedBody = buf[:n]
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	p := provider.NewDiscordProvider(server.Client())
	config := newDiscordConfig(server.URL)

	req := provider.SendRequest{Subject: "My Title", Body: "My body text", FormatMode: "markdown"}
	_, err := p.Send(context.Background(), config, req)
	require.NoError(t, err)

	var payload map[string]string
	require.NoError(t, json.Unmarshal(capturedBody, &payload))
	assert.Equal(t, "**My Title**\nMy body text", payload["content"])
}

func TestBuildDiscordMessage_WithSubject_PlainMode(t *testing.T) {
	var capturedBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 4096)
		n, _ := r.Body.Read(buf)
		capturedBody = buf[:n]
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	p := provider.NewDiscordProvider(server.Client())
	config := newDiscordConfig(server.URL)

	req := provider.SendRequest{Subject: "My Title", Body: "My body text", FormatMode: "plain"}
	_, err := p.Send(context.Background(), config, req)
	require.NoError(t, err)

	var payload map[string]string
	require.NoError(t, json.Unmarshal(capturedBody, &payload))
	assert.Equal(t, "My Title\nMy body text", payload["content"])
}

func TestBuildDiscordMessage_WithSubject_HTMLMode(t *testing.T) {
	var capturedBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 4096)
		n, _ := r.Body.Read(buf)
		capturedBody = buf[:n]
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	p := provider.NewDiscordProvider(server.Client())
	config := newDiscordConfig(server.URL)

	// html mode falls through to the "plain" branch because Discord doesn't
	// render HTML markup.
	req := provider.SendRequest{Subject: "My Title", Body: "My body text", FormatMode: "html"}
	_, err := p.Send(context.Background(), config, req)
	require.NoError(t, err)

	var payload map[string]string
	require.NoError(t, json.Unmarshal(capturedBody, &payload))
	assert.Equal(t, "My Title\nMy body text", payload["content"])
}

func TestBuildDiscordMessage_WithoutSubject(t *testing.T) {
	var capturedBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 4096)
		n, _ := r.Body.Read(buf)
		capturedBody = buf[:n]
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	p := provider.NewDiscordProvider(server.Client())
	config := newDiscordConfig(server.URL)

	req := provider.SendRequest{Body: "Only the body", FormatMode: "markdown"}
	_, err := p.Send(context.Background(), config, req)
	require.NoError(t, err)

	var payload map[string]string
	require.NoError(t, json.Unmarshal(capturedBody, &payload))
	assert.Equal(t, "Only the body", payload["content"])
}
