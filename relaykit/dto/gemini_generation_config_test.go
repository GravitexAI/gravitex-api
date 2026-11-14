package dto

import (
	"encoding/json"
	"testing"

	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGeminiChatGenerationConfigPreservesExplicitZeroValuesCamelCase(t *testing.T) {
	raw := []byte(`{
		"contents":[{"role":"user","parts":[{"text":"hello"}]}],
		"generationConfig":{
			"topP":0,
			"topK":0,
			"maxOutputTokens":0,
			"candidateCount":0,
			"seed":0,
			"responseLogprobs":false
		}
	}`)

	var req GeminiChatRequest
	require.NoError(t, kitutil.Unmarshal(raw, &req))

	encoded, err := kitutil.Marshal(req)
	require.NoError(t, err)

	var out map[string]any
	require.NoError(t, kitutil.Unmarshal(encoded, &out))

	generationConfig, ok := out["generationConfig"].(map[string]any)
	require.True(t, ok)

	assert.Contains(t, generationConfig, "topP")
	assert.Contains(t, generationConfig, "topK")
	assert.Contains(t, generationConfig, "maxOutputTokens")
	assert.Contains(t, generationConfig, "candidateCount")
	assert.Contains(t, generationConfig, "seed")
	assert.Contains(t, generationConfig, "responseLogprobs")

	assert.Equal(t, float64(0), generationConfig["topP"])
	assert.Equal(t, float64(0), generationConfig["topK"])
	assert.Equal(t, float64(0), generationConfig["maxOutputTokens"])
	assert.Equal(t, float64(0), generationConfig["candidateCount"])
	assert.Equal(t, float64(0), generationConfig["seed"])
	assert.Equal(t, false, generationConfig["responseLogprobs"])
}

func TestGeminiChatGenerationConfigPreservesExplicitZeroValuesSnakeCase(t *testing.T) {
	raw := []byte(`{
		"contents":[{"role":"user","parts":[{"text":"hello"}]}],
		"generationConfig":{
			"top_p":0,
			"top_k":0,
			"max_output_tokens":0,
			"candidate_count":0,
			"seed":0,
			"response_logprobs":false
		}
	}`)

	var req GeminiChatRequest
	require.NoError(t, kitutil.Unmarshal(raw, &req))

	encoded, err := kitutil.Marshal(req)
	require.NoError(t, err)

	var out map[string]any
	require.NoError(t, kitutil.Unmarshal(encoded, &out))

	generationConfig, ok := out["generationConfig"].(map[string]any)
	require.True(t, ok)

	assert.Contains(t, generationConfig, "topP")
	assert.Contains(t, generationConfig, "topK")
	assert.Contains(t, generationConfig, "maxOutputTokens")
	assert.Contains(t, generationConfig, "candidateCount")
	assert.Contains(t, generationConfig, "seed")
	assert.Contains(t, generationConfig, "responseLogprobs")

	assert.Equal(t, float64(0), generationConfig["topP"])
	assert.Equal(t, float64(0), generationConfig["topK"])
	assert.Equal(t, float64(0), generationConfig["maxOutputTokens"])
	assert.Equal(t, float64(0), generationConfig["candidateCount"])
	assert.Equal(t, float64(0), generationConfig["seed"])
	assert.Equal(t, false, generationConfig["responseLogprobs"])
}

func TestGeminiChatGenerationConfigPreservesResponseFormat(t *testing.T) {
	const responseFormat = `{"text":{"mimeType":"APPLICATION_JSON","schema":{"type":"OBJECT","properties":{"answer":{"type":"STRING"}},"required":["answer"]}}}`
	for _, key := range []string{"responseFormat", "response_format"} {
		t.Run(key, func(t *testing.T) {
			raw := []byte(`{"contents":[{"role":"user","parts":[{"text":"hi"}]}],"generationConfig":{"` + key + `":` + responseFormat + `}}`)

			var req GeminiChatRequest
			require.NoError(t, kitutil.Unmarshal(raw, &req))

			encoded, err := kitutil.Marshal(req)
			require.NoError(t, err)

			var out struct {
				GenerationConfig map[string]json.RawMessage `json:"generationConfig"`
			}
			require.NoError(t, kitutil.Unmarshal(encoded, &out))
			assert.JSONEq(t, responseFormat, string(out.GenerationConfig["responseFormat"]))
			assert.NotContains(t, out.GenerationConfig, "response_format")
		})
	}
}

// Official request fields the gateway has no logic for must reach upstream unchanged,
// in both the camelCase and the snake_case spelling.
func TestGeminiChatRequestPreservesOfficialPassthroughFields(t *testing.T) {
	const want = `{
		"contents":[{"role":"user","parts":[
			{"text":"hi","partMetadata":{"source":"a.txt"},"speechMetadata":{"speaker":"Joe"}},
			{"inlineData":{"mimeType":"audio/wav","data":"AAAA","displayName":"clip.wav"},"audioTranscription":{"text":"hello"}},
			{"fileData":{"mimeType":"application/pdf","fileUri":"gs://b/a.pdf","displayName":"a.pdf"}},
			{"toolCall":{"toolType":"GOOGLE_SEARCH_WEB","id":"tc1","args":{"q":"x"}}},
			{"toolResponse":{"toolType":"GOOGLE_SEARCH_WEB","id":"tc1","response":{"r":1}}},
			{"executableCode":{"language":"PYTHON","code":"print(1)","id":"ec1"}},
			{"codeExecutionResult":{"outcome":"OUTCOME_OK","output":"1","id":"ec1"}}
		]}],
		"safetySettings":[{"category":"HARM_CATEGORY_HARASSMENT","threshold":"BLOCK_NONE","method":"SEVERITY"}],
		"generationConfig":{
			"enableAffectiveDialog":false,
			"audioTimestamp":true,
			"translationConfig":{"targetLanguageCode":"en"},
			"audioTranscriptionConfig":{"languageCodes":["zh"],"diarization":true},
			"_responseJsonSchema":{"type":"object"}
		},
		"labels":{"team":"a"},
		"store":false
	}`
	const snake = `{
		"contents":[{"role":"user","parts":[
			{"text":"hi","part_metadata":{"source":"a.txt"},"speech_metadata":{"speaker":"Joe"}},
			{"inline_data":{"mime_type":"audio/wav","data":"AAAA","display_name":"clip.wav"},"audio_transcription":{"text":"hello"}},
			{"file_data":{"mime_type":"application/pdf","file_uri":"gs://b/a.pdf","display_name":"a.pdf"}},
			{"tool_call":{"toolType":"GOOGLE_SEARCH_WEB","id":"tc1","args":{"q":"x"}}},
			{"tool_response":{"toolType":"GOOGLE_SEARCH_WEB","id":"tc1","response":{"r":1}}},
			{"executable_code":{"language":"PYTHON","code":"print(1)","id":"ec1"}},
			{"code_execution_result":{"outcome":"OUTCOME_OK","output":"1","id":"ec1"}}
		]}],
		"safety_settings":[{"category":"HARM_CATEGORY_HARASSMENT","threshold":"BLOCK_NONE","method":"SEVERITY"}],
		"generation_config":{
			"enable_affective_dialog":false,
			"audio_timestamp":true,
			"translation_config":{"targetLanguageCode":"en"},
			"audio_transcription_config":{"languageCodes":["zh"],"diarization":true},
			"_responseJsonSchema":{"type":"object"}
		},
		"labels":{"team":"a"},
		"store":false
	}`
	for name, body := range map[string]string{"camelCase": want, "snake_case": snake} {
		t.Run(name, func(t *testing.T) {
			var req GeminiChatRequest
			require.NoError(t, kitutil.Unmarshal([]byte(body), &req))
			encoded, err := kitutil.Marshal(req)
			require.NoError(t, err)
			assert.JSONEq(t, want, string(encoded))
		})
	}
}
