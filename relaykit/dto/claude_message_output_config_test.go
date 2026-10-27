package dto

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 原生 /v1/messages 默认路径会解码为 ClaudeRequest 再重新序列化发上游，
// per-message effort 的消息级 output_config 必须在 round-trip 后原样保留，
// 未携带的消息序列化输出不得出现该键。
func TestClaudeRequestRoundTripKeepsMessageOutputConfig(t *testing.T) {
	raw := `{"model":"claude-test","max_tokens":1024,"output_config":{"effort":"high"},"messages":[` +
		`{"role":"user","content":"Plan a migration."},` +
		`{"role":"assistant","content":"1. Export the data."},` +
		`{"role":"system","content":[],"output_config":{"effort":"low"}},` +
		`{"role":"user","content":"Summarize."}]}`

	var request ClaudeRequest
	require.NoError(t, json.Unmarshal([]byte(raw), &request))
	body, err := json.Marshal(request)
	require.NoError(t, err)

	var decoded struct {
		Messages []map[string]any `json:"messages"`
	}
	require.NoError(t, json.Unmarshal(body, &decoded))
	require.Len(t, decoded.Messages, 4)
	assert.Equal(t, map[string]any{
		"role":          "system",
		"content":       []any{},
		"output_config": map[string]any{"effort": "low"},
	}, decoded.Messages[2])
	for _, index := range []int{0, 1, 3} {
		assert.NotContains(t, decoded.Messages[index], "output_config")
	}
}
