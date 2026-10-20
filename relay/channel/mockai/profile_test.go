package mockai

import (
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fixedSource 是测试用的确定性随机源：mode 0 恒返回 0，mode 1 恒返回 n-1。
type fixedSource struct{ top bool }

func (f fixedSource) Intn(n int) int {
	if f.top {
		return n - 1
	}
	return 0
}

func fullProfile() dto.MockAIModelProfile {
	return dto.MockAIModelProfile{
		TextInput:            dto.MockAIRange{Min: 10, Max: 20},
		TextOutput:           dto.MockAIRange{Min: 30, Max: 40},
		CacheRead:            dto.MockAIRange{Min: 50, Max: 60},
		CacheCreation:        dto.MockAIRange{Min: 70, Max: 80},
		CacheCreation1hRatio: 0,
		Reasoning:            &dto.MockAIRange{Min: 90, Max: 100},
		LatencyMs:            dto.MockAIRange{Min: 1000, Max: 2000},
		FirstTokenMs:         dto.MockAIRange{Min: 100, Max: 200},
	}
}

func TestResolveProfile(t *testing.T) {
	gpt := fullProfile()
	def := dto.MockAIModelProfile{TextInput: dto.MockAIRange{Min: 1, Max: 2}}
	cfg := &dto.MockAIConfig{
		Default: def,
		Models:  map[string]dto.MockAIModelProfile{"gpt-4o": gpt},
	}

	// 原始模型名命中配置条目（不是重定向后的上游名）
	got := resolveProfile(cfg, "gpt-4o")
	require.Equal(t, gpt, got)

	// 未配置的模型回落到 Default
	got = resolveProfile(cfg, "not-configured")
	require.Equal(t, def, got)

	// cfg 为 nil 时使用内置兜底
	got = resolveProfile(nil, "gpt-4o")
	require.Equal(t, defaultProfile, got)
	assert.Greater(t, defaultProfile.TextInput.Max, 0)
	assert.Greater(t, defaultProfile.LatencyMs.Min, defaultProfile.FirstTokenMs.Max)
}

func TestRollWithinRange(t *testing.T) {
	p := fullProfile()

	lo := roll(p, fixedSource{top: false})
	assert.Equal(t, 10, lo.TextInput)
	assert.Equal(t, 30, lo.TextOutput)
	assert.Equal(t, 50, lo.CacheRead)
	assert.Equal(t, 70, lo.CacheCreation)
	assert.Equal(t, 90, lo.Reasoning)
	assert.Equal(t, 1000, lo.LatencyMs)
	assert.Equal(t, 100, lo.FirstTokenMs)

	hi := roll(p, fixedSource{top: true})
	assert.Equal(t, 20, hi.TextInput)
	assert.Equal(t, 40, hi.TextOutput)
	assert.Equal(t, 60, hi.CacheRead)
	assert.Equal(t, 80, hi.CacheCreation)
	assert.Equal(t, 100, hi.Reasoning)
	assert.Equal(t, 2000, hi.LatencyMs)
	assert.Equal(t, 200, hi.FirstTokenMs)
}

func TestRollDegenerateRange(t *testing.T) {
	r := dto.MockAIRange{Min: 7, Max: 7}
	p := dto.MockAIModelProfile{
		TextInput: r, TextOutput: r, CacheRead: r, CacheCreation: r,
		Reasoning: &r, LatencyMs: r, FirstTokenMs: r,
	}
	for _, top := range []bool{false, true} {
		var got mockRoll
		require.NotPanics(t, func() { got = roll(p, fixedSource{top: top}) })
		assert.Equal(t, 7, got.TextInput)
		assert.Equal(t, 7, got.TextOutput)
		assert.Equal(t, 7, got.CacheRead)
		assert.Equal(t, 7, got.CacheCreation)
		assert.Equal(t, 7, got.Reasoning)
		assert.Equal(t, 7, got.LatencyMs)
		assert.Equal(t, 7, got.FirstTokenMs)
	}
}

func TestRollCacheCreationSplit(t *testing.T) {
	for _, ratio := range []float64{0, 0.5, 1, 0.333} {
		p := fullProfile()
		p.CacheCreation = dto.MockAIRange{Min: 1001, Max: 1001}
		p.CacheCreation1hRatio = ratio
		got := roll(p, fixedSource{})
		require.Equal(t, got.CacheCreation, got.CacheCreation5m+got.CacheCreation1h, "ratio=%v", ratio)
	}
}

func TestRollReasoningDisabled(t *testing.T) {
	p := fullProfile()
	p.Reasoning = nil
	got := roll(p, fixedSource{top: true})
	assert.Equal(t, 0, got.Reasoning)
}
