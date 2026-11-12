package oaichat

import (
	"fmt"
	"net/http"
	"strings"

	"context"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/internal/convdiag"
	relaymedia "github.com/QuantumNous/new-api/relaykit/relayconvert/internal/media"
	sharedclaude "github.com/QuantumNous/new-api/relaykit/relayconvert/internal/shared/claude"
	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/reasoning"
	"github.com/QuantumNous/new-api/relaykit/types"
)

func OpenAIChatRequestToClaudeMessages(c context.Context, info convmeta.Meta, textRequest dto.GeneralOpenAIRequest) (*dto.ClaudeRequest, error) {
	opts := convmeta.OptionsOf(info)
	claudeTools := make([]any, 0, len(textRequest.Tools))

	for _, tool := range textRequest.Tools {
		if kitutil.GetJsonType(tool.Function.Parameters) != "object" && tool.Type != "function" {
			continue
		}
		inputSchema, _ := sharedclaude.FunctionParametersToInputSchema(tool.Function.Parameters)
		claudeTools = append(claudeTools, &dto.Tool{
			Name:        tool.Function.Name,
			Description: tool.Function.Description,
			InputSchema: inputSchema,
		})
	}

	if textRequest.WebSearchOptions != nil {
		webSearchTool := dto.ClaudeWebSearchTool{
			Type: "web_search_20250305",
			Name: "web_search",
		}

		if textRequest.WebSearchOptions.UserLocation != nil {
			anthropicUserLocation := &dto.ClaudeWebSearchUserLocation{
				Type: "approximate",
			}

			var userLocationMap map[string]any
			if err := kitutil.Unmarshal(textRequest.WebSearchOptions.UserLocation, &userLocationMap); err == nil {
				if approximateData, ok := userLocationMap["approximate"].(map[string]any); ok {
					if timezone, ok := approximateData["timezone"].(string); ok && timezone != "" {
						anthropicUserLocation.Timezone = timezone
					}
					if country, ok := approximateData["country"].(string); ok && country != "" {
						anthropicUserLocation.Country = country
					}
					if region, ok := approximateData["region"].(string); ok && region != "" {
						anthropicUserLocation.Region = region
					}
					if city, ok := approximateData["city"].(string); ok && city != "" {
						anthropicUserLocation.City = city
					}
				}
			}

			webSearchTool.UserLocation = anthropicUserLocation
		}

		claudeTools = append(claudeTools, &webSearchTool)
	}

	claudeRequest := dto.ClaudeRequest{
		Model:         textRequest.Model,
		StopSequences: nil,
		Temperature:   textRequest.Temperature,
	}
	if len(claudeTools) > 0 {
		claudeRequest.Tools = claudeTools
	}
	if textRequest.MaxCompletionTokens != nil && *textRequest.MaxCompletionTokens > 0 {
		claudeRequest.MaxTokens = kitutil.GetPointer(*textRequest.MaxCompletionTokens)
	} else if textRequest.MaxTokens != nil && *textRequest.MaxTokens > 0 {
		claudeRequest.MaxTokens = kitutil.GetPointer(*textRequest.MaxTokens)
	}
	if textRequest.TopP != nil {
		claudeRequest.TopP = kitutil.GetPointer(*textRequest.TopP)
	}
	if textRequest.TopK != nil {
		claudeRequest.TopK = kitutil.GetPointer(*textRequest.TopK)
	}
	if textRequest.IsStream(nil) {
		claudeRequest.Stream = kitutil.GetPointer(true)
	}

	if textRequest.ToolChoice != nil || textRequest.ParallelTooCalls != nil {
		claudeToolChoice := sharedclaude.MapOpenAIToolChoice(textRequest.ToolChoice, textRequest.ParallelTooCalls)
		if claudeToolChoice != nil {
			claudeRequest.ToolChoice = claudeToolChoice
		}
	}

	sourceReasoning, diagnostics, err := reasoning.FromOpenAIChat(&textRequest)
	if err != nil {
		return nil, reasoning.AsClientError(err)
	}
	convdiag.Add(c, diagnostics...)
	if err := sharedclaude.ApplyReasoning(c, &claudeRequest, info, sourceReasoning, true); err != nil {
		return nil, reasoning.AsClientError(err)
	}
	if claudeRequest.MaxTokens == nil {
		if defaultMaxTokens, configured := opts.Claude.DefaultMaxTokensFor(claudeRequest.Model); configured {
			value := uint(defaultMaxTokens)
			claudeRequest.MaxTokens = &value
		}
	}

	claudeRequest.StopSequences = parseClaudeStopSequences(textRequest.Stop)

	formatMessages := make([]dto.Message, 0)
	// sourceIndexes[i] 为 formatMessages[i] 在客户端原始 messages 中的下标，
	// 同角色字符串合并会改变下标，报错定位需用原始下标。
	sourceIndexes := make([]int, 0, len(textRequest.Messages))
	lastMessage := dto.Message{
		Role: "tool",
	}
	lastMessageHasPlaceholder := false
	for sourceIndex, message := range textRequest.Messages {
		switch message.Role {
		case "":
			message.Role = "user"
		case "developer":
			message.Role = "system"
		case "function":
			if message.ToolCallId != "" {
				message.Role = "tool"
			} else {
				message.Role = "user"
			}
		case "tool":
			if message.ToolCallId == "" {
				message.Role = "user"
			}
		case "system", "user", "assistant":
		default:
			message.Role = "user"
		}
		fmtMessage := dto.Message{
			Role:    message.Role,
			Content: message.Content,
		}
		if message.Role == "tool" {
			fmtMessage.ToolCallId = message.ToolCallId
		}
		if message.Role == "assistant" && message.ToolCalls != nil {
			fmtMessage.ToolCalls = message.ToolCalls
		}
		if lastMessage.Role == message.Role && lastMessage.Role != "tool" {
			if lastMessage.IsStringContent() && message.IsStringContent() {
				previousToolCalls := lastMessage.ParseToolCalls()
				currentToolCalls := fmtMessage.ParseToolCalls()
				if len(previousToolCalls) > 0 || len(currentToolCalls) > 0 {
					// Keep both messages' tool calls so later tool results still
					// match a tool_use block.
					texts := make([]string, 0, 2)
					// Omit only text injected for an empty tool-call message.
					isToolPlaceholder := lastMessageHasPlaceholder && len(previousToolCalls) > 0
					if previousText := lastMessage.StringContent(); previousText != "" && !isToolPlaceholder {
						texts = append(texts, previousText)
					}
					if currentText := message.StringContent(); currentText != "" {
						texts = append(texts, currentText)
					}
					fmtMessage.SetStringContent(strings.Join(texts, " "))
					fmtMessage.SetToolCalls(append(previousToolCalls, currentToolCalls...))
				} else {
					fmtMessage.SetStringContent(fmt.Sprintf("%s %s", lastMessage.StringContent(), message.StringContent()))
				}
				formatMessages = formatMessages[:len(formatMessages)-1]
				sourceIndexes = sourceIndexes[:len(sourceIndexes)-1]
			}
		}
		hasPlaceholder := fmtMessage.Content == nil || (fmtMessage.IsStringContent() && fmtMessage.StringContent() == "")
		if hasPlaceholder {
			fmtMessage.SetStringContent("...")
		}
		formatMessages = append(formatMessages, fmtMessage)
		sourceIndexes = append(sourceIndexes, sourceIndex)
		lastMessage = fmtMessage
		lastMessageHasPlaceholder = hasPlaceholder
	}

	claudeMessages := make([]dto.ClaudeMessage, 0)
	isFirstMessage := true
	var systemMessages []dto.ClaudeMediaMessage
	placeholderUserMessage := dto.ClaudeMessage{
		Role: "user",
		Content: []dto.ClaudeMediaMessage{
			{
				Type: "text",
				Text: kitutil.GetPointer[string]("..."),
			},
		},
	}

	for messageIndex, message := range formatMessages {
		if message.Role == "system" {
			if message.IsStringContent() {
				if text := message.StringContent(); text != "" {
					systemMessages = append(systemMessages, dto.ClaudeMediaMessage{
						Type: "text",
						Text: kitutil.GetPointer[string](text),
					})
				}
			} else {
				for _, ctx := range message.ParseContent() {
					if ctx.Type == "text" && ctx.Text != "" {
						systemMessages = append(systemMessages, dto.ClaudeMediaMessage{
							Type: "text",
							Text: kitutil.GetPointer[string](ctx.Text),
						})
					}
				}
			}
			continue
		}

		if isFirstMessage {
			isFirstMessage = false
			if message.Role != "user" {
				claudeMessages = append(claudeMessages, placeholderUserMessage)
			}
		}

		claudeMessage := dto.ClaudeMessage{
			Role: message.Role,
		}
		if message.Role == "tool" {
			toolResultContent, err := claudeToolResultContent(c, message.Content, sourceIndexes[messageIndex])
			if err != nil {
				return nil, err
			}
			if len(claudeMessages) > 0 && claudeMessages[len(claudeMessages)-1].Role == "user" {
				lastClaudeMessage := claudeMessages[len(claudeMessages)-1]
				if content, ok := lastClaudeMessage.Content.(string); ok {
					lastClaudeMessage.Content = []dto.ClaudeMediaMessage{
						{
							Type: "text",
							Text: kitutil.GetPointer[string](content),
						},
					}
				}
				lastClaudeMessage.Content = append(lastClaudeMessage.Content.([]dto.ClaudeMediaMessage), dto.ClaudeMediaMessage{
					Type:      "tool_result",
					ToolUseId: message.ToolCallId,
					Content:   toolResultContent,
				})
				claudeMessages[len(claudeMessages)-1] = lastClaudeMessage
				continue
			}

			claudeMessage.Role = "user"
			claudeMessage.Content = []dto.ClaudeMediaMessage{
				{
					Type:      "tool_result",
					ToolUseId: message.ToolCallId,
					Content:   toolResultContent,
				},
			}
		} else if message.IsStringContent() && message.ToolCalls == nil {
			text := message.StringContent()
			if text == "" {
				text = "..."
			}
			claudeMessage.Content = text
		} else {
			claudeMediaMessages := make([]dto.ClaudeMediaMessage, 0)
			for partIndex, mediaMessage := range message.ParseContent() {
				switch mediaMessage.Type {
				case "text":
					if mediaMessage.Text != "" {
						claudeMediaMessages = append(claudeMediaMessages, dto.ClaudeMediaMessage{
							Type: "text",
							Text: kitutil.GetPointer[string](mediaMessage.Text),
						})
					}
				default:
					source := mediaMessage.ToFileSource()
					if source == nil {
						continue
					}
					// Claude content blocks carry images, PDF documents, and text
					// documents; audio and video have no Claude block.
					reason := fmt.Sprintf("Claude Messages cannot carry %s content", mediaMessage.Type)
					var block dto.ClaudeMediaMessage
					if mediaMessage.Type != dto.ContentTypeInputAudio && mediaMessage.Type != dto.ContentTypeVideoUrl {
						base64Data, mimeType, err := relaymedia.ResolveBase64Data(c, source, "formatting image for Claude")
						if err != nil {
							location := fmt.Sprintf("messages[%d](%s).content[%d]", messageIndex, message.Role, partIndex)
							return nil, types.NewFileSourceError(err, location, mediaMessage.Type, source)
						}
						block, reason = sharedclaude.MediaBlock(base64Data, mimeType, mediaMessage.Type == dto.ContentTypeImageURL)
					}
					if reason != "" {
						convdiag.Add(c, types.ConversionDiagnostic{
							Code:     "unsupported_media_type",
							Path:     "messages.content",
							Message:  reason + "; the part was omitted",
							Severity: types.ConversionDiagnosticError,
						})
						continue
					}
					claudeMediaMessages = append(claudeMediaMessages, block)
					continue
				}
			}

			if message.ToolCalls != nil {
				for _, toolCall := range message.ParseToolCalls() {
					inputObj := make(map[string]any)
					if args := toolCall.Function.Arguments; args != "" {
						if err := kitutil.Unmarshal([]byte(args), &inputObj); err != nil {
							kitutil.LogInfo("tool call function arguments is not a map[string]any: " + fmt.Sprintf("%v", toolCall.Function.Arguments))
						}
					}
					claudeMediaMessages = append(claudeMediaMessages, dto.ClaudeMediaMessage{
						Type:  "tool_use",
						Id:    toolCall.ID,
						Name:  toolCall.Function.Name,
						Input: inputObj,
					})
				}
			}
			if len(claudeMediaMessages) == 0 {
				// Every part was omitted; Claude rejects an empty content array.
				claudeMediaMessages = append(claudeMediaMessages, dto.ClaudeMediaMessage{Type: "text", Text: kitutil.GetPointer("...")})
			}
			claudeMessage.Content = claudeMediaMessages
		}
		claudeMessages = append(claudeMessages, claudeMessage)
	}
	if len(claudeMessages) == 0 && len(systemMessages) > 0 {
		claudeMessages = append(claudeMessages, placeholderUserMessage)
	}

	if len(systemMessages) > 0 {
		claudeRequest.System = systemMessages
	}

	claudeRequest.Prompt = ""
	claudeRequest.Messages = claudeMessages
	// Checked last so every injection path (default hook, thinking adapter
	// floor) has had its chance to satisfy the required field.
	if claudeRequest.MaxTokens == nil {
		return nil, sharedclaude.ErrMissingMaxTokens
	}
	return &claudeRequest, nil
}

// claudeToolResultContent 把 OpenAI 工具消息内容转换为 Claude tool_result.content。
//
// OpenAI 官方工具消息只允许文本，部分客户端会扩展放入 image_url / file 块（截图、读取的文件）。
// 只转换这两类块为 Claude image / document；其余块（text、Claude 原生块、未知类型）
// 保持原始 map 透传，cache_control 等字段不丢失，合法性交由上游裁决。
// 不含需转换块的内容（字符串、纯文本数组）原样返回，与改动前行为一致。
func claudeToolResultContent(c context.Context, content any, sourceIndex int) (any, error) {
	parts, ok := content.([]any)
	if !ok {
		return content, nil
	}
	needsConversion := false
	for _, part := range parts {
		if partType := toolResultPartType(part); partType == dto.ContentTypeImageURL || partType == dto.ContentTypeFile {
			needsConversion = true
			break
		}
	}
	if !needsConversion {
		return content, nil
	}

	converted := make([]any, 0, len(parts))
	for partIndex, part := range parts {
		partType := toolResultPartType(part)
		if partType != dto.ContentTypeImageURL && partType != dto.ContentTypeFile {
			converted = append(converted, part)
			continue
		}
		location := fmt.Sprintf("messages[%d](tool).content[%d]", sourceIndex, partIndex)

		// 复用 ParseContent 解析单个块，与普通 user 消息的字段解析规则保持一致
		partMessage := dto.Message{Content: []any{part}}
		parsed := partMessage.ParseContent()
		var source types.FileSource
		if len(parsed) == 1 {
			source = parsed[0].ToFileSource()
		}
		if source == nil {
			return nil, types.NewErrorWithStatusCode(
				fmt.Errorf("invalid %s block at %s: missing url or file_data", partType, location),
				types.ErrorCodeInvalidRequest,
				http.StatusBadRequest,
				types.ErrOptionWithSkipRetry(),
			)
		}
		base64Data, mimeType, err := relaymedia.ResolveBase64Data(c, source, "formatting tool result media for Claude")
		if err != nil {
			return nil, types.NewFileSourceError(err, location, partType, source)
		}

		// Claude tool_result 仅接受 image（jpeg/png/gif/webp）与 document（pdf）
		mediaType := strings.ToLower(strings.TrimSpace(strings.Split(mimeType, ";")[0]))
		blockType := ""
		switch mediaType {
		case "image/jpeg", "image/png", "image/gif", "image/webp":
			blockType = "image"
		case "application/pdf":
			blockType = "document"
		default:
			return nil, types.NewErrorWithStatusCode(
				fmt.Errorf("unsupported media type %q in tool result at %s: only jpeg/png/gif/webp images and pdf documents are allowed", mimeType, location),
				types.ErrorCodeInvalidRequest,
				http.StatusBadRequest,
				types.ErrOptionWithSkipRetry(),
			)
		}

		block := dto.ClaudeMediaMessage{
			Type: blockType,
			Source: &dto.ClaudeMessageSource{
				Type:      "base64",
				MediaType: mediaType,
				Data:      base64Data,
			},
		}
		if cacheControl, ok := part.(map[string]any)["cache_control"]; ok && cacheControl != nil {
			raw, err := kitutil.Marshal(cacheControl)
			if err != nil {
				return nil, err
			}
			block.CacheControl = raw
		}
		converted = append(converted, block)
	}
	return converted, nil
}

// toolResultPartType 返回原始工具结果块的 type 字段，非对象或无 type 时返回空串。
func toolResultPartType(part any) string {
	partMap, ok := part.(map[string]any)
	if !ok {
		return ""
	}
	partType, _ := partMap["type"].(string)
	return partType
}

func parseClaudeStopSequences(stop any) []string {
	switch stop := stop.(type) {
	case string:
		return []string{stop}
	case []string:
		return stop
	case []any:
		sequences := make([]string, 0, len(stop))
		for _, item := range stop {
			if sequence, ok := item.(string); ok {
				sequences = append(sequences, sequence)
			}
		}
		return sequences
	default:
		return nil
	}
}
