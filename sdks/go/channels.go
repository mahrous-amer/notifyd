package notifyd

import (
	"context"
	"net/http"
)

// ListChannels returns all channel configs belonging to the authenticated
// tenant.
func (c *Client) ListChannels(ctx context.Context) ([]ChannelConfig, error) {
	var out []ChannelConfig
	err := c.doRequest(ctx, requestOptions{
		method: http.MethodGet,
		path:   "/channels",
		out:    &out,
	})
	return out, err
}

// CreateChannel creates a new channel config for the authenticated tenant.
func (c *Client) CreateChannel(ctx context.Context, input CreateChannelInput) (*ChannelConfig, error) {
	var out ChannelConfig
	err := c.doRequest(ctx, requestOptions{
		method: http.MethodPost,
		path:   "/channels",
		body:   input,
		out:    &out,
	})
	return &out, err
}

// GetChannel fetches one channel config by ID.
func (c *Client) GetChannel(ctx context.Context, channelID string) (*ChannelConfig, error) {
	var out ChannelConfig
	err := c.doRequest(ctx, requestOptions{
		method: http.MethodGet,
		path:   "/channels/" + channelID,
		out:    &out,
	})
	return &out, err
}

// UpdateChannel updates a channel config. Fields left nil in input are not
// sent, so the existing value is preserved server-side.
func (c *Client) UpdateChannel(ctx context.Context, channelID string, input UpdateChannelInput) (*ChannelConfig, error) {
	var out ChannelConfig
	err := c.doRequest(ctx, requestOptions{
		method: http.MethodPatch,
		path:   "/channels/" + channelID,
		body:   input,
		out:    &out,
	})
	return &out, err
}

// DeleteChannel deletes a channel config by ID.
func (c *Client) DeleteChannel(ctx context.Context, channelID string) error {
	return c.doRequest(ctx, requestOptions{
		method:      http.MethodDelete,
		path:        "/channels/" + channelID,
		expectEmpty: true,
	})
}
