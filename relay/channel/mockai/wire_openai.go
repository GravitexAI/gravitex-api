package mockai

import (
	"time"

	"github.com/QuantumNous/new-api/common"
)

// 下面这组 wire 结构刻意独立于项目内的 dto，目的是让合成的字节与真实上游
// OpenAI chat/completions 响应逐字节同构：只出现官方字段，不带任何网关扩展。

type openAIWireInputDetails struct {
	CachedTokens int `json:"cached_tokens"`
}

type openAIWireOutputDetails struct {
	ReasoningTokens int `json:"reasoning_tokens"`
}

type openAIWireUsage struct {
	PromptTokens            int                     `json:"prompt_tokens"`
	CompletionTokens        int                     `json:"completion_tokens"`
	TotalTokens             int                     `json:"total_tokens"`
	PromptTokensDetails     openAIWireInputDetails  `json:"prompt_tokens_details"`
	CompletionTokensDetails openAIWireOutputDetails `json:"completion_tokens_details"`
}

type openAIWireMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIWireChoice struct {
	Index        int               `json:"index"`
	Message      openAIWireMessage `json:"message"`
	FinishReason string            `json:"finish_reason"`
}

type openAIWireChatCompletion struct {
	ID      string             `json:"id"`
	Object  string             `json:"object"`
	Created int64              `json:"created"`
	Model   string             `json:"model"`
	Choices []openAIWireChoice `json:"choices"`
	Usage   openAIWireUsage    `json:"usage"`
}

type openAIWireDelta struct {
	Role string `json:"role,omitempty"`
	// Content 用指针是为了区分「这一片没有内容」（末片 delta 为 {}）
	// 与「内容是空串」（首片 role chunk），两者在真实上游里都会出现。
	Content *string `json:"content,omitempty"`
}

type openAIWireChunkChoice struct {
	Index        int             `json:"index"`
	Delta        openAIWireDelta `json:"delta"`
	FinishReason *string         `json:"finish_reason"`
}

type openAIWireChunk struct {
	ID      string                  `json:"id"`
	Object  string                  `json:"object"`
	Created int64                   `json:"created"`
	Model   string                  `json:"model"`
	Choices []openAIWireChunkChoice `json:"choices"`
	// 真实上游在 include_usage 打开时，非末片也会带 "usage": null。
	Usage *openAIWireUsage `json:"usage"`
}

// sseDataFrame 把一个值序列化成一条 SSE data 帧。
func sseDataFrame(v any) []byte {
	// 这些 wire 结构只含字符串、整数、指针与切片，common.Marshal 不会失败。
	body, _ := common.Marshal(v)
	frame := make([]byte, 0, len(body)+8)
	frame = append(frame, "data: "...)
	frame = append(frame, body...)
	return append(frame, '\n', '\n')
}

// sseDoneFrame 是 OpenAI 系端点的流式收尾帧。
func sseDoneFrame() []byte {
	return []byte("data: [DONE]\n\n")
}

// buildOpenAIChatFrames 合成 /v1/chat/completions 的上游字节。
// 用量换算遵循设计文档 §5.4：prompt = TextInput + CacheRead，cached_tokens = CacheRead。
// 流式下无条件下发 usage，不看客户端有没有传 stream_options.include_usage。
func buildOpenAIChatFrames(r mockRoll, answer, model string, stream bool) [][]byte {
	id := "chatcmpl-" + common.GetRandomString(29)
	created := time.Now().Unix()
	promptTokens := r.TextInput + r.CacheRead
	usage := openAIWireUsage{
		PromptTokens:            promptTokens,
		CompletionTokens:        r.TextOutput,
		TotalTokens:             promptTokens + r.TextOutput,
		PromptTokensDetails:     openAIWireInputDetails{CachedTokens: r.CacheRead},
		CompletionTokensDetails: openAIWireOutputDetails{ReasoningTokens: r.Reasoning},
	}

	if !stream {
		body, _ := common.Marshal(openAIWireChatCompletion{
			ID:      id,
			Object:  "chat.completion",
			Created: created,
			Model:   model,
			Choices: []openAIWireChoice{{
				Message:      openAIWireMessage{Role: "assistant", Content: answer},
				FinishReason: "stop",
			}},
			Usage: usage,
		})
		return [][]byte{body}
	}

	newChunk := func(delta openAIWireDelta, finishReason *string) openAIWireChunk {
		return openAIWireChunk{
			ID:      id,
			Object:  "chat.completion.chunk",
			Created: created,
			Model:   model,
			Choices: []openAIWireChunkChoice{{Delta: delta, FinishReason: finishReason}},
		}
	}

	runes := []rune(answer)
	frames := make([][]byte, 0, len(runes)+4)
	empty := ""
	frames = append(frames, sseDataFrame(newChunk(openAIWireDelta{Role: "assistant", Content: &empty}, nil)))
	for i := range runes {
		piece := string(runes[i])
		frames = append(frames, sseDataFrame(newChunk(openAIWireDelta{Content: &piece}, nil)))
	}
	stopReason := "stop"
	frames = append(frames, sseDataFrame(newChunk(openAIWireDelta{}, &stopReason)))

	usageChunk := newChunk(openAIWireDelta{}, nil)
	usageChunk.Choices = []openAIWireChunkChoice{}
	usageChunk.Usage = &usage
	frames = append(frames, sseDataFrame(usageChunk))

	return append(frames, sseDoneFrame())
}
