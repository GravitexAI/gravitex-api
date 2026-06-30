package model

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLogMarshalJSONIDsAreString 验证 Log 序列化时 id 和 user_id 都是字符串，
// 避免 JS 端反序列化 Snowflake 时精度丢失：
//   - user_id 精度丢失 → 点击日志行查看用户详情失败
//   - id 精度丢失 → React Table key 冲突，展开行内容错位（"点击详情乱了"）
func TestLogMarshalJSONIDsAreString(t *testing.T) {
	log := Log{
		Id:       7892436951729840128, // ByteHouse 服务端 generateSnowflakeID() 生成的雪花 ID
		UserId:   2039533181434499136, // user.id Snowflake
		Username: "chz",
		Quota:    100,
	}

	data, err := common.Marshal(log)
	require.NoError(t, err)

	output := string(data)
	// id 必须以字符串形式输出
	assert.Containsf(t, output, `"id":"7892436951729840128"`,
		"log.id 必须是字符串以避免 React Table key 精度冲突，得到 output=%s", output)
	// user_id 必须以字符串形式输出
	assert.Containsf(t, output, `"user_id":"2039533181434499136"`,
		"user_id 必须是字符串以避免 JS 精度丢失，得到 output=%s", output)
	// 不能是 JSON number 形式
	assert.NotContains(t, output, `"id":7892436951729840128`,
		"log.id 不能是 JSON number")
	assert.NotContains(t, output, `"user_id":2039533181434499136`,
		"user_id 不能是 JSON number")

	// 其他字段保留原样
	assert.Contains(t, output, `"username":"chz"`)
	assert.Contains(t, output, `"quota":100`)
}

// TestLogMarshalJSONNoIDsRecursion 用 Alias 套路是否会发生 MarshalJSON 无限递归。
// 通过比较输出 != "" 来判定（无限递归会爆栈或卡死），同时确保 id/user_id 只输出
// 一次（字符串形式），不会因为内嵌 Alias 字段又把原始 int 也输出一遍。
func TestLogMarshalJSONNoIDsRecursion(t *testing.T) {
	log := Log{Id: 1, UserId: 12345}
	data, err := common.Marshal(log)
	require.NoError(t, err)
	require.True(t, len(data) > 0)
	output := string(data)
	require.False(t, strings.Contains(output, `"id":1,`),
		"内嵌 alias 不能让原始 id 也以 number 形式输出一遍")
	require.False(t, strings.Contains(output, `"user_id":12345,`),
		"内嵌 alias 不能让原始 user_id 也以 number 形式输出一遍")
}
