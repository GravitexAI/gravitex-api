package model

// GetPlatformCostDiscount 获取平台成本折扣（OEM 版本暂未启用平台成本表）
// 这里返回 1.0，表示不区分平台成本，后续如果需要可以接入与 gravitex-api 相同的 platform_cost 表。
func GetPlatformCostDiscount(modelName, vendorName string) float64 {
	return 1.0
}
