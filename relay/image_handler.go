package relay

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relay/requestplugin"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/model_setting"

	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
	"github.com/tidwall/sjson"
)

func truncateImageLogBody(body []byte) string {
	bodyStr := strings.TrimSpace(string(body))
	if bodyStr == "" {
		return "<empty>"
	}
	// JSON 请求体按字段截断，避免 base64 等长字符串把整条日志刷爆。
	if strings.HasPrefix(bodyStr, "{") || strings.HasPrefix(bodyStr, "[") {
		return common.TruncateJsonValues(bodyStr)
	}
	const maxLogLen = 2000
	if len(bodyStr) > maxLogLen {
		return bodyStr[:maxLogLen] + fmt.Sprintf("...(truncated,total=%d)", len(body))
	}
	return bodyStr
}

// outboundImageCounter is an image adaptor whose upstream body carries the
// image count in a member other than n. OutboundImageCount reads the count
// from that body, nil when the member is absent.
type outboundImageCounter interface {
	OutboundImageCount(body []byte) (*uint, error)
}

func ImageHelper(c *gin.Context, info *relaycommon.RelayInfo) (newAPIError *types.NewAPIError) {
	info.InitChannelMeta(c)
	info.BillingImageCount = nil
	info.ImageRequestCount = 0

	imageReq, ok := info.Request.(*dto.ImageRequest)
	if !ok {
		return types.NewErrorWithStatusCode(fmt.Errorf("invalid request type, expected dto.ImageRequest, got %T", info.Request), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	// 记录用户侧解析后的图片请求参数，便于对比后续实际发往上游的内容。
	if requestBytes, err := common.Marshal(imageReq); err == nil {
		var requestMap map[string]json.RawMessage
		if err := common.Unmarshal(requestBytes, &requestMap); err == nil {
			for key, value := range imageReq.Extra {
				if _, exists := requestMap[key]; !exists {
					requestMap[key] = value
				}
			}
			if mergedBytes, err := common.Marshal(requestMap); err == nil {
				requestBytes = mergedBytes
			}
		}
		logger.LogDebug(c, fmt.Sprintf("image user request params: %s", common.TruncateJsonValues(string(requestBytes))))
	}

	request, err := common.DeepCopy(imageReq)
	if err != nil {
		return types.NewError(fmt.Errorf("failed to copy request to ImageRequest: %w", err), types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
	}

	err = helper.ModelMappedHelper(c, info, request)
	if err != nil {
		return types.NewError(err, types.ErrorCodeChannelModelMappedError, types.ErrOptionWithSkipRetry())
	}

	adaptor := GetAdaptor(info.ApiType)
	if adaptor == nil {
		return types.NewError(fmt.Errorf("invalid api type: %d", info.ApiType), types.ErrorCodeInvalidApiType, types.ErrOptionWithSkipRetry())
	}
	adaptor.Init(info)
	imageCount, err := request.ImageCount(info.ChannelType == constant.ChannelTypeAli)
	if err != nil {
		return types.NewErrorWithStatusCode(err, types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	promptExtend := request.BillingParameters != nil && request.BillingParameters.PromptExtend != nil && *request.BillingParameters.PromptExtend

	// gpt-image 系列：当 /v1/images/generations 请求包含 image 字段时，自动升级为 edits 模式
	if info.RelayMode == relayconstant.RelayModeImagesGenerations && strings.HasPrefix(request.Model, "gpt-image") {
		hasImage := (request.Image != nil && len(request.Image) > 0) ||
			(request.Images != nil && len(request.Images) > 0)
		if !hasImage && request.Extra != nil {
			_, hasImage = request.Extra["image"]
			if !hasImage {
				_, hasImage = request.Extra["images"]
			}
		}
		if hasImage {
			info.RelayMode = relayconstant.RelayModeImagesEdits
			info.RequestURLPath = strings.Replace(info.RequestURLPath, "/images/generations", "/images/edits", 1)
			logger.LogDebug(c, fmt.Sprintf("gpt-image auto-upgrade: generations → edits (model=%s)", request.Model))
		}
	}

	// gpt-image 图生图：打印用户入参摘要
	if strings.HasPrefix(request.Model, "gpt-image") && info.RelayMode == relayconstant.RelayModeImagesEdits {
		imageCount := 0
		imageSummary := "none"
		if request.Image != nil && len(request.Image) > 0 {
			// 判断 Image 是 string 还是 []string
			raw := string(request.Image)
			if len(raw) > 0 && raw[0] == '[' {
				var arr []interface{}
				if err := common.Unmarshal(request.Image, &arr); err == nil {
					imageCount = len(arr)
				}
			} else {
				imageCount = 1
			}
			// 截断 base64 数据，只显示前 80 字符
			if len(raw) > 80 {
				imageSummary = raw[:80] + "...(truncated)"
			} else {
				imageSummary = raw
			}
		}
		n := uint(1)
		if request.N != nil {
			n = *request.N
		}
		logger.LogDebug(c, fmt.Sprintf("gpt-image edits user request: model=%s, prompt=%q, size=%s, quality=%s, n=%d, input_fidelity=%q, image_count=%d, image_preview=%s",
			request.Model, request.Prompt, request.Size, request.Quality, n, lo.FromPtr(request.InputFidelity), imageCount, imageSummary))
	} else if strings.HasPrefix(request.Model, "gpt-image") && info.RelayMode == relayconstant.RelayModeImagesGenerations {
		n := uint(1)
		if request.N != nil {
			n = *request.N
		}
		logger.LogInfo(c, fmt.Sprintf("gpt-image generations user request: model=%s, prompt=%q, size=%s, quality=%s, n=%d",
			request.Model, request.Prompt, request.Size, request.Quality, n))
	}

	var requestBody io.Reader
	var jsonData []byte
	var outbound struct {
		N          *uint                       `json:"n"`
		Parameters *dto.ImageBillingParameters `json:"parameters"`
	}
	validateQuantity := false

	if model_setting.GetGlobalSettings().PassThroughRequestEnabled || info.ChannelSetting.PassThroughBodyEnabled {
		storage, err := common.GetBodyStorage(c)
		if err != nil {
			return types.NewErrorWithStatusCode(err, types.ErrorCodeReadRequestBodyFailed, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
		}
		if newAPIError = requestplugin.PassThrough(c, info, storage); newAPIError != nil {
			return newAPIError
		}
		isMultipart := strings.Contains(c.Request.Header.Get("Content-Type"), "multipart/form-data")
		if !isMultipart && info.ChannelType == constant.ChannelTypeAli {
			// alpha：阿里渠道透传时仍需改写 parameters.n，读出完整 JSON 走下方统一的 jsonData 流程。
			jsonData, err = storage.Bytes()
			if err != nil {
				return types.NewErrorWithStatusCode(err, types.ErrorCodeReadRequestBodyFailed, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
			}
		} else if !isMultipart {
			counter, countsOwnMember := adaptor.(outboundImageCounter)
			switch {
			case countsOwnMember:
				// The adaptor sends the count in its own member, which only
				// it can read, so it reads the whole body.
				var raw []byte
				raw, err = storage.Bytes()
				if err == nil {
					err = common.Unmarshal(raw, &outbound)
				}
				if err == nil {
					outbound.N, err = counter.OutboundImageCount(raw)
				}
			case storage.IsDisk():
				reader, readErr := storage.NewReader()
				if readErr != nil {
					return types.NewErrorWithStatusCode(readErr, types.ErrorCodeReadRequestBodyFailed, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
				}
				err = common.DecodeJsonBody(reader, &outbound)
				_ = reader.Close()
			default:
				var raw []byte
				raw, err = storage.Bytes()
				if err == nil {
					err = common.Unmarshal(raw, &outbound)
				}
			}
			if err != nil {
				return types.NewErrorWithStatusCode(fmt.Errorf("invalid image billing parameters: %w", err), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
			}
			validateQuantity = true
		}
		// 透传模式下记录原始请求体，排查"用户传了什么"与"上游收到了什么"是否一致。
		if common.DebugEnabled && jsonData == nil {
			if raw, readErr := storage.Bytes(); readErr == nil {
				logger.LogDebug(c, "image upstream request body(size=%d): %s", len(raw), truncateImageLogBody(raw))
			}
		}
		if jsonData == nil {
			requestBody = common.NewReplayableBodyReader(storage)
		}
	} else {
		convertedRequest, err := adaptor.ConvertImageRequest(c, info, *request)
		if err != nil {
			return types.NewError(err, types.ErrorCodeConvertRequestFailed)
		}
		relaycommon.AppendRequestConversionFromRequest(info, convertedRequest)

		switch convertedRequest.(type) {
		case *bytes.Buffer:
			// multipart / buffer 形式的上游请求在这里记录最终内容，方便定位转换后的差异。
			bodyBytes := convertedRequest.(*bytes.Buffer).Bytes()
			logger.LogDebug(c, "image upstream request body(size=%d): %s", len(bodyBytes), truncateImageLogBody(bodyBytes))
			if newAPIError = requestplugin.PassThrough(c, info, nil); newAPIError != nil {
				return newAPIError
			}
			requestBody = convertedRequest.(io.Reader)
		default:
			jsonData, err = common.Marshal(convertedRequest)
			if err != nil {
				return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
			}

			if jsonData, newAPIError = requestplugin.Outbound(c, info, jsonData); newAPIError != nil {
				return newAPIError
			}
			// apply param override
			if len(info.ParamOverride) > 0 {
				jsonData, err = relaycommon.ApplyParamOverrideWithRelayInfo(jsonData, info)
				if err != nil {
					return newAPIErrorFromParamOverride(err)
				}
			}
			if newAPIError = requestplugin.Egress(c, info, jsonData); newAPIError != nil {
				return newAPIError
			}

		}
	}
	if jsonData != nil {
		// This is a different trust boundary from ingress: channel overrides
		// and request plugins can change the quantity actually submitted, in
		// n or in the member the adaptor sends it in.
		err := common.Unmarshal(jsonData, &outbound)
		if counter, ok := adaptor.(outboundImageCounter); ok && err == nil {
			outbound.N, err = counter.OutboundImageCount(jsonData)
		}
		if err != nil {
			return types.NewErrorWithStatusCode(fmt.Errorf("invalid image billing parameters: %w", err), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
		}
		validateQuantity = true
	}
	if validateQuantity {
		// Validate the count in the body sent upstream, including channel overrides.
		quantityRequest := dto.ImageRequest{N: outbound.N, BillingParameters: outbound.Parameters}
		if quantityRequest.N == nil {
			quantityRequest.N = common.GetPointer(uint(imageCount))
		}
		imageCount, err = quantityRequest.ImageCount(info.ChannelType == constant.ChannelTypeAli)
		if err != nil {
			return types.NewErrorWithStatusCode(err, types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
		}
		promptExtend = outbound.Parameters != nil && outbound.Parameters.PromptExtend != nil && *outbound.Parameters.PromptExtend
		if jsonData != nil && info.ChannelType == constant.ChannelTypeAli {
			// Always send the same explicit quantity that is reserved, including
			// when an empty parameters object accompanies a top-level n.
			jsonData, err = sjson.SetBytes(jsonData, "parameters.n", imageCount)
			if err != nil {
				return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
			}
		}
	}
	if jsonData != nil {
		// JSON 形式的上游请求在应用参数覆盖后记录，确保日志反映最终发送结果（截断 base64）。
		logger.LogDebug(c, "image upstream request body(size=%d): %s", len(jsonData), truncateImageLogBody(jsonData))
		body, closer, err := relaycommon.NewOutboundJSONBody(jsonData)
		if err != nil {
			return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
		}
		defer closer.Close()
		requestBody = body
	}
	if billingErr := service.PrepareImageBillingForRequest(c, info, imageCount, promptExtend); billingErr != nil {
		return billingErr
	}

	statusCodeMappingStr := c.GetString("status_code_mapping")

	resp, err := adaptor.DoRequest(c, info, requestBody)
	if err != nil {
		return types.NewOpenAIError(err, types.ErrorCodeDoRequestFailed, http.StatusInternalServerError)
	}
	var httpResp *http.Response
	if resp != nil {
		httpResp = resp.(*http.Response)
		info.IsStream = info.IsStream || strings.HasPrefix(httpResp.Header.Get("Content-Type"), "text/event-stream")
		if httpResp.StatusCode != http.StatusOK {
			if httpResp.StatusCode == http.StatusCreated && info.ApiType == constant.APITypeReplicate {
				// replicate channel returns 201 Created when using Prefer: wait, treat it as success.
				httpResp.StatusCode = http.StatusOK
			} else {
				newAPIError = service.RelayErrorHandler(c.Request.Context(), httpResp, false)
				// reset status code 重置状态码
				service.ResetStatusCode(newAPIError, statusCodeMappingStr)
				return newAPIError
			}
		}
	}

	usage, newAPIError := adaptor.DoResponse(c, httpResp, info)
	if newAPIError != nil {
		// reset status code 重置状态码
		service.ResetStatusCode(newAPIError, statusCodeMappingStr)
		return newAPIError
	}

	if info.PriceData.ImagePerImagePricing != nil {
		if actualPrice, billingUsage, err := helper.SettleImagePerImageUsage(*info.PriceData.ImagePerImagePricing, request, usage.(*dto.Usage)); err == nil {
			info.PriceData.ModelPrice = actualPrice
			info.PriceData.ImageBillingUsage = billingUsage
			if billingUsage != nil {
				info.PriceData.PerImageUnitPrice = helper.ImageOutputUnitPriceForUsage(*info.PriceData.ImagePerImagePricing, billingUsage)
			}
			info.PriceData.ImagePriceMultiplier = 1
		}
	}

	// 按张计费：优先使用上游返回的 usage.generated_images（实际生成数量），
	// 其次回退到请求入参的 N，兜底为 1。
	// 解决场景：用户请求 n=4，但上游（如 seedream-5）实际只返回 1 张图，
	// 不应该按 4 张计费。
	imageN := uint(1)
	if u, ok := usage.(*dto.Usage); ok && u != nil && u.GeneratedImages > 0 {
		imageN = uint(u.GeneratedImages)
	} else if request.N != nil && *request.N > 0 {
		imageN = *request.N
	}

	// n 通过 OtherRatio 只参与一次计费。官方 PrepareImageBillingForRequest 已按请求张数预扣并写入 n，
	// 适配器可在响应后用 UpdateImageCount 改成实际张数；这里补 alpha 规则：
	// - 按张计费（Seedream 逐张分档）：ModelPrice 已是实际总价，n 复位为 1，避免张数重复计算
	// - 上游 usage 返回 generated_images 时按实际生成张数结算（例如请求 n=4 实际只出 1 张）
	if info.PriceData.ImagePerImagePricing != nil {
		info.PriceData.AddOtherRatio("n", 1)
	} else if u, ok := usage.(*dto.Usage); ok && u != nil && u.GeneratedImages > 0 {
		info.UpdateImageCount(int64(u.GeneratedImages))
	}

	if usage.(*dto.Usage).TotalTokens == 0 {
		usage.(*dto.Usage).TotalTokens = 1
	}
	if usage.(*dto.Usage).PromptTokens == 0 {
		usage.(*dto.Usage).PromptTokens = 1
	}

	// 按张计费：ImagePriceMultiplier 仅作日志/展示用途，反映「尺寸/质量倍率 × 实际张数」。
	// 注意：不要在此处用张数覆盖 ModelPrice —— ModelPrice 在 price.go 中已是
	// `unit_price × size_ratio × quality_ratio`，张数已通过 OtherRatios["n"] 一次性参与计费，
	// 否则会丢失 size/quality 倍率，并与 OtherRatios["n"] 双重计算。
	if info.PriceData.PerImageUnitPrice > 0 {
		sizeQualityMultiplier := info.PriceData.ImagePriceMultiplier
		if sizeQualityMultiplier <= 0 {
			sizeQualityMultiplier = 1
		}
		info.PriceData.ImagePriceMultiplier = sizeQualityMultiplier * float64(imageN)
	}

	quality := "standard"
	if request.Quality == "hd" {
		quality = "hd"
	}

	var logContent []string

	if len(request.Size) > 0 {
		logContent = append(logContent, fmt.Sprintf("大小 %s", request.Size))
	}
	if len(quality) > 0 {
		logContent = append(logContent, fmt.Sprintf("品质 %s", quality))
	}
	if imageN > 0 {
		logContent = append(logContent, fmt.Sprintf("生成数量 %d", imageN))
	}

	service.PostTextConsumeQuota(c, info, usage.(*dto.Usage), logContent)
	return nil
}
