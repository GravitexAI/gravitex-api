package mockai

import (
	"time"

	"github.com/QuantumNous/new-api/common"
)

// 下面这组 wire 结构刻意独立于项目内的 dto，目的是让合成的字节与真实上游
// OpenAI /v1/responses 响应逐字节同构：只出现官方字段，不带任何网关扩展。

type responsesWireInputDetails struct {
	CachedTokens int `json:"cached_tokens"`
}

type responsesWireOutputDetails struct {
	ReasoningTokens int `json:"reasoning_tokens"`
}

type responsesWireUsage struct {
	InputTokens         int                        `json:"input_tokens"`
	InputTokensDetails  responsesWireInputDetails  `json:"input_tokens_details"`
	OutputTokens        int                        `json:"output_tokens"`
	OutputTokensDetails responsesWireOutputDetails `json:"output_tokens_details"`
	TotalTokens         int                        `json:"total_tokens"`
}

type responsesWireContent struct {
	Type        string `json:"type"`
	Annotations []any  `json:"annotations"`
	Text        string `json:"text"`
}

type responsesWireMessage struct {
	ID      string                 `json:"id"`
	Type    string                 `json:"type"`
	Status  string                 `json:"status"`
	Role    string                 `json:"role"`
	Content []responsesWireContent `json:"content"`
}

// responsesWireResponse 在 response.created / response.completed 里作为 "response" 出现，
// 非流式时它本身就是整个 body。Usage 用指针：created 里真实上游给 null。
type responsesWireResponse struct {
	ID        string                 `json:"id"`
	Object    string                 `json:"object"`
	CreatedAt int64                  `json:"created_at"`
	Status    string                 `json:"status"`
	Model     string                 `json:"model"`
	Output    []responsesWireMessage `json:"output"`
	Usage     *responsesWireUsage    `json:"usage"`
}

// responsesWireEvent 是流式事件的并集；各事件只填自己有的字段。
type responsesWireEvent struct {
	Type           string                 `json:"type"`
	SequenceNumber int                    `json:"sequence_number"`
	Response       *responsesWireResponse `json:"response,omitempty"`
	OutputIndex    *int                   `json:"output_index,omitempty"`
	ContentIndex   *int                   `json:"content_index,omitempty"`
	ItemID         string                 `json:"item_id,omitempty"`
	Item           *responsesWireMessage  `json:"item,omitempty"`
	Part           *responsesWireContent  `json:"part,omitempty"`
	Delta          *string                `json:"delta,omitempty"`
	Text           *string                `json:"text,omitempty"`
}

// sseEventFrame 与真实上游一致：先一行 event:，再一行 data:。
func sseEventFrame(eventType string, v any) []byte {
	frame := []byte("event: " + eventType + "\n")
	return append(frame, sseDataFrame(v)...)
}

// buildResponsesFrames 合成 /v1/responses 的上游字节。
// 用量换算遵循设计文档 §5.4：input_tokens = TextInput + CacheRead，cached_tokens = CacheRead。
// 流式下 usage 无条件放在 response.completed 里。
func buildResponsesFrames(r mockRoll, answer, model string, stream bool) [][]byte {
	respID := "resp_" + common.GetRandomString(48)
	msgID := "msg_" + common.GetRandomString(48)
	createdAt := time.Now().Unix()
	inputTokens := r.TextInput + r.CacheRead
	usage := &responsesWireUsage{
		InputTokens:         inputTokens,
		InputTokensDetails:  responsesWireInputDetails{CachedTokens: r.CacheRead},
		OutputTokens:        r.TextOutput,
		OutputTokensDetails: responsesWireOutputDetails{ReasoningTokens: r.Reasoning},
		TotalTokens:         inputTokens + r.TextOutput,
	}
	newMessage := func(status, text string) responsesWireMessage {
		content := []responsesWireContent{}
		if text != "" {
			content = append(content, responsesWireContent{Type: "output_text", Annotations: []any{}, Text: text})
		}
		return responsesWireMessage{ID: msgID, Type: "message", Status: status, Role: "assistant", Content: content}
	}
	newResponse := func(status string, output []responsesWireMessage, u *responsesWireUsage) *responsesWireResponse {
		return &responsesWireResponse{
			ID: respID, Object: "response", CreatedAt: createdAt,
			Status: status, Model: model, Output: output, Usage: u,
		}
	}

	if !stream {
		body, _ := common.Marshal(newResponse("completed", []responsesWireMessage{newMessage("completed", answer)}, usage))
		return [][]byte{body}
	}

	seq := 0
	zero := 0
	emit := func(ev responsesWireEvent) []byte {
		ev.SequenceNumber = seq
		seq++
		return sseEventFrame(ev.Type, ev)
	}

	inProgress := newMessage("in_progress", "")
	emptyPart := responsesWireContent{Type: "output_text", Annotations: []any{}, Text: ""}
	frames := [][]byte{
		emit(responsesWireEvent{Type: "response.created", Response: newResponse("in_progress", []responsesWireMessage{}, nil)}),
		emit(responsesWireEvent{Type: "response.in_progress", Response: newResponse("in_progress", []responsesWireMessage{}, nil)}),
		emit(responsesWireEvent{Type: "response.output_item.added", OutputIndex: &zero, Item: &inProgress}),
		emit(responsesWireEvent{Type: "response.content_part.added", ItemID: msgID, OutputIndex: &zero, ContentIndex: &zero, Part: &emptyPart}),
	}
	for _, ch := range answer {
		piece := string(ch)
		frames = append(frames, emit(responsesWireEvent{Type: "response.output_text.delta", ItemID: msgID, OutputIndex: &zero, ContentIndex: &zero, Delta: &piece}))
	}
	fullPart := responsesWireContent{Type: "output_text", Annotations: []any{}, Text: answer}
	done := newMessage("completed", answer)
	frames = append(frames,
		emit(responsesWireEvent{Type: "response.output_text.done", ItemID: msgID, OutputIndex: &zero, ContentIndex: &zero, Text: &answer}),
		emit(responsesWireEvent{Type: "response.content_part.done", ItemID: msgID, OutputIndex: &zero, ContentIndex: &zero, Part: &fullPart}),
		emit(responsesWireEvent{Type: "response.output_item.done", OutputIndex: &zero, Item: &done}),
		emit(responsesWireEvent{Type: "response.completed", Response: newResponse("completed", []responsesWireMessage{done}, usage)}),
	)
	return frames
}
