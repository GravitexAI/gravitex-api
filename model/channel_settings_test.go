package model

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelValidateSettingsRejectsInvalidHTTPTransport(t *testing.T) {
	tests := []struct {
		name    string
		setting dto.ChannelSettings
		wantErr string
	}{
		{
			name:    "auto with shards is valid",
			setting: dto.ChannelSettings{HTTPProtocol: "auto", HTTP2ConnectionShards: 4},
		},
		{
			name:    "http1 with shards greater than one rejected",
			setting: dto.ChannelSettings{HTTPProtocol: "http1", HTTP2ConnectionShards: 2},
			wantErr: "http2_connection_shards",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			channel := &Channel{}
			channel.SetSetting(tt.setting)
			err := channel.ValidateSettings()
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestChannelValidateSettingsAcceptsZeroCostDiscount(t *testing.T) {
	zero := 0.0
	channel := &Channel{CostDiscount: &zero}

	require.NoError(t, channel.ValidateSettings())
}

func TestChannelValidateSettingsAcceptsZeroModelCostDiscount(t *testing.T) {
	channel := &Channel{}
	channel.SetOtherSettings(dto.ChannelOtherSettings{
		ModelCostDiscount: map[string]float64{"seedance-2-0-fast": 0},
	})

	require.NoError(t, channel.ValidateSettings())
}

func TestChannelValidateSettingsRejectsOutOfRangeCostDiscount(t *testing.T) {
	overOne := 1.001
	channel := &Channel{CostDiscount: &overOne}

	err := channel.ValidateSettings()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cost_discount")
}

// ValidateSettings 必须接通 ChannelOtherSettings.ValidateToolLossPolicy，
// 该校验曾在 main-alpha 合并中丢失，导致非法 tool_loss_policy 静默入库。
func TestChannelValidateSettingsRejectsInvalidToolLossPolicy(t *testing.T) {
	tests := []struct {
		name          string
		toolLossPolicy string
		wantErr        string
	}{
		{name: "empty keeps default allow policy", toolLossPolicy: ""},
		{name: "allow is valid", toolLossPolicy: "allow"},
		{name: "safe is valid", toolLossPolicy: "safe"},
		{name: "strict is valid", toolLossPolicy: "strict"},
		{name: "unknown policy rejected", toolLossPolicy: "drop", wantErr: "tool_loss_policy"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			channel := &Channel{}
			channel.SetOtherSettings(dto.ChannelOtherSettings{
				ToolLossPolicy: tt.toolLossPolicy,
			})

			err := channel.ValidateSettings()
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestAdvancedCustomChannelRequiresModelListRouteOnlyWhenUpdateChecksEnabled(t *testing.T) {
	inferenceRoute := dto.AdvancedCustomRoute{
		IncomingPath: "/v1/chat/completions",
		UpstreamPath: "/v1/chat/completions",
		Converter:    "none",
	}

	tests := []struct {
		name          string
		checksEnabled bool
		routes        []dto.AdvancedCustomRoute
		wantErr       string
	}{
		{
			name:   "legacy channel without discovery route remains valid",
			routes: []dto.AdvancedCustomRoute{inferenceRoute},
		},
		{
			name:          "enabled checks require discovery route",
			checksEnabled: true,
			routes:        []dto.AdvancedCustomRoute{inferenceRoute},
			wantErr:       dto.AdvancedCustomModelListPath,
		},
		{
			name:          "enabled checks accept discovery route",
			checksEnabled: true,
			routes: []dto.AdvancedCustomRoute{
				inferenceRoute,
				{
					IncomingPath: dto.AdvancedCustomModelListPath,
					UpstreamPath: dto.AdvancedCustomModelListPath,
					Converter:    "none",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			channel := &Channel{Type: constant.ChannelTypeAdvancedCustom}
			channel.SetOtherSettings(dto.ChannelOtherSettings{
				UpstreamModelUpdateCheckEnabled: tt.checksEnabled,
				AdvancedCustom: &dto.AdvancedCustomConfig{
					Routes: tt.routes,
				},
			})

			err := channel.ValidateSettings()
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}
