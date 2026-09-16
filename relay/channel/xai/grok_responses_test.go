package xai

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func grokResponsesRelayInfo(channelType int, upstreamModel string) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       channelType,
			UpstreamModelName: upstreamModel,
		},
		RelayMode: relayconstant.RelayModeResponses,
	}
}

func TestPrepareGrok46ResponsesRequestRejectsCustomToolOnlyForXAIUpstream(t *testing.T) {
	req := dto.OpenAIResponsesRequest{
		Model: "grok-4.6",
		Tools: json.RawMessage(`[{"type":"custom","name":"apply_patch"}]`),
	}

	got, err := prepareGrok46ResponsesRequest(grokResponsesRelayInfo(constant.ChannelTypeXai, "grok-4.6"), req)
	require.NoError(t, err)
	assert.JSONEq(t, `[]`, string(got.Tools))
}

func TestPrepareGrok46ResponsesRequestLeavesNonGrokRequestBytesUntouched(t *testing.T) {
	tools := json.RawMessage(`[{"type":"mcp","server_label":"repo","server_url":"https://example.test/mcp","connector_id":"keep-me","require_approval":"always"}]`)
	req := dto.OpenAIResponsesRequest{
		Model: "claude-opus-4-7",
		Tools: tools,
	}

	got, err := prepareGrok46ResponsesRequest(grokResponsesRelayInfo(constant.ChannelTypeXai, "claude-opus-4-7"), req)
	require.NoError(t, err)
	assert.Equal(t, string(tools), string(got.Tools))
}

func TestPrepareGrok46ResponsesRequestSanitizesOnlyUnsupportedMCPFields(t *testing.T) {
	req := dto.OpenAIResponsesRequest{
		Model: "grok-4.6",
		Tools: json.RawMessage(`[{"type":"mcp","server_label":"repo","server_url":"https://example.test/mcp","connector_id":"openai-connector","require_approval":"always","allowed_tools":["read_file"]}]`),
	}

	got, err := prepareGrok46ResponsesRequest(grokResponsesRelayInfo(constant.ChannelTypeXai, "grok-4.6"), req)
	require.NoError(t, err)
	assert.JSONEq(t, `[{"type":"mcp","server_label":"repo","server_url":"https://example.test/mcp","allowed_tools":["read_file"]}]`, string(got.Tools))
}

func TestPrepareGrok46ResponsesRequestPreservesNativeXAIToolsAndStructuredOutput(t *testing.T) {
	tools := json.RawMessage(`[{"type":"web_search"},{"type":"x_search","allowed_x_handles":["xai"]},{"type":"code_interpreter"}]`)
	textFormat := json.RawMessage(`{"format":{"type":"json_schema","name":"answer","schema":{"type":"object","properties":{"ok":{"type":"boolean"}}}}}`)
	req := dto.OpenAIResponsesRequest{
		Model: "grok-4.6",
		Tools: tools,
		Text:  textFormat,
	}

	got, err := prepareGrok46ResponsesRequest(grokResponsesRelayInfo(constant.ChannelTypeXai, "grok-4.6"), req)
	require.NoError(t, err)
	assert.Equal(t, string(tools), string(got.Tools))
	assert.Equal(t, string(textFormat), string(got.Text))
}

func TestPrepareGrok46ResponsesRequestKeepsEveryXAIAdvertisedToolType(t *testing.T) {
	tools := json.RawMessage(`[
		{"type":"function","name":"read_file","parameters":{"type":"object"}},
		{"type":"web_search"},
		{"type":"x_search"},
		{"type":"image_generation"},
		{"type":"collections_search"},
		{"type":"file_search","vector_store_ids":["collection_1"]},
		{"type":"code_execution"},
		{"type":"code_interpreter"},
		{"type":"mcp","server_label":"repo","server_url":"https://example.test/mcp"},
		{"type":"shell","environment":{"type":"container_auto"}},
		{"type":"tool_search"}
	]`)

	got, err := prepareGrok46ResponsesRequest(grokResponsesRelayInfo(constant.ChannelTypeXai, "grok-4.6"), dto.OpenAIResponsesRequest{Tools: tools})
	require.NoError(t, err)
	assert.Equal(t, string(tools), string(got.Tools))
}

func TestPrepareGrok46ResponsesRequestDropsToolsMissingRequiredXAIFields(t *testing.T) {
	tools := json.RawMessage(`[
		{"type":"function","parameters":{"type":"object"}},
		{"type":"file_search"},
		{"type":"mcp","server_label":"repo"},
		{"type":"shell"},
		{"type":"web_search"}
	]`)

	got, err := prepareGrok46ResponsesRequest(grokResponsesRelayInfo(constant.ChannelTypeXai, "grok-4.6"), dto.OpenAIResponsesRequest{Tools: tools})
	require.NoError(t, err)
	assert.JSONEq(t, `[{"type":"web_search"}]`, string(got.Tools))
}

func TestPrepareGrok46ResponsesRequestDropsUnsupportedNamespaceTool(t *testing.T) {
	req := dto.OpenAIResponsesRequest{
		Model: "grok-4.6",
		Tools: json.RawMessage(`[
			{"type":"web_search"},
			{"type":"namespace","name":"mcp__codex_apps__github","tools":[{"type":"function","name":"list_issues"}]},
			{"type":"x_search"}
		]`),
	}

	got, err := prepareGrok46ResponsesRequest(grokResponsesRelayInfo(constant.ChannelTypeXai, "grok-4.6"), req)
	require.NoError(t, err)
	assert.JSONEq(t, `[{"type":"web_search"},{"type":"x_search"}]`, string(got.Tools))
}

func TestPrepareGrok46OutboundRequestBodyFiltersToolsWhenPassThroughIsEnabled(t *testing.T) {
	body := strings.NewReader(`{
		"model":"grok-4.6",
		"input":"keep this field",
		"tools":[
			{"type":"web_search"},
			{"type":"custom","name":"apply_patch"},
			{"type":"namespace","name":"mcp__codex_apps__github"}
		]
	}`)

	prepared, err := prepareGrok46OutboundRequestBody(grokResponsesRelayInfo(constant.ChannelTypeXai, "grok-4.6"), body)
	require.NoError(t, err)
	encoded, err := io.ReadAll(prepared)
	require.NoError(t, err)
	assert.JSONEq(t, `{"model":"grok-4.6","input":"keep this field","tools":[{"type":"web_search"}]}`, string(encoded))
}

func TestPrepareGrok46OutboundRequestBodyDropsUnsupportedExternalWebAccess(t *testing.T) {
	body := strings.NewReader(`{
		"model":"grok-4.6",
		"input":"keep this field",
		"external_web_access":false,
		"tools":[{"type":"web_search"}]
	}`)

	prepared, err := prepareGrok46OutboundRequestBody(grokResponsesRelayInfo(constant.ChannelTypeXai, "grok-4.6"), body)
	require.NoError(t, err)
	encoded, err := io.ReadAll(prepared)
	require.NoError(t, err)
	assert.JSONEq(t, `{"model":"grok-4.6","input":"keep this field","tools":[{"type":"web_search"}]}`, string(encoded))
}

func TestPrepareGrok46OutboundRequestBodyAppliesXAIResponsesSchema(t *testing.T) {
	body := strings.NewReader(`{
		"model":"grok-4.6",
		"input":"keep",
		"instructions":"keep",
		"reasoning":{"effort":"high"},
		"stream":true,
		"prompt_cache_key":"keep",
		"client_metadata":{"thread_id":"drop"},
		"context_management":{"mode":"drop"},
		"include":["reasoning.encrypted_content"],
		"prompt_cache_options":{"mode":"explicit"},
		"stream_options":{"include_usage":true},
		"frequency_penalty":0.2,
		"presence_penalty":0.2,
		"tools":[
			{"type":"web_search","filters":{"allowed_domains":["x.ai"]},"external_web_access":false,"search_context_size":"high","user_location":{"type":"approximate"}},
			{"type":"file_search","vector_store_ids":["collection_1"],"filters":{"type":"eq"},"ranking_options":{"ranker":"auto"}},
			{"type":"code_interpreter","container":{"type":"auto"}},
			{"type":"function","name":"keep","description":"keep","parameters":{"type":"object"}}
		],
		"tool_choice":{"type":"custom","name":"apply_patch"}
	}`)

	prepared, err := prepareGrok46OutboundRequestBody(grokResponsesRelayInfo(constant.ChannelTypeXai, "grok-4.6"), body)
	require.NoError(t, err)
	encoded, err := io.ReadAll(prepared)
	require.NoError(t, err)
	assert.JSONEq(t, `{
		"model":"grok-4.6",
		"input":"keep",
		"instructions":"keep",
		"reasoning":{"effort":"high"},
		"stream":true,
		"prompt_cache_key":"keep",
		"tools":[
			{"type":"web_search","filters":{"allowed_domains":["x.ai"]}},
			{"type":"file_search","vector_store_ids":["collection_1"]},
			{"type":"code_interpreter"},
			{"type":"function","name":"keep","description":"keep","parameters":{"type":"object"}}
		]
	}`, string(encoded))
}

func TestPrepareGrok46OutboundRequestBodyDropsUnsupportedReasoningEffort(t *testing.T) {
	body := strings.NewReader(`{
		"model":"grok-4.6",
		"input":"keep",
		"reasoning":{"effort":"none","summary":"auto"}
	}`)

	prepared, err := prepareGrok46OutboundRequestBody(grokResponsesRelayInfo(constant.ChannelTypeXai, "grok-4.6"), body)
	require.NoError(t, err)
	encoded, err := io.ReadAll(prepared)
	require.NoError(t, err)
	assert.JSONEq(t, `{"model":"grok-4.6","input":"keep","reasoning":{"summary":"auto"}}`, string(encoded))
}

func TestPrepareGrok46OutboundRequestBodyKeepsSupportedReasoningEfforts(t *testing.T) {
	for _, effort := range []string{"low", "medium", "high", "xhigh"} {
		t.Run(effort, func(t *testing.T) {
			body := strings.NewReader(`{"model":"grok-4.6","input":"keep","reasoning":{"effort":"` + effort + `"}}`)
			prepared, err := prepareGrok46OutboundRequestBody(grokResponsesRelayInfo(constant.ChannelTypeXai, "grok-4.6"), body)
			require.NoError(t, err)
			encoded, err := io.ReadAll(prepared)
			require.NoError(t, err)
			assert.JSONEq(t, `{"model":"grok-4.6","input":"keep","reasoning":{"effort":"`+effort+`"}}`, string(encoded))
		})
	}
}

func TestPrepareGrok46OutboundRequestBodyNormalizesNullReasoningInputContent(t *testing.T) {
	body := strings.NewReader(`{
		"model":"grok-4.6",
		"input":[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]},
			{"type":"reasoning","id":"rs_123","summary":[],"content":null}
		]
	}`)

	prepared, err := prepareGrok46OutboundRequestBody(grokResponsesRelayInfo(constant.ChannelTypeXai, "grok-4.6"), body)
	require.NoError(t, err)
	encoded, err := io.ReadAll(prepared)
	require.NoError(t, err)
	assert.JSONEq(t, `{
		"model":"grok-4.6",
		"input":[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]},
			{"type":"reasoning","id":"rs_123","summary":[],"content":[]}
		]
	}`, string(encoded))
}

func TestPrepareGrok46OutboundRequestBodyLeavesNonGrokBytesUntouched(t *testing.T) {
	rawBody := []byte(`{"tools":[{"type":"namespace","name":"keep"}],"input":[{"type":"reasoning","content":null}]}`)
	prepared, err := prepareGrok46OutboundRequestBody(grokResponsesRelayInfo(constant.ChannelTypeXai, "grok-4.1"), bytes.NewReader(rawBody))
	require.NoError(t, err)
	got, err := io.ReadAll(prepared)
	require.NoError(t, err)
	assert.Equal(t, rawBody, got)
}

func TestPrepareGrok46OutboundRequestBodyLeavesChatCompletionsBytesUntouched(t *testing.T) {
	rawBody := []byte(`{"model":"grok-4.6","messages":[{"role":"user","content":"hello"}],"max_tokens":100,"search_parameters":{"mode":"auto"}}`)
	info := grokResponsesRelayInfo(constant.ChannelTypeXai, "grok-4.6")
	info.RelayMode = relayconstant.RelayModeChatCompletions

	prepared, err := prepareGrok46OutboundRequestBody(info, bytes.NewReader(rawBody))
	require.NoError(t, err)
	got, err := io.ReadAll(prepared)
	require.NoError(t, err)
	assert.Equal(t, rawBody, got)
}

func TestPrepareGrok46OutboundRequestBodyLeavesNonJSONAudioBytesUntouched(t *testing.T) {
	rawBody := []byte("--multipart-boundary\r\nContent-Disposition: form-data; name=\"file\"\r\n\r\naudio\r\n--multipart-boundary--\r\n")
	info := grokResponsesRelayInfo(constant.ChannelTypeXai, "grok-4.6")
	info.RelayMode = relayconstant.RelayModeAudioTranscription

	prepared, err := prepareGrok46OutboundRequestBody(info, bytes.NewReader(rawBody))
	require.NoError(t, err)
	got, err := io.ReadAll(prepared)
	require.NoError(t, err)
	assert.Equal(t, rawBody, got)
}

func TestIsXaiGrok46ToolChoice(t *testing.T) {
	for _, test := range []struct {
		name string
		raw  string
		keep bool
	}{
		{name: "auto", raw: `"auto"`, keep: true},
		{name: "required", raw: `"required"`, keep: true},
		{name: "function", raw: `{"type":"function","name":"read_file"}`, keep: true},
		{name: "custom", raw: `{"type":"custom","name":"apply_patch"}`, keep: false},
		{name: "shell", raw: `{"type":"shell"}`, keep: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			keep, err := isXaiGrok46ToolChoice(json.RawMessage(test.raw))
			require.NoError(t, err)
			assert.Equal(t, test.keep, keep)
		})
	}
}

func TestXAIResponsesCompactUsesResponsesCompactHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body: io.NopCloser(strings.NewReader(`{
			"id":"resp_compact_grok_46",
			"object":"response.compaction",
			"output":[],
			"usage":{"input_tokens":13,"output_tokens":5,"total_tokens":18}
		}`)),
		Header: make(http.Header),
	}
	info := grokResponsesRelayInfo(constant.ChannelTypeXai, "grok-4.6")
	info.RelayMode = relayconstant.RelayModeResponsesCompact

	usage, apiErr := (&Adaptor{}).DoResponse(c, resp, info)
	require.Nil(t, apiErr)
	got, ok := usage.(*dto.Usage)
	require.True(t, ok)
	assert.Equal(t, 13, got.PromptTokens)
	assert.Equal(t, 5, got.CompletionTokens)
	assert.Equal(t, 18, got.TotalTokens)
}
