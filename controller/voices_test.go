package controller

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildVoicesUpstreamRequestUsesGenerativeLanguageForAPIKeyChannels(t *testing.T) {
	baseURL := ""
	channel := &model.Channel{Id: 1, Key: "AIza-test-key", Type: 41, BaseURL: &baseURL}

	url, header, err := buildVoicesUpstreamRequest(channel)
	require.NoError(t, err)
	assert.Equal(t, "https://generativelanguage.googleapis.com/v1beta/voices", url)
	assert.Equal(t, "AIza-test-key", header.Get("x-goog-api-key"))
}

func TestBuildVoicesUpstreamRequestPreservesProxyBaseURL(t *testing.T) {
	baseURL := "https://my-proxy.example.com"
	channel := &model.Channel{Id: 2, Key: "AIza-test-key", Type: 41, BaseURL: &baseURL}

	url, _, err := buildVoicesUpstreamRequest(channel)
	require.NoError(t, err)
	assert.Equal(t, "https://my-proxy.example.com/v1beta/voices", url)
}

func TestBuildVoicesUpstreamRequestNeverSendsSAKeyToDeveloperHost(t *testing.T) {
	// SA 渠道即使误配了 generativelanguage base，也必须改走 aiplatform
	// 项目地址；此用例只验证 URL 规划，令牌交换不在单测中触发——用
	// 无效凭据让交换失败，断言错误发生在凭据阶段而非 URL 阶段。
	saKey := `{"project_id":"proj-1","private_key":"not-a-valid-key"}`
	baseURL := "https://generativelanguage.googleapis.com"
	channel := &model.Channel{Id: 3, Key: saKey, Type: 41, BaseURL: &baseURL}

	_, _, err := buildVoicesUpstreamRequest(channel)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "acquire vertex access token")
}

func TestFirstChannelKeyReturnsFirstNonEmptyLine(t *testing.T) {
	assert.Equal(t, "k1", firstChannelKey("k1\nk2\nk3"))
	assert.Equal(t, "k2", firstChannelKey("\n  \nk2\nk3"))
	assert.Equal(t, "", firstChannelKey("\n \n"))
}
