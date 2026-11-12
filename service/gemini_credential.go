package service

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"

	"golang.org/x/sync/singleflight"
)

const (
	// geminiOAuthRefreshMargin 提前刷新余量：覆盖 broker 耗时、在途请求、token 提前失效三段时间
	geminiOAuthRefreshMargin = 150 * time.Second
	// geminiOAuthDefaultLifetime broker 未返回任何过期字段时的兜底有效期
	geminiOAuthDefaultLifetime = 30 * time.Minute
	geminiOAuthBrokerTimeout   = 15 * time.Second
	geminiOAuthBrokerMaxBody   = 1 << 20
)

// GeminiAuth is the credential a Gemini (AI Studio endpoint) channel sends to Google.
type GeminiAuth struct {
	HeaderName  string // "x-goog-api-key" 或 "Authorization"
	HeaderValue string
	UserProject string // 非空时附带 x-goog-user-project（OAuth 用户凭证的配额项目）
}

// Apply writes the credential into an upstream request header.
// OAuth 模式下会删除 x-goog-api-key，保证 Google 只看到 Bearer 一种凭据。
func (a GeminiAuth) Apply(header http.Header) {
	if a.HeaderName != "x-goog-api-key" {
		header.Del("x-goog-api-key")
	}
	header.Set(a.HeaderName, a.HeaderValue)
	if a.UserProject != "" {
		header.Set("x-goog-user-project", a.UserProject)
	}
}

// Headers returns the credential as a plain header map, for download helpers that take map headers.
func (a GeminiAuth) Headers() map[string]string {
	headers := map[string]string{a.HeaderName: a.HeaderValue}
	if a.UserProject != "" {
		headers["x-goog-user-project"] = a.UserProject
	}
	return headers
}

// BearerToken returns the bare OAuth token, or "" for API key credentials.
func (a GeminiAuth) BearerToken() string {
	return strings.TrimPrefix(a.HeaderValue, "Bearer ")
}

// GeminiOAuthBrokerConfig is the channel key content for gemini_key_type=oauth_broker.
type GeminiOAuthBrokerConfig struct {
	BrokerURL   string            `json:"broker_url"`
	Method      string            `json:"method,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
	UserProject string            `json:"user_project,omitempty"`
}

// ParseGeminiOAuthBrokerConfig parses and validates an oauth_broker channel key.
func ParseGeminiOAuthBrokerConfig(rawKey string) (*GeminiOAuthBrokerConfig, error) {
	var cfg GeminiOAuthBrokerConfig
	if err := common.UnmarshalJsonStr(strings.TrimSpace(rawKey), &cfg); err != nil {
		return nil, fmt.Errorf("gemini oauth broker config must be a JSON object: %w", err)
	}
	cfg.BrokerURL = strings.TrimSpace(cfg.BrokerURL)
	parsed, err := url.Parse(cfg.BrokerURL)
	if err != nil || parsed.Host == "" || parsed.Scheme != "https" {
		return nil, errors.New("gemini oauth broker_url must be an https URL")
	}
	cfg.Method = strings.ToUpper(strings.TrimSpace(cfg.Method))
	if cfg.Method == "" {
		cfg.Method = http.MethodGet
	}
	if cfg.Method != http.MethodGet && cfg.Method != http.MethodPost {
		return nil, fmt.Errorf("gemini oauth broker method must be GET or POST, got %q", cfg.Method)
	}
	cfg.UserProject = strings.TrimSpace(cfg.UserProject)
	return &cfg, nil
}

// ResolveGeminiAuth resolves the upstream credential of a Gemini channel key.
// oauth_broker 模式返回缓存中的 token，到期前 150 秒自动向 broker 刷新。
func ResolveGeminiAuth(channelID int, rawKey string, keyType dto.GeminiKeyType, proxy string) (GeminiAuth, error) {
	switch keyType {
	case dto.GeminiKeyTypeOAuthToken:
		return GeminiAuth{HeaderName: "Authorization", HeaderValue: "Bearer " + strings.TrimSpace(rawKey)}, nil
	case dto.GeminiKeyTypeOAuthBroker:
		cfg, err := ParseGeminiOAuthBrokerConfig(rawKey)
		if err != nil {
			return GeminiAuth{}, err
		}
		token, err := geminiOAuthBrokerToken(channelID, rawKey, cfg, proxy)
		if err != nil {
			return GeminiAuth{}, err
		}
		return GeminiAuth{HeaderName: "Authorization", HeaderValue: "Bearer " + token, UserProject: cfg.UserProject}, nil
	default:
		return GeminiAuth{HeaderName: "x-goog-api-key", HeaderValue: rawKey}, nil
	}
}

// ResolveGeminiTaskAuth resolves the credential for follow-up calls of a submitted Gemini task (video download).
// 任务快照里的 ya29 token 半小时就过期：快照为空或是 oauth_token 时改用渠道当前的 key；broker 配置与 API Key 直接用快照。
func ResolveGeminiTaskAuth(channel *model.Channel, taskKey string) (GeminiAuth, error) {
	key := strings.TrimSpace(taskKey)
	if key == "" || InferGeminiKeyType(key) == dto.GeminiKeyTypeOAuthToken {
		if current, _, apiErr := channel.GetNextEnabledKey(); apiErr == nil && strings.TrimSpace(current) != "" {
			key = strings.TrimSpace(current)
		}
	}
	if key == "" {
		return GeminiAuth{}, fmt.Errorf("channel %d has no usable gemini key", channel.Id)
	}
	return ResolveGeminiAuth(channel.Id, key, InferGeminiKeyType(key), channel.GetDialProxy())
}

// InferGeminiKeyType infers the credential type from the stored key, for paths that only see the key
// (task polling, video download). broker 配置一定是 JSON 对象，Google OAuth access token 一定以 ya29. 开头。
func InferGeminiKeyType(rawKey string) dto.GeminiKeyType {
	trimmed := strings.TrimSpace(rawKey)
	switch {
	case strings.HasPrefix(trimmed, "{"):
		return dto.GeminiKeyTypeOAuthBroker
	case strings.HasPrefix(trimmed, "ya29."):
		return dto.GeminiKeyTypeOAuthToken
	default:
		return dto.GeminiKeyTypeAPIKey
	}
}

// InvalidateGeminiOAuthToken drops the cached broker token set if it still contains failedToken,
// so the next resolve refetches. 只在缓存仍是旧 token 组时才丢弃，避免把别的协程刚换好的新组冲掉。
func InvalidateGeminiOAuthToken(rawKey string, failedToken string) {
	cacheKey := geminiOAuthCacheKey(rawKey)
	value, ok := geminiOAuthTokenSets.Load(cacheKey)
	if !ok {
		return
	}
	set := value.(*geminiOAuthTokenSet)
	for _, token := range set.tokens {
		if token == failedToken {
			geminiOAuthTokenSets.CompareAndDelete(cacheKey, set)
			return
		}
	}
}

// geminiOAuthTokenSet 是 broker 一次返回的整组 token；刷新时整组替换（replace，非 append）
type geminiOAuthTokenSet struct {
	tokens    []string
	expiresAt time.Time
	next      atomic.Uint64
}

func (s *geminiOAuthTokenSet) fresh(now time.Time) bool {
	return now.Before(s.expiresAt.Add(-geminiOAuthRefreshMargin))
}

// pick 在组内轮询，多 token 分摊上游配额
func (s *geminiOAuthTokenSet) pick() string {
	return s.tokens[(s.next.Add(1)-1)%uint64(len(s.tokens))]
}

var (
	geminiOAuthTokenSets sync.Map // cacheKey -> *geminiOAuthTokenSet
	geminiOAuthRefresh   singleflight.Group
)

// geminiOAuthCacheKey 按 broker 配置内容哈希：配置一改缓存自然失效；
// 同一份配置在提交、轮询、下载等路径（以及多 key 的每一行）共享同一组 token
func geminiOAuthCacheKey(rawKey string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(rawKey)))
	return fmt.Sprintf("%x", sum[:8])
}

func geminiOAuthBrokerToken(channelID int, rawKey string, cfg *GeminiOAuthBrokerConfig, proxy string) (string, error) {
	cacheKey := geminiOAuthCacheKey(rawKey)
	if value, ok := geminiOAuthTokenSets.Load(cacheKey); ok {
		if set := value.(*geminiOAuthTokenSet); set.fresh(time.Now()) {
			return set.pick(), nil
		}
	}

	// single-flight：过期瞬间的并发请求只放一个去 broker，其余共享结果
	value, err, _ := geminiOAuthRefresh.Do(cacheKey, func() (interface{}, error) {
		if cached, ok := geminiOAuthTokenSets.Load(cacheKey); ok {
			if set := cached.(*geminiOAuthTokenSet); set.fresh(time.Now()) {
				return set, nil
			}
		}
		set, fetchErr := fetchGeminiOAuthTokenSet(cfg, proxy)
		if fetchErr != nil {
			return nil, fetchErr
		}
		geminiOAuthTokenSets.Store(cacheKey, set)
		return set, nil
	})
	if err != nil {
		// broker 暂时不可用但旧 token 还没真正过期时继续用旧的，不让一次 broker 抖动打断流量
		if cached, ok := geminiOAuthTokenSets.Load(cacheKey); ok {
			if set := cached.(*geminiOAuthTokenSet); time.Now().Before(set.expiresAt) {
				common.SysError(fmt.Sprintf("gemini oauth broker refresh failed for channel %d, using unexpired cached token: %v", channelID, err))
				return set.pick(), nil
			}
		}
		return "", err
	}
	return value.(*geminiOAuthTokenSet).pick(), nil
}

func fetchGeminiOAuthTokenSet(cfg *GeminiOAuthBrokerConfig, proxy string) (*geminiOAuthTokenSet, error) {
	client, err := GetHttpClientWithProxy(proxy)
	if err != nil {
		return nil, fmt.Errorf("gemini oauth broker http client: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), geminiOAuthBrokerTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, cfg.Method, cfg.BrokerURL, nil)
	if err != nil {
		return nil, fmt.Errorf("gemini oauth broker request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	for name, value := range cfg.Headers {
		req.Header.Set(name, value)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gemini oauth broker request failed: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, geminiOAuthBrokerMaxBody))
	if err != nil {
		return nil, fmt.Errorf("gemini oauth broker read body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gemini oauth broker returned status %d", resp.StatusCode)
	}
	return parseGeminiOAuthBrokerResponse(body, time.Now())
}

// geminiOAuthBrokerResponse 兼容 Google 体系常见的 token 返回结构：
// OAuth /token 的 access_token+expires_in、IAM Credentials 的 accessToken+expireTime，以及整组 tokens 数组。
type geminiOAuthBrokerResponse struct {
	AccessToken      string   `json:"access_token"`
	AccessTokenCamel string   `json:"accessToken"`
	Tokens           []string `json:"tokens"`
	ExpiresIn        *int64   `json:"expires_in"`
	ExpireTime       string   `json:"expireTime"`
	ExpireTimeSnake  string   `json:"expire_time"`
	Expiry           string   `json:"expiry"`
}

func parseGeminiOAuthBrokerResponse(body []byte, now time.Time) (*geminiOAuthTokenSet, error) {
	var payload geminiOAuthBrokerResponse
	if err := common.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("gemini oauth broker response is not valid JSON: %w", err)
	}

	tokens := make([]string, 0, len(payload.Tokens)+1)
	for _, token := range append(payload.Tokens, payload.AccessToken, payload.AccessTokenCamel) {
		if token = strings.TrimSpace(token); token != "" {
			tokens = append(tokens, token)
		}
	}
	if len(tokens) == 0 {
		return nil, errors.New("gemini oauth broker response contains no token")
	}

	expiresAt := now.Add(geminiOAuthDefaultLifetime)
	expireTime := payload.ExpireTime
	if expireTime == "" {
		expireTime = payload.ExpireTimeSnake
	}
	if expireTime == "" {
		expireTime = payload.Expiry
	}
	switch {
	case payload.ExpiresIn != nil && *payload.ExpiresIn > 0:
		expiresAt = now.Add(time.Duration(*payload.ExpiresIn) * time.Second)
	case expireTime != "":
		parsed, err := time.Parse(time.RFC3339, expireTime)
		if err != nil {
			return nil, fmt.Errorf("gemini oauth broker expire time %q is not RFC3339: %w", expireTime, err)
		}
		expiresAt = parsed
	default:
		common.SysLog("gemini oauth broker response has no expiry, assuming 30 minutes")
	}
	return &geminiOAuthTokenSet{tokens: tokens, expiresAt: expiresAt}, nil
}
