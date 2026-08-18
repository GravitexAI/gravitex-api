package service

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsAutoModel(t *testing.T) {
	assert.True(t, IsAutoModel("auto"))
	assert.True(t, IsAutoModel("auto:medium"))
	assert.True(t, IsAutoModel("auto"+ratio_setting.CompactModelSuffix))
	assert.True(t, IsAutoModel("auto:high"+ratio_setting.CompactModelSuffix))
	assert.False(t, IsAutoModel("auto-group"))
	assert.False(t, IsAutoModel("gpt-4o"))
}

func TestIsAutoSupportedPath(t *testing.T) {
	assert.True(t, IsAutoSupportedPath("/v1/chat/completions"))
	assert.True(t, IsAutoSupportedPath("/v1/responses"))
	assert.True(t, IsAutoSupportedPath("/v1/responses/compact"))
	assert.True(t, IsAutoSupportedPath("/v1/messages"))
	assert.True(t, IsAutoSupportedPath("/pg/chat/completions"))
	assert.False(t, IsAutoSupportedPath("/v1/embeddings"))
	assert.False(t, IsAutoSupportedPath("/v1/audio/speech"))
	assert.False(t, IsAutoSupportedPath("/v1/images/generations"))
}

func TestClassifyAutoTask(t *testing.T) {
	assert.Equal(t, "agent", classifyAutoTask([]byte(`{"tools":[{"type":"function"}]}`)))
	assert.Equal(t, "vision", classifyAutoTask([]byte(`{"messages":[{"content":[{"type":"image_url","image_url":{"url":"x"}}]}]}`)))
	assert.Equal(t, "code", classifyAutoTask([]byte(`{"messages":[{"role":"user","content":"fix this traceback in def main"}]}`)))
	assert.Equal(t, "translation", classifyAutoTask([]byte(`{"messages":[{"role":"user","content":"请翻译这段话"}]}`)))
	assert.Equal(t, "reasoning", classifyAutoTask([]byte(`{"messages":[{"role":"user","content":"prove this step by step"}]}`)))
	assert.Equal(t, "general", classifyAutoTask([]byte(`{"messages":[{"role":"user","content":"hello"}]}`)))
	assert.Equal(t, "general", classifyAutoTask([]byte(`{"messages":[{"role":"user","content":"how does function_call work"}]}`)))
}

func TestResolveAutoModelDisabled(t *testing.T) {
	withAutoRouterSetting(t, `{"enabled":false}`)
	_, err := ResolveAutoModel(newAutoTestContext(), "auto", []byte(`{"messages":[]}`), []string{"deepseek-chat"})
	require.ErrorIs(t, err, ErrAutoRouterDisabled)
}

func TestResolveAutoModelUsesConfiguredPoolAndTaskPrefer(t *testing.T) {
	withAutoRouterSetting(t, `{
		"enabled": true,
		"default_tier": "medium",
		"stickiness_ttl": 0,
		"tiers": {"medium": ["deepseek-chat", "claude-sonnet"]},
		"task_prefer": {"code": ["claude-sonnet"]}
	}`)

	result, err := ResolveAutoModel(
		newAutoTestContext(),
		"auto",
		[]byte(`{"messages":[{"role":"user","content":"fix this def main traceback"}]}`),
		[]string{"deepseek-chat", "claude-sonnet", "gpt-4o"},
	)
	require.NoError(t, err)
	assert.Equal(t, "claude-sonnet", result.Model)
	assert.Equal(t, "code", result.Task)
	assert.Equal(t, "medium", result.Tier)
	assert.False(t, result.Sticky)
}

func TestResolveAutoModelFiltersUnavailableAndCapability(t *testing.T) {
	withAutoRouterSetting(t, `{
		"enabled": true,
		"default_tier": "medium",
		"stickiness_ttl": 0,
		"tiers": {"medium": ["deepseek-chat", "gpt-4o"]},
		"capabilities": {
			"deepseek-chat": {"vision": false, "tools": false},
			"gpt-4o": {"vision": true, "tools": true}
		}
	}`)

	result, err := ResolveAutoModel(
		newAutoTestContext(),
		"auto",
		[]byte(`{"messages":[{"content":[{"type":"text","text":"what is this"},{"type":"image_url","image_url":{"url":"x"}}]}]}`),
		[]string{"deepseek-chat", "gpt-4o"},
	)
	require.NoError(t, err)
	assert.Equal(t, "gpt-4o", result.Model)
	assert.Equal(t, "vision", result.Task)
}

func TestResolveAutoModelRespectsCostTierAndHeader(t *testing.T) {
	withAutoRouterSetting(t, `{
		"enabled": true,
		"default_tier": "medium",
		"stickiness_ttl": 0,
		"tiers": {
			"low": ["deepseek-chat"],
			"medium": ["gpt-4o-mini"],
			"high": ["claude-sonnet"]
		}
	}`)

	low, err := ResolveAutoModel(newAutoTestContext(), "auto:low", []byte(`{"messages":[]}`), []string{"deepseek-chat", "gpt-4o-mini", "claude-sonnet"})
	require.NoError(t, err)
	assert.Equal(t, "deepseek-chat", low.Model)
	assert.Equal(t, "low", low.Tier)

	ctx := newAutoTestContext()
	ctx.Request.Header.Set("X-Cost-Tier", "high")
	high, err := ResolveAutoModel(ctx, "auto", []byte(`{"messages":[]}`), []string{"deepseek-chat", "gpt-4o-mini", "claude-sonnet"})
	require.NoError(t, err)
	assert.Equal(t, "claude-sonnet", high.Model)
	assert.Equal(t, "high", high.Tier)
}

func TestResolveAutoModelStickiness(t *testing.T) {
	withAutoRouterSetting(t, `{
		"enabled": true,
		"default_tier": "medium",
		"stickiness_ttl": 1800,
		"tiers": {"medium": ["deepseek-chat", "gpt-4o-mini"]},
		"weights": {"deepseek-chat": 1, "gpt-4o-mini": 1}
	}`)

	ctx := newAutoTestContext()
	ctx.Request.Header.Set("X-Session-Id", "session-sticky-1")
	body := []byte(`{"messages":[{"role":"user","content":"hello there"}]}`)
	available := []string{"deepseek-chat", "gpt-4o-mini"}

	first, err := ResolveAutoModel(ctx, "auto", body, available)
	require.NoError(t, err)

	second, err := ResolveAutoModel(ctx, "auto", body, available)
	require.NoError(t, err)
	assert.Equal(t, first.Model, second.Model)
	assert.True(t, second.Sticky)
}

func TestResolveAutoModelStickinessUsesFirstUserMessage(t *testing.T) {
	withAutoRouterSetting(t, `{
		"enabled": true,
		"default_tier": "medium",
		"stickiness_ttl": 1800,
		"tiers": {"medium": ["deepseek-chat", "gpt-4o-mini"]}
	}`)

	ctx := newAutoTestContext()
	available := []string{"deepseek-chat", "gpt-4o-mini"}
	first, err := ResolveAutoModel(ctx, "auto", []byte(`{"messages":[{"role":"user","content":"sticky prefix hello"}]}`), available)
	require.NoError(t, err)
	second, err := ResolveAutoModel(ctx, "auto", []byte(`{"messages":[{"role":"user","content":"sticky prefix hello"},{"role":"assistant","content":"ok"},{"role":"user","content":"and then what"}]}`), available)
	require.NoError(t, err)
	assert.Equal(t, first.Model, second.Model)
	assert.True(t, second.Sticky)
}

func TestResolveAutoModelTokenLimitKeepsAllowedModels(t *testing.T) {
	withAutoRouterSetting(t, `{
		"enabled": true,
		"default_tier": "medium",
		"stickiness_ttl": 0,
		"tiers": {"medium": ["deepseek-chat", "gpt-4o"]}
	}`)

	ctx := newAutoTestContext()
	common.SetContextKey(ctx, constant.ContextKeyTokenModelLimitEnabled, true)
	common.SetContextKey(ctx, constant.ContextKeyTokenModelLimit, map[string]bool{"deepseek-chat": true})

	result, err := ResolveAutoModel(ctx, "auto", []byte(`{"messages":[]}`), []string{"deepseek-chat", "gpt-4o"})
	require.NoError(t, err)
	assert.Equal(t, "deepseek-chat", result.Model)
}

func TestIsAutoModelAllowedByToken(t *testing.T) {
	ctx := newAutoTestContext()
	common.SetContextKey(ctx, constant.ContextKeyTokenModelLimit, map[string]bool{"auto": true})
	assert.True(t, IsAutoModelAllowedByToken(ctx, "auto:medium", "gpt-4o"))

	ctx = newAutoTestContext()
	common.SetContextKey(ctx, constant.ContextKeyTokenModelLimit, map[string]bool{"gpt-4o": true})
	assert.True(t, IsAutoModelAllowedByToken(ctx, "auto", "gpt-4o"))
	assert.False(t, IsAutoModelAllowedByToken(ctx, "auto", "claude-sonnet"))
}

func TestShouldExposeAutoModels(t *testing.T) {
	withAutoRouterSetting(t, `{"enabled":false}`)
	assert.False(t, ShouldExposeAutoModels(newAutoTestContext(), []string{"deepseek-chat"}))

	withAutoRouterSetting(t, `{"enabled":true}`)
	assert.True(t, ShouldExposeAutoModels(newAutoTestContext(), []string{"deepseek-chat"}))
	assert.False(t, ShouldExposeAutoModels(newAutoTestContext(), nil))
}

func TestAppendAutoRouterAdminInfo(t *testing.T) {
	ctx := newAutoTestContext()
	adminInfo := map[string]interface{}{}
	AppendAutoRouterAdminInfo(ctx, adminInfo)
	assert.Empty(t, adminInfo)

	ApplyAutoResolveTransparency(ctx, "auto:medium", &AutoResolveResult{Model: "gpt-4o", Task: "code", Tier: "medium"})
	AppendAutoRouterAdminInfo(ctx, adminInfo)
	assert.Equal(t, "auto:medium", adminInfo["auto_original_model"])
	assert.Equal(t, "code", adminInfo["auto_task"])
	assert.Equal(t, "medium", adminInfo["auto_tier"])
	assert.Equal(t, "gpt-4o", ctx.Writer.Header().Get("X-Auto-Model"))
}

func TestFilterAutoPoolByCapabilityJSON(t *testing.T) {
	jsonFalse := false
	pool := filterAutoPoolByCapability(
		[]byte(`{"response_format":{"type":"json_object"}}`),
		[]string{"reasoner", "chat"},
		map[string]setting.AutoModelCapability{
			"reasoner": {JSON: &jsonFalse},
		},
	)
	assert.Equal(t, []string{"chat"}, pool)
}

func TestFilterAutoPoolByCapabilityKeepsToolFilterWhenVisionUnknown(t *testing.T) {
	toolsFalse := false
	pool := filterAutoPoolByCapability(
		[]byte(`{"tools":[{"type":"function"}],"messages":[{"content":[{"type":"image_url","image_url":{"url":"x"}}]}]}`),
		[]string{"text-only", "chat"},
		map[string]setting.AutoModelCapability{
			"text-only": {Tools: &toolsFalse},
		},
	)
	assert.Equal(t, []string{"chat"}, pool)
}

func withAutoRouterSetting(t *testing.T, jsonString string) {
	t.Helper()
	original := setting.AutoRouter2JsonString()
	require.NoError(t, setting.UpdateAutoRouterByJsonString(jsonString))
	t.Cleanup(func() {
		require.NoError(t, setting.UpdateAutoRouterByJsonString(original))
	})
}

func newAutoTestContext() *gin.Context {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	common.SetContextKey(ctx, constant.ContextKeyUserId, 42)
	return ctx
}
