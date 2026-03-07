package model

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
)

type OemConfig struct {
	Id              int64     `json:"id" gorm:"primaryKey"`
	OemCode         string    `json:"oem_code" gorm:"column:oem_code;type:varchar(32);uniqueIndex"`
	OemName         string    `json:"oem_name" gorm:"column:oem_name;type:varchar(64)"`
	ApiPrefix       string    `json:"api_prefix" gorm:"column:api_prefix;type:varchar(64);uniqueIndex"`
	ApiPrefixAlias  *string   `json:"api_prefix_alias" gorm:"column:api_prefix_alias;type:varchar(255)"`
	Domain          *string   `json:"domain" gorm:"column:domain;type:varchar(255)"`
	LogoUrl         *string   `json:"logo_url" gorm:"column:logo_url;type:varchar(255)"`
	CompanyName     *string   `json:"company_name" gorm:"column:company_name;type:varchar(128)"`
	ContactEmail    *string   `json:"contact_email" gorm:"column:contact_email;type:varchar(128)"`
	OemAdminUserId  *int64    `json:"oem_admin_user_id" gorm:"column:oem_admin_user_id"`
	FeatureFlags    *string   `json:"feature_flags" gorm:"column:feature_flags;type:json"`
	Enabled         int       `json:"enabled" gorm:"column:enabled"`
	Remark          *string   `json:"remark" gorm:"column:remark;type:varchar(500)"`
	CreateTime      time.Time `json:"create_time" gorm:"column:create_time"`
	UpdateTime      time.Time `json:"update_time" gorm:"column:update_time"`
	RegistrationFee *float64  `json:"registration_fee" gorm:"column:registration_fee;type:decimal(10,2)"`
	ServiceHotline  *string   `json:"service_hotline" gorm:"column:service_hotline;type:varchar(50)"`
	ContactImageUrl *string   `json:"contact_image_url" gorm:"column:contact_image_url;type:varchar(500)"`
	ServiceHours    *string   `json:"service_hours" gorm:"column:service_hours;type:varchar(200)"`
	RecordInfo      *string   `json:"record_info" gorm:"column:record_info;type:varchar(128)"`
	WebsiteUrl      *string   `json:"website_url" gorm:"column:website_url;type:varchar(128)"`
	PowerBy         *string   `json:"power_by" gorm:"column:power_by;type:varchar(128)"`
	ExtraConfig     *string   `json:"extra_config" gorm:"column:extra_config;type:json"`
}

func (OemConfig) TableName() string {
	return "oem_config"
}

var (
	oemConfigCache     map[string]*OemConfig // key: oem_code
	oemConfigCacheById map[int64]*OemConfig  // key: id
	oemConfigCacheLock sync.RWMutex
	oemConfigCacheTime time.Time
	// 缓存过期时间，复用平台成本中的约定：1 小时
	cacheExpireDuration = 1 * time.Hour
)

// InitOemConfig 初始化OEM配置缓存
func InitOemConfig() {
	RefreshOemConfigCache()
}

// RefreshOemConfigCache 刷新OEM配置缓存
func RefreshOemConfigCache() {
	var configs []*OemConfig
	err := DB.Where("enabled = ?", 1).Find(&configs).Error
	if err != nil {
		common.SysLog("刷新OEM配置缓存失败: " + err.Error())
		return
	}

	newCache := make(map[string]*OemConfig)
	newCacheById := make(map[int64]*OemConfig)
	for _, config := range configs {
		newCache[config.OemCode] = config
		newCacheById[config.Id] = config
	}

	oemConfigCacheLock.Lock()
	oemConfigCache = newCache
	oemConfigCacheById = newCacheById
	oemConfigCacheTime = time.Now()
	oemConfigCacheLock.Unlock()
	common.SysLog(fmt.Sprintf("OEM配置缓存已刷新，共 %d 条", len(configs)))
}

// GetOemConfigByCode 根据OEM代码获取OEM配置
func GetOemConfigByCode(oemCode string) *OemConfig {
	if oemCode == "" {
		oemCode = "gravitex" // 默认系统
	}

	oemConfigCacheLock.RLock()
	defer oemConfigCacheLock.RUnlock()

	// 检查缓存是否过期
	if time.Since(oemConfigCacheTime) > cacheExpireDuration {
		// 异步刷新缓存
		go RefreshOemConfigCache()
	}

	return oemConfigCache[oemCode]
}

// GetOemConfigById 根据OEM ID获取OEM配置
func GetOemConfigById(oemId int64) *OemConfig {
	if oemId <= 0 {
		return nil
	}

	oemConfigCacheLock.RLock()
	defer oemConfigCacheLock.RUnlock()

	// 检查缓存是否过期
	if time.Since(oemConfigCacheTime) > cacheExpireDuration {
		// 异步刷新缓存
		go RefreshOemConfigCache()
	}

	return oemConfigCacheById[oemId]
}

// GetOemConfigByApiPrefix 根据API前缀获取OEM配置
func GetOemConfigByApiPrefix(apiPrefix string) *OemConfig {
	oemConfigCacheLock.RLock()
	defer oemConfigCacheLock.RUnlock()

	// 检查缓存是否过期
	if time.Since(oemConfigCacheTime) > cacheExpireDuration {
		// 异步刷新缓存
		go RefreshOemConfigCache()
	}

	for _, config := range oemConfigCache {
		if config.ApiPrefix == apiPrefix {
			return config
		}
	}

	return nil
}

// GetOemConfigByApiPrefixAlias 根据API前缀别名获取OEM配置
func GetOemConfigByApiPrefixAlias(apiPrefixAlias string) *OemConfig {
	oemConfigCacheLock.RLock()
	defer oemConfigCacheLock.RUnlock()

	// 检查缓存是否过期
	if time.Since(oemConfigCacheTime) > cacheExpireDuration {
		// 异步刷新缓存
		go RefreshOemConfigCache()
	}

	for _, config := range oemConfigCache {
		if config.ApiPrefixAlias != nil {
			// 检查别名是否匹配（支持逗号分隔的多个别名）
			aliases := strings.Split(*config.ApiPrefixAlias, ",")
			for _, alias := range aliases {
				if strings.TrimSpace(alias) == apiPrefixAlias {
					return config
				}
			}
		}
	}

	return nil
}

// GetOemConfigByDomain 根据域名获取OEM配置
func GetOemConfigByDomain(domain string) *OemConfig {
	if domain == "" {
		return nil
	}
	// 本地开发常用域名不查库，由上层中间件回退到默认 oem（如 gravitex），避免 "record not found" 日志与无效查询
	if domain == "127.0.0.1" || domain == "localhost" {
		return nil
	}

	oemConfigCacheLock.RLock()
	defer oemConfigCacheLock.RUnlock()

	// 检查缓存是否过期
	if time.Since(oemConfigCacheTime) > cacheExpireDuration {
		// 异步刷新缓存
		go RefreshOemConfigCache()
	}

	// 从缓存中查找
	for _, config := range oemConfigCache {
		if config.Domain != nil && *config.Domain == domain {
			return config
		}
	}

	// 缓存中没有找到,从数据库查询
	var config OemConfig
	err := DB.Where("domain = ? AND enabled = 1", domain).First(&config).Error
	if err != nil {
		return nil
	}

	// 如果找到,可以考虑更新缓存(但这里不更新,等待下次刷新)
	return &config
}

// 向后兼容函数
func GetSystemConfigByCode(systemCode string) *OemConfig {
	return GetOemConfigByCode(systemCode)
}

func GetSystemConfigByApiPrefix(apiPrefix string) *OemConfig {
	return GetOemConfigByApiPrefix(apiPrefix)
}

func GetSystemConfigByApiPrefixAlias(apiPrefixAlias string) *OemConfig {
	return GetOemConfigByApiPrefixAlias(apiPrefixAlias)
}

// SyncOemConfigCache 定时刷新OEM相关的所有缓存
// frequency: 刷新间隔（秒）
func SyncOemConfigCache(frequency int) {
	for {
		time.Sleep(time.Duration(frequency) * time.Second)
		common.SysLog("定时刷新OEM配置缓存...")

		// 刷新所有OEM相关缓存
		RefreshOemConfigCache()
		RefreshOemDiscountCache()
		RefreshOemUserDiscountCache()

		common.SysLog("OEM配置缓存刷新完成")
	}
}
