package oaichat

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenAIToGeminiMediaProcessing(t *testing.T) {
	cases := []struct {
		name, extra, want string
		invalid           bool
	}{
		{name: "absent", extra: `{}`},
		{name: "null", extra: `{"google":{"media_processing":null}}`},
		{name: "native", extra: `{"google":{"mediaProcessing":"AGENTIC"}}`, want: `"AGENTIC"`},
		{name: "snake", extra: `{"google":{"media_processing":"STATIC"}}`, want: `"STATIC"`},
		{name: "snake precedence", extra: `{"google":{"mediaProcessing":"STATIC","media_processing":"AGENTIC"}}`, want: `"AGENTIC"`},
		{name: "explicit empty", extra: `{"google":{"media_processing":""}}`, want: `""`},
		{name: "invalid type", extra: `{"google":{"media_processing":123}}`, invalid: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var req dto.GeneralOpenAIRequest
			require.NoError(t, kitutil.Unmarshal([]byte(`{"model":"gemini-4-argon","messages":[{"role":"user","content":"hello"}],"extra_body":`+tc.extra+`}`), &req))
			result, err := OpenAIChatRequestToGeminiGenerateContent(context.Background(), req, nil)
			if tc.invalid {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			encoded, err := kitutil.Marshal(result.GenerationConfig)
			require.NoError(t, err)
			var config map[string]any
			require.NoError(t, kitutil.Unmarshal(encoded, &config))
			if tc.want == "" {
				assert.NotContains(t, config, "mediaProcessing")
				return
			}
			var want string
			require.NoError(t, kitutil.Unmarshal([]byte(tc.want), &want))
			assert.Equal(t, want, config["mediaProcessing"])
			assert.NotContains(t, config, "media_processing")
		})
	}
}
