package setting

import (
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
)

const (
	AutoRouterOptionKey = "AutoRouter"
	AutoCostTierLow     = "low"
	AutoCostTierMedium  = "medium"
	AutoCostTierHigh    = "high"
	AutoCostTierMax     = "max"
)

var autoCostTiers = []string{AutoCostTierLow, AutoCostTierMedium, AutoCostTierHigh, AutoCostTierMax}

// AutoModelCapability is an optional hard-filter hint for a concrete model.
// A nil pointer means "unknown" and does not exclude the model.
type AutoModelCapability struct {
	Tools  *bool `json:"tools,omitempty"`
	Vision *bool `json:"vision,omitempty"`
	JSON   *bool `json:"json,omitempty"`
}

type AutoRouterSetting struct {
	Enabled       bool                           `json:"enabled"`
	DefaultTier   string                         `json:"default_tier"`
	StickinessTTL int                            `json:"stickiness_ttl"`
	Tiers         map[string][]string            `json:"tiers"`
	TaskPrefer    map[string][]string            `json:"task_prefer"`
	Weights       map[string]int                 `json:"weights"`
	Capabilities  map[string]AutoModelCapability `json:"capabilities"`
}

var (
	autoRouterMu sync.RWMutex
	autoRouter   = defaultAutoRouterSetting()
)

func defaultAutoRouterSetting() AutoRouterSetting {
	return AutoRouterSetting{
		Enabled:       false,
		DefaultTier:   AutoCostTierMedium,
		StickinessTTL: 1800,
		Tiers: map[string][]string{
			AutoCostTierLow:    {},
			AutoCostTierMedium: {},
			AutoCostTierHigh:   {},
			AutoCostTierMax:    {},
		},
		TaskPrefer:   map[string][]string{},
		Weights:      map[string]int{},
		Capabilities: map[string]AutoModelCapability{},
	}
}

func GetAutoRouterSetting() AutoRouterSetting {
	autoRouterMu.RLock()
	defer autoRouterMu.RUnlock()
	return cloneAutoRouterSetting(autoRouter)
}

func AutoRouter2JsonString() string {
	autoRouterMu.RLock()
	defer autoRouterMu.RUnlock()
	jsonBytes, err := common.Marshal(autoRouter)
	if err != nil {
		return "{}"
	}
	return string(jsonBytes)
}

func ValidateAutoRouterJSON(jsonString string) error {
	next := defaultAutoRouterSetting()
	if strings.TrimSpace(jsonString) == "" {
		return nil
	}
	return common.UnmarshalJsonStr(jsonString, &next)
}

func UpdateAutoRouterByJsonString(jsonString string) error {
	next := defaultAutoRouterSetting()
	if strings.TrimSpace(jsonString) != "" {
		if err := common.UnmarshalJsonStr(jsonString, &next); err != nil {
			return err
		}
	}
	normalizeAutoRouterSetting(&next)
	autoRouterMu.Lock()
	autoRouter = next
	autoRouterMu.Unlock()
	return nil
}

func NormalizeAutoCostTier(tier string) string {
	switch strings.ToLower(strings.TrimSpace(tier)) {
	case AutoCostTierLow, AutoCostTierMedium, AutoCostTierHigh, AutoCostTierMax:
		return strings.ToLower(strings.TrimSpace(tier))
	default:
		return ""
	}
}

func AutoCostTiers() []string {
	cloned := make([]string, len(autoCostTiers))
	copy(cloned, autoCostTiers)
	return cloned
}

func normalizeAutoRouterSetting(cfg *AutoRouterSetting) {
	if NormalizeAutoCostTier(cfg.DefaultTier) == "" {
		cfg.DefaultTier = AutoCostTierMedium
	} else {
		cfg.DefaultTier = NormalizeAutoCostTier(cfg.DefaultTier)
	}
	if cfg.StickinessTTL < 0 {
		cfg.StickinessTTL = 0
	}
	if cfg.Tiers == nil {
		cfg.Tiers = map[string][]string{}
	}
	if cfg.TaskPrefer == nil {
		cfg.TaskPrefer = map[string][]string{}
	}
	if cfg.Weights == nil {
		cfg.Weights = map[string]int{}
	}
	if cfg.Capabilities == nil {
		cfg.Capabilities = map[string]AutoModelCapability{}
	}
	for _, tier := range autoCostTiers {
		if _, ok := cfg.Tiers[tier]; !ok {
			cfg.Tiers[tier] = []string{}
		}
	}
}

func cloneAutoRouterSetting(src AutoRouterSetting) AutoRouterSetting {
	dst := AutoRouterSetting{
		Enabled:       src.Enabled,
		DefaultTier:   src.DefaultTier,
		StickinessTTL: src.StickinessTTL,
		Tiers:         cloneStringSliceMap(src.Tiers),
		TaskPrefer:    cloneStringSliceMap(src.TaskPrefer),
		Weights:       cloneIntMap(src.Weights),
		Capabilities:  cloneAutoCapabilityMap(src.Capabilities),
	}
	return dst
}

func cloneStringSliceMap(src map[string][]string) map[string][]string {
	if src == nil {
		return map[string][]string{}
	}
	dst := make(map[string][]string, len(src))
	for key, values := range src {
		cloned := make([]string, len(values))
		copy(cloned, values)
		dst[key] = cloned
	}
	return dst
}

func cloneIntMap(src map[string]int) map[string]int {
	if src == nil {
		return map[string]int{}
	}
	dst := make(map[string]int, len(src))
	for key, value := range src {
		dst[key] = value
	}
	return dst
}

func cloneAutoCapabilityMap(src map[string]AutoModelCapability) map[string]AutoModelCapability {
	if src == nil {
		return map[string]AutoModelCapability{}
	}
	dst := make(map[string]AutoModelCapability, len(src))
	for key, value := range src {
		dst[key] = value
	}
	return dst
}
