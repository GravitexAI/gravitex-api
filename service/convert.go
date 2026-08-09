package service

import (
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert"
)

func NormalizeCacheCreationSplit(totalTokens int, tokens5m int, tokens1h int) (int, int) {
	return relayconvert.NormalizeCacheCreationSplit(totalTokens, tokens5m, tokens1h)
}

// 转换实现已迁入 relaykit/relayconvert，那是独立 module，不能反向依赖根模块的
// RelayInfo，因此「记录转换后真正下发给客户端的用量」这一埋点落在本代理层：
// 从转换结果里取回 usage 再写入 info。转换结果里的 usage 还挂着仅供内部计费链路
// 使用的 billing_usage，下面两个函数负责剥掉它，让记录下来的只是协议本身的字段。

func setClaudeUsageConversion(info *relaycommon.RelayInfo, usage *dto.ClaudeUsage) {
	if usage == nil {
		return
	}
	cleaned := *usage
	cleaned.BillingUsage = nil
	info.SetUsageConversion(&cleaned)
}

func setGeminiUsageConversion(info *relaycommon.RelayInfo, usage dto.GeminiUsageMetadata) {
	usage.BillingUsage = nil
	info.SetUsageConversion(usage)
}

func StreamResponseOpenAI2Claude(openAIResponse *dto.ChatCompletionsStreamResponse, info *relaycommon.RelayInfo) []*dto.ClaudeResponse {
	claudeResponses := relayconvert.StreamResponseOpenAI2Claude(openAIResponse, info)
	// 流式下用量只随 message_delta 下发，逐个覆盖后留下的就是最终用量。
	for _, claudeResponse := range claudeResponses {
		if claudeResponse != nil && claudeResponse.Type == "message_delta" {
			setClaudeUsageConversion(info, claudeResponse.Usage)
		}
	}
	return claudeResponses
}

func ResponseOpenAI2Claude(openAIResponse *dto.OpenAITextResponse, info *relaycommon.RelayInfo) *dto.ClaudeResponse {
	claudeResponse := relayconvert.ResponseOpenAI2Claude(openAIResponse, info)
	if claudeResponse != nil {
		setClaudeUsageConversion(info, claudeResponse.Usage)
	}
	return claudeResponse
}

func ResponseOpenAI2Gemini(openAIResponse *dto.OpenAITextResponse, info *relaycommon.RelayInfo) *dto.GeminiChatResponse {
	geminiResponse := relayconvert.ResponseOpenAI2Gemini(openAIResponse, info)
	if geminiResponse != nil {
		setGeminiUsageConversion(info, geminiResponse.UsageMetadata)
	}
	return geminiResponse
}

func StreamResponseOpenAI2Gemini(openAIResponse *dto.ChatCompletionsStreamResponse, info *relaycommon.RelayInfo) *dto.GeminiChatResponse {
	geminiResponse := relayconvert.StreamResponseOpenAI2Gemini(openAIResponse, info)
	if geminiResponse != nil {
		setGeminiUsageConversion(info, geminiResponse.UsageMetadata)
	}
	return geminiResponse
}
