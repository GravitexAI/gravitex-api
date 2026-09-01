package service

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestChannelAffinityRuleKeyAffinityDefaultsDisabled(t *testing.T) {
	var rule operation_setting.ChannelAffinityRule
	require.NoError(t, json.Unmarshal([]byte(`{"name":"legacy"}`), &rule))
	require.False(t, rule.KeyAffinityEnabled)

	require.NoError(t, json.Unmarshal([]byte(`{"key_affinity_enabled":true}`), &rule))
	require.True(t, rule.KeyAffinityEnabled)
}

func TestChannelAffinityKeyHash_NormalStringTrimsOnlyOuterWhitespace(t *testing.T) {
	got := channelAffinityKeyHash("  api-key-123  ")
	want := channelAffinityKeyHash("api-key-123")
	require.NotEmpty(t, got)
	require.Equal(t, want, got)
	require.Len(t, got, 64)
}

func TestChannelAffinityKeyHash_VertexCredentialIsStableAcrossJSONFormatting(t *testing.T) {
	first := `{"project_id":"project-a","client_email":"vertex@example.com","private_key_id":"key-1","private_key":"PRIVATE"}`
	second := "{\n  \"private_key\": \"PRIVATE\",\n  \"private_key_id\": \"key-1\",\n  \"client_email\": \"vertex@example.com\",\n  \"project_id\": \"project-a\"\n}"

	firstHash, err := channelAffinityCredentialHash(first)
	require.NoError(t, err)
	secondHash, err := channelAffinityCredentialHash(second)
	require.NoError(t, err)
	require.Equal(t, firstHash, secondHash)
}

func TestDecodeChannelAffinityBinding_AcceptsLegacyChannelID(t *testing.T) {
	binding, legacy, err := decodeChannelAffinityBinding("9528")
	require.NoError(t, err)
	require.True(t, legacy)
	require.Equal(t, ChannelAffinityBinding{ChannelID: 9528}, binding)
}

func TestDecodeChannelAffinityBinding_AcceptsJSONBinding(t *testing.T) {
	raw, err := json.Marshal(ChannelAffinityBinding{ChannelID: 9528, KeyIndex: 2, KeyHash: "abc"})
	require.NoError(t, err)

	binding, legacy, err := decodeChannelAffinityBinding(string(raw))
	require.NoError(t, err)
	require.False(t, legacy)
	require.Equal(t, ChannelAffinityBinding{ChannelID: 9528, KeyIndex: 2, KeyHash: "abc"}, binding)
}

func TestRecordChannelAffinity_PersistsSelectedKeyIdentity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"gpt-5"}`))
	cacheKey := "binding-write-test"
	setChannelAffinityContext(ctx, channelAffinityMeta{
		CacheKey:           channelAffinityCacheNamespace + ":" + cacheKey,
		TTLSeconds:         60,
		RuleName:           "binding-write",
		KeyAffinityEnabled: true,
	})
	ctx.Set(string(constant.ContextKeyChannelKey), "vertex-key-b")
	ctx.Set(string(constant.ContextKeyChannelMultiKeyIndex), 1)

	cache := getChannelAffinityCache()
	_, _ = cache.DeleteMany([]string{cacheKey, channelAffinityCacheNamespace + ":" + cacheKey})
	t.Cleanup(func() {
		_, _ = cache.DeleteMany([]string{cacheKey, channelAffinityCacheNamespace + ":" + cacheKey})
	})
	RecordChannelAffinity(ctx, 9528)

	raw, found, err := cache.Get(cacheKey)
	require.NoError(t, err)
	require.True(t, found)
	binding, legacy, err := decodeChannelAffinityBinding(raw)
	require.NoError(t, err)
	require.False(t, legacy)
	require.Equal(t, ChannelAffinityBinding{ChannelID: 9528, KeyIndex: 1, KeyHash: channelAffinityKeyHash("vertex-key-b")}, binding)
}
