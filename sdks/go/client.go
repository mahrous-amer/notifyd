// Package notifyd is the official Go client for the notifyd notification
// delivery API (https://notifyd.fluxintek.com). It wraps token exchange,
// caching, and refresh-on-401 retry around plain HTTP calls, plus ergonomic
// methods for every resource the API exposes to tenants.
package notifyd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// DefaultBaseURL is the production notifyd API endpoint used when Config
// leaves BaseURL empty.
const DefaultBaseURL = "https://notifyd.fluxintek.com/api"

// Config holds the credentials and connection settings for a Client.
type Config struct {
	APIKey    string
	APISecret string

	// BaseURL overrides DefaultBaseURL. Mainly for testing against a local
	// server or a non-production environment.
	BaseURL string

	// HTTPClient overrides the default *http.Client. Mainly for testing.
	HTTPClient *http.Client
}

// Client is a notifyd API client. It is safe for concurrent use: token
// exchange is serialized internally so concurrent requests share one token
// instead of each triggering their own /auth/token call.
type Client struct {
	apiKey     string
	apiSecret  string
	baseURL    string
	httpClient *http.Client

	tokenMu     sync.Mutex
	cachedToken string
	expiresAt   time.Time
}

// New creates a Client from the given Config. APIKey and APISecret are
// required; BaseURL defaults to DefaultBaseURL and HTTPClient defaults to
// http.DefaultClient's timeout settings via a fresh client.
func New(cfg Config) (*Client, error) {
	if cfg.APIKey == "" || cfg.APISecret == "" {
		return nil, fmt.Errorf("notifyd: APIKey and APISecret are required")
	}
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{
		apiKey:     cfg.APIKey,
		apiSecret:  cfg.APISecret,
		baseURL:    baseURL,
		httpClient: httpClient,
	}, nil
}

// RequestError is returned for any non-2xx API response. StatusCode and
// Code (the machine-readable "error" field, when present) let callers
// branch on specific failure modes like QUOTA_EXCEEDED without parsing
// strings. Body holds the fully parsed JSON response, so error shapes that
// carry extra context beyond "error" -- QuotaExceededError's upgrade_url,
// SubscriptionExpiredError's renew_url -- are still reachable without the
// SDK needing a bespoke type for every error variant the API might add.
type RequestError struct {
	StatusCode int
	Code       string
	Message    string
	Body       map[string]any
}

func (e *RequestError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("notifyd: %d %s", e.StatusCode, e.Code)
	}
	return fmt.Sprintf("notifyd: %d %s", e.StatusCode, e.Message)
}

// Field returns a string-valued field from the parsed error body, such as
// upgrade_url on a 429 QUOTA_EXCEEDED or renew_url on a 402
// SUBSCRIPTION_PERIOD_EXPIRED. ok is false if the body has no such field,
// the response body wasn't JSON, or the field isn't a string.
func (e *RequestError) Field(key string) (value string, ok bool) {
	if e.Body == nil {
		return "", false
	}
	raw, present := e.Body[key]
	if !present {
		return "", false
	}
	value, ok = raw.(string)
	return value, ok
}

// tokenResponse mirrors the TokenResponse schema.
type tokenResponse struct {
	Token     string `json:"token"`
	ExpiresIn string `json:"expires_in"`
}

// authenticatedToken returns a cached JWT if it isn't near expiry, or
// exchanges credentials for a fresh one. The 30-second safety margin
// absorbs request latency between this check and the token actually being
// used, without relying solely on the refresh-on-401 fallback.
//
// tokenMu is deliberately held across the /auth/token HTTP round trip
// inside fetchAndCacheToken (not just around the cache read): concurrent
// callers that all observe a stale cache must queue behind the first
// exchange and then reuse its result, rather than each firing their own
// redundant request. See TestConcurrentCallersShareOneTokenExchange.
func (c *Client) authenticatedToken(ctx context.Context) (string, error) {
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()

	const expiryMargin = 30 * time.Second
	if c.cachedToken != "" && time.Now().Add(expiryMargin).Before(c.expiresAt) {
		return c.cachedToken, nil
	}
	return c.fetchAndCacheToken(ctx)
}

// fetchAndCacheToken exchanges the configured API key/secret for a new JWT
// and caches it. Callers must hold tokenMu.
func (c *Client) fetchAndCacheToken(ctx context.Context) (string, error) {
	body, err := json.Marshal(map[string]string{
		"api_key":    c.apiKey,
		"api_secret": c.apiSecret,
	})
	if err != nil {
		return "", fmt.Errorf("notifyd: encoding token request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/auth/token", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("notifyd: building token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("notifyd: token exchange: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("notifyd: reading token response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", requestErrorFromBody(resp.StatusCode, respBody)
	}

	var tr tokenResponse
	if err := json.Unmarshal(respBody, &tr); err != nil {
		return "", fmt.Errorf("notifyd: decoding token response: %w", err)
	}

	expiresIn, err := time.ParseDuration(tr.ExpiresIn)
	if err != nil {
		return "", fmt.Errorf("notifyd: unparseable expires_in %q: %w", tr.ExpiresIn, err)
	}

	c.cachedToken = tr.Token
	c.expiresAt = time.Now().Add(expiresIn)
	return c.cachedToken, nil
}

// forceRefreshToken discards the cached token and fetches a new one. Used
// by the refresh-on-401 retry path, where the cached token was presumably
// revoked or expired earlier than expires_in predicted (e.g. clock skew).
func (c *Client) forceRefreshToken(ctx context.Context) (string, error) {
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()
	c.cachedToken = ""
	return c.fetchAndCacheToken(ctx)
}

// requestOptions configures a single call to doRequest.
type requestOptions struct {
	method      string
	path        string
	query       url.Values
	body        any
	out         any
	expectEmpty bool
}

// doRequest sends one authenticated API call, retrying exactly once with a
// forced token refresh if the first attempt gets a 401. A second 401 after
// refresh means the credentials themselves are bad, not just the token, so
// it's surfaced as a normal RequestError instead of retrying again.
func (c *Client) doRequest(ctx context.Context, opts requestOptions) error {
	token, err := c.authenticatedToken(ctx)
	if err != nil {
		return err
	}

	resp, respBody, err := c.send(ctx, token, opts)
	if err != nil {
		return err
	}

	if resp.StatusCode == http.StatusUnauthorized {
		token, err = c.forceRefreshToken(ctx)
		if err != nil {
			return err
		}
		resp, respBody, err = c.send(ctx, token, opts)
		if err != nil {
			return err
		}
	}

	if resp.StatusCode >= 300 {
		return requestErrorFromBody(resp.StatusCode, respBody)
	}
	if opts.expectEmpty || opts.out == nil {
		return nil
	}
	if err := json.Unmarshal(respBody, opts.out); err != nil {
		return fmt.Errorf("notifyd: decoding response from %s %s: %w", opts.method, opts.path, err)
	}
	return nil
}

// send performs one bare HTTP round trip with no retry logic, returning the
// raw response and its fully-read body.
func (c *Client) send(ctx context.Context, token string, opts requestOptions) (*http.Response, []byte, error) {
	requestURL := c.baseURL + opts.path
	if len(opts.query) > 0 {
		requestURL += "?" + opts.query.Encode()
	}

	var bodyReader io.Reader
	if opts.body != nil {
		encoded, err := json.Marshal(opts.body)
		if err != nil {
			return nil, nil, fmt.Errorf("notifyd: encoding request body: %w", err)
		}
		bodyReader = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, opts.method, requestURL, bodyReader)
	if err != nil {
		return nil, nil, fmt.Errorf("notifyd: building request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if opts.body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("notifyd: %s %s: %w", opts.method, opts.path, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("notifyd: reading response body: %w", err)
	}
	return resp, respBody, nil
}

// requestErrorFromBody builds a *RequestError from a non-2xx response,
// keeping the full parsed body (so extra fields like upgrade_url/renew_url
// stay reachable via RequestError.Field) whenever it parses as a JSON
// object with a string "error" field. A body that isn't JSON (e.g. an
// upstream proxy's HTML error page) still produces a usable error with the
// raw body as Message.
func requestErrorFromBody(statusCode int, body []byte) *RequestError {
	var parsedBody map[string]any
	if err := json.Unmarshal(body, &parsedBody); err == nil {
		code, _ := parsedBody["error"].(string)
		if code != "" {
			return &RequestError{StatusCode: statusCode, Code: code, Body: parsedBody}
		}
	}
	return &RequestError{StatusCode: statusCode, Message: string(body)}
}
