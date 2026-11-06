package common

import (
	"fmt"
	"net"
	"strings"
	"sync"
	"syscall"
)

// 供应商渠道拨号防护（防 DNS rebinding 打内网）。
//
// Java 只在保存/测试时校验供应商 base_url 解析出的 IP，Go 真正发请求时会重新解析 DNS，
// 因此必须在建立 TCP 连接的那一刻检查"实际连接的 IP"。仅对供应商渠道生效，官方渠道不受影响。
// 配置了代理时实际连接的是代理地址，检查的也是代理地址（预期行为）；
// 内网预设代理可通过 SUPPLIER_CHANNEL_ALLOW_CIDRS 放行。

// supplierDialGuardProxyPrefix 是供应商渠道"拨号代理键"的前缀标记。
// 通过 WithSupplierDialGuard 加在渠道 proxy 字符串前，由 HTTP 客户端选择逻辑剥离并改用受保护客户端。
// 该前缀只在运行期内存中流转，绝不能写回数据库。
const supplierDialGuardProxyPrefix = "supplier-dial-guard:"

// WithSupplierDialGuard 为供应商渠道的 proxy 加上拨号防护标记（proxy 可为空，表示直连）。
func WithSupplierDialGuard(proxy string) string {
	if strings.HasPrefix(proxy, supplierDialGuardProxyPrefix) {
		return proxy
	}
	return supplierDialGuardProxyPrefix + proxy
}

// SplitSupplierDialGuard 剥离拨号防护标记，返回原始 proxy 以及是否需要启用供应商拨号防护。
func SplitSupplierDialGuard(raw string) (proxy string, guarded bool) {
	if strings.HasPrefix(raw, supplierDialGuardProxyPrefix) {
		return strings.TrimPrefix(raw, supplierDialGuardProxyPrefix), true
	}
	return raw, false
}

// supplierDialAllowNets 解析环境变量 SUPPLIER_CHANNEL_ALLOW_CIDRS（逗号分隔 CIDR，默认空），
// 命中的网段即使是内网也放行（用于内网预设代理）。非法 CIDR 记录错误并忽略。
var supplierDialAllowNets = sync.OnceValue(func() []*net.IPNet {
	return ParseSupplierDialAllowCIDRs(GetEnvOrDefaultString("SUPPLIER_CHANNEL_ALLOW_CIDRS", ""))
})

// ParseSupplierDialAllowCIDRs 解析逗号分隔的 CIDR 列表；单个 IP 视为 /32 或 /128。
func ParseSupplierDialAllowCIDRs(raw string) []*net.IPNet {
	var nets []*net.IPNet
	for _, item := range strings.Split(raw, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if !strings.Contains(item, "/") {
			if ip := net.ParseIP(item); ip != nil {
				if ip.To4() != nil {
					item += "/32"
				} else {
					item += "/128"
				}
			}
		}
		_, ipNet, err := net.ParseCIDR(item)
		if err != nil {
			SysError(fmt.Sprintf("SUPPLIER_CHANNEL_ALLOW_CIDRS 中的网段非法，已忽略: %s", item))
			continue
		}
		nets = append(nets, ipNet)
	}
	return nets
}

// NewSupplierDialControl 返回用于 net.Dialer.Control 的校验函数：
// address 为实际拨号的 ip:port（DNS 已解析完毕），命中内网/保留地址且不在放行网段内时拒绝连接。
func NewSupplierDialControl(allowNets []*net.IPNet) func(network, address string, c syscall.RawConn) error {
	return func(network, address string, _ syscall.RawConn) error {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			host = address
		}
		ip := net.ParseIP(strings.Trim(host, "[]"))
		if ip == nil {
			return fmt.Errorf("供应商渠道禁止访问内网地址: %s", host)
		}
		// IPv4-mapped IPv6（::ffff:a.b.c.d）统一按 IPv4 判断
		if v4 := ip.To4(); v4 != nil {
			ip = v4
		}
		for _, allowNet := range allowNets {
			if allowNet.Contains(ip) {
				return nil
			}
		}
		if isPrivateIP(ip) {
			return fmt.Errorf("供应商渠道禁止访问内网地址: %s", ip.String())
		}
		return nil
	}
}

// SupplierDialControl 是使用环境变量放行网段的供应商渠道拨号校验函数。
func SupplierDialControl(network, address string, c syscall.RawConn) error {
	return NewSupplierDialControl(supplierDialAllowNets())(network, address, c)
}
