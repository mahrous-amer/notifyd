package provider

import (
	"context"
	"encoding/json"
)

type SendRequest struct {
	Subject  string
	Body     string
	Metadata json.RawMessage
}

type SendResponse struct {
	Success      bool
	ProviderData json.RawMessage
	ErrorMessage string
}

type Provider interface {
	Type() string
	Send(ctx context.Context, channelConfig json.RawMessage, req SendRequest) (*SendResponse, error)
	ValidateConfig(config json.RawMessage) error
}
