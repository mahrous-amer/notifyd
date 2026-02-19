package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type telegramConfig struct {
	BotToken string `json:"bot_token"`
	ChatID   string `json:"chat_id"`
}

type TelegramProvider struct {
	client *http.Client
}

func NewTelegramProvider(client *http.Client) *TelegramProvider {
	return &TelegramProvider{client: client}
}

func (t *TelegramProvider) Type() string { return "telegram" }

func (t *TelegramProvider) ValidateConfig(raw json.RawMessage) error {
	var cfg telegramConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return fmt.Errorf("invalid telegram config: %w", err)
	}
	if cfg.BotToken == "" {
		return fmt.Errorf("telegram config: bot_token is required")
	}
	if cfg.ChatID == "" {
		return fmt.Errorf("telegram config: chat_id is required")
	}
	return nil
}

func (t *TelegramProvider) Send(ctx context.Context, rawConfig json.RawMessage, req SendRequest) (*SendResponse, error) {
	var cfg telegramConfig
	if err := json.Unmarshal(rawConfig, &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal telegram config: %w", err)
	}

	text := req.Body
	if req.Subject != "" {
		text = fmt.Sprintf("*%s*\n%s", req.Subject, req.Body)
	}

	payload, _ := json.Marshal(map[string]string{
		"chat_id":    cfg.ChatID,
		"text":       text,
		"parse_mode": "Markdown",
	})

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", cfg.BotToken)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := t.client.Do(httpReq)
	if err != nil {
		return &SendResponse{Success: false, ErrorMessage: err.Error()}, nil
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	if resp.StatusCode == http.StatusOK {
		return &SendResponse{Success: true, ProviderData: respBody}, nil
	}
	return &SendResponse{
		Success:      false,
		ProviderData: respBody,
		ErrorMessage: fmt.Sprintf("telegram API returned %d: %s", resp.StatusCode, string(respBody)),
	}, nil
}
