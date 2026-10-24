package model

import (
	"errors"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setSupplierProxyPoolForTest(t *testing.T, items []SupplierChannelProxy, loadErr error) {
	t.Helper()
	previous := supplierProxyPool.Load()
	supplierProxyPool.Store(&supplierProxyPoolSnapshot{items: items, err: loadErr, loadedAt: time.Now().Add(time.Hour)})
	t.Cleanup(func() { supplierProxyPool.Store(previous) })
}

func setProxyRotationNowForTest(t *testing.T, unix int64) {
	t.Helper()
	previous := proxyRotationNow
	proxyRotationNow = func() time.Time { return time.Unix(unix, 0) }
	t.Cleanup(func() { proxyRotationNow = previous })
}

var testSupplierProxies = []SupplierChannelProxy{
	{Id: 1, Proxy: "socks5://u:p@10.0.0.1:1080", Region: "us-west", PublicFlag: 1},
	{Id: 2, Proxy: "http://10.0.0.2:8080", Region: "us-west", PublicFlag: 1},
	{Id: 3, Proxy: "http://10.0.0.3:8080", Region: "ap-tokyo", PublicFlag: 1},
	{Id: 4, Proxy: "http://10.0.0.4:8080", Region: "ap-tokyo", PublicFlag: 0},
	{Id: 5, Proxy: "not a proxy", Region: "us-west", PublicFlag: 1},
}

func TestResolveChannelProxyKeepsFixedAndEmpty(t *testing.T) {
	setSupplierProxyPoolForTest(t, testSupplierProxies, nil)

	assert.Equal(t, "", ResolveChannelProxy(1, dto.ChannelSettings{}))
	assert.Equal(t, "http://1.2.3.4:8080", ResolveChannelProxy(1, dto.ChannelSettings{Proxy: "http://1.2.3.4:8080"}))
}

func TestResolveChannelProxyRandomFiltersPool(t *testing.T) {
	setSupplierProxyPoolForTest(t, testSupplierProxies, nil)

	tests := []struct {
		name    string
		setting dto.ChannelSettings
		allowed []string
	}{
		{
			name:    "default scope is public only and skips invalid URLs",
			setting: dto.ChannelSettings{Proxy: dto.ProxyRandom},
			allowed: []string{"socks5://u:p@10.0.0.1:1080", "http://10.0.0.2:8080", "http://10.0.0.3:8080"},
		},
		{
			name:    "all scope includes internal proxies",
			setting: dto.ChannelSettings{Proxy: dto.ProxyRandom, ProxyRandomScope: dto.ProxyRandomScopeAll, ProxyRandomRegions: []string{"ap-tokyo"}},
			allowed: []string{"http://10.0.0.3:8080", "http://10.0.0.4:8080"},
		},
		{
			name:    "region filter",
			setting: dto.ChannelSettings{Proxy: dto.ProxyRandom, ProxyRandomRegions: []string{"ap-tokyo"}},
			allowed: []string{"http://10.0.0.3:8080"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			seen := map[string]bool{}
			for slice := int64(0); slice < 20; slice++ {
				setProxyRotationNowForTest(t, slice*proxyRandomRotateSeconds)
				seen[ResolveChannelProxy(7, tt.setting)] = true
			}
			assert.ElementsMatch(t, tt.allowed, mapKeys(seen))
		})
	}
}

func TestResolveChannelProxyStableWithinSliceAndRotatesBetweenSlices(t *testing.T) {
	setSupplierProxyPoolForTest(t, testSupplierProxies, nil)
	setting := dto.ChannelSettings{Proxy: dto.ProxyRandom, ProxyRandomRegions: []string{"us-west"}}
	base := int64(1000) * proxyRandomRotateSeconds

	setProxyRotationNowForTest(t, base)
	first := ResolveChannelProxy(42, setting)
	setProxyRotationNowForTest(t, base+proxyRandomRotateSeconds-1)
	assert.Equal(t, first, ResolveChannelProxy(42, setting))
	setProxyRotationNowForTest(t, base+proxyRandomRotateSeconds)
	assert.NotEqual(t, first, ResolveChannelProxy(42, setting))
}

func TestResolveChannelProxyFallsBackToDirect(t *testing.T) {
	setSupplierProxyPoolForTest(t, testSupplierProxies, nil)
	assert.Equal(t, "", ResolveChannelProxy(1, dto.ChannelSettings{Proxy: dto.ProxyRandom, ProxyRandomRegions: []string{"eu-paris"}}))

	setSupplierProxyPoolForTest(t, nil, errors.New("table missing"))
	assert.Equal(t, "", ResolveChannelProxy(1, dto.ChannelSettings{Proxy: dto.ProxyRandom}))
}

func TestIsProxyInSupplierPool(t *testing.T) {
	setSupplierProxyPoolForTest(t, testSupplierProxies, nil)
	inPool, err := IsProxyInSupplierPool(" http://10.0.0.4:8080 ")
	require.NoError(t, err)
	assert.True(t, inPool, "internal proxies are selectable on the admin side")
	inPool, err = IsProxyInSupplierPool("http://9.9.9.9:8080")
	require.NoError(t, err)
	assert.False(t, inPool)

	setSupplierProxyPoolForTest(t, testSupplierProxies, errors.New("db down"))
	_, err = IsProxyInSupplierPool("http://10.0.0.4:8080")
	require.Error(t, err)
}

func TestValidateSettingsAcceptsRandomProxy(t *testing.T) {
	tests := []struct {
		setting string
		wantErr bool
	}{
		{setting: `{"proxy":"random"}`},
		{setting: `{"proxy":"random","proxy_random_scope":"all","proxy_random_regions":["us-west"]}`},
		{setting: `{"proxy":"random","proxy_random_scope":"internal"}`, wantErr: true},
		{setting: `{"proxy":"randomx"}`, wantErr: true},
	}
	for _, tt := range tests {
		setting := tt.setting
		err := (&Channel{Setting: &setting}).ValidateSettings()
		if tt.wantErr {
			assert.Error(t, err, tt.setting)
		} else {
			assert.NoError(t, err, tt.setting)
		}
	}
}

func mapKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
