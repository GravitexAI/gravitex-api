package middleware

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

// SystemIdentify OEM识别中间件
// OEM 优先级：1) 用户 token（GetUserCache/TokenAuth 写入） 2) 请求头 X-Oem-Code/域名/路径 3) 默认 gravitex。
// 从请求 Header、域名或 URI 提取 oem_code 并加载 OemConfig；无头时由 TokenAuth 按用户 oem_id 写入。
func SystemIdentify() gin.HandlerFunc {
	return func(c *gin.Context) {
		var oemCode string
		var oemConfig *model.OemConfig

		// 1. 优先从Nginx传递的Header获取
		oemCode = c.GetHeader("X-Oem-Code")

		// 2. 如果Header没有，从Host/Referer头提取域名查询
		if oemCode == "" {
			oemCode = extractOemCodeFromDomain(c)
		}

		// 3. 如果还是没有，从请求路径中提取
		if oemCode == "" {
			requestURI := c.Request.RequestURI
			oemCode = extractOemCodeFromPath(requestURI)
		}

		// 4. 如果还是没有，使用默认系统（优先级：TokenAuth 用户 oem > 请求头 > 默认 gravitex）
		if oemCode == "" {
			oemCode = "gravitex"
		}

		// 5. 获取OEM配置
		oemConfig = model.GetOemConfigByCode(oemCode)
		if oemConfig == nil {
			oemCode = "gravitex"
			oemConfig = model.GetOemConfigByCode(oemCode)
		}

		if oemConfig != nil && oemConfig.Enabled == 0 {
			common.SysLog("OEM已停用: oemCode=" + oemCode)
			oemCode = "gravitex"
			oemConfig = model.GetOemConfigByCode(oemCode)
		}

		// 5. 存储到Context
		common.SetContextKey(c, constant.ContextKeyOemCode, oemCode)

		if oemConfig != nil {
			oemId := oemConfig.Id
			common.SetContextKey(c, constant.ContextKeyOemId, oemId)
			common.SetContextKey(c, constant.ContextKeyOemConfig, oemConfig)
			// 向后兼容：同时设置SystemConfig（使用向后兼容函数）
			common.SetContextKey(c, constant.ContextKeySystemConfig, oemConfig)
		}

		if oemConfig != nil {
			common.SysLog(fmt.Sprintf("OEM识别成功: oemCode=%s, oemId=%d, apiPrefix=%s", oemCode, oemConfig.Id, oemConfig.ApiPrefix))
		}

		// 执行后续中间件（包括认证）
		c.Next()

		// 认证后检查用户oem_id，优先使用用户的oem_id
		if userOemId, exists := c.Get(string(constant.ContextKeyUserOemId)); exists {
			if oemIdInt64, ok := userOemId.(int64); ok && oemIdInt64 > 0 {
				// 用户有oem_id，优先使用
				userOemConfig := model.GetOemConfigById(oemIdInt64)
				if userOemConfig != nil {
					common.SetContextKey(c, constant.ContextKeyOemCode, userOemConfig.OemCode)
					common.SetContextKey(c, constant.ContextKeyOemId, userOemConfig.Id)
					common.SetContextKey(c, constant.ContextKeyOemConfig, userOemConfig)
					common.SysLog(fmt.Sprintf("使用用户oem_id: userId=%d, oemId=%d, oemCode=%s",
						c.GetInt("id"), oemIdInt64, userOemConfig.OemCode))
				}
			}
		}
	}
}

// extractOemCodeFromPath 从请求路径中提取OEM代码
func extractOemCodeFromPath(requestURI string) string {
	if requestURI == "" {
		return ""
	}

	// 移除开头的斜杠和查询参数
	path := strings.TrimPrefix(requestURI, "/")
	if idx := strings.Index(path, "?"); idx != -1 {
		path = path[:idx]
	}

	// 分割路径
	segments := strings.Split(path, "/")
	if len(segments) < 1 {
		return ""
	}

	firstSegment := segments[0]

	// 特殊处理：/prod-api 映射到默认 OEM
	if firstSegment == "prod-api" {
		return "gravitex"
	}

	// 通过API前缀查询OEM代码
	apiPrefix := "/" + firstSegment
	oemConfig := model.GetOemConfigByApiPrefix(apiPrefix)
	if oemConfig != nil {
		return oemConfig.OemCode
	}

	// 尝试通过别名查询
	oemConfig = model.GetOemConfigByApiPrefixAlias(apiPrefix)
	if oemConfig != nil {
		return oemConfig.OemCode
	}

	return ""
}

// extractOemCodeFromDomain 从请求的Host或Referer头中提取域名，并通过域名查询OEM代码
func extractOemCodeFromDomain(c *gin.Context) string {
	// 优先从Host头获取域名
	host := c.Request.Host

	// 如果Host头没有，尝试从Referer头提取域名
	if host == "" {
		referer := c.GetHeader("Referer")
		if referer != "" {
			// 简单提取域名（不依赖URL解析库）
			// 移除协议前缀
			if strings.HasPrefix(referer, "http://") {
				referer = referer[7:]
			} else if strings.HasPrefix(referer, "https://") {
				referer = referer[8:]
			}
			// 提取域名部分（到第一个/或:之前）
			if idx := strings.IndexAny(referer, "/:"); idx != -1 {
				host = referer[:idx]
			} else {
				host = referer
			}
		}
	}

	if host == "" {
		return ""
	}

	// 移除端口号（如果有）
	if idx := strings.Index(host, ":"); idx != -1 {
		host = host[:idx]
	}

	// 通过域名查询OEM配置
	oemConfig := model.GetOemConfigByDomain(host)
	if oemConfig != nil {
		common.SysLog(fmt.Sprintf("通过域名识别OEM: domain=%s, oemCode=%s", host, oemConfig.OemCode))
		return oemConfig.OemCode
	}

	return ""
}
