package dto

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMessagePreservesDynamicToolsWithoutContent(t *testing.T) {
	partial := true
	var request GeneralOpenAIRequest
	require.NoError(t, json.Unmarshal([]byte(`{
		"model":"kimi-k3",
		"messages":[{
			"role":"system",
			"partial":true,
			"tools":[{"type":"function","function":{"name":"Calculator"}}]
		}]
	}`), &request))

	require.Len(t, request.Messages, 1)
	require.NotEmpty(t, request.Messages[0].Tools)
	require.NotNil(t, request.Messages[0].Partial)
	require.Equal(t, partial, *request.Messages[0].Partial)

	body, err := json.Marshal(request)
	require.NoError(t, err)
	serialized := string(body)
	require.Contains(t, serialized, `"tools":[{"type":"function"`)
	require.Contains(t, serialized, `"partial":true`)
	require.NotContains(t, serialized, `"content":null`)
	require.False(t, strings.Contains(serialized, `"content":""`))
}

func TestMessageOmitsToolsWhenNotProvided(t *testing.T) {
	body, err := json.Marshal(GeneralOpenAIRequest{
		Model:    "gpt-test",
		Messages: []Message{{Role: "user", Content: "hello"}},
	})
	require.NoError(t, err)
	require.NotContains(t, string(body), `"tools"`)
}
