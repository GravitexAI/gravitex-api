package service

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
)

// PriceChain 价格链条结果
type PriceChain struct {
	OemId          *int64  // OEM ID
	OemCode        string  // OEM代码（用于向后兼容和日志显示）
	OfficialQuota  int64   // 官方价格quota（基于ModelRatio）
	CostDiscount   float64 // 成本折扣率
	CostQuota      int64   // 成本价quota
	SystemDiscount float64 // 系统销售折扣率
	SystemQuota    int64   // 系统销售价quota
	PlatformProfit int64   // 平台利润quota
	UserDiscount   float64 // 用户折扣率（GroupRatio）
	UserQuota      int64   // 用户支付价quota
	OemSubsidy     int64   // OEM补贴quota（负数表示补贴，正数表示盈利）
	VendorId       *int64  // 厂商ID（用于日志 other 持久化与账单导出）
}

// CalculatePriceChain 计算完整价格链条
// 价格链条：官方价格 → 平台成本 → 系统销售价 → 用户支付价
// 优先级：用户 oem（GetUserCache/TokenAuth）> 请求头 X-Oem-Code > 默认 gravitex
// 注意：用户已认证时优先使用用户的oem_id，未认证或oem_id为空时使用请求头
// userQuota 参数是实际扣费的 quota，用于确保价格链中的 user_quota 与实际扣费一致
//
// 重要：这个函数基于实际 quota 反推官方价格
func CalculatePriceChain(c *gin.Context, modelName string, vendorName string, tokens int, userQuota int) *PriceChain {
	chain := &PriceChain{}

	// 1. 优先从Context获取OEM信息
	var oemId *int64
	var oemCode string

	if c != nil {
		// 优先获取OEM ID（可能来自用户oem_id或请求头）
		if id, exists := c.Get(string(constant.ContextKeyOemId)); exists {
			if idInt64, ok := id.(int64); ok {
				oemId = &idInt64
			}
		}
		// 获取OEM Code（用于显示和fallback）
		if code, exists := c.Get(string(constant.ContextKeyOemCode)); exists {
			if codeStr, ok := code.(string); ok && codeStr != "" {
				oemCode = codeStr
			}
		}
		// 如果只有oemCode没有oemId，通过oemCode查询oemId
		if oemId == nil && oemCode != "" {
			oemConfig := model.GetOemConfigByCode(oemCode)
			if oemConfig != nil {
				oemId = &oemConfig.Id
			}
		}
	}

	if oemCode == "" {
		oemCode = "gravitex"
	}
	if oemId == nil {
		oemConfig := model.GetOemConfigByCode("gravitex")
		if oemConfig != nil {
			oemId = &oemConfig.Id
		}
	}

	chain.OemCode = oemCode
	chain.OemId = oemId

	// 2. 获取各种折扣率
	// oem_user_discount: OEM给用户的折扣
	var oemUserDiscount float64
	if oemId != nil {
		oemUserDiscount = model.GetOemUserDiscount(*oemId, modelName, vendorName)
	} else {
		oemUserDiscount = model.GetOemUserDiscountByCode(oemCode, modelName, vendorName)
	}
	if oemUserDiscount <= 0 {
		oemUserDiscount = 1.0
	}

	// oem_discount: 平台给OEM的折扣（OEM的成本）
	var oemDiscount float64
	if oemId != nil {
		oemDiscount = model.GetOemDiscount(*oemId, modelName, vendorName)
	} else {
		oemDiscount = model.GetOemDiscountByCode(oemCode, modelName, vendorName)
	}
	if oemDiscount <= 0 {
		oemDiscount = 1.0
	}

	// platform_cost: 平台成本折扣
	costDiscount := model.GetPlatformCostDiscount(modelName, vendorName)
	if costDiscount <= 0 {
		costDiscount = 1.0
	}
	chain.CostDiscount = costDiscount

	// group_ratio: 用户分组倍率
	groupRatio := GetGroupRatioByOemFromContext(c, "default")
	if c != nil {
		if group, exists := c.Get("group"); exists {
			if groupStr, ok := group.(string); ok && groupStr != "" {
				groupRatio = GetGroupRatioByOemFromContext(c, groupStr)
			}
		}
	}
	if groupRatio <= 0 {
		groupRatio = 1.0
	}

	// 3. 基于实际 quota 反推官方价格
	// 实际扣费公式：user_quota = official_quota × oem_user_discount × group_ratio
	// 反推：official_quota = user_quota / (oem_user_discount × group_ratio)
	userQuotaValue := int64(userQuota)
	var officialQuota int64
	if oemUserDiscount > 0 && groupRatio > 0 {
		officialQuota = int64(float64(userQuotaValue) / (oemUserDiscount * groupRatio))
	} else {
		officialQuota = userQuotaValue
	}
	chain.OfficialQuota = officialQuota

	// 4. 计算平台成本价
	costQuota := int64(float64(officialQuota) * costDiscount)
	chain.CostQuota = costQuota

	// 5. 计算系统销售价（OEM的成本）
	systemQuota := int64(float64(officialQuota) * oemDiscount)
	chain.SystemDiscount = oemDiscount
	chain.SystemQuota = systemQuota

	// 6. 计算平台利润
	chain.PlatformProfit = systemQuota - costQuota

	// 7. 用户支付价
	chain.UserDiscount = oemUserDiscount
	chain.UserQuota = userQuotaValue

	// 8. 计算OEM盈亏（正数表示盈利，负数表示亏损/补贴）
	// OEM盈亏 = 用户支付价 - OEM成本（系统销售价）
	chain.OemSubsidy = userQuotaValue - systemQuota

	return chain
}

// GetGroupRatioByOem 根据OEM代码获取GroupRatio
// 优先级：OEM特定GroupRatio > 全局GroupRatio > 默认值(1.0)
func GetGroupRatioByOem(oemCode, group string) float64 {
	// 1. 尝试获取OEM特定的GroupRatio（如GroupRatio_gravitex、GroupRatio_xiaomai）
	systemGroupRatioKey := fmt.Sprintf("GroupRatio_%s", oemCode)
	option, err := model.GetOption(systemGroupRatioKey)
	if err == nil && option != nil && option.Value != "" {
		// 解析JSON格式的GroupRatio配置
		groupRatioMap := make(map[string]float64)
		if err := common.Unmarshal([]byte(option.Value), &groupRatioMap); err == nil {
			if ratio, ok := groupRatioMap[group]; ok {
				return ratio
			}
			// 如果没有找到特定group，尝试default
			if ratio, ok := groupRatioMap["default"]; ok {
				return ratio
			}
		}
	}

	// 2. 使用全局GroupRatio
	return ratio_setting.GetGroupRatio(group)
}

// GetGroupRatioByOemFromContext 从Context中获取OEM代码，然后获取对应的GroupRatio
// 用于实际扣费时获取OEM特定的GroupRatio
func GetGroupRatioByOemFromContext(c *gin.Context, group string) float64 {
	oemCode := "gravitex" // 默认系统
	if c != nil {
		if code, exists := c.Get(string(constant.ContextKeyOemCode)); exists {
			if codeStr, ok := code.(string); ok && codeStr != "" {
				oemCode = codeStr
			}
		}
	}
	return GetGroupRatioByOem(oemCode, group)
}

// GetVendorNameFromModel 从模型名称获取厂商名称（委托 model 包）
func GetVendorNameFromModel(modelName string) string {
	return model.GetVendorNameFromModel(modelName)
}

// GetVendorIdFromModel 从模型名称获取厂商 ID（委托 model 包）
func GetVendorIdFromModel(modelName string) *int64 {
	return model.GetVendorIdFromModel(modelName)
}

// CalculatePriceChainForLog 为日志记录计算价格链条（与 Nebula 一致）
// 基于实际扣费 quota 反推官方价，再算成本价、系统销售价、平台利润、OEM 盈亏，供日志表与 Expenses 展示
func CalculatePriceChainForLog(c *gin.Context, modelName string, promptTokens, completionTokens, quota int) *model.PriceChainParams {
	vendorName := GetVendorNameFromModel(modelName)
	vendorId := GetVendorIdFromModel(modelName)
	chain := CalculatePriceChain(c, modelName, vendorName, promptTokens+completionTokens, quota)
	if chain == nil {
		return nil
	}
	return &model.PriceChainParams{
		OemId:          chain.OemId,
		OemCode:        chain.OemCode,
		OfficialQuota:  chain.OfficialQuota,
		CostQuota:      chain.CostQuota,
		SystemQuota:    chain.SystemQuota,
		UserQuota:      chain.UserQuota,
		PlatformProfit: chain.PlatformProfit,
		OemSubsidy:     chain.OemSubsidy,
		VendorId:       vendorId,
	}
}

// CalculatePriceChainForLogFromParams 在无 gin.Context 的场景（如视频轮询计费）下计算价格链条
// 与 CalculatePriceChainForLog 公式一致，供 handleSora2TaskBilling 等写入日志价格链字段
func CalculatePriceChainForLogFromParams(oemCode, modelName string, userQuota int, oemUserDiscount, groupRatio float64) *model.PriceChainParams {
	if oemCode == "" {
		oemCode = "gravitex"
	}
	if oemUserDiscount <= 0 {
		oemUserDiscount = 1.0
	}
	if groupRatio <= 0 {
		groupRatio = 1.0
	}
	vendorName := GetVendorNameFromModel(modelName)
	vendorId := GetVendorIdFromModel(modelName)
	oemConfig := model.GetOemConfigByCode(oemCode)
	var oemId *int64
	if oemConfig != nil {
		oemId = &oemConfig.Id
	}
	oemDiscount := model.GetOemDiscountByCode(oemCode, modelName, vendorName)
	if oemDiscount <= 0 {
		oemDiscount = 1.0
	}
	costDiscount := model.GetPlatformCostDiscount(modelName, vendorName)
	if costDiscount <= 0 {
		costDiscount = 1.0
	}
	userQuotaVal := int64(userQuota)
	officialQuota := int64(float64(userQuotaVal) / (oemUserDiscount * groupRatio))
	costQuota := int64(float64(officialQuota) * costDiscount)
	systemQuota := int64(float64(officialQuota) * oemDiscount)
	platformProfit := systemQuota - costQuota
	oemSubsidy := userQuotaVal - systemQuota
	return &model.PriceChainParams{
		OemId:          oemId,
		OemCode:        oemCode,
		OfficialQuota:  officialQuota,
		CostQuota:      costQuota,
		SystemQuota:    systemQuota,
		UserQuota:      userQuotaVal,
		PlatformProfit: platformProfit,
		OemSubsidy:     oemSubsidy,
		VendorId:       vendorId,
	}
}

// GetOemUserDiscountForQuota 获取用于quota计算的OEM用户折扣
// 用于在实际扣费时应用OEM给用户的折扣
// 优先级：扣费用户的 oemId（Context 由鉴权注入）> 请求头 X-Oem-Code > 默认 gravitex
func GetOemUserDiscountForQuota(c *gin.Context, modelName string) float64 {
	var oemId *int64
	var oemCode string

	if c != nil {
		// 优先使用扣费用户的 oemId（鉴权时已写入 Context）
		if id, exists := c.Get(string(constant.ContextKeyOemId)); exists {
			if idInt64, ok := id.(int64); ok {
				oemId = &idInt64
			}
		}
		if oemId == nil {
			if code, exists := c.Get(string(constant.ContextKeyOemCode)); exists {
				if codeStr, ok := code.(string); ok && codeStr != "" {
					oemCode = codeStr
				}
			}
		}
	}

	if oemCode == "" {
		oemCode = "gravitex"
	}
	if oemId == nil {
		oemConfig := model.GetOemConfigByCode(oemCode)
		if oemConfig != nil {
			oemId = &oemConfig.Id
		}
	}

	if oemId == nil {
		if common.DebugEnabled {
			common.SysLog("[GetOemUserDiscountForQuota] oemId is nil, returning 1.0")
		}
		return 1.0
	}

	// 与价格链一致：按 模型级 > 厂商级 > 通配符 匹配，避免扣费用通配符 1.0、日志用厂商级 0.9 导致多扣
	vendorName := GetVendorNameFromModel(modelName)
	discount := model.GetOemUserDiscount(*oemId, modelName, vendorName)
	if common.DebugEnabled {
		common.SysLog(fmt.Sprintf("[GetOemUserDiscountForQuota] oemId=%d, modelName=%s, vendorName=%s, discount=%.4f",
			*oemId, modelName, vendorName, discount))
	}
	return discount
}

// GetOemUserDiscountForUserId 在无 gin.Context 时按扣费用户 ID 获取 OEM 用户折扣（如视频轮询计费）
// 优先级：扣费用户的 oem_id > 默认 gravitex；与 GetOemUserDiscountForQuota 一致传 vendorName，保证厂商级/模型级折扣生效
func GetOemUserDiscountForUserId(userId int64, modelName string) float64 {
	vendorName := GetVendorNameFromModel(modelName)
	user, err := model.GetUserById(int(userId), false)
	if err != nil || user == nil {
		d := model.GetOemUserDiscountByCode("gravitex", modelName, vendorName)
		if d <= 0 {
			return 1.0
		}
		return d
	}
	if user.OemId != nil && *user.OemId != 0 {
		d := model.GetOemUserDiscount(*user.OemId, modelName, vendorName)
		if d > 0 {
			return d
		}
	}
	d := model.GetOemUserDiscountByCode("gravitex", modelName, vendorName)
	if d <= 0 {
		return 1.0
	}
	return d
}
