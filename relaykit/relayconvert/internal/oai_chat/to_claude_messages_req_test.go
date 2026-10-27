package oaichat

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	relaymedia "github.com/QuantumNous/new-api/relaykit/relayconvert/internal/media"
	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenAIChatRequestToClaudeMessagesNormalizesToolInputSchema(t *testing.T) {
	tests := []struct {
		name       string
		parameters any
		wantSchema map[string]any
	}{
		{
			name:       "omitted parameters",
			parameters: nil,
			wantSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			name: "missing type and properties",
			parameters: map[string]any{
				"additionalProperties": false,
			},
			wantSchema: map[string]any{
				"type":                 "object",
				"properties":           map[string]any{},
				"additionalProperties": false,
			},
		},
		{
			name: "non-string type",
			parameters: map[string]any{
				"type":       123,
				"properties": map[string]any{},
			},
			wantSchema: map[string]any{
				"type":       123,
				"properties": map[string]any{},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			maxTokens := uint(1024)
			got, err := OpenAIChatRequestToClaudeMessages(context.Background(), nil, dto.GeneralOpenAIRequest{
				Model:     "claude-test",
				MaxTokens: &maxTokens,
				Messages: []dto.Message{
					{Role: "user", Content: "Call the tool."},
				},
				Tools: []dto.ToolCallRequest{
					{
						Type: "function",
						Function: dto.FunctionRequest{
							Name:        "get_current_time",
							Description: "Get the current time",
							Parameters:  tt.parameters,
						},
					},
				},
			})

			require.NoError(t, err)
			tools, ok := got.Tools.([]any)
			require.True(t, ok)
			require.Len(t, tools, 1)
			tool, ok := tools[0].(*dto.Tool)
			require.True(t, ok)
			assert.Equal(t, "get_current_time", tool.Name)
			assert.Equal(t, tt.wantSchema, tool.InputSchema)
		})
	}
}

// installToolResultMediaResolver 注入确定性媒体解析桩：
// data URL 直接拆出 MIME 与数据；远程 URL 按 remote 表返回并记录访问。
func installToolResultMediaResolver(t *testing.T, remote map[string][2]string) *[]string {
	t.Helper()
	fetched := make([]string, 0)
	relaymedia.SetMediaResolver(relaymedia.MediaResolver{
		GetBase64Data: func(c context.Context, source types.FileSource, reason ...string) (string, string, error) {
			raw := source.GetRawData()
			if source.IsURL() {
				fetched = append(fetched, raw)
				got, ok := remote[raw]
				if !ok {
					return "", "", errors.New("not found")
				}
				return got[1], got[0], nil
			}
			header, data, ok := strings.Cut(strings.TrimPrefix(raw, "data:"), ",")
			if !ok {
				return "", "", errors.New("invalid data url")
			}
			return data, strings.TrimSuffix(header, ";base64"), nil
		},
	})
	t.Cleanup(func() { relaymedia.SetMediaResolver(relaymedia.MediaResolver{}) })
	return &fetched
}

// convertToolResultMessages 将带工具调用的历史转换为 Claude 请求，并返回序列化后的 messages。
func convertToolResultMessages(t *testing.T, messages []dto.Message) ([]map[string]any, error) {
	t.Helper()
	maxTokens := uint(1024)
	got, err := OpenAIChatRequestToClaudeMessages(context.Background(), nil, dto.GeneralOpenAIRequest{
		Model:     "claude-test",
		MaxTokens: &maxTokens,
		Messages:  messages,
	})
	if err != nil {
		return nil, err
	}
	body, err := kitutil.Marshal(got)
	require.NoError(t, err)
	var decoded struct {
		Messages []map[string]any `json:"messages"`
	}
	require.NoError(t, kitutil.Unmarshal(body, &decoded))
	return decoded.Messages, nil
}

func toolCallHistory(t *testing.T, toolCallIDs ...string) []dto.Message {
	t.Helper()
	toolCalls := make([]any, 0, len(toolCallIDs))
	for _, id := range toolCallIDs {
		toolCalls = append(toolCalls, map[string]any{
			"id":       id,
			"type":     "function",
			"function": map[string]any{"name": "screenshot", "arguments": "{}"},
		})
	}
	rawToolCalls, err := kitutil.Marshal(toolCalls)
	require.NoError(t, err)
	return []dto.Message{
		{Role: "user", Content: "Take a screenshot."},
		{Role: "assistant", Content: "", ToolCalls: rawToolCalls},
	}
}

// toolResultContentOf 取出最后一条 user 消息中第 index 个 tool_result 的 content。
func toolResultContentOf(t *testing.T, messages []map[string]any, index int) any {
	t.Helper()
	last := messages[len(messages)-1]
	require.Equal(t, "user", last["role"])
	blocks, ok := last["content"].([]any)
	require.True(t, ok)
	require.Greater(t, len(blocks), index)
	block := blocks[index].(map[string]any)
	require.Equal(t, "tool_result", block["type"])
	return block["content"]
}

func TestOpenAIChatToolResultKeepsTextOnlyContent(t *testing.T) {
	installToolResultMediaResolver(t, nil)
	tests := []struct {
		name    string
		content any
		want    any
	}{
		{name: "string", content: "done", want: "done"},
		{
			name: "text blocks with cache_control",
			content: []any{
				map[string]any{"type": "text", "text": "page title", "cache_control": map[string]any{"type": "ephemeral"}},
			},
			want: []any{
				map[string]any{"type": "text", "text": "page title", "cache_control": map[string]any{"type": "ephemeral"}},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			messages := append(toolCallHistory(t, "call_1"), dto.Message{Role: "tool", ToolCallId: "call_1", Content: tt.content})
			got, err := convertToolResultMessages(t, messages)
			require.NoError(t, err)
			assert.Equal(t, tt.want, toolResultContentOf(t, got, 0))
		})
	}
}

func TestOpenAIChatToolResultConvertsMediaBlocks(t *testing.T) {
	fetched := installToolResultMediaResolver(t, map[string][2]string{
		"https://example.test/shot.webp":  {"image/webp", "V0VCUA=="},
		"https://example.test/report.pdf": {"application/pdf", "UERG"},
	})
	messages := append(toolCallHistory(t, "call_1"), dto.Message{
		Role:       "tool",
		ToolCallId: "call_1",
		Content: []any{
			map[string]any{"type": "text", "text": "current page", "cache_control": map[string]any{"type": "ephemeral"}},
			map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:image/png;base64,UE5H"}},
			map[string]any{"type": "image_url", "image_url": "https://example.test/shot.webp", "cache_control": map[string]any{"type": "ephemeral"}},
			map[string]any{"type": "file", "file": map[string]any{"filename": "report.pdf", "file_data": "https://example.test/report.pdf"}},
			map[string]any{"type": "image", "source": map[string]any{"type": "base64", "media_type": "image/gif", "data": "R0lG"}},
		},
	})

	got, err := convertToolResultMessages(t, messages)
	require.NoError(t, err)
	assert.Equal(t, []any{
		map[string]any{"type": "text", "text": "current page", "cache_control": map[string]any{"type": "ephemeral"}},
		map[string]any{"type": "image", "source": map[string]any{"type": "base64", "media_type": "image/png", "data": "UE5H"}},
		map[string]any{"type": "image", "source": map[string]any{"type": "base64", "media_type": "image/webp", "data": "V0VCUA=="}, "cache_control": map[string]any{"type": "ephemeral"}},
		map[string]any{"type": "document", "source": map[string]any{"type": "base64", "media_type": "application/pdf", "data": "UERG"}},
		// Claude 原生块原样透传
		map[string]any{"type": "image", "source": map[string]any{"type": "base64", "media_type": "image/gif", "data": "R0lG"}},
	}, toolResultContentOf(t, got, 0))
	assert.Equal(t, []string{"https://example.test/shot.webp", "https://example.test/report.pdf"}, *fetched)
}

func TestOpenAIChatToolResultMergesConsecutiveResults(t *testing.T) {
	installToolResultMediaResolver(t, nil)
	messages := append(toolCallHistory(t, "call_1", "call_2"),
		dto.Message{Role: "tool", ToolCallId: "call_1", Content: "first"},
		dto.Message{Role: "tool", ToolCallId: "call_2", Content: []any{
			map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:image/jpeg;base64,SlBH"}},
		}},
	)

	got, err := convertToolResultMessages(t, messages)
	require.NoError(t, err)
	last := got[len(got)-1]["content"].([]any)
	require.Len(t, last, 2)
	assert.Equal(t, "call_1", last[0].(map[string]any)["tool_use_id"])
	assert.Equal(t, "first", last[0].(map[string]any)["content"])
	assert.Equal(t, "call_2", last[1].(map[string]any)["tool_use_id"])
	assert.Equal(t, []any{
		map[string]any{"type": "image", "source": map[string]any{"type": "base64", "media_type": "image/jpeg", "data": "SlBH"}},
	}, last[1].(map[string]any)["content"])
}

func TestOpenAIChatToolResultRejectsUnconvertibleMedia(t *testing.T) {
	installToolResultMediaResolver(t, map[string][2]string{
		"https://example.test/voice.mp3": {"audio/mpeg", "TVAz"},
	})
	tests := []struct {
		name    string
		part    map[string]any
		wantErr string
	}{
		{
			name:    "missing url",
			part:    map[string]any{"type": "image_url", "image_url": map[string]any{}},
			wantErr: "messages[4](tool).content[1]",
		},
		{
			name:    "file_id only",
			part:    map[string]any{"type": "file", "file": map[string]any{"file_id": "file-abc"}},
			wantErr: "missing url or file_data",
		},
		{
			name:    "media type outside allowlist",
			part:    map[string]any{"type": "image_url", "image_url": map[string]any{"url": "https://example.test/voice.mp3"}},
			wantErr: `unsupported media type "audio/mpeg"`,
		},
		{
			name:    "fetch failure",
			part:    map[string]any{"type": "image_url", "image_url": map[string]any{"url": "https://example.test/missing.png"}},
			wantErr: "get file data failed at messages[4](tool).content[1]",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 前置两条连续 user 字符串消息会被合并，工具消息在原始请求中的下标是 4
			messages := []dto.Message{{Role: "user", Content: "hello"}}
			messages = append(messages, toolCallHistory(t, "call_1")...)
			messages = append(messages,
				dto.Message{Role: "user", Content: "still there?"},
				dto.Message{Role: "tool", ToolCallId: "call_1", Content: []any{
					map[string]any{"type": "text", "text": "result"},
					tt.part,
				}},
			)
			_, err := convertToolResultMessages(t, messages)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}
