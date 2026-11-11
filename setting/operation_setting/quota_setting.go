package operation_setting

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
)

type QuotaSetting struct {
	EnableFreeModelPreConsume bool           `json:"enable_free_model_pre_consume"` // 是否对免费模型启用预消耗
	MinimumRemainingQuota     int            `json:"minimum_remaining_quota"`       // 模型预扣后需要保留的最低额度
	ModelQuotaReserve         map[string]int `json:"model_quota_reserve"`           // 模型最低保留额度，支持末尾 * 前缀匹配
	TrustQuotaUSD             float64        `json:"trust_quota_usd"`               // 钱包免预扣门槛，0 表示禁用
	PreConsumeMultiplier      float64        `json:"pre_consume_multiplier"`        // 预计输入费用的预扣倍率，仅影响预留
}

// 默认配置
var quotaSetting = QuotaSetting{
	EnableFreeModelPreConsume: true,
	TrustQuotaUSD:             10,
	PreConsumeMultiplier:      1,
}

func init() {
	// 注册到全局配置管理器
	config.GlobalConfig.Register("quota_setting", &quotaSetting)
}

func GetQuotaSetting() *QuotaSetting {
	return &quotaSetting
}

// ValidateModelQuotaReserve validates the persisted model reserve rules.
// Only exact model names and trailing-* prefixes are accepted.
func ValidateModelQuotaReserve(value string) error {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	var rules map[string]int
	if err := common.UnmarshalJsonStr(value, &rules); err != nil {
		return fmt.Errorf("model quota reserve rules must be a JSON object: %w", err)
	}
	if rules == nil {
		return fmt.Errorf("model quota reserve rules must be a JSON object")
	}
	for pattern, quota := range rules {
		if pattern == "" || (strings.Contains(pattern, "*") && !strings.HasSuffix(pattern, "*")) {
			return fmt.Errorf("invalid model quota reserve pattern: %q", pattern)
		}
		if quota < 0 {
			return fmt.Errorf("model quota reserve cannot be negative: %q", pattern)
		}
	}
	return nil
}

// ValidateQuotaOption validates reservation settings before they are persisted.
func ValidateQuotaOption(key, value string) error {
	if key != "quota_setting.trust_quota_usd" && key != "quota_setting.pre_consume_multiplier" {
		return nil
	}
	number, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(number) || math.IsInf(number, 0) || number < 0 {
		return fmt.Errorf("%s must be a finite non-negative number", key)
	}
	if key == "quota_setting.pre_consume_multiplier" && number == 0 {
		return fmt.Errorf("%s must be greater than zero", key)
	}
	return nil
}

// GetMinimumRemainingQuota returns the minimum wallet quota that must remain
// after a model's pre-consumption. Exact model rules take precedence over the
// longest trailing-* prefix rule, followed by the global default.
func GetMinimumRemainingQuota(modelName string) int {
	defaultValue := quotaSetting.MinimumRemainingQuota
	if defaultValue < 0 {
		defaultValue = 0
	}
	if value, ok := quotaSetting.ModelQuotaReserve[modelName]; ok && value >= 0 {
		return value
	}

	bestPrefixLength := -1
	bestValue := defaultValue
	for pattern, value := range quotaSetting.ModelQuotaReserve {
		if value < 0 || !strings.HasSuffix(pattern, "*") {
			continue
		}
		prefix := strings.TrimSuffix(pattern, "*")
		if len(prefix) <= bestPrefixLength || !strings.HasPrefix(modelName, prefix) {
			continue
		}
		bestPrefixLength = len(prefix)
		bestValue = value
	}
	return bestValue
}

// InputPreConsumeMultiplier rejects invalid runtime settings as well as invalid saves.
func InputPreConsumeMultiplier() (float64, error) {
	multiplier := quotaSetting.PreConsumeMultiplier
	if multiplier <= 0 || math.IsNaN(multiplier) || math.IsInf(multiplier, 0) {
		return 0, fmt.Errorf("pre-consume multiplier must be a finite number greater than zero")
	}
	return multiplier, nil
}
