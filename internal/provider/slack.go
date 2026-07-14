package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/bse/notifyd/internal/domain"
)

// slackWebhookPrefix is the only host Slack issues incoming-webhook URLs
// under. Requiring it at validation time rejects copy-paste mistakes (a
// Discord URL pasted into the wrong field, an internal URL, etc.) before the
// channel is ever saved.
const slackWebhookPrefix = "https://hooks.slack.com/"

type slackConfig struct {
	WebhookURL string `json:"webhook_url"`
}

type SlackProvider struct {
	client *http.Client
}

func NewSlackProvider(client *http.Client) *SlackProvider {
	return &SlackProvider{client: client}
}

func (s *SlackProvider) Type() string { return "slack" }

// Capabilities returns an empty set. Slack incoming webhooks are
// fire-and-forget; they do not support read receipts or delivery status
// polling.
func (s *SlackProvider) Capabilities() ProviderCapabilities {
	return ProviderCapabilities{}
}

func (s *SlackProvider) ValidateConfig(raw json.RawMessage) error {
	var cfg slackConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return fmt.Errorf("invalid slack config: %w", err)
	}
	if cfg.WebhookURL == "" {
		return fmt.Errorf("slack config: webhook_url is required")
	}
	if !strings.HasPrefix(cfg.WebhookURL, slackWebhookPrefix) {
		return fmt.Errorf("slack config: webhook_url must start with %s", slackWebhookPrefix)
	}
	return nil
}

func (s *SlackProvider) Send(ctx context.Context, rawConfig json.RawMessage, req SendRequest) (*SendResponse, error) {
	var cfg slackConfig
	if err := json.Unmarshal(rawConfig, &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal slack config: %w", err)
	}

	if req.FormatMode == "html" {
		// Slack incoming webhooks render mrkdwn only; there is no HTML mode.
		// This can never succeed regardless of retry, so it's a permanent
		// failure rather than a transient one. Checked at send time (not
		// ValidateConfig) because FormatMode comes from delivery preferences,
		// which are independent of the channel config being validated.
		return &SendResponse{
			Success:      false,
			Permanent:    true,
			ErrorMessage: "slack: html format is not supported; use markdown or plain",
		}, nil
	}

	payload, _ := json.Marshal(map[string]string{"text": buildSlackMessage(req)})

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.WebhookURL, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(httpReq)
	if err != nil {
		return &SendResponse{Success: false, ErrorMessage: err.Error()}, nil
	}
	defer resp.Body.Close() //nolint:errcheck
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return &SendResponse{Success: true, ProviderData: respBody}, nil
	}
	return &SendResponse{
		Success:      false,
		ProviderData: respBody,
		ErrorMessage: fmt.Sprintf("slack API returned %d: %s", resp.StatusCode, string(respBody)),
		// 429 and 5xx are the server telling us to back off or that it had a
		// transient problem; everything else (400 bad payload, 404 revoked
		// webhook) will fail identically on every retry.
		Permanent: resp.StatusCode != http.StatusTooManyRequests && resp.StatusCode < 500,
	}, nil
}

// FetchMetrics always returns ErrMetricsNotSupported because Slack incoming
// webhooks offer no mechanism to query delivery or read status after
// sending.
func (s *SlackProvider) FetchMetrics(_ context.Context, _ json.RawMessage, _ string) (*DeliveryMetrics, error) {
	return nil, domain.ErrMetricsNotSupported
}

// buildSlackMessage composes the Slack message text, adjusting subject
// formatting based on the requested FormatMode. "markdown" bolds the subject
// as a first line in Slack mrkdwn and converts the body from CommonMark;
// "plain" (and any other non-html mode) includes the subject undecorated and
// leaves the body untouched. html is rejected before this is called.
func buildSlackMessage(req SendRequest) string {
	body := req.Body
	if req.FormatMode == "markdown" {
		body = MarkdownToSlackMrkdwn(req.Body)
	}

	if req.Subject == "" {
		return body
	}

	subject := req.Subject
	if req.FormatMode == "markdown" {
		subject = fmt.Sprintf("*%s*", req.Subject)
	}
	return subject + "\n" + body
}

// Slack mrkdwn differs from CommonMark in three ways this conversion covers:
// bold uses single asterisks instead of double, italic uses underscores
// instead of asterisks, and links use "<url|text>" instead of "[text](url)".
// Inline code (`code`) is identical in both and needs no conversion.
var (
	boldPattern   = regexp.MustCompile(`\*\*(.+?)\*\*`)
	italicPattern = regexp.MustCompile(`\*(.+?)\*`)
	linkPattern   = regexp.MustCompile(`\[(.+?)\]\((\S+?)\)`)
)

// boldPlaceholder brackets a converted bold span so the italic pass (which
// also matches single asterisks) skips over it instead of treating the
// bold markers as its own delimiters. U+E000 is in Unicode's Private Use
// Area, so it cannot appear in ordinary notification text.
const boldPlaceholder = ""

// MarkdownToSlackMrkdwn converts a CommonMark-ish subset (bold, italic,
// inline code, links) to Slack's mrkdwn syntax. Unrecognized syntax passes
// through unchanged. Exported for table-driven testing and reuse by callers
// that need the conversion independent of a full Send call.
func MarkdownToSlackMrkdwn(text string) string {
	// Links first: rewriting "[text](url)" before the bold/italic passes
	// avoids the asterisk patterns matching characters inside the URL.
	text = linkPattern.ReplaceAllString(text, "<$2|$1>")

	// Bold before italic, with the result fenced in placeholders: mrkdwn
	// bold ("*x*") and CommonMark italic ("*x*") use the identical
	// delimiter, so without protection the italic pass below would
	// immediately re-match and mangle every bold span it just produced.
	text = boldPattern.ReplaceAllString(text, boldPlaceholder+"${1}"+boldPlaceholder)
	text = italicPattern.ReplaceAllString(text, "_${1}_")
	text = strings.ReplaceAll(text, boldPlaceholder, "*")

	return text
}
