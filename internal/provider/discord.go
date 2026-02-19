package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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

func (d *DiscordProvider) Send(ctx context.Context, rawConfig json.RawMessage, req SendRequest) (*SendResponse, error) {
	var cfg discordConfig
	if err := json.Unmarshal(rawConfig, &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal discord config: %w", err)
	}

	content := req.Body
	if req.Subject != "" {
		content = fmt.Sprintf("**%s**\n%s", req.Subject, req.Body)
	}

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
