package provider

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/bse/notifyd/internal/domain"
)

// webhookDialTimeout bounds DNS resolution and TCP connect for a single
// dial attempt. Kept short because a webhook target that can't be reached
// quickly is not worth blocking a worker slot for.
const webhookDialTimeout = 10 * time.Second

// webhookRequestTimeout bounds the entire request including the receiver's
// processing time, independent of any deadline the caller's context sets.
const webhookRequestTimeout = 30 * time.Second

type webhookConfig struct {
	URL     string            `json:"url"`
	Secret  string            `json:"secret,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

// webhookHeaderBlockPattern matches header names the tenant must not be able
// to set: anything in the X-Notifyd-* family, which this provider uses for
// its own signature/timestamp headers. Letting a tenant-supplied header
// collide would let them forge or shadow the signature.
var webhookHeaderBlockPattern = regexp.MustCompile(`(?i)^x-notifyd`)

// WebhookProvider posts a JSON payload to a tenant-supplied HTTPS endpoint.
// It is notifyd's most powerful and most dangerous channel: unlike Discord,
// Slack, or email, the destination is an arbitrary URL under tenant control,
// which makes it a natural SSRF vector against the network notifyd itself
// runs on. Every dial goes through guardedDialContext (see ssrf_guard.go),
// and redirects are never followed (see newWebhookHTTPClient).
type WebhookProvider struct {
	client *http.Client
}

func NewWebhookProvider() *WebhookProvider {
	return &WebhookProvider{client: newWebhookHTTPClient()}
}

// NewWebhookProviderWithClient builds a WebhookProvider around a caller-
// supplied *http.Client instead of the SSRF-guarded default.
//
// This exists only so tests can exercise Send's request-building,
// signing, and status classification against an httptest.Server, which
// always listens on a loopback address — exactly what the production
// dialer refuses to connect to. Do not call this outside tests: it has no
// SSRF protection at all. cmd/api and cmd/worker must always wire up
// NewWebhookProvider.
func NewWebhookProviderWithClient(client *http.Client) *WebhookProvider {
	return &WebhookProvider{client: client}
}

// newWebhookHTTPClient builds the http.Client used for all webhook sends.
// Two properties make this safe to point at tenant-supplied URLs:
//
//  1. DialContext is replaced with guardedDialContext, which validates the
//     resolved address (not just the hostname) before every connection —
//     including connections opened while following a redirect, since a
//     redirect reuses the same Transport/DialContext.
//  2. CheckRedirect refuses to follow any redirect at all. A legitimate
//     webhook receiver has no reason to redirect a POST; disabling redirects
//     outright is simpler and strictly safer than re-validating each hop.
func newWebhookHTTPClient() *http.Client {
	dialer := &net.Dialer{Timeout: webhookDialTimeout}
	transport := &http.Transport{
		DialContext: guardedDialContext(dialer),
	}
	return &http.Client{
		Transport: transport,
		Timeout:   webhookRequestTimeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func (w *WebhookProvider) Type() string { return "webhook" }

// Capabilities returns an empty set. Generic webhooks have no standard
// delivery or read receipt mechanism to poll.
func (w *WebhookProvider) Capabilities() ProviderCapabilities {
	return ProviderCapabilities{}
}

// FetchMetrics always returns ErrMetricsNotSupported: an arbitrary receiver
// offers no common protocol for querying delivery or read status.
func (w *WebhookProvider) FetchMetrics(_ context.Context, _ json.RawMessage, _ string) (*DeliveryMetrics, error) {
	return nil, domain.ErrMetricsNotSupported
}

func (w *WebhookProvider) ValidateConfig(raw json.RawMessage) error {
	var cfg webhookConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return fmt.Errorf("invalid webhook config: %w", err)
	}
	return validateWebhookConfig(cfg)
}

func validateWebhookConfig(cfg webhookConfig) error {
	if cfg.URL == "" {
		return fmt.Errorf("webhook config: url is required")
	}
	if !strings.HasPrefix(cfg.URL, "https://") {
		return fmt.Errorf("webhook config: url must use https")
	}
	return validateWebhookHeaderNames(cfg.Headers, cfg.Secret != "")
}

// validateWebhookHeaderNames rejects tenant-supplied header names that could
// let a webhook config hijack the request in ways the sender doesn't intend:
//   - Host: would not actually change the connection target (net/http
//     controls that separately) but is nonsensical and a sign of a
//     misconfigured or malicious config.
//   - Authorization: when a secret is configured, this provider signs the
//     request itself; letting the tenant also set Authorization is a
//     confusing double meaning and is rejected outright.
//   - X-Notifyd-*: reserved for this provider's own signature/timestamp
//     headers (see signWebhookPayload). Allowing a tenant override would let
//     them shadow or forge the signature the receiver is meant to trust.
func validateWebhookHeaderNames(headers map[string]string, hasSecret bool) error {
	for name := range headers {
		lower := strings.ToLower(name)
		if lower == "host" {
			return fmt.Errorf("webhook config: header %q is not allowed", name)
		}
		if hasSecret && lower == "authorization" {
			return fmt.Errorf("webhook config: header %q is not allowed when secret is set", name)
		}
		if webhookHeaderBlockPattern.MatchString(name) {
			return fmt.Errorf("webhook config: header %q is reserved for notifyd's own use", name)
		}
	}
	return nil
}

// webhookPayload is the JSON body posted to every webhook receiver.
type webhookPayload struct {
	NotificationID string          `json:"notification_id"`
	Subject        string          `json:"subject"`
	Body           string          `json:"body"`
	Format         string          `json:"format"`
	Metadata       json.RawMessage `json:"metadata"`
	SentAt         string          `json:"sent_at"`
}

func buildWebhookPayload(req SendRequest) ([]byte, error) {
	metadata := req.Metadata
	if metadata == nil {
		metadata = json.RawMessage("{}")
	}
	payload := webhookPayload{
		NotificationID: req.NotificationID.String(),
		Subject:        req.Subject,
		Body:           req.Body,
		Format:         req.FormatMode,
		Metadata:       metadata,
		SentAt:         time.Now().UTC().Format(time.RFC3339),
	}
	return json.Marshal(payload)
}

// signWebhookPayload computes the HMAC-SHA256 signature notifyd sends
// alongside a signed webhook request. The signature covers
// "timestamp.body" rather than just body so a captured (timestamp,
// signature, body) triple cannot be replayed under a different timestamp —
// the receiver is expected to reject requests whose timestamp is too old,
// which only works if the timestamp itself is part of what's signed.
func signWebhookPayload(secret, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func (w *WebhookProvider) Send(ctx context.Context, rawConfig json.RawMessage, req SendRequest) (*SendResponse, error) {
	var cfg webhookConfig
	if err := json.Unmarshal(rawConfig, &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal webhook config: %w", err)
	}

	body, err := buildWebhookPayload(req)
	if err != nil {
		return nil, fmt.Errorf("build webhook payload: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.URL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	for name, value := range cfg.Headers {
		httpReq.Header.Set(name, value)
	}
	if cfg.Secret != "" {
		timestamp := strconv.FormatInt(time.Now().Unix(), 10)
		signature := signWebhookPayload(cfg.Secret, timestamp, body)
		httpReq.Header.Set("X-Notifyd-Timestamp", timestamp)
		httpReq.Header.Set("X-Notifyd-Signature", "sha256="+signature)
	}

	resp, err := w.client.Do(httpReq)
	if err != nil {
		return classifyWebhookTransportError(err), nil
	}
	defer resp.Body.Close() //nolint:errcheck
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return &SendResponse{Success: true, ProviderData: respBody}, nil
	}
	if isRedirectStatus(resp.StatusCode) {
		return &SendResponse{
			Success:      false,
			Permanent:    true,
			ErrorMessage: fmt.Sprintf("webhook returned redirect status %d; redirects are not followed", resp.StatusCode),
		}, nil
	}
	return &SendResponse{
		Success:      false,
		ProviderData: respBody,
		ErrorMessage: fmt.Sprintf("webhook endpoint returned %d: %s", resp.StatusCode, string(respBody)),
		// 429 and 5xx signal a transient problem on the receiver's side;
		// any other 4xx (bad request, unauthorized, not found) will fail
		// identically on every retry with an unchanged payload.
		Permanent: resp.StatusCode != http.StatusTooManyRequests && resp.StatusCode < 500,
	}, nil
}

func isRedirectStatus(statusCode int) bool {
	return statusCode >= 300 && statusCode < 400
}

// classifyWebhookTransportError turns a transport-level failure (DNS,
// connect, TLS, timeout, or the SSRF guard) into a SendResponse. An address
// the SSRF guard refused can never succeed on retry — the destination host
// resolves to a blocked range regardless of how many times we try — so that
// case is marked permanent. Every other transport failure (timeout,
// connection refused, TLS handshake failure) is treated as transient: the
// same request may well succeed once the network or receiver recovers.
func classifyWebhookTransportError(err error) *SendResponse {
	return &SendResponse{
		Success:      false,
		ErrorMessage: err.Error(),
		Permanent:    errors.Is(err, errBlockedAddress),
	}
}
