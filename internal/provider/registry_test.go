package provider_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bse/notifyd/internal/domain"
	"github.com/bse/notifyd/internal/provider"
)

// stubProvider is a minimal Provider implementation used only to exercise the
// Registry. It satisfies the interface without performing any real work.
type stubProvider struct {
	channelType string
}

func (s *stubProvider) Type() string { return s.channelType }

func (s *stubProvider) Capabilities() provider.ProviderCapabilities {
	return provider.ProviderCapabilities{}
}

func (s *stubProvider) Send(_ context.Context, _ json.RawMessage, _ provider.SendRequest) (*provider.SendResponse, error) {
	return &provider.SendResponse{Success: true}, nil
}

func (s *stubProvider) FetchMetrics(_ context.Context, _ json.RawMessage, _ string) (*provider.DeliveryMetrics, error) {
	return nil, domain.ErrMetricsNotSupported
}

func (s *stubProvider) ValidateConfig(_ json.RawMessage) error {
	return nil
}

func TestRegistry_RegisterAndGet_Success(t *testing.T) {
	registry := provider.NewRegistry()
	stub := &stubProvider{channelType: "test-channel"}

	registry.Register(stub)

	got, err := registry.Get("test-channel")

	require.NoError(t, err)
	assert.Equal(t, stub, got)
}

func TestRegistry_Get_UnknownType_ReturnsErrUnsupportedChannel(t *testing.T) {
	registry := provider.NewRegistry()

	_, err := registry.Get("nonexistent")

	require.Error(t, err)
	assert.True(t, errors.Is(err, domain.ErrUnsupportedChannel))
}

func TestRegistry_Register_OverwritesExistingProvider(t *testing.T) {
	registry := provider.NewRegistry()

	original := &stubProvider{channelType: "overwrite-channel"}
	replacement := &stubProvider{channelType: "overwrite-channel"}

	registry.Register(original)
	registry.Register(replacement)

	got, err := registry.Get("overwrite-channel")

	require.NoError(t, err)
	assert.Same(t, replacement, got, "second registration should overwrite the first")
	assert.NotSame(t, original, got)
}
