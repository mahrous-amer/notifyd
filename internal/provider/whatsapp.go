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

type WhatsAppProvider struct {
	client *http.Client
}

func NewWhatsAppProvider(client *http.Client) *WhatsAppProvider {
	return &WhatsAppProvider{client: client}
}

func (w *WhatsAppProvider) Type() string { return "whatsapp" }

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

func (w *WhatsAppProvider) Send(ctx context.Context, rawConfig json.RawMessage, req SendRequest) (*SendResponse, error) {
	var cfg whatsappConfig
	if err := json.Unmarshal(rawConfig, &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal whatsapp config: %w", err)
	}

	text := req.Body
	if req.Subject != "" {
		text = fmt.Sprintf("*%s*\n%s", req.Subject, req.Body)
	}

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

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return &SendResponse{Success: true, ProviderData: respBody}, nil
	}
	return &SendResponse{
		Success:      false,
		ProviderData: respBody,
		ErrorMessage: fmt.Sprintf("whatsapp API returned %d: %s", resp.StatusCode, string(respBody)),
	}, nil
}
