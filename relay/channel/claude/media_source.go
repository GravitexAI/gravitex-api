package claude

import (
	"fmt"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
)

// ConvertURLSourcesToBase64 将 message content 中 image/document block 里
// source.type == "url" 的媒体下载并就地转成 base64。
//
// 背景：Anthropic 官方 API 原生支持 image/document 的 URL source，但 Bedrock 和
// Vertex AI 均不支持（官方文档：仅 base64-encoded source 可用），带 URL source 的
// 请求会被上游直接拒绝。这不是协议宽松度问题，是这两个渠道的硬性限制，因此对
// 需要转换的目标一律做转换，不做用户或渠道级别的开关。
//
// 调用方负责判断"是否需要转换"（本函数不做目标判断）：
//   - relay/channel/aws（原生 Bedrock 渠道）：始终调用。
//   - relay/channel/vertex（原生 Vertex 渠道）：始终调用。
//   - relay/channel/claude（渠道类型是 Anthropic，但 anthropic_beta_target 显式
//     指向 Bedrock/Vertex 的兼容资源场景）：仅当 ResolveBetaTarget 结果不是
//     TargetAnthropicDirect 时调用。
func ConvertURLSourcesToBase64(c *gin.Context, request *dto.ClaudeRequest) error {
	for i, message := range request.Messages {
		if message.IsStringContent() {
			continue
		}
		content, err := message.ParseContent()
		if err != nil {
			return errors.Wrap(err, "failed to parse message content")
		}
		updated := false
		for i2, mediaMessage := range content {
			if mediaMessage.Source == nil || mediaMessage.Source.Type != "url" {
				continue
			}
			label := mediaSourceLabel(mediaMessage.Type)
			source := types.NewURLFileSource(mediaMessage.Source.Url)
			base64Data, mimeType, err := service.GetBase64Data(c, source, fmt.Sprintf("formatting %s for Claude", label))
			if err != nil {
				// 下载失败（源站因 User-Agent/风控策略返回 403、超时、DNS 失败等）时不
				// 阻断整个请求：保留原始 URL source 不做修改，把用户原始请求原样转发给
				// 上游。Bedrock/Vertex 本身不支持 URL source，上游会用它自己的报错拒绝，
				// 但这样不会把"我们这边下载失败"和"上游本身不支持"这两种不同性质的错误
				// 混在一起，也不会因为一次可能是暂时性的下载失败就直接拒绝整个请求。
				logger.LogWarn(c, fmt.Sprintf("failed to fetch %s from url, keep original url source and forward as-is: %s", label, err.Error()))
				continue
			}
			mediaMessage.Source.MediaType = mimeType
			mediaMessage.Source.Data = base64Data
			mediaMessage.Source.Url = ""
			mediaMessage.Source.Type = "base64"
			content[i2] = mediaMessage
			updated = true
		}
		if updated {
			message.SetContent(content)
			request.Messages[i] = message
		}
	}
	return nil
}

// mediaSourceLabel 把 content block 的 type 转成日志里可读的媒体类型名，
// 用于区分是图片下载失败还是文档下载失败。
func mediaSourceLabel(blockType string) string {
	if blockType == "image" {
		return "image"
	}
	return "document"
}
