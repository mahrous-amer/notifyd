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

// newSlackConfig returns a JSON-encoded slack config pointing at the given
// webhook URL. Encoding is assumed to succeed since the input is controlled.
func newSlackConfig(webhookURL string) json.RawMessage {
	raw, _ := json.Marshal(map[string]string{"webhook_url": webhookURL})
	return raw
}

func TestSlackProvider_Type(t *testing.T) {
	p := provider.NewSlackProvider(http.DefaultClient)
	assert.Equal(t, "slack", p.Type())
}

func TestSlackProvider_Capabilities_ReturnsEmpty(t *testing.T) {
	p := provider.NewSlackProvider(http.DefaultClient)
	caps := p.Capabilities()
	assert.Empty(t, caps.Capabilities)
}

func TestSlackProvider_FetchMetrics_ReturnsErrMetricsNotSupported(t *testing.T) {
	p := provider.NewSlackProvider(http.DefaultClient)

	metrics, err := p.FetchMetrics(context.Background(), nil, "any-id")

	assert.Nil(t, metrics)
	assert.True(t, errors.Is(err, domain.ErrMetricsNotSupported))
}

func TestSlackProvider_ValidateConfig(t *testing.T) {
	p := provider.NewSlackProvider(http.DefaultClient)

	t.Run("valid hooks.slack.com URL", func(t *testing.T) {
		config := newSlackConfig("https://hooks.slack.com/services/T00/B00/xxx")
		require.NoError(t, p.ValidateConfig(config))
	})

	t.Run("missing webhook_url", func(t *testing.T) {
		config, _ := json.Marshal(map[string]string{"webhook_url": ""})
		err := p.ValidateConfig(config)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "webhook_url is required")
	})

	t.Run("rejects non-slack host", func(t *testing.T) {
		config := newSlackConfig("https://evil.example.com/services/T00/B00/xxx")
		err := p.ValidateConfig(config)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "hooks.slack.com")
	})

	t.Run("rejects plain http even on the right host", func(t *testing.T) {
		config := newSlackConfig("http://hooks.slack.com/services/T00/B00/xxx")
		err := p.ValidateConfig(config)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "hooks.slack.com")
	})

	t.Run("invalid JSON", func(t *testing.T) {
		err := p.ValidateConfig(json.RawMessage(`{not valid json}`))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid slack config")
	})
}

func TestSlackProvider_Send_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	p := provider.NewSlackProvider(server.Client())
	config := newSlackConfig(server.URL)
	req := provider.SendRequest{Subject: "Hello", Body: "World", FormatMode: "markdown"}

	resp, err := p.Send(context.Background(), config, req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Success)
	assert.Empty(t, resp.ProviderMsgID)
}

func TestSlackProvider_Send_ServerError_IsTransient(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal_error"))
	}))
	defer server.Close()

	p := provider.NewSlackProvider(server.Client())
	config := newSlackConfig(server.URL)
	req := provider.SendRequest{Body: "test"}

	resp, err := p.Send(context.Background(), config, req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.False(t, resp.Success)
	assert.False(t, resp.Permanent)
	assert.Contains(t, resp.ErrorMessage, "500")
}

func TestSlackProvider_Send_ClientError_IsPermanent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("invalid_payload"))
	}))
	defer server.Close()

	p := provider.NewSlackProvider(server.Client())
	config := newSlackConfig(server.URL)
	req := provider.SendRequest{Body: "test"}

	resp, err := p.Send(context.Background(), config, req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.False(t, resp.Success)
	assert.True(t, resp.Permanent)
	assert.Contains(t, resp.ErrorMessage, "400")
}

func TestSlackProvider_Send_NetworkError_IsTransient(t *testing.T) {
	p := provider.NewSlackProvider(http.DefaultClient)

	config := newSlackConfig("http://127.0.0.1:0/invalid-endpoint")
	req := provider.SendRequest{Body: "test"}

	resp, err := p.Send(context.Background(), config, req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.False(t, resp.Success)
	assert.False(t, resp.Permanent)
	assert.NotEmpty(t, resp.ErrorMessage)
}

// TestSlackProvider_Send_HTMLFormat_RejectedAsPermanent verifies that "html"
// FormatMode is rejected at send time rather than at ValidateConfig time:
// FormatMode comes from delivery preferences, which can be set per-channel or
// per-notification independently of the channel config, so the provider
// cannot know at config-validation time whether a future send will request
// html. Slack has no HTML rendering mode, so any html-formatted send can
// never succeed — that makes it a permanent failure, not a transient one.
func TestSlackProvider_Send_HTMLFormat_RejectedAsPermanent(t *testing.T) {
	var serverCalled bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverCalled = true
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	p := provider.NewSlackProvider(server.Client())
	config := newSlackConfig(server.URL)
	req := provider.SendRequest{Subject: "Hi", Body: "<b>bold</b>", FormatMode: "html"}

	resp, err := p.Send(context.Background(), config, req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.False(t, resp.Success)
	assert.True(t, resp.Permanent)
	assert.Contains(t, resp.ErrorMessage, "html")
	assert.False(t, serverCalled, "must not call Slack when the format is unsendable")
}

func TestBuildSlackMessage_Subject(t *testing.T) {
	t.Run("markdown mode bolds the subject as a first line", func(t *testing.T) {
		var captured map[string]string
		server := captureSlackPayload(t, &captured)
		defer server.Close()

		p := provider.NewSlackProvider(server.Client())
		req := provider.SendRequest{Subject: "My Title", Body: "My body text", FormatMode: "markdown"}
		_, err := p.Send(context.Background(), newSlackConfig(server.URL), req)
		require.NoError(t, err)

		assert.Equal(t, "*My Title*\nMy body text", captured["text"])
	})

	t.Run("plain mode includes the subject undecorated", func(t *testing.T) {
		var captured map[string]string
		server := captureSlackPayload(t, &captured)
		defer server.Close()

		p := provider.NewSlackProvider(server.Client())
		req := provider.SendRequest{Subject: "My Title", Body: "My body text", FormatMode: "plain"}
		_, err := p.Send(context.Background(), newSlackConfig(server.URL), req)
		require.NoError(t, err)

		assert.Equal(t, "My Title\nMy body text", captured["text"])
	})

	t.Run("no subject sends body only", func(t *testing.T) {
		var captured map[string]string
		server := captureSlackPayload(t, &captured)
		defer server.Close()

		p := provider.NewSlackProvider(server.Client())
		req := provider.SendRequest{Body: "Only the body", FormatMode: "markdown"}
		_, err := p.Send(context.Background(), newSlackConfig(server.URL), req)
		require.NoError(t, err)

		assert.Equal(t, "Only the body", captured["text"])
	})
}

// captureSlackPayload starts a test server that decodes each request body
// into dest and always replies 200 OK, mimicking a healthy Slack webhook.
func captureSlackPayload(t *testing.T, dest *map[string]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(dest)
		w.WriteHeader(http.StatusOK)
	}))
}

func TestMarkdownToSlackMrkdwn(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "bold",
			input:    "This is **bold** text",
			expected: "This is *bold* text",
		},
		{
			name:     "italic underscore",
			input:    "This is _italic_ text",
			expected: "This is _italic_ text",
		},
		{
			name:     "italic asterisk converts to underscore",
			input:    "This is *italic* text",
			expected: "This is _italic_ text",
		},
		{
			name:     "inline code passes through",
			input:    "Run `go test` now",
			expected: "Run `go test` now",
		},
		{
			name:     "link converts to slack angle-bracket form",
			input:    "See [the docs](https://example.com/docs) for more",
			expected: "See <https://example.com/docs|the docs> for more",
		},
		{
			name:     "bold and link together",
			input:    "**Important**: read [this](https://example.com)",
			expected: "*Important*: read <https://example.com|this>",
		},
		{
			name:     "plain text unaffected",
			input:    "Nothing special here",
			expected: "Nothing special here",
		},
		{
			name:     "multiple bold spans",
			input:    "**one** and **two**",
			expected: "*one* and *two*",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, provider.MarkdownToSlackMrkdwn(tt.input))
		})
	}
}
