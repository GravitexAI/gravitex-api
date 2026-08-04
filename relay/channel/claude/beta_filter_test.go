package claude

import (
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/assert"
)

// ResolveBetaTarget 决定"当客户端 anthropic-beta 里含有一些只被部分上游支持的 flag 时，
// 到底按哪份白名单裁剪"。回归目标：
//  1. 空 override → 完全等价于原有的 TargetFromChannelType 行为（不破坏历史渠道）
//  2. 显式 override → 覆盖 ChannelType 的默认判断（覆盖 Anthropic-typed 但上游是 Bedrock 的场景）
//  3. 未知 override → 回退到按 ChannelType，不能因为 admin 拼错字符串就静默放开过滤
func TestResolveBetaTarget(t *testing.T) {
	cases := []struct {
		name        string
		channelType int
		override    string
		want        FilterTarget
	}{
		{"empty override, anthropic channel keeps direct", constant.ChannelTypeAnthropic, "", TargetAnthropicDirect},
		{"empty override, aws channel keeps bedrock", constant.ChannelTypeAws, "", TargetBedrock},
		{"empty override, vertex channel keeps vertex", constant.ChannelTypeVertexAi, "", TargetVertex},

		{"anthropic-typed channel overridden to bedrock", constant.ChannelTypeAnthropic, "bedrock", TargetBedrock},
		{"anthropic-typed channel overridden to vertex", constant.ChannelTypeAnthropic, "vertex", TargetVertex},
		{"override is trimmed and case-insensitive", constant.ChannelTypeAnthropic, "  Bedrock  ", TargetBedrock},
		{"bedrock-converse override reaches extended whitelist", constant.ChannelTypeAnthropic, "bedrock-converse", TargetBedrockConverse},
		{"direct override forces passthrough on aws channel", constant.ChannelTypeAws, "direct", TargetAnthropicDirect},

		{"unknown override falls back to channel type", constant.ChannelTypeAws, "gemini", TargetBedrock},
		{"unknown override on anthropic falls back to direct", constant.ChannelTypeAnthropic, "invalid-value", TargetAnthropicDirect},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ResolveBetaTarget(tc.channelType, tc.override)
			assert.Equal(t, tc.want, got)
		})
	}
}

// StripOutputConfigForDroppedBetas 回归目标：只裁剪失效的子字段（effort/format 各自
// 依赖不同 beta），不能因为其中一个 beta 被过滤就把整个 output_config 清空误伤另一个。
func TestStripOutputConfigForDroppedBetas(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		kept map[string]bool
		want string
	}{
		{"both betas kept, passthrough", `{"effort":"high","format":{"type":"json"}}`, map[string]bool{"effort-2025-11-24": true, "structured-outputs-2025-11-13": true}, `{"effort":"high","format":{"type":"json"}}`},
		{"format beta dropped, only effort survives", `{"effort":"high","format":{"type":"json"}}`, map[string]bool{"effort-2025-11-24": true}, `{"effort":"high"}`},
		{"effort beta dropped, only format survives", `{"effort":"high","format":{"type":"json"}}`, map[string]bool{"structured-outputs-2025-11-13": true}, `{"format":{"type":"json"}}`},
		{"both betas dropped, field cleared entirely", `{"effort":"high","format":{"type":"json"}}`, map[string]bool{}, ``},
		{"nil input stays nil", ``, map[string]bool{}, ``},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var raw json.RawMessage
			if tc.raw != "" {
				raw = json.RawMessage(tc.raw)
			}
			got := StripOutputConfigForDroppedBetas(raw, tc.kept)
			if tc.want == "" {
				assert.Nil(t, got)
				return
			}
			assert.JSONEq(t, tc.want, string(got))
		})
	}
}

// StripContextManagementForDroppedBetas 回归目标：仅在对应 beta 未被保留时才清空。
func TestStripContextManagementForDroppedBetas(t *testing.T) {
	raw := json.RawMessage(`{"edits":[{"type":"clear_tool_uses_20250919"}]}`)

	got := StripContextManagementForDroppedBetas(raw, map[string]bool{"context-management-2025-06-27": true})
	assert.Equal(t, raw, got)

	got = StripContextManagementForDroppedBetas(raw, map[string]bool{})
	assert.Nil(t, got)
}
