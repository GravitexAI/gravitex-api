package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestNativeInteractionResponseReturnsUsageFromVideoMetadata(t *testing.T) {
	body, err := nativeInteractionResponse([]byte(`{
		"data": {
			"id": "video-1",
			"model": "gemini-omni-flash-preview",
			"status": "completed",
			"metadata": {
				"usage": {
					"total_input_tokens": 21,
					"total_output_tokens": 17788,
					"input_tokens_by_modality": [{"modality":"text","tokens":21}],
					"output_tokens_by_modality": [{"modality":"video","tokens":17376},{"modality":"thought","tokens":412}]
				}
			}
		}
	}`), "")
	require.NoError(t, err)
	var response map[string]any
	require.NoError(t, common.Unmarshal(body, &response))
	require.Equal(t, float64(21), response["usage"].(map[string]any)["total_input_tokens"])
	require.Equal(t, float64(17376), response["usage"].(map[string]any)["output_tokens_by_modality"].([]any)[0].(map[string]any)["tokens"])
}
