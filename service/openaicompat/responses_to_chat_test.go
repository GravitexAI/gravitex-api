package openaicompat

import (
	"testing"

	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResponsesResponseToChatCompletionsResponseMapsCacheWriteTokens(t *testing.T) {
	resp := &dto.OpenAIResponsesResponse{
		ID: "resp_cache_write",
		Usage: &dto.Usage{
			InputTokens:  1200,
			OutputTokens: 10,
			TotalTokens:  1210,
			InputTokensDetails: &dto.InputTokenDetails{
				CachedTokens:     100,
				CacheWriteTokens: 900,
			},
		},
	}

	_, usage, err := ResponsesResponseToChatCompletionsResponse(resp, "chat_cache_write")

	require.NoError(t, err)
	require.NotNil(t, usage)
	assert.Equal(t, 100, usage.PromptTokensDetails.CachedTokens)
	assert.Equal(t, 900, usage.PromptTokensDetails.CacheWriteTokens)
}
