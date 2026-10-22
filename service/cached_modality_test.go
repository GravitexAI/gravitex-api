package service

import (
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"

	"github.com/stretchr/testify/assert"
)

// 真实上游样本（gemini-3.1-pro-preview，request_id pzO9auP8BpXRi9sPprO96Ao）：
//
//	promptTokenCount 35537 = TEXT 1967 + IMAGE 33570
//	cachedContentTokenCount 32689
//
// promptTokensDetails 按模态切整个输入（含缓存部分），cachedContentTokenCount 是
// 另一把刀切同一批 token，两者重叠。非缓存输入只有 35537-32689=2848，却有 33570
// 个图片 token，说明图片里至少 30722 个落在缓存区间内。
func TestResolveFreshModalityTokens(t *testing.T) {
	const (
		promptTokens = 35537
		cachedTokens = 32689
		textTotal    = 1967
		imageTotal   = 33570
	)

	cases := []struct {
		name string
		in   dto.InputTokenDetails
		want freshModalityTokens
	}{
		{
			// 上游给出完整 cacheTokensDetails：直接采信，无需反推。
			name: "complete cache details",
			in: dto.InputTokenDetails{
				CachedTokens: cachedTokens, TextTokens: textTotal, ImageTokens: imageTotal,
				CachedTextTokens: 1000, CachedImageTokens: 31689,
			},
			want: freshModalityTokens{Image: 1881},
		},
		{
			// 缓存全部由图片构成。
			name: "cache is all image",
			in: dto.InputTokenDetails{
				CachedTokens: cachedTokens, TextTokens: textTotal, ImageTokens: imageTotal,
				CachedImageTokens: cachedTokens,
			},
			want: freshModalityTokens{Image: 881},
		},
		{
			// 文本全部命中缓存，其余缓存归图片。
			name: "cache covers all text",
			in: dto.InputTokenDetails{
				CachedTokens: cachedTokens, TextTokens: textTotal, ImageTokens: imageTotal,
				CachedTextTokens: textTotal, CachedImageTokens: 30722,
			},
			want: freshModalityTokens{Image: 2848},
		},
		{
			// 只给了 TEXT 一项：缺口 32689-1000 归到未列出的 IMAGE，
			// 结果应与 "complete cache details" 完全一致。
			name: "partial cache details are completed from the gap",
			in: dto.InputTokenDetails{
				CachedTokens: cachedTokens, TextTokens: textTotal, ImageTokens: imageTotal,
				CachedTextTokens: 1000,
			},
			want: freshModalityTokens{Image: 1881},
		},
		{
			// 完全没有 cacheTokensDetails：按模态在总输入中的占比摊缓存。
			// 32689 × 33570/35537 = 30879.6 → 33570-30879 = 2691
			name: "no cache details falls back to proportional split",
			in: dto.InputTokenDetails{
				CachedTokens: cachedTokens, TextTokens: textTotal, ImageTokens: imageTotal,
			},
			want: freshModalityTokens{Image: 2691},
		},
		{
			// 没有缓存命中时，各模态全额都是「新」token，行为与改动前一致。
			name: "no cache hit keeps every modality intact",
			in: dto.InputTokenDetails{
				TextTokens: textTotal, ImageTokens: imageTotal, AudioTokens: 10, VideoTokens: 20,
			},
			want: freshModalityTokens{Image: imageTotal, Audio: 10, Video: 20},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveFreshModalityTokens(tc.in, promptTokens)
			assert.Equal(t, tc.want, got)
		})
	}
}

// 无论缓存如何分布，各模态的「非缓存部分」加起来都不能超过非缓存输入总量，
// 否则计费时 baseTokens 会被重复扣减成负数。
func TestResolveFreshModalityTokensNeverExceedsUncachedInput(t *testing.T) {
	cases := []dto.InputTokenDetails{
		{CachedTokens: 32689, TextTokens: 1967, ImageTokens: 33570},
		{CachedTokens: 32689, TextTokens: 1967, ImageTokens: 33570, CachedTextTokens: 1000},
		{CachedTokens: 21831, TextTokens: 16012, ImageTokens: 8537},
		{CachedTokens: 6000, TextTokens: 8000, ImageTokens: 2000, AudioTokens: 500},
	}
	for _, in := range cases {
		promptTokens := in.TextTokens + in.ImageTokens + in.AudioTokens + in.VideoTokens
		got := resolveFreshModalityTokens(in, promptTokens)
		assert.LessOrEqual(t, got.Image+got.Audio+got.Video, promptTokens-in.CachedTokens,
			"fresh modality tokens must fit inside the uncached input")
	}
}
