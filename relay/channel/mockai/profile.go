package mockai

import (
	"math"

	"github.com/QuantumNous/new-api/relaykit/dto"
)

// defaultProfile 是 cfg == nil 时的内置兜底档位。
// 满足配置校验约束：LatencyMs.Min > FirstTokenMs.Max。
var defaultProfile = dto.MockAIModelProfile{
	TextInput:     dto.MockAIRange{Min: 50, Max: 500},
	TextOutput:    dto.MockAIRange{Min: 50, Max: 300},
	CacheRead:     dto.MockAIRange{Min: 0, Max: 0},
	CacheCreation: dto.MockAIRange{Min: 0, Max: 0},
	LatencyMs:     dto.MockAIRange{Min: 1000, Max: 3000},
	FirstTokenMs:  dto.MockAIRange{Min: 200, Max: 800},
}

// rollSource 是随机源注入点，生产使用 math/rand，测试使用确定性实现。
type rollSource interface {
	Intn(n int) int
}

// mockRoll 是一次请求从档位区间中随机出的用量与时延。
type mockRoll struct {
	TextInput       int
	TextOutput      int
	CacheRead       int
	CacheCreation   int
	CacheCreation5m int
	CacheCreation1h int
	Reasoning       int
	LatencyMs       int
	FirstTokenMs    int
}

// resolveProfile 按「重定向之前的原始模型名」查找档位。
// 刻意不用 model_mapping 之后的上游名：Mock 没有真实上游，且前端下拉框只能列出
// 渠道「模型」字段里的原始名；若用上游名，配置了 model_mapping 后查找会永远落空，
// 静默回落到默认档位。未命中回落 cfg.Default，cfg 为 nil 回落 defaultProfile。
func resolveProfile(cfg *dto.MockAIConfig, originModel string) dto.MockAIModelProfile {
	if cfg == nil {
		return defaultProfile
	}
	if p, ok := cfg.Models[originModel]; ok {
		return p
	}
	return cfg.Default
}

// rollRange 在闭区间 [Min, Max] 内取值；Min == Max 时 Intn(1) 恒为 0。
func rollRange(r dto.MockAIRange, src rollSource) int {
	return r.Min + src.Intn(r.Max-r.Min+1)
}

// roll 对档位中的每个区间各取一个随机值。
func roll(p dto.MockAIModelProfile, src rollSource) mockRoll {
	out := mockRoll{
		TextInput:     rollRange(p.TextInput, src),
		TextOutput:    rollRange(p.TextOutput, src),
		CacheRead:     rollRange(p.CacheRead, src),
		CacheCreation: rollRange(p.CacheCreation, src),
		LatencyMs:     rollRange(p.LatencyMs, src),
		FirstTokenMs:  rollRange(p.FirstTokenMs, src),
	}
	if p.Reasoning != nil {
		out.Reasoning = rollRange(*p.Reasoning, src)
	}
	// 先对 1h 部分舍入，再用总数减去它得到 5m 部分，保证两者之和严格等于总数，
	// 否则计费侧会对不上账。
	out.CacheCreation1h = int(math.Round(float64(out.CacheCreation) * p.CacheCreation1hRatio))
	out.CacheCreation5m = out.CacheCreation - out.CacheCreation1h
	return out
}
