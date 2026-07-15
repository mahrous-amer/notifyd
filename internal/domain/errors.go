package domain

import "errors"

var (
	ErrNotFound            = errors.New("not found")
	ErrValidationFailed    = errors.New("validation failed")
	ErrUnsupportedChannel  = errors.New("unsupported channel")
	ErrMetricsNotSupported = errors.New("metrics not supported")
	ErrChannelNotInPlan    = errors.New("channel not in plan")
	ErrKeyLimitReached     = errors.New("api key limit reached")
	ErrWebhookLimitReached = errors.New("webhook endpoint limit reached")
)
