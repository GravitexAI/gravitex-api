package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
)

// TestMockAIPricingEndpointTypes calls the pure selector directly (no DB
// fixture): a Mock AI channel reports only the endpoints it has enabled, and
// falls back to the full set when no config is available.
func TestMockAIPricingEndpointTypes(t *testing.T) {
	ability := AbilityWithChannel{
		Ability:     Ability{ChannelId: 7, Model: "mock-model"},
		ChannelType: constant.ChannelTypeMockAI,
	}

	tests := []struct {
		name    string
		configs map[int]*dto.MockAIConfig
		want    []constant.EndpointType
	}{
		{
			name: "only messages enabled",
			configs: map[int]*dto.MockAIConfig{
				7: {Endpoints: dto.MockAIEndpoints{Messages: true}},
			},
			want: []constant.EndpointType{constant.EndpointTypeAnthropic},
		},
		{
			name: "chat and responses enabled",
			configs: map[int]*dto.MockAIConfig{
				7: {Endpoints: dto.MockAIEndpoints{ChatCompletions: true, Responses: true}},
			},
			want: []constant.EndpointType{constant.EndpointTypeOpenAI, constant.EndpointTypeOpenAIResponse},
		},
		{
			name:    "no endpoint enabled yields empty list",
			configs: map[int]*dto.MockAIConfig{7: {}},
			want:    []constant.EndpointType{},
		},
		{
			name:    "missing config falls back to full set",
			configs: map[int]*dto.MockAIConfig{},
			want:    common.GetEndpointTypesByChannelType(constant.ChannelTypeMockAI, "mock-model"),
		},
		{
			name:    "nil config map falls back to full set",
			configs: nil,
			want:    common.GetEndpointTypesByChannelType(constant.ChannelTypeMockAI, "mock-model"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getPricingEndpointTypesForAbility(ability, nil, tt.configs)
			assert.Equal(t, tt.want, got)
		})
	}
}
