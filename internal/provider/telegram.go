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

type telegramConfig struct {
	BotToken string `json:"bot_token"`
	ChatID   string `json:"chat_id"`
}

// telegramSendResult mirrors the relevant fields of the Telegram sendMessage response.
type telegramSendResult struct {
	OK     bool `json:"ok"`
	Result struct {
		MessageID int `json:"message_id"`
	} `json:"result"`
}

type TelegramProvider struct {
	client *http.Client
}

func NewTelegramProvider(client *http.Client) *TelegramProvider {
	return &TelegramProvider{client: client}
}

func (t *TelegramProvider) Type() string { return "telegram" }

// Capabilities returns delivery_status. Telegram confirms message receipt
// synchronously in the sendMessage response, so we can report basic delivery.
func (t *TelegramProvider) Capabilities() ProviderCapabilities {
	return ProviderCapabilities{
		Capabilities: []Capability{CapDeliveryStatus},
	}
}

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

// buildTelegramMessage returns the message text and the Telegram parse_mode value
// based on the request's FormatMode. For "plain", parse_mode is omitted (empty
// string) so Telegram renders the text as-is. For "html", it uses HTML mode.
// All other values (including the empty default) use Markdown for backwards
// compatibility with the existing bold-subject formatting.
func buildTelegramMessage(req SendRequest) (text string, parseMode string) {
	switch req.FormatMode {
	case "plain":
		text = req.Body
		if req.Subject != "" {
			text = req.Subject + "\n" + req.Body
		}
		return text, ""
	case "html":
		text = req.Body
		if req.Subject != "" {
			text = fmt.Sprintf("<b>%s</b>\n%s", req.Subject, req.Body)
		}
		return text, "HTML"
	default: // "markdown" or ""
		text = req.Body
		if req.Subject != "" {
			text = fmt.Sprintf("*%s*\n%s", req.Subject, req.Body)
		}
		return text, "Markdown"
	}
}

func (t *TelegramProvider) Send(ctx context.Context, rawConfig json.RawMessage, req SendRequest) (*SendResponse, error) {
	var cfg telegramConfig
	if err := json.Unmarshal(rawConfig, &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal telegram config: %w", err)
	}

	text, parseMode := buildTelegramMessage(req)
	telegramPayload := map[string]string{
		"chat_id": cfg.ChatID,
		"text":    text,
	}
	if parseMode != "" {
		telegramPayload["parse_mode"] = parseMode
	}
	payload, _ := json.Marshal(telegramPayload)

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
	defer resp.Body.Close() //nolint:errcheck
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	if resp.StatusCode != http.StatusOK {
		return &SendResponse{
			Success:      false,
			ProviderData: respBody,
			ErrorMessage: fmt.Sprintf("telegram API returned %d: %s", resp.StatusCode, string(respBody)),
		}, nil
	}

	providerMsgID := extractTelegramMessageID(respBody)
	return &SendResponse{
		Success:       true,
		ProviderMsgID: providerMsgID,
		ProviderData:  respBody,
	}, nil
}

// extractTelegramMessageID parses the message_id from a Telegram sendMessage
// response body. It returns an empty string when parsing fails so the caller
// still gets a successful SendResponse.
func extractTelegramMessageID(respBody []byte) string {
	var result telegramSendResult
	if err := json.Unmarshal(respBody, &result); err != nil {
		return ""
	}
	if !result.OK || result.Result.MessageID == 0 {
		return ""
	}
	return fmt.Sprintf("%d", result.Result.MessageID)
}

// FetchMetrics returns a basic delivery confirmation for Telegram. Telegram's
// Bot API does not expose a read-receipt endpoint, but a successfully sent
// message is considered delivered.
func (t *TelegramProvider) FetchMetrics(_ context.Context, _ json.RawMessage, providerMsgID string) (*DeliveryMetrics, error) {
	if providerMsgID == "" {
		return nil, domain.ErrMetricsNotSupported
	}
	// Telegram has no polling API for read status; we confirm delivery only.
	return &DeliveryMetrics{
		ProviderMsgID: providerMsgID,
	}, nil
}
