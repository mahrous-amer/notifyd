package domain

import "errors"

var (
	ErrNotFound            = errors.New("not found")
	ErrValidationFailed    = errors.New("validation failed")
	ErrUnsupportedChannel  = errors.New("unsupported channel")
	ErrMetricsNotSupported = errors.New("metrics not supported")
)
