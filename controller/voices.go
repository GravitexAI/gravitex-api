package controller

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/vertex"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

// RelayVoices 透传 GET /v1beta/voices：列出 TTS 音色库（扩展音色/复刻音色）。
//
// 该上游接口没有模型参数，因此网关要求通过 ?model= 查询参数指定一个
// 模型来选择渠道（例如 ?model=gemini-3.8-flash-tts），沿用该模型所属
// 渠道的凭据转发。其余查询参数（language_code/gender/pitch/context/
// type/search/page_size 等）原样转发，响应原样返回。列表查询不产生
// token 消耗，不做计费，只走令牌鉴权。
func RelayVoices(c *gin.Context) {
	modelName := strings.TrimSpace(c.Query("model"))
	if modelName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{
			"message": "query parameter model is required to select a channel, e.g. /v1beta/voices?model=gemini-3.8-flash-tts",
			"type":    "invalid_request_error",
		}})
		return
	}
	// 令牌设置了模型白名单时必须校验：否则受限令牌可以借 voices 探测
	// 任意模型在分组内的渠道可用性。
	if c.GetBool("token_model_limit_enabled") {
		allowed, _ := c.Get("token_model_limit")
		if limit, ok := allowed.(map[string]bool); ok && !limit[modelName] {
			c.JSON(http.StatusForbidden, gin.H{"error": gin.H{
				"message": fmt.Sprintf("model %s is not allowed for this token", modelName),
				"type":    "invalid_request_error",
			}})
			return
		}
	}

	usingGroup := common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
	channel, _, err := service.CacheGetRandomSatisfiedChannel(&service.RetryParam{
		Ctx:         c,
		ModelName:   modelName,
		TokenGroup:  usingGroup,
		RequestPath: c.Request.URL.Path,
		Retry:       common.GetPointer(0),
	})
	if err != nil || channel == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{
			"message": fmt.Sprintf("no available channel for model %s in group %s: %v", modelName, usingGroup, err),
			"type":    "api_error",
		}})
		return
	}

	upstreamURL, header, err := buildVoicesUpstreamRequest(channel)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{
			"message": err.Error(),
			"type":    "api_error",
		}})
		return
	}

	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, upstreamURL, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{
			"message": err.Error(),
			"type":    "api_error",
		}})
		return
	}
	for key, value := range header {
		req.Header[key] = value
	}
	// 转发除 model 外的全部查询参数
	query := req.URL.Query()
	for key, values := range c.Request.URL.Query() {
		if key == "model" {
			continue
		}
		for _, value := range values {
			query.Add(key, value)
		}
	}
	req.URL.RawQuery = query.Encode()

	client := service.GetHttpClient()
	resp, err := client.Do(req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": gin.H{
			"message": fmt.Sprintf("upstream voices request failed: %v", err),
			"type":    "api_error",
		}})
		return
	}
	defer resp.Body.Close()

	contentType := resp.Header.Get("Content-Type")
	if contentType != "" {
		c.Header("Content-Type", contentType)
	}
	c.Writer.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(c.Writer, resp.Body)
}

// buildVoicesUpstreamRequest 按渠道凭据类型构造上游 voices 地址与鉴权头。
// SA（服务账号 JSON）走 aiplatform 项目地址 + Bearer；API Key 走
// generativelanguage 风格地址 + x-goog-api-key。带自定义 base 的代理渠道
// 直接复用其 base。
func buildVoicesUpstreamRequest(channel *model.Channel) (string, http.Header, error) {
	key := firstChannelKey(channel.Key)
	if key == "" {
		return "", nil, fmt.Errorf("channel %d has no key", channel.Id)
	}
	base := strings.TrimRight(strings.TrimSpace(channel.GetBaseURL()), "/")

	var credentials vertex.Credentials
	if common.Unmarshal([]byte(key), &credentials) == nil && credentials.PrivateKey != "" {
		// Vertex 服务账号：项目级 aiplatform 地址 + OAuth Bearer。
		if strings.Contains(strings.ToLower(base), "generativelanguage.googleapis.com") {
			// SA 凭据绝不能发往 Gemini Developer API host。
			base = ""
		}
		upstreamURL := vertex.BuildAPIBaseURL(base, "v1beta1", credentials.ProjectID, "global") + "/voices"
		setting := channel.GetSetting()
		accessToken, err := voicesAccessToken(channel, credentials, setting.Proxy)
		if err != nil {
			return "", nil, fmt.Errorf("acquire vertex access token failed: %w", err)
		}
		header := http.Header{}
		header.Set("Authorization", "Bearer "+accessToken)
		if credentials.ProjectID != "" {
			header.Set("x-goog-user-project", credentials.ProjectID)
		}
		return upstreamURL, header, nil
	}

	// API Key 渠道：generativelanguage 风格地址。
	if base == "" || strings.Contains(strings.ToLower(base), "aiplatform.googleapis.com") {
		base = "https://generativelanguage.googleapis.com"
	}
	header := http.Header{}
	header.Set("x-goog-api-key", key)
	return base + "/v1beta/voices", header, nil
}

// voicesAccessToken 返回 SA 渠道的 OAuth 访问令牌。复用 vertex 包的
// asynccache（渠道粒度、30 分钟过期），避免每次 voices 查询都做一次
// RSA 签名 + 令牌交换。缓存键与 relay 流程区分开，避免多 key 渠道的
// 语义混淆。
func voicesAccessToken(channel *model.Channel, credentials vertex.Credentials, proxy string) (string, error) {
	cacheKey := fmt.Sprintf("access-token-voices-%d", channel.Id)
	if val, err := vertex.Cache.Get(cacheKey); err == nil {
		if token, ok := val.(string); ok {
			return token, nil
		}
	}
	token, err := vertex.AcquireAccessToken(credentials, proxy)
	if err != nil {
		return "", err
	}
	_ = vertex.Cache.SetDefault(cacheKey, token)
	return token, nil
}

func firstChannelKey(joined string) string {
	for _, line := range strings.Split(joined, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
