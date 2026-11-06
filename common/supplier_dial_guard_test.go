package common

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSupplierDialControl_BlocksPrivateAndAllowsPublic(t *testing.T) {
	control := NewSupplierDialControl(nil)
	cases := []struct {
		address string
		blocked bool
	}{
		{"10.201.0.5:443", true},
		{"100.100.100.200:80", true}, // 阿里云元数据
		{"127.0.0.1:8080", true},
		{"169.254.169.254:80", true},
		{"172.16.3.4:443", true},
		{"192.168.1.1:443", true},
		{"0.0.0.0:80", true},
		{"[::1]:443", true},
		{"[fc00::1]:443", true},
		{"[fe80::1]:443", true},
		{"[::ffff:10.0.0.1]:443", true}, // IPv4-mapped IPv6
		{"8.8.8.8:443", false},
		{"[2001:4860:4860::8888]:443", false},
		{"[::ffff:8.8.8.8]:443", false},
	}
	for _, tc := range cases {
		err := control("tcp", tc.address, nil)
		if tc.blocked {
			require.Error(t, err, tc.address)
			assert.Contains(t, err.Error(), "供应商渠道禁止访问内网地址")
		} else {
			assert.NoError(t, err, tc.address)
		}
	}
}

func TestSupplierDialControl_AllowList(t *testing.T) {
	allow := ParseSupplierDialAllowCIDRs(" 10.201.0.0/16, 192.168.5.9 ,bad-cidr")
	require.Len(t, allow, 2)
	control := NewSupplierDialControl(allow)

	assert.NoError(t, control("tcp", "10.201.3.4:3128", nil))
	assert.NoError(t, control("tcp", "192.168.5.9:1080", nil))
	assert.NoError(t, control("tcp", "[::ffff:10.201.0.1]:3128", nil))
	assert.Error(t, control("tcp", "10.202.0.1:3128", nil))
	assert.Error(t, control("tcp", "100.100.100.200:80", nil))
}

func TestSupplierDialGuardProxyMarker(t *testing.T) {
	marked := WithSupplierDialGuard("http://proxy.example:8080")
	proxy, guarded := SplitSupplierDialGuard(marked)
	assert.True(t, guarded)
	assert.Equal(t, "http://proxy.example:8080", proxy)
	// 重复标记幂等
	assert.Equal(t, marked, WithSupplierDialGuard(marked))

	proxy, guarded = SplitSupplierDialGuard(WithSupplierDialGuard(""))
	assert.True(t, guarded)
	assert.Equal(t, "", proxy)

	proxy, guarded = SplitSupplierDialGuard("socks5://p:1080")
	assert.False(t, guarded)
	assert.Equal(t, "socks5://p:1080", proxy)
}
