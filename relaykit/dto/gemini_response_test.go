package dto

import (
	"encoding/json"
	"testing"

	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGeminiChatResponseUnmarshalPreservesGroundingMetadata(t *testing.T) {
	var response GeminiChatResponse
	err := json.Unmarshal([]byte(`{
		"candidates": [{
			"groundingMetadata": {
				"webSearchQueries": ["人工智能行业最新动态", "生成式人工智能趋势"]
			}
		}],
		"usageMetadata": {"totalTokenCount": 2}
	}`), &response)
	require.NoError(t, err)
	require.Len(t, response.Candidates, 1)
	require.NotNil(t, response.Candidates[0].GroundingMetadata)
	assert.Equal(t, []string{"人工智能行业最新动态", "生成式人工智能趋势"}, response.Candidates[0].GroundingMetadata.WebSearchQueries)
}

func TestGeminiChatResponseUsageMetadataPresence(t *testing.T) {
	var missing GeminiChatResponse
	require.NoError(t, kitutil.Unmarshal([]byte(`{"candidates":[]}`), &missing))
	assert.False(t, missing.HasUsageMetadata)
	assert.Nil(t, missing.GetUsageMetadata())

	var empty GeminiChatResponse
	require.NoError(t, kitutil.Unmarshal([]byte(`{"candidates":[],"usageMetadata":{}}`), &empty))
	assert.True(t, empty.HasUsageMetadata)
	require.NotNil(t, empty.GetUsageMetadata())
	assert.False(t, HasGeminiUsageMetadataTokens(empty.GetUsageMetadata()))

	var populated GeminiChatResponse
	require.NoError(t, kitutil.Unmarshal([]byte(`{"candidates":[],"usageMetadata":{"promptTokenCount":3}}`), &populated))
	assert.True(t, populated.HasUsageMetadata)
	require.NotNil(t, populated.GetUsageMetadata())
	assert.True(t, HasGeminiUsageMetadataTokens(populated.GetUsageMetadata()))
}

func TestGeminiChatResponseMarshalKeepsUsageMetadataField(t *testing.T) {
	data, err := kitutil.Marshal(GeminiChatResponse{})
	require.NoError(t, err)
	assert.Contains(t, string(data), `"usageMetadata"`)
}
