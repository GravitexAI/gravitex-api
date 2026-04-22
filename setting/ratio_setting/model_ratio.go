package ratio_setting

import (
	"regexp"
	"sort"
	"strings"
	"sync"

	"encoding/json"
	"math"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"
)

// from songquanpeng/one-api
const (
	USD2RMB = 7.3 // 暂定 1 USD = 7.3 RMB
	USD     = 500 // $0.002 = 1 -> $1 = 500
	RMB     = USD / USD2RMB
)

// modelRatio
// https://platform.openai.com/docs/models/model-endpoint-compatibility
// https://cloud.baidu.com/doc/WENXINWORKSHOP/s/Blfmc9dlf
// https://openai.com/pricing
// TODO: when a new api is enabled, check the pricing here
// 1 === $0.002 / 1K tokens
// 1 === ￥0.014 / 1k tokens

var defaultModelRatio = map[string]float64{
	//"midjourney":                50,
	"gpt-4-gizmo-*":  15,
	"gpt-4o-gizmo-*": 2.5,
	"gpt-4-all":      15,
	"gpt-4o-all":     15,
	"gpt-4":          15,
	//"gpt-4-0314":                   15, //deprecated
	"gpt-4-0613": 15,
	"gpt-4-32k":  30,
	//"gpt-4-32k-0314":               30, //deprecated
	"gpt-4-32k-0613":                          30,
	"gpt-4-1106-preview":                      5,    // $10 / 1M tokens
	"gpt-4-0125-preview":                      5,    // $10 / 1M tokens
	"gpt-4-turbo-preview":                     5,    // $10 / 1M tokens
	"gpt-4-vision-preview":                    5,    // $10 / 1M tokens
	"gpt-4-1106-vision-preview":               5,    // $10 / 1M tokens
	"chatgpt-4o-latest":                       2.5,  // $5 / 1M tokens
	"gpt-4o":                                  1.25, // $2.5 / 1M tokens
	"gpt-4o-audio-preview":                    1.25, // $2.5 / 1M tokens
	"gpt-4o-audio-preview-2024-10-01":         1.25, // $2.5 / 1M tokens
	"gpt-4o-2024-05-13":                       2.5,  // $5 / 1M tokens
	"gpt-4o-2024-08-06":                       1.25, // $2.5 / 1M tokens
	"gpt-4o-2024-11-20":                       1.25, // $2.5 / 1M tokens
	"gpt-4o-realtime-preview":                 2.5,
	"gpt-4o-realtime-preview-2024-10-01":      2.5,
	"gpt-4o-realtime-preview-2024-12-17":      2.5,
	"gpt-4o-mini-realtime-preview":            0.3,
	"gpt-4o-mini-realtime-preview-2024-12-17": 0.3,
	"gpt-4.1":                          1.0,  // $2 / 1M tokens
	"gpt-4.1-2025-04-14":               1.0,  // $2 / 1M tokens
	"gpt-4.1-mini":                     0.2,  // $0.4 / 1M tokens
	"gpt-4.1-mini-2025-04-14":          0.2,  // $0.4 / 1M tokens
	"gpt-4.1-nano":                     0.05, // $0.1 / 1M tokens
	"gpt-4.1-nano-2025-04-14":          0.05, // $0.1 / 1M tokens
	"gpt-image-1":                      2.5,  // $5 / 1M tokens
	"gpt-image-2":                      2.5,  // $5 / 1M tokens
	"o1":                               7.5,  // $15 / 1M tokens
	"o1-2024-12-17":                    7.5,  // $15 / 1M tokens
	"o1-preview":                       7.5,  // $15 / 1M tokens
	"o1-preview-2024-09-12":            7.5,  // $15 / 1M tokens
	"o1-mini":                          0.55, // $1.1 / 1M tokens
	"o1-mini-2024-09-12":               0.55, // $1.1 / 1M tokens
	"o1-pro":                           75.0, // $150 / 1M tokens
	"o1-pro-2025-03-19":                75.0, // $150 / 1M tokens
	"o3-mini":                          0.55,
	"o3-mini-2025-01-31":               0.55,
	"o3-mini-high":                     0.55,
	"o3-mini-2025-01-31-high":          0.55,
	"o3-mini-low":                      0.55,
	"o3-mini-2025-01-31-low":           0.55,
	"o3-mini-medium":                   0.55,
	"o3-mini-2025-01-31-medium":        0.55,
	"o3":                               1.0,  // $2 / 1M tokens
	"o3-2025-04-16":                    1.0,  // $2 / 1M tokens
	"o3-pro":                           10.0, // $20 / 1M tokens
	"o3-pro-2025-06-10":                10.0, // $20 / 1M tokens
	"o3-deep-research":                 5.0,  // $10 / 1M tokens
	"o3-deep-research-2025-06-26":      5.0,  // $10 / 1M tokens
	"o4-mini":                          0.55, // $1.1 / 1M tokens
	"o4-mini-2025-04-16":               0.55, // $1.1 / 1M tokens
	"o4-mini-deep-research":            1.0,  // $2 / 1M tokens
	"o4-mini-deep-research-2025-06-26": 1.0,  // $2 / 1M tokens
	"gpt-4o-mini":                      0.075,
	"gpt-4o-mini-2024-07-18":           0.075,
	"gpt-4-turbo":                      5, // $0.01 / 1K tokens
	"gpt-4-turbo-2024-04-09":           5, // $0.01 / 1K tokens
	"gpt-4.5-preview":                  37.5,
	"gpt-4.5-preview-2025-02-27":       37.5,
	"gpt-5":                            0.625,
	"gpt-5-2025-08-07":                 0.625,
	"gpt-5-chat-latest":                0.625,
	"gpt-5-mini":                       0.125,
	"gpt-5-mini-2025-08-07":            0.125,
	"gpt-5-nano":                       0.025,
	"gpt-5-nano-2025-08-07":            0.025,
	//"gpt-3.5-turbo-0301":           0.75, //deprecated
	"gpt-3.5-turbo":          0.25,
	"gpt-3.5-turbo-0613":     0.75,
	"gpt-3.5-turbo-16k":      1.5, // $0.003 / 1K tokens
	"gpt-3.5-turbo-16k-0613": 1.5,
	"gpt-3.5-turbo-instruct": 0.75, // $0.0015 / 1K tokens
	"gpt-3.5-turbo-1106":     0.5,  // $0.001 / 1K tokens
	"gpt-3.5-turbo-0125":     0.25,
	"babbage-002":            0.2, // $0.0004 / 1K tokens
	"davinci-002":            1,   // $0.002 / 1K tokens
	"text-ada-001":           0.2,
	"text-babbage-001":       0.25,
	"text-curie-001":         1,
	//"text-davinci-002":               10,
	//"text-davinci-003":               10,
	"text-davinci-edit-001":                     10,
	"code-davinci-edit-001":                     10,
	"whisper-1":                                 15,  // $0.006 / minute -> $0.006 / 150 words -> $0.006 / 200 tokens -> $0.03 / 1k tokens
	"tts-1":                                     7.5, // 1k characters -> $0.015
	"tts-1-1106":                                7.5, // 1k characters -> $0.015
	"tts-1-hd":                                  15,  // 1k characters -> $0.03
	"tts-1-hd-1106":                             15,  // 1k characters -> $0.03
	"davinci":                                   10,
	"curie":                                     10,
	"babbage":                                   10,
	"ada":                                       10,
	"text-embedding-3-small":                    0.01,
	"text-embedding-3-large":                    0.065,
	"text-embedding-ada-002":                    0.05,
	"text-search-ada-doc-001":                   10,
	"text-moderation-stable":                    0.1,
	"text-moderation-latest":                    0.1,
	"claude-3-haiku-20240307":                   0.125, // $0.25 / 1M tokens
	"claude-3-5-haiku-20241022":                 0.5,   // $1 / 1M tokens
	"claude-haiku-4-5-20251001":                 0.5,   // $1 / 1M tokens
	"claude-3-sonnet-20240229":                  1.5,   // $3 / 1M tokens
	"claude-3-5-sonnet-20240620":                1.5,
	"claude-3-5-sonnet-20241022":                1.5,
	"claude-3-7-sonnet-20250219":                1.5,
	"claude-3-7-sonnet-20250219-thinking":       1.5,
	"claude-sonnet-4-20250514":                  1.5,
	"claude-sonnet-4-5-20250929":                1.5,
	"claude-opus-4-5-20251101":                  2.5,
	"claude-opus-4-6":                           2.5,
	"claude-opus-4-6-max":                       2.5,
	"claude-opus-4-6-high":                      2.5,
	"claude-opus-4-6-medium":                    2.5,
	"claude-opus-4-6-low":                       2.5,
	"claude-3-opus-20240229":                    7.5, // $15 / 1M tokens
	"claude-opus-4-20250514":                    7.5,
	"claude-opus-4-1-20250805":                  7.5,
	"ERNIE-4.0-8K":                              0.120 * RMB,
	"ERNIE-3.5-8K":                              0.012 * RMB,
	"ERNIE-3.5-8K-0205":                         0.024 * RMB,
	"ERNIE-3.5-8K-1222":                         0.012 * RMB,
	"ERNIE-Bot-8K":                              0.024 * RMB,
	"ERNIE-3.5-4K-0205":                         0.012 * RMB,
	"ERNIE-Speed-8K":                            0.004 * RMB,
	"ERNIE-Speed-128K":                          0.004 * RMB,
	"ERNIE-Lite-8K-0922":                        0.008 * RMB,
	"ERNIE-Lite-8K-0308":                        0.003 * RMB,
	"ERNIE-Tiny-8K":                             0.001 * RMB,
	"BLOOMZ-7B":                                 0.004 * RMB,
	"Embedding-V1":                              0.002 * RMB,
	"bge-large-zh":                              0.002 * RMB,
	"bge-large-en":                              0.002 * RMB,
	"tao-8k":                                    0.002 * RMB,
	"PaLM-2":                                    1,
	"gemini-1.5-pro-latest":                     1.25, // $3.5 / 1M tokens
	"gemini-1.5-flash-latest":                   0.075,
	"gemini-2.0-flash":                          0.05,
	"gemini-2.5-pro-exp-03-25":                  0.625,
	"gemini-2.5-pro-preview-03-25":              0.625,
	"gemini-2.5-pro":                            0.625,
	"gemini-2.5-flash-preview-04-17":            0.075,
	"gemini-2.5-flash-preview-04-17-thinking":   0.075,
	"gemini-2.5-flash-preview-04-17-nothinking": 0.075,
	"gemini-2.5-flash-preview-05-20":            0.075,
	"gemini-2.5-flash-preview-05-20-thinking":   0.075,
	"gemini-2.5-flash-preview-05-20-nothinking": 0.075,
	"gemini-2.5-flash-thinking-*":               0.075, // 用于为后续所有2.5 flash thinking budget 模型设置默认倍率
	"gemini-2.5-pro-thinking-*":                 0.625, // 用于为后续所有2.5 pro thinking budget 模型设置默认倍率
	"gemini-2.5-flash-lite-preview-thinking-*":  0.05,
	"gemini-2.5-flash-lite-preview-06-17":       0.05,
	"gemini-2.5-flash":                          0.15,
	"gemini-robotics-er-1.5-preview":            0.15,
	"gemini-embedding-001":                      0.075,
	"text-embedding-004":                        0.001,
	"chatglm_turbo":                             0.3572,     // ￥0.005 / 1k tokens
	"chatglm_pro":                               0.7143,     // ￥0.01 / 1k tokens
	"chatglm_std":                               0.3572,     // ￥0.005 / 1k tokens
	"chatglm_lite":                              0.1429,     // ￥0.002 / 1k tokens
	"glm-4":                                     7.143,      // ￥0.1 / 1k tokens
	"glm-4v":                                    0.05 * RMB, // ￥0.05 / 1k tokens
	"glm-4-alltools":                            0.1 * RMB,  // ￥0.1 / 1k tokens
	"glm-3-turbo":                               0.3572,
	"glm-4-plus":                                0.05 * RMB,
	"glm-4-0520":                                0.1 * RMB,
	"glm-4-air":                                 0.001 * RMB,
	"glm-4-airx":                                0.01 * RMB,
	"glm-4-long":                                0.001 * RMB,
	"glm-4-flash":                               0,
	"glm-4v-plus":                               0.01 * RMB,
	"qwen-turbo":                                0.8572, // ￥0.012 / 1k tokens
	"qwen-plus":                                 10,     // ￥0.14 / 1k tokens
	"text-embedding-v1":                         0.05,   // ￥0.0007 / 1k tokens
	"SparkDesk-v1.1":                            1.2858, // ￥0.018 / 1k tokens
	"SparkDesk-v2.1":                            1.2858, // ￥0.018 / 1k tokens
	"SparkDesk-v3.1":                            1.2858, // ￥0.018 / 1k tokens
	"SparkDesk-v3.5":                            1.2858, // ￥0.018 / 1k tokens
	"SparkDesk-v4.0":                            1.2858,
	"360GPT_S2_V9":                              0.8572, // ¥0.012 / 1k tokens
	"360gpt-turbo":                              0.0858, // ¥0.0012 / 1k tokens
	"360gpt-turbo-responsibility-8k":            0.8572, // ¥0.012 / 1k tokens
	"360gpt-pro":                                0.8572, // ¥0.012 / 1k tokens
	"360gpt2-pro":                               0.8572, // ¥0.012 / 1k tokens
	"embedding-bert-512-v1":                     0.0715, // ¥0.001 / 1k tokens
	"embedding_s1_v1":                           0.0715, // ¥0.001 / 1k tokens
	"semantic_similarity_s1_v1":                 0.0715, // ¥0.001 / 1k tokens
	"hunyuan":                                   7.143,  // ¥0.1 / 1k tokens  // https://cloud.tencent.com/document/product/1729/97731#e0e6be58-60c8-469f-bdeb-6c264ce3b4d0
	// https://platform.lingyiwanwu.com/docs#-计费单元
	// 已经按照 7.2 来换算美元价格
	"yi-34b-chat-0205":       0.18,
	"yi-34b-chat-200k":       0.864,
	"yi-vl-plus":             0.432,
	"yi-large":               20.0 / 1000 * RMB,
	"yi-medium":              2.5 / 1000 * RMB,
	"yi-vision":              6.0 / 1000 * RMB,
	"yi-medium-200k":         12.0 / 1000 * RMB,
	"yi-spark":               1.0 / 1000 * RMB,
	"yi-large-rag":           25.0 / 1000 * RMB,
	"yi-large-turbo":         12.0 / 1000 * RMB,
	"yi-large-preview":       20.0 / 1000 * RMB,
	"yi-large-rag-preview":   25.0 / 1000 * RMB,
	"command":                0.5,
	"command-nightly":        0.5,
	"command-light":          0.5,
	"command-light-nightly":  0.5,
	"command-r":              0.25,
	"command-r-plus":         1.5,
	"command-r-08-2024":      0.075,
	"command-r-plus-08-2024": 1.25,
	"deepseek-chat":          0.27 / 2,
	"deepseek-coder":         0.27 / 2,
	"deepseek-reasoner":      0.55 / 2, // 0.55 / 1k tokens
	// Perplexity online 模型对搜索额外收费，有需要应自行调整，此处不计入搜索费用
	"llama-3-sonar-small-32k-chat":   0.2 / 1000 * USD,
	"llama-3-sonar-small-32k-online": 0.2 / 1000 * USD,
	"llama-3-sonar-large-32k-chat":   1.0 / 1000 * USD,
	"llama-3-sonar-large-32k-online": 1.0 / 1000 * USD,
	// grok
	"grok-3-beta":           1.5,
	"grok-3-mini-beta":      0.15,
	"grok-2":                1,
	"grok-2-vision":         1,
	"grok-beta":             2.5,
	"grok-vision-beta":      2.5,
	"grok-3-fast-beta":      2.5,
	"grok-3-mini-fast-beta": 0.3,
	// submodel
	"NousResearch/Hermes-4-405B-FP8":          0.8,
	"Qwen/Qwen3-235B-A22B-Thinking-2507":      0.6,
	"Qwen/Qwen3-Coder-480B-A35B-Instruct-FP8": 0.8,
	"Qwen/Qwen3-235B-A22B-Instruct-2507":      0.3,
	"zai-org/GLM-4.5-FP8":                     0.8,
	"openai/gpt-oss-120b":                     0.5,
	"deepseek-ai/DeepSeek-R1-0528":            0.8,
	"deepseek-ai/DeepSeek-R1":                 0.8,
	"deepseek-ai/DeepSeek-V3-0324":            0.8,
	"deepseek-ai/DeepSeek-V3.1":               0.8,
}

var defaultModelPrice = map[string]float64{
	"suno_music":                     0.1,
	"suno_lyrics":                    0.01,
	"dall-e-3":                       0.04,
	"imagen-3.0-generate-002":        0.03,
	"black-forest-labs/flux-1.1-pro": 0.04,
	"gpt-4-gizmo-*":                  0.1,
	"mj_video":                       0.8,
	"mj_imagine":                     0.1,
	"mj_edits":                       0.1,
	"mj_variation":                   0.1,
	"mj_reroll":                      0.1,
	"mj_blend":                       0.1,
	"mj_modal":                       0.1,
	"mj_zoom":                        0.1,
	"mj_shorten":                     0.1,
	"mj_high_variation":              0.1,
	"mj_low_variation":               0.1,
	"mj_pan":                         0.1,
	"mj_inpaint":                     0,
	"mj_custom_zoom":                 0,
	"mj_describe":                    0.05,
	"mj_upscale":                     0.05,
	"swap_face":                      0.05,
	"mj_upload":                      0.05,
	"sora-2":                         0.3,
	"sora-2-pro":                     0.5,
	"gpt-4o-mini-tts":                0.3,
	"veo-3.0-generate-001":           0.4,
	"veo-3.0-fast-generate-001":      0.15,
	"veo-3.1-generate-preview":       0.4,
	"veo-3.1-fast-generate-preview":  0.15,
}

var defaultAudioRatio = map[string]float64{
	"gpt-4o-audio-preview":         16,
	"gpt-4o-mini-audio-preview":    66.67,
	"gpt-4o-realtime-preview":      8,
	"gpt-4o-mini-realtime-preview": 16.67,
	"gpt-4o-mini-tts":              25,
}

var defaultAudioCompletionRatio = map[string]float64{
	"gpt-4o-realtime":      2,
	"gpt-4o-mini-realtime": 2,
	"gpt-4o-mini-tts":      1,
	"tts-1":                0,
	"tts-1-hd":             0,
	"tts-1-1106":           0,
	"tts-1-hd-1106":        0,
}

var modelPriceMap = types.NewRWMap[string, float64]()
var modelRatioMap = types.NewRWMap[string, float64]()
var completionRatioMap = types.NewRWMap[string, float64]()

var defaultCompletionRatio = map[string]float64{
	"gpt-4-gizmo-*":  2,
	"gpt-4o-gizmo-*": 3,
	"gpt-4-all":      2,
	"gpt-image-1":    8,
	"gpt-image-2":    6,
}

// InitRatioSettings initializes all model related settings maps with code defaults.
// 优先使用 options 表：model.InitOptionMap() 会从 DB 加载 options 并覆盖对应 key（如 ImageCompletionRatio），
// 表里没有的 key 则保持此处默认值。
// 各倍率/价格语义（计费时）：
//   - ModelRatio: 模型倍率，对应文本输入
//   - CompletionRatio: 模型补全倍率，对应文本输出
//   - AudioRatio: 音频倍率，对应音频输入
//   - AudioCompletionRatio: 音频补全倍率，对应音频输出（与音频倍率成倍数关系；未配置时用模型倍率）
//   - ImageRatio: 图片倍率，对应图片输入
//   - ImageCompletionRatio: 图片补全倍率，对应图片输出（与图片倍率成倍数关系；未配置时用模型倍率）
//   - CacheRatio: 缓存倍率，与模型倍率成倍数关系
//
// 其它：ImageModelPricePerImage 按张计费从 OptionMap 加载；VideoModelPricePerSecond 在 loadVideoModelPricePerSecondFromDatabase
func InitRatioSettings() {
	modelPriceMap.AddAll(defaultModelPrice)
	modelRatioMap.AddAll(defaultModelRatio)
	completionRatioMap.AddAll(defaultCompletionRatio)
	cacheRatioMap.AddAll(defaultCacheRatio)
	createCacheRatioMap.AddAll(defaultCreateCacheRatio)
	imageRatioMap.AddAll(defaultImageRatio)
	imageCompletionRatioMap.AddAll(defaultImageCompletionRatio)
	audioRatioMap.AddAll(defaultAudioRatio)
	audioCompletionRatioMap.AddAll(defaultAudioCompletionRatio)
	loadVideoRatioFromDatabase()
	loadVideoCompletionRatioFromDatabase()
	loadVideoModelPricePerSecondFromDatabase()
}

func GetModelPriceMap() map[string]float64 {
	return modelPriceMap.ReadAll()
}

func ModelPrice2JSONString() string {
	return modelPriceMap.MarshalJSONString()
}

func UpdateModelPriceByJSONString(jsonStr string) error {
	return types.LoadFromJsonStringWithCallback(modelPriceMap, jsonStr, InvalidateExposedDataCache)
}

// GetModelPrice 返回模型的价格，如果模型不存在则返回-1，false
func GetModelPrice(name string, printErr bool) (float64, bool) {
	name = FormatMatchingModelName(name)

	if price, ok := modelPriceMap.Get(name); ok {
		return price, true
	}

	if strings.HasSuffix(name, CompactModelSuffix) {
		price, ok := modelPriceMap.Get(CompactWildcardModelKey)
		if !ok {
			if printErr {
				common.SysError("model price not found: " + name)
			}
			return -1, false
		}
		return price, true
	}

	// 按张计费：若 options 中配置了 ImageModelPricePerImage，则视为已配置价格（兼容 seedream-* 与 doubao-seedream-* 两种 key）
	if perImage, okImage := GetImageModelPricePerImage(name); okImage && perImage >= 0 {
		return perImage, true
	}
	// 兜底：直接读 OptionMap 并查找（避免懒加载时 OptionMap 尚未就绪）
	if p, ok2 := getImageModelPricePerImageFromOptionMap(name); ok2 && p >= 0 {
		return p, true
	}
	if printErr {
		common.SysError("model price not found: " + name)
	}
	return -1, false
}

func UpdateModelRatioByJSONString(jsonStr string) error {
	return types.LoadFromJsonStringWithCallback(modelRatioMap, jsonStr, InvalidateExposedDataCache)
}

// 处理带有思考预算的模型名称，方便统一定价
func handleThinkingBudgetModel(name, prefix, wildcard string) string {
	if strings.HasPrefix(name, prefix) && strings.Contains(name, "-thinking-") {
		return wildcard
	}
	return name
}

func GetModelRatio(name string) (float64, bool, string) {
	name = FormatMatchingModelName(name)

	ratio, ok := modelRatioMap.Get(name)
	if !ok {
		if strings.HasSuffix(name, CompactModelSuffix) {
			if wildcardRatio, ok := modelRatioMap.Get(CompactWildcardModelKey); ok {
				return wildcardRatio, true, name
			}
			//return 0, true, name
		}
		return 37.5, operation_setting.SelfUseModeEnabled, name
	}
	return ratio, true, name
}

func DefaultModelRatio2JSONString() string {
	jsonBytes, err := common.Marshal(defaultModelRatio)
	if err != nil {
		common.SysError("error marshalling model ratio: " + err.Error())
	}
	return string(jsonBytes)
}

func GetDefaultModelRatioMap() map[string]float64 {
	return defaultModelRatio
}

func GetDefaultModelPriceMap() map[string]float64 {
	return defaultModelPrice
}

func CompletionRatio2JSONString() string {
	return completionRatioMap.MarshalJSONString()
}

func UpdateCompletionRatioByJSONString(jsonStr string) error {
	return types.LoadFromJsonStringWithCallback(completionRatioMap, jsonStr, InvalidateExposedDataCache)
}

func GetCompletionRatio(name string) float64 {
	name = FormatMatchingModelName(name)

	if strings.Contains(name, "/") {
		if ratio, ok := completionRatioMap.Get(name); ok {
			return ratio
		}
	}
	hardCodedRatio, contain := getHardcodedCompletionModelRatio(name)
	if contain {
		return hardCodedRatio
	}
	if ratio, ok := completionRatioMap.Get(name); ok {
		return ratio
	}
	return hardCodedRatio
}

type CompletionRatioInfo struct {
	Ratio  float64 `json:"ratio"`
	Locked bool    `json:"locked"`
}

func GetCompletionRatioInfo(name string) CompletionRatioInfo {
	name = FormatMatchingModelName(name)

	if strings.Contains(name, "/") {
		if ratio, ok := completionRatioMap.Get(name); ok {
			return CompletionRatioInfo{
				Ratio:  ratio,
				Locked: false,
			}
		}
	}

	hardCodedRatio, locked := getHardcodedCompletionModelRatio(name)
	if locked {
		return CompletionRatioInfo{
			Ratio:  hardCodedRatio,
			Locked: true,
		}
	}

	if ratio, ok := completionRatioMap.Get(name); ok {
		return CompletionRatioInfo{
			Ratio:  ratio,
			Locked: false,
		}
	}

	return CompletionRatioInfo{
		Ratio:  hardCodedRatio,
		Locked: false,
	}
}

func getHardcodedCompletionModelRatio(name string) (float64, bool) {

	isReservedModel := strings.HasSuffix(name, "-all") || strings.HasSuffix(name, "-gizmo-*")
	if isReservedModel {
		return 2, false
	}

	if strings.HasPrefix(name, "gpt-") {
		if strings.HasPrefix(name, "gpt-4o") {
			if name == "gpt-4o-2024-05-13" {
				return 3, true
			}
			if strings.HasPrefix(name, "gpt-4o-mini-tts") {
				return 20, false
			}
			return 4, false
		}
		// gpt-5 匹配
		if strings.HasPrefix(name, "gpt-5") {
			if strings.HasPrefix(name, "gpt-5.4") {
				if strings.HasPrefix(name, "gpt-5.4-nano") {
					return 6.25, true
				}
				return 6, true
			}
			return 8, true
		}
		// gpt-4.5-preview匹配
		if strings.HasPrefix(name, "gpt-4.5-preview") {
			return 2, true
		}
		if strings.HasPrefix(name, "gpt-4-turbo") || strings.HasSuffix(name, "gpt-4-1106") || strings.HasSuffix(name, "gpt-4-1105") {
			return 3, true
		}
		// 没有特殊标记的 gpt-4 模型默认倍率为 2
		return 2, false
	}
	if strings.HasPrefix(name, "o1") || strings.HasPrefix(name, "o3") {
		return 4, true
	}
	if name == "chatgpt-4o-latest" {
		return 3, true
	}

	if strings.Contains(name, "claude-3") {
		return 5, true
	} else if strings.Contains(name, "claude-sonnet-4") || strings.Contains(name, "claude-opus-4") || strings.Contains(name, "claude-haiku-4") {
		return 5, true
	}

	if strings.HasPrefix(name, "gpt-3.5") {
		if name == "gpt-3.5-turbo" || strings.HasSuffix(name, "0125") {
			// https://openai.com/blog/new-embedding-models-and-api-updates
			// Updated GPT-3.5 Turbo model and lower pricing
			return 3, true
		}
		if strings.HasSuffix(name, "1106") {
			return 2, true
		}
		return 4.0 / 3.0, true
	}
	if strings.HasPrefix(name, "mistral-") {
		return 3, true
	}
	if strings.HasPrefix(name, "gemini-") {
		if strings.HasPrefix(name, "gemini-1.5") {
			return 4, true
		} else if strings.HasPrefix(name, "gemini-2.0") {
			return 4, true
		} else if strings.HasPrefix(name, "gemini-2.5-pro") { // 移除preview来增加兼容性，这里假设正式版的倍率和preview一致
			return 8, false
		} else if strings.HasPrefix(name, "gemini-2.5-flash") { // 处理不同的flash模型倍率
			if strings.HasPrefix(name, "gemini-2.5-flash-preview") {
				if strings.HasSuffix(name, "-nothinking") {
					return 4, false
				}
				return 3.5 / 0.15, false
			}
			if strings.HasPrefix(name, "gemini-2.5-flash-lite") {
				return 4, false
			}
			return 2.5 / 0.3, false
		} else if strings.HasPrefix(name, "gemini-robotics-er-1.5") {
			return 2.5 / 0.3, false
		} else if strings.HasPrefix(name, "gemini-3-pro") {
			if strings.HasPrefix(name, "gemini-3-pro-image") {
				return 60, false
			}
			return 6, false
		}
		return 4, false
	}
	if strings.HasPrefix(name, "command") {
		switch name {
		case "command-r":
			return 3, true
		case "command-r-plus":
			return 5, true
		case "command-r-08-2024":
			return 4, true
		case "command-r-plus-08-2024":
			return 4, true
		default:
			return 4, false
		}
	}
	// hint 只给官方上4倍率，由于开源模型供应商自行定价，不对其进行补全倍率进行强制对齐
	if strings.HasPrefix(name, "ERNIE-Speed-") {
		return 2, true
	} else if strings.HasPrefix(name, "ERNIE-Lite-") {
		return 2, true
	} else if strings.HasPrefix(name, "ERNIE-Character") {
		return 2, true
	} else if strings.HasPrefix(name, "ERNIE-Functions") {
		return 2, true
	}
	switch name {
	case "llama2-70b-4096":
		return 0.8 / 0.64, true
	case "llama3-8b-8192":
		return 2, true
	case "llama3-70b-8192":
		return 0.79 / 0.59, true
	}
	return 1, false
}

func GetAudioRatio(name string) float64 {
	name = FormatMatchingModelName(name)
	if ratio, ok := audioRatioMap.Get(name); ok {
		return ratio
	}
	return 1
}

func GetAudioCompletionRatio(name string) float64 {
	name = FormatMatchingModelName(name)
	if ratio, ok := audioCompletionRatioMap.Get(name); ok {
		return ratio
	}
	return 1
}

func ContainsAudioRatio(name string) bool {
	name = FormatMatchingModelName(name)
	_, ok := audioRatioMap.Get(name)
	return ok
}

func ContainsAudioCompletionRatio(name string) bool {
	name = FormatMatchingModelName(name)
	_, ok := audioCompletionRatioMap.Get(name)
	return ok
}

func ModelRatio2JSONString() string {
	return modelRatioMap.MarshalJSONString()
}

var defaultImageRatio = map[string]float64{
	"gpt-image-1": 2,
	"gpt-image-2": 1.6, // $8 / $5 = 1.6x
}

// defaultImageCompletionRatio 图片输出 token 计费倍率，未配置时 GetImageCompletionRatio 回退到 CompletionRatio
var defaultImageCompletionRatio = map[string]float64{
	"gpt-image-1":                8, // 与文本补全倍率一致
	"gpt-image-2":                6, // $30 / $5 = 6x
	"gemini-3-pro-image-preview": 6, // Gemini 图片模型输出
}

var imageRatioMap = types.NewRWMap[string, float64]()
var imageCompletionRatioMap = types.NewRWMap[string, float64]()
var audioRatioMap = types.NewRWMap[string, float64]()
var audioCompletionRatioMap = types.NewRWMap[string, float64]()

func ImageRatio2JSONString() string {
	return imageRatioMap.MarshalJSONString()
}

func UpdateImageRatioByJSONString(jsonStr string) error {
	return types.LoadFromJsonString(imageRatioMap, jsonStr)
}

func GetImageRatio(name string) (float64, bool) {
	ratio, ok := imageRatioMap.Get(name)
	if !ok {
		return 1, false // Default to 1 if not found
	}
	return ratio, true
}

// GetImageCompletionRatio 获取输出图片 token 的计费倍率，未配置时回退到 CompletionRatio
func GetImageCompletionRatio(name string) float64 {
	name = FormatMatchingModelName(name)
	if strings.Contains(name, "/") {
		if ratio, ok := imageCompletionRatioMap.Get(name); ok {
			return ratio
		}
	}
	if ratio, ok := imageCompletionRatioMap.Get(name); ok {
		return ratio
	}
	return GetCompletionRatio(name)
}

func ImageCompletionRatio2JSONString() string {
	return imageCompletionRatioMap.MarshalJSONString()
}

func UpdateImageCompletionRatioByJSONString(jsonStr string) error {
	return types.LoadFromJsonStringWithCallback(imageCompletionRatioMap, jsonStr, InvalidateExposedDataCache)
}

func GetImageCompletionRatioCopy() map[string]float64 {
	return imageCompletionRatioMap.ReadAll()
}

func AudioRatio2JSONString() string {
	return audioRatioMap.MarshalJSONString()
}

func UpdateAudioRatioByJSONString(jsonStr string) error {
	return types.LoadFromJsonStringWithCallback(audioRatioMap, jsonStr, InvalidateExposedDataCache)
}

func AudioCompletionRatio2JSONString() string {
	return audioCompletionRatioMap.MarshalJSONString()
}

func UpdateAudioCompletionRatioByJSONString(jsonStr string) error {
	return types.LoadFromJsonStringWithCallback(audioCompletionRatioMap, jsonStr, InvalidateExposedDataCache)
}

func GetModelRatioCopy() map[string]float64 {
	return modelRatioMap.ReadAll()
}

func GetModelPriceCopy() map[string]float64 {
	return modelPriceMap.ReadAll()
}

func GetCompletionRatioCopy() map[string]float64 {
	return completionRatioMap.ReadAll()
}

// 转换模型名，减少渠道必须配置各种带参数模型
func FormatMatchingModelName(name string) string {

	// 一些业务侧会把模型做成别名后缀（例如 "-nsfw" 用于走不同 endpoint），
	// 但定价/倍率通常与基础模型一致；这里做归一化以复用配置。
	if strings.HasSuffix(name, "-nsfw") {
		name = strings.TrimSuffix(name, "-nsfw")
	}

	if strings.HasPrefix(name, "gemini-2.5-flash-lite") {
		name = handleThinkingBudgetModel(name, "gemini-2.5-flash-lite", "gemini-2.5-flash-lite-thinking-*")
	} else if strings.HasPrefix(name, "gemini-2.5-flash") {
		name = handleThinkingBudgetModel(name, "gemini-2.5-flash", "gemini-2.5-flash-thinking-*")
	} else if strings.HasPrefix(name, "gemini-2.5-pro") {
		name = handleThinkingBudgetModel(name, "gemini-2.5-pro", "gemini-2.5-pro-thinking-*")
	}

	if strings.HasPrefix(name, "gpt-4-gizmo") {
		name = "gpt-4-gizmo-*"
	}
	if strings.HasPrefix(name, "gpt-4o-gizmo") {
		name = "gpt-4o-gizmo-*"
	}
	return name
}

// result: 倍率or价格， usePrice， exist
func GetModelRatioOrPrice(model string) (float64, bool, bool) { // price or ratio
	price, usePrice := GetModelPrice(model, false)
	if usePrice {
		return price, true, true
	}
	modelRatio, success, _ := GetModelRatio(model)
	if success {
		return modelRatio, false, true
	}
	return 37.5, false, false
}

// ==================== VideoRatio / VideoCompletionRatio ====================
//
// VideoRatio: 视频倍率（与 ModelRatio 语义一致，倍率以系统基准 $2/M tokens 为基准），用于视频按量计费模型的「输入价格」。
// VideoCompletionRatio: 视频补全倍率/价格。
//   - 当 VideoRatio 存在且 != 0：VideoCompletionRatio 作为倍率（相对 VideoRatio）
//   - 当 VideoRatio 不存在或 == 0：VideoCompletionRatio 作为价格（$/M tokens），直接用于计费
//
// VideoCompletionRatio 支持两种格式：
//   - 数字：价格或倍率（取决于 VideoRatio 是否有效）
//   - 带音频分档：{"noAudio": 1.2, "audio": 2.4}（价格或倍率，按 generate_audio 取值）

var (
	videoRatioMap      map[string]float64 = nil
	videoRatioMapMutex                    = sync.RWMutex{}
)

var (
	videoCompletionRatioPrimaryMap    map[string]float64           = nil
	videoCompletionRatioAudioMap      map[string]VideoAudioPricing = nil
	videoCompletionRatioRawMap        map[string]interface{}       = nil
	videoCompletionRatioMapMutex                                   = sync.RWMutex{}
	videoCompletionRatioAudioMapMutex                              = sync.RWMutex{}
	videoCompletionRatioRawMapMutex                                = sync.RWMutex{}
)

func GetVideoRatio(name string) (float64, bool) {
	name = FormatMatchingModelName(name)
	videoRatioMapMutex.RLock()
	defer videoRatioMapMutex.RUnlock()
	if videoRatioMap == nil {
		return 0, false
	}
	v, ok := videoRatioMap[name]
	return v, ok
}

func GetVideoCompletionRatioPricing(name string, generateAudio bool) (float64, bool) {
	name = FormatMatchingModelName(name)

	// 1) 优先取 VideoCompletionRatio 的分档（noAudio/audio）
	if audioPricing, ok := getVideoCompletionAudioPricing(name); ok {
		value := 0.0
		if generateAudio && audioPricing.Audio > 0 {
			value = audioPricing.Audio
		} else if !generateAudio && audioPricing.NoAudio > 0 {
			value = audioPricing.NoAudio
		} else if audioPricing.NoAudio > 0 {
			value = audioPricing.NoAudio
		} else if audioPricing.Audio > 0 {
			value = audioPricing.Audio
		}
		if value > 0 {
			if vr, hasVR := GetVideoRatio(name); hasVR && vr != 0 {
				return vr * value, true
			}
			return value, true
		}
	}

	// 2) 再取 VideoCompletionRatio 的数字值
	if v, ok := getVideoCompletionPrimaryValue(name); ok && v > 0 {
		if vr, hasVR := GetVideoRatio(name); hasVR && vr != 0 {
			return vr * v, true
		}
		return v, true
	}

	return 0, false
}

func getVideoCompletionPrimaryValue(name string) (float64, bool) {
	videoCompletionRatioMapMutex.RLock()
	defer videoCompletionRatioMapMutex.RUnlock()
	if videoCompletionRatioPrimaryMap == nil {
		return 0, false
	}
	v, ok := videoCompletionRatioPrimaryMap[name]
	return v, ok
}

func getVideoCompletionAudioPricing(name string) (VideoAudioPricing, bool) {
	videoCompletionRatioAudioMapMutex.RLock()
	defer videoCompletionRatioAudioMapMutex.RUnlock()
	if videoCompletionRatioAudioMap == nil {
		return VideoAudioPricing{}, false
	}
	v, ok := videoCompletionRatioAudioMap[name]
	return v, ok
}

// GetVideoCompletionRatioVideoPricing 按「是否有视频输入」维度获取 VideoCompletionRatio（UpToken seedance 等使用）
// 当配置中包含 noVideo/video 字段时使用此维度；否则回退到 GetVideoCompletionRatioPricing 的 noAudio/audio 逻辑
func GetVideoCompletionRatioVideoPricing(name string, hasVideoInput bool) (float64, bool) {
	name = FormatMatchingModelName(name)

	// 1) 优先取 VideoCompletionRatio 的 noVideo/video 分档
	if pricing, ok := getVideoCompletionAudioPricing(name); ok {
		// 仅当 noVideo/video 维度有配置时才走此分支
		if pricing.NoVideo > 0 || pricing.Video > 0 {
			value := 0.0
			if hasVideoInput && pricing.Video > 0 {
				value = pricing.Video
			} else if !hasVideoInput && pricing.NoVideo > 0 {
				value = pricing.NoVideo
			} else if pricing.NoVideo > 0 {
				value = pricing.NoVideo
			} else if pricing.Video > 0 {
				value = pricing.Video
			}
			if value > 0 {
				if vr, hasVR := GetVideoRatio(name); hasVR && vr != 0 {
					return vr * value, true
				}
				return value, true
			}
		}
	}

	// 2) 再取 VideoCompletionRatio 的数字值
	if v, ok := getVideoCompletionPrimaryValue(name); ok && v > 0 {
		if vr, hasVR := GetVideoRatio(name); hasVR && vr != 0 {
			return vr * v, true
		}
		return v, true
	}

	return 0, false
}

// GetVideoCompletionRatioResolutionPricing 获取带分辨率维度的视频定价
// 优先从分辨率嵌套配置查找，如果没有则回退到扁平 noVideo/video 配置
func GetVideoCompletionRatioResolutionPricing(name string, hasVideoInput bool, resolution string) (float64, bool) {
	name = FormatMatchingModelName(name)
	resolution = normalizeResolution(resolution)

	// 1) 尝试分辨率嵌套配置
	videoCompletionRatioResolutionMapMutex.RLock()
	resMap, ok := videoCompletionRatioResolutionMap[name]
	videoCompletionRatioResolutionMapMutex.RUnlock()

	if ok && len(resMap.Resolutions) > 0 {
		// 精确匹配分辨率
		if pricing, found := resMap.Resolutions[resolution]; found {
			return selectVideoInputPrice(pricing, hasVideoInput, name)
		}
		// 最近分辨率回退
		if pricing, found := findNearestResolution(resMap, resolution); found {
			return selectVideoInputPrice(pricing, hasVideoInput, name)
		}
	}

	// 2) 回退到扁平 noVideo/video 配置（向后兼容）
	return GetVideoCompletionRatioVideoPricing(name, hasVideoInput)
}

// HasVideoCompletionRatioResolution 检查模型是否配置了分辨率维度定价
func HasVideoCompletionRatioResolution(name string) bool {
	name = FormatMatchingModelName(name)
	videoCompletionRatioResolutionMapMutex.RLock()
	resMap, ok := videoCompletionRatioResolutionMap[name]
	videoCompletionRatioResolutionMapMutex.RUnlock()
	return ok && len(resMap.Resolutions) > 0
}

// selectVideoInputPrice 根据是否有视频输入选择对应的价格值
func selectVideoInputPrice(pricing VideoAudioPricing, hasVideoInput bool, name string) (float64, bool) {
	value := 0.0
	if hasVideoInput && pricing.Video > 0 {
		value = pricing.Video
	} else if !hasVideoInput && pricing.NoVideo > 0 {
		value = pricing.NoVideo
	}
	// 如果 noVideo/video 维度没有值，尝试 noAudio/audio
	if value == 0 {
		if hasVideoInput && pricing.Audio > 0 {
			value = pricing.Audio
		} else if !hasVideoInput && pricing.NoAudio > 0 {
			value = pricing.NoAudio
		}
	}
	if value <= 0 {
		return 0, false
	}

	// 检查是否是倍率体系（有 VideoRatio）
	if vr, hasVR := GetVideoRatio(name); hasVR && vr != 0 {
		return vr * value, true
	}
	return value, true
}

// normalizeResolution 标准化分辨率字符串 "720P" -> "720p", "1080P" -> "1080p", "4K" -> "4k"
func normalizeResolution(s string) string {
	return strings.TrimSpace(strings.ToLower(s))
}

// resolutionToPixels 将分辨率字符串转为数字像素值，用于最近匹配
func resolutionToPixels(res string) int {
	if v, ok := resolutionOrder[res]; ok {
		return v
	}
	// 尝试从字符串中提取数字（如 "1440p" -> 1440）
	numStr := strings.TrimRight(strings.TrimRight(res, "pkPK"), " ")
	if v := 0; len(numStr) > 0 {
		for _, c := range numStr {
			if c >= '0' && c <= '9' {
				v = v*10 + int(c-'0')
			} else {
				return 0
			}
		}
		return v
	}
	return 0
}

// findNearestResolution 在分辨率配置中找到最近的匹配
func findNearestResolution(resMap VideoResolutionPricing, targetResolution string) (VideoAudioPricing, bool) {
	targetPixels := resolutionToPixels(targetResolution)
	if targetPixels == 0 {
		// 无法解析目标分辨率，尝试回退到 720p
		if p, ok := resMap.Resolutions["720p"]; ok {
			return p, true
		}
		// 返回第一个可用的
		for _, p := range resMap.Resolutions {
			return p, true
		}
		return VideoAudioPricing{}, false
	}

	// 收集所有配置的分辨率并排序
	type resEntry struct {
		resolution string
		pixels     int
		pricing    VideoAudioPricing
	}
	entries := make([]resEntry, 0, len(resMap.Resolutions))
	for res, pricing := range resMap.Resolutions {
		pixels := resolutionToPixels(res)
		if pixels > 0 {
			entries = append(entries, resEntry{resolution: res, pixels: pixels, pricing: pricing})
		}
	}
	if len(entries) == 0 {
		return VideoAudioPricing{}, false
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].pixels < entries[j].pixels
	})

	// 找到最近的（绝对值差最小）
	best := entries[0]
	bestDist := math.Abs(float64(best.pixels - targetPixels))
	for _, e := range entries[1:] {
		dist := math.Abs(float64(e.pixels - targetPixels))
		if dist < bestDist {
			best = e
			bestDist = dist
		}
	}

	return best.pricing, true
}

// loadVideoRatioFromDatabase 从数据库加载视频倍率配置（OptionMap["VideoRatio"]）
func loadVideoRatioFromDatabase() {
	videoRatioMapMutex.Lock()
	defer videoRatioMapMutex.Unlock()

	common.OptionMapRWMutex.RLock()
	videoStr, exists := common.OptionMap["VideoRatio"]
	common.OptionMapRWMutex.RUnlock()

	m := make(map[string]float64)
	if exists && videoStr != "" {
		var raw map[string]float64
		if err := common.Unmarshal([]byte(videoStr), &raw); err == nil {
			for k, v := range raw {
				m[k] = v
				formatted := FormatMatchingModelName(k)
				if formatted != k {
					m[formatted] = v
				}
			}
			videoRatioMap = m
			common.SysLog("Loaded VideoRatio configuration from database")
			return
		}
	}

	// 默认无配置：保持空 map，表示没有启用 VideoRatio 体系
	videoRatioMap = m
}

// loadVideoCompletionRatioFromDatabase 从数据库加载视频补全倍率/价格（OptionMap["VideoCompletionRatio"]）
func loadVideoCompletionRatioFromDatabase() {
	videoCompletionRatioMapMutex.Lock()
	defer videoCompletionRatioMapMutex.Unlock()
	videoCompletionRatioAudioMapMutex.Lock()
	defer videoCompletionRatioAudioMapMutex.Unlock()
	videoCompletionRatioRawMapMutex.Lock()
	defer videoCompletionRatioRawMapMutex.Unlock()
	videoCompletionRatioResolutionMapMutex.Lock()
	defer videoCompletionRatioResolutionMapMutex.Unlock()

	common.OptionMapRWMutex.RLock()
	videoStr, exists := common.OptionMap["VideoCompletionRatio"]
	common.OptionMapRWMutex.RUnlock()

	if exists && videoStr != "" {
		var rawMap map[string]interface{}
		if err := common.Unmarshal([]byte(videoStr), &rawMap); err == nil {
			videoCompletionRatioRawMap = rawMap
			videoCompletionRatioPrimaryMap, videoCompletionRatioAudioMap, videoCompletionRatioResolutionMap = buildVideoCompletionRatioCaches(rawMap)
			common.SysLog("Loaded VideoCompletionRatio configuration from database")
			return
		}
	}

	videoCompletionRatioRawMap = make(map[string]interface{})
	videoCompletionRatioPrimaryMap = make(map[string]float64)
	videoCompletionRatioAudioMap = make(map[string]VideoAudioPricing)
	videoCompletionRatioResolutionMap = make(map[string]VideoResolutionPricing)
}

func buildVideoCompletionRatioCaches(rawMap map[string]interface{}) (map[string]float64, map[string]VideoAudioPricing, map[string]VideoResolutionPricing) {
	primary := make(map[string]float64, len(rawMap))
	audio := make(map[string]VideoAudioPricing)
	resolution := make(map[string]VideoResolutionPricing)

	for modelName, value := range rawMap {
		targetKeys := []string{modelName}
		formatted := FormatMatchingModelName(modelName)
		if formatted != modelName {
			targetKeys = append(targetKeys, formatted)
		}

		switch v := value.(type) {
		case map[string]interface{}:
			// 检测是否为分辨率嵌套格式：所有 key 匹配分辨率模式且 value 为 map
			if isResolutionNestedMap(v) {
				resPricing := VideoResolutionPricing{
					Resolutions: make(map[string]VideoAudioPricing),
				}
				for resKey, resVal := range v {
					normalizedRes := normalizeResolution(resKey)
					if resMap, ok := resVal.(map[string]interface{}); ok {
						pricing := parseVideoAudioPricingFromMap(resMap)
						resPricing.Resolutions[normalizedRes] = pricing
					}
				}
				if len(resPricing.Resolutions) > 0 {
					for _, key := range targetKeys {
						resolution[key] = resPricing
					}
					// 同时将默认分辨率（720p）的值写入 audio map 作为兼容兜底
					if defaultPricing, ok := resPricing.Resolutions["720p"]; ok {
						if defaultPricing.NoVideo > 0 || defaultPricing.Video > 0 || defaultPricing.NoAudio > 0 || defaultPricing.Audio > 0 {
							for _, key := range targetKeys {
								audio[key] = defaultPricing
							}
						}
					}
				}
			} else {
				// 原有逻辑：扁平的 noAudio/audio/noVideo/video 结构
				pricing := parseVideoAudioPricingFromMap(v)
				if pricing.NoAudio > 0 || pricing.Audio > 0 || pricing.NoVideo > 0 || pricing.Video > 0 {
					for _, key := range targetKeys {
						audio[key] = pricing
					}
				}

				// 支持 default 字段作为数字回退
				if def, ok := extractFloatFromMap(v, "default"); ok && def > 0 {
					for _, key := range targetKeys {
						primary[key] = def
					}
				}
			}
		default:
			if f, ok := extractFloat(value); ok && f > 0 {
				for _, key := range targetKeys {
					primary[key] = f
				}
			}
		}
	}

	return primary, audio, resolution
}

// isResolutionNestedMap 检测 map 是否为分辨率嵌套格式
// 判断逻辑：至少有一个 key 匹配分辨率模式（如 720p, 1080p, 4k），且对应 value 为 map
func isResolutionNestedMap(m map[string]interface{}) bool {
	hasResKey := false
	for key, val := range m {
		if resolutionPattern.MatchString(key) {
			if _, isMap := val.(map[string]interface{}); isMap {
				hasResKey = true
			}
		}
	}
	return hasResKey
}

// parseVideoAudioPricingFromMap 从 map 解析 VideoAudioPricing 结构
func parseVideoAudioPricingFromMap(m map[string]interface{}) VideoAudioPricing {
	pricing := VideoAudioPricing{}
	if noAudio, ok := extractFloatFromMap(m, "noAudio", "no_audio"); ok {
		pricing.NoAudio = noAudio
	}
	if audioVal, ok := extractFloatFromMap(m, "audio", "withAudio", "with_audio"); ok {
		pricing.Audio = audioVal
	}
	// 有无视频输入维度（UpToken seedance 等）
	if noVideo, ok := extractFloatFromMap(m, "noVideo", "no_video"); ok {
		pricing.NoVideo = noVideo
	}
	if videoVal, ok := extractFloatFromMap(m, "video", "withVideo", "with_video"); ok {
		pricing.Video = videoVal
	}
	return pricing
}

func UpdateVideoRatioByJSONString(jsonStr string) error {
	var raw map[string]float64
	if err := common.Unmarshal([]byte(jsonStr), &raw); err != nil {
		return err
	}

	m := make(map[string]float64, len(raw))
	for k, v := range raw {
		m[k] = v
		formatted := FormatMatchingModelName(k)
		if formatted != k {
			m[formatted] = v
		}
	}

	videoRatioMapMutex.Lock()
	videoRatioMap = m
	videoRatioMapMutex.Unlock()

	InvalidateExposedDataCache()
	return nil
}

func UpdateVideoCompletionRatioByJSONString(jsonStr string) error {
	var rawMap map[string]interface{}
	if err := common.Unmarshal([]byte(jsonStr), &rawMap); err != nil {
		return err
	}

	primary, audio, resolutionData := buildVideoCompletionRatioCaches(rawMap)

	videoCompletionRatioMapMutex.Lock()
	videoCompletionRatioPrimaryMap = primary
	videoCompletionRatioMapMutex.Unlock()

	videoCompletionRatioAudioMapMutex.Lock()
	videoCompletionRatioAudioMap = audio
	videoCompletionRatioAudioMapMutex.Unlock()

	videoCompletionRatioRawMapMutex.Lock()
	videoCompletionRatioRawMap = rawMap
	videoCompletionRatioRawMapMutex.Unlock()

	videoCompletionRatioResolutionMapMutex.Lock()
	videoCompletionRatioResolutionMap = resolutionData
	videoCompletionRatioResolutionMapMutex.Unlock()

	InvalidateExposedDataCache()
	return nil
}

// ==================== VideoModelPricePerSecond ====================
// 优先级：先读数据库 OptionMap["VideoModelPricePerSecond"]，仅当无配置或解析失败时才用下方默认值兜底。

var defaultVideoModelPricePerSecond = map[string]float64{
	"sora-2":     0.1,  // $0.1/秒
	"sora-2-pro": 0.15, // $0.15/秒
	// wan2.6 系列（720P/480P 基准价，按秒计费，分辨率倍率通过 ProcessAliOtherRatios 处理）
	// 价格来源：阿里云官方定价，RMB÷7.3 转美元
	"wan2.6-t2v": 0.0192, // ¥0.14/s÷7.3，720P 基准
	"wan2.6-i2v": 0.0048, // ¥0.035/s÷7.3，720P 基准
	"wan2.6-r2v": 0.0192, // ¥0.14/s÷7.3，720P 基准
}

// VideoAudioPricing 带音频/无音频的视频定价结构（Veo：generateAudio true 用 audio，否则用 noAudio）
// 同时支持有无视频输入的定价维度（UpToken seedance：hasVideo true 用 Video，否则用 NoVideo）
type VideoAudioPricing struct {
	NoAudio float64 `json:"noAudio,omitempty"`
	Audio   float64 `json:"audio,omitempty"`
	NoVideo float64 `json:"noVideo,omitempty"`
	Video   float64 `json:"video,omitempty"`
}

// VideoResolutionPricing 分辨率维度的视频定价结构
// 例如 {"720p": {NoVideo: 7.35, Video: 4.515}, "1080p": {NoVideo: 8.085, Video: 4.935}}
type VideoResolutionPricing struct {
	Resolutions map[string]VideoAudioPricing
}

var (
	videoCompletionRatioResolutionMap      map[string]VideoResolutionPricing = nil
	videoCompletionRatioResolutionMapMutex                                   = sync.RWMutex{}
)

// resolutionPattern 用于检测 key 是否为分辨率格式（如 480p, 720p, 1080p, 4k）
var resolutionPattern = regexp.MustCompile(`(?i)^\d+[pk]$`)

// resolutionOrder 分辨率排序，数字越大分辨率越高（用于最近匹配）
var resolutionOrder = map[string]int{
	"360p":  360,
	"480p":  480,
	"720p":  720,
	"768p":  768,
	"1080p": 1080,
	"2k":    1440,
	"4k":    2160,
}

// VideoFlashResolutionPricing wan2.6-*-flash：noAudio/audio 各对应 720p/1080p 等分档单价（美元/秒）
type VideoFlashResolutionPricing struct {
	NoAudio map[string]float64 `json:"noAudio,omitempty"`
	Audio   map[string]float64 `json:"audio,omitempty"`
}

// Veo 模型（含 generate / fast）：按 parameters.generateAudio 选 noAudio 或 audio 价
var defaultVideoAudioPricing = map[string]VideoAudioPricing{
	"veo-3.0-generate-preview":      {NoAudio: 0.2, Audio: 0.4},
	"veo-3.1-generate-preview":      {NoAudio: 0.2, Audio: 0.4},
	"veo-3.0-fast-generate-001":     {NoAudio: 0.1, Audio: 0.15},
	"veo-3.1-fast-generate-preview": {NoAudio: 0.1, Audio: 0.15},
	// Kling V3 系列：按 generate_audio 选 noAudio 或 audio 价（官方单位：USD/秒）
	// 标准模式：$0.084(无音频) / $0.126(有音频)
	// 专业模式（kling-v3-pro）：$0.112(无音频) / $0.168(有音频)
	"kling-v3":          {NoAudio: 0.084, Audio: 0.126},
	"kling-v3-pro":      {NoAudio: 0.112, Audio: 0.168},
	"kling-v3-omni":     {NoAudio: 0.084, Audio: 0.126},
	"kling-v3-omni-pro": {NoAudio: 0.112, Audio: 0.168},
}

// wan2.6 flash：¥0.15/0.25（无声 720P/1080P）、¥0.3/0.5（有声 720P/1080P），按 ¥÷7.0 换算为美元/秒
// Veo 3.1 系列：按 generateAudio × resolution 分档（美元/秒）
var defaultVideoFlashResolutionPricing = map[string]VideoFlashResolutionPricing{
	"wan2.6-i2v-flash": {
		NoAudio: map[string]float64{"720p": 0.0214, "1080p": 0.0357},
		Audio:   map[string]float64{"720p": 0.0429, "1080p": 0.0714},
	},
	"wan2.6-r2v-flash": {
		NoAudio: map[string]float64{"720p": 0.0214, "1080p": 0.0357},
		Audio:   map[string]float64{"720p": 0.0429, "1080p": 0.0714},
	},
	// Veo 3.1：720p/1080p 同价，4k 更贵
	"veo-3.1-generate-001": {
		NoAudio: map[string]float64{"720p": 0.20, "1080p": 0.20, "4k": 0.40},
		Audio:   map[string]float64{"720p": 0.40, "1080p": 0.40, "4k": 0.60},
	},
	// Veo 3.1 Fast：各分辨率独立定价
	"veo-3.1-fast-generate-001": {
		NoAudio: map[string]float64{"720p": 0.08, "1080p": 0.10, "4k": 0.25},
		Audio:   map[string]float64{"720p": 0.10, "1080p": 0.12, "4k": 0.30},
	},
	// Veo 3.1 Lite：仅 720p/1080p
	"veo-3.1-lite-generate-001": {
		NoAudio: map[string]float64{"720p": 0.03, "1080p": 0.05},
		Audio:   map[string]float64{"720p": 0.05, "1080p": 0.08},
	},
}

var (
	videoModelPricePerSecondMap      map[string]float64 = nil
	videoModelPricePerSecondMapMutex                    = sync.RWMutex{}
)

// ImageModelPricePerImage：按张计费，从 options 表 key=ImageModelPricePerImage 的 JSON 加载；懒加载（因 InitRatioSettings 早于 InitOptionMap）
var (
	imageModelPricePerImageMap      map[string]float64
	imageModelPricePerImageMapMutex = sync.RWMutex{}
	imageModelPricePerImageLoadOnce sync.Once
)

var (
	videoModelAudioPricePerSecondMap      map[string]VideoAudioPricing = nil
	videoModelAudioPricePerSecondMapMutex                              = sync.RWMutex{}
)

var (
	videoFlashResolutionPricePerSecondMap      map[string]VideoFlashResolutionPricing = nil
	videoFlashResolutionPricePerSecondMapMutex                                        = sync.RWMutex{}
)

// videoResolutionPricePerSecondMap 非 flash 模型按分辨率分档单价（如 wan2.6-t2v: {"720p": N, "1080p": N}）
var (
	videoResolutionPricePerSecondMap      map[string]map[string]float64 = nil
	videoResolutionPricePerSecondMapMutex                               = sync.RWMutex{}
)

var (
	videoModelPricePerSecondRawMap      map[string]interface{} = nil
	videoModelPricePerSecondRawMapMutex                        = sync.RWMutex{}
)

// GetImageModelPricePerImageFromOptionMap 直接从 OptionMap 读并按 name 查找（供 relay 兜底），兼容 seedream-* / doubao-seedream-*
func GetImageModelPricePerImageFromOptionMap(name string) (float64, bool) {
	return getImageModelPricePerImageFromOptionMap(name)
}

func getImageModelPricePerImageFromOptionMap(name string) (float64, bool) {
	common.OptionMapRWMutex.RLock()
	priceStr := common.OptionMap["ImageModelPricePerImage"]
	common.OptionMapRWMutex.RUnlock()
	if priceStr == "" {
		return -1, false
	}
	var m map[string]float64
	if err := common.Unmarshal([]byte(priceStr), &m); err != nil {
		return -1, false
	}
	if p, ok := m[name]; ok && p >= 0 {
		return p, true
	}
	if !strings.HasPrefix(name, "doubao-") {
		if p, ok := m["doubao-"+name]; ok && p >= 0 {
			return p, true
		}
	} else {
		if p, ok := m[strings.TrimPrefix(name, "doubao-")]; ok && p >= 0 {
			return p, true
		}
	}
	return -1, false
}

// loadImageModelPricePerImageFromDatabase 从 OptionMap["ImageModelPricePerImage"] 加载按张计费价格（需在 InitOptionMap 之后生效，通过 Get 时懒加载）
func loadImageModelPricePerImageFromDatabase() {
	imageModelPricePerImageMapMutex.Lock()
	defer imageModelPricePerImageMapMutex.Unlock()
	imageModelPricePerImageMap = make(map[string]float64)
	common.OptionMapRWMutex.RLock()
	priceStr, exists := common.OptionMap["ImageModelPricePerImage"]
	common.OptionMapRWMutex.RUnlock()
	if exists && priceStr != "" {
		var priceMap map[string]float64
		if err := common.Unmarshal([]byte(priceStr), &priceMap); err == nil {
			imageModelPricePerImageMap = priceMap
		}
	}
}

// GetImageModelPricePerImage 获取按张计费价格；兼容 key 为 seedream-* 或 doubao-seedream-*（先查 name，再查 doubao-+name，再查去掉 doubao- 的 name）
// 优先从 OptionMap 实时读取，与 nebula 行为一致
func GetImageModelPricePerImage(name string) (float64, bool) {
	if p, ok := getImageModelPricePerImageFromOptionMap(name); ok {
		return p, true
	}
	imageModelPricePerImageLoadOnce.Do(loadImageModelPricePerImageFromDatabase)
	imageModelPricePerImageMapMutex.RLock()
	defer imageModelPricePerImageMapMutex.RUnlock()
	if price, ok := imageModelPricePerImageMap[name]; ok {
		return price, true
	}
	if !strings.HasPrefix(name, "doubao-") {
		if price, ok := imageModelPricePerImageMap["doubao-"+name]; ok {
			return price, true
		}
	} else {
		alt := strings.TrimPrefix(name, "doubao-")
		if price, ok := imageModelPricePerImageMap[alt]; ok {
			return price, true
		}
	}
	return -1, false
}

// NormalizeVideoResolutionKey 统一为 480p/720p/1080p/4k 小写
func NormalizeVideoResolutionKey(res string) string {
	s := strings.TrimSpace(strings.ToLower(res))
	if s == "" {
		return "720p"
	}
	// 4k / 2160p 统一为 "4k"
	if s == "4k" || s == "2160p" || strings.Contains(s, "2160") {
		return "4k"
	}
	if !strings.HasSuffix(s, "p") && !strings.HasSuffix(s, "k") {
		s = s + "p"
	}
	return s
}

func minPositiveInFloatMap(m map[string]float64) float64 {
	var min float64
	for _, v := range m {
		if v <= 0 {
			continue
		}
		if min == 0 || v < min {
			min = v
		}
	}
	return min
}

func minFlashResolutionPrice(f VideoFlashResolutionPricing) float64 {
	a := minPositiveInFloatMap(f.NoAudio)
	b := minPositiveInFloatMap(f.Audio)
	if a == 0 {
		return b
	}
	if b == 0 {
		return a
	}
	if a < b {
		return a
	}
	return b
}

// minResolutionPrice 返回分辨率价格表中最小的正数价格（用于余额预检兜底）
func minResolutionPrice(m map[string]float64) float64 {
	return minPositiveInFloatMap(m)
}

func getVideoResolutionPricing(name string) (map[string]float64, bool) {
	videoResolutionPricePerSecondMapMutex.RLock()
	defer videoResolutionPricePerSecondMapMutex.RUnlock()
	pricing, ok := videoResolutionPricePerSecondMap[name]
	return pricing, ok
}

// GetVideoModelPricePerSecond 获取视频模型每秒价格
func GetVideoModelPricePerSecond(name string) (float64, bool) {
	name = FormatMatchingModelName(name)
	price, ok := getVideoPerSecondPriceFromPrimaryMap(name)
	if ok && price > 0 {
		return price, true
	}

	if flash, ok := getVideoFlashResolutionPricing(name); ok {
		if m := minFlashResolutionPrice(flash); m > 0 {
			return m, true
		}
	}

	if audioPricing, ok := getVideoAudioPricing(name); ok {
		if audioPricing.NoAudio > 0 {
			return audioPricing.NoAudio, true
		}
		if audioPricing.Audio > 0 {
			return audioPricing.Audio, true
		}
	}

	return -1, false
}

// GetVideoModelPricePerSecondForBillingWithResolution 按音频与分辨率取价（wan2.6-flash 分档；其它模型忽略 resolution，回退为原逻辑）
func GetVideoModelPricePerSecondForBillingWithResolution(name string, generateAudio bool, resolution string) (float64, bool) {
	name = FormatMatchingModelName(name)
	resKey := NormalizeVideoResolutionKey(resolution)

	if flash, ok := getVideoFlashResolutionPricing(name); ok {
		var tier map[string]float64
		if generateAudio {
			tier = flash.Audio
		} else {
			tier = flash.NoAudio
		}
		if len(tier) > 0 {
			if p, ok := tier[resKey]; ok && p > 0 {
				return p, true
			}
			if p, ok := tier["720p"]; ok && p > 0 {
				return p, true
			}
			if p, ok := tier["1080p"]; ok && p > 0 {
				return p, true
			}
			if p, ok := tier["480p"]; ok && p > 0 {
				return p, true
			}
			for _, p := range tier {
				if p > 0 {
					return p, true
				}
			}
		}
	}

	// 非 flash：查分辨率分档价表（{"720p": N, "1080p": N}）
	if resMap, ok := getVideoResolutionPricing(name); ok && len(resMap) > 0 {
		if p, ok := resMap[resKey]; ok && p > 0 {
			return p, true
		}
		// 分辨率未命中时按优先级回退：720p → 1080p → 480p → 任意正值
		for _, fallback := range []string{"720p", "1080p", "480p"} {
			if p, ok := resMap[fallback]; ok && p > 0 {
				return p, true
			}
		}
		for _, p := range resMap {
			if p > 0 {
				return p, true
			}
		}
	}

	return getVideoModelPricePerSecondForBillingFlat(name, generateAudio)
}

// GetVideoModelPricePerSecondForBilling 按是否生成音频返回每秒价格（用于 Veo 等 noAudio/audio 分离定价）
// wan2.6-flash 有分档时默认按 720p 档取价（与旧行为一致）
func GetVideoModelPricePerSecondForBilling(name string, generateAudio bool) (float64, bool) {
	return GetVideoModelPricePerSecondForBillingWithResolution(name, generateAudio, "720p")
}

// getVideoModelPricePerSecondForBillingFlat 非 flash 分档时的 noAudio/audio 与单一数字价
func getVideoModelPricePerSecondForBillingFlat(name string, generateAudio bool) (float64, bool) {
	name = FormatMatchingModelName(name)
	if audioPricing, ok := getVideoAudioPricing(name); ok {
		if generateAudio && audioPricing.Audio > 0 {
			return audioPricing.Audio, true
		}
		if !generateAudio && audioPricing.NoAudio > 0 {
			return audioPricing.NoAudio, true
		}
		if audioPricing.NoAudio > 0 {
			return audioPricing.NoAudio, true
		}
		if audioPricing.Audio > 0 {
			return audioPricing.Audio, true
		}
	}
	price, ok := getVideoPerSecondPriceFromPrimaryMap(name)
	if ok && price > 0 {
		return price, true
	}
	return -1, false
}

func getVideoPerSecondPriceFromPrimaryMap(name string) (float64, bool) {
	videoModelPricePerSecondMapMutex.RLock()
	defer videoModelPricePerSecondMapMutex.RUnlock()
	price, ok := videoModelPricePerSecondMap[name]
	return price, ok
}

func getVideoAudioPricing(name string) (VideoAudioPricing, bool) {
	videoModelAudioPricePerSecondMapMutex.RLock()
	defer videoModelAudioPricePerSecondMapMutex.RUnlock()
	pricing, ok := videoModelAudioPricePerSecondMap[name]
	return pricing, ok
}

func getVideoFlashResolutionPricing(name string) (VideoFlashResolutionPricing, bool) {
	videoFlashResolutionPricePerSecondMapMutex.RLock()
	defer videoFlashResolutionPricePerSecondMapMutex.RUnlock()
	pricing, ok := videoFlashResolutionPricePerSecondMap[name]
	return pricing, ok
}

func extractResolutionFloatMap(m map[string]interface{}) map[string]float64 {
	out := make(map[string]float64)
	for k, val := range m {
		if f, ok := extractFloat(val); ok && f > 0 {
			out[NormalizeVideoResolutionKey(k)] = f
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func mergeVideoFlashResolutionPricing(dst *VideoFlashResolutionPricing, v map[string]interface{}) {
	if na, ok := v["noAudio"]; ok {
		switch t := na.(type) {
		case map[string]interface{}:
			dst.NoAudio = extractResolutionFloatMap(t)
		}
	}
	if na, ok := v["no_audio"]; ok && dst.NoAudio == nil {
		switch t := na.(type) {
		case map[string]interface{}:
			dst.NoAudio = extractResolutionFloatMap(t)
		}
	}
	if au, ok := v["audio"]; ok {
		switch t := au.(type) {
		case map[string]interface{}:
			dst.Audio = extractResolutionFloatMap(t)
		}
	}
	if au, ok := v["withAudio"]; ok && dst.Audio == nil {
		switch t := au.(type) {
		case map[string]interface{}:
			dst.Audio = extractResolutionFloatMap(t)
		}
	}
	if au, ok := v["with_audio"]; ok && dst.Audio == nil {
		switch t := au.(type) {
		case map[string]interface{}:
			dst.Audio = extractResolutionFloatMap(t)
		}
	}
}

func buildVideoModelPriceCaches(rawMap map[string]interface{}) (map[string]float64, map[string]VideoAudioPricing, map[string]VideoFlashResolutionPricing, map[string]map[string]float64) {
	priceMap := make(map[string]float64, len(rawMap))
	audioMap := make(map[string]VideoAudioPricing)
	flashMap := make(map[string]VideoFlashResolutionPricing)
	resolutionMap := make(map[string]map[string]float64)

	for modelName, value := range rawMap {
		targetKeys := []string{modelName}
		formatted := FormatMatchingModelName(modelName)
		if formatted != modelName {
			targetKeys = append(targetKeys, formatted)
		}

		switch v := value.(type) {
		case map[string]interface{}:
			var flash VideoFlashResolutionPricing
			mergeVideoFlashResolutionPricing(&flash, v)
			hasFlash := flash.NoAudio != nil || flash.Audio != nil
			if hasFlash {
				for _, key := range targetKeys {
					flashMap[key] = flash
				}
			}

			pricing := VideoAudioPricing{}
			if noAudio, ok := extractFloatFromMap(v, "noAudio", "no_audio"); ok {
				pricing.NoAudio = noAudio
			}
			if audio, ok := extractFloatFromMap(v, "audio", "withAudio", "with_audio"); ok {
				pricing.Audio = audio
			}
			if pricing.NoAudio > 0 || pricing.Audio > 0 {
				for _, key := range targetKeys {
					audioMap[key] = pricing
				}
			}
			if def, ok := extractFloatFromMap(v, "default"); ok {
				for _, key := range targetKeys {
					priceMap[key] = def
				}
			} else if !hasFlash && pricing.NoAudio > 0 {
				for _, key := range targetKeys {
					priceMap[key] = pricing.NoAudio
				}
			} else if !hasFlash && pricing.Audio > 0 {
				for _, key := range targetKeys {
					priceMap[key] = pricing.Audio
				}
			}

			// 非 flash、无 noAudio/audio 时，尝试作为分辨率分档价格表
			// 支持两种格式：
			//   带 wrapper：{"resolutions": {"720p": N, "1080p": N}}（与 Java 侧一致）
			//   裸键：{"720p": N, "1080p": N}
			if !hasFlash && pricing.NoAudio == 0 && pricing.Audio == 0 {
				var rm map[string]float64
				if resObj, ok := v["resolutions"]; ok {
					// 带 resolutions wrapper key
					if resMap, ok := resObj.(map[string]interface{}); ok {
						rm = extractResolutionFloatMap(resMap)
					}
				}
				if len(rm) == 0 {
					// 裸键兜底
					rm = extractResolutionFloatMap(v)
				}
				if len(rm) > 0 {
					for _, key := range targetKeys {
						resolutionMap[key] = rm
					}
					// 取最小分辨率价作为 priceMap 兜底（供余额预检用）
					if _, exists := priceMap[targetKeys[0]]; !exists {
						if minPrice := minResolutionPrice(rm); minPrice > 0 {
							for _, key := range targetKeys {
								priceMap[key] = minPrice
							}
						}
					}
				}
			}
		default:
			if f, ok := extractFloat(v); ok {
				for _, key := range targetKeys {
					priceMap[key] = f
				}
			}
		}
	}

	return priceMap, audioMap, flashMap, resolutionMap
}

func extractFloatFromMap(m map[string]interface{}, keys ...string) (float64, bool) {
	for _, key := range keys {
		if val, exists := m[key]; exists {
			if f, ok := extractFloat(val); ok {
				return f, true
			}
		}
	}
	return 0, false
}

func extractFloat(value interface{}) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case int32:
		return float64(v), true
	case uint:
		return float64(v), true
	case uint64:
		return float64(v), true
	case json.Number:
		if f, err := v.Float64(); err == nil {
			return f, true
		}
	}
	return 0, false
}

// loadVideoModelPricePerSecondFromDatabase 从数据库加载视频按秒价格配置
func loadVideoModelPricePerSecondFromDatabase() {
	videoModelPricePerSecondMapMutex.Lock()
	defer videoModelPricePerSecondMapMutex.Unlock()
	videoModelAudioPricePerSecondMapMutex.Lock()
	defer videoModelAudioPricePerSecondMapMutex.Unlock()
	videoFlashResolutionPricePerSecondMapMutex.Lock()
	defer videoFlashResolutionPricePerSecondMapMutex.Unlock()
	videoResolutionPricePerSecondMapMutex.Lock()
	defer videoResolutionPricePerSecondMapMutex.Unlock()
	videoModelPricePerSecondRawMapMutex.Lock()
	defer videoModelPricePerSecondRawMapMutex.Unlock()

	if videoStr, exists := common.OptionMap["VideoModelPricePerSecond"]; exists && videoStr != "" {
		var rawMap map[string]interface{}
		if err := common.Unmarshal([]byte(videoStr), &rawMap); err == nil {
			videoModelPricePerSecondRawMap = rawMap
			videoModelPricePerSecondMap, videoModelAudioPricePerSecondMap, videoFlashResolutionPricePerSecondMap, videoResolutionPricePerSecondMap = buildVideoModelPriceCaches(rawMap)
			common.SysLog("Loaded video model price per second configuration from database")
			return
		}
	}

	// 无数据库配置或解析失败时，使用代码中的默认价格兜底
	videoModelPricePerSecondMap = make(map[string]float64, len(defaultVideoModelPricePerSecond))
	for k, v := range defaultVideoModelPricePerSecond {
		videoModelPricePerSecondMap[k] = v
	}
	videoModelAudioPricePerSecondMap = make(map[string]VideoAudioPricing, len(defaultVideoAudioPricing))
	videoFlashResolutionPricePerSecondMap = make(map[string]VideoFlashResolutionPricing, len(defaultVideoFlashResolutionPricing))
	videoResolutionPricePerSecondMap = make(map[string]map[string]float64)
	videoModelPricePerSecondRawMap = make(map[string]interface{})
	for k, v := range defaultVideoAudioPricing {
		videoModelAudioPricePerSecondMap[k] = v
		videoModelPricePerSecondMap[k] = v.NoAudio
		videoModelPricePerSecondRawMap[k] = map[string]float64{
			"noAudio": v.NoAudio,
			"audio":   v.Audio,
		}
	}
	for k, v := range defaultVideoFlashResolutionPricing {
		videoFlashResolutionPricePerSecondMap[k] = v
		videoModelPricePerSecondRawMap[k] = map[string]interface{}{
			"noAudio": map[string]float64{
				"720p":  v.NoAudio["720p"],
				"1080p": v.NoAudio["1080p"],
			},
			"audio": map[string]float64{
				"720p":  v.Audio["720p"],
				"1080p": v.Audio["1080p"],
			},
		}
		if m := minFlashResolutionPrice(v); m > 0 {
			videoModelPricePerSecondMap[k] = m
		}
	}
	for k, v := range defaultVideoModelPricePerSecond {
		if _, exists := videoModelPricePerSecondRawMap[k]; !exists {
			videoModelPricePerSecondRawMap[k] = v
		}
	}
	common.SysLog("Using default video model price per second configuration")
}

// UpdateVideoModelPricePerSecondByJSONString 更新视频按秒价格（由后台配置变更触发）
func UpdateVideoModelPricePerSecondByJSONString(jsonStr string) error {
	var rawMap map[string]interface{}
	if err := common.Unmarshal([]byte(jsonStr), &rawMap); err != nil {
		return err
	}

	videoModelPricePerSecondMapMutex.Lock()
	defer videoModelPricePerSecondMapMutex.Unlock()
	videoModelAudioPricePerSecondMapMutex.Lock()
	defer videoModelAudioPricePerSecondMapMutex.Unlock()
	videoFlashResolutionPricePerSecondMapMutex.Lock()
	defer videoFlashResolutionPricePerSecondMapMutex.Unlock()
	videoResolutionPricePerSecondMapMutex.Lock()
	defer videoResolutionPricePerSecondMapMutex.Unlock()
	videoModelPricePerSecondRawMapMutex.Lock()
	defer videoModelPricePerSecondRawMapMutex.Unlock()

	videoModelPricePerSecondRawMap = rawMap
	videoModelPricePerSecondMap, videoModelAudioPricePerSecondMap, videoFlashResolutionPricePerSecondMap, videoResolutionPricePerSecondMap = buildVideoModelPriceCaches(rawMap)
	InvalidateExposedDataCache()
	return nil
}
