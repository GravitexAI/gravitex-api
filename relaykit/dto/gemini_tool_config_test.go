package dto

import (
	"testing"

	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// toolConfig 是 Gemini 内置工具的开关载体，snake_case 写法被丢弃时上游收不到开关，
// 表现为「请求返回 200 但内置工具事件全部缺失」这种极难定位的静默降级。
func TestToolConfigUnmarshalAcceptsBothCasings(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{
			name: "camelCase",
			body: `{"functionCallingConfig":{"mode":"ANY","allowedFunctionNames":["lookup"]},"includeServerSideToolInvocations":true}`,
		},
		{
			name: "snake_case",
			body: `{"function_calling_config":{"mode":"ANY","allowedFunctionNames":["lookup"]},"include_server_side_tool_invocations":true}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got ToolConfig
			require.NoError(t, kitutil.Unmarshal([]byte(tc.body), &got))

			require.NotNil(t, got.IncludeServerSideToolInvocations)
			assert.True(t, *got.IncludeServerSideToolInvocations)
			require.NotNil(t, got.FunctionCallingConfig)
			assert.Equal(t, FunctionCallingConfigMode("ANY"), got.FunctionCallingConfig.Mode)
			assert.Equal(t, []string{"lookup"}, got.FunctionCallingConfig.AllowedFunctionNames)
		})
	}
}

// 显式 false 必须保留（而不是退化成 nil 被 omitempty 丢掉），否则调用方无法关闭该开关。
func TestToolConfigPreservesExplicitFalse(t *testing.T) {
	var got ToolConfig
	require.NoError(t, kitutil.Unmarshal([]byte(`{"include_server_side_tool_invocations":false}`), &got))

	require.NotNil(t, got.IncludeServerSideToolInvocations)
	assert.False(t, *got.IncludeServerSideToolInvocations)

	out, err := kitutil.Marshal(&got)
	require.NoError(t, err)
	assert.JSONEq(t, `{"includeServerSideToolInvocations":false}`, string(out))
}

// 整个 GeminiChatRequest 的往返：内置工具相关字段必须逐字节等价地送到上游。
func TestGeminiChatRequestPreservesBuiltInToolFields(t *testing.T) {
	const body = `{
      "contents":[{"role":"user","parts":[{"text":"Query: \"AIRPAZ SINGAPORE PTE. LTD.\" 201528606C"}]}],
      "tools":[{"googleSearch":{}},{"urlContext":{}},{"functionDeclarations":[{"name":"resolve","parametersJsonSchema":{"type":"object","additionalProperties":false}}]}],
      "toolConfig":{"include_server_side_tool_invocations":true}
    }`

	var req GeminiChatRequest
	require.NoError(t, kitutil.Unmarshal([]byte(body), &req))

	out, err := kitutil.Marshal(&req)
	require.NoError(t, err)

	var roundTripped GeminiChatRequest
	require.NoError(t, kitutil.Unmarshal(out, &roundTripped))

	require.NotNil(t, roundTripped.ToolConfig)
	require.NotNil(t, roundTripped.ToolConfig.IncludeServerSideToolInvocations)
	assert.True(t, *roundTripped.ToolConfig.IncludeServerSideToolInvocations)
	assert.JSONEq(t,
		`[{"googleSearch":{}},{"urlContext":{}},{"functionDeclarations":[{"name":"resolve","parametersJsonSchema":{"type":"object","additionalProperties":false}}]}]`,
		string(roundTripped.Tools))
	require.Len(t, roundTripped.Contents, 1)
	require.Len(t, roundTripped.Contents[0].Parts, 1)
	assert.Equal(t, `Query: "AIRPAZ SINGAPORE PTE. LTD." 201528606C`, roundTripped.Contents[0].Parts[0].Text)
}
