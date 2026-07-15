package notifyd

import (
	"context"
	"net/http"
)

// ListAPIKeys returns all API keys for the authenticated tenant. Secret
// hashes are never included.
func (c *Client) ListAPIKeys(ctx context.Context) ([]APIKey, error) {
	var out []APIKey
	err := c.doRequest(ctx, requestOptions{
		method: http.MethodGet,
		path:   "/keys",
		out:    &out,
	})
	return out, err
}

// CreateAPIKey creates a new API key with the given label. The returned
// APISecret is shown only in this response — save it immediately.
func (c *Client) CreateAPIKey(ctx context.Context, label string) (*CreateAPIKeyResponse, error) {
	var out CreateAPIKeyResponse
	err := c.doRequest(ctx, requestOptions{
		method: http.MethodPost,
		path:   "/keys",
		body:   map[string]string{"label": label},
		out:    &out,
	})
	return &out, err
}

// RevokeAPIKey revokes an API key by ID.
func (c *Client) RevokeAPIKey(ctx context.Context, keyID string) error {
	return c.doRequest(ctx, requestOptions{
		method:      http.MethodDelete,
		path:        "/keys/" + keyID,
		expectEmpty: true,
	})
}
