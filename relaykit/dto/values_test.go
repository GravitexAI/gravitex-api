package dto

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIntValueUnmarshalJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    IntValue
		wantErr bool
	}{
		{name: "plain int", input: `123`, want: 123},
		{name: "negative int", input: `-45`, want: -45},
		{name: "zero", input: `0`, want: 0},
		{name: "integral float", input: `1792987007.0`, want: 1792987007},
		{name: "integral float negative", input: `-3.0`, want: -3},
		{name: "integral float compact", input: `42.0`, want: 42},
		{name: "non-integral float rejected", input: `2.5`, wantErr: true},
		{name: "non-integral float negative rejected", input: `-0.5`, wantErr: true},
		{name: "float beyond int64 max rejected", input: `1e300`, wantErr: true},
		{name: "float just above int64 max rejected", input: `9.3e18`, wantErr: true},
		{name: "numeric string", input: `"77"`, want: 77},
		{name: "non-numeric string rejected", input: `"abc"`, wantErr: true},
		{name: "bool rejected", input: `true`, wantErr: true},
		// 与原生 int 字段语义一致：std json 对 null 不报错，值保持零值
		{name: "null accepted like plain int", input: `null`, want: 0},
		{name: "object rejected", input: `{"a":1}`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var got IntValue
			err := json.Unmarshal([]byte(tt.input), &got)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIntValueMarshalJSONEmitsInt(t *testing.T) {
	t.Parallel()

	out, err := json.Marshal(IntValue(1792987007))
	require.NoError(t, err)
	assert.Equal(t, "1792987007", string(out))
}

// TestOpenAIResponsesResponseMetaFloatCreatedAt 复现 Meta /v1/responses 的线上行为：
// created_at 编码为整数值浮点（1792987007.0），修复前 std json 解进 int 字段
// 会直接报错，导致整个响应/流事件解析失败。
func TestOpenAIResponsesResponseMetaFloatCreatedAt(t *testing.T) {
	t.Parallel()

	const metaBody = `{
		"id": "resp_meta_fixture",
		"object": "response",
		"created_at": 1792987007.0,
		"status": "completed",
		"model": "muse-wonder-1.0",
		"output": [
			{
				"type": "message",
				"id": "msg_meta_fixture",
				"role": "assistant",
				"content": [
					{"type": "output_text", "text": "hello from responses"}
				]
			}
		],
		"usage": {
			"input_tokens": 17,
			"output_tokens": 5,
			"total_tokens": 22,
			"output_tokens_details": {"reasoning_tokens": 0}
		}
	}`

	var resp OpenAIResponsesResponse
	require.NoError(t, json.Unmarshal([]byte(metaBody), &resp))
	assert.Equal(t, IntValue(1792987007), resp.CreatedAt)
	assert.Equal(t, "resp_meta_fixture", resp.ID)
	require.NotNil(t, resp.Usage)
	assert.Equal(t, 17, resp.Usage.InputTokens)
	assert.Equal(t, 5, resp.Usage.OutputTokens)

	// 往客户端回写时仍是标准 int 编码
	out, err := json.Marshal(&resp)
	require.NoError(t, err)
	assert.Contains(t, string(out), `"created_at":1792987007`)
	assert.NotContains(t, string(out), "1792987007.0")
}
