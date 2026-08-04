package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestAffinityShouldRefreshTTL 锁定「固定过期 / 滑动过期」的续期契约：
// 固定过期下命中同一渠道不续期（让缓存自然到期以触发重新负载均衡），
// 其余情况（滑动过期、首次分配、渠道切换）均需续期。
func TestAffinityShouldRefreshTTL(t *testing.T) {
	cases := []struct {
		name           string
		fixedTTL       bool
		hitChannelID   int
		finalChannelID int
		want           bool
	}{
		{
			name:           "sliding ttl always refreshes",
			fixedTTL:       false,
			hitChannelID:   7,
			finalChannelID: 7,
			want:           true,
		},
		{
			name:           "fixed ttl first assignment refreshes",
			fixedTTL:       true,
			hitChannelID:   0,
			finalChannelID: 7,
			want:           true,
		},
		{
			name:           "fixed ttl same channel does not refresh",
			fixedTTL:       true,
			hitChannelID:   7,
			finalChannelID: 7,
			want:           false,
		},
		{
			name:           "fixed ttl channel switched refreshes",
			fixedTTL:       true,
			hitChannelID:   7,
			finalChannelID: 9,
			want:           true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := affinityShouldRefreshTTL(tc.fixedTTL, tc.hitChannelID, tc.finalChannelID)
			assert.Equal(t, tc.want, got)
		})
	}
}
