package mockai

import (
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relay/channel/claude"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/gin-gonic/gin"
)

const ChannelName = "mock_ai"

// wireBuilder 合成一次响应的上游字节。三个端点各有一个实现，签名保持一致。
type wireBuilder func(r mockRoll, answer, model string, stream bool) [][]byte

// globalRollSource 是生产随机源。math/rand 的包级函数自带锁，可并发调用。
type globalRollSource struct{}

func (globalRollSource) Intn(n int) int { return rand.Intn(n) }

var defaultRollSource rollSource = globalRollSource{}

// Adaptor 是 Mock AI 渠道的适配器。它不发任何上游请求，而是就地合成与真实上游
// 逐字节同构的响应，再交给未经改动的真实响应处理器去解析、聚合、计费。
//
// 刻意不持有任何可变状态：端点门禁由 DoRequest 与 DoResponse 各自调用纯函数
// wireBuilderFor 判定，而不是靠 DoRequest 置位、DoResponse 消费的字段。
// 后者会隐式要求 GetAdaptor 每次返回新实例（今天成立，但没有任何东西拦住将来
// 有人加 adaptor 缓存），也会让两处的 case 集合可能不同步。
type Adaptor struct{}

func (a *Adaptor) Init(info *relaycommon.RelayInfo) {}

func (a *Adaptor) GetRequestURL(info *relaycommon.RelayInfo) (string, error) {
	return "", nil
}

func (a *Adaptor) SetupRequestHeader(c *gin.Context, req *http.Header, info *relaycommon.RelayInfo) error {
	return nil
}

func (a *Adaptor) ConvertOpenAIRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest) (any, error) {
	return request, nil
}

func (a *Adaptor) ConvertRerankRequest(c *gin.Context, relayMode int, request dto.RerankRequest) (any, error) {
	return request, nil
}

func (a *Adaptor) ConvertEmbeddingRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.EmbeddingRequest) (any, error) {
	return request, nil
}

// ConvertAudioRequest 没有「原样返回」的形态可言（入参不是 io.Reader）。
// 音频端点不在支持范围内，请求会在 DoResponse 拿到 404。
func (a *Adaptor) ConvertAudioRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.AudioRequest) (io.Reader, error) {
	return nil, nil
}

func (a *Adaptor) ConvertImageRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.ImageRequest) (any, error) {
	return request, nil
}

func (a *Adaptor) ConvertOpenAIResponsesRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.OpenAIResponsesRequest) (any, error) {
	return request, nil
}

func (a *Adaptor) ConvertClaudeRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.ClaudeRequest) (any, error) {
	return request, nil
}

func (a *Adaptor) ConvertGeminiRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeminiChatRequest) (any, error) {
	return request, nil
}

func (a *Adaptor) GetModelList() []string { return nil }

func (a *Adaptor) GetChannelName() string { return ChannelName }

// DoRequest 不发出任何网络请求，而是合成一个真的 *http.Response，
// 它的 Body 按配置的时延节奏吐出上游字节。
func (a *Adaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (any, error) {
	cfg := info.ChannelOtherSettings.MockAI
	// 保存时已经校验过，这里再校验一次是因为有人可以直接改数据库绕过保存路径。
	// 非法区间会让 roll 里的 Intn 收到 <= 0 的参数而 panic，而这些数字直接
	// 扣用户额度，不能靠 clamp 让坏配置静默通过。
	if cfg != nil {
		if err := cfg.Validate(); err != nil {
			return nil, fmt.Errorf("invalid mock_ai channel config: %w", err)
		}
	}

	// 端点未启用时返回 (nil, nil)，由 DoResponse 返 404：DoRequest 返回的 error
	// 会被 relay handler 无条件包成 500（见 relay/compatible_handler.go），
	// 404 出不去，而 DoResponse 的返回类型本就是 *types.NewAPIError。
	build := wireBuilderFor(info.RelayFormat, info.RelayMode, endpointsOf(cfg))
	if build == nil {
		return nil, nil
	}

	// 档位查找用重定向之前的原始模型名：Mock 没有真实上游，前端配置下拉框只列
	// 渠道「模型」字段里的原始名，用上游名会在配了 model_mapping 后永远落空。
	r := roll(resolveProfile(cfg, info.OriginModelName), defaultRollSource)
	answer := mockAnswers[defaultRollSource.Intn(len(mockAnswers))]
	frames := build(r, answer, info.UpstreamModelName, info.IsStream)

	totalDelay := time.Duration(r.LatencyMs) * time.Millisecond
	firstDelay := totalDelay
	contentType := "application/json"
	contentLength := int64(0)
	if info.IsStream {
		firstDelay = time.Duration(r.FirstTokenMs) * time.Millisecond
		contentType = "text/event-stream"
		contentLength = -1
	} else {
		for _, frame := range frames {
			contentLength += int64(len(frame))
		}
	}

	// 渠道测试与定时健康检查按墙钟耗时判定并可能自动禁用渠道，
	// 模拟时延在这两条路径上必须跳过。
	if common.GetContextKeyBool(c, constant.ContextKeyChannelTesting) {
		firstDelay, totalDelay = 0, 0
	}

	header := http.Header{}
	header.Set("Content-Type", contentType)
	return &http.Response{
		Status:        "200 OK",
		StatusCode:    http.StatusOK,
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        header,
		ContentLength: contentLength,
		Body:          newPacedBody(c.Request.Context(), frames, firstDelay, totalDelay, nil),
	}, nil
}

// DoResponse 把合成的字节交给真实的响应处理器，Mock 自己不解析、不聚合、不算用量。
func (a *Adaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (any, *types.NewAPIError) {
	if wireBuilderFor(info.RelayFormat, info.RelayMode, endpointsOf(info.ChannelOtherSettings.MockAI)) == nil {
		// 形态照 relay/responses_handler.go 的先例：这是预期内的端点能力缺失，
		// 不是供应商故障，所以不记错误日志、不重试。
		return nil, types.WithOpenAIError(types.OpenAIError{
			Type:    "not_found_error",
			Code:    "endpoint_not_found",
			Message: fmt.Sprintf("Requested endpoint %s is not enabled on this Mock AI channel.", info.RequestURLPath),
		},
			http.StatusNotFound,
			types.ErrOptionWithSkipRetry(),
			types.ErrOptionWithNoRecordErrorLog(),
		)
	}

	switch info.RelayFormat {
	case types.RelayFormatOpenAI:
		if info.IsStream {
			usage, err := openai.OaiStreamHandler(c, info, resp)
			if err != nil {
				return nil, err
			}
			return usage, nil
		}
		usage, err := openai.OpenaiHandler(c, info, resp)
		if err != nil {
			return nil, err
		}
		return usage, nil
	case types.RelayFormatOpenAIResponses:
		if info.IsStream {
			usage, err := openai.OaiResponsesStreamHandler(c, info, resp)
			if err != nil {
				return nil, err
			}
			return usage, nil
		}
		usage, err := openai.OaiResponsesHandler(c, info, resp)
		if err != nil {
			return nil, err
		}
		return usage, nil
	case types.RelayFormatClaude:
		if info.IsStream {
			usage, err := claude.ClaudeStreamHandler(c, resp, info)
			if err != nil {
				return nil, err
			}
			return usage, nil
		}
		usage, err := claude.ClaudeHandler(c, resp, info)
		if err != nil {
			return nil, err
		}
		return usage, nil
	}
	// 不可达：上面的 wireBuilderFor 守卫已经把不支持的格式挡成 404。
	return nil, types.NewError(fmt.Errorf("mock_ai: unsupported relay format %q", info.RelayFormat), types.ErrorCodeInvalidApiType)
}

// endpointsOf 返回渠道实际勾选的端点。配置整体缺失时放开全部三个端点，
// 与 common.GetEndpointTypesByChannelType 的无配置兜底保持一致。
func endpointsOf(cfg *dto.MockAIConfig) dto.MockAIEndpoints {
	if cfg == nil {
		return dto.MockAIEndpoints{ChatCompletions: true, Responses: true, Messages: true}
	}
	return cfg.Endpoints
}

// wireBuilderFor 把请求格式映射到字节合成器。格式不支持、或对应端点没勾选时返回 nil，
// 调用方据此返回 404。这是一个纯函数，DoRequest 与 DoResponse 各自调用它判定门禁，
// 因此不存在两处 case 集合不同步的可能。新增端点时在这里加一个 case 即可。
func wireBuilderFor(format types.RelayFormat, relayMode int, endpoints dto.MockAIEndpoints) wireBuilder {
	switch format {
	case types.RelayFormatOpenAI:
		// RelayFormatOpenAI 同时覆盖 /v1/chat/completions 与 /v1/completions
		// （router/relay-router.go 两条路由传的是同一个 format），必须再看 RelayMode。
		// /v1/completions 不在支持范围内（设计 §2），不能拿 chat.completion 的形状
		// 去冒充 text_completion —— 那会直接破坏「与真实上游逐字节同构」这条核心契约。
		if relayMode == relayconstant.RelayModeChatCompletions && endpoints.ChatCompletions {
			return buildOpenAIChatFrames
		}
	case types.RelayFormatOpenAIResponses:
		// Responses 格式只对应 /v1/responses 一条路由，不需要再判 RelayMode。
		if endpoints.Responses {
			return buildResponsesFrames
		}
	case types.RelayFormatClaude:
		// Claude 格式只对应 /v1/messages 一条路由，不需要再判 RelayMode。
		if endpoints.Messages {
			return buildClaudeMessagesFrames
		}
	}
	return nil
}
