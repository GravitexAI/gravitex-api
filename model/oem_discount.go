package model

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
)

type OemDiscount struct {
	Id             int64     `json:"id" gorm:"primaryKey"`
	OemId          int64     `json:"oem_id" gorm:"column:oem_id;index"`
	VendorID       *int64    `json:"vendor_id" gorm:"column:vendor_id;index"` // NULL表示通配符
	ModelName      *string   `json:"model_name" gorm:"column:model_name;type:varchar(128)"`
	SystemDiscount float64   `json:"system_discount" gorm:"column:system_discount;type:decimal(10,4)"`
	Priority       int       `json:"priority" gorm:"column:priority"`
	Enabled        int       `json:"enabled" gorm:"column:enabled"`
	Remark         *string   `json:"remark" gorm:"column:remark;type:varchar(500)"`
	CreateTime     time.Time `json:"create_time" gorm:"column:create_time"`
	UpdateTime     time.Time `json:"update_time" gorm:"column:update_time"`
}

func (OemDiscount) TableName() string {
	return "oem_discount"
}

var (
	oemDiscountCache       map[int64][]*OemDiscount  // key: oem_id
	oemDiscountCacheByCode map[string][]*OemDiscount // key: oem_code (向后兼容)
	oemDiscountCacheLock   sync.RWMutex
	oemDiscountCacheTime   time.Time
	// 复用与 OEM 配置相同的缓存过期时间
)

// InitOemDiscount 初始化OEM折扣配置缓存
func InitOemDiscount() {
	RefreshOemDiscountCache()
}

// RefreshOemDiscountCache 刷新OEM折扣配置缓存
func RefreshOemDiscountCache() {
	var discounts []*OemDiscount
	err := DB.Where("enabled = ?", 1).Order("priority DESC").Find(&discounts).Error
	if err != nil {
		common.SysLog("刷新OEM折扣配置缓存失败: " + err.Error())
		return
	}

	newCache := make(map[int64][]*OemDiscount)
	newCacheByCode := make(map[string][]*OemDiscount)

	// 按oem_id分组
	for _, discount := range discounts {
		if newCache[discount.OemId] == nil {
			newCache[discount.OemId] = make([]*OemDiscount, 0)
		}
		newCache[discount.OemId] = append(newCache[discount.OemId], discount)
	}

	// 按oem_code分组（向后兼容）
	for oemId, discountList := range newCache {
		oemConfig := GetOemConfigById(oemId)
		if oemConfig != nil {
			newCacheByCode[oemConfig.OemCode] = discountList
		}
	}

	oemDiscountCacheLock.Lock()
	oemDiscountCache = newCache
	oemDiscountCacheByCode = newCacheByCode
	oemDiscountCacheTime = time.Now()
	oemDiscountCacheLock.Unlock()

	common.SysLog(fmt.Sprintf("OEM折扣配置缓存已刷新，共 %d 条", len(discounts)))
}

// GetOemDiscounts 获取OEM所有折扣配置（通过OEM ID）
func GetOemDiscounts(oemId int64) []*OemDiscount {
	if oemId <= 0 {
		return nil
	}

	oemDiscountCacheLock.RLock()
	defer oemDiscountCacheLock.RUnlock()

	// 检查缓存是否过期
	if time.Since(oemDiscountCacheTime) > cacheExpireDuration {
		// 异步刷新缓存
		go RefreshOemDiscountCache()
	}

	return oemDiscountCache[oemId]
}

// GetOemDiscountsByCode 获取OEM所有折扣配置（通过OEM代码，向后兼容）
func GetOemDiscountsByCode(oemCode string) []*OemDiscount {
	if oemCode == "" {
		oemCode = "gravitex" // 默认系统
	}

	oemConfig := GetOemConfigByCode(oemCode)
	if oemConfig == nil {
		return nil
	}

	return GetOemDiscounts(oemConfig.Id)
}

// GetOemDiscount 获取OEM销售折扣（通过OEM ID）
// 优先级：模型级 > 厂商级 > 通配符 > 默认值(1.0)
func GetOemDiscount(oemId int64, modelName, vendorName string) float64 {
	// 先通过vendorName获取vendorId
	var vendorID *int64
	if vendorName != "" {
		var vendor Vendor
		err := DB.Where("name = ? AND deleted_at IS NULL", vendorName).First(&vendor).Error
		if err == nil {
			vendorIDInt64 := int64(vendor.Id)
			vendorID = &vendorIDInt64
		}
	}

	discounts := GetOemDiscounts(oemId)
	if len(discounts) == 0 {
		return 1.0 // 无配置，返回原价
	}

	// 按优先级排序（数字越大优先级越高）
	sortedDiscounts := make([]*OemDiscount, len(discounts))
	copy(sortedDiscounts, discounts)
	sort.Slice(sortedDiscounts, func(i, j int) bool {
		return sortedDiscounts[i].Priority > sortedDiscounts[j].Priority
	})

	// 1. 模型级匹配（vendor_id和model_name都不为空，且都匹配）
	if modelName != "" && vendorID != nil {
		for _, discount := range sortedDiscounts {
			if discount.VendorID != nil && discount.ModelName != nil &&
				*discount.VendorID == *vendorID && *discount.ModelName == modelName {
				return discount.SystemDiscount
			}
		}
	}

	// 2. 厂商级匹配（vendor_id不为空，model_name为空）
	if vendorID != nil {
		for _, discount := range sortedDiscounts {
			if discount.VendorID != nil &&
				(discount.ModelName == nil || *discount.ModelName == "") &&
				*discount.VendorID == *vendorID {
				return discount.SystemDiscount
			}
		}
	}

	// 3. 通配符匹配（vendor_id为NULL，model_name为空）
	for _, discount := range sortedDiscounts {
		if discount.VendorID == nil &&
			(discount.ModelName == nil || *discount.ModelName == "") {
			return discount.SystemDiscount
		}
	}

	// 4. 无配置，返回原价（1.0）
	return 1.0
}

// GetOemDiscountByCode 获取OEM销售折扣（通过OEM代码，向后兼容）
func GetOemDiscountByCode(oemCode, modelName, vendorName string) float64 {
	if oemCode == "" {
		oemCode = "gravitex" // 默认系统
	}

	oemConfig := GetOemConfigByCode(oemCode)
	if oemConfig == nil {
		return 1.0
	}

	return GetOemDiscount(oemConfig.Id, modelName, vendorName)
}

// 向后兼容函数
func GetSystemDiscounts(systemCode string) []*OemDiscount {
	return GetOemDiscountsByCode(systemCode)
}

func GetSystemDiscount(systemCode, modelName, vendorName string) float64 {
	return GetOemDiscountByCode(systemCode, modelName, vendorName)
}
