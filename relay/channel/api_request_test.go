package channel

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// setRewindableBody must wire req.GetBody for a seekable upstream body
// (ReaderOnly(BodyStorage)) so net/http can rewind and retry the request after a
// dropped keep-alive connection, instead of failing with
// "net/http: cannot rewind body after connection loss". GetBody must yield the
// full original body even after the primary body was already consumed.
func TestSetRewindableBody_RewindsSeekableBody(t *testing.T) {
	t.Parallel()

	jsonData := []byte(`{"model":"claude-3","stream":true,"messages":[{"role":"user","content":"hi"}]}`)
	body, size, closer, err := relaycommon.NewOutboundJSONBody(jsonData)
	require.NoError(t, err)
	t.Cleanup(func() { _ = closer.Close() })
	require.Equal(t, int64(len(jsonData)), size)

	req, err := http.NewRequest(http.MethodPost, "https://example.com/v1/messages", body)
	require.NoError(t, err)
	// A type-erased seekable body is not one of the in-memory types http.NewRequest
	// auto-detects, so GetBody starts nil — this is exactly the gap being fixed.
	require.Nil(t, req.GetBody)

	setRewindableBody(req, body)
	require.NotNil(t, req.GetBody, "GetBody must be set so net/http can retry after connection loss")

	// Consume the primary body, as the transport does on the first attempt.
	first, err := io.ReadAll(req.Body)
	require.NoError(t, err)
	require.Equal(t, jsonData, first)

	// GetBody must rewind and yield the full body again for the retry.
	rewound, err := req.GetBody()
	require.NoError(t, err)
	t.Cleanup(func() { _ = rewound.Close() })
	second, err := io.ReadAll(rewound)
	require.NoError(t, err)
	require.Equal(t, jsonData, second)
}

// A non-seekable body cannot be rewound, so setRewindableBody must leave GetBody
// nil rather than wiring a closure that would replay a truncated body.
func TestSetRewindableBody_NonSeekableLeavesGetBodyNil(t *testing.T) {
	t.Parallel()

	req, err := http.NewRequest(http.MethodPost, "https://example.com/v1/messages", nil)
	require.NoError(t, err)
	req.GetBody = nil

	// *bytes.Buffer is a Reader but not a Seeker.
	setRewindableBody(req, bytes.NewBufferString("not seekable"))
	require.Nil(t, req.GetBody, "non-seekable body must leave GetBody nil")
}

func TestProcessHeaderOverride_ChannelTestSkipsPassthroughRules(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx.Request.Header.Set("X-Trace-Id", "trace-123")

	info := &relaycommon.RelayInfo{
		IsChannelTest: true,
		ChannelMeta: &relaycommon.ChannelMeta{
			HeadersOverride: map[string]any{
				"*": "",
			},
		},
	}

	headers, err := processHeaderOverride(info, ctx)
	require.NoError(t, err)
	require.Empty(t, headers)
}

func TestProcessHeaderOverride_ChannelTestSkipsClientHeaderPlaceholder(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx.Request.Header.Set("X-Trace-Id", "trace-123")

	info := &relaycommon.RelayInfo{
		IsChannelTest: true,
		ChannelMeta: &relaycommon.ChannelMeta{
			HeadersOverride: map[string]any{
				"X-Upstream-Trace": "{client_header:X-Trace-Id}",
			},
		},
	}

	headers, err := processHeaderOverride(info, ctx)
	require.NoError(t, err)
	_, ok := headers["x-upstream-trace"]
	require.False(t, ok)
}

func TestProcessHeaderOverride_NonTestKeepsClientHeaderPlaceholder(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx.Request.Header.Set("X-Trace-Id", "trace-123")

	info := &relaycommon.RelayInfo{
		IsChannelTest: false,
		ChannelMeta: &relaycommon.ChannelMeta{
			HeadersOverride: map[string]any{
				"X-Upstream-Trace": "{client_header:X-Trace-Id}",
			},
		},
	}

	headers, err := processHeaderOverride(info, ctx)
	require.NoError(t, err)
	require.Equal(t, "trace-123", headers["x-upstream-trace"])
}

func TestProcessHeaderOverride_RuntimeOverrideIsFinalHeaderMap(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	info := &relaycommon.RelayInfo{
		IsChannelTest:             false,
		UseRuntimeHeadersOverride: true,
		RuntimeHeadersOverride: map[string]any{
			"x-static":  "runtime-value",
			"x-runtime": "runtime-only",
		},
		ChannelMeta: &relaycommon.ChannelMeta{
			HeadersOverride: map[string]any{
				"X-Static": "legacy-value",
				"X-Legacy": "legacy-only",
			},
		},
	}

	headers, err := processHeaderOverride(info, ctx)
	require.NoError(t, err)
	require.Equal(t, "runtime-value", headers["x-static"])
	require.Equal(t, "runtime-only", headers["x-runtime"])
	_, exists := headers["x-legacy"]
	require.False(t, exists)
}

func TestProcessHeaderOverride_PassthroughSkipsAcceptEncoding(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx.Request.Header.Set("X-Trace-Id", "trace-123")
	ctx.Request.Header.Set("Accept-Encoding", "gzip")

	info := &relaycommon.RelayInfo{
		IsChannelTest: false,
		ChannelMeta: &relaycommon.ChannelMeta{
			HeadersOverride: map[string]any{
				"*": "",
			},
		},
	}

	headers, err := processHeaderOverride(info, ctx)
	require.NoError(t, err)
	require.Equal(t, "trace-123", headers["x-trace-id"])

	_, hasAcceptEncoding := headers["accept-encoding"]
	require.False(t, hasAcceptEncoding)
}

// anthropic-beta / anthropic-version 必须从通配符 passthrough 黑名单跳过，
// 否则会绕过 claude.CommonClaudeHeadersOperation 里的白名单过滤，把客户端原始的
// beta flag 直接透传给 Bedrock / Vertex，触发 "invalid beta flag" /
// "Unexpected value(s) ..." 类报错（真实生产案例）。
func TestProcessHeaderOverride_PassthroughSkipsAnthropicBetaAndVersion(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	ctx.Request.Header.Set("Anthropic-Beta", "advisor-tool-2026-03-01,prompt-caching-scope-2026-01-05")
	ctx.Request.Header.Set("Anthropic-Version", "2023-06-01")
	ctx.Request.Header.Set("X-Trace-Id", "trace-123")

	info := &relaycommon.RelayInfo{
		IsChannelTest: false,
		ChannelMeta: &relaycommon.ChannelMeta{
			HeadersOverride: map[string]any{
				"*": "",
			},
		},
	}

	headers, err := processHeaderOverride(info, ctx)
	require.NoError(t, err)

	// 非敏感 header 仍走 passthrough
	require.Equal(t, "trace-123", headers["x-trace-id"])

	// anthropic-beta / anthropic-version 必须被跳过，由渠道专属代码处理
	_, hasBeta := headers["anthropic-beta"]
	require.False(t, hasBeta, "anthropic-beta must not be passed through by wildcard")

	_, hasVersion := headers["anthropic-version"]
	require.False(t, hasVersion, "anthropic-version must not be passed through by wildcard")
}

// 显式 override 仍可以设置 anthropic-beta 进入 headerOverride map
// （场景：渠道管理员手动配置覆盖值，processHeaderOverride 不会过滤），
// 但 applyHeaderOverrideToRequest 不会把它真正覆盖到最终请求上（见下一个测试）。
func TestProcessHeaderOverride_ExplicitAnthropicBetaOverrideStillWorks(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	info := &relaycommon.RelayInfo{
		IsChannelTest: false,
		ChannelMeta: &relaycommon.ChannelMeta{
			HeadersOverride: map[string]any{
				"anthropic-beta": "computer-use-2025-01-24",
			},
		},
	}

	headers, err := processHeaderOverride(info, ctx)
	require.NoError(t, err)
	require.Equal(t, "computer-use-2025-01-24", headers["anthropic-beta"])
}

// applyHeaderOverrideToRequest 必须跳过 anthropic-beta，保护 SetupRequestHeader
// 里 claude.CommonClaudeHeadersOperation 已经过滤好的安全值不被任何形式的
// header_override（含 {client_header:anthropic-beta} placeholder）复活成脏值。
// 真实生产案例：filter 日志显示 advisor-tool-2026-03-01 已 dropped，但 Vertex 仍报
// "Unexpected value(s) advisor-tool-2026-03-01 for the anthropic-beta header"。
func TestApplyHeaderOverrideToRequest_SkipsAnthropicBetaToProtectFilter(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodPost, "https://example.com/v1/messages", nil)
	// 模拟 CommonClaudeHeadersOperation 已经过滤完毕，req 上是干净的值
	req.Header.Set("anthropic-beta", "interleaved-thinking-2025-05-14,context-management-2025-06-27")
	req.Header.Set("x-trace-id", "trace-123")

	// 模拟 header_override 试图把脏值覆盖回去（典型场景：{client_header:anthropic-beta} 解析后）
	headerOverride := map[string]string{
		"anthropic-beta": "advisor-tool-2026-03-01,prompt-caching-scope-2026-01-05",
		"x-custom-header": "custom-value",
	}

	applyHeaderOverrideToRequest(req, headerOverride)

	// anthropic-beta 必须保持过滤后的干净值，不能被覆盖
	require.Equal(t,
		"interleaved-thinking-2025-05-14,context-management-2025-06-27",
		req.Header.Get("anthropic-beta"),
		"anthropic-beta must not be overridden by applyHeaderOverrideToRequest")

	// 其他 header 正常被覆盖
	require.Equal(t, "custom-value", req.Header.Get("x-custom-header"))
}

func TestProcessHeaderOverride_PassHeadersTemplateSetsRuntimeHeaders(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	ctx.Request.Header.Set("Originator", "Codex CLI")
	ctx.Request.Header.Set("Session_id", "sess-123")

	info := &relaycommon.RelayInfo{
		IsChannelTest: false,
		RequestHeaders: map[string]string{
			"Originator": "Codex CLI",
			"Session_id": "sess-123",
		},
		ChannelMeta: &relaycommon.ChannelMeta{
			ParamOverride: map[string]any{
				"operations": []any{
					map[string]any{
						"mode":  "pass_headers",
						"value": []any{"Originator", "Session_id", "X-Codex-Beta-Features"},
					},
				},
			},
			HeadersOverride: map[string]any{
				"X-Static": "legacy-value",
			},
		},
	}

	_, err := relaycommon.ApplyParamOverrideWithRelayInfo([]byte(`{"model":"gpt-4.1"}`), info)
	require.NoError(t, err)
	require.True(t, info.UseRuntimeHeadersOverride)
	require.Equal(t, "Codex CLI", info.RuntimeHeadersOverride["originator"])
	require.Equal(t, "sess-123", info.RuntimeHeadersOverride["session_id"])
	_, exists := info.RuntimeHeadersOverride["x-codex-beta-features"]
	require.False(t, exists)
	require.Equal(t, "legacy-value", info.RuntimeHeadersOverride["x-static"])

	headers, err := processHeaderOverride(info, ctx)
	require.NoError(t, err)
	require.Equal(t, "Codex CLI", headers["originator"])
	require.Equal(t, "sess-123", headers["session_id"])
	_, exists = headers["x-codex-beta-features"]
	require.False(t, exists)

	upstreamReq := httptest.NewRequest(http.MethodPost, "https://example.com/v1/responses", nil)
	applyHeaderOverrideToRequest(upstreamReq, headers)
	require.Equal(t, "Codex CLI", upstreamReq.Header.Get("Originator"))
	require.Equal(t, "sess-123", upstreamReq.Header.Get("Session_id"))
	require.Empty(t, upstreamReq.Header.Get("X-Codex-Beta-Features"))
}
