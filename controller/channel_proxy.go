package controller

import (
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

// validateChannelProxyInPool enforces that a fixed proxy chosen from the admin side
// exists in supplier_channel_proxy (any non-deleted row, public or internal).
// An unchanged value (originProxy) is grandfathered so legacy channels stay editable.
func validateChannelProxyInPool(setting dto.ChannelSettings, originProxy string) error {
	proxy := strings.TrimSpace(setting.Proxy)
	if proxy == "" || proxy == dto.ProxyRandom || proxy == strings.TrimSpace(originProxy) {
		return nil
	}
	inPool, err := model.IsProxyInSupplierPool(proxy)
	if err != nil {
		return fmt.Errorf("cannot verify channel proxy against the proxy list: %w", err)
	}
	if !inPool {
		return fmt.Errorf("channel proxy must be selected from the proxy list")
	}
	return nil
}

// GetChannelProxyOptions returns the whole proxy list (public and internal) plus its
// distinct regions, for the fixed-proxy dropdown and the random-mode region filter.
func GetChannelProxyOptions(c *gin.Context) {
	items, err := model.GetSupplierChannelProxies()
	if err != nil && len(items) == 0 {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	regionSet := make(map[string]struct{})
	for _, item := range items {
		if item.Region != "" {
			regionSet[item.Region] = struct{}{}
		}
	}
	regions := make([]string, 0, len(regionSet))
	for region := range regionSet {
		regions = append(regions, region)
	}
	sort.Strings(regions)
	if items == nil {
		items = []model.SupplierChannelProxy{}
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"items":   items,
			"regions": regions,
		},
	})
}

type testChannelProxyRequest struct {
	Proxy string `json:"proxy"`
}

// TestChannelProxy validates a proxy URL and measures one request through it.
func TestChannelProxy(c *gin.Context) {
	var req testChannelProxyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "Invalid request"})
		return
	}
	latency, err := service.ProbeProxy(c.Request.Context(), req.Proxy)
	result := gin.H{"ok": err == nil, "latency_ms": latency.Milliseconds(), "error": ""}
	if err != nil {
		result["error"] = err.Error()
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": result})
}
