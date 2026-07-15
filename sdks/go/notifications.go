package notifyd

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
)

// Send enqueues a single notification for async delivery. It returns
// immediately with the notification in "pending" or "processing" status;
// poll GetNotification for the terminal outcome.
func (c *Client) Send(ctx context.Context, input SendInput) (*Notification, error) {
	var out Notification
	err := c.doRequest(ctx, requestOptions{
		method: http.MethodPost,
		path:   "/notifications/send",
		body:   input,
		out:    &out,
	})
	return &out, err
}

// SendMulti enqueues notifications across up to 50 channel configs in one
// request. Partial success is expected: check the returned Errors slice
// even when the call itself doesn't return an error.
func (c *Client) SendMulti(ctx context.Context, inputs []SendInput) (*SendMultiResponse, error) {
	var out SendMultiResponse
	err := c.doRequest(ctx, requestOptions{
		method: http.MethodPost,
		path:   "/notifications/send-multi",
		body:   map[string]any{"channels": inputs},
		out:    &out,
	})
	return &out, err
}

// ListNotifications returns a page of the authenticated tenant's
// notifications, optionally filtered by status and/or channel.
func (c *Client) ListNotifications(ctx context.Context, params ListNotificationsParams) (*NotificationList, error) {
	query := url.Values{}
	if params.Limit > 0 {
		query.Set("limit", strconv.Itoa(params.Limit))
	}
	if params.Offset > 0 {
		query.Set("offset", strconv.Itoa(params.Offset))
	}
	if params.Status != "" {
		query.Set("status", string(params.Status))
	}
	if params.Channel != "" {
		query.Set("channel", string(params.Channel))
	}

	var out NotificationList
	err := c.doRequest(ctx, requestOptions{
		method: http.MethodGet,
		path:   "/notifications",
		query:  query,
		out:    &out,
	})
	return &out, err
}

// GetNotification fetches one notification by ID.
func (c *Client) GetNotification(ctx context.Context, notificationID string) (*Notification, error) {
	var out Notification
	err := c.doRequest(ctx, requestOptions{
		method: http.MethodGet,
		path:   "/notifications/" + notificationID,
		out:    &out,
	})
	return &out, err
}

// ListAttempts returns every delivery attempt recorded for a notification,
// ordered by attempt number.
func (c *Client) ListAttempts(ctx context.Context, notificationID string) ([]DeliveryAttempt, error) {
	var out []DeliveryAttempt
	err := c.doRequest(ctx, requestOptions{
		method: http.MethodGet,
		path:   "/notifications/" + notificationID + "/attempts",
		out:    &out,
	})
	return out, err
}

// GetMetrics returns provider-reported engagement metrics for a delivered
// notification. Returns a *RequestError with StatusCode 404 if metrics
// haven't been collected yet.
func (c *Client) GetMetrics(ctx context.Context, notificationID string) (*DeliveryMetric, error) {
	var out DeliveryMetric
	err := c.doRequest(ctx, requestOptions{
		method: http.MethodGet,
		path:   "/notifications/" + notificationID + "/metrics",
		out:    &out,
	})
	return &out, err
}
