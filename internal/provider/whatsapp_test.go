package provider_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bse/notifyd/internal/provider"
)

// newWhatsAppConfig returns a JSON-encoded whatsappConfig.
func newWhatsAppConfig(phoneNumberID, accessToken, recipient string) json.RawMessage {
	raw, _ := json.Marshal(map[string]string{
		"phone_number_id": phoneNumberID,
		"access_token":    accessToken,
		"recipient":       recipient,
	})
	return raw
}

// whatsAppTestServer spins up a local httptest.Server and returns a
// WhatsAppProvider whose HTTP client transparently rewrites all outgoing
// requests to that server. This lets us intercept the calls the provider
// normally sends to graph.facebook.com.
func whatsAppTestServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *provider.WhatsAppProvider) {
	t.Helper()
	server := httptest.NewServer(handler)
	transport := &hostOverrideTransport{
		base:         http.DefaultTransport,
		targetOrigin: server.URL,
	}
	client := &http.Client{Transport: transport}
	return server, provider.NewWhatsAppProvider(client)
}

// --- Tests ---

func TestWhatsAppProvider_Type(t *testing.T) {
	p := provider.NewWhatsAppProvider(http.DefaultClient)
	assert.Equal(t, "whatsapp", p.Type())
}

func TestWhatsAppProvider_Capabilities_ReturnsReadReceiptsAndDeliveryStatus(t *testing.T) {
	p := provider.NewWhatsAppProvider(http.DefaultClient)
	caps := p.Capabilities()

	require.Len(t, caps.Capabilities, 2)
	assert.Contains(t, caps.Capabilities, provider.CapReadReceipts)
	assert.Contains(t, caps.Capabilities, provider.CapDeliveryStatus)
}

func TestWhatsAppProvider_ValidateConfig(t *testing.T) {
	p := provider.NewWhatsAppProvider(http.DefaultClient)

	t.Run("valid config", func(t *testing.T) {
		config := newWhatsAppConfig("phone-id-123", "token-abc", "+15551234567")
		err := p.ValidateConfig(config)
		require.NoError(t, err)
	})

	t.Run("missing phone_number_id", func(t *testing.T) {
		config := newWhatsAppConfig("", "token-abc", "+15551234567")
		err := p.ValidateConfig(config)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "phone_number_id is required")
	})

	t.Run("missing access_token", func(t *testing.T) {
		config := newWhatsAppConfig("phone-id-123", "", "+15551234567")
		err := p.ValidateConfig(config)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "access_token is required")
	})

	t.Run("missing recipient", func(t *testing.T) {
		config := newWhatsAppConfig("phone-id-123", "token-abc", "")
		err := p.ValidateConfig(config)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "recipient is required")
	})

	t.Run("invalid JSON", func(t *testing.T) {
		err := p.ValidateConfig(json.RawMessage(`{not valid json}`))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid whatsapp config")
	})
}

func TestWhatsAppProvider_Send_Success_ExtractsProviderMsgID(t *testing.T) {
	responseBody := `{"messages":[{"id":"wamid.abc123"}]}`
	server, p := whatsAppTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		assert.Contains(t, r.Header.Get("Authorization"), "Bearer")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(responseBody))
	})
	defer server.Close()

	config := newWhatsAppConfig("phone-id", "my-token", "+15550001111")
	req := provider.SendRequest{Subject: "Alert", Body: "Something happened"}

	resp, err := p.Send(context.Background(), config, req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Success)
	assert.Equal(t, "wamid.abc123", resp.ProviderMsgID)
}

func TestWhatsAppProvider_Send_Failure_Non2xxStatus(t *testing.T) {
	server, p := whatsAppTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"Invalid OAuth access token"}}`))
	})
	defer server.Close()

	config := newWhatsAppConfig("phone-id", "bad-token", "+15550001111")
	req := provider.SendRequest{Body: "test"}

	resp, err := p.Send(context.Background(), config, req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.False(t, resp.Success)
	assert.Contains(t, resp.ErrorMessage, "401")
}

func TestWhatsAppProvider_FetchMetrics_WithProviderMsgID(t *testing.T) {
	p := provider.NewWhatsAppProvider(http.DefaultClient)

	metrics, err := p.FetchMetrics(context.Background(), nil, "wamid.xyz")

	require.NoError(t, err)
	require.NotNil(t, metrics)
	assert.Equal(t, "wamid.xyz", metrics.ProviderMsgID)
}

func TestWhatsAppProvider_FetchMetrics_WithoutProviderMsgID(t *testing.T) {
	p := provider.NewWhatsAppProvider(http.DefaultClient)

	metrics, err := p.FetchMetrics(context.Background(), nil, "")

	assert.Nil(t, metrics)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "providerMsgID is required")
}

func TestBuildWhatsAppMessage_WithSubject(t *testing.T) {
	var capturedBody []byte
	server, p := whatsAppTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 4096)
		n, _ := r.Body.Read(buf)
		capturedBody = buf[:n]
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"messages":[{"id":"wamid.1"}]}`))
	})
	defer server.Close()

	config := newWhatsAppConfig("phone-id", "tok", "+1555")
	req := provider.SendRequest{Subject: "Important", Body: "Read this now"}
	_, err := p.Send(context.Background(), config, req)
	require.NoError(t, err)

	var payload map[string]interface{}
	require.NoError(t, json.Unmarshal(capturedBody, &payload))

	textBlock, ok := payload["text"].(map[string]interface{})
	require.True(t, ok, "expected text block in payload")
	assert.Equal(t, "Important\nRead this now", textBlock["body"])
}

func TestBuildWhatsAppMessage_WithoutSubject(t *testing.T) {
	var capturedBody []byte
	server, p := whatsAppTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 4096)
		n, _ := r.Body.Read(buf)
		capturedBody = buf[:n]
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"messages":[{"id":"wamid.2"}]}`))
	})
	defer server.Close()

	config := newWhatsAppConfig("phone-id", "tok", "+1555")
	req := provider.SendRequest{Body: "Only the body"}
	_, err := p.Send(context.Background(), config, req)
	require.NoError(t, err)

	var payload map[string]interface{}
	require.NoError(t, json.Unmarshal(capturedBody, &payload))

	textBlock, ok := payload["text"].(map[string]interface{})
	require.True(t, ok, "expected text block in payload")
	assert.Equal(t, "Only the body", textBlock["body"])
}

func TestExtractWhatsAppMessageID_ValidResponse(t *testing.T) {
	server, p := whatsAppTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"messages":[{"id":"wamid.validID"}]}`))
	})
	defer server.Close()

	config := newWhatsAppConfig("phone-id", "tok", "+1555")
	req := provider.SendRequest{Body: "test"}
	resp, err := p.Send(context.Background(), config, req)

	require.NoError(t, err)
	assert.Equal(t, "wamid.validID", resp.ProviderMsgID)
}

func TestExtractWhatsAppMessageID_EmptyMessagesArray(t *testing.T) {
	// An empty messages array means extraction returns "" but Send is still
	// reported as successful because the HTTP status was 2xx.
	server, p := whatsAppTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"messages":[]}`))
	})
	defer server.Close()

	config := newWhatsAppConfig("phone-id", "tok", "+1555")
	req := provider.SendRequest{Body: "test"}
	resp, err := p.Send(context.Background(), config, req)

	require.NoError(t, err)
	assert.True(t, resp.Success)
	assert.Empty(t, resp.ProviderMsgID)
}

func TestExtractWhatsAppMessageID_MalformedJSON(t *testing.T) {
	// Malformed response body should not crash the provider; ProviderMsgID is
	// empty but the overall Send outcome is still Success=true.
	server, p := whatsAppTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{not valid json`))
	})
	defer server.Close()

	config := newWhatsAppConfig("phone-id", "tok", "+1555")
	req := provider.SendRequest{Body: "test"}
	resp, err := p.Send(context.Background(), config, req)

	require.NoError(t, err)
	assert.True(t, resp.Success)
	assert.Empty(t, resp.ProviderMsgID)
}
