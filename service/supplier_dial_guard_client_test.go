package service

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 供应商渠道客户端：连接 127.0.0.1（httptest）必须在拨号阶段被拒；官方渠道同一地址正常访问，且两者不共用缓存客户端。
func TestSupplierGuardedClientBlocksLoopbackOfficialUnaffected(t *testing.T) {
	InitHttpClient()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	officialClient, err := GetHttpClientWithProxySettings("", dto.ChannelSettings{})
	require.NoError(t, err)
	supplierClient, err := GetHttpClientWithProxySettings(common.WithSupplierDialGuard(""), dto.ChannelSettings{})
	require.NoError(t, err)
	require.NotSame(t, officialClient, supplierClient)

	resp, err := officialClient.Get(server.URL)
	require.NoError(t, err)
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	_, err = supplierClient.Get(server.URL)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "供应商渠道禁止访问内网地址: 127.0.0.1")

	// 走 HTTP 代理时检查的是代理地址（这里代理在 127.0.0.1，同样被拒）
	proxied, err := GetHttpClientWithProxy(common.WithSupplierDialGuard(server.URL))
	require.NoError(t, err)
	_, err = proxied.Get("http://example.com/")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "供应商渠道禁止访问内网地址: 127.0.0.1")

	// 官方渠道辅助请求保持原 fallback 客户端
	aux, err := GetChannelAuxHttpClient("", officialClient)
	require.NoError(t, err)
	assert.Same(t, officialClient, aux)
}
