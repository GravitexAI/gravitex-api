package dto

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUsageParsesCacheWriteTokensDetails(t *testing.T) {
	// Chat Completions shape: prompt_tokens_details.cache_write_tokens_details
	var ccUsage Usage
	err := json.Unmarshal([]byte(`{
		"prompt_tokens": 100,
		"prompt_tokens_details": {
			"cached_tokens": 10,
			"cache_write_tokens": 30,
			"cache_write_tokens_details": {"ttl_30m_tokens": 20, "ttl_12h_tokens": 10}
		}
	}`), &ccUsage)
	require.NoError(t, err)
	require.NotNil(t, ccUsage.PromptTokensDetails.CacheWriteTokensDetails)
	assert.Equal(t, 30, ccUsage.PromptTokensDetails.CacheWriteTokens)
	assert.Equal(t, 20, ccUsage.PromptTokensDetails.CacheWriteTokensDetails.Ttl30mTokens)
	assert.Equal(t, 10, ccUsage.PromptTokensDetails.CacheWriteTokensDetails.Ttl12hTokens)
	assert.Equal(t, 30, ccUsage.PromptTokensDetails.CacheCreationTokensTotal())

	// Responses shape: input_tokens_details.cache_write_tokens_details
	var respUsage Usage
	err = json.Unmarshal([]byte(`{
		"input_tokens": 100,
		"input_tokens_details": {
			"cached_tokens": 10,
			"cache_write_tokens": 30,
			"cache_write_tokens_details": {"ttl_30m_tokens": 20, "ttl_12h_tokens": 10}
		}
	}`), &respUsage)
	require.NoError(t, err)
	require.NotNil(t, respUsage.InputTokensDetails)
	require.NotNil(t, respUsage.InputTokensDetails.CacheWriteTokensDetails)
	assert.Equal(t, 20, respUsage.InputTokensDetails.CacheWriteTokensDetails.Ttl30mTokens)
	assert.Equal(t, 10, respUsage.InputTokensDetails.CacheWriteTokensDetails.Ttl12hTokens)
}

func TestUsageOmitsCacheWriteTokensDetailsWhenAbsent(t *testing.T) {
	// Azure reports cache_write_tokens without the TTL breakdown.
	var usage Usage
	err := json.Unmarshal([]byte(`{
		"prompt_tokens": 100,
		"prompt_tokens_details": {"cached_tokens": 10, "cache_write_tokens": 30}
	}`), &usage)
	require.NoError(t, err)
	assert.Nil(t, usage.PromptTokensDetails.CacheWriteTokensDetails)
	// Encoding must not emit the field when unset (omitempty).
	encoded, err := json.Marshal(usage.PromptTokensDetails)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "cache_write_tokens_details")
}

func TestMergeInputTokenDetailsCarriesCacheWriteTokensDetails(t *testing.T) {
	var current, incoming InputTokenDetails
	incoming.CacheWriteTokens = 30
	incoming.CacheWriteTokensDetails = &CacheWriteTokensDetails{Ttl30mTokens: 20, Ttl12hTokens: 10}

	mergeInputTokenDetails(&current, incoming)
	require.NotNil(t, current.CacheWriteTokensDetails)
	assert.Equal(t, 20, current.CacheWriteTokensDetails.Ttl30mTokens)
	assert.Equal(t, 10, current.CacheWriteTokensDetails.Ttl12hTokens)
}
