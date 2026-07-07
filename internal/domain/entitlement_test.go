package domain

import (
	"testing"

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
