package openai

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func convertChatForTest(t *testing.T, body string) []byte {
	t.Helper()
	var request dto.GeneralOpenAIRequest
	require.NoError(t, common.Unmarshal([]byte(body), &request))
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeChatCompletions,
		OriginModelName: request.Model,
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenAI, UpstreamModelName: request.Model},
	}
	converted, err := (&Adaptor{}).ConvertOpenAIRequest(c, info, &request)
	require.NoError(t, err)
	data, err := common.Marshal(converted)
	require.NoError(t, err)
	return data
}

func convertResponsesForTest(t *testing.T, body string) []byte {
	t.Helper()
	var request dto.OpenAIResponsesRequest
	require.NoError(t, common.Unmarshal([]byte(body), &request))
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: request.Model,
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenAI, UpstreamModelName: request.Model},
	}
	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(c, info, request)
	require.NoError(t, err)
	data, err := common.Marshal(converted)
	require.NoError(t, err)
	return data
}

func TestConvertOpenAIRequestMaxEffortPerEndpoint(t *testing.T) {
	tests := []struct {
		model             string
		expectedResponses string
	}{
		{model: "gpt-6-sol", expectedResponses: "max"},
		{model: "gpt-5.6-terra", expectedResponses: "max"},
		{model: "gpt-6.1-sol", expectedResponses: "max"},
		{model: "gpt-5.4", expectedResponses: "xhigh"},
	}
	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			chat := convertChatForTest(t, `{"model":"`+tt.model+`","messages":[{"role":"user","content":"hi"}],"reasoning_effort":"max"}`)
			assert.Equal(t, "xhigh", gjson.GetBytes(chat, "reasoning_effort").String())

			responses := convertResponsesForTest(t, `{"model":"`+tt.model+`","input":"hi","reasoning":{"effort":"max"}}`)
			assert.Equal(t, tt.expectedResponses, gjson.GetBytes(responses, "reasoning.effort").String())
		})
	}
}

func TestConvertOpenAIRequestSamplingParamsFollowNoneEffort(t *testing.T) {
	tests := []struct {
		name         string
		model        string
		effort       string
		keepSampling bool
	}{
		{name: "kept when reasoning disabled", model: "gpt-6-sol", effort: "none", keepSampling: true},
		{name: "kept for dated gpt-5.6 when reasoning disabled", model: "gpt-5.6-sol-2026-07-09", effort: "none", keepSampling: true},
		{name: "dropped with reasoning enabled", model: "gpt-6-sol", effort: "high", keepSampling: false},
		{name: "dropped without explicit effort", model: "gpt-6-luna", effort: "", keepSampling: false},
		{name: "dropped for model rejecting none", model: "gpt-6-astra", effort: "none", keepSampling: false},
		// 官方能力表：gpt-5.1/5.2/5.4 标准型号关闭推理时支持采样参数，具名变体不继承
		{name: "kept for gpt-5.4 when reasoning disabled", model: "gpt-5.4", effort: "none", keepSampling: true},
		{name: "dropped for gpt-5.4 named variant", model: "gpt-5.4-pro", effort: "none", keepSampling: false},
		{name: "dropped for gpt-5.4 with reasoning enabled", model: "gpt-5.4", effort: "high", keepSampling: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			effort := ""
			if tt.effort != "" {
				effort = `,"reasoning_effort":"` + tt.effort + `"`
			}
			data := convertChatForTest(t, `{"model":"`+tt.model+`","messages":[{"role":"user","content":"hi"}],"temperature":0.2,"top_p":0.5,"logprobs":true,"top_logprobs":3`+effort+`}`)
			assert.Equal(t, tt.keepSampling, gjson.GetBytes(data, "temperature").Exists())
			assert.Equal(t, tt.keepSampling, gjson.GetBytes(data, "top_p").Exists())
			assert.Equal(t, tt.keepSampling, gjson.GetBytes(data, "logprobs").Exists())
			assert.Equal(t, tt.keepSampling, gjson.GetBytes(data, "top_logprobs").Exists())
		})
	}
}

func TestConvertOpenAIRequestPreservesOfficialChatFields(t *testing.T) {
	data := convertChatForTest(t, `{"model":"gpt-6-sol","messages":[{"role":"user","content":"hi"},{"role":"assistant","content":"x","refusal":"no","audio":{"id":"audio_1"}}],"stream":true,"stream_options":{"include_usage":true,"include_obfuscation":false},"tools":[{"type":"function","function":{"name":"f","parameters":{"type":"object"}}},{"type":"custom","custom":{"name":"c","format":{"type":"text"}}}]}`)

	assert.Equal(t, "no", gjson.GetBytes(data, "messages.1.refusal").String())
	assert.Equal(t, "audio_1", gjson.GetBytes(data, "messages.1.audio.id").String())
	assert.True(t, gjson.GetBytes(data, "stream_options.include_obfuscation").Exists())
	assert.False(t, gjson.GetBytes(data, "stream_options.include_obfuscation").Bool())
	assert.Equal(t, "f", gjson.GetBytes(data, "tools.0.function.name").String())
	assert.False(t, gjson.GetBytes(data, "tools.1.function").Exists())
	assert.Equal(t, "c", gjson.GetBytes(data, "tools.1.custom.name").String())
}
