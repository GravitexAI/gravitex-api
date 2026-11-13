package dto

import (
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGeminiMediaProcessingRoundTrip(t *testing.T) {
	for _, key := range []string{"mediaProcessing", "media_processing"} {
		for _, mode := range []string{"AGENTIC", "STATIC", "MEDIA_PROCESSING_UNSPECIFIED", ""} {
			t.Run(key+"/"+mode, func(t *testing.T) {
				body := fmt.Sprintf(`{"contents":[{"role":"user","parts":[{"fileData":{"mimeType":"video/mp4","fileUri":"gs://bucket/video.mp4"},%q:%q},{"inlineData":{"mimeType":"video/mp4","data":"aGk="},%q:"STATIC"},{"text":"Analyze"}]}],"generationConfig":{%q:"STATIC"}}`, key, mode, key, key)
				var req GeminiChatRequest
				require.NoError(t, kitutil.Unmarshal([]byte(body), &req))
				encoded, err := kitutil.Marshal(req)
				require.NoError(t, err)
				expected := fmt.Sprintf(`{"contents":[{"role":"user","parts":[{"fileData":{"mimeType":"video/mp4","fileUri":"gs://bucket/video.mp4"},"mediaProcessing":%q},{"inlineData":{"mimeType":"video/mp4","data":"aGk="},"mediaProcessing":"STATIC"},{"text":"Analyze"}]}],"generationConfig":{"mediaProcessing":"STATIC"}}`, mode)
				assert.JSONEq(t, expected, string(encoded))
			})
		}
	}
}

func TestGeminiMediaProcessingAbsentAndNull(t *testing.T) {
	for _, body := range []string{`{}`, `{"mediaProcessing":null}`, `{"media_processing":null}`} {
		for _, value := range []any{&GeminiPart{}, &GeminiChatGenerationConfig{}} {
			require.NoError(t, kitutil.Unmarshal([]byte(body), value))
			encoded, err := kitutil.Marshal(value)
			require.NoError(t, err)
			assert.JSONEq(t, `{}`, string(encoded))
		}
	}
}

func TestGeminiMediaProcessingRejectsInvalidTypes(t *testing.T) {
	for _, key := range []string{"mediaProcessing", "media_processing"} {
		for _, value := range []any{&GeminiPart{}, &GeminiChatGenerationConfig{}} {
			require.Error(t, kitutil.Unmarshal([]byte(fmt.Sprintf(`{%q:123}`, key)), value))
		}
	}
}
