package handler

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bse/notifyd/internal/domain"
)

// TestNewWebhookEndpointCreatedResponse_IncludesSecretExactlyOnce verifies
// the create-response shape: every WebhookEndpoint field is present (via the
// embedded pointer) plus exactly one top-level "secret" key carrying the
// plaintext value the service returned — not the (always empty, per
// json:"-") value that would come from the embedded struct itself.
func TestNewWebhookEndpointCreatedResponse_IncludesSecretExactlyOnce(t *testing.T) {
	endpoint := &domain.WebhookEndpoint{
		ID:        uuid.New(),
		TenantID:  uuid.New(),
		URL:       "https://example.com/hooks/notifyd",
		Secret:    "should-never-appear-in-json",
		Events:    []string{"notification.delivered"},
		IsActive:  true,
		CreatedAt: time.Now(),
	}

	body := newWebhookEndpointCreatedResponse(endpoint, "the-plaintext-secret")

	encoded, err := json.Marshal(body)
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(encoded, &decoded))

	assert.Equal(t, "the-plaintext-secret", decoded["secret"])
	assert.Equal(t, endpoint.URL, decoded["url"])
	assert.Equal(t, endpoint.ID.String(), decoded["id"])
	assert.NotContains(t, string(encoded), "should-never-appear-in-json")
}
