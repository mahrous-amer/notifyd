package notifyd

import (
	"context"
	"net/http"
)

// ListWebhooks returns all status-webhook endpoints belonging to the
// authenticated tenant. Signing secrets are never included.
func (c *Client) ListWebhooks(ctx context.Context) ([]WebhookEndpoint, error) {
	var out []WebhookEndpoint
	err := c.doRequest(ctx, requestOptions{
		method: http.MethodGet,
		path:   "/webhooks",
		out:    &out,
	})
	return out, err
}

// CreateWebhook registers a new destination for notification.delivered /
// notification.failed status events. The returned Secret is shown only in
// this response — save it immediately, then verify incoming deliveries
// with VerifyWebhookSignature.
func (c *Client) CreateWebhook(ctx context.Context, input CreateWebhookInput) (*WebhookEndpointCreated, error) {
	var out WebhookEndpointCreated
	err := c.doRequest(ctx, requestOptions{
		method: http.MethodPost,
		path:   "/webhooks",
		body:   input,
		out:    &out,
	})
	return &out, err
}

// UpdateWebhook updates a webhook endpoint. Fields left nil in input are
// not sent, so the existing value is preserved server-side. Never returns
// the secret.
func (c *Client) UpdateWebhook(ctx context.Context, webhookID string, input UpdateWebhookInput) (*WebhookEndpoint, error) {
	var out WebhookEndpoint
	err := c.doRequest(ctx, requestOptions{
		method: http.MethodPut,
		path:   "/webhooks/" + webhookID,
		body:   input,
		out:    &out,
	})
	return &out, err
}

// DeleteWebhook deletes a webhook endpoint by ID.
func (c *Client) DeleteWebhook(ctx context.Context, webhookID string) error {
	return c.doRequest(ctx, requestOptions{
		method:      http.MethodDelete,
		path:        "/webhooks/" + webhookID,
		expectEmpty: true,
	})
}
