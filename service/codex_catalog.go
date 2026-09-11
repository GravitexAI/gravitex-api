package service

import (
	"sort"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
)

// Codex 模型目录构建。
//
// 目录数据来自两处，官方值优先：
//  1. codexOfficialWindows —— openai/codex 仓库 models.json 里的会话窗口。Codex 的
//     context_window 与模型在 OpenAI API 侧标称的窗口不是一个概念（gpt-6-astra
//     API 标称 1,050,000，Codex 目录是 272,000 / 872,000），官方有值就必须照官方来。
//  2. t_extension_models.context_window —— 其余模型（第三方、国产）用库里配的值。
//     这些模型的 API 窗口和可用窗口基本一致，直接用是合理的。
//
// 没有任何一处有值的模型会被丢弃，而不是猜一个默认值：Codex 会拿 context_window
// 去做 auto-compact 和请求裁剪，猜错比缺失更难排查。

// codexContextWindow 是单个模型的会话窗口配置。
type codexContextWindow struct {
	ContextWindow    int
	MaxContextWindow int
}

// codexOfficialWindows 来自 openai/codex 的 codex-rs/models-manager/models.json。
// 只收录我们实际会对外提供的 slug；官方目录里的 gpt-daybreak-* 和 codex-auto-review
// 是内部模型，不放。客户端大版本升级后需要复核这张表。
var codexOfficialWindows = map[string]codexContextWindow{
	"gpt-6-astra":   {ContextWindow: 272000, MaxContextWindow: 872000},
	"gpt-5.6-sol":   {ContextWindow: 272000, MaxContextWindow: 872000},
	"gpt-5.6-terra": {ContextWindow: 272000, MaxContextWindow: 872000},
	"gpt-5.6-luna":  {ContextWindow: 272000, MaxContextWindow: 872000},
	"gpt-5.5":       {ContextWindow: 272000, MaxContextWindow: 272000},
	"gpt-5.4":       {ContextWindow: 272000, MaxContextWindow: 1000000},
}

// codexReasoningLevels 是推理模型统一提供的档位。官方对不同模型给的档位集合不同
// （例如 gpt-6-astra 多了 max/ultra），但多给档位会让客户端发出上游不认的 effort，
// 所以统一取四档这个各家都支持的交集。
var codexReasoningLevels = []dto.CodexReasoningLevel{
	{Effort: "low", Description: "Fast responses with lighter reasoning"},
	{Effort: "medium", Description: "Balances speed and reasoning depth for everyday tasks"},
	{Effort: "high", Description: "Greater reasoning depth for complex problems"},
	{Effort: "xhigh", Description: "Extra high reasoning depth for complex problems"},
}

// codexMinContextWindow 是进目录的最小窗口。低于这个值的多半是图像 / 视频 / 向量
// 模型误填（库里存在 480、1024、3072 这类值），真喂给 Codex 会导致每轮都 auto-compact。
const codexMinContextWindow = 8192

// BuildCodexCatalog 把一批模型名转成 Codex 模型目录。
//
// modelNames 应当是调用方已经按用户分组、令牌模型限制筛过的可用模型集合，
// 这里只再做「能不能给 Codex 用」这一层过滤。
// extensions 通常来自 model.GetModelExtensions()，由调用方传入而不是在这里取，
// 目的是让过滤和取值规则可以脱离数据库单测。
func BuildCodexCatalog(modelNames []string, extensions map[string]model.ModelExtensionInfo) dto.CodexCatalog {
	models := make([]dto.CodexModel, 0, len(modelNames))
	for _, modelName := range modelNames {
		info, ok := extensions[modelName]
		if !ok {
			continue
		}
		entry, ok := buildCodexModel(modelName, info)
		if !ok {
			continue
		}
		models = append(models, entry)
	}

	// 目录按窗口从大到小排，priority 随之递增，让选择器里更强的模型排前面。
	sort.SliceStable(models, func(i, j int) bool {
		if models[i].ContextWindow != models[j].ContextWindow {
			return models[i].ContextWindow > models[j].ContextWindow
		}
		return models[i].Slug < models[j].Slug
	})
	for i := range models {
		models[i].Priority = i + 1
	}

	return dto.CodexCatalog{Models: models}
}

func buildCodexModel(modelName string, info model.ModelExtensionInfo) (dto.CodexModel, bool) {
	// Codex 是编码 agent，只能消费对话 / 推理类模型。图像、视频、向量、语音模型
	// 即使标了 openai-response 也不该进目录。
	isReasoning := info.IOSchema.PrimaryMode == "reasoning"
	if !isReasoning && info.IOSchema.PrimaryMode != "chat" {
		return dto.CodexModel{}, false
	}

	// Codex 自 2026-02 起只剩 Responses 一条链路，chat/completions 已被移除。
	// 不支持 Responses 的模型放进目录，用户一选就是 404。
	if !info.SupportsEndpointType("openai-response") {
		return dto.CodexModel{}, false
	}

	window, ok := codexContextWindowFor(modelName, info)
	if !ok {
		return dto.CodexModel{}, false
	}

	entry := dto.CodexModel{
		Slug:        modelName,
		DisplayName: info.DisplayName,
		Description: info.Description,

		ContextWindow:         window.ContextWindow,
		MaxContextWindow:      window.MaxContextWindow,
		AutoCompactTokenLimit: nil, // null 表示让 Codex 自己按窗口推导

		SupportedReasoningLevels:          nil,
		DefaultReasoningSummary:           "none",
		SupportsReasoningSummaries:        false,
		SupportsReasoningSummaryParameter: false,

		InputModalities:             codexInputModalities(info),
		SupportsImageDetailOriginal: false,

		ApplyPatchToolType: "freeform",
		WebSearchToolType:  "text",
		SupportsSearchTool: info.Capabilities.SupportsWebSearch,
		// unified_exec 是官方新模型用的形态，对第三方上游未必稳；shell_command 是
		// 两种形态里更保守的一个，Codex 目录 schema 里同样是合法值。
		ShellType:                  "shell_command",
		ToolMode:                   nil,
		TruncationPolicy:           dto.CodexTruncationPolicy{Mode: "tokens", Limit: 10000},
		SupportsParallelToolCalls:  info.Capabilities.SupportsFunctionCalling,
		ExperimentalSupportedTools: []string{},

		SupportVerbosity: false,
		DefaultVerbosity: "low",

		// 第三方上游没有 websocket 通道，也没有 responses-lite / 官方技能体系。
		PreferWebsockets:               false,
		UseResponsesLite:               false,
		IncludeSkillsUsageInstructions: false,
		IncludeAppsUsageInstructions:   false,
		IncludePluginUsageInstructions: false,

		NodeReplAutoReviewRequired: false,
		NodeReplDisabled:           false,
		AutoReviewModelOverride:    nil,

		MultiAgentVersion: nil,
		ModelSpecialty:    nil,

		DefaultServiceTier:   nil,
		ServiceTiers:         []dto.CodexServiceTier{},
		AdditionalSpeedTiers: []string{},

		Visibility:     "list",
		SupportedInApi: true,
		// 0.0.0 表示不对客户端版本设下限，避免老客户端被静默过滤掉。
		MinimalClientVersion: "0.0.0",
		AvailabilityNux:      nil,
		Upgrade:              nil,
		CompHash:             "",
	}

	if isReasoning {
		defaultLevel := "medium"
		entry.DefaultReasoningLevel = &defaultLevel
		entry.SupportedReasoningLevels = codexReasoningLevels
		entry.SupportsReasoningSummaries = true
		entry.SupportsReasoningSummaryParameter = true
	}
	for _, modality := range entry.InputModalities {
		if modality == "image" {
			entry.SupportsImageDetailOriginal = true
			break
		}
	}

	return entry, true
}

func codexContextWindowFor(modelName string, info model.ModelExtensionInfo) (codexContextWindow, bool) {
	if official, ok := codexOfficialWindows[modelName]; ok {
		return official, true
	}
	if info.ContextWindow < codexMinContextWindow {
		return codexContextWindow{}, false
	}
	return codexContextWindow{
		ContextWindow:    info.ContextWindow,
		MaxContextWindow: info.ContextWindow,
	}, true
}

// codexInputModalities 把库里的模态列表收敛到 Codex 认识的 text / image 两种。
// io_schema 里常见的 file / audio 不在 Codex 的枚举里，发过去会反序列化失败。
func codexInputModalities(info model.ModelExtensionInfo) []string {
	modalities := make([]string, 0, 2)
	for _, modality := range info.PrimaryInputModalities() {
		switch modality {
		case "text", "image":
			modalities = append(modalities, modality)
		}
	}
	if len(modalities) == 0 {
		// 能进目录的都是对话/推理模型，至少支持文本
		modalities = append(modalities, "text")
	}
	return modalities
}
