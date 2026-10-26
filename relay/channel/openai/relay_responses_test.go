package openai

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOaiResponsesHandlerMapsInputTokenDetails(t *testing.T) {
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })

	body := `{
		"id": "resp_test",
		"usage": {
			"input_tokens": 162,
			"output_tokens": 1065,
			"total_tokens": 1227,
			"input_tokens_details": {
				"cached_tokens": 3,
				"cache_write_tokens": 7,
				"text_tokens": 63,
				"audio_tokens": 96,
				"image_tokens": 0
			}
		}
	}`

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
	}
	info := &relaycommon.RelayInfo{}

	usage, err := OaiResponsesHandler(c, info, resp)

	require.Nil(t, err)
	require.NotNil(t, usage)
	assert.Equal(t, 162, usage.PromptTokens)
	assert.Equal(t, 1065, usage.CompletionTokens)
	assert.Equal(t, 1227, usage.TotalTokens)
	assert.Equal(t, 3, usage.PromptTokensDetails.CachedTokens)
	assert.Equal(t, 7, usage.PromptTokensDetails.CacheWriteTokens)
	assert.Equal(t, 63, usage.PromptTokensDetails.TextTokens)
	assert.Equal(t, 96, usage.PromptTokensDetails.AudioTokens)
	assert.Equal(t, 0, usage.PromptTokensDetails.ImageTokens)
	assert.Equal(t, "resp_test", info.UpstreamResponseId)
}

// Meta /v1/responses 会把时间戳编码成整数值浮点（例：1792990141.0），
// 归一化测试覆盖：改写、幂等、null/字符串/非整数浮点不动、未知字段保留。
func TestNormalizeResponsesTimestamps(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		// wantContains: 改写后必须出现的片段；wantIdentical: true 表示必须与输入逐字节相同
		wantContains  []string
		wantIdentical bool
	}{
		{
			name: "meta float created_at and completed_at rewritten",
			input: `{"id":"resp_1","object":"response","created_at":1792990141.0,` +
				`"completed_at":1792990142.0,"status":"completed","top_logprobs":0,` +
				`"usage":{"input_tokens":15,"output_tokens":52}}`,
			wantContains: []string{`"created_at":1792990141`, `"completed_at":1792990142`},
		},
		{
			name: "exponent form float rewritten",
			input: `{"created_at":1.792987007e9,"status":"completed"}`,
			wantContains: []string{`"created_at":1792987007`},
		},
		{
			name:          "already integer encoding untouched",
			input:         `{"id":"resp_2","created_at":1792990141,"status":"completed"}`,
			wantIdentical: true,
		},
		{
			name:          "null timestamps untouched",
			input:         `{"created_at":null,"completed_at":null,"status":"in_progress"}`,
			wantIdentical: true,
		},
		{
			name:          "non-integral float untouched",
			input:         `{"created_at":2.5,"status":"completed"}`,
			wantIdentical: true,
		},
		{
			name:          "string timestamp untouched",
			input:         `{"created_at":"1792990141"}`,
			wantIdentical: true,
		},
		{
			name:          "no timestamp fields untouched",
			input:         `{"id":"resp_3","status":"completed","usage":{"input_tokens":1}}`,
			wantIdentical: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			out := normalizeResponsesTimestamps([]byte(tt.input))
			if tt.wantIdentical {
				require.Equal(t, tt.input, string(out))
				return
			}
			for _, frag := range tt.wantContains {
				assert.Contains(t, string(out), frag)
			}
			assert.NotContains(t, string(out), "1792990141.0")
		})
	}
}

func TestNormalizeResponsesTimestampsKeepsUnknownFieldsAndValues(t *testing.T) {
	t.Parallel()

	// Meta 超发的未文档化字段必须原值保留，不能因结构体缺字段而丢失
	input := []byte(`{"created_at":1792990141.0,"top_logprobs":0,"temperature":1.0,` +
		`"service_tier":"auto","background":false,"output":[{"type":"message","text":"hi"}]}`)
	out := string(normalizeResponsesTimestamps(input))

	assert.Contains(t, out, `"created_at":1792990141`)
	var parsed map[string]json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(out), &parsed))
	assert.JSONEq(t, `0`, string(parsed["top_logprobs"]))
	assert.JSONEq(t, `1.0`, string(parsed["temperature"]))
	assert.JSONEq(t, `"auto"`, string(parsed["service_tier"]))
	assert.JSONEq(t, `false`, string(parsed["background"]))
	assert.JSONEq(t, `[{"type":"message","text":"hi"}]`, string(parsed["output"]))
}

func TestNormalizeResponsesEventTimestamps(t *testing.T) {
	t.Parallel()

	t.Run("rewrites timestamps inside response sub-object", func(t *testing.T) {
		t.Parallel()

		event := `{"type":"response.created","response":{"id":"resp_1",` +
			`"object":"response","created_at":1792990141.0,"completed_at":1792990142.0,` +
			`"status":"in_progress","top_logprobs":0}}`
		out := normalizeResponsesEventTimestamps(event)
		assert.Contains(t, out, `"created_at":1792990141`)
		assert.Contains(t, out, `"completed_at":1792990142`)
		assert.NotContains(t, out, "1792990141.0")
		// response 子对象外的字段保留
		assert.Contains(t, out, `"type":"response.created"`)
		var parsed map[string]json.RawMessage
		require.NoError(t, json.Unmarshal([]byte(out), &parsed))
		require.Contains(t, parsed, "response")
	})

	t.Run("delta event passes through unchanged", func(t *testing.T) {
		t.Parallel()

		event := `{"type":"response.output_text.delta","item_id":"msg_1","output_index":0,"content_index":0,"delta":"hello"}`
		require.Equal(t, event, normalizeResponsesEventTimestamps(event))
	})

	t.Run("event without response sub-object unchanged", func(t *testing.T) {
		t.Parallel()

		event := `{"type":"response.output_item.added","created_at_marker":false}`
		require.Equal(t, event, normalizeResponsesEventTimestamps(event))
	})
}
