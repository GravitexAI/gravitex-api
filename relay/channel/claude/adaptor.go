package claude

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

type Adaptor struct {
}

func (a *Adaptor) ConvertGeminiRequest(*gin.Context, *relaycommon.RelayInfo, *dto.GeminiChatRequest) (any, error) {
	//TODO implement me
	return nil, errors.New("not implemented")
}

func (a *Adaptor) ConvertClaudeRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.ClaudeRequest) (any, error) {
	// Native /v1/messages 路径保持透传：不做 top_p/temperature/top_k 剥离、
	// thinking.type 转换或 tool_choice 降级。这些兼容性处理只在 OpenAI→Claude
	// (RequestOpenAI2ClaudeMessage) 路径执行，用于屏蔽 OpenAI 客户端不感知的
	// Claude 限制。直连 /v1/messages 的调用方应自行遵循 Anthropic 协议规范，
	// 不符合时由上游统一返回 400。
	stripClaudeRequestFieldsForDroppedBetas(c, info, request)
	// 渠道类型是 Anthropic，但 anthropic_beta_target 指向 Bedrock/Vertex 兼容
	// 资源时，image/document 的 URL source 同样需要转 base64（这两个目标硬性
	// 不支持 URL source），否则会被上游拒绝。直连 Anthropic 场景保持透传。
	if ResolveBetaTarget(info.ChannelType, info.ChannelOtherSettings.AnthropicBetaTarget) != TargetAnthropicDirect {
		if err := ConvertURLSourcesToBase64(c, request); err != nil {
			return nil, err
		}
	}
	return request, nil
}

// stripClaudeRequestFieldsForDroppedBetas 处理"渠道类型是 Anthropic，但实际接的是
// Bedrock/Vertex 等兼容资源"的场景：渠道会通过 anthropic_beta_target 覆盖白名单过滤
// 目标（如 "bedrock-converse"）。按最终会发给上游的 anthropic-beta（客户端声明 + admin
// 在 model_headers_settings 配置的值合并后过滤，与 CommonClaudeHeadersOperation 写请求
// 头时用的是同一份计算逻辑），把对应 beta 被剔除的顶级 body 字段一并清理，否则这些兼容
// 资源会返回 "Extra inputs are not permitted"。目标为直连 Anthropic（默认）时不做任何
// 处理，不会误伤任何字段。
func stripClaudeRequestFieldsForDroppedBetas(c *gin.Context, info *relaycommon.RelayInfo, request *dto.ClaudeRequest) {
	target := ResolveBetaTarget(info.ChannelType, info.ChannelOtherSettings.AnthropicBetaTarget)
	if target == TargetAnthropicDirect {
		return
	}
	kept := KeptBetaSet(resolveEffectiveBetaFlags(c, info, target))
	request.ContextManagement = StripContextManagementForDroppedBetas(request.ContextManagement, kept)
	request.OutputConfig = StripOutputConfigForDroppedBetas(request.OutputConfig, kept)
}

// resolveEffectiveBetaFlags 复现 CommonClaudeHeadersOperation 里的合并逻辑（客户端声明
// + admin 在 model_headers_settings 为该模型配置的值），再按目标过滤，得到最终会发给
// 上游的 anthropic-beta flag 列表。
func resolveEffectiveBetaFlags(c *gin.Context, info *relaycommon.RelayInfo, target FilterTarget) []string {
	header := http.Header{}
	if anthropicBeta := c.Request.Header.Get("anthropic-beta"); anthropicBeta != "" {
		header.Set("anthropic-beta", anthropicBeta)
	}
	model_setting.GetClaudeSettings().WriteHeaders(info.OriginModelName, &header)
	merged := header.Get("anthropic-beta")
	if merged == "" {
		return nil
	}
	return FilterBetaFlags(merged, target, info.RequestId)
}

func (a *Adaptor) ConvertAudioRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.AudioRequest) (io.Reader, error) {
	//TODO implement me
	return nil, errors.New("not implemented")
}

func (a *Adaptor) ConvertImageRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.ImageRequest) (any, error) {
	//TODO implement me
	return nil, errors.New("not implemented")
}

func (a *Adaptor) Init(info *relaycommon.RelayInfo) {
}

func (a *Adaptor) GetRequestURL(info *relaycommon.RelayInfo) (string, error) {
	requestURL := fmt.Sprintf("%s/v1/messages", info.ChannelBaseUrl)
	if !shouldAppendClaudeBetaQuery(info) {
		return requestURL, nil
	}

	parsedURL, err := url.Parse(requestURL)
	if err != nil {
		return "", err
	}
	query := parsedURL.Query()
	query.Set("beta", "true")
	parsedURL.RawQuery = query.Encode()
	return parsedURL.String(), nil
}

func shouldAppendClaudeBetaQuery(info *relaycommon.RelayInfo) bool {
	if info == nil {
		return false
	}
	if info.IsClaudeBetaQuery {
		return true
	}
	if info.ChannelOtherSettings.ClaudeBetaQuery {
		return true
	}
	return false
}

func CommonClaudeHeadersOperation(c *gin.Context, req *http.Header, info *relaycommon.RelayInfo) {
	// 透传客户端 anthropic-beta 到 req，让 WriteHeaders 能合并 admin 配置。
	anthropicBeta := c.Request.Header.Get("anthropic-beta")
	if anthropicBeta != "" {
		req.Set("anthropic-beta", anthropicBeta)
	}
	// 合并 admin 在 model_headers_settings 里为该模型配置的 anthropic-beta（如果有）。
	model_setting.GetClaudeSettings().WriteHeaders(info.OriginModelName, req)

	// 在 WriteHeaders 之后做白名单过滤：客户端原值 + admin 配置都过同一道闸，
	// 保证最终发出的 flag 都在目标渠道（Bedrock / Vertex / 直连）的支持列表内。
	// 否则 admin 在 model_headers_settings 配的 flag 会绕过过滤直达上游。
	// 与 stripClaudeRequestFieldsForDroppedBetas 共用 resolveEffectiveBetaFlags，
	// 保证请求头与 body 顶级字段的裁剪判断一致。
	if req.Get("anthropic-beta") != "" {
		target := ResolveBetaTarget(info.ChannelType, info.ChannelOtherSettings.AnthropicBetaTarget)
		filtered := resolveEffectiveBetaFlags(c, info, target)
		if len(filtered) > 0 {
			req.Set("anthropic-beta", strings.Join(filtered, ","))
		} else {
			req.Del("anthropic-beta")
		}
	}
}

func (a *Adaptor) SetupRequestHeader(c *gin.Context, req *http.Header, info *relaycommon.RelayInfo) error {
	channel.SetupApiRequestHeader(info, c, req)
	req.Set("x-api-key", info.ApiKey)
	anthropicVersion := c.Request.Header.Get("anthropic-version")
	if anthropicVersion == "" {
		anthropicVersion = "2023-06-01"
	}
	req.Set("anthropic-version", anthropicVersion)
	CommonClaudeHeadersOperation(c, req, info)
	return nil
}

func (a *Adaptor) ConvertOpenAIRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest) (any, error) {
	if request == nil {
		return nil, errors.New("request is nil")
	}
	return RequestOpenAI2ClaudeMessage(c, *request)
}

func (a *Adaptor) ConvertRerankRequest(c *gin.Context, relayMode int, request dto.RerankRequest) (any, error) {
	return nil, nil
}

func (a *Adaptor) ConvertEmbeddingRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.EmbeddingRequest) (any, error) {
	//TODO implement me
	return nil, errors.New("not implemented")
}

func (a *Adaptor) ConvertOpenAIResponsesRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.OpenAIResponsesRequest) (any, error) {
	// TODO implement me
	return nil, errors.New("not implemented")
}

func (a *Adaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (any, error) {
	return channel.DoApiRequest(a, c, info, requestBody)
}

func (a *Adaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (usage any, err *types.NewAPIError) {
	info.FinalRequestRelayFormat = types.RelayFormatClaude
	if info.IsStream {
		return ClaudeStreamHandler(c, resp, info)
	} else {
		return ClaudeHandler(c, resp, info)
	}
}

func (a *Adaptor) GetModelList() []string {
	return ModelList
}

func (a *Adaptor) GetChannelName() string {
	return ChannelName
}
