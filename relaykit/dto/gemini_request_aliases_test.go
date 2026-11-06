package dto

import (
	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestGeminiRequestFieldAliases(t *testing.T) {
	cases := []struct {
		name         string
		newValue     func() any
		camel, snake string
	}{
		{"request", func() any { return &GeminiChatRequest{} },
			`{"contents":[],"safetySettings":[{"category":"HARM_CATEGORY_HATE_SPEECH","threshold":"BLOCK_NONE"}],"generationConfig":{"maxOutputTokens":100},"toolConfig":{"includeServerSideToolInvocations":false},"cachedContent":"cache","systemInstruction":{"parts":[{"text":"system"}]}}`,
			`{"contents":[],"safety_settings":[{"category":"HARM_CATEGORY_HATE_SPEECH","threshold":"BLOCK_NONE"}],"generation_config":{"max_output_tokens":100},"tool_config":{"include_server_side_tool_invocations":false},"cached_content":"cache","system_instruction":{"parts":[{"text":"system"}]}}`},
		{"parts", func() any { return &GeminiChatContent{} },
			`{"parts":[{"text":"hi","thought":true},{"inlineData":{"mimeType":"image/png","data":"aGk="}},{"fileData":{"mimeType":"image/png","fileUri":"gs://bucket/image.png"}},{"functionCall":{"name":"lookup","args":{"user_key":1}}},{"functionResponse":{"name":"lookup","response":{"result_key":2}}},{"text":"thinking","thoughtSignature":"c2ln"},{"fileData":{"fileUri":"video"},"mediaResolution":{"level":"HIGH"},"videoMetadata":{"start_offset":"1s"}},{"executableCode":{"language":"PYTHON","code":"print(1)"}},{"codeExecutionResult":{"outcome":"OUTCOME_OK","output":"1"}}]}`,
			`{"parts":[{"text":"hi","thought":true},{"inline_data":{"mime_type":"image/png","data":"aGk="}},{"file_data":{"mime_type":"image/png","file_uri":"gs://bucket/image.png"}},{"function_call":{"name":"lookup","args":{"user_key":1}}},{"function_response":{"name":"lookup","response":{"result_key":2}}},{"text":"thinking","thought_signature":"c2ln"},{"file_data":{"file_uri":"video"},"media_resolution":{"level":"HIGH"},"video_metadata":{"start_offset":"1s"}},{"executable_code":{"language":"PYTHON","code":"print(1)"}},{"code_execution_result":{"outcome":"OUTCOME_OK","output":"1"}}]}`},
		{"function config", func() any { return &FunctionCallingConfig{} }, `{"mode":"ANY","allowedFunctionNames":["lookup"],"streamFunctionCallArguments":false}`, `{"mode":"ANY","allowed_function_names":["lookup"],"stream_function_call_arguments":false}`},
		{"retrieval", func() any { return &RetrievalConfig{} }, `{"latLng":{"latitude":0,"longitude":0},"languageCode":"zh"}`, `{"lat_lng":{"latitude":0,"longitude":0},"language_code":"zh"}`},
		{"function call", func() any { return &FunctionCall{} }, `{"name":"lookup","args":{"some_key":0},"partialArgs":[{"jsonPath":"$.some_key","numberValue":0,"willContinue":false}],"willContinue":false}`, `{"name":"lookup","args":{"some_key":0},"partial_args":[{"json_path":"$.some_key","number_value":0,"will_continue":false}],"will_continue":false}`},
		{"partial string", func() any { return &GeminiPartialArg{} }, `{"jsonPath":"$.s","stringValue":""}`, `{"json_path":"$.s","string_value":""}`},
		{"partial bool", func() any { return &GeminiPartialArg{} }, `{"jsonPath":"$.b","boolValue":false}`, `{"json_path":"$.b","bool_value":false}`},
		{"partial null", func() any { return &GeminiPartialArg{} }, `{"jsonPath":"$.n","nullValue":null}`, `{"json_path":"$.n","null_value":null}`},
		{"response", func() any { return &GeminiFunctionResponse{} }, `{"name":"lookup","response":{"some_key":1},"willContinue":false,"parts":[{"custom_key":1}]}`, `{"name":"lookup","response":{"some_key":1},"will_continue":false,"parts":[{"custom_key":1}]}`},
		{"tools", func() any { return &GeminiChatTool{} },
			`{"googleSearch":{},"googleSearchRetrieval":{"dynamic_retrieval_config":{}},"googleMaps":{},"enterpriseWebSearch":{},"codeExecution":{},"functionDeclarations":[{"name":"lookup","parameters":{"properties":{"some_key":{"type":"STRING"}}}}],"urlContext":{},"fileSearch":{"file_search_store_names":["store"]},"computerUse":{"environment":"ENVIRONMENT_BROWSER"},"retrieval":{"custom_key":1}}`,
			`{"google_search":{},"google_search_retrieval":{"dynamic_retrieval_config":{}},"google_maps":{},"enterprise_web_search":{},"code_execution":{},"function_declarations":[{"name":"lookup","parameters":{"properties":{"some_key":{"type":"STRING"}}}}],"url_context":{},"file_search":{"file_search_store_names":["store"]},"computer_use":{"environment":"ENVIRONMENT_BROWSER"},"retrieval":{"custom_key":1}}`},
		{"batch embedding", func() any { return &GeminiBatchEmbeddingRequest{} }, `{"requests":[{"model":"models/test","content":{"parts":[{"text":"hi"}]},"taskType":"RETRIEVAL_QUERY","outputDimensionality":256}]}`, `{"requests":[{"model":"models/test","content":{"parts":[{"text":"hi"}]},"task_type":"RETRIEVAL_QUERY","output_dimensionality":256}]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			camel, snake := tc.newValue(), tc.newValue()
			require.NoError(t, kitutil.Unmarshal([]byte(tc.camel), camel))
			require.NoError(t, kitutil.Unmarshal([]byte(tc.snake), snake))
			assert.Equal(t, camel, snake)
			out, err := kitutil.Marshal(snake)
			require.NoError(t, err)
			assert.JSONEq(t, tc.camel, string(out))
		})
	}
}

func TestGeminiAliasPrecedenceAndEmptyValues(t *testing.T) {
	cases := []struct {
		name       string
		value      any
		body, want string
	}{
		{"snake strings win", &GeminiFileData{}, `{"fileUri":"old","file_uri":"new","mimeType":"image/jpeg","mime_type":"image/png"}`, `{"fileUri":"new","mimeType":"image/png"}`},
		{"empty strings fall back", &GeminiFileData{}, `{"fileUri":"old","file_uri":"","mimeType":"image/png","mime_type":null}`, `{"fileUri":"old","mimeType":"image/png"}`},
		{"pointer replaces part", &GeminiPart{}, `{"fileData":{"fileUri":"old"},"file_data":{"file_uri":"new"}}`, `{"fileData":{"fileUri":"new"}}`},
		{"null pointer falls back", &GeminiPart{}, `{"fileData":{"fileUri":"old"},"file_data":null}`, `{"fileData":{"fileUri":"old"}}`},
		{"empty collection falls back", &FunctionCallingConfig{}, `{"allowedFunctionNames":["old"],"allowed_function_names":[]}`, `{"allowedFunctionNames":["old"]}`},
		{"nonempty collection wins", &FunctionCallingConfig{}, `{"allowedFunctionNames":["old"],"allowed_function_names":["new"]}`, `{"allowedFunctionNames":["new"]}`},
		{"empty declarations fall back", &GeminiChatTool{}, `{"functionDeclarations":[{"name":"old"}],"function_declarations":[]}`, `{"functionDeclarations":[{"name":"old"}]}`},
		{"false wins", &FunctionCallingConfig{}, `{"streamFunctionCallArguments":true,"stream_function_call_arguments":false}`, `{"streamFunctionCallArguments":false}`},
		{"zero wins", &GeminiPartialArg{}, `{"json_path":"$.n","numberValue":2,"number_value":0}`, `{"jsonPath":"$.n","numberValue":0}`},
		{"embedding zero wins", &GeminiEmbeddingRequest{}, `{"outputDimensionality":256,"output_dimensionality":0}`, `{"content":{"parts":null}}`},
		{"absent optional scalars", &GeminiPartialArg{}, `{}`, `{"jsonPath":""}`},
		{"mixed file naming", &GeminiPart{}, `{"file_data":{"mimeType":"image/png","file_uri":"image"}}`, `{"fileData":{"mimeType":"image/png","fileUri":"image"}}`},
		{"mixed config naming", &GeminiChatRequest{}, `{"contents":[],"generation_config":{"maxOutputTokens":0,"thinking_config":{"includeThoughts":false,"thinking_budget":0}}}`, `{"contents":[],"generationConfig":{"maxOutputTokens":0,"thinkingConfig":{"includeThoughts":false,"thinkingBudget":0}}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, kitutil.Unmarshal([]byte(tc.body), tc.value))
			out, err := kitutil.Marshal(tc.value)
			require.NoError(t, err)
			assert.JSONEq(t, tc.want, string(out))
		})
	}
}

func TestGeminiAliasesRejectInvalidTypes(t *testing.T) {
	cases := []struct {
		value any
		body  string
	}{
		{&GeminiChatRequest{}, `{"generation_config":[]}`}, {&GeminiChatRequest{}, `{"safety_settings":{}}`},
		{&GeminiPart{}, `{"file_data":"bad"}`}, {&GeminiFileData{}, `{"mime_type":12}`}, {&GeminiFileData{}, `{"file_uri":{}}`},
		{&FunctionCallingConfig{}, `{"allowed_function_names":"bad"}`}, {&FunctionCall{}, `{"will_continue":"false"}`},
		{&GeminiPartialArg{}, `{"number_value":"0"}`}, {&GeminiEmbeddingRequest{}, `{"output_dimensionality":"256"}`},
	}
	for _, tc := range cases {
		t.Run(tc.body, func(t *testing.T) { require.Error(t, kitutil.Unmarshal([]byte(tc.body), tc.value)) })
	}
}

func TestGeminiNestedSnakeRequestPreservesPayloads(t *testing.T) {
	const body = `{"contents":[{"role":"user","parts":[{"text":"hi"},{"file_data":{"mime_type":"image/png","file_uri":"gs://bucket/image.png"}},{"function_call":{"name":"lookup","args":{"file_data":"user value"}}},{"function_response":{"name":"lookup","response":{"mime_type":"user value"}}}]}],"generation_config":{"max_output_tokens":100,"thinking_config":{"include_thoughts":false,"thinking_budget":0},"response_json_schema":{"properties":{"file_data":{"type":"string"}}},"image_config":{"aspect_ratio":"1:1"},"speech_config":{"voice_config":{"custom_key":1}}},"tools":[{"function_declarations":[{"name":"lookup","parameters":{"properties":{"file_uri":{"type":"STRING"}}}}]}],"tool_config":{"function_calling_config":{"allowed_function_names":["lookup"],"stream_function_call_arguments":false}}}`
	var req GeminiChatRequest
	require.NoError(t, kitutil.Unmarshal([]byte(body), &req))
	require.Len(t, req.Contents, 1)
	require.Len(t, req.Contents[0].Parts, 4)
	require.NotNil(t, req.Contents[0].Parts[1].FileData)
	assert.Equal(t, "gs://bucket/image.png", req.Contents[0].Parts[1].FileData.FileUri)
	require.NotNil(t, req.Contents[0].Parts[2].FunctionCall)
	require.NotNil(t, req.Contents[0].Parts[3].FunctionResponse)
	assert.Equal(t, map[string]any{"file_data": "user value"}, req.Contents[0].Parts[2].FunctionCall.Arguments)
	assert.Equal(t, map[string]any{"mime_type": "user value"}, req.Contents[0].Parts[3].FunctionResponse.Response)
	assert.JSONEq(t, `{"properties":{"file_data":{"type":"string"}}}`, string(req.GenerationConfig.ResponseJsonSchema))
	assert.JSONEq(t, `{"aspect_ratio":"1:1"}`, string(req.GenerationConfig.ImageConfig))
	assert.JSONEq(t, `{"voice_config":{"custom_key":1}}`, string(req.GenerationConfig.SpeechConfig))
	originalTools := string(req.Tools)
	tools := req.GetTools()
	require.Len(t, tools, 1)
	require.NotNil(t, tools[0].FunctionDeclarations)
	assert.Equal(t, originalTools, string(req.Tools))
	req.SetTools(tools)
	assert.JSONEq(t, `[{"functionDeclarations":[{"name":"lookup","parameters":{"properties":{"file_uri":{"type":"STRING"}}}}]}]`, string(req.Tools))
	out, err := kitutil.Marshal(&req)
	require.NoError(t, err)
	var roundTrip GeminiChatRequest
	require.NoError(t, kitutil.Unmarshal(out, &roundTrip))
	assert.Equal(t, req, roundTrip)
}
