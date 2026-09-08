package common

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TruncateBase64Content 的输出会被写进 MySQL 的 json 列（tasks.upstream_request_body），
// 非法 JSON 会让整条 INSERT 以 Error 3140 失败、任务行连带丢失。
// 复盘：docs/故障复盘/2026-09-08_seedance2任务丢失_upstream_request_body非法JSON.md
func TestTruncateBase64Content_OutputStaysValidJSON(t *testing.T) {
	repeat := func(unit string, n int) string { return strings.Repeat(unit, n) }
	body := func(text string) string {
		raw, err := json.Marshal(map[string]any{
			"model":   "ep-20260420145750-ghkx9",
			"ratio":   "16:9",
			"content": []any{map[string]any{"type": "text", "text": text}},
		})
		require.NoError(t, err)
		return string(raw)
	}

	cases := []struct {
		name string
		text string
	}{
		{"长英文无引号", repeat("man walking slowly across the empty street ", 60)},
		{"长英文且引号在截断点之后", repeat("manwalkingslowlyacrosstheemptystreet", 80) + `say "hi" then tail`},
		{"长英文且引号在截断点之前", `say "hi" ` + repeat("manwalkingslowlyacrosstheemptystreet", 80)},
		{"长英文含换行转义", repeat("manwalking\\nslowlyacrosstheemptystreet", 80)},
		{"长中文", repeat("电影级游戏CG，PBR 物理材质，8K 高细节，", 60)},
		{"长中文含引号", repeat("电影级游戏CG，PBR 物理材质，", 60) + `他说"你好"结束`},
		{"中英混排", repeat("cinematic 电影级 CG PBR material lighting 8K ", 60)},
		{"纯 base64 长串后紧跟转义引号", repeat("QUJDREVGR0hJSktMTU5PUFFSU1RVVldYWVo", 80) + `\"tail`},
		{"短文本", "a man walking across the street"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := body(tc.text)
			require.True(t, json.Valid([]byte(in)), "fixture must be valid JSON")

			out := TruncateBase64Content(in)

			assert.True(t, json.Valid([]byte(out)), "truncated output must stay valid JSON, got: %s", tailOf(out))
		})
	}
}

// 截断能力不能因为修复而失效：data URI 和无前缀裸 base64 都必须被截短并留下标记。
func TestTruncateBase64Content_StillTruncatesBase64(t *testing.T) {
	const marker = "...[base64数据已截断，长度:"

	t.Run("data URI", func(t *testing.T) {
		payload := strings.Repeat("iVBORw0KGgoAAAANSUhEUg", 400) // ~8800 字节
		in := `{"model":"m","content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,` + payload + `"}}]}`
		require.True(t, json.Valid([]byte(in)))

		out := TruncateBase64Content(in)

		assert.True(t, json.Valid([]byte(out)))
		assert.Contains(t, out, marker)
		assert.Less(t, len(out), len(in))
	})

	t.Run("无前缀裸 base64", func(t *testing.T) {
		payload := strings.Repeat("QUJDREVGR0hJSktMTU5PUFFS", 200) // ~4800 字节，无空白
		in := `{"model":"m","image":"` + payload + `"}`
		require.True(t, json.Valid([]byte(in)))

		out := TruncateBase64Content(in)

		assert.True(t, json.Valid([]byte(out)))
		assert.Contains(t, out, marker)
		assert.Less(t, len(out), len(in))
	})
}

func TestIndexJSONStringEnd(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  int
	}{
		{"普通结束引号", `abc"rest`, 3},
		{"跳过转义引号", `ab\"cd"rest`, 6},
		{"跳过转义反斜杠", `ab\\"rest`, 4},
		{"没有结束引号", `abc`, -1},
		{"转义引号后立即结束", `\""`, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, indexJSONStringEnd(tc.input))
		})
	}
}

func TestTruncateJSONStringPrefix(t *testing.T) {
	cases := []struct {
		name  string
		input string
		max   int
		want  string
	}{
		{"无需截断", "abc", 10, "abc"},
		{"纯 ASCII 按字节截断", "abcdef", 3, "abc"},
		{"不留下孤立反斜杠", `ab\ncd`, 3, "ab"},
		{"保留完整转义序列", `ab\ncd`, 4, `ab\n`},
		{"不切断 \\uXXXX", "ab" + `\u0026` + "cd", 5, "ab"},
		{"保留完整 \\uXXXX", "ab" + `\u0026` + "cd", 8, "ab" + `\u0026`},
		{"不切断多字节字符", "ab中文", 4, "ab"},
		{"保留完整多字节字符", "ab中文", 5, "ab中"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := truncateJSONStringPrefix(tc.input, tc.max)
			assert.Equal(t, tc.want, got)
			assert.LessOrEqual(t, len(got), tc.max)
		})
	}
}

// 长英文散文不是 base64：只按字符占比判断会把提示词误判并去截断它，
// 那才是 2026-09-08 那次任务丢失的起点。
func TestIsBase64String_RejectsProseWithWhitespace(t *testing.T) {
	assert.False(t, isBase64String(strings.Repeat("man walking across the street ", 80)))
	assert.True(t, isBase64String(strings.Repeat("QUJDREVGR0hJSktMTU5PUFFS", 80)))
}

func tailOf(s string) string {
	if len(s) <= 200 {
		return s
	}
	return "..." + s[len(s)-200:]
}
