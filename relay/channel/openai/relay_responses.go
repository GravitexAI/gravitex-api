package openai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func OaiResponsesHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	defer service.CloseResponseBodyGracefully(resp)

	// read response body
	var responsesResponse dto.OpenAIResponsesResponse
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}
	err = common.Unmarshal(responseBody, &responsesResponse)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	if oaiError := responsesResponse.GetOpenAIError(); oaiError != nil && oaiError.Type != "" {
		return nil, types.WithOpenAIError(*oaiError, resp.StatusCode)
	}
	info.UpstreamResponseId = responsesResponse.ID
	info.CaptureUpstreamServiceTier(responseBody)

	// 写入新的 response body（SGLang 时间戳修正 + Meta 的整数值浮点时间戳归一化为整数再透传）
	responseBody = rewriteSGLangResponsesCreatedAt(info, responseBody, "created_at", responsesResponse.CreatedAt)
	responseBody = normalizeResponsesTimestamps(responseBody)
	service.IOCopyBytesGracefully(c, resp, responseBody)

	// compute usage
	usage := dto.Usage{}
	if responsesResponse.Usage != nil {
		// 官方 ApplyResponsesUsage：完整复制输入明细（含 12h 缓存写入、视频、缓存图片拆分）
		service.ApplyResponsesUsage(&usage, responsesResponse.Usage)
		if service.ValidUsage(&usage) {
			info.SetUpstreamResponsesField("usage", responsesResponse.Usage)
		}
	}
	// Count actual tool invocations from Output (not tool declarations).
	for _, output := range responsesResponse.Output {
		switch output.Type {
		case dto.BuildInCallWebSearchCall:
			info.CountBillableToolCall(dto.BuildInCallWebSearchCall, "")
		case dto.BuildInCallFileSearchCall:
			info.CountBillableToolCall(dto.BuildInCallFileSearchCall, "")
		case dto.BuildInCallFunctionCall:
			info.CountBillableToolCall(dto.BuildInCallFunctionCall, output.Name)
		}
	}

	imageCounter := &relaycommon.ImageGenerationCallCounter{}
	if !relaycommon.IsNonBillableResponsesStatus(responsesResponse.Status) {
		for i := range responsesResponse.Output {
			idx := i
			imageCounter.Observe(&responsesResponse.Output[i], &idx)
		}
	}
	imageCounter.Commit(info)

	return &usage, nil
}

func OaiResponsesStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		logger.LogError(c, "invalid response or response body")
		return nil, types.NewError(fmt.Errorf("invalid response"), types.ErrorCodeBadResponse)
	}

	defer service.CloseResponseBodyGracefully(resp)

	var usage = &dto.Usage{}
	var responseTextBuilder strings.Builder
	imageCounter := &relaycommon.ImageGenerationCallCounter{}
	imageCommitted := false
	// 在收到第一个实质内容事件前，先缓存开头事件、不写给客户端；
	// 若此时上游失败/空流结束，客户端尚未收到任何数据，可安全走重试换渠道。
	type pendingEvent struct {
		resp dto.ResponsesStreamResponse
		data string
	}
	var pendingEvents []pendingEvent
	flushed := false
	var upstreamFailure error

	helper.StreamScannerHandler(c, resp, info, func(data string, sr *helper.StreamResult) {

		// 检查当前数据是否包含 completed 状态和 usage 信息
		var streamResponse dto.ResponsesStreamResponse
		if err := common.UnmarshalJsonStr(data, &streamResponse); err != nil {
			logger.LogError(c, "failed to unmarshal stream response: "+err.Error())
			sr.Error(err)
			return
		}
		if !flushed {
			switch streamResponse.Type {
			case "response.created", "response.in_progress", "response.queued":
				pendingEvents = append(pendingEvents, pendingEvent{streamResponse, data})
				return
			case "error", "response.failed":
				upstreamFailure = fmt.Errorf("upstream %s before any output: %s", streamResponse.Type, data)
				sr.Stop(upstreamFailure)
				return
			}
			for _, event := range pendingEvents {
				sendResponsesStreamData(c, event.resp, event.data)
			}
			pendingEvents = nil
			flushed = true
		}
		if streamResponse.Response != nil {
			data = string(rewriteSGLangResponsesCreatedAt(info, []byte(data), "response.created_at", streamResponse.Response.CreatedAt))
		}
		sendResponsesStreamData(c, streamResponse, data)
		switch streamResponse.Type {
		case "response.completed", "response.done":
			if streamResponse.Response != nil {
				info.UpstreamResponseId = streamResponse.Response.ID
				info.CaptureUpstreamServiceTier(common.StringToByteSlice(data))
				if streamResponse.Response.Usage != nil {
					// 官方 ApplyResponsesUsage：只合并非零值，完整复制输入明细
					service.ApplyResponsesUsage(usage, streamResponse.Response.Usage)
					if service.ValidUsage(usage) {
						info.SetUpstreamResponsesField("usage", streamResponse.Response.Usage)
					}
				}
				if !imageCommitted {
					if relaycommon.IsNonBillableResponsesStatus(streamResponse.Response.Status) {
						imageCounter.Reset()
						imageCounter.Commit(info)
						imageCommitted = true
					} else {
						for i := range streamResponse.Response.Output {
							idx := i
							imageCounter.Observe(&streamResponse.Response.Output[i], &idx)
						}
						imageCounter.Commit(info)
						imageCommitted = true
					}
				}
			} else if !imageCommitted {
				imageCounter.Commit(info)
				imageCommitted = true
			}
		case "response.failed", "response.incomplete", "response.cancelled", "response.canceled":
			if !imageCommitted {
				imageCounter.Reset()
				imageCounter.Commit(info)
				imageCommitted = true
			}
		case "response.output_text.delta":
			// 处理输出文本
			responseTextBuilder.WriteString(streamResponse.Delta)
		case dto.ResponsesOutputTypeItemDone:
			if streamResponse.Item != nil {
				switch streamResponse.Item.Type {
				case dto.BuildInCallWebSearchCall:
					info.CountBillableToolCall(dto.BuildInCallWebSearchCall, "")
				case dto.BuildInCallFileSearchCall:
					info.CountBillableToolCall(dto.BuildInCallFileSearchCall, "")
				case dto.BuildInCallFunctionCall:
					info.CountBillableToolCall(dto.BuildInCallFunctionCall, streamResponse.Item.Name)
				case dto.ResponsesOutputTypeImageGenerationCall:
					if !imageCommitted {
						imageCounter.Observe(streamResponse.Item, streamResponse.OutputIndex)
					}
				}
			}
		}
	})

	if !flushed && c.Request.Context().Err() == nil {
		if upstreamFailure == nil {
			upstreamFailure = fmt.Errorf("upstream stream ended without any output, received %d events", len(pendingEvents))
		}
		logger.LogError(c, upstreamFailure.Error())
		return nil, types.NewErrorWithStatusCode(upstreamFailure, types.ErrorCodeBadResponse, http.StatusBadGateway)
	}

	if usage.CompletionTokens == 0 {
		// 计算输出文本的 token 数量
		tempStr := responseTextBuilder.String()
		if len(tempStr) > 0 {
			// 非正常结束，使用输出文本的 token 数量
			common.SetContextKey(c, constant.ContextKeyLocalCountTokens, true)
			completionTokens := service.CountTextToken(tempStr, info.UpstreamModelName)
			usage.CompletionTokens = completionTokens
		}
	}

	if usage.PromptTokens == 0 && usage.CompletionTokens != 0 {
		usage.PromptTokens = info.GetEstimatePromptTokens()
	}

	usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens

	return usage, nil
}

// responsesTimestampFields 是 /v1/responses 响应中需要归一化为整数编码的时间戳字段。
// Meta 等上游会把它们编码成整数值浮点（例：1792990141.0），与 OpenAI 规范以及
// Meta 自己文档示例中的 integer Unix 秒都不一致，严格解析的客户端会解析失败。
var responsesTimestampFields = [...]string{"created_at", "completed_at"}

var (
	responsesCreatedAtMarker   = []byte("created_at")
	responsesCompletedAtMarker = []byte("completed_at")
)

// normalizeResponsesTimestamps 把响应对象顶层的 created_at / completed_at
// 从整数值浮点改写为标准整数编码，其余字段的值保持原始字节不变。
// 解析失败或值已是整数编码时原样返回，对 OpenAI 等上游零影响。
func normalizeResponsesTimestamps(body []byte) []byte {
	if !bytes.Contains(body, responsesCreatedAtMarker) && !bytes.Contains(body, responsesCompletedAtMarker) {
		return body
	}
	var top map[string]json.RawMessage
	if err := common.Unmarshal(body, &top); err != nil {
		return body
	}
	changed := false
	for _, field := range responsesTimestampFields {
		raw, ok := top[field]
		if !ok {
			continue
		}
		norm, ok := normalizeTimestampBytes(raw)
		if !ok {
			continue
		}
		top[field] = norm
		changed = true
	}
	if !changed {
		return body
	}
	if out, err := common.Marshal(top); err == nil {
		return out
	}
	return body
}

// normalizeResponsesEventTimestamps 处理流式事件：时间戳位于 response 子对象内。
// 不含时间戳的事件（如 output_text.delta）原样返回，不产生额外分配。
func normalizeResponsesEventTimestamps(data string) string {
	if !strings.Contains(data, "created_at") && !strings.Contains(data, "completed_at") {
		return data
	}
	var top map[string]json.RawMessage
	if err := common.Unmarshal([]byte(data), &top); err != nil {
		return data
	}
	respRaw, ok := top["response"]
	if !ok {
		return data
	}
	patched := normalizeResponsesTimestamps(respRaw)
	if bytes.Equal(patched, respRaw) {
		return data
	}
	top["response"] = patched
	if out, err := common.Marshal(top); err == nil {
		return string(out)
	}
	return data
}

// normalizeTimestampBytes 把单个时间戳字段从整数值浮点归一化为整数编码。
// null、字符串、布尔、已是整数编码、非整数或超出 int 范围的浮点都不改写，
// 只有整数值浮点（例：1792990141.0、1.792987007e9）会被改写。
func normalizeTimestampBytes(raw []byte) ([]byte, bool) {
	trimmed := bytes.TrimSpace(raw)
	var num json.Number
	if err := common.Unmarshal(trimmed, &num); err != nil {
		return nil, false
	}
	if _, err := num.Int64(); err == nil {
		return nil, false
	}
	f, err := num.Float64()
	if err != nil || f != math.Trunc(f) || f >= math.MaxInt64 || f < math.MinInt64 {
		return nil, false
	}
	return []byte(strconv.FormatInt(int64(f), 10)), true
}

func rewriteSGLangResponsesCreatedAt(info *relaycommon.RelayInfo, payload []byte, path string, createdAt dto.IntValue) []byte {
	if info.GetChannelType() != constant.ChannelTypeSGLang {
		return payload
	}
	if !gjson.GetBytes(payload, path).Exists() {
		return payload
	}
	patched, err := sjson.SetBytes(payload, path, int(createdAt))
	if err != nil {
		return payload
	}
	return patched
}
