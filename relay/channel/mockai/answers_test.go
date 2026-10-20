package mockai

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMockAnswers 校验文案池的数量、长度、唯一性，以及不含任何露馅字眼。
func TestMockAnswers(t *testing.T) {
	require.Len(t, mockAnswers, 100)

	banned := []string{"mock", "测试", "占位", "示例", "placeholder", "AI 助手", "AI助手"}
	seen := make(map[string]struct{}, len(mockAnswers))

	for i, a := range mockAnswers {
		require.NotEmpty(t, strings.TrimSpace(a), "answer #%d is blank", i)
		assert.LessOrEqual(t, utf8.RuneCountInString(a), 100, "answer #%d too long: %q", i, a)

		_, dup := seen[a]
		assert.False(t, dup, "answer #%d duplicated: %q", i, a)
		seen[a] = struct{}{}

		lower := strings.ToLower(a)
		for _, b := range banned {
			assert.NotContains(t, lower, strings.ToLower(b), "answer #%d contains banned %q", i, b)
		}
	}
}
