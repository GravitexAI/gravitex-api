package model

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLogMarshalJSONUserIdIsString 验证 Log 序列化时 user_id 是字符串，
// 避免 JS 端反序列化 Snowflake 时精度丢失（导致点击日志行查看用户详情失败）。
func TestLogMarshalJSONUserIdIsString(t *testing.T) {
	log := Log{
		Id:       42,
		UserId:   2039533181434499136, // Snowflake，超过 JS Number.MAX_SAFE_INTEGER
		Username: "chz",
		Quota:    100,
	}

	data, err := common.Marshal(log)
	require.NoError(t, err)

	output := string(data)
	// user_id 必须以字符串形式输出（带引号）
	assert.Containsf(t, output, `"user_id":"2039533181434499136"`,
		"user_id 必须是字符串以避免 JS 精度丢失，得到 output=%s", output)
	// 不能是 JSON number 形式
	assert.NotContains(t, output, `"user_id":2039533181434499136`,
		"user_id 不能是 JSON number")

	// 其他字段保留原样
	assert.Contains(t, output, `"id":42`)
	assert.Contains(t, output, `"username":"chz"`)
	assert.Contains(t, output, `"quota":100`)
}

// TestLogMarshalJSONNoUserIdRecursion 用 Alias 套路是否会发生 MarshalJSON 无限递归。
// 通过比较输出 != "" 来判定（无限递归会爆栈或卡死）。
func TestLogMarshalJSONNoUserIdRecursion(t *testing.T) {
	log := Log{Id: 1, UserId: 12345}
	data, err := common.Marshal(log)
	require.NoError(t, err)
	require.True(t, len(data) > 0)
	require.False(t, strings.Contains(string(data), `"user_id":12345,`),
		"内嵌 alias 不能让原始 int 也输出一遍")
}
