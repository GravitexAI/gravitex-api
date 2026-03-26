package service

import (
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
)

// GetVendorNameFromModel 从模型名称获取厂商名称（委托 model 包）
func GetVendorNameFromModel(modelName string) string {
	return model.GetVendorNameFromModel(modelName)
}

// GetVendorIdFromModel 从模型名称获取厂商 ID（委托 model 包）
func GetVendorIdFromModel(modelName string) *int64 {
	return model.GetVendorIdFromModel(modelName)
}

// CalculatePriceChainForLog 为日志记录计算价格链条
// 仅记录 VendorId 供日志表与 Expenses 展示
func CalculatePriceChainForLog(c *gin.Context, modelName string, promptTokens, completionTokens, quota int) *model.PriceChainParams {
	vendorId := GetVendorIdFromModel(modelName)
	return &model.PriceChainParams{
		VendorId: vendorId,
	}
}

// CalculatePriceChainForLogFromParams 在无 gin.Context 的场景（如视频轮询计费）下计算价格链条
func CalculatePriceChainForLogFromParams(modelName string, userQuota int, groupRatio float64) *model.PriceChainParams {
	vendorId := GetVendorIdFromModel(modelName)
	return &model.PriceChainParams{
		VendorId: vendorId,
	}
}

// GetGroupRatioFromContext 从 Context 中获取分组倍率
func GetGroupRatioFromContext(c *gin.Context, group string) float64 {
	return ratio_setting.GetGroupRatio(group)
}
