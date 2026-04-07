package service

import (
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

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
