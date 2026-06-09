package controller

import (
	"net"
	"net/http"
	"os"
	"runtime"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

// TestMetricsResponse 压测监控指标（轻量级，无需认证）
type TestMetricsResponse struct {
	Hostname     string  `json:"hostname"`
	IP           string  `json:"ip"`
	CPUUsage     float64 `json:"cpu_usage"`
	MemUsage     float64 `json:"mem_usage"`
	HeapAllocMB  float64 `json:"heap_alloc_mb"`
	HeapSysMB    float64 `json:"heap_sys_mb"`
	NumGoroutine int     `json:"num_goroutine"`
	NumGC        uint32  `json:"num_gc"`
}

var (
	cachedHostname string
	cachedIP       string
	hostInfoOnce   sync.Once
)

func getHostInfo() (hostname, ip string) {
	hostInfoOnce.Do(func() {
		cachedHostname, _ = os.Hostname()
		cachedIP = getLocalIP()
	})
	return cachedHostname, cachedIP
}

// getLocalIP 获取本机非 loopback 的第一个 IPv4 地址
func getLocalIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, addr := range addrs {
		if ipNet, ok := addr.(*net.IPNet); ok && !ipNet.IP.IsLoopback() && ipNet.IP.To4() != nil {
			return ipNet.IP.String()
		}
	}
	return strings.Join(getAllIPs(), ", ")
}

func getAllIPs() []string {
	var ips []string
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ips
	}
	for _, addr := range addrs {
		if ipNet, ok := addr.(*net.IPNet); ok && !ipNet.IP.IsLoopback() {
			ips = append(ips, ipNet.IP.String())
		}
	}
	return ips
}

// GetTestMetrics 获取压测监控指标
func GetTestMetrics(c *gin.Context) {
	systemStatus := common.GetSystemStatus()

	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	hostname, ip := getHostInfo()

	c.JSON(http.StatusOK, TestMetricsResponse{
		Hostname:     hostname,
		IP:           ip,
		CPUUsage:     systemStatus.CPUUsage,
		MemUsage:     systemStatus.MemoryUsage,
		HeapAllocMB:  float64(memStats.HeapAlloc) / 1024 / 1024,
		HeapSysMB:    float64(memStats.HeapSys) / 1024 / 1024,
		NumGoroutine: runtime.NumGoroutine(),
		NumGC:        memStats.NumGC,
	})
}
