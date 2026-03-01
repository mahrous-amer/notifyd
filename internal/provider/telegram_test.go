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

// newTelegramConfig returns a JSON-encoded telegramConfig.
func newTelegramConfig(botToken, chatID string) json.RawMessage {
	raw, _ := json.Marshal(map[string]string{
		"bot_token": botToken,
		"chat_id":   chatID,
	})
	return raw
}

// telegramTestServer spins up an httptest.Server and returns a TelegramProvider
// whose HTTP client transparently redirects all outgoing requests to that
// server. The Telegram provider normally constructs URLs pointing at
// api.telegram.org; the hostOverrideTransport rewrites the host at the
// transport layer so tests never make real network calls.
func telegramTestServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *provider.TelegramProvider) {
	t.Helper()
	server := httptest.NewServer(handler)
	client := &http.Client{
		Transport: &hostOverrideTransport{
			base:         http.DefaultTransport,
			targetOrigin: server.URL,
		},
	}
	return server, provider.NewTelegramProvider(client)
}

// --- Tests ---

func TestTelegramProvider_Type(t *testing.T) {
	p := provider.NewTelegramProvider(http.DefaultClient)
	assert.Equal(t, "telegram", p.Type())
}

func TestTelegramProvider_Capabilities_ReturnsDeliveryStatus(t *testing.T) {
	p := provider.NewTelegramProvider(http.DefaultClient)
	caps := p.Capabilities()
	require.Len(t, caps.Capabilities, 1)
	assert.Equal(t, provider.CapDeliveryStatus, caps.Capabilities[0])
}

func TestTelegramProvider_ValidateConfig(t *testing.T) {
	p := provider.NewTelegramProvider(http.DefaultClient)

	t.Run("valid config", func(t *testing.T) {
		config := newTelegramConfig("bot-token-123", "chat-456")
		err := p.ValidateConfig(config)
		require.NoError(t, err)
	})

	t.Run("missing bot_token", func(t *testing.T) {
		config := newTelegramConfig("", "chat-456")
		err := p.ValidateConfig(config)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "bot_token is required")
	})

	t.Run("missing chat_id", func(t *testing.T) {
		config := newTelegramConfig("bot-token-123", "")
		err := p.ValidateConfig(config)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "chat_id is required")
	})

	t.Run("invalid JSON", func(t *testing.T) {
		err := p.ValidateConfig(json.RawMessage(`{not valid json}`))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid telegram config")
	})
}

func TestTelegramProvider_Send_Success_ExtractsProviderMsgID(t *testing.T) {
	server, p := telegramTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":42}}`))
	})
	defer server.Close()

	config := newTelegramConfig("test-bot-token", "test-chat-id")
	req := provider.SendRequest{Subject: "Hello", Body: "World"}

	resp, err := p.Send(context.Background(), config, req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Success)
	assert.Equal(t, "42", resp.ProviderMsgID)
}

func TestTelegramProvider_Send_Failure_NonOKStatus(t *testing.T) {
	server, p := telegramTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"ok":false,"description":"Bad Request"}`))
	})
	defer server.Close()

	config := newTelegramConfig("test-bot-token", "test-chat-id")
	req := provider.SendRequest{Body: "test message"}

	resp, err := p.Send(context.Background(), config, req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.False(t, resp.Success)
	assert.Contains(t, resp.ErrorMessage, "400")
}

func TestTelegramProvider_FetchMetrics_WithProviderMsgID(t *testing.T) {
	p := provider.NewTelegramProvider(http.DefaultClient)

	metrics, err := p.FetchMetrics(context.Background(), nil, "42")

	require.NoError(t, err)
	require.NotNil(t, metrics)
	assert.Equal(t, "42", metrics.ProviderMsgID)
}

func TestTelegramProvider_FetchMetrics_WithoutProviderMsgID(t *testing.T) {
	p := provider.NewTelegramProvider(http.DefaultClient)

	metrics, err := p.FetchMetrics(context.Background(), nil, "")

	assert.Nil(t, metrics)
	assert.True(t, errors.Is(err, domain.ErrMetricsNotSupported))
}

func TestBuildTelegramMessage_MarkdownMode_Default(t *testing.T) {
	var capturedBody []byte
	server, p := telegramTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 4096)
		n, _ := r.Body.Read(buf)
		capturedBody = buf[:n]
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":1}}`))
	})
	defer server.Close()

	config := newTelegramConfig("tok", "cid")
	req := provider.SendRequest{Subject: "Title", Body: "Body text"}
	_, err := p.Send(context.Background(), config, req)
	require.NoError(t, err)

	var payload map[string]string
	require.NoError(t, json.Unmarshal(capturedBody, &payload))
	assert.Equal(t, "*Title*\nBody text", payload["text"])
	assert.Equal(t, "Markdown", payload["parse_mode"])
}

func TestBuildTelegramMessage_MarkdownMode_Explicit(t *testing.T) {
	var capturedBody []byte
	server, p := telegramTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 4096)
		n, _ := r.Body.Read(buf)
		capturedBody = buf[:n]
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":1}}`))
	})
	defer server.Close()

	config := newTelegramConfig("tok", "cid")
	req := provider.SendRequest{Subject: "Title", Body: "Body text", FormatMode: "markdown"}
	_, err := p.Send(context.Background(), config, req)
	require.NoError(t, err)

	var payload map[string]string
	require.NoError(t, json.Unmarshal(capturedBody, &payload))
	assert.Equal(t, "*Title*\nBody text", payload["text"])
	assert.Equal(t, "Markdown", payload["parse_mode"])
}

func TestBuildTelegramMessage_HTMLMode(t *testing.T) {
	var capturedBody []byte
	server, p := telegramTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 4096)
		n, _ := r.Body.Read(buf)
		capturedBody = buf[:n]
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":1}}`))
	})
	defer server.Close()

	config := newTelegramConfig("tok", "cid")
	req := provider.SendRequest{Subject: "Title", Body: "Body text", FormatMode: "html"}
	_, err := p.Send(context.Background(), config, req)
	require.NoError(t, err)

	var payload map[string]string
	require.NoError(t, json.Unmarshal(capturedBody, &payload))
	assert.Equal(t, "<b>Title</b>\nBody text", payload["text"])
	assert.Equal(t, "HTML", payload["parse_mode"])
}

func TestBuildTelegramMessage_PlainMode(t *testing.T) {
	var capturedBody []byte
	server, p := telegramTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 4096)
		n, _ := r.Body.Read(buf)
		capturedBody = buf[:n]
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":1}}`))
	})
	defer server.Close()

	config := newTelegramConfig("tok", "cid")
	req := provider.SendRequest{Subject: "Title", Body: "Body text", FormatMode: "plain"}
	_, err := p.Send(context.Background(), config, req)
	require.NoError(t, err)

	var payload map[string]string
	require.NoError(t, json.Unmarshal(capturedBody, &payload))
	assert.Equal(t, "Title\nBody text", payload["text"])
	// parse_mode must be absent in plain mode.
	_, hasParseMode := payload["parse_mode"]
	assert.False(t, hasParseMode, "parse_mode should not be present in plain mode")
}

func TestBuildTelegramMessage_NoSubject(t *testing.T) {
	var capturedBody []byte
	server, p := telegramTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 4096)
		n, _ := r.Body.Read(buf)
		capturedBody = buf[:n]
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":1}}`))
	})
	defer server.Close()

	config := newTelegramConfig("tok", "cid")
	req := provider.SendRequest{Body: "Just the body", FormatMode: "markdown"}
	_, err := p.Send(context.Background(), config, req)
	require.NoError(t, err)

	var payload map[string]string
	require.NoError(t, json.Unmarshal(capturedBody, &payload))
	assert.Equal(t, "Just the body", payload["text"])
}

func TestExtractTelegramMessageID_ValidResponse(t *testing.T) {
	server, p := telegramTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":99}}`))
	})
	defer server.Close()

	config := newTelegramConfig("tok", "cid")
	req := provider.SendRequest{Body: "test"}
	resp, err := p.Send(context.Background(), config, req)

	require.NoError(t, err)
	assert.Equal(t, "99", resp.ProviderMsgID)
}

func TestExtractTelegramMessageID_MalformedJSON(t *testing.T) {
	// When the response body is malformed, the provider should still report
	// success but return an empty ProviderMsgID rather than an error.
	server, p := telegramTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{not valid json`))
	})
	defer server.Close()

	config := newTelegramConfig("tok", "cid")
	req := provider.SendRequest{Body: "test"}
	resp, err := p.Send(context.Background(), config, req)

	require.NoError(t, err)
	assert.True(t, resp.Success)
	assert.Empty(t, resp.ProviderMsgID)
}

func TestExtractTelegramMessageID_MissingResult(t *testing.T) {
	// ok=true but message_id is 0 (the zero value) means extraction returns "".
	server, p := telegramTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true,"result":{}}`))
	})
	defer server.Close()

	config := newTelegramConfig("tok", "cid")
	req := provider.SendRequest{Body: "test"}
	resp, err := p.Send(context.Background(), config, req)

	require.NoError(t, err)
	assert.True(t, resp.Success)
	assert.Empty(t, resp.ProviderMsgID)
}
