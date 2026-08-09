package openai

import (
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

func TestCaptureOpenAIStreamUsageKeepsUsageFromNonFinalChunk(t *testing.T) {
	info := &relaycommon.RelayInfo{}
	captureOpenAIStreamUsage(info, `{"id":"chatcmpl-test","usage":{"prompt_tokens":1037,"completion_tokens":0,"total_tokens":1037}}`)

	require.Equal(t, map[string]any{
		"usage": map[string]any{
			"prompt_tokens":     float64(1037),
			"completion_tokens": float64(0),
			"total_tokens":      float64(1037),
		},
	}, info.UpstreamResponses)

	captureOpenAIStreamUsage(info, "[DONE]")
	require.NotNil(t, info.UpstreamResponses)
}
