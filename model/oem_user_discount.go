package model

import (
	"fmt"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
)

// OemUserDiscount OEM用户折扣配置
// OEM贴牌客户给自己用户配置的模型折扣
type OemUserDiscount struct {
	Id           int64      `json:"id" gorm:"primaryKey;autoIncrement"`
	OemId        int64      `json:"oem_id" gorm:"index;not null"`                                    // OEM ID
	VendorId     *int64     `json:"vendor_id" gorm:"index"`                                          // 厂商ID（NULL表示通配符）
	ModelName    *string    `json:"model_name" gorm:"column:model_name;type:varchar(128)"`           // 模型名称（NULL表示厂商级别）
	UserDiscount float64    `json:"user_discount" gorm:"type:decimal(10,4);not null;default:1.0000"` // 用户折扣（如0.9表示9折）
	Priority     int        `json:"priority" gorm:"default:0"`                                       // 优先级（数字越大优先级越高）
	Enabled      int        `json:"enabled" gorm:"default:1"`                                        // 是否启用（1启用，0禁用）
	Remark       string     `json:"remark"`                                                          // 备注
	CreateTime   *time.Time `json:"create_time" gorm:"column:create_time;autoCreateTime"`            // 创建时间
	UpdateTime   *time.Time `json:"update_time" gorm:"column:update_time;autoUpdateTime"`            // 更新时间
}

func (OemUserDiscount) TableName() string {
	return "oem_user_discount"
}

var (
	oemUserDiscountCache     map[int64][]*OemUserDiscount // key: oem_id
	oemUserDiscountCacheLock sync.RWMutex
	oemUserDiscountCacheTime time.Time
	// 复用与 OEM 配置相同的缓存过期时间
)

// InitOemUserDiscount 初始化OEM用户折扣配置缓存
func InitOemUserDiscount() {
	RefreshOemUserDiscountCache()
}

// RefreshOemUserDiscountCache 刷新OEM用户折扣配置缓存
func RefreshOemUserDiscountCache() {
	var discounts []*OemUserDiscount
	err := DB.Where("enabled = ?", 1).Order("priority DESC").Find(&discounts).Error
	if err != nil {
		common.SysLog("刷新OEM用户折扣配置缓存失败: " + err.Error())
		return
	}

	newCache := make(map[int64][]*OemUserDiscount)

	// 按oem_id分组
	for _, discount := range discounts {
		if newCache[discount.OemId] == nil {
			newCache[discount.OemId] = make([]*OemUserDiscount, 0)
		}
		newCache[discount.OemId] = append(newCache[discount.OemId], discount)
	}

	oemUserDiscountCacheLock.Lock()
	oemUserDiscountCache = newCache
	oemUserDiscountCacheTime = time.Now()
	oemUserDiscountCacheLock.Unlock()

	common.SysLog(fmt.Sprintf("OEM用户折扣配置缓存已刷新，共 %d 条", len(discounts)))
}

// GetOemUserDiscounts 获取OEM所有用户折扣配置（通过OEM ID）
func GetOemUserDiscounts(oemId int64) []*OemUserDiscount {
	if oemId <= 0 {
		return nil
	}

	oemUserDiscountCacheLock.RLock()
	defer oemUserDiscountCacheLock.RUnlock()

	// 检查缓存是否过期
	if time.Since(oemUserDiscountCacheTime) > cacheExpireDuration {
		// 异步刷新缓存
		go RefreshOemUserDiscountCache()
	}

	return oemUserDiscountCache[oemId]
}

// GetOemUserDiscount 获取OEM用户折扣
// 优先级：模型级 > 厂商级 > 通配符 > 默认值1.0
func GetOemUserDiscount(oemId int64, modelName string, vendorName string) float64 {
	// 1. 先通过vendorName获取vendorId
	var vendorId *int64
	if vendorName != "" {
		var vendor Vendor
		err := DB.Where("name = ? AND deleted_at IS NULL", vendorName).First(&vendor).Error
		if err == nil {
			vendorIdInt64 := int64(vendor.Id)
			vendorId = &vendorIdInt64
		}
	}

	// 2. 从缓存获取该OEM的所有折扣配置
	discounts := GetOemUserDiscounts(oemId)
	if len(discounts) == 0 {
		if common.DebugEnabled {
			common.SysLog(fmt.Sprintf("[GetOemUserDiscount] OEM %d 没有配置用户折扣，返回默认值 1.0", oemId))
		}
		return 1.0
	}

	// 3. 按优先级匹配
	// 3.1 模型级匹配（vendor_id和model_name都不为空，且都匹配）
	if modelName != "" && vendorId != nil {
		for _, discount := range discounts {
			if discount.VendorId != nil && discount.ModelName != nil &&
				*discount.VendorId == *vendorId && *discount.ModelName == modelName {
				if common.DebugEnabled {
					common.SysLog(fmt.Sprintf("[GetOemUserDiscount] 模型级匹配成功: vendorId=%d, modelName=%s, discount=%.4f",
						*vendorId, modelName, discount.UserDiscount))
				}
				return discount.UserDiscount
			}
		}
	}

	// 3.2 厂商级匹配（vendor_id不为空，model_name为空或为NULL）
	if vendorId != nil {
		for _, discount := range discounts {
			// model_name 为 NULL 或空字符串都视为厂商级配置
			isModelNameEmpty := discount.ModelName == nil || *discount.ModelName == ""
			if discount.VendorId != nil && isModelNameEmpty &&
				*discount.VendorId == *vendorId {
				if common.DebugEnabled {
					common.SysLog(fmt.Sprintf("[GetOemUserDiscount] 厂商级匹配成功: vendorId=%d, discount=%.4f",
						*vendorId, discount.UserDiscount))
				}
				return discount.UserDiscount
			}
		}
	}

	// 3.3 通配符匹配（vendor_id为NULL，model_name为空或NULL）
	for _, discount := range discounts {
		isModelNameEmpty := discount.ModelName == nil || *discount.ModelName == ""
		if discount.VendorId == nil && isModelNameEmpty {
			if common.DebugEnabled {
				common.SysLog(fmt.Sprintf("[GetOemUserDiscount] 通配符匹配成功: discount=%.4f", discount.UserDiscount))
			}
			return discount.UserDiscount
		}
	}

	// 4. 无配置，返回默认值1.0（不打折）
	if common.DebugEnabled {
		common.SysLog("[GetOemUserDiscount] 没有匹配到折扣配置，返回默认值 1.0")
	}
	return 1.0
}

// GetOemUserDiscountByOemId 根据OEM ID查询所有折扣配置（直接从数据库查询，用于管理界面）
func GetOemUserDiscountByOemId(oemId int64) ([]OemUserDiscount, error) {
	var discounts []OemUserDiscount
	err := DB.Where("oem_id = ?", oemId).Find(&discounts).Error
	return discounts, err
}

// GetOemUserDiscountByCode 根据OEM Code获取OEM用户折扣
func GetOemUserDiscountByCode(oemCode, modelName, vendorName string) float64 {
	if oemCode == "" {
		oemCode = "gravitex" // 默认系统
	}

	oemConfig := GetOemConfigByCode(oemCode)
	if oemConfig == nil {
		return 1.0
	}

	return GetOemUserDiscount(oemConfig.Id, modelName, vendorName)
}
