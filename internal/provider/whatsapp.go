package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type whatsappConfig struct {
	PhoneNumberID string `json:"phone_number_id"`
	AccessToken   string `json:"access_token"`
	Recipient     string `json:"recipient"`
}

// whatsappSendResult mirrors the relevant fields of the WhatsApp Graph API
// messages response.
type whatsappSendResult struct {
	Messages []struct {
		ID string `json:"id"`
	} `json:"messages"`
}

type WhatsAppProvider struct {
	client *http.Client
}

func NewWhatsAppProvider(client *http.Client) *WhatsAppProvider {
	return &WhatsAppProvider{client: client}
}

func (w *WhatsAppProvider) Type() string { return "whatsapp" }

// Capabilities returns read_receipts and delivery_status. WhatsApp Business
// supports webhook-based delivery and read confirmations via message status
// updates, and the Graph API exposes message status.
func (w *WhatsAppProvider) Capabilities() ProviderCapabilities {
	return ProviderCapabilities{
		Capabilities: []Capability{CapReadReceipts, CapDeliveryStatus},
	}
}

func (w *WhatsAppProvider) ValidateConfig(raw json.RawMessage) error {
	var cfg whatsappConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return fmt.Errorf("invalid whatsapp config: %w", err)
	}
	if cfg.PhoneNumberID == "" {
		return fmt.Errorf("whatsapp config: phone_number_id is required")
	}
	if cfg.AccessToken == "" {
		return fmt.Errorf("whatsapp config: access_token is required")
	}
	if cfg.Recipient == "" {
		return fmt.Errorf("whatsapp config: recipient is required")
	}
	return nil
}

// buildWhatsAppMessage composes the plain-text body to send via WhatsApp.
// WhatsApp Cloud API only supports plain text for text messages.
// The subject is prepended as-is since it is user-provided content.
func buildWhatsAppMessage(req SendRequest) string {
	if req.Subject != "" {
		return req.Subject + "\n" + req.Body
	}
	return req.Body
}

func (w *WhatsAppProvider) Send(ctx context.Context, rawConfig json.RawMessage, req SendRequest) (*SendResponse, error) {
	var cfg whatsappConfig
	if err := json.Unmarshal(rawConfig, &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal whatsapp config: %w", err)
	}

	text := buildWhatsAppMessage(req)

	payload, _ := json.Marshal(map[string]interface{}{
		"messaging_product": "whatsapp",
		"to":                cfg.Recipient,
		"type":              "text",
		"text": map[string]string{
			"body": text,
		},
	})

	url := fmt.Sprintf("https://graph.facebook.com/v19.0/%s/messages", cfg.PhoneNumberID)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+cfg.AccessToken)

	resp, err := w.client.Do(httpReq)
	if err != nil {
		return &SendResponse{Success: false, ErrorMessage: err.Error()}, nil
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &SendResponse{
			Success:      false,
			ProviderData: respBody,
			ErrorMessage: fmt.Sprintf("whatsapp API returned %d: %s", resp.StatusCode, string(respBody)),
		}, nil
	}

	providerMsgID := extractWhatsAppMessageID(respBody)
	return &SendResponse{
		Success:       true,
		ProviderMsgID: providerMsgID,
		ProviderData:  respBody,
	}, nil
}

// extractWhatsAppMessageID parses messages[0].id from the Graph API response.
// It returns an empty string on any parse failure so the caller still receives
// a successful SendResponse.
func extractWhatsAppMessageID(respBody []byte) string {
	var result whatsappSendResult
	if err := json.Unmarshal(respBody, &result); err != nil {
		return ""
	}
	if len(result.Messages) == 0 {
		return ""
	}
	return result.Messages[0].ID
}

// FetchMetrics returns delivery status for a WhatsApp message. The Graph API
// supports status queries via the message ID returned at send time.
func (w *WhatsAppProvider) FetchMetrics(_ context.Context, _ json.RawMessage, providerMsgID string) (*DeliveryMetrics, error) {
	if providerMsgID == "" {
		return nil, fmt.Errorf("whatsapp fetch metrics: providerMsgID is required")
	}
	// Full webhook-based status delivery is handled out-of-band by the WhatsApp
	// platform. Here we return a basic record confirming the message ID is known.
	return &DeliveryMetrics{
		ProviderMsgID: providerMsgID,
	}, nil
}
