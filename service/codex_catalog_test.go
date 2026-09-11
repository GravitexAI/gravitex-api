package service

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func chatExtension(name string, contextWindow int, endpointTypes []string, inputs []string) model.ModelExtensionInfo {
	return reasoningLikeExtension(name, "chat", contextWindow, endpointTypes, inputs)
}

func reasoningLikeExtension(name, primaryMode string, contextWindow int, endpointTypes []string, inputs []string) model.ModelExtensionInfo {
	return model.ModelExtensionInfo{
		ModelName:     name,
		DisplayName:   name,
		ContextWindow: contextWindow,
		IOSchema: model.ModelIOSchema{
			PrimaryMode:   primaryMode,
			EndpointTypes: endpointTypes,
			Modalities: map[string]model.ModelModalityPair{
				primaryMode: {Input: inputs, Output: []string{"text"}},
			},
		},
	}
}

// Codex 解析目录时认的是顶层 models 数组 + slug；发成 OpenAI 的 object/data + id
// 会让它整段反序列化失败，客户端拿不到上下文窗口，会话直接起不来。
func TestBuildCodexCatalogUsesCodexEnvelopeAndSlug(t *testing.T) {
	extensions := map[string]model.ModelExtensionInfo{
		"kimi-k2.7-code": chatExtension("kimi-k2.7-code", 256000, []string{"openai", "openai-response"}, []string{"text"}),
	}

	catalog := BuildCodexCatalog([]string{"kimi-k2.7-code"}, extensions)

	require.Len(t, catalog.Models, 1)
	assert.Equal(t, "kimi-k2.7-code", catalog.Models[0].Slug)
	assert.Equal(t, 256000, catalog.Models[0].ContextWindow)

	raw, err := common.Marshal(catalog)
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, common.Unmarshal(raw, &decoded))
	require.Contains(t, decoded, "models", "Codex 只认顶层 models 数组")
	assert.NotContains(t, decoded, "data", "不能混入 OpenAI 的 data 信封")
	assert.NotContains(t, decoded, "object")

	entries, ok := decoded["models"].([]any)
	require.True(t, ok)
	require.Len(t, entries, 1)
	entry, ok := entries[0].(map[string]any)
	require.True(t, ok)
	assert.Contains(t, entry, "slug")
	assert.NotContains(t, entry, "id", "Codex 用 slug 作主键，不是 id")
	// 这些可空字段必须发 null 而不是省略键，否则 Codex 侧缺 serde default 会解析失败
	for _, key := range []string{"auto_compact_token_limit", "tool_mode", "availability_nux", "upgrade"} {
		assert.Contains(t, entry, key)
		assert.Nil(t, entry[key])
	}
}

// Codex 目录里的 context_window 是「会话窗口」，和模型在 OpenAI API 侧标称的窗口
// 不是一回事：gpt-6-astra API 标称 1,050,000，Codex 目录是 272,000 / 872,000。
// 照搬库里的 API 窗口会让客户端塞超上游能接受的量并拿到 400。
func TestBuildCodexCatalogPrefersOfficialWindowOverDatabaseValue(t *testing.T) {
	extensions := map[string]model.ModelExtensionInfo{
		"gpt-6-astra": reasoningLikeExtension("gpt-6-astra", "reasoning", 1050000,
			[]string{"openai", "openai-response"}, []string{"text", "image", "file"}),
	}

	catalog := BuildCodexCatalog([]string{"gpt-6-astra"}, extensions)

	require.Len(t, catalog.Models, 1)
	entry := catalog.Models[0]
	assert.Equal(t, 272000, entry.ContextWindow)
	assert.Equal(t, 872000, entry.MaxContextWindow)
	// file 不在 Codex 的模态枚举里，必须过滤掉
	assert.Equal(t, []string{"text", "image"}, entry.InputModalities)
	require.NotNil(t, entry.DefaultReasoningLevel)
	assert.Equal(t, "medium", *entry.DefaultReasoningLevel)
}

func TestBuildCodexCatalogFiltersUnusableModels(t *testing.T) {
	cases := []struct {
		name      string
		extension model.ModelExtensionInfo
		reason    string
	}{
		{
			name:      "imagen-4.0-generate-001",
			extension: reasoningLikeExtension("imagen-4.0-generate-001", "image", 480, []string{"openai", "openai-response"}, []string{"text"}),
			reason:    "图像模型的 context_window 是提示词上限，喂给 Codex 会每轮都触发 auto-compact",
		},
		{
			name:      "gemini-embedding-001",
			extension: reasoningLikeExtension("gemini-embedding-001", "embedding", 3072, []string{"openai", "openai-response"}, []string{"text"}),
			reason:    "向量模型不能当对话模型用",
		},
		{
			name:      "gpt-5.1-codex",
			extension: reasoningLikeExtension("gpt-5.1-codex", "reasoning", 400000, []string{"openai"}, []string{"text"}),
			reason:    "没有 openai-response，Codex 只走 Responses API，选中即 404",
		},
		{
			name:      "gpt-5.2-chat",
			extension: reasoningLikeExtension("gpt-5.2-chat", "reasoning", 0, []string{"openai", "openai-response"}, []string{"text"}),
			reason:    "缺 context_window，猜一个值比缺失更难排查",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			catalog := BuildCodexCatalog([]string{tc.name}, map[string]model.ModelExtensionInfo{tc.name: tc.extension})
			assert.Empty(t, catalog.Models, tc.reason)
		})
	}
}

// 没有扩展配置的模型不能凭空进目录，否则 Codex 拿到的是一条没有窗口信息的条目。
func TestBuildCodexCatalogSkipsModelsWithoutExtension(t *testing.T) {
	catalog := BuildCodexCatalog([]string{"unknown-model"}, map[string]model.ModelExtensionInfo{})
	assert.Empty(t, catalog.Models)
}

func TestBuildCodexCatalogOrdersByContextWindowAndAssignsPriority(t *testing.T) {
	extensions := map[string]model.ModelExtensionInfo{
		"small": chatExtension("small", 128000, []string{"openai-response"}, []string{"text"}),
		"large": chatExtension("large", 1000000, []string{"openai-response"}, []string{"text"}),
		"mid":   chatExtension("mid", 256000, []string{"openai-response"}, []string{"text"}),
	}

	catalog := BuildCodexCatalog([]string{"small", "large", "mid"}, extensions)

	require.Len(t, catalog.Models, 3)
	assert.Equal(t, []string{"large", "mid", "small"},
		[]string{catalog.Models[0].Slug, catalog.Models[1].Slug, catalog.Models[2].Slug})
	assert.Equal(t, []int{1, 2, 3},
		[]int{catalog.Models[0].Priority, catalog.Models[1].Priority, catalog.Models[2].Priority})
}
