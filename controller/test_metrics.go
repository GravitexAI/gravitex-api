package controller

import (
	"net/http"
	"runtime"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

// TestMetricsResponse 压测监控指标（轻量级，无需认证）
type TestMetricsResponse struct {
	CPUUsage     float64 `json:"cpu_usage"`
	MemUsage     float64 `json:"mem_usage"`
	HeapAllocMB  float64 `json:"heap_alloc_mb"`
	HeapSysMB    float64 `json:"heap_sys_mb"`
	NumGoroutine int     `json:"num_goroutine"`
	NumGC        uint32  `json:"num_gc"`
}

// GetTestMetrics 获取压测监控指标
func GetTestMetrics(c *gin.Context) {
	systemStatus := common.GetSystemStatus()

	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	c.JSON(http.StatusOK, TestMetricsResponse{
		CPUUsage:     systemStatus.CPUUsage,
		MemUsage:     systemStatus.MemoryUsage,
		HeapAllocMB:  float64(memStats.HeapAlloc) / 1024 / 1024,
		HeapSysMB:    float64(memStats.HeapSys) / 1024 / 1024,
		NumGoroutine: runtime.NumGoroutine(),
		NumGC:        memStats.NumGC,
	})
}
