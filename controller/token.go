package controller

import (
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

// isAllowIpsBlank 判断出口 IP 白名单是否为空（nil、空串或仅含空白字符）。
func isAllowIpsBlank(allowIps *string) bool {
	return allowIps == nil || strings.TrimSpace(*allowIps) == ""
}

// tokenAutoGroupsInput 区分「请求没带 auto_groups」和「带了但为 null/空数组」两种语义：
// 前者保持原值不动，后者清空为继承用户分组。
type tokenAutoGroupsInput struct {
	Set    bool
	Groups []string
}

func (input *tokenAutoGroupsInput) UnmarshalJSON(data []byte) error {
	input.Set = true
	if strings.TrimSpace(string(data)) == "null" {
		input.Groups = nil
		return nil
	}
	return common.Unmarshal(data, &input.Groups)
}

type tokenRequest struct {
	model.Token
	AutoGroups tokenAutoGroupsInput `json:"auto_groups"`
}

// tokenResponse 把库里存 JSON 字符串的 auto_groups 以数组形式返回给前端。
type tokenResponse struct {
	*model.Token
	AutoGroups []string `json:"auto_groups"`
}

func maxTokenQuota() int {
	quota, err := common.WalletQuotaFromDecimalStrict(
		decimal.NewFromInt(1_000_000_000).Mul(decimal.NewFromFloat(common.QuotaPerUnit)),
	)
	if err != nil {
		return common.MaxWalletQuota
	}
	return quota
}

func buildMaskedTokenResponse(token *model.Token) *tokenResponse {
	if token == nil {
		return nil
	}
	maskedToken := *token
	maskedToken.Key = token.GetMaskedKey()
	autoGroups, err := token.GetAutoGroups()
	if err != nil {
		common.SysError(fmt.Sprintf("failed to parse auto groups for token %d: %v", token.Id, err))
		autoGroups = nil
	}
	if len(autoGroups) == 0 {
		autoGroups = nil
	}
	return &tokenResponse{Token: &maskedToken, AutoGroups: autoGroups}
}

func buildMaskedTokenResponses(tokens []*model.Token) []*tokenResponse {
	maskedTokens := make([]*tokenResponse, 0, len(tokens))
	for _, token := range tokens {
		maskedTokens = append(maskedTokens, buildMaskedTokenResponse(token))
	}
	return maskedTokens
}

// setTokenAutoGroups 校验并写入候选分组：数量上限、去重、必须是该用户可选分组。
func setTokenAutoGroups(c *gin.Context, token *model.Token, groups []string) bool {
	if len(groups) == 0 {
		if err := token.SetAutoGroups(nil); err != nil {
			common.ApiError(c, err)
			return false
		}
		return true
	}

	maxCount := setting.GetMaxTokenAutoGroups()
	if len(groups) > maxCount {
		common.ApiErrorI18n(c, i18n.MsgTokenAutoGroupsTooMany, map[string]any{"Max": maxCount})
		return false
	}

	userGroup, err := getTokenRequestUserGroup(c)
	if err != nil {
		common.ApiError(c, err)
		return false
	}
	seen := make(map[string]struct{}, len(groups))
	for _, group := range groups {
		if _, ok := seen[group]; ok {
			common.ApiErrorI18n(c, i18n.MsgTokenAutoGroupsDuplicate, map[string]any{"Group": group})
			return false
		}
		seen[group] = struct{}{}
		if !service.IsUserSelectableGroup(userGroup, group) {
			common.ApiErrorI18n(c, i18n.MsgTokenAutoGroupsInvalid, map[string]any{"Group": group})
			return false
		}
	}

	if err := token.SetAutoGroups(groups); err != nil {
		common.ApiError(c, err)
		return false
	}
	return true
}

func GetAllTokens(c *gin.Context) {
	userId := c.GetInt("id")
	pageInfo := common.GetPageQuery(c)
	tokens, err := model.GetAllUserTokens(userId, pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	total, _ := model.CountUserTokens(userId)
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(buildMaskedTokenResponses(tokens))
	common.ApiSuccess(c, pageInfo)
}

func SearchTokens(c *gin.Context) {
	userId := c.GetInt("id")
	keyword := c.Query("keyword")
	token := c.Query("token")

	pageInfo := common.GetPageQuery(c)

	tokens, total, err := model.SearchUserTokens(userId, keyword, token, pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(buildMaskedTokenResponses(tokens))
	common.ApiSuccess(c, pageInfo)
}

func GetToken(c *gin.Context) {
	id64, err := strconv.ParseInt(c.Param("id"), 10, 64)
	userId := c.GetInt("id")
	if err != nil {
		common.ApiError(c, err)
		return
	}
	token, err := model.GetTokenByIds(int(id64), userId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, buildMaskedTokenResponse(token))
}

// getTokenRequestUserGroup 取当前请求用户所属分组，优先用上下文里已解析的，避免重复查库。
func getTokenRequestUserGroup(c *gin.Context) (string, error) {
	if userGroup := common.GetContextKeyString(c, constant.ContextKeyUserGroup); userGroup != "" {
		return userGroup, nil
	}
	if userGroup := c.GetString("group"); userGroup != "" {
		return userGroup, nil
	}
	return model.GetUserGroup(c.GetInt("id"), false)
}

// GetTokenAutoGroups 返回 auto 分组下用户可选的候选分组列表及数量上限（官方自动分组功能的只读端点）。
func GetTokenAutoGroups(c *gin.Context) {
	userGroup, err := getTokenRequestUserGroup(c)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{
		"groups":    service.GetUserAutoGroup(userGroup),
		"max_count": setting.GetMaxTokenAutoGroups(),
	})
}

func GetTokenKey(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	userId := c.GetInt("id")
	if err != nil {
		common.ApiError(c, err)
		return
	}
	token, err := model.GetTokenByIds(id, userId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{
		"key": token.GetFullKey(),
	})
}

func GetTokenStatus(c *gin.Context) {
	tokenId := c.GetInt("token_id")
	userId := c.GetInt("id")
	token, err := model.GetTokenByIds(tokenId, userId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	expiredAt := token.ExpiredTime
	if expiredAt == -1 {
		expiredAt = 0
	}
	c.JSON(http.StatusOK, gin.H{
		"object":          "credit_summary",
		"total_granted":   token.RemainQuota,
		"total_used":      0, // not supported currently
		"total_available": token.RemainQuota,
		"expires_at":      expiredAt * 1000,
	})
}

func GetTokenUsage(c *gin.Context) {
	authHeader := c.GetHeader("Authorization")
	if authHeader == "" {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "No Authorization header",
		})
		return
	}

	parts := strings.Split(authHeader, " ")
	if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "Invalid Bearer token",
		})
		return
	}
	tokenKey := parts[1]

	token, err := model.GetTokenByKey(strings.TrimPrefix(tokenKey, "sk-"), false)
	if err != nil {
		common.SysError("failed to get token by key: " + err.Error())
		common.ApiErrorI18n(c, i18n.MsgTokenGetInfoFailed)
		return
	}

	expiredAt := token.ExpiredTime
	if expiredAt == -1 {
		expiredAt = 0
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    true,
		"message": "ok",
		"data": gin.H{
			"object":               "token_usage",
			"name":                 token.Name,
			"total_granted":        token.RemainQuota + token.UsedQuota,
			"total_used":           token.UsedQuota,
			"total_available":      token.RemainQuota,
			"unlimited_quota":      token.UnlimitedQuota,
			"model_limits":         token.GetModelLimitsMap(),
			"model_limits_enabled": token.ModelLimitsEnabled,
			"vendor_limits":        token.GetVendorLimitsMap(),
			"expires_at":           expiredAt,
		},
	})
}

// capTokenLimitsForEnterpriseSubAccount keeps the existing enterprise default
// for tokens without an explicit restriction. Explicit model/vendor limits are
// persisted as submitted and take precedence for that token.
func capTokenLimitsForEnterpriseSubAccount(userId int, submittedEnabled bool, submittedModelLimits string, submittedVendorLimits string) (bool, string, string) {
	allowed, restricted, err := model.GetSubAccountAllowedModelSet(userId)
	if err != nil {
		common.SysError("check enterprise allowed models failed: " + err.Error())
		return submittedEnabled, submittedModelLimits, submittedVendorLimits
	}
	if !restricted {
		return submittedEnabled, submittedModelLimits, submittedVendorLimits
	}
	if strings.TrimSpace(submittedModelLimits) != "" || strings.TrimSpace(submittedVendorLimits) != "" {
		return submittedEnabled, submittedModelLimits, submittedVendorLimits
	}

	effectiveSet := make(map[string]bool)
	hasSubmitted := false
	for _, m := range strings.Split(submittedModelLimits, ",") {
		m = strings.TrimSpace(m)
		if m == "" {
			continue
		}
		hasSubmitted = true
		if allowed[m] {
			effectiveSet[m] = true
		}
	}

	if !hasSubmitted {
		for m := range allowed {
			effectiveSet[m] = true
		}
	}
	effective := make([]string, 0, len(effectiveSet))
	for modelName := range effectiveSet {
		effective = append(effective, modelName)
	}
	slices.Sort(effective)
	return true, strings.Join(effective, ","), submittedVendorLimits
}

func AddToken(c *gin.Context) {
	request := tokenRequest{}
	err := c.ShouldBindJSON(&request)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	token := request.Token
	// 企业主账号（已开启限制）无权创建 API 密钥。
	// 判定依赖 Java 维护的企业表；查询出错时按"不受限"处理并记录日志，
	// 避免因企业表异常波及全体（含非企业）用户创建密钥。
	restricted, rerr := model.IsEnterpriseApikeyRestrictedOwner(c.GetInt("id"))
	if rerr != nil {
		common.SysError("check enterprise apikey restriction failed (AddToken): " + rerr.Error())
	} else if restricted {
		common.ApiErrorI18n(c, i18n.MsgEnterpriseOwnerApikeyForbidden)
		return
	}
	if len(token.Name) > 50 {
		common.ApiErrorI18n(c, i18n.MsgTokenNameTooLong)
		return
	}
	// 非无限额度时，检查额度值是否超出有效范围
	if !token.UnlimitedQuota {
		if token.RemainQuota < 0 {
			common.ApiErrorI18n(c, i18n.MsgTokenQuotaNegative)
			return
		}
		maxQuotaValue := maxTokenQuota()
		if token.RemainQuota > maxQuotaValue {
			common.ApiErrorI18n(c, i18n.MsgTokenQuotaExceedMax, map[string]any{"Max": maxQuotaValue})
			return
		}
	}
	// 检查用户令牌数量是否已达上限
	maxTokens := operation_setting.GetMaxUserTokens()
	count, err := model.CountUserTokens(c.GetInt("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if int(count) >= maxTokens {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": fmt.Sprintf("已达到最大令牌数量限制 (%d)", maxTokens),
		})
		return
	}
	// auto 分组才有候选分组；其余分组清空候选并关闭跨组重试
	if token.Group == "auto" {
		if !setTokenAutoGroups(c, &token, request.AutoGroups.Groups) {
			return
		}
	} else {
		token.CrossGroupRetry = false
		_ = token.SetAutoGroups(nil)
	}
	key, err := common.GenerateKey()
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgTokenGenerateFailed)
		common.SysLog("failed to generate token key: " + err.Error())
		return
	}
	cappedEnabled, cappedModelLimits, cappedVendorLimits := capTokenLimitsForEnterpriseSubAccount(c.GetInt("id"), token.ModelLimitsEnabled, token.ModelLimits, token.VendorLimits)
	cleanToken := model.Token{
		UserId:              c.GetInt("id"),
		Name:                token.Name,
		Remake:              token.Remake,
		Key:                 key,
		CreatedTime:         common.GetTimestamp(),
		AccessedTime:        common.GetTimestamp(),
		ExpiredTime:         token.ExpiredTime,
		RemainQuota:         token.RemainQuota,
		UnlimitedQuota:      token.UnlimitedQuota,
		ModelLimitsEnabled:  cappedEnabled,
		ModelLimits:         cappedModelLimits,
		VendorLimits:        cappedVendorLimits,
		AllowIps:            token.AllowIps,
		Group:               token.Group,
		CrossGroupRetry:     token.CrossGroupRetry,
		DailySpendThreshold: token.DailySpendThreshold,
		AutoGroups:          token.AutoGroups,
	}
	err = cleanToken.Insert()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	// 企业子账号敏感操作告警：新增 API 密钥。best-effort，绝不影响本次响应。
	userId := c.GetInt("id")
	// 企业子账号风险预警：新增的 API 密钥未设置出口 IP 白名单。best-effort，绝不影响本次响应。
	if isAllowIpsBlank(token.AllowIps) {
		gopool.Go(func() {
			service.NotifyRiskWarning(userId, cleanToken.Id, "create", "ip_whitelist_missing")
		})
	} else {
		gopool.Go(func() {
			service.NotifySensitiveOp(userId, cleanToken.Id, "create", "apikey_created")
		})
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
}

func DeleteToken(c *gin.Context) {
	id64, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	id := int(id64)
	userId := c.GetInt("id")
	err := model.DeleteTokenById(id, userId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	// 删除后仍传入当前 token ID，由 Java 侧按 token_id 生成邮件内容。
	gopool.Go(func() {
		service.NotifySensitiveOp(userId, id, "delete", "apikey_deleted")
	})
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
}

func UpdateToken(c *gin.Context) {
	userId := c.GetInt("id")
	statusOnly := c.Query("status_only")
	type updateTokenRequest struct {
		Id                  common.Int64Flexible `json:"id"`
		Status              int                  `json:"status"`
		Name                string               `json:"name"`
		Remake              *string              `json:"remake"`
		ExpiredTime         int64                `json:"expired_time"`
		RemainQuota         int                  `json:"remain_quota"`
		UnlimitedQuota      bool                 `json:"unlimited_quota"`
		ModelLimitsEnabled  bool                 `json:"model_limits_enabled"`
		ModelLimits         string               `json:"model_limits"`
		VendorLimits        string               `json:"vendor_limits"`
		AllowIps            *string              `json:"allow_ips"`
		Group               string               `json:"group"`
		CrossGroupRetry     bool                 `json:"cross_group_retry"`
		DailySpendThreshold int                  `json:"daily_spend_threshold"`
		AutoGroups          tokenAutoGroupsInput `json:"auto_groups"`
	}

	var req updateTokenRequest
	err := c.ShouldBindJSON(&req)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	// 企业主账号（已开启限制）无权修改 API 密钥；仅启用/禁用（status_only）不拦截
	if statusOnly != "true" {
		restricted, rerr := model.IsEnterpriseApikeyRestrictedOwner(userId)
		if rerr != nil {
			common.SysError("check enterprise apikey restriction failed (UpdateToken): " + rerr.Error())
		} else if restricted {
			common.ApiErrorI18n(c, i18n.MsgEnterpriseOwnerApikeyForbidden)
			return
		}
	}

	cappedEnabled, cappedModelLimits, cappedVendorLimits := capTokenLimitsForEnterpriseSubAccount(userId, req.ModelLimitsEnabled, req.ModelLimits, req.VendorLimits)
	token := model.Token{
		Id:                  req.Id.Int(),
		Status:              req.Status,
		Name:                req.Name,
		ExpiredTime:         req.ExpiredTime,
		RemainQuota:         req.RemainQuota,
		UnlimitedQuota:      req.UnlimitedQuota,
		ModelLimitsEnabled:  cappedEnabled,
		ModelLimits:         cappedModelLimits,
		VendorLimits:        cappedVendorLimits,
		AllowIps:            req.AllowIps,
		Group:               req.Group,
		CrossGroupRetry:     req.CrossGroupRetry,
		DailySpendThreshold: req.DailySpendThreshold,
	}

	if token.Id == 0 {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if len(token.Name) > 50 {
		common.ApiErrorI18n(c, i18n.MsgTokenNameTooLong)
		return
	}
	if !token.UnlimitedQuota {
		if token.RemainQuota < 0 {
			common.ApiErrorI18n(c, i18n.MsgTokenQuotaNegative)
			return
		}
		maxQuotaValue := maxTokenQuota()
		if token.RemainQuota > maxQuotaValue {
			common.ApiErrorI18n(c, i18n.MsgTokenQuotaExceedMax, map[string]any{"Max": maxQuotaValue})
			return
		}
	}
	cleanToken, err := model.GetTokenByIds(token.Id, userId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if token.Status == common.TokenStatusEnabled {
		if cleanToken.Status == common.TokenStatusExpired && cleanToken.ExpiredTime <= common.GetTimestamp() && cleanToken.ExpiredTime != -1 {
			common.ApiErrorI18n(c, i18n.MsgTokenExpiredCannotEnable)
			return
		}
		if cleanToken.Status == common.TokenStatusExhausted && cleanToken.RemainQuota <= 0 && !cleanToken.UnlimitedQuota {
			common.ApiErrorI18n(c, i18n.MsgTokenExhaustedCannotEable)
			return
		}
	}
	if statusOnly != "" {
		cleanToken.Status = token.Status
	} else {
		// If you add more fields, please also update token.Update()
		cleanToken.Name = token.Name
		if req.Remake != nil {
			cleanToken.Remake = *req.Remake
		}
		cleanToken.ExpiredTime = token.ExpiredTime
		cleanToken.RemainQuota = token.RemainQuota
		cleanToken.UnlimitedQuota = token.UnlimitedQuota
		cleanToken.ModelLimitsEnabled = token.ModelLimitsEnabled
		cleanToken.ModelLimits = token.ModelLimits
		cleanToken.VendorLimits = token.VendorLimits
		cleanToken.AllowIps = token.AllowIps
		cleanToken.Group = token.Group
		cleanToken.CrossGroupRetry = token.CrossGroupRetry
		cleanToken.DailySpendThreshold = token.DailySpendThreshold
		// auto_groups 三态：非 auto 分组一律清空并关闭跨组重试；
		// auto 分组下只有请求显式带了字段才改，没带就保持原值。
		if token.Group != "auto" {
			cleanToken.CrossGroupRetry = false
			_ = cleanToken.SetAutoGroups(nil)
		} else if req.AutoGroups.Set {
			if !setTokenAutoGroups(c, cleanToken, req.AutoGroups.Groups) {
				return
			}
		}
	}
	err = cleanToken.Update()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	// 企业子账号敏感操作告警：更新出口 IP 白名单。仅当本次请求非纯状态更新且
	// 携带了 allow_ips 字段时触发；不区分"是否真的发生变化"，best-effort，绝不影响本次响应。
	if statusOnly != "true" && req.AllowIps != nil {
		// 企业子账号风险预警：更新后的 API 密钥未设置出口 IP 白名单。best-effort，绝不影响本次响应。
		if isAllowIpsBlank(req.AllowIps) {
			gopool.Go(func() {
				service.NotifyRiskWarning(userId, cleanToken.Id, "update", "ip_whitelist_missing")
			})
		} else {
			gopool.Go(func() {
				service.NotifySensitiveOp(userId, cleanToken.Id, "update", "apikey_updated")
			})
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    buildMaskedTokenResponse(cleanToken),
	})
}

type TokenBatch struct {
	Ids []int `json:"ids"`
}

func DeleteTokenBatch(c *gin.Context) {
	tokenBatch := TokenBatch{}
	if err := c.ShouldBindJSON(&tokenBatch); err != nil || len(tokenBatch.Ids) == 0 {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	userId := c.GetInt("id")
	count, err := model.BatchDeleteTokens(tokenBatch.Ids, userId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    count,
	})
}

func GetTokenKeysBatch(c *gin.Context) {
	tokenBatch := TokenBatch{}
	if err := c.ShouldBindJSON(&tokenBatch); err != nil || len(tokenBatch.Ids) == 0 {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if len(tokenBatch.Ids) > 100 {
		common.ApiErrorI18n(c, i18n.MsgBatchTooMany, map[string]any{"Max": 100})
		return
	}
	userId := c.GetInt("id")
	tokens, err := model.GetTokenKeysByIds(tokenBatch.Ids, userId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	keysMap := make(map[int]string)
	for _, t := range tokens {
		keysMap[t.Id] = t.GetFullKey()
	}
	common.ApiSuccess(c, gin.H{"keys": keysMap})
}
