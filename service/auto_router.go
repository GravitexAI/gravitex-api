package service

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/cachex"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
	"github.com/samber/hot"
	"github.com/tidwall/gjson"
)

const (
	autoStickyCacheNamespace = "new-api:auto_router_sticky:v1"
	autoVirtualModel         = "auto"
	autoHeaderCostTier       = "X-Cost-Tier"
	autoHeaderSessionID      = "X-Session-Id"
	autoHeaderSessionAlt     = "X-Auto-Session"
	autoHeaderModel          = "X-Auto-Model"
	autoHeaderTask           = "X-Auto-Task"
	autoHeaderTier           = "X-Auto-Tier"
	autoFingerprintLimit     = 2048
	autoStickyCacheCapacity  = 100_000
)

var (
	ErrAutoRouterDisabled = errors.New("auto router is not enabled")
	ErrAutoNoAvailable    = errors.New("no available model in auto pool")

	autoStickyCacheOnce sync.Once
	autoStickyCache     *cachex.HybridCache[string]
)

type AutoResolveResult struct {
	Model  string
	Task   string
	Tier   string
	Sticky bool
}

func IsAutoModel(name string) bool {
	return name == autoVirtualModel || strings.HasPrefix(name, autoVirtualModel+":")
}

func IsAutoSupportedPath(path string) bool {
	return strings.HasPrefix(path, "/v1/chat/completions") ||
		strings.HasPrefix(path, "/v1/responses") ||
		strings.HasPrefix(path, "/v1/messages") ||
		strings.HasPrefix(path, "/pg/chat/completions")
}

func AutoVirtualModelNames() []string {
	names := []string{autoVirtualModel}
	for _, tier := range setting.AutoCostTiers() {
		names = append(names, autoVirtualModel+":"+tier)
	}
	return names
}

func ShouldExposeAutoModels(c *gin.Context, available []string) bool {
	if !setting.GetAutoRouterSetting().Enabled {
		return false
	}
	if len(available) > 0 {
		return true
	}
	return c != nil && IsTokenModelAccessLimited(c) && IsModelAllowedByToken(c, autoVirtualModel)
}

func ResolveAutoModel(c *gin.Context, autoName string, body []byte, available []string) (*AutoResolveResult, error) {
	cfg := setting.GetAutoRouterSetting()
	if !cfg.Enabled {
		return nil, ErrAutoRouterDisabled
	}

	tier := parseAutoTier(autoName, headerCostTier(c), cfg.DefaultTier)
	task := classifyAutoTask(body)
	pool := cfg.Tiers[tier]
	if len(pool) == 0 {
		pool = available
	}
	pool = intersectAutoPool(pool, available)
	pool = filterAutoPoolByToken(c, autoName, pool)
	pool = filterAutoPoolByCapability(body, pool, cfg.Capabilities)
	pool = uniqueNonEmpty(pool)
	if prefer := cfg.TaskPrefer[task]; len(prefer) > 0 {
		if preferred := intersectAutoPool(prefer, pool); len(preferred) > 0 {
			pool = preferred
		}
	}
	if len(pool) == 0 {
		return nil, ErrAutoNoAvailable
	}

	if cfg.StickinessTTL > 0 {
		if sticky := getAutoStickyModel(c, autoName, body, cfg.StickinessTTL); sticky != "" && containsString(pool, sticky) {
			return &AutoResolveResult{Model: sticky, Task: task, Tier: tier, Sticky: true}, nil
		}
	}

	selected := weightedAutoPick(pool, cfg.Weights)
	if cfg.StickinessTTL > 0 {
		setAutoStickyModel(c, autoName, body, selected, cfg.StickinessTTL)
	}
	return &AutoResolveResult{Model: selected, Task: task, Tier: tier, Sticky: false}, nil
}

func CollectAutoAvailableModels(c *gin.Context) []string {
	usingGroup := common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
	if usingGroup == "auto" {
		userGroup := common.GetContextKeyString(c, constant.ContextKeyUserGroup)
		return GetGroupsEnabledModels(GetRequestAutoGroups(c, userGroup))
	}
	if usingGroup == "" {
		return nil
	}
	return model.CacheGetGroupEnabledModels(usingGroup)
}

func IsAutoModelAllowedByToken(c *gin.Context, autoName, resolved string) bool {
	if IsModelAllowedByToken(c, resolved) {
		return true
	}
	if IsModelAllowedByToken(c, autoVirtualModel) {
		return true
	}
	return autoName != autoVirtualModel && IsModelAllowedByToken(c, autoName)
}

func ApplyAutoResolveTransparency(c *gin.Context, original string, result *AutoResolveResult) {
	if result == nil {
		return
	}
	common.SetContextKey(c, constant.ContextKeyAutoOriginalModel, original)
	common.SetContextKey(c, constant.ContextKeyAutoTask, result.Task)
	common.SetContextKey(c, constant.ContextKeyAutoTier, result.Tier)
	c.Header(autoHeaderModel, result.Model)
	c.Header(autoHeaderTask, result.Task)
	c.Header(autoHeaderTier, result.Tier)
}

func AppendAutoRouterAdminInfo(c *gin.Context, adminInfo map[string]interface{}) {
	if adminInfo == nil || c == nil {
		return
	}
	original := common.GetContextKeyString(c, constant.ContextKeyAutoOriginalModel)
	if original == "" {
		return
	}
	adminInfo["auto_original_model"] = original
	if task := common.GetContextKeyString(c, constant.ContextKeyAutoTask); task != "" {
		adminInfo["auto_task"] = task
	}
	if tier := common.GetContextKeyString(c, constant.ContextKeyAutoTier); tier != "" {
		adminInfo["auto_tier"] = tier
	}
}

func classifyAutoTask(body []byte) string {
	if hasAutoTools(body) {
		return "agent"
	}
	if hasAutoVision(body) {
		return "vision"
	}
	text := strings.ToLower(extractAutoPromptPrefix(body))
	switch {
	case strings.Contains(text, "```"),
		strings.Contains(text, "def "),
		strings.Contains(text, "import "),
		strings.Contains(text, "traceback"),
		strings.Contains(text, "func "),
		strings.Contains(text, "class "),
		strings.Contains(text, "pytest"):
		return "code"
	case strings.Contains(text, "翻译"),
		strings.Contains(text, "translate"),
		strings.Contains(text, "译成"):
		return "translation"
	case strings.Contains(text, "证明"),
		strings.Contains(text, "方程"),
		strings.Contains(text, "step by step"),
		strings.Contains(text, "\\frac"),
		strings.Contains(text, "\\sum"):
		return "reasoning"
	default:
		return "general"
	}
}

func filterAutoPoolByCapability(body []byte, pool []string, caps map[string]setting.AutoModelCapability) []string {
	needsTools := hasAutoTools(body)
	needsVision := hasAutoVision(body)
	needsJSON := hasAutoJSON(body)
	if !needsTools && !needsVision && !needsJSON {
		return pool
	}

	filtered := make([]string, 0, len(pool))
	visionNamed := make([]string, 0, len(pool))
	unknownVision := make([]string, 0, len(pool))
	explicitVision := false
	for _, name := range pool {
		cap, ok := caps[name]
		if needsTools && ok && cap.Tools != nil && !*cap.Tools {
			continue
		}
		if needsJSON && ok && cap.JSON != nil && !*cap.JSON {
			continue
		}
		if needsVision {
			if ok && cap.Vision != nil {
				if *cap.Vision {
					explicitVision = true
					filtered = append(filtered, name)
				}
				continue
			}
			if looksLikeVisionModel(name) {
				visionNamed = append(visionNamed, name)
				continue
			}
			unknownVision = append(unknownVision, name)
			continue
		}
		filtered = append(filtered, name)
	}
	if !needsVision {
		return filtered
	}
	if explicitVision {
		return filtered
	}
	if len(visionNamed) > 0 {
		return visionNamed
	}
	if needsTools || needsJSON {
		return unknownVision
	}
	return pool
}

func parseAutoTier(autoName, headerTier, defaultTier string) string {
	if strings.HasPrefix(autoName, autoVirtualModel+":") {
		if tier := setting.NormalizeAutoCostTier(strings.TrimPrefix(autoName, autoVirtualModel+":")); tier != "" {
			return tier
		}
	}
	if tier := setting.NormalizeAutoCostTier(headerTier); tier != "" {
		return tier
	}
	if tier := setting.NormalizeAutoCostTier(defaultTier); tier != "" {
		return tier
	}
	return setting.AutoCostTierMedium
}

func headerCostTier(c *gin.Context) string {
	if c == nil || c.Request == nil {
		return ""
	}
	return c.GetHeader(autoHeaderCostTier)
}

func autoSessionKey(c *gin.Context, autoName string, body []byte) string {
	sessionID := ""
	if c != nil && c.Request != nil {
		sessionID = strings.TrimSpace(c.GetHeader(autoHeaderSessionID))
		if sessionID == "" {
			sessionID = strings.TrimSpace(c.GetHeader(autoHeaderSessionAlt))
		}
	}
	if sessionID == "" {
		sessionID = gjson.GetBytes(body, "metadata.user_id").String()
	}
	if sessionID == "" {
		sessionID = fingerprintAutoPrompt(body)
	}
	userID := 0
	if c != nil {
		userID = common.GetContextKeyInt(c, constant.ContextKeyUserId)
	}
	return fmt.Sprintf("%d:%s:%s", userID, autoName, sessionID)
}

func fingerprintAutoPrompt(body []byte) string {
	text := extractAutoPromptPrefix(body)
	if len(text) > autoFingerprintLimit {
		text = text[:autoFingerprintLimit]
	}
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:16])
}

func extractAutoPromptPrefix(body []byte) string {
	var builder strings.Builder
	if system := gjson.GetBytes(body, "system"); system.Exists() {
		writeAutoContentText(&builder, system)
		builder.WriteByte('\n')
	}
	appendFirstSystemAndUser(&builder, gjson.GetBytes(body, "messages"))
	if builder.Len() == 0 {
		appendFirstSystemAndUser(&builder, gjson.GetBytes(body, "input"))
	}
	return builder.String()
}

func appendFirstSystemAndUser(builder *strings.Builder, messages gjson.Result) {
	if !messages.Exists() {
		return
	}
	if messages.Type == gjson.String {
		builder.WriteString(messages.String())
		return
	}
	if !messages.IsArray() {
		return
	}
	wroteSystem := builder.Len() > 0
	wroteUser := false
	for _, message := range messages.Array() {
		role := message.Get("role").String()
		if role == "system" && !wroteSystem {
			writeAutoContentText(builder, message.Get("content"))
			builder.WriteByte('\n')
			wroteSystem = true
		}
		if (role == "user" || role == "") && !wroteUser {
			writeAutoContentText(builder, message.Get("content"))
			builder.WriteByte('\n')
			wroteUser = true
		}
		if wroteSystem && wroteUser {
			return
		}
	}
}

func writeAutoContentText(builder *strings.Builder, content gjson.Result) {
	if content.Type == gjson.String {
		builder.WriteString(content.String())
		return
	}
	if !content.IsArray() {
		return
	}
	for _, part := range content.Array() {
		if text := part.Get("text"); text.Exists() {
			builder.WriteString(text.String())
			builder.WriteByte('\n')
		}
	}
}

func hasAutoTools(body []byte) bool {
	tools := gjson.GetBytes(body, "tools")
	if tools.IsArray() && len(tools.Array()) > 0 {
		return true
	}
	functions := gjson.GetBytes(body, "functions")
	return functions.IsArray() && len(functions.Array()) > 0
}

func hasAutoVision(body []byte) bool {
	return contentHasAutoVision(gjson.GetBytes(body, "messages")) ||
		contentHasAutoVision(gjson.GetBytes(body, "input"))
}

func contentHasAutoVision(messages gjson.Result) bool {
	if !messages.IsArray() {
		return false
	}
	for _, message := range messages.Array() {
		content := message.Get("content")
		if !content.IsArray() {
			continue
		}
		for _, part := range content.Array() {
			partType := part.Get("type").String()
			if part.Get("image_url").Exists() || part.Get("source").Exists() ||
				partType == "image" || partType == "input_image" || partType == "image_url" {
				return true
			}
		}
	}
	return false
}

func hasAutoJSON(body []byte) bool {
	switch gjson.GetBytes(body, "response_format.type").String() {
	case "json_object", "json_schema":
		return true
	default:
		return false
	}
}

func looksLikeVisionModel(name string) bool {
	lower := strings.ToLower(name)
	markers := []string{
		"gpt-4o", "gpt-4.1", "gpt-5", "omni",
		"claude", "gemini", "qwen-vl", "qwen2.5-vl", "qwen3-vl",
		"glm-4v", "glm-4.1v", "pixtral", "llama-4", "grok-2-vision", "vision",
	}
	for _, marker := range markers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func intersectAutoPool(pool, available []string) []string {
	if available == nil {
		return pool
	}
	allowed := make(map[string]struct{}, len(available))
	for _, name := range available {
		allowed[name] = struct{}{}
	}
	filtered := make([]string, 0, len(pool))
	for _, name := range pool {
		if _, ok := allowed[name]; ok {
			filtered = append(filtered, name)
		}
	}
	return filtered
}

func filterAutoPoolByToken(c *gin.Context, autoName string, pool []string) []string {
	if c == nil || !IsTokenModelAccessLimited(c) {
		return pool
	}
	if IsModelAllowedByToken(c, autoVirtualModel) || (autoName != autoVirtualModel && IsModelAllowedByToken(c, autoName)) {
		return pool
	}
	filtered := make([]string, 0, len(pool))
	for _, name := range pool {
		if IsModelAllowedByToken(c, name) {
			filtered = append(filtered, name)
		}
	}
	return filtered
}

func weightedAutoPick(pool []string, weights map[string]int) string {
	if len(pool) == 1 {
		return pool[0]
	}
	total := 0
	values := make([]int, len(pool))
	for i, name := range pool {
		weight := 1
		if weights != nil {
			if configured := weights[name]; configured > 0 {
				weight = configured
			}
		}
		values[i] = weight
		total += weight
	}
	if total <= 0 {
		return pool[0]
	}
	ticket := rand.IntN(total)
	for i, weight := range values {
		ticket -= weight
		if ticket < 0 {
			return pool[i]
		}
	}
	return pool[0]
}

func uniqueNonEmpty(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func getAutoStickyCache() *cachex.HybridCache[string] {
	autoStickyCacheOnce.Do(func() {
		autoStickyCache = cachex.NewHybridCache[string](cachex.HybridCacheConfig[string]{
			Namespace: cachex.Namespace(autoStickyCacheNamespace),
			Redis:     common.RDB,
			RedisEnabled: func() bool {
				return common.RedisEnabled && common.RDB != nil
			},
			RedisCodec: cachex.StringCodec{},
			Memory: func() *hot.HotCache[string, string] {
				return hot.NewHotCache[string, string](hot.LRU, autoStickyCacheCapacity).
					WithTTL(30 * time.Minute).
					WithJanitor().
					Build()
			},
		})
	})
	return autoStickyCache
}

func getAutoStickyModel(c *gin.Context, autoName string, body []byte, ttlSeconds int) string {
	if ttlSeconds <= 0 {
		return ""
	}
	value, found, err := getAutoStickyCache().Get(autoSessionKey(c, autoName, body))
	if err != nil || !found {
		return ""
	}
	return value
}

func setAutoStickyModel(c *gin.Context, autoName string, body []byte, modelName string, ttlSeconds int) {
	if ttlSeconds <= 0 || modelName == "" {
		return
	}
	_ = getAutoStickyCache().SetWithTTL(autoSessionKey(c, autoName, body), modelName, time.Duration(ttlSeconds)*time.Second)
}

func ReadAutoRequestBody(c *gin.Context) []byte {
	if c == nil {
		return nil
	}
	storage, err := common.GetBodyStorage(c)
	if err != nil || storage == nil {
		return nil
	}
	bytes, err := storage.Bytes()
	if err != nil {
		return nil
	}
	if _, seekErr := storage.Seek(0, io.SeekStart); seekErr == nil {
		c.Request.Body = io.NopCloser(storage)
	}
	return bytes
}
