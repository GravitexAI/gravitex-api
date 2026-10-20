package mockai

import (
	"github.com/QuantumNous/new-api/common"
)

// 下面这组 wire 结构刻意独立于项目内的 dto，目的是让合成的字节与真实上游
// Anthropic /v1/messages 响应逐字节同构：只出现官方字段，不带任何网关扩展。

// claudeWireCacheCreation 是 usage.cache_creation 子对象。两个字段都不带 omitempty：
// 真实上游在启用缓存时会把 0 也显式下发，而这两个数字分别对应 5m / 1h 两档不同的
// 计费倍率，少一个字段就等于把那一档静默归零。
type claudeWireCacheCreation struct {
	Ephemeral5mInputTokens int `json:"ephemeral_5m_input_tokens"`
	Ephemeral1hInputTokens int `json:"ephemeral_1h_input_tokens"`
}

// claudeWireUsage 遵循设计 §5.4 的 messages 换算表。注意与 OpenAI 系端点的本质区别：
// input_tokens 与两个缓存字段是并列关系，缓存 token 不计入 input_tokens。
type claudeWireUsage struct {
	InputTokens              int                     `json:"input_tokens"`
	CacheCreationInputTokens int                     `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int                     `json:"cache_read_input_tokens"`
	CacheCreation            claudeWireCacheCreation `json:"cache_creation"`
	OutputTokens             int                     `json:"output_tokens"`
}

type claudeWireContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// claudeWireMessage 在 message_start 里作为 "message" 出现，非流式时它本身就是整个 body。
// StopReason 用指针：message_start 里真实上游给 null。
type claudeWireMessage struct {
	ID           string              `json:"id"`
	Type         string              `json:"type"`
	Role         string              `json:"role"`
	Model        string              `json:"model"`
	Content      []claudeWireContent `json:"content"`
	StopReason   *string             `json:"stop_reason"`
	StopSequence *string             `json:"stop_sequence"`
	Usage        claudeWireUsage     `json:"usage"`
}

// claudeWireTextDelta 是 content_block_delta 的 delta。
type claudeWireTextDelta struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// claudeWireMessageDelta 是 message_delta 的 delta。
type claudeWireMessageDelta struct {
	StopReason   string  `json:"stop_reason"`
	StopSequence *string `json:"stop_sequence"`
}

// claudeWireEvent 是流式事件的并集；各事件只填自己有的字段。
// Delta 用 any 是因为 content_block_delta 与 message_delta 的 delta 形状不同。
type claudeWireEvent struct {
	Type         string             `json:"type"`
	Message      *claudeWireMessage `json:"message,omitempty"`
	Index        *int               `json:"index,omitempty"`
	ContentBlock *claudeWireContent `json:"content_block,omitempty"`
	Delta        any                `json:"delta,omitempty"`
	Usage        *claudeWireUsage   `json:"usage,omitempty"`
}

// buildClaudeMessagesFrames 合成 /v1/messages 的上游字节。
// 用量换算遵循设计文档 §5.4 的 messages 换算表；5m/1h 拆分直接取 mockRoll 里
// 已经算好的值，不在这里重做舍入，否则两处舍入口径一旦分叉就会少算或多算 token。
// 流式按真实协议在 message_start 与 message_delta 各下发一次 usage，两处数字一致。
func buildClaudeMessagesFrames(r mockRoll, answer, model string, stream bool) [][]byte {
	id := "msg_" + common.GetRandomString(24)
	usage := claudeWireUsage{
		InputTokens:              r.TextInput,
		CacheCreationInputTokens: r.CacheCreation,
		CacheReadInputTokens:     r.CacheRead,
		CacheCreation: claudeWireCacheCreation{
			Ephemeral5mInputTokens: r.CacheCreation5m,
			Ephemeral1hInputTokens: r.CacheCreation1h,
		},
		OutputTokens: r.TextOutput,
	}
	endTurn := "end_turn"

	if !stream {
		// 这些 wire 结构只含字符串、整数、指针与切片，common.Marshal 不会失败。
		body, _ := common.Marshal(claudeWireMessage{
			ID:         id,
			Type:       "message",
			Role:       "assistant",
			Model:      model,
			Content:    []claudeWireContent{{Type: "text", Text: answer}},
			StopReason: &endTurn,
			Usage:      usage,
		})
		return [][]byte{body}
	}

	emit := func(ev claudeWireEvent) []byte { return sseEventFrame(ev.Type, ev) }
	zero := 0

	runes := []rune(answer)
	frames := make([][]byte, 0, len(runes)+5)
	frames = append(frames,
		emit(claudeWireEvent{Type: "message_start", Message: &claudeWireMessage{
			ID:      id,
			Type:    "message",
			Role:    "assistant",
			Model:   model,
			Content: []claudeWireContent{},
			Usage:   usage,
		}}),
		emit(claudeWireEvent{Type: "content_block_start", Index: &zero, ContentBlock: &claudeWireContent{Type: "text"}}),
	)
	for i := range runes {
		frames = append(frames, emit(claudeWireEvent{
			Type:  "content_block_delta",
			Index: &zero,
			Delta: claudeWireTextDelta{Type: "text_delta", Text: string(runes[i])},
		}))
	}
	return append(frames,
		emit(claudeWireEvent{Type: "content_block_stop", Index: &zero}),
		emit(claudeWireEvent{
			Type:  "message_delta",
			Delta: claudeWireMessageDelta{StopReason: endTurn},
			Usage: &usage,
		}),
		emit(claudeWireEvent{Type: "message_stop"}),
	)
}
