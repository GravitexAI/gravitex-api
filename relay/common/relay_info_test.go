package common

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRelayInfoGetFinalRequestRelayFormatPrefersExplicitFinal(t *testing.T) {
	info := &RelayInfo{
		RelayFormat:             types.RelayFormatOpenAI,
		RequestConversionChain:  []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatClaude},
		FinalRequestRelayFormat: types.RelayFormatOpenAIResponses,
	}

	require.Equal(t, types.RelayFormat(types.RelayFormatOpenAIResponses), info.GetFinalRequestRelayFormat())
}

func TestRelayInfoGetFinalRequestRelayFormatFallsBackToConversionChain(t *testing.T) {
	info := &RelayInfo{
		RelayFormat:            types.RelayFormatOpenAI,
		RequestConversionChain: []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatClaude},
	}

	require.Equal(t, types.RelayFormat(types.RelayFormatClaude), info.GetFinalRequestRelayFormat())
}

func TestRelayInfoGetFinalRequestRelayFormatFallsBackToRelayFormat(t *testing.T) {
	info := &RelayInfo{
		RelayFormat: types.RelayFormatGemini,
	}

	require.Equal(t, types.RelayFormat(types.RelayFormatGemini), info.GetFinalRequestRelayFormat())
}

func TestRelayInfoGetFinalRequestRelayFormatNilReceiver(t *testing.T) {
	var info *RelayInfo
	require.Equal(t, types.RelayFormat(""), info.GetFinalRequestRelayFormat())
}

func TestRelayInfoMetaTypedNilReceiver(t *testing.T) {
	var info *RelayInfo
	var meta convmeta.Meta = info

	assert.Empty(t, meta.GetOriginModelName())
	assert.Empty(t, meta.GetUpstreamModelName())
	assert.False(t, meta.HasChannelMeta())
	assert.Zero(t, meta.GetChannelID())
	assert.Zero(t, meta.GetChannelType())
	assert.False(t, meta.GetIsStream())
	assert.Empty(t, meta.GetReasoningEffort())
	assert.Nil(t, meta.ReasoningState())
	assert.Zero(t, meta.GetEstimatePromptTokens())
	assert.Zero(t, meta.GetSendResponseCount())

	assert.NotPanics(t, func() {
		meta.SetReasoningEffort("high")
		meta.IncrSendResponseCount()
		meta.AppendRequestConversion(types.RelayFormatClaude)
	})

	firstState := meta.EnsureClaudeConvertInfo()
	secondState := meta.EnsureClaudeConvertInfo()
	require.NotNil(t, firstState)
	require.NotNil(t, secondState)
	assert.Equal(t, convmeta.LastMessageTypeNone, firstState.LastMessagesType)
	assert.NotSame(t, firstState, secondState)

	firstOptions := meta.ConvOptions()
	secondOptions := meta.ConvOptions()
	require.NotNil(t, firstOptions)
	require.NotNil(t, secondOptions)
	assert.NotSame(t, firstOptions, secondOptions)
	assert.NotNil(t, firstOptions.Claude.DefaultMaxTokens)
	assert.NotNil(t, firstOptions.Gemini.SupportsImagine)
	assert.NotNil(t, firstOptions.Gemini.SafetySetting)
	assert.NotNil(t, firstOptions.PreserveThinkingSuffix)
	assert.NotNil(t, firstOptions.PreserveEffortTail)
}

func TestRelayInfoUsageConversionAndRequestFormatConversion(t *testing.T) {
	info := &RelayInfo{
		RequestConversionChain: []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatGemini},
	}
	info.SetUsageConversion(map[string]any{
		"promptTokenCount": 12,
		"nested":           map[string]any{"tokenCount": 3},
	})

	require.True(t, info.HasRequestFormatConversion())
	require.Equal(t, map[string]any{
		"promptTokenCount": float64(12),
		"nested":           map[string]any{"tokenCount": float64(3)},
	}, info.UsageConversion)

	info.RequestConversionChain = []types.RelayFormat{types.RelayFormatOpenAI}
	assert.False(t, info.HasRequestFormatConversion())
	info.RequestConversionChain = []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatOpenAI}
	assert.False(t, info.HasRequestFormatConversion())
}

func TestGenBaseRelayInfoPreservesNativeInteractionsMarkerAfterPathRewrite(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/video/generations", nil)
	c.Set("native_interactions", true)

	info := genBaseRelayInfo(c, nil)

	require.True(t, info.NativeInteractions)
	require.Equal(t, "/v1/video/generations", info.RequestURLPath)
}

func TestGenRelayInfoCapturesRequestReasoningEffort(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name        string
		path        string
		relayFormat types.RelayFormat
		request     dto.Request
		expected    string
	}{
		{
			name:        "OpenAI chat top-level effort",
			path:        "/v1/chat/completions",
			relayFormat: types.RelayFormatOpenAI,
			request:     &dto.GeneralOpenAIRequest{Model: "gpt-5.6-sol", ReasoningEffort: " high "},
			expected:    "high",
		},
		{
			name:        "OpenRouter nested chat effort",
			path:        "/v1/chat/completions",
			relayFormat: types.RelayFormatOpenAI,
			request:     &dto.GeneralOpenAIRequest{Model: "anthropic/claude", Reasoning: json.RawMessage(`{"effort":"xhigh"}`)},
			expected:    "xhigh",
		},
		{
			name:        "OpenAI Responses effort",
			path:        "/v1/responses",
			relayFormat: types.RelayFormatOpenAIResponses,
			request:     &dto.OpenAIResponsesRequest{Model: "gpt-5.6-sol", Reasoning: &dto.Reasoning{Effort: "max"}},
			expected:    "max",
		},
		{
			name:        "explicit none is preserved",
			path:        "/v1/responses",
			relayFormat: types.RelayFormatOpenAIResponses,
			request:     &dto.OpenAIResponsesRequest{Model: "gpt-5.6-sol", Reasoning: &dto.Reasoning{Effort: "none"}},
			expected:    "none",
		},
		{
			name:        "non-string nested effort is ignored",
			path:        "/v1/chat/completions",
			relayFormat: types.RelayFormatOpenAI,
			request:     &dto.GeneralOpenAIRequest{Model: "anthropic/claude", Reasoning: json.RawMessage(`{"effort":42}`)},
			expected:    "",
		},
		{
			name:        "Claude output config effort",
			path:        "/v1/messages",
			relayFormat: types.RelayFormatClaude,
			request:     &dto.ClaudeRequest{Model: "claude-opus-4-7", OutputConfig: json.RawMessage(`{"effort":"medium"}`)},
			expected:    "medium",
		},
		{
			name:        "Gemini thinking level",
			path:        "/v1beta/models/gemini-3-pro:generateContent",
			relayFormat: types.RelayFormatGemini,
			request: &dto.GeminiChatRequest{GenerationConfig: dto.GeminiChatGenerationConfig{
				ThinkingConfig: &dto.GeminiThinkingConfig{ThinkingLevel: "low"},
			}},
			expected: "low",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest("POST", tt.path, nil)

			info, err := GenRelayInfo(ctx, tt.relayFormat, tt.request, nil)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, info.ReasoningEffort)
		})
	}
}

func TestInitChannelMetaRestoresRequestReasoningEffortForRetry(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	request := &dto.OpenAIResponsesRequest{
		Model:     "gpt-5.6-sol",
		Reasoning: &dto.Reasoning{Effort: "max"},
	}
	info, err := GenRelayInfo(ctx, types.RelayFormatOpenAIResponses, request, nil)
	require.NoError(t, err)

	info.SetReasoningEffort("high")
	info.InitChannelMeta(ctx)
	assert.Equal(t, "max", info.ReasoningEffort)

	info.SetReasoningEffort("low")
	info.InitChannelMeta(ctx)
	assert.Equal(t, "max", info.ReasoningEffort)
}

func TestInitChannelMetaResetsPerAttemptStreamStateAndPreservesRequestState(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)

	info, err := GenRelayInfo(ctx, types.RelayFormatOpenAI, &dto.GeneralOpenAIRequest{Model: "gpt-test"}, nil)
	require.NoError(t, err)

	claudeState := relayconvert.NewClaudeToChatStreamState()
	_, err = claudeState.ConvertChunk(&dto.ClaudeResponse{
		Type:  "content_block_start",
		Index: ptr(7),
		ContentBlock: &dto.ClaudeMediaMessage{
			Type: "tool_use",
			Id:   "toolu_1",
			Name: "lookup",
		},
	})
	require.NoError(t, err)
	_, err = claudeState.ConvertChunk(&dto.ClaudeResponse{
		Type:  "content_block_delta",
		Index: ptr(7),
		Delta: &dto.ClaudeMediaMessage{
			Type:        "input_json_delta",
			PartialJson: ptr(`{"q":"x"}`),
		},
	})
	require.NoError(t, err)

	geminiState, err := relayconvert.NewResponseStreamState(types.RelayFormatOpenAI, types.RelayFormatGemini, relayconvert.ResponseStreamOptions{
		ID:    "chatcmpl_1",
		Model: "gpt-test",
	})
	require.NoError(t, err)

	info.SendResponseCount = 3
	info.ClaudeToChatStreamState = claudeState
	info.ChatToGeminiStreamState = geminiState
	info.LastError = types.NewError(assert.AnError, types.ErrorCodeBadResponseBody)
	info.StreamStatus = NewStreamStatus()
	info.StreamStatus.RecordError("attempt 1 soft error")
	info.RecordConversionDiagnostics(context.Background(), []types.ConversionDiagnostic{{
		Code:     "test.loss",
		Message:  "attempt 1 conversion loss",
		Severity: types.ConversionDiagnosticWarning,
		From:     types.RelayFormatClaude,
		To:       types.RelayFormatOpenAI,
	}})

	info.InitChannelMeta(ctx)

	assert.Zero(t, info.SendResponseCount)
	assert.Nil(t, info.ClaudeToChatStreamState)
	assert.Nil(t, info.ChatToGeminiStreamState)

	require.NotNil(t, info.StreamStatus)
	assert.True(t, info.StreamStatus.HasErrors())
	assert.Equal(t, 1, info.StreamStatus.TotalErrorCount())
	diagnostics := info.ConversionDiagnostics()
	require.Len(t, diagnostics, 1)
	assert.Equal(t, "test.loss", diagnostics[0].Code)
	require.NotNil(t, info.LastError)

	freshClaude := relayconvert.NewClaudeToChatStreamState()
	_, err = freshClaude.ConvertChunk(&dto.ClaudeResponse{
		Type:  "content_block_delta",
		Index: ptr(7),
		Delta: &dto.ClaudeMediaMessage{
			Type:        "input_json_delta",
			PartialJson: ptr(`{"q":"x"}`),
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown content block index")

	info.IncrSendResponseCount()
	responses := relayconvert.StreamResponseOpenAI2Claude(&dto.ChatCompletionsStreamResponse{
		Id:    "chatcmpl_retry",
		Model: "gpt-test",
		Choices: []dto.ChatCompletionsStreamResponseChoice{{
			Delta: dto.ChatCompletionsStreamResponseChoiceDelta{Content: ptr("hello")},
		}},
	}, info)
	require.NotEmpty(t, responses)
	assert.Equal(t, "message_start", responses[0].Type)
}

func ptr[T any](value T) *T {
	return &value
}

// 上游实际服务档位的提取位置：Chat Completions（含流式 chunk）与 Responses 非流式在顶层，
// Responses 流式事件在 response.service_tier；无档位的 chunk 不能清空已记录的档位。
func TestCaptureUpstreamServiceTier(t *testing.T) {
	tests := []struct {
		name   string
		chunks []string
		want   string
	}{
		{name: "chat completion top level", chunks: []string{`{"id":"chatcmpl-1","service_tier":"ultrafast"}`}, want: "ultrafast"},
		{name: "responses stream event", chunks: []string{`{"type":"response.completed","response":{"id":"resp_1","service_tier":"priority"}}`}, want: "priority"},
		{name: "later chunk without tier keeps earlier value", chunks: []string{`{"service_tier":"ultrafast"}`, `{"choices":[]}`}, want: "ultrafast"},
		{name: "final event overrides initial tier", chunks: []string{`{"type":"response.created","response":{"service_tier":"auto"}}`, `{"type":"response.completed","response":{"service_tier":"default"}}`}, want: "default"},
		{name: "null tier ignored", chunks: []string{`{"service_tier":null}`}, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := &RelayInfo{}
			for _, chunk := range tt.chunks {
				info.CaptureUpstreamServiceTier([]byte(chunk))
			}
			assert.Equal(t, tt.want, info.UpstreamServiceTier)
		})
	}
}

func TestNoteUpstreamResponseModelRecordsMismatch(t *testing.T) {
	info := &RelayInfo{ChannelMeta: &ChannelMeta{UpstreamModelName: "model-sent"}}
	info.NoteUpstreamResponseModel("model-actual")
	require.NotNil(t, info.UpstreamModelFallback)
	assert.Equal(t, "model-sent", info.UpstreamModelFallback.SentModel)
	assert.Equal(t, "model-actual", info.UpstreamModelFallback.ActualModel)
}

func TestNoteUpstreamResponseModelSameModelIsNoop(t *testing.T) {
	info := &RelayInfo{ChannelMeta: &ChannelMeta{UpstreamModelName: "model-sent"}}
	info.NoteUpstreamResponseModel("model-sent")
	assert.Nil(t, info.UpstreamModelFallback)
}

func TestNoteUpstreamResponseModelEmptyActualIsNoop(t *testing.T) {
	info := &RelayInfo{ChannelMeta: &ChannelMeta{UpstreamModelName: "model-sent"}}
	info.NoteUpstreamResponseModel("")
	assert.Nil(t, info.UpstreamModelFallback)
}

func TestNoteUpstreamResponseModelFirstMismatchWins(t *testing.T) {
	info := &RelayInfo{ChannelMeta: &ChannelMeta{UpstreamModelName: "model-sent"}}
	info.NoteUpstreamResponseModel("model-actual-1")
	info.UpstreamModelName = "model-actual-1"
	info.NoteUpstreamResponseModel("model-actual-2")
	require.NotNil(t, info.UpstreamModelFallback)
	assert.Equal(t, "model-actual-1", info.UpstreamModelFallback.ActualModel)
}

func TestNoteUpstreamResponseModelNilReceiverIsNoop(t *testing.T) {
	var info *RelayInfo
	assert.NotPanics(t, func() {
		info.NoteUpstreamResponseModel("model-actual")
	})
}

func TestInitChannelMetaClearsUpstreamModelFallback(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	info, err := GenRelayInfo(ctx, types.RelayFormatOpenAI, &dto.GeneralOpenAIRequest{Model: "gpt-test"}, nil)
	require.NoError(t, err)
	info.UpstreamModelFallback = &ModelFallbackInfo{SentModel: "gpt-test", ActualModel: "gpt-cheap"}

	info.InitChannelMeta(ctx)
	assert.Nil(t, info.UpstreamModelFallback)
}

func TestNoteUpstreamResponseModelVersionSuffixIsNotFallback(t *testing.T) {
	cases := []struct{ sent, actual string }{
		{"gpt-4o", "gpt-4o-2024-08-06"},
		{"gpt-4o-2024-08-06", "gpt-4o"},
		{"gpt-4o-2024-08-06", "gpt-4o-2024-11-20"},
		{"claude-sonnet-4-5", "claude-sonnet-4-5-20250929"}, // Anthropic 紧凑日期快照
		{"claude-sonnet-4-5-20250929", "claude-sonnet-4-5"},
	}
	for _, tc := range cases {
		info := &RelayInfo{ChannelMeta: &ChannelMeta{UpstreamModelName: tc.sent}}
		info.NoteUpstreamResponseModel(tc.actual)
		assert.Nil(t, info.UpstreamModelFallback, "date-suffixed echo of %s must not count as fallback", tc.actual)
	}
}

func TestNoteUpstreamResponseModelDifferentDateStillSameModel(t *testing.T) {
	// 同一模型不同日期版本：日期后缀去掉后完全一致，不算回退。
	info := &RelayInfo{ChannelMeta: &ChannelMeta{UpstreamModelName: "gpt-4o-2024-08-06"}}
	info.NoteUpstreamResponseModel("gpt-4o-2024-08-06")
	assert.Nil(t, info.UpstreamModelFallback)
}

func TestNoteUpstreamResponseModelRealSwapDetected(t *testing.T) {
	// 真换模型（基础名不同）：即使带日期版本号也必须检测到。
	info := &RelayInfo{ChannelMeta: &ChannelMeta{UpstreamModelName: "gpt-4o"}}
	info.NoteUpstreamResponseModel("gpt-4o-mini-2024-07-18")
	require.NotNil(t, info.UpstreamModelFallback)
	assert.Equal(t, "gpt-4o-mini-2024-07-18", info.UpstreamModelFallback.ActualModel)
}

func TestTrimModelVersionSuffix(t *testing.T) {
	assert.Equal(t, "gpt-4o", trimModelVersionSuffix("gpt-4o-2024-08-06"))
	assert.Equal(t, "gpt-4o", trimModelVersionSuffix("gpt-4o"))
	assert.Equal(t, "claude-sonnet-4-5", trimModelVersionSuffix("claude-sonnet-4-5-20250929"))
	assert.Equal(t, "m-20241399", trimModelVersionSuffix("m-20241399"))
	// 结构化后缀不是日期，必须保留。
	assert.Equal(t, "gemini-2.5-flash-thinking-8192", trimModelVersionSuffix("gemini-2.5-flash-thinking-8192"))
	assert.Equal(t, "model-nothinking", trimModelVersionSuffix("model-nothinking"))
	// 疑似日期但月份/日期非法的保留。
	assert.Equal(t, "m-2024-13-99", trimModelVersionSuffix("m-2024-13-99"))
	assert.Equal(t, "m-2024-1-45", trimModelVersionSuffix("m-2024-1-45"))
}
