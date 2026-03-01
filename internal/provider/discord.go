package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/bse/notifyd/internal/domain"
)

type discordConfig struct {
	WebhookURL string `json:"webhook_url"`
}

type DiscordProvider struct {
	client *http.Client
}

func NewDiscordProvider(client *http.Client) *DiscordProvider {
	return &DiscordProvider{client: client}
}

func (d *DiscordProvider) Type() string { return "discord" }

// Capabilities returns an empty set. Discord webhooks are fire-and-forget;
// they do not support read receipts or delivery status polling.
func (d *DiscordProvider) Capabilities() ProviderCapabilities {
	return ProviderCapabilities{}
}

func (d *DiscordProvider) ValidateConfig(raw json.RawMessage) error {
	var cfg discordConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return fmt.Errorf("invalid discord config: %w", err)
	}
	if cfg.WebhookURL == "" {
		return fmt.Errorf("discord config: webhook_url is required")
	}
	return nil
}

// buildDiscordMessage composes the Discord message content string, adjusting
// subject formatting based on the requested FormatMode. Discord supports
// markdown natively, so the default is to bold the subject with **. For
// "plain", the subject is included without any markdown decoration. For "html",
// the subject is also sent without decoration because Discord does not render
// HTML markup.
func buildDiscordMessage(req SendRequest) string {
	if req.Subject == "" {
		return req.Body
	}

	switch req.FormatMode {
	case "plain", "html":
		return req.Subject + "\n" + req.Body
	default: // "markdown" or ""
		return fmt.Sprintf("**%s**\n%s", req.Subject, req.Body)
	}
}

func (d *DiscordProvider) Send(ctx context.Context, rawConfig json.RawMessage, req SendRequest) (*SendResponse, error) {
	var cfg discordConfig
	if err := json.Unmarshal(rawConfig, &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal discord config: %w", err)
	}

	content := buildDiscordMessage(req)

	payload, _ := json.Marshal(map[string]string{"content": content})

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.WebhookURL, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := d.client.Do(httpReq)
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
		ErrorMessage: fmt.Sprintf("discord API returned %d: %s", resp.StatusCode, string(respBody)),
	}, nil
}

// FetchMetrics always returns ErrMetricsNotSupported because Discord webhooks
// offer no mechanism to query delivery or read status after sending.
func (d *DiscordProvider) FetchMetrics(_ context.Context, _ json.RawMessage, _ string) (*DeliveryMetrics, error) {
	return nil, domain.ErrMetricsNotSupported
}
