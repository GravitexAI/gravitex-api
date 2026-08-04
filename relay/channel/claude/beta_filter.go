package claude

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
)

// BetaFilterDebugLog 控制是否输出 anthropic-beta 过滤/重命名的详细日志。
// 上线初期保持 true 便于排查；稳定后可改为 false，或挂到环境变量 / 配置中心。
var BetaFilterDebugLog = true

// FilterTarget 标识本次过滤的目标上游。
type FilterTarget int

const (
	TargetAnthropicDirect FilterTarget = iota // 直连 Anthropic：透传，仅在 debug 时识别未知 flag
	TargetBedrock                             // AWS Bedrock InvokeModel / InvokeModelWithResponseStream
	TargetBedrockConverse                     // AWS Bedrock Converse / ConverseStream（扩展白名单）
	TargetVertex                              // Google Vertex AI
)

// TargetFromChannelType 根据 RelayInfo.ChannelType 推导目标。
func TargetFromChannelType(channelType int) FilterTarget {
	switch channelType {
	case constant.ChannelTypeAws:
		return TargetBedrock
	case constant.ChannelTypeVertexAi:
		return TargetVertex
	default:
		return TargetAnthropicDirect
	}
}

// ResolveBetaTarget 解析目标：override 非空且识别时优先使用，否则 fallback 到按 ChannelType。
// 用于 Anthropic 类型渠道但实际上游是 Bedrock/Vertex 转发的场景：admin 在渠道配置里
// 显式指定 anthropic_beta_target，即可让白名单过滤按真实底层执行，避免脏 flag 上打 400。
func ResolveBetaTarget(channelType int, override string) FilterTarget {
	switch strings.ToLower(strings.TrimSpace(override)) {
	case "bedrock":
		return TargetBedrock
	case "bedrock-converse":
		return TargetBedrockConverse
	case "vertex":
		return TargetVertex
	case "direct", "anthropic":
		return TargetAnthropicDirect
	}
	return TargetFromChannelType(channelType)
}

// Bedrock InvokeModel / InvokeModelWithResponseStream 支持的 beta flag 白名单。
// 数据源：AWS 官方文档 + LiteLLM bedrock mapping。
var bedrockInvokeBetaWhitelist = map[string]bool{
	"computer-use-2025-01-24":                true,
	"computer-use-2025-11-24":                true,
	"token-efficient-tools-2025-02-19":       true,
	"output-128k-2025-02-19":                 true,
	"dev-full-thinking-2025-05-14":           true,
	"context-1m-2025-08-07":                  true,
	"effort-2025-11-24":                      true,
	"tool-search-tool-2025-10-19":            true,
	"tool-examples-2025-10-29":               true,
	"fine-grained-tool-streaming-2025-05-14": true,
	"compact-2026-01-12":                     true,
}

// Bedrock Converse 在 InvokeModel 白名单基础上额外支持的 flag。
var bedrockConverseBetaExtra = map[string]bool{
	"interleaved-thinking-2025-05-14": true,
	"context-management-2025-06-27":   true,
	"structured-outputs-2025-11-13":   true,
}

// Vertex AI 支持的 beta flag 白名单。
var vertexBetaWhitelist = map[string]bool{
	"computer-use-2025-01-24":         true,
	"computer-use-2025-11-24":         true,
	"context-1m-2025-08-07":           true,
	"context-management-2025-06-27":   true,
	"compact-2026-01-12":              true,
	"interleaved-thinking-2025-05-14": true,
	"tool-search-tool-2025-10-19":     true,
	"web-search-2025-03-05":           true,
}

// 名称映射：客户端发的旧名 → 供应商接受的新名。
var bedrockBetaRename = map[string]string{
	"advanced-tool-use-2025-11-20": "tool-search-tool-2025-10-19",
}
var vertexBetaRename = map[string]string{
	"advanced-tool-use-2025-11-20": "tool-search-tool-2025-10-19",
}

// Anthropic 官方已知的全部 beta flag（直连透传时用于识别"未知新 flag"）。
// 同步源：anthropic-sdk-python anthropic_beta_param.py + LiteLLM anthropic mapping。
var anthropicKnownBetas = map[string]bool{
	"claude-code-20250219":                     true,
	"computer-use-2025-01-24":                  true,
	"computer-use-2025-11-24":                  true,
	"context-1m-2025-08-07":                    true,
	"context-management-2025-06-27":            true,
	"compact-2026-01-12":                       true,
	"effort-2025-11-24":                        true,
	"extended-cache-ttl-2025-04-11":            true,
	"files-api-2025-04-14":                     true,
	"fine-grained-tool-streaming-2025-05-14":   true,
	"interleaved-thinking-2025-05-14":          true,
	"message-batches-2024-09-24":               true,
	"model-context-window-exceeded-2025-08-26": true,
	"output-128k-2025-02-19":                   true,
	"pdfs-2024-09-25":                          true,
	"prompt-caching-2024-07-31":                true,
	"prompt-caching-scope-2026-01-05":          true,
	"redact-thinking-2026-02-12":               true,
	"skills-2025-10-02":                        true,
	"structured-outputs-2025-11-13":            true,
	"thinking-token-count-2026-05-13":          true,
	"token-counting-2024-11-01":                true,
	"token-efficient-tools-2025-02-19":         true,
	"tool-examples-2025-10-29":                 true,
	"tool-search-tool-2025-10-19":              true,
	"advanced-tool-use-2025-11-20":             true,
	"advisor-tool-2026-03-01":                  true,
	"code-execution-2025-05-22":                true,
	"code-execution-2025-08-25":                true,
	"mcp-client-2025-04-04":                    true,
	"mcp-client-2025-11-20":                    true,
	"web-fetch-2025-09-10":                     true,
	"web-search-2025-03-05":                    true,
	"fast-mode-2026-02-01":                     true,
	"dev-full-thinking-2025-05-14":             true,
	"oauth-2025-04-20":                         true,
	"output-300k-2026-03-24":                   true,
	"user-profiles-2026-03-24":                 true,
	"managed-agents-2026-04-01":                true,
	"cache-diagnosis-2026-04-07":               true,
	"server-side-fallback-2026-06-01":          true,
	"fallback-credit-2026-06-01":               true,
}

// FilterBetaFlags 对 anthropic-beta 原始字符串做白名单过滤 + 重命名。
//
// 参数：
//   - raw       客户端发来的逗号分隔字符串，例如 "context-1m-2025-08-07,effort-2025-11-24"
//   - target    目标上游（决定使用哪份白名单 / 重命名表）
//   - requestID 仅用于调试日志的关联，可传空串
//
// 返回值是经重命名后的最终 flag 列表；调用方自行决定如何序列化（拼字符串/JSON 数组等）。
// 当 BetaFilterDebugLog 为 true 且发生丢弃/重命名时，会通过 common.SysLog 输出一条记录。
func FilterBetaFlags(raw string, target FilterTarget, requestID string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")

	// 直连分支：完全透传，可选输出"未知 flag"告警
	if target == TargetAnthropicDirect {
		kept := make([]string, 0, len(parts))
		unknown := make([]string, 0)
		for _, p := range parts {
			flag := strings.TrimSpace(p)
			if flag == "" {
				continue
			}
			kept = append(kept, flag)
			if BetaFilterDebugLog && !anthropicKnownBetas[flag] {
				unknown = append(unknown, flag)
			}
		}
		if BetaFilterDebugLog && len(unknown) > 0 {
			common.SysLog(fmt.Sprintf(
				"[anthropic_compat] channel=anthropic req=%s unknown_betas=%v (consider updating whitelist)",
				requestID, unknown,
			))
		}
		return kept
	}

	// 非直连分支：白名单 + 重命名
	whitelist, rename := lookupRules(target)
	kept := make([]string, 0, len(parts))
	dropped := make([]string, 0)
	renamed := make(map[string]string)
	seen := make(map[string]bool)

	for _, p := range parts {
		flag := strings.TrimSpace(p)
		if flag == "" {
			continue
		}
		original := flag
		if mapped, ok := rename[flag]; ok {
			flag = mapped
			renamed[original] = mapped
		}
		if whitelist[flag] {
			if !seen[flag] {
				kept = append(kept, flag)
				seen[flag] = true
			}
			continue
		}
		dropped = append(dropped, original)
	}

	if BetaFilterDebugLog && (len(dropped) > 0 || len(renamed) > 0) {
		common.SysLog(fmt.Sprintf(
			"[anthropic_compat] channel=%s req=%s kept=%v dropped=%v renamed=%v",
			targetName(target), requestID, kept, dropped, renamed,
		))
	}
	return kept
}

func lookupRules(target FilterTarget) (map[string]bool, map[string]string) {
	switch target {
	case TargetBedrock:
		return bedrockInvokeBetaWhitelist, bedrockBetaRename
	case TargetBedrockConverse:
		merged := make(map[string]bool, len(bedrockInvokeBetaWhitelist)+len(bedrockConverseBetaExtra))
		for k, v := range bedrockInvokeBetaWhitelist {
			merged[k] = v
		}
		for k, v := range bedrockConverseBetaExtra {
			merged[k] = v
		}
		return merged, bedrockBetaRename
	case TargetVertex:
		return vertexBetaWhitelist, vertexBetaRename
	default:
		return nil, nil
	}
}

func targetName(t FilterTarget) string {
	switch t {
	case TargetBedrock:
		return "bedrock"
	case TargetBedrockConverse:
		return "bedrock-converse"
	case TargetVertex:
		return "vertex"
	default:
		return "anthropic"
	}
}

// KeptBetaSet 把 FilterBetaFlags 返回的最终 flag 列表转换成 set，方便 O(1) 判断某个
// beta 是否被保留。供 body 顶级字段裁剪（见下方 Strip* 函数）复用。
func KeptBetaSet(keptBetas []string) map[string]bool {
	kept := make(map[string]bool, len(keptBetas))
	for _, b := range keptBetas {
		kept[b] = true
	}
	return kept
}

// StripContextManagementForDroppedBetas 清理 context_management 顶级字段：该字段依赖
// context-management-2025-06-27 beta（仅 Bedrock Converse / Vertex 支持）。beta 被过滤
// 目标剔除后仍把字段发上去，Bedrock/Vertex 会返回 "Extra inputs are not permitted"。
func StripContextManagementForDroppedBetas(raw json.RawMessage, kept map[string]bool) json.RawMessage {
	if len(raw) == 0 || kept["context-management-2025-06-27"] {
		return raw
	}
	return nil
}

// StripOutputConfigForDroppedBetas 清理 output_config 中依赖具体 beta 的子字段：
//   - effort 依赖 effort-2025-11-24
//   - format 依赖 structured-outputs-2025-11-13（该字段在官方 Anthropic API 已 GA，
//     客户端可能不再携带对应 beta；但 Bedrock/Vertex 等兼容层仍按白名单判断是否支持）
//
// 只裁剪失效的子字段、保留其余合法字段，避免同时携带 effort 和 format 时被整体清空误伤。
func StripOutputConfigForDroppedBetas(raw json.RawMessage, kept map[string]bool) json.RawMessage {
	if len(raw) == 0 || (kept["effort-2025-11-24"] && kept["structured-outputs-2025-11-13"]) {
		return raw
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return raw
	}
	if !kept["effort-2025-11-24"] {
		delete(m, "effort")
	}
	if !kept["structured-outputs-2025-11-13"] {
		delete(m, "format")
	}
	if len(m) == 0 {
		return nil
	}
	out, err := json.Marshal(m)
	if err != nil {
		return raw
	}
	return out
}
