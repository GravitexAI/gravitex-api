package model

import (
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
)

// ModelExtension 映射 t_extension_models（模型扩展属性表）。
//
// 这张表的 owner 是 Java 管理后端（Gravitex-API-End 的 ExtensionModels），
// Go 网关只读不写，所以它**不能**加进 model/main.go 的 AutoMigrate 列表，
// 否则 GORM 会按 Go 侧的结构体去 ALTER 一张不归自己管的表。
type ModelExtension struct {
	Id              int     `gorm:"column:id;primaryKey"`
	ModelId         int     `gorm:"column:model_id"`
	IoSchema        string  `gorm:"column:io_schema"`
	Capabilities    string  `gorm:"column:capabilities"`
	ContextWindow   *int    `gorm:"column:context_window"`
	MaxOutputTokens *int    `gorm:"column:max_output_tokens"`
	Status          int     `gorm:"column:status"`
	DeletedAt       *string `gorm:"column:deleted_at"`
}

func (ModelExtension) TableName() string {
	return "t_extension_models"
}

// ModelIOSchema 是 t_extension_models.io_schema 的结构，例如：
//
//	{"modes":["reasoning"],
//	 "modalities":{"reasoning":{"input":["text","image","file"],"output":["text"]}},
//	 "primary_mode":"reasoning",
//	 "endpoint_types":["openai","openai-response"]}
type ModelIOSchema struct {
	Modes         []string                     `json:"modes"`
	Modalities    map[string]ModelModalityPair `json:"modalities"`
	PrimaryMode   string                       `json:"primary_mode"`
	EndpointTypes []string                     `json:"endpoint_types"`
}

type ModelModalityPair struct {
	Input  []string `json:"input"`
	Output []string `json:"output"`
}

// ModelCapabilities 是 t_extension_models.capabilities 的结构，只取网关用得上的开关。
type ModelCapabilities struct {
	SupportsFunctionCalling bool `json:"supports_function_calling"`
	SupportsWebSearch       bool `json:"supports_web_search"`
	SupportsStructuredOut   bool `json:"supports_structured_output"`
	SupportsPromptCaching   bool `json:"supports_prompt_caching"`
}

// ModelExtensionInfo 是扩展属性解析后的形态，按模型名索引。
type ModelExtensionInfo struct {
	ModelName       string
	DisplayName     string
	Description     string
	ContextWindow   int
	MaxOutputTokens int
	IOSchema        ModelIOSchema
	Capabilities    ModelCapabilities
}

// PrimaryInputModalities 返回主模式下的输入模态，例如 ["text","image","file"]。
func (info ModelExtensionInfo) PrimaryInputModalities() []string {
	if info.IOSchema.PrimaryMode == "" {
		return nil
	}
	return info.IOSchema.Modalities[info.IOSchema.PrimaryMode].Input
}

// SupportsEndpointType 判断 io_schema.endpoint_types 是否含指定端点，
// 例如 "openai-response" 表示该模型可以走 Responses API。
func (info ModelExtensionInfo) SupportsEndpointType(endpointType string) bool {
	for _, et := range info.IOSchema.EndpointTypes {
		if strings.EqualFold(strings.TrimSpace(et), endpointType) {
			return true
		}
	}
	return false
}

const modelExtensionCacheTTL = 5 * time.Minute

var (
	modelExtensionCache     map[string]ModelExtensionInfo
	modelExtensionCacheTime time.Time
	modelExtensionCacheLock sync.RWMutex
)

// GetModelExtensions 返回「模型名 -> 扩展属性」映射，带 5 分钟缓存。
// 扩展属性是运营在管理后台配的，变更不频繁，缓存失效时整表重建即可。
func GetModelExtensions() map[string]ModelExtensionInfo {
	modelExtensionCacheLock.RLock()
	if modelExtensionCache != nil && time.Since(modelExtensionCacheTime) < modelExtensionCacheTTL {
		cached := modelExtensionCache
		modelExtensionCacheLock.RUnlock()
		return cached
	}
	modelExtensionCacheLock.RUnlock()

	modelExtensionCacheLock.Lock()
	defer modelExtensionCacheLock.Unlock()
	// 拿到写锁后二次确认，避免并发重复查库
	if modelExtensionCache != nil && time.Since(modelExtensionCacheTime) < modelExtensionCacheTTL {
		return modelExtensionCache
	}

	refreshed, err := loadModelExtensions()
	if err != nil {
		common.SysError("load model extensions failed: " + err.Error())
		// 查库失败时沿用旧缓存，避免 catalog 突然变空导致客户端模型列表清零
		if modelExtensionCache != nil {
			return modelExtensionCache
		}
		modelExtensionCache = map[string]ModelExtensionInfo{}
		modelExtensionCacheTime = time.Now()
		return modelExtensionCache
	}

	modelExtensionCache = refreshed
	modelExtensionCacheTime = time.Now()
	return modelExtensionCache
}

// InvalidateModelExtensionCache 清空扩展属性缓存，供管理端改完配置后主动刷新。
func InvalidateModelExtensionCache() {
	modelExtensionCacheLock.Lock()
	defer modelExtensionCacheLock.Unlock()
	modelExtensionCache = nil
	modelExtensionCacheTime = time.Time{}
}

func loadModelExtensions() (map[string]ModelExtensionInfo, error) {
	// 一次 join 把模型名带出来，避免 N+1
	var rows []struct {
		ModelName       string
		ModelNickName   string
		Description     string
		ContextWindow   *int
		MaxOutputTokens *int
		IoSchema        string
		Capabilities    string
	}
	err := DB.Table("t_extension_models AS e").
		Select("m.model_name AS model_name, m.model_nick_name AS model_nick_name, "+
			"m.description AS description, e.context_window AS context_window, "+
			"e.max_output_tokens AS max_output_tokens, e.io_schema AS io_schema, e.capabilities AS capabilities").
		Joins("JOIN models AS m ON m.id = e.model_id").
		Where("e.status = ?", 1).
		Where("e.deleted_at IS NULL OR e.deleted_at = ?", "").
		Where("m.deleted_at IS NULL").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	result := make(map[string]ModelExtensionInfo, len(rows))
	for _, row := range rows {
		if row.ModelName == "" {
			continue
		}
		info := ModelExtensionInfo{
			ModelName:   row.ModelName,
			DisplayName: row.ModelNickName,
			Description: row.Description,
		}
		if info.DisplayName == "" {
			info.DisplayName = row.ModelName
		}
		if row.ContextWindow != nil {
			info.ContextWindow = *row.ContextWindow
		}
		if row.MaxOutputTokens != nil {
			info.MaxOutputTokens = *row.MaxOutputTokens
		}
		if row.IoSchema != "" {
			if err := common.UnmarshalJsonStr(row.IoSchema, &info.IOSchema); err != nil {
				common.SysError("parse io_schema failed for model " + row.ModelName + ": " + err.Error())
			}
		}
		if row.Capabilities != "" {
			if err := common.UnmarshalJsonStr(row.Capabilities, &info.Capabilities); err != nil {
				common.SysError("parse capabilities failed for model " + row.ModelName + ": " + err.Error())
			}
		}
		result[row.ModelName] = info
	}
	return result, nil
}
