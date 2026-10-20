package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
)

func TestNormalizeChannelTestEndpointMockAI(t *testing.T) {
	mock := func(settings string) *model.Channel {
		return &model.Channel{Type: constant.ChannelTypeMockAI, OtherSettings: settings}
	}
	tests := []struct {
		name    string
		channel *model.Channel
		want    string
	}{
		{"messages only", mock(`{"mock_ai":{"endpoints":{"messages":true}}}`), string(constant.EndpointTypeAnthropic)},
		{"responses only", mock(`{"mock_ai":{"endpoints":{"responses":true}}}`), string(constant.EndpointTypeOpenAIResponse)},
		{"chat preferred when enabled", mock(`{"mock_ai":{"endpoints":{"chat_completions":true,"messages":true}}}`), string(constant.EndpointTypeOpenAI)},
		{"no mock_ai config keeps default", mock(`{}`), ""},
		{"empty settings keeps default", mock(``), ""},
		{"no endpoint enabled keeps default", mock(`{"mock_ai":{"endpoints":{}}}`), ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, normalizeChannelTestEndpoint(tt.channel, ""))
		})
	}
	assert.Equal(t, "custom", normalizeChannelTestEndpoint(mock(`{"mock_ai":{"endpoints":{"messages":true}}}`), " custom "))
}
