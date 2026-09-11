package service

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func bodyPrefixHashContextForTest(t *testing.T, body string) *gin.Context {
	t.Helper()
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	return ctx
}

func bodyPrefixHashForTest(t *testing.T, body string, paths string) string {
	t.Helper()
	ctx := bodyPrefixHashContextForTest(t, body)
	return extractChannelAffinityValue(ctx, operation_setting.ChannelAffinityKeySource{
		Type: "body_prefix_hash",
		Path: paths,
	})
}

// 会话指纹必须在多轮之间保持稳定：后续轮次只往 messages 尾部追加消息，
// 前缀字段不变，所以同一个会话要始终落在同一个上游渠道上。
func TestBodyPrefixHashStableAcrossConversationTurns(t *testing.T) {
	turn1 := `{"model":"claude-opus-4-7","system":"you are a helpful agent",` +
		`"messages":[{"role":"user","content":"fix the failing test"}]}`
	turn2 := `{"model":"claude-opus-4-7","system":"you are a helpful agent",` +
		`"messages":[{"role":"user","content":"fix the failing test"},` +
		`{"role":"assistant","content":"on it"},` +
		`{"role":"user","content":"also update the docs"}]}`

	first := bodyPrefixHashForTest(t, turn1, "system,messages.0")
	require.NotEmpty(t, first)
	assert.Len(t, first, bodyPrefixHashHexLen)
	assert.Equal(t, first, bodyPrefixHashForTest(t, turn2, "system,messages.0"))
}

// 不同会话必须拿到不同指纹，否则整个租户又会塌缩成一条亲和绑定。
func TestBodyPrefixHashDistinguishesConversations(t *testing.T) {
	a := bodyPrefixHashForTest(t,
		`{"system":"you are a helpful agent","messages":[{"role":"user","content":"fix the failing test"}]}`, "")
	b := bodyPrefixHashForTest(t,
		`{"system":"you are a helpful agent","messages":[{"role":"user","content":"write the release notes"}]}`, "")

	require.NotEmpty(t, a)
	require.NotEmpty(t, b)
	assert.NotEqual(t, a, b)
}

// 默认路径要同时覆盖两种体裁：Claude Messages 的 system 在顶层，
// OpenAI Chat 没有顶层 system 而是把它放在 messages.0。缺失的路径跳过即可。
func TestBodyPrefixHashCoversBothRequestShapes(t *testing.T) {
	claudeShape := `{"system":"agent rules","messages":[{"role":"user","content":"hello"}]}`
	openaiShape := `{"messages":[{"role":"system","content":"agent rules"},{"role":"user","content":"hello"}]}`

	claudeHash := bodyPrefixHashForTest(t, claudeShape, "")
	openaiHash := bodyPrefixHashForTest(t, openaiShape, "")

	require.NotEmpty(t, claudeHash, "claude messages shape must yield a fingerprint")
	require.NotEmpty(t, openaiHash, "openai chat shape must yield a fingerprint")
	assert.NotEqual(t, claudeHash, openaiHash)
}

// system 尾部的易变内容（时间戳、git 状态）不能把同一个会话的指纹逐请求打散，
// 所以每个路径只有前 bodyPrefixHashMaxBytesPerPath 字节参与哈希。
func TestBodyPrefixHashIgnoresVolatileTailBeyondLimit(t *testing.T) {
	stable := strings.Repeat("a", bodyPrefixHashMaxBytesPerPath)
	build := func(tail string) string {
		return `{"system":"` + stable + tail + `","messages":[{"role":"user","content":"hi"}]}`
	}

	withFirstTail := bodyPrefixHashForTest(t, build("10:25:01"), "system")
	withSecondTail := bodyPrefixHashForTest(t, build("11:40:59"), "system")

	require.NotEmpty(t, withFirstTail)
	assert.Equal(t, withFirstTail, withSecondTail)
}

// 一个路径都取不到时必须返回空串，让回退链继续尝试下一个 key source，
// 而不是产出一个所有请求共用的常量指纹。
func TestBodyPrefixHashReturnsEmptyWhenNoPathMatches(t *testing.T) {
	assert.Empty(t, bodyPrefixHashForTest(t, `{"model":"claude-opus-4-7"}`, ""))
	assert.Empty(t, bodyPrefixHashForTest(t, `{"system":"agent rules"}`, "prompt_cache_key"))
	assert.Empty(t, bodyPrefixHashForTest(t, ``, ""))
}
