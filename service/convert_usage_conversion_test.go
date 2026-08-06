package service

import (
	"testing"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

func TestStreamResponseOpenAI2ClaudeCapturesOnlyFinalUsage(t *testing.T) {
	partialContent := "partial"
	info := &relaycommon.RelayInfo{
		ClaudeConvertInfo: &relaycommon.ClaudeConvertInfo{},
	}
	interim := &dto.ChatCompletionsStreamResponse{
		Usage: &dto.Usage{PromptTokens: 1037, CompletionTokens: 0, TotalTokens: 1037},
		Choices: []dto.ChatCompletionsStreamResponseChoice{{
			Delta: dto.ChatCompletionsStreamResponseChoiceDelta{Content: &partialContent},
		}},
	}

	StreamResponseOpenAI2Claude(interim, info)
	require.Nil(t, info.UsageConversion)

	finishReason := "stop"
	final := &dto.ChatCompletionsStreamResponse{
		Usage: &dto.Usage{PromptTokens: 1037, CompletionTokens: 35, TotalTokens: 1072},
		Choices: []dto.ChatCompletionsStreamResponseChoice{{
			FinishReason: &finishReason,
		}},
	}

	StreamResponseOpenAI2Claude(final, info)
	require.Equal(t, map[string]any{
		"input_tokens":                float64(1037),
		"cache_creation_input_tokens": float64(0),
		"cache_read_input_tokens":     float64(0),
		"output_tokens":               float64(35),
	}, info.UsageConversion)
}
