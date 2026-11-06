package geminichat

import (
	"github.com/QuantumNous/new-api/relaykit/dto"
	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestSnakeCaseRequestKeepsToolsDuringOpenAIConversion(t *testing.T) {
	var req dto.GeminiChatRequest
	require.NoError(t, kitutil.Unmarshal([]byte(`{"contents":[{"role":"user","parts":[{"text":"lookup"}]}],"generation_config":{"max_output_tokens":100,"temperature":0},"system_instruction":{"parts":[{"text":"system"}]},"tools":[{"function_declarations":[{"name":"lookup","parameters":{"type":"object","properties":{"file_uri":{"type":"string"}}}}]}]}`), &req))
	got, err := GeminiGenerateContentRequestToOpenAIChat(&req, nil)
	require.NoError(t, err)
	require.Len(t, got.Tools, 1)
	assert.Equal(t, "lookup", got.Tools[0].Function.Name)
	params, err := kitutil.Marshal(got.Tools[0].Function.Parameters)
	require.NoError(t, err)
	assert.JSONEq(t, `{"type":"object","properties":{"file_uri":{"type":"string"}}}`, string(params))
	require.NotNil(t, got.MaxTokens)
	assert.EqualValues(t, 100, *got.MaxTokens)
	require.NotNil(t, got.Temperature)
	assert.Zero(t, *got.Temperature)
	require.Len(t, got.Messages, 2)
	assert.Equal(t, "system", got.Messages[0].Role)
}
