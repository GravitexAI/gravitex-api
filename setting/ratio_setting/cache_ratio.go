package ratio_setting

import (
	"fmt"
	"math"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/types"
)

var defaultCacheRatio = map[string]float64{
	"gemini-3-flash-preview":              0.1,
	"gemini-3-pro-preview":                0.1,
	"gemini-3.1-pro-preview":              0.1,
	"gpt-4":                               0.5,
	"o1":                                  0.5,
	"o1-2024-12-17":                       0.5,
	"o1-preview-2024-09-12":               0.5,
	"o1-preview":                          0.5,
	"o1-mini-2024-09-12":                  0.5,
	"o1-mini":                             0.5,
	"o3-mini":                             0.5,
	"o3-mini-2025-01-31":                  0.5,
	"gpt-4o-2024-11-20":                   0.5,
	"gpt-4o-2024-08-06":                   0.5,
	"gpt-4o":                              0.5,
	"gpt-4o-mini-2024-07-18":              0.5,
	"gpt-4o-mini":                         0.5,
	"gpt-4o-realtime-preview":             0.5,
	"gpt-4o-mini-realtime-preview":        0.5,
	"gpt-4.5-preview":                     0.5,
	"gpt-4.5-preview-2025-02-27":          0.5,
	"gpt-4.1":                             0.25,
	"gpt-4.1-mini":                        0.25,
	"gpt-4.1-nano":                        0.25,
	"gpt-image-2":                         0.25, // $1.25 / $5 = 0.25x
	"gpt-5":                               0.1,
	"gpt-5-2025-08-07":                    0.1,
	"gpt-5-chat-latest":                   0.1,
	"gpt-5-mini":                          0.1,
	"gpt-5-mini-2025-08-07":               0.1,
	"gpt-5-nano":                          0.1,
	"gpt-5-nano-2025-08-07":               0.1,
	"gpt-5.6-sol":                         0.1,
	"gpt-5.6-terra":                       0.1,
	"gpt-5.6-luna":                        0.1,
	"deepseek-chat":                       0.25,
	"deepseek-reasoner":                   0.25,
	"deepseek-coder":                      0.25,
	"claude-3-sonnet-20240229":            0.1,
	"claude-3-opus-20240229":              0.1,
	"claude-3-haiku-20240307":             0.1,
	"claude-3-5-haiku-20241022":           0.1,
	"claude-haiku-4-5-20251001":           0.1,
	"claude-3-5-sonnet-20240620":          0.1,
	"claude-3-5-sonnet-20241022":          0.1,
	"claude-3-7-sonnet-20250219":          0.1,
	"claude-3-7-sonnet-20250219-thinking": 0.1,
	"claude-sonnet-4-20250514":            0.1,
	"claude-sonnet-4-20250514-thinking":   0.1,
	"claude-opus-4-20250514":              0.1,
	"claude-opus-4-20250514-thinking":     0.1,
	"claude-opus-4-1-20250805":            0.1,
	"claude-opus-4-1-20250805-thinking":   0.1,
	"claude-sonnet-4-5-20250929":          0.1,
	"claude-sonnet-4-5-20250929-thinking": 0.1,
	"claude-opus-4-5-20251101":            0.1,
	"claude-opus-4-5-20251101-thinking":   0.1,
	"claude-opus-4-6":                     0.1,
	"claude-opus-4-6-thinking":            0.1,
	"claude-opus-4-6-max":                 0.1,
	"claude-opus-4-6-high":                0.1,
	"claude-opus-4-6-medium":              0.1,
	"claude-opus-4-6-low":                 0.1,
	"claude-opus-4-7":                     0.1,
	"claude-opus-4-7-thinking":            0.1,
	"claude-opus-4-7-max":                 0.1,
	"claude-opus-4-7-xhigh":               0.1,
	"claude-opus-4-7-high":                0.1,
	"claude-opus-4-7-medium":              0.1,
	"claude-opus-4-7-low":                 0.1,
	"claude-opus-4-8":                     0.1,
	"claude-opus-4-8-thinking":            0.1,
	"claude-opus-4-8-max":                 0.1,
	"claude-opus-4-8-xhigh":               0.1,
	"claude-opus-4-8-high":                0.1,
	"claude-opus-4-8-medium":              0.1,
	"claude-opus-4-8-low":                 0.1,
	"claude-opus-4-9":                     0.1,
	"claude-opus-4-9-thinking":            0.1,
	"claude-opus-4-9-max":                 0.1,
	"claude-opus-4-9-xhigh":               0.1,
	"claude-opus-4-9-high":                0.1,
	"claude-opus-4-9-medium":              0.1,
	"claude-opus-4-9-low":                 0.1,
	"claude-opus-4-10":                    0.1,
	"claude-opus-4-10-thinking":           0.1,
	"claude-opus-4-10-max":                0.1,
	"claude-opus-4-10-xhigh":              0.1,
	"claude-opus-4-10-high":               0.1,
	"claude-opus-4-10-medium":             0.1,
	"claude-opus-4-10-low":                0.1,
}

var defaultCreateCacheRatio = map[string]float64{
	// gpt-5.6+: cache writes billed at 1.25x the uncached input rate (same for Sol/Terra/Luna)
	"gpt-5.6-sol":                         1.25,
	"gpt-5.6-terra":                       1.25,
	"gpt-5.6-luna":                        1.25,
	"claude-3-sonnet-20240229":            1.25,
	"claude-3-opus-20240229":              1.25,
	"claude-3-haiku-20240307":             1.25,
	"claude-3-5-haiku-20241022":           1.25,
	"claude-haiku-4-5-20251001":           1.25,
	"claude-3-5-sonnet-20240620":          1.25,
	"claude-3-5-sonnet-20241022":          1.25,
	"claude-3-7-sonnet-20250219":          1.25,
	"claude-3-7-sonnet-20250219-thinking": 1.25,
	"claude-sonnet-4-20250514":            1.25,
	"claude-sonnet-4-20250514-thinking":   1.25,
	"claude-opus-4-20250514":              1.25,
	"claude-opus-4-20250514-thinking":     1.25,
	"claude-opus-4-1-20250805":            1.25,
	"claude-opus-4-1-20250805-thinking":   1.25,
	"claude-sonnet-4-5-20250929":          1.25,
	"claude-sonnet-4-5-20250929-thinking": 1.25,
	"claude-opus-4-5-20251101":            1.25,
	"claude-opus-4-5-20251101-thinking":   1.25,
	"claude-opus-4-6":                     1.25,
	"claude-opus-4-6-thinking":            1.25,
	"claude-opus-4-6-max":                 1.25,
	"claude-opus-4-6-high":                1.25,
	"claude-opus-4-6-medium":              1.25,
	"claude-opus-4-6-low":                 1.25,
	"claude-opus-4-7":                     1.25,
	"claude-opus-4-7-thinking":            1.25,
	"claude-opus-4-7-max":                 1.25,
	"claude-opus-4-7-xhigh":               1.25,
	"claude-opus-4-7-high":                1.25,
	"claude-opus-4-7-medium":              1.25,
	"claude-opus-4-7-low":                 1.25,
	"claude-opus-4-8":                     1.25,
	"claude-opus-4-8-thinking":            1.25,
	"claude-opus-4-8-max":                 1.25,
	"claude-opus-4-8-xhigh":               1.25,
	"claude-opus-4-8-high":                1.25,
	"claude-opus-4-8-medium":              1.25,
	"claude-opus-4-8-low":                 1.25,
	"claude-opus-4-9":                     1.25,
	"claude-opus-4-9-thinking":            1.25,
	"claude-opus-4-9-max":                 1.25,
	"claude-opus-4-9-xhigh":               1.25,
	"claude-opus-4-9-high":                1.25,
	"claude-opus-4-9-medium":              1.25,
	"claude-opus-4-9-low":                 1.25,
	"claude-opus-4-10":                    1.25,
	"claude-opus-4-10-thinking":           1.25,
	"claude-opus-4-10-max":                1.25,
	"claude-opus-4-10-xhigh":              1.25,
	"claude-opus-4-10-high":               1.25,
	"claude-opus-4-10-medium":             1.25,
	"claude-opus-4-10-low":                1.25,
}

//var defaultCreateCacheRatio = map[string]float64{}

var cacheRatioMap = types.NewRWMap[string, float64]()
var createCacheRatioMap = types.NewRWMap[string, float64]()

// createCache1hRatioMap 1h 缓存创建倍率（相对输入价），未配置的模型按 5m 缓存创建倍率的固定比例推算
var createCache1hRatioMap = types.NewRWMap[string, float64]()

// GetCacheRatioMap returns a copy of the cache ratio map
func GetCacheRatioMap() map[string]float64 {
	return cacheRatioMap.ReadAll()
}

// CacheRatio2JSONString converts the cache ratio map to a JSON string
func CacheRatio2JSONString() string {
	return cacheRatioMap.MarshalJSONString()
}

// CreateCacheRatio2JSONString converts the create cache ratio map to a JSON string
func CreateCacheRatio2JSONString() string {
	return createCacheRatioMap.MarshalJSONString()
}

// CreateCache1hRatio2JSONString converts the 1h create cache ratio map to a JSON string
func CreateCache1hRatio2JSONString() string {
	return createCache1hRatioMap.MarshalJSONString()
}

// UpdateCacheRatioByJSONString updates the cache ratio map from a JSON string
func UpdateCacheRatioByJSONString(jsonStr string) error {
	return types.LoadFromJsonStringWithCallback(cacheRatioMap, jsonStr, InvalidateExposedDataCache)
}

// UpdateCreateCacheRatioByJSONString updates the create cache ratio map from a JSON string
func UpdateCreateCacheRatioByJSONString(jsonStr string) error {
	return types.LoadFromJsonStringWithCallback(createCacheRatioMap, jsonStr, InvalidateExposedDataCache)
}

// GetCacheRatio returns the cache ratio for a model
func GetCacheRatio(name string) (float64, bool) {
	ratio, ok := cacheRatioMap.Get(name)
	if !ok {
		return 1, false // Default to 1 if not found
	}
	return ratio, true
}

// UpdateCreateCache1hRatioByJSONString updates the 1h create cache ratio map from a JSON string
func UpdateCreateCache1hRatioByJSONString(jsonStr string) error {
	return types.LoadFromJsonStringWithCallback(createCache1hRatioMap, jsonStr, InvalidateExposedDataCache)
}

// MaxCreateCache1hRatio 1h 缓存创建倍率上限，防止误配出异常扣费
const MaxCreateCache1hRatio = 100

// ValidateCreateCache1hRatio 校验 1h 缓存创建倍率配置：倍率必须是 (0, MaxCreateCache1hRatio] 内的有限数
func ValidateCreateCache1hRatio(jsonStr string) error {
	ratios := make(map[string]float64)
	if err := common.UnmarshalJsonStr(jsonStr, &ratios); err != nil {
		return fmt.Errorf("1h 缓存创建倍率必须是 {模型名: 倍率} 的 JSON 对象: %w", err)
	}
	for model, ratio := range ratios {
		if math.IsNaN(ratio) || math.IsInf(ratio, 0) || ratio <= 0 || ratio > MaxCreateCache1hRatio {
			return fmt.Errorf("模型 %s 的 1h 缓存创建倍率 %v 无效，必须大于 0 且不超过 %d", model, ratio, MaxCreateCache1hRatio)
		}
	}
	return nil
}

// GetCreateCache1hRatio 返回模型配置的 1h 缓存创建倍率，未配置时返回 false 由调用方按 5m 倍率推算
func GetCreateCache1hRatio(name string) (float64, bool) {
	return createCache1hRatioMap.Get(name)
}

func GetCreateCacheRatio(name string) (float64, bool) {
	ratio, ok := createCacheRatioMap.Get(name)
	if !ok {
		return 1.25, false // Default to 1.25 if not found
	}
	return ratio, true
}

func GetCacheRatioCopy() map[string]float64 {
	return cacheRatioMap.ReadAll()
}

func GetCreateCacheRatioCopy() map[string]float64 {
	return createCacheRatioMap.ReadAll()
}

func GetCreateCache1hRatioCopy() map[string]float64 {
	return createCache1hRatioMap.ReadAll()
}
