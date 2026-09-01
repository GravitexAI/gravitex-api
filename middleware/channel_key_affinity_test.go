package middleware

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/require"
)

func TestResolveChannelAffinityKey_UsesMatchingHashAfterReorder(t *testing.T) {
	hash, err := service.ChannelAffinityKeyHash("key-b")
	require.NoError(t, err)

	channel := &model.Channel{
		Id:     101,
		Key:    "key-b\nkey-a",
		Status: common.ChannelStatusEnabled,
		ChannelInfo: model.ChannelInfo{
			IsMultiKey:   true,
			MultiKeySize: 2,
			MultiKeyMode: constant.MultiKeyModePolling,
			MultiKeyStatusList: map[int]int{
				0: common.ChannelStatusEnabled,
				1: common.ChannelStatusEnabled,
			},
		},
	}
	key, index, gotHash, ok := resolveChannelAffinityKey(channel, service.ChannelAffinityBinding{
		ChannelID: channel.Id,
		KeyIndex:  1,
		KeyHash:   hash,
	})
	require.True(t, ok)
	require.Equal(t, "key-b", key)
	require.Equal(t, 0, index)
	require.Equal(t, hash, gotHash)
}

func TestResolveChannelAffinityKey_RejectsDisabledKey(t *testing.T) {
	channel := &model.Channel{
		Id:  102,
		Key: "key-a\nkey-b",
		ChannelInfo: model.ChannelInfo{
			IsMultiKey: true,
			MultiKeyStatusList: map[int]int{
				0: common.ChannelStatusAutoDisabled,
				1: common.ChannelStatusEnabled,
			},
		},
	}
	hash, err := service.ChannelAffinityKeyHash("key-a")
	require.NoError(t, err)

	_, _, _, ok := resolveChannelAffinityKey(channel, service.ChannelAffinityBinding{
		ChannelID: channel.Id,
		KeyIndex:  0,
		KeyHash:   hash,
	})
	require.False(t, ok)
}

func TestSelectReplacementChannelAffinityKey_SkipsFailedKey(t *testing.T) {
	channel := &model.Channel{
		Id:  103,
		Key: "key-a\nkey-b",
		ChannelInfo: model.ChannelInfo{
			IsMultiKey: true,
			MultiKeyStatusList: map[int]int{
				0: common.ChannelStatusEnabled,
				1: common.ChannelStatusEnabled,
			},
		},
	}
	failedHash, err := service.ChannelAffinityKeyHash("key-a")
	require.NoError(t, err)

	binding, ok := selectReplacementChannelAffinityKey(channel, failedHash)
	require.True(t, ok)
	require.Equal(t, 1, binding.KeyIndex)
	require.Equal(t, channel.Id, binding.ChannelID)
}
