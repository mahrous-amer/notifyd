package domain

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestEntitlements_AllowsChannel(t *testing.T) {
	e := &Entitlements{AllowedChannels: []ChannelType{ChannelDiscord, ChannelTelegram}}
	assert.True(t, e.AllowsChannel(ChannelDiscord))
	assert.True(t, e.AllowsChannel(ChannelTelegram))
	assert.False(t, e.AllowsChannel(ChannelWhatsApp))
}

func TestEntitlements_AllowsChannel_Empty(t *testing.T) {
	e := &Entitlements{}
	assert.False(t, e.AllowsChannel(ChannelDiscord))
}

// TestFreeDefaults_AllowsEmail verifies that email is available on the Free
// plan. Email is the baseline channel every evaluator expects; gating it
// behind a paid plan would undercut the acquisition story.
func TestFreeDefaults_AllowsEmail(t *testing.T) {
	ent := FreeDefaults(uuid.New(), time.Now())
	assert.True(t, ent.AllowsChannel(ChannelEmail))
}
