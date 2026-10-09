package ratio_setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// -nsfw 后缀归一化的契约：任何路径（计费、渠道路由、令牌白名单）都不剥掉
// "-nsfw" 后缀——nsfw 别名是独立模型，定价/渠道/白名单必须显式配置，
// 禁止回退到基础模型（见 model/channel_cache.go 的路由归一化回退）。
func TestFormatModelNameKeepsNSFWSuffix(t *testing.T) {
	assert.Equal(t, "glm-5.3-nsfw", FormatMatchingModelName("glm-5.3-nsfw"))
	assert.Equal(t, "seedance-2.0-nsfw", FormatMatchingModelName("seedance-2.0-nsfw"))
}

// thinking 预算与 gizmo 通配折叠行为保持不变。
func TestFormatModelNameStillCollapsesWildcardVariants(t *testing.T) {
	assert.Equal(t, "gemini-2.5-pro-thinking-*", FormatMatchingModelName("gemini-2.5-pro-thinking-1024"))
	assert.Equal(t, "gemini-2.5-flash-lite-thinking-*", FormatMatchingModelName("gemini-2.5-flash-lite-thinking-64k"))
	assert.Equal(t, "gpt-4-gizmo-*", FormatMatchingModelName("gpt-4-gizmo-abc"))
	assert.Equal(t, "gpt-4o-gizmo-*", FormatMatchingModelName("gpt-4o-gizmo-xyz"))
}
