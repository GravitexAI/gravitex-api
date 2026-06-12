package claude

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/relay/channel/openrouter"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relay/reasonmap"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/setting/reasoning"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const (
	WebSearchMaxUsesLow    = 1
	WebSearchMaxUsesMedium = 5
	WebSearchMaxUsesHigh   = 10
)

// jsonSchemaToolNameKey is the gin context key holding the synthetic tool name
// used to emulate OpenAI response_format=json_schema via Claude tool-forcing.
// When set, the forced tool_use output is unwrapped back into message content
// (a JSON string) instead of being surfaced as tool_calls.
const jsonSchemaToolNameKey = "claude_json_schema_tool_name"

// toolUseIDPattern matches the tool_use id format Anthropic accepts. Ids with
// other characters (e.g. colons from some OpenAI clients) are rejected with 400.
var toolUseIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// sanitizeToolUseID returns id unchanged when it already satisfies Anthropic's
// tool_use id format. Otherwise it derives a compliant id (illegal chars -> '_')
// and caches the mapping keyed on the original id, so a tool_use and its matching
// tool_result resolve to the same sanitized id regardless of which is seen first.
func sanitizeToolUseID(id string, m map[string]string) string {
	if id == "" || toolUseIDPattern.MatchString(id) {
		return id
	}
	if mapped, ok := m[id]; ok {
		return mapped
	}
	sanitized := regexp.MustCompile(`[^a-zA-Z0-9_-]`).ReplaceAllString(id, "_")
	if sanitized == "" {
		sanitized = "tool"
	}
	// avoid colliding with an id already assigned to a different original
	base, n := sanitized, 1
	for {
		clash := false
		for orig, v := range m {
			if v == sanitized && orig != id {
				clash = true
				break
			}
		}
		if !clash {
			break
		}
		sanitized = fmt.Sprintf("%s_%d", base, n)
		n++
	}
	m[id] = sanitized
	return sanitized
}

func stopReasonClaude2OpenAI(reason string) string {
	return reasonmap.ClaudeStopReasonToOpenAIFinishReason(reason)
}

func maybeMarkClaudeRefusal(c *gin.Context, stopReason string) {
	if c == nil {
		return
	}
	if strings.EqualFold(stopReason, "refusal") {
		common.SetContextKey(c, constant.ContextKeyAdminRejectReason, "claude_stop_reason=refusal")
	}
}

func RequestOpenAI2ClaudeMessage(c *gin.Context, textRequest dto.GeneralOpenAIRequest) (*dto.ClaudeRequest, error) {
	claudeTools := make([]any, 0, len(textRequest.Tools))

	for _, tool := range textRequest.Tools {
		if params, ok := tool.Function.Parameters.(map[string]any); ok {
			claudeTool := dto.Tool{
				Name:        tool.Function.Name,
				Description: tool.Function.Description,
			}
			claudeTool.InputSchema = make(map[string]interface{})
			if params["type"] != nil {
				claudeTool.InputSchema["type"] = params["type"].(string)
			}
			claudeTool.InputSchema["properties"] = params["properties"]
			claudeTool.InputSchema["required"] = params["required"]
			for s, a := range params {
				if s == "type" || s == "properties" || s == "required" {
					continue
				}
				claudeTool.InputSchema[s] = a
			}
			claudeTools = append(claudeTools, &claudeTool)
		}
	}

	// Web search tool
	// https://docs.anthropic.com/en/docs/agents-and-tools/tool-use/web-search-tool
	if textRequest.WebSearchOptions != nil {
		webSearchTool := dto.ClaudeWebSearchTool{
			Type: "web_search_20250305",
			Name: "web_search",
		}

		// 处理 user_location
		if textRequest.WebSearchOptions.UserLocation != nil {
			anthropicUserLocation := &dto.ClaudeWebSearchUserLocation{
				Type: "approximate", // 固定为 "approximate"
			}

			// 解析 UserLocation JSON
			var userLocationMap map[string]interface{}
			if err := common.Unmarshal(textRequest.WebSearchOptions.UserLocation, &userLocationMap); err == nil {
				// 检查是否有 approximate 字段
				if approximateData, ok := userLocationMap["approximate"].(map[string]interface{}); ok {
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

		// 处理 search_context_size 转换为 max_uses
		if textRequest.WebSearchOptions.SearchContextSize != "" {
			switch textRequest.WebSearchOptions.SearchContextSize {
			case "low":
				webSearchTool.MaxUses = WebSearchMaxUsesLow
			case "medium":
				webSearchTool.MaxUses = WebSearchMaxUsesMedium
			case "high":
				webSearchTool.MaxUses = WebSearchMaxUsesHigh
			}
		}

		claudeTools = append(claudeTools, &webSearchTool)
	}

	// response_format=json_schema has no native equivalent on the Claude (OpenAI)
	// path; emulate structured output by wrapping the schema as a synthetic tool
	// and forcing the model to call it. The tool_use input is unwrapped back into
	// message content on the response side.
	jsonSchemaToolName := ""
	if rf := textRequest.ResponseFormat; rf != nil && rf.Type == "json_schema" && len(rf.JsonSchema) > 0 {
		var fjs dto.FormatJsonSchema
		if err := common.Unmarshal(rf.JsonSchema, &fjs); err == nil {
			if schemaMap, ok := fjs.Schema.(map[string]any); ok && len(schemaMap) > 0 {
				jsonSchemaToolName = strings.TrimSpace(fjs.Name)
				if jsonSchemaToolName == "" {
					jsonSchemaToolName = "json_schema_output"
				}
				desc := fjs.Description
				if desc == "" {
					desc = "Return the result strictly as arguments matching the required JSON schema."
				}
				claudeTools = append(claudeTools, &dto.Tool{
					Name:        jsonSchemaToolName,
					Description: desc,
					InputSchema: schemaMap,
				})
			}
		}
	}

	claudeRequest := dto.ClaudeRequest{
		Model:         textRequest.Model,
		StopSequences: nil,
		Temperature:   textRequest.Temperature,
		Tools:         claudeTools,
	}
	if maxTokens := textRequest.GetMaxTokens(); maxTokens > 0 {
		claudeRequest.MaxTokens = common.GetPointer(maxTokens)
	}
	if textRequest.TopP != nil {
		claudeRequest.TopP = common.GetPointer(*textRequest.TopP)
	}
	if textRequest.TopK != nil {
		claudeRequest.TopK = common.GetPointer(*textRequest.TopK)
	}
	if textRequest.IsStream(nil) {
		claudeRequest.Stream = common.GetPointer(true)
	}

	// 处理 tool_choice 和 parallel_tool_calls
	if textRequest.ToolChoice != nil || textRequest.ParallelTooCalls != nil {
		claudeToolChoice := mapToolChoice(textRequest.ToolChoice, textRequest.ParallelTooCalls)
		if claudeToolChoice != nil {
			claudeRequest.ToolChoice = claudeToolChoice
		}
	}

	// json_schema emulation forces the synthetic tool, overriding any client
	// tool_choice so the structured output is guaranteed.
	if jsonSchemaToolName != "" {
		claudeRequest.ToolChoice = &dto.ClaudeToolChoice{Type: "tool", Name: jsonSchemaToolName}
		if c != nil {
			c.Set(jsonSchemaToolNameKey, jsonSchemaToolName)
		}
	}

	if claudeRequest.MaxTokens == nil || *claudeRequest.MaxTokens == 0 {
		defaultMaxTokens := uint(model_setting.GetClaudeSettings().GetDefaultMaxTokens(textRequest.Model))
		claudeRequest.MaxTokens = &defaultMaxTokens
	}

	if baseModel, effortLevel, ok := reasoning.TrimEffortSuffix(textRequest.Model); ok && effortLevel != "" &&
		(strings.HasPrefix(textRequest.Model, "claude-opus-4-6") || strings.HasPrefix(textRequest.Model, "claude-opus-4-7")) {
		claudeRequest.Model = baseModel
		claudeRequest.Thinking = &dto.Thinking{
			Type: "adaptive",
		}
		claudeRequest.OutputConfig = json.RawMessage(fmt.Sprintf(`{"effort":"%s"}`, effortLevel))
		if strings.HasPrefix(baseModel, "claude-opus-4-7") {
			// Opus 4.7 rejects non-default temperature/top_p/top_k with 400
			// and defaults display to "omitted"; restore the 4.6 visible summary.
			claudeRequest.Thinking.Display = "summarized"
			claudeRequest.Temperature = nil
			claudeRequest.TopP = nil
			claudeRequest.TopK = nil
		} else {
			claudeRequest.TopP = nil
			claudeRequest.Temperature = common.GetPointer[float64](1.0)
		}
	} else if model_setting.GetClaudeSettings().ThinkingAdapterEnabled &&
		strings.HasSuffix(textRequest.Model, "-thinking") {

		trimmedModel := strings.TrimSuffix(textRequest.Model, "-thinking")
		if strings.HasPrefix(trimmedModel, "claude-opus-4-7") {
			// Opus 4.7 rejects thinking.type="enabled"; use adaptive at high effort.
			claudeRequest.Thinking = &dto.Thinking{Type: "adaptive", Display: "summarized"}
			claudeRequest.OutputConfig = json.RawMessage(`{"effort":"high"}`)
			claudeRequest.Temperature = nil
			claudeRequest.TopP = nil
			claudeRequest.TopK = nil
		} else {
			// 因为BudgetTokens 必须大于1024
			if claudeRequest.MaxTokens == nil || *claudeRequest.MaxTokens < 1280 {
				claudeRequest.MaxTokens = common.GetPointer[uint](1280)
			}

			// BudgetTokens 为 max_tokens 的 80%
			claudeRequest.Thinking = &dto.Thinking{
				Type:         "enabled",
				BudgetTokens: common.GetPointer[int](int(float64(*claudeRequest.MaxTokens) * model_setting.GetClaudeSettings().ThinkingAdapterBudgetTokensPercentage)),
			}
			// TODO: 临时处理
			// https://docs.anthropic.com/en/docs/build-with-claude/extended-thinking#important-considerations-when-using-extended-thinking
			claudeRequest.TopP = nil
			claudeRequest.Temperature = common.GetPointer[float64](1.0)
		}
		if !model_setting.ShouldPreserveThinkingSuffix(textRequest.Model) {
			claudeRequest.Model = trimmedModel
		}
	}

	if textRequest.ReasoningEffort != "" {
		switch textRequest.ReasoningEffort {
		case "low":
			claudeRequest.Thinking = &dto.Thinking{
				Type:         "enabled",
				BudgetTokens: common.GetPointer[int](1280),
			}
		case "medium":
			claudeRequest.Thinking = &dto.Thinking{
				Type:         "enabled",
				BudgetTokens: common.GetPointer[int](2048),
			}
		case "high":
			claudeRequest.Thinking = &dto.Thinking{
				Type:         "enabled",
				BudgetTokens: common.GetPointer[int](4096),
			}
		}
	}

	// 指定了 reasoning 参数,覆盖 budgetTokens
	if textRequest.Reasoning != nil {
		var reasoning openrouter.RequestReasoning
		if err := common.Unmarshal(textRequest.Reasoning, &reasoning); err != nil {
			return nil, err
		}

		// reasoning.enabled alone must turn thinking on; budget_tokens is optional
		// (defaults to a sane value, since the enabled type requires >=1024).
		if reasoning.Enabled || reasoning.MaxTokens > 0 {
			budgetTokens := reasoning.MaxTokens
			if budgetTokens <= 0 {
				budgetTokens = 4096
			}
			claudeRequest.Thinking = &dto.Thinking{
				Type:         "enabled",
				BudgetTokens: &budgetTokens,
			}
		}
	}

	if textRequest.Stop != nil {
		// stop maybe string/array string, convert to array string
		switch textRequest.Stop.(type) {
		case string:
			claudeRequest.StopSequences = []string{textRequest.Stop.(string)}
		case []interface{}:
			stopSequences := make([]string, 0)
			for _, stop := range textRequest.Stop.([]interface{}) {
				stopSequences = append(stopSequences, stop.(string))
			}
			claudeRequest.StopSequences = stopSequences
		}
	}
	formatMessages := make([]dto.Message, 0)
	lastMessage := dto.Message{
		Role: "tool",
	}
	for i, message := range textRequest.Messages {
		if message.Role == "" {
			textRequest.Messages[i].Role = "user"
		}
		// New OpenAI SDK sends "developer" in place of "system"; map it so the
		// accumulation logic below routes it into Claude's system field.
		if message.Role == "developer" {
			message.Role = "system"
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
				fmtMessage.SetStringContent(strings.Trim(fmt.Sprintf("%s %s", lastMessage.StringContent(), message.StringContent()), "\""))
				// delete last message
				formatMessages = formatMessages[:len(formatMessages)-1]
			}
		}
		if fmtMessage.Content == nil || (fmtMessage.IsStringContent() && fmtMessage.StringContent() == "") {
			fmtMessage.SetStringContent("...")
		}
		formatMessages = append(formatMessages, fmtMessage)
		lastMessage = fmtMessage
	}

	claudeMessages := make([]dto.ClaudeMessage, 0)
	isFirstMessage := true
	// 初始化system消息数组，用于累积多个system消息
	var systemMessages []dto.ClaudeMediaMessage
	// 缓存「原始 tool id -> 净化后 id」，保证 tool_use 与 tool_result 映射一致
	toolIdMap := map[string]string{}

	for _, message := range formatMessages {
		if message.Role == "system" {
			// 根据Claude API规范，system字段使用数组格式更有通用性
			if message.IsStringContent() {
				if text := message.StringContent(); text != "" {
					systemMessages = append(systemMessages, dto.ClaudeMediaMessage{
						Type: "text",
						Text: common.GetPointer[string](text),
					})
				}
			} else {
				// 支持复合内容的system消息（虽然不常见，但需要考虑完整性）
				for _, ctx := range message.ParseContent() {
					if ctx.Type == "text" && ctx.Text != "" {
						systemMessages = append(systemMessages, dto.ClaudeMediaMessage{
							Type: "text",
							Text: common.GetPointer[string](ctx.Text),
						})
					}
					// 未来可以在这里扩展对图片等其他类型的支持
				}
			}
		} else {
			if isFirstMessage {
				isFirstMessage = false
				if message.Role != "user" {
					// fix: first message is assistant, add user message
					claudeMessage := dto.ClaudeMessage{
						Role: "user",
						Content: []dto.ClaudeMediaMessage{
							{
								Type: "text",
								Text: common.GetPointer[string]("..."),
							},
						},
					}
					claudeMessages = append(claudeMessages, claudeMessage)
				}
			}
			claudeMessage := dto.ClaudeMessage{
				Role: message.Role,
			}
			if message.Role == "tool" {
				if len(claudeMessages) > 0 && claudeMessages[len(claudeMessages)-1].Role == "user" {
					lastMessage := claudeMessages[len(claudeMessages)-1]
					if content, ok := lastMessage.Content.(string); ok {
						lastMessage.Content = []dto.ClaudeMediaMessage{
							{
								Type: "text",
								Text: common.GetPointer[string](content),
							},
						}
					}
					lastMessage.Content = append(lastMessage.Content.([]dto.ClaudeMediaMessage), dto.ClaudeMediaMessage{
						Type:      "tool_result",
						ToolUseId: sanitizeToolUseID(message.ToolCallId, toolIdMap),
						Content:   message.Content,
					})
					claudeMessages[len(claudeMessages)-1] = lastMessage
					continue
				} else {
					claudeMessage.Role = "user"
					claudeMessage.Content = []dto.ClaudeMediaMessage{
						{
							Type:      "tool_result",
							ToolUseId: sanitizeToolUseID(message.ToolCallId, toolIdMap),
							Content:   message.Content,
						},
					}
				}
			} else if message.IsStringContent() && message.ToolCalls == nil {
				text := message.StringContent()
				if text == "" {
					text = "..."
				}
				claudeMessage.Content = text
			} else {
				claudeMediaMessages := make([]dto.ClaudeMediaMessage, 0)
				for _, mediaMessage := range message.ParseContent() {
					switch mediaMessage.Type {
					case "text":
						if mediaMessage.Text != "" {
							claudeMediaMessages = append(claudeMediaMessages, dto.ClaudeMediaMessage{
								Type: "text",
								Text: common.GetPointer[string](mediaMessage.Text),
							})
						}
					default:
						source := mediaMessage.ToFileSource()
						if source == nil {
							continue
						}
						base64Data, mimeType, err := service.GetBase64Data(c, source, "formatting image for Claude")
						if err != nil {
							return nil, fmt.Errorf("get file data failed: %s", err.Error())
						}
						claudeMediaMessage := dto.ClaudeMediaMessage{
							Source: &dto.ClaudeMessageSource{
								Type: "base64",
							},
						}
						if strings.HasPrefix(mimeType, "application/pdf") {
							claudeMediaMessage.Type = "document"
						} else {
							claudeMediaMessage.Type = "image"
						}

						claudeMediaMessage.Source.MediaType = mimeType
						claudeMediaMessage.Source.Data = base64Data
						claudeMediaMessages = append(claudeMediaMessages, claudeMediaMessage)
						continue
					}
				}

				if message.ToolCalls != nil {
					for _, toolCall := range message.ParseToolCalls() {
						inputObj := make(map[string]any)
						if args := strings.TrimSpace(toolCall.Function.Arguments); args != "" {
							// 部分客户端会在 JSON 对象后带垃圾(如 <|tool_calls_section_end|>)，
							// Decoder 只读第一个 JSON 值、忽略尾部，Unmarshal 则会失败。
							// 解析失败时保留空对象，确保 tool_use 块仍被生成、与其
							// tool_result 配对，避免上游因孤儿 tool_result 报 400。
							if err := common.DecodeJson(strings.NewReader(args), &inputObj); err != nil {
								common.SysLog("tool call arguments not a JSON object, using empty input: " + fmt.Sprintf("%v", toolCall.Function.Arguments))
								inputObj = make(map[string]any)
							}
						}
						claudeMediaMessages = append(claudeMediaMessages, dto.ClaudeMediaMessage{
							Type:  "tool_use",
							Id:    sanitizeToolUseID(toolCall.ID, toolIdMap),
							Name:  toolCall.Function.Name,
							Input: inputObj,
						})
					}
				}
				claudeMessage.Content = claudeMediaMessages
			}
			claudeMessages = append(claudeMessages, claudeMessage)
		}
	}

	// 设置累积的system消息
	if len(systemMessages) > 0 {
		claudeRequest.System = systemMessages
	}

	claudeRequest.Prompt = ""
	claudeRequest.Messages = claudeMessages

	// Anthropic-native thinking object / effort sent through the OpenAI-compatible
	// endpoint. The base conversion above never reads them, so an explicit client
	// thinking object would otherwise be dropped. Honor them as the final override.
	if len(textRequest.THINKING) > 0 {
		var thinking dto.Thinking
		if err := common.Unmarshal(textRequest.THINKING, &thinking); err == nil && thinking.Type != "" {
			// Opus 4.7+ removed thinking.type="enabled" (upstream returns 400). When a
			// client passes it directly through the OpenAI-compatible endpoint, surface
			// the same 400 rather than silently rewriting it, so callers learn to switch
			// to adaptive. Internally-generated enabled (from reasoning / reasoning_effort
			// above) is not affected: it is converted to adaptive a few lines below.
			if thinking.Type == "enabled" && isAdaptiveOnlyModel(claudeRequest.Model) {
				return nil, types.NewError(
					fmt.Errorf("thinking.type \"enabled\" is not supported on %s; use thinking.type \"adaptive\" with output_config.effort", claudeRequest.Model),
					types.ErrorCodeInvalidRequest,
					types.ErrOptionWithStatusCode(http.StatusBadRequest),
					types.ErrOptionWithSkipRetry(),
				)
			}
			claudeRequest.Thinking = &thinking
		}
	}
	if textRequest.Effort != "" {
		claudeRequest.Effort = textRequest.Effort
	}

	// OpenAI-compat path: Opus 4.7+ and Fable models only support adaptive thinking
	// and reject thinking.type="enabled" with a 400. Client-direct thinking is
	// already rejected above; here we transparently switch internally-generated
	// enabled thinking (from reasoning_effort / reasoning inputs) to adaptive so
	// those requests succeed and return visible reasoning.
	if claudeRequest.Thinking != nil && claudeRequest.Thinking.Type == "enabled" &&
		isAdaptiveOnlyModel(claudeRequest.Model) {
		claudeRequest.Thinking.Type = "adaptive"
		claudeRequest.Thinking.BudgetTokens = nil
		if claudeRequest.Thinking.Display == "" {
			claudeRequest.Thinking.Display = "summarized"
		}
	}

	ApplyClaudeThinkingPolicy(&claudeRequest)
	ApplyClaudeSamplingPolicy(&claudeRequest)
	ApplyClaudeToolChoicePolicy(&claudeRequest)
	return &claudeRequest, nil
}

// ApplyClaudeThinkingPolicy normalizes thinking/effort parameters before the
// request is sent upstream:
//   - a lenient top-level effort is merged into output_config.effort (Anthropic
//     has no top-level effort field), then cleared;
//   - on Opus 4.7+ adaptive thinking defaults display to "omitted", so restore
//     the visible summary when the client requested adaptive without a display.
//
// Exported so other Anthropic-family upstreams (e.g. Vertex) can apply the same
// normalization on their native /v1/messages path.
func ApplyClaudeThinkingPolicy(req *dto.ClaudeRequest) {
	if req == nil {
		return
	}
	// Native /v1/messages path: honor an OpenRouter-style reasoning field by
	// translating it into Claude thinking. Opus 4.7+ and Fable models only accept
	// adaptive thinking; older models keep enabled semantics with a budget. The
	// field is always cleared so it is never forwarded upstream (Anthropic rejects it).
	if len(req.Reasoning) > 0 {
		if req.Thinking == nil {
			var r openrouter.RequestReasoning
			if err := common.Unmarshal(req.Reasoning, &r); err == nil && (r.Enabled || r.MaxTokens > 0) {
				if isAdaptiveOnlyModel(req.Model) {
					req.Thinking = &dto.Thinking{Type: "adaptive"}
				} else {
					budget := r.MaxTokens
					if budget <= 0 {
						budget = 4096
					}
					req.Thinking = &dto.Thinking{Type: "enabled", BudgetTokens: &budget}
				}
			}
		}
		req.Reasoning = nil
	}
	if req.Effort != "" {
		req.OutputConfig = mergeEffortIntoOutputConfig(req.OutputConfig, req.Effort)
		req.Effort = ""
	}
	if req.Thinking != nil && isAdaptiveOnlyModel(req.Model) &&
		req.Thinking.Type == "adaptive" && req.Thinking.Display == "" {
		req.Thinking.Display = "summarized"
	}
}

// mergeEffortIntoOutputConfig sets output_config.effort while preserving any
// other keys. An effort already present in output_config wins (explicit nesting
// is more specific than the top-level alias).
func mergeEffortIntoOutputConfig(existing json.RawMessage, effort string) json.RawMessage {
	if effort == "" {
		return existing
	}
	cfg := map[string]any{}
	if len(existing) > 0 {
		if err := common.Unmarshal(existing, &cfg); err != nil {
			return existing
		}
	}
	if _, ok := cfg["effort"]; ok {
		return existing
	}
	cfg["effort"] = effort
	data, err := common.Marshal(cfg)
	if err != nil {
		return existing
	}
	return json.RawMessage(data)
}

// opusVersionAtLeast47 reports whether model is claude-opus with version >= 4.7.
// Anthropic deprecated temperature/top_p/top_k for Opus 4.7 and later: any
// non-default value returns 400. Parsing the version (rather than matching a
// fixed string) auto-covers future opus releases (4-8, 4-9, 5-x).
func opusVersionAtLeast47(model string) bool {
	rest, ok := strings.CutPrefix(model, "claude-opus-")
	if !ok {
		return false
	}
	parts := strings.SplitN(rest, "-", 3) // "4-7" / "4-8" / "4-7-20260416" / "5-0"
	if len(parts) < 2 {
		return false
	}
	major, err1 := strconv.Atoi(parts[0])
	minor, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return false
	}
	return major > 4 || (major == 4 && minor >= 7)
}

// IsFableModel reports whether the model is a claude-fable series model.
// Fable models share the same restrictions as Opus 4.7+: top_p/temperature/top_k
// are deprecated and thinking.type="enabled" is not supported (adaptive only).
// They also do not support forced tool_choice (type=any/tool).
func IsFableModel(model string) bool {
	return strings.HasPrefix(model, "claude-fable-")
}

// isAdaptiveOnlyModel reports whether the model requires adaptive thinking
// (thinking.type="enabled" returns 400). Covers Opus 4.7+ and all Fable models.
func isAdaptiveOnlyModel(model string) bool {
	return opusVersionAtLeast47(model) || IsFableModel(model)
}

// ApplyClaudeSamplingPolicy strips sampling params that the upstream would
// reject, before the request is sent. Opus 4.7+ and Fable models reject any
// temperature/top_p/top_k; sonnet/haiku and older opus still support them.
// Since Opus 4.1, temperature and top_p cannot both be supplied, so drop top_p
// when both exist.
//
// Exported so other Anthropic-family upstreams (e.g. Vertex) can apply the same
// normalization on their native /v1/messages path.
func ApplyClaudeSamplingPolicy(req *dto.ClaudeRequest) {
	if req == nil {
		return
	}
	if isAdaptiveOnlyModel(req.Model) {
		req.Temperature, req.TopP, req.TopK = nil, nil, nil
		return
	}
	if req.Temperature != nil && req.TopP != nil {
		req.TopP = nil
	}
}

// ApplyClaudeToolChoicePolicy strips forced tool_choice types (any, tool) for
// models that do not support them, silently downgrading to auto. Currently
// applies to all Fable models, which reject forced tool_choice with 400.
//
// Exported so other Anthropic-family upstreams (e.g. Vertex) can apply the same
// normalization on their native /v1/messages path.
func ApplyClaudeToolChoicePolicy(req *dto.ClaudeRequest) {
	if req == nil || req.ToolChoice == nil {
		return
	}
	if IsFableModel(req.Model) {
		if tc, ok := req.ToolChoice.(*dto.ClaudeToolChoice); ok {
			if tc.Type == "any" || tc.Type == "tool" {
				req.ToolChoice = &dto.ClaudeToolChoice{Type: "auto"}
			}
		}
	}
}

func StreamResponseClaude2OpenAI(claudeResponse *dto.ClaudeResponse, jsonSchemaToolName string) *dto.ChatCompletionsStreamResponse {
	var response dto.ChatCompletionsStreamResponse
	response.Object = "chat.completion.chunk"
	response.Model = claudeResponse.Model
	response.Choices = make([]dto.ChatCompletionsStreamResponseChoice, 0)
	tools := make([]dto.ToolCallResponse, 0)
	fcIdx := 0
	if claudeResponse.Index != nil {
		fcIdx = *claudeResponse.Index - 1
		if fcIdx < 0 {
			fcIdx = 0
		}
	}
	var choice dto.ChatCompletionsStreamResponseChoice
	if claudeResponse.Type == "message_start" {
		if claudeResponse.Message != nil {
			response.Id = claudeResponse.Message.Id
			response.Model = claudeResponse.Message.Model
		}
		//claudeUsage = &claudeResponse.Message.Usage
		choice.Delta.SetContentString("")
		choice.Delta.Role = "assistant"
	} else if claudeResponse.Type == "content_block_start" {
		if claudeResponse.ContentBlock != nil {
			// 如果是文本块，尽可能发送首段文本（若存在）
			if claudeResponse.ContentBlock.Type == "text" && claudeResponse.ContentBlock.Text != nil {
				choice.Delta.SetContentString(*claudeResponse.ContentBlock.Text)
			}
			if claudeResponse.ContentBlock.Type == "tool_use" {
				// json_schema emulation: the forced tool's start carries no
				// content; its arguments stream in as input_json_delta and are
				// surfaced as message content below, not as a tool call.
				if jsonSchemaToolName != "" && claudeResponse.ContentBlock.Name == jsonSchemaToolName {
					choice.Delta.SetContentString("")
				} else {
					tools = append(tools, dto.ToolCallResponse{
						Index: common.GetPointer(fcIdx),
						ID:    claudeResponse.ContentBlock.Id,
						Type:  "function",
						Function: dto.FunctionResponse{
							Name:      claudeResponse.ContentBlock.Name,
							Arguments: "",
						},
					})
				}
			}
		} else {
			return nil
		}
	} else if claudeResponse.Type == "content_block_delta" {
		if claudeResponse.Delta != nil {
			choice.Delta.Content = claudeResponse.Delta.Text
			switch claudeResponse.Delta.Type {
			case "input_json_delta":
				// json_schema emulation: stream the forced tool's argument
				// fragments as plain content instead of tool_call arguments.
				if jsonSchemaToolName != "" {
					if claudeResponse.Delta.PartialJson != nil {
						choice.Delta.SetContentString(*claudeResponse.Delta.PartialJson)
					}
					break
				}
				tools = append(tools, dto.ToolCallResponse{
					Type:  "function",
					Index: common.GetPointer(fcIdx),
					Function: dto.FunctionResponse{
						Arguments: *claudeResponse.Delta.PartialJson,
					},
				})
			case "signature_delta":
				// 加密的不处理
				signatureContent := "\n"
				choice.Delta.ReasoningContent = &signatureContent
			case "thinking_delta":
				choice.Delta.ReasoningContent = claudeResponse.Delta.Thinking
			}
		}
	} else if claudeResponse.Type == "message_delta" {
		if claudeResponse.Delta != nil && claudeResponse.Delta.StopReason != nil {
			finishReason := stopReasonClaude2OpenAI(*claudeResponse.Delta.StopReason)
			if finishReason != "null" {
				choice.FinishReason = &finishReason
			}
		}
		//claudeUsage = &claudeResponse.Usage
	} else if claudeResponse.Type == "message_stop" {
		return nil
	} else {
		return nil
	}
	if len(tools) > 0 {
		choice.Delta.Content = nil // compatible with other OpenAI derivative applications, like LobeOpenAICompatibleFactory ...
		choice.Delta.ToolCalls = tools
	}
	response.Choices = append(response.Choices, choice)

	return &response
}

func ResponseClaude2OpenAI(claudeResponse *dto.ClaudeResponse, jsonSchemaToolName string) *dto.OpenAITextResponse {
	choices := make([]dto.OpenAITextResponseChoice, 0)
	fullTextResponse := dto.OpenAITextResponse{
		Id:      fmt.Sprintf("chatcmpl-%s", common.GetUUID()),
		Object:  "chat.completion",
		Created: common.GetTimestamp(),
	}
	var responseText string
	var responseThinking string
	if len(claudeResponse.Content) > 0 {
		responseText = claudeResponse.Content[0].GetText()
		if claudeResponse.Content[0].Thinking != nil {
			responseThinking = *claudeResponse.Content[0].Thinking
		}
	}
	tools := make([]dto.ToolCallResponse, 0)
	thinkingContent := ""
	jsonSchemaContent := ""

	fullTextResponse.Id = claudeResponse.Id
	for _, message := range claudeResponse.Content {
		switch message.Type {
		case "tool_use":
			// json_schema emulation: unwrap the forced tool's input back into
			// message content as a JSON string instead of a tool call.
			if jsonSchemaToolName != "" && message.Name == jsonSchemaToolName {
				args, _ := json.Marshal(message.Input)
				jsonSchemaContent = string(args)
				if jsonSchemaContent == "" || jsonSchemaContent == "null" {
					jsonSchemaContent = "{}"
				}
				continue
			}
			args, _ := json.Marshal(message.Input)
			arguments := string(args)
			if arguments == "" || arguments == "null" {
				arguments = "{}"
			}
			tools = append(tools, dto.ToolCallResponse{
				ID:   message.Id,
				Type: "function", // compatible with other OpenAI derivative applications
				Function: dto.FunctionResponse{
					Name:      message.Name,
					Arguments: arguments,
				},
			})
		case "thinking":
			// 加密的不管， 只输出明文的推理过程
			if message.Thinking != nil {
				thinkingContent = *message.Thinking
			}
		case "text":
			responseText = message.GetText()
		}
	}
	if jsonSchemaContent != "" {
		responseText = jsonSchemaContent
	}
	choice := dto.OpenAITextResponseChoice{
		Index: 0,
		Message: dto.Message{
			Role: "assistant",
		},
		FinishReason: stopReasonClaude2OpenAI(claudeResponse.StopReason),
	}
	choice.SetStringContent(responseText)
	if len(responseThinking) > 0 {
		choice.ReasoningContent = responseThinking
	}
	if len(tools) > 0 {
		choice.Message.SetToolCalls(tools)
	}
	choice.Message.ReasoningContent = thinkingContent
	fullTextResponse.Model = claudeResponse.Model
	choices = append(choices, choice)
	fullTextResponse.Choices = choices
	return &fullTextResponse
}

type ClaudeResponseInfo struct {
	ResponseId   string
	Created      int64
	Model        string
	ResponseText strings.Builder
	Usage        *dto.Usage
	Done         bool
	// toolArgsSeen tracks, per streamed tool_use content block index, whether any
	// input_json_delta was emitted. Used to backfill "{}" for no-argument tools.
	toolArgsSeen map[int]bool
}

func cacheCreationTokensForOpenAIUsage(usage *dto.Usage) int {
	if usage == nil {
		return 0
	}
	splitCacheCreationTokens := usage.ClaudeCacheCreation5mTokens + usage.ClaudeCacheCreation1hTokens
	if splitCacheCreationTokens == 0 {
		return usage.PromptTokensDetails.CachedCreationTokens
	}
	if usage.PromptTokensDetails.CachedCreationTokens > splitCacheCreationTokens {
		return usage.PromptTokensDetails.CachedCreationTokens
	}
	return splitCacheCreationTokens
}

func buildOpenAIStyleUsageFromClaudeUsage(usage *dto.Usage) dto.Usage {
	if usage == nil {
		return dto.Usage{}
	}
	clone := *usage
	clone.ClaudeCacheCreation5mTokens, clone.ClaudeCacheCreation1hTokens = service.NormalizeCacheCreationSplit(
		usage.PromptTokensDetails.CachedCreationTokens,
		usage.ClaudeCacheCreation5mTokens,
		usage.ClaudeCacheCreation1hTokens,
	)
	cacheCreationTokens := cacheCreationTokensForOpenAIUsage(usage)
	totalInputTokens := usage.PromptTokens + usage.PromptTokensDetails.CachedTokens + cacheCreationTokens
	clone.PromptTokens = totalInputTokens
	clone.InputTokens = totalInputTokens
	clone.TotalTokens = totalInputTokens + usage.CompletionTokens
	clone.UsageSemantic = "openai"
	clone.UsageSource = "anthropic"
	return clone
}

func buildMessageDeltaPatchUsage(claudeResponse *dto.ClaudeResponse, claudeInfo *ClaudeResponseInfo) *dto.ClaudeUsage {
	usage := &dto.ClaudeUsage{}
	if claudeResponse != nil && claudeResponse.Usage != nil {
		*usage = *claudeResponse.Usage
	}

	if claudeInfo == nil || claudeInfo.Usage == nil {
		return usage
	}

	if usage.InputTokens == 0 && claudeInfo.Usage.PromptTokens > 0 {
		usage.InputTokens = claudeInfo.Usage.PromptTokens
	}
	if usage.CacheReadInputTokens == 0 && claudeInfo.Usage.PromptTokensDetails.CachedTokens > 0 {
		usage.CacheReadInputTokens = claudeInfo.Usage.PromptTokensDetails.CachedTokens
	}
	if usage.CacheCreationInputTokens == 0 && claudeInfo.Usage.PromptTokensDetails.CachedCreationTokens > 0 {
		usage.CacheCreationInputTokens = claudeInfo.Usage.PromptTokensDetails.CachedCreationTokens
	}
	cacheCreation5m := 0
	cacheCreation1h := 0
	if usage.CacheCreation != nil {
		cacheCreation5m = usage.CacheCreation.Ephemeral5mInputTokens
		cacheCreation1h = usage.CacheCreation.Ephemeral1hInputTokens
	} else {
		cacheCreation5m = claudeInfo.Usage.ClaudeCacheCreation5mTokens
		cacheCreation1h = claudeInfo.Usage.ClaudeCacheCreation1hTokens
	}
	cacheCreation5m, cacheCreation1h = service.NormalizeCacheCreationSplit(
		usage.CacheCreationInputTokens,
		cacheCreation5m,
		cacheCreation1h,
	)
	if usage.CacheCreation == nil && (cacheCreation5m > 0 || cacheCreation1h > 0) {
		usage.CacheCreation = &dto.ClaudeCacheCreationUsage{}
	}
	if usage.CacheCreation != nil {
		usage.CacheCreation.Ephemeral5mInputTokens = cacheCreation5m
		usage.CacheCreation.Ephemeral1hInputTokens = cacheCreation1h
	}
	return usage
}

func shouldSkipClaudeMessageDeltaUsagePatch(info *relaycommon.RelayInfo) bool {
	if model_setting.GetGlobalSettings().PassThroughRequestEnabled {
		return true
	}
	if info == nil {
		return false
	}
	return info.ChannelSetting.PassThroughBodyEnabled
}

func patchClaudeMessageDeltaUsageData(data string, usage *dto.ClaudeUsage) string {
	if data == "" || usage == nil {
		return data
	}

	data = setMessageDeltaUsageInt(data, "usage.input_tokens", usage.InputTokens)
	data = setMessageDeltaUsageInt(data, "usage.cache_read_input_tokens", usage.CacheReadInputTokens)
	data = setMessageDeltaUsageInt(data, "usage.cache_creation_input_tokens", usage.CacheCreationInputTokens)

	if usage.CacheCreation != nil {
		data = setMessageDeltaUsageInt(data, "usage.cache_creation.ephemeral_5m_input_tokens", usage.CacheCreation.Ephemeral5mInputTokens)
		data = setMessageDeltaUsageInt(data, "usage.cache_creation.ephemeral_1h_input_tokens", usage.CacheCreation.Ephemeral1hInputTokens)
	}

	return data
}

func setMessageDeltaUsageInt(data string, path string, localValue int) string {
	if localValue <= 0 {
		return data
	}

	upstreamValue := gjson.Get(data, path)
	if upstreamValue.Exists() && upstreamValue.Int() > 0 {
		return data
	}

	patchedData, err := sjson.Set(data, path, localValue)
	if err != nil {
		return data
	}
	return patchedData
}

func FormatClaudeResponseInfo(claudeResponse *dto.ClaudeResponse, oaiResponse *dto.ChatCompletionsStreamResponse, claudeInfo *ClaudeResponseInfo) bool {
	if claudeInfo == nil {
		return false
	}
	if claudeInfo.Usage == nil {
		claudeInfo.Usage = &dto.Usage{}
	}
	if claudeResponse.Type == "message_start" {
		if claudeResponse.Message != nil {
			claudeInfo.ResponseId = claudeResponse.Message.Id
			claudeInfo.Model = claudeResponse.Message.Model
		}

		// message_start, 获取usage
		if claudeResponse.Message != nil && claudeResponse.Message.Usage != nil {
			claudeInfo.Usage.PromptTokens = claudeResponse.Message.Usage.InputTokens
			claudeInfo.Usage.UsageSemantic = "anthropic"
			claudeInfo.Usage.PromptTokensDetails.CachedTokens = claudeResponse.Message.Usage.CacheReadInputTokens
			claudeInfo.Usage.PromptTokensDetails.CachedCreationTokens = claudeResponse.Message.Usage.CacheCreationInputTokens
			claudeInfo.Usage.ClaudeCacheCreation5mTokens = claudeResponse.Message.Usage.GetCacheCreation5mTokens()
			claudeInfo.Usage.ClaudeCacheCreation1hTokens = claudeResponse.Message.Usage.GetCacheCreation1hTokens()
			claudeInfo.Usage.CompletionTokens = claudeResponse.Message.Usage.OutputTokens
			claudeInfo.Usage.CompletionTokenDetails.ReasoningTokens = claudeResponse.Message.Usage.GetThinkingTokens()
		}
	} else if claudeResponse.Type == "content_block_delta" {
		if claudeResponse.Delta != nil {
			if claudeResponse.Delta.Text != nil {
				claudeInfo.ResponseText.WriteString(*claudeResponse.Delta.Text)
			}
			if claudeResponse.Delta.Thinking != nil {
				claudeInfo.ResponseText.WriteString(*claudeResponse.Delta.Thinking)
			}
		}
	} else if claudeResponse.Type == "message_delta" {
		// 最终的usage获取
		if claudeResponse.Usage != nil {
			claudeInfo.Usage.UsageSemantic = "anthropic"
			if claudeResponse.Usage.InputTokens > 0 {
				// 不叠加，只取最新的
				claudeInfo.Usage.PromptTokens = claudeResponse.Usage.InputTokens
			}
			if claudeResponse.Usage.CacheReadInputTokens > 0 {
				claudeInfo.Usage.PromptTokensDetails.CachedTokens = claudeResponse.Usage.CacheReadInputTokens
			}
			if claudeResponse.Usage.CacheCreationInputTokens > 0 {
				claudeInfo.Usage.PromptTokensDetails.CachedCreationTokens = claudeResponse.Usage.CacheCreationInputTokens
			}
			if cacheCreation5m := claudeResponse.Usage.GetCacheCreation5mTokens(); cacheCreation5m > 0 {
				claudeInfo.Usage.ClaudeCacheCreation5mTokens = cacheCreation5m
			}
			if cacheCreation1h := claudeResponse.Usage.GetCacheCreation1hTokens(); cacheCreation1h > 0 {
				claudeInfo.Usage.ClaudeCacheCreation1hTokens = cacheCreation1h
			}
			if claudeResponse.Usage.OutputTokens > 0 {
				claudeInfo.Usage.CompletionTokens = claudeResponse.Usage.OutputTokens
			}
			if thinkingTokens := claudeResponse.Usage.GetThinkingTokens(); thinkingTokens > 0 {
				claudeInfo.Usage.CompletionTokenDetails.ReasoningTokens = thinkingTokens
			}
			claudeInfo.Usage.TotalTokens = claudeInfo.Usage.PromptTokens + claudeInfo.Usage.CompletionTokens
		}

		// 判断是否完整
		claudeInfo.Done = true
	} else if claudeResponse.Type == "content_block_start" {
	} else {
		return false
	}
	if oaiResponse != nil {
		oaiResponse.Id = claudeInfo.ResponseId
		oaiResponse.Created = claudeInfo.Created
		oaiResponse.Model = claudeInfo.Model
	}
	return true
}

// trackToolUseArgs records whether each streamed tool_use block emitted any
// arguments. On block stop it returns a synthetic chunk carrying arguments "{}"
// for no-argument tools (Anthropic sends no input_json_delta for those), so
// OpenAI clients that concatenate argument fragments still get valid JSON.
func trackToolUseArgs(claudeInfo *ClaudeResponseInfo, cr *dto.ClaudeResponse) *dto.ChatCompletionsStreamResponse {
	idx := 0
	if cr.Index != nil {
		idx = *cr.Index
	}
	switch cr.Type {
	case "content_block_start":
		if cr.ContentBlock != nil && cr.ContentBlock.Type == "tool_use" {
			if claudeInfo.toolArgsSeen == nil {
				claudeInfo.toolArgsSeen = map[int]bool{}
			}
			claudeInfo.toolArgsSeen[idx] = false
		}
	case "content_block_delta":
		// Only a non-empty partial_json counts as real arguments. Anthropic may emit
		// an input_json_delta with partial_json="" even for no-argument tools, which
		// must NOT suppress the "{}" backfill.
		if cr.Delta != nil && cr.Delta.Type == "input_json_delta" &&
			cr.Delta.PartialJson != nil && *cr.Delta.PartialJson != "" {
			if _, ok := claudeInfo.toolArgsSeen[idx]; ok {
				claudeInfo.toolArgsSeen[idx] = true
			}
		}
	case "content_block_stop":
		if seen, ok := claudeInfo.toolArgsSeen[idx]; ok {
			delete(claudeInfo.toolArgsSeen, idx)
			if !seen {
				return buildEmptyToolArgsChunk(idx)
			}
		}
	}
	return nil
}

func buildEmptyToolArgsChunk(anthropicIndex int) *dto.ChatCompletionsStreamResponse {
	fcIdx := anthropicIndex - 1
	if fcIdx < 0 {
		fcIdx = 0
	}
	var resp dto.ChatCompletionsStreamResponse
	resp.Object = "chat.completion.chunk"
	choice := dto.ChatCompletionsStreamResponseChoice{}
	choice.Delta.ToolCalls = []dto.ToolCallResponse{
		{
			Index: common.GetPointer(fcIdx),
			Type:  "function",
			Function: dto.FunctionResponse{
				Arguments: "{}",
			},
		},
	}
	resp.Choices = []dto.ChatCompletionsStreamResponseChoice{choice}
	return &resp
}

func HandleStreamResponseData(c *gin.Context, info *relaycommon.RelayInfo, claudeInfo *ClaudeResponseInfo, data string) *types.NewAPIError {
	var claudeResponse dto.ClaudeResponse
	err := common.UnmarshalJsonStr(data, &claudeResponse)
	if err != nil {
		common.SysLog("error unmarshalling stream response: " + err.Error())
		return types.NewError(err, types.ErrorCodeBadResponseBody)
	}
	if claudeError := claudeResponse.GetClaudeError(); claudeError != nil && claudeError.Type != "" {
		return types.WithClaudeError(*claudeError, http.StatusInternalServerError)
	}
	if claudeResponse.StopReason != "" {
		maybeMarkClaudeRefusal(c, claudeResponse.StopReason)
	}
	if claudeResponse.Delta != nil && claudeResponse.Delta.StopReason != nil {
		maybeMarkClaudeRefusal(c, *claudeResponse.Delta.StopReason)
	}
	if info.RelayFormat == types.RelayFormatClaude {
		FormatClaudeResponseInfo(&claudeResponse, nil, claudeInfo)

		if claudeResponse.Type == "message_start" {
			// message_start, 获取usage
			if claudeResponse.Message != nil {
				info.UpstreamModelName = claudeResponse.Message.Model
			}
		} else if claudeResponse.Type == "message_delta" {
			// 确保 message_delta 的 usage 包含完整的 input_tokens 和 cache 相关字段
			// 解决 AWS Bedrock 等上游返回的 message_delta 缺少这些字段的问题
			if !shouldSkipClaudeMessageDeltaUsagePatch(info) {
				data = patchClaudeMessageDeltaUsageData(data, buildMessageDeltaPatchUsage(&claudeResponse, claudeInfo))
			}
		}
		helper.ClaudeChunkData(c, claudeResponse, data)
	} else if info.RelayFormat == types.RelayFormatOpenAI {
		jsonSchemaToolName := c.GetString(jsonSchemaToolNameKey)

		// In json_schema mode the forced tool's arguments are surfaced as content,
		// so the "{}" tool-args backfill (which emits a tool_call) does not apply.
		var emptyArgsChunk *dto.ChatCompletionsStreamResponse
		if jsonSchemaToolName == "" {
			emptyArgsChunk = trackToolUseArgs(claudeInfo, &claudeResponse)
		}

		response := StreamResponseClaude2OpenAI(&claudeResponse, jsonSchemaToolName)

		if FormatClaudeResponseInfo(&claudeResponse, response, claudeInfo) {
			if err = helper.ObjectData(c, response); err != nil {
				logger.LogError(c, "send_stream_response_failed: "+err.Error())
			}
		}

		if emptyArgsChunk != nil {
			emptyArgsChunk.Id = claudeInfo.ResponseId
			emptyArgsChunk.Created = claudeInfo.Created
			emptyArgsChunk.Model = claudeInfo.Model
			if err = helper.ObjectData(c, emptyArgsChunk); err != nil {
				logger.LogError(c, "send_stream_response_failed: "+err.Error())
			}
		}
	}
	return nil
}

func HandleStreamFinalResponse(c *gin.Context, info *relaycommon.RelayInfo, claudeInfo *ClaudeResponseInfo) {
	if claudeInfo.Usage.PromptTokens == 0 {
		//上游出错
	}
	if claudeInfo.Usage.CompletionTokens == 0 || !claudeInfo.Done {
		if common.DebugEnabled {
			common.SysLog("claude response usage is not complete, maybe upstream error")
		}
		// 只补缺失字段，不整份覆盖——保留 message_start 已拿到的 cache 字段
		fallback := service.ResponseText2Usage(c, claudeInfo.ResponseText.String(), info.UpstreamModelName, info.GetEstimatePromptTokens())
		if claudeInfo.Usage.CompletionTokens == 0 ||
			(!claudeInfo.Done && fallback.CompletionTokens > claudeInfo.Usage.CompletionTokens) {
			claudeInfo.Usage.CompletionTokens = fallback.CompletionTokens
		}
		if claudeInfo.Usage.PromptTokens == 0 {
			claudeInfo.Usage.PromptTokens = fallback.PromptTokens
		}
		claudeInfo.Usage.TotalTokens = claudeInfo.Usage.PromptTokens + claudeInfo.Usage.CompletionTokens
	}
	if claudeInfo.Usage != nil {
		claudeInfo.Usage.UsageSemantic = "anthropic"
	}

	if info.RelayFormat == types.RelayFormatClaude {
		//
	} else if info.RelayFormat == types.RelayFormatOpenAI {
		if info.ShouldIncludeUsage {
			openAIUsage := buildOpenAIStyleUsageFromClaudeUsage(claudeInfo.Usage)
			response := helper.GenerateFinalUsageResponse(claudeInfo.ResponseId, claudeInfo.Created, info.UpstreamModelName, openAIUsage)
			err := helper.ObjectData(c, response)
			if err != nil {
				common.SysLog("send final response failed: " + err.Error())
			}
		}
		helper.Done(c)
	}
}

func ClaudeStreamHandler(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (*dto.Usage, *types.NewAPIError) {
	claudeInfo := &ClaudeResponseInfo{
		ResponseId:   helper.GetResponseID(c),
		Created:      common.GetTimestamp(),
		Model:        info.UpstreamModelName,
		ResponseText: strings.Builder{},
		Usage:        &dto.Usage{},
	}
	var err *types.NewAPIError
	helper.StreamScannerHandler(c, resp, info, func(data string, sr *helper.StreamResult) {
		err = HandleStreamResponseData(c, info, claudeInfo, data)
		if err != nil {
			sr.Stop(err)
		}
	})
	if err != nil {
		return nil, err
	}

	info.UpstreamResponseId = claudeInfo.ResponseId

	HandleStreamFinalResponse(c, info, claudeInfo)
	return claudeInfo.Usage, nil
}

func HandleClaudeResponseData(c *gin.Context, info *relaycommon.RelayInfo, claudeInfo *ClaudeResponseInfo, httpResp *http.Response, data []byte) *types.NewAPIError {
	var claudeResponse dto.ClaudeResponse
	err := common.Unmarshal(data, &claudeResponse)
	if err != nil {
		return types.NewError(err, types.ErrorCodeBadResponseBody)
	}
	if claudeError := claudeResponse.GetClaudeError(); claudeError != nil && claudeError.Type != "" {
		return types.WithClaudeError(*claudeError, http.StatusInternalServerError)
	}
	maybeMarkClaudeRefusal(c, claudeResponse.StopReason)
	if claudeInfo.Usage == nil {
		claudeInfo.Usage = &dto.Usage{}
	}
	if claudeResponse.Usage != nil {
		claudeInfo.Usage.PromptTokens = claudeResponse.Usage.InputTokens
		claudeInfo.Usage.CompletionTokens = claudeResponse.Usage.OutputTokens
		claudeInfo.Usage.CompletionTokenDetails.ReasoningTokens = claudeResponse.Usage.GetThinkingTokens()
		claudeInfo.Usage.TotalTokens = claudeResponse.Usage.InputTokens + claudeResponse.Usage.OutputTokens
		claudeInfo.Usage.UsageSemantic = "anthropic"
		claudeInfo.Usage.PromptTokensDetails.CachedTokens = claudeResponse.Usage.CacheReadInputTokens
		claudeInfo.Usage.PromptTokensDetails.CachedCreationTokens = claudeResponse.Usage.CacheCreationInputTokens
		claudeInfo.Usage.ClaudeCacheCreation5mTokens = claudeResponse.Usage.GetCacheCreation5mTokens()
		claudeInfo.Usage.ClaudeCacheCreation1hTokens = claudeResponse.Usage.GetCacheCreation1hTokens()
	}
	var responseData []byte
	switch info.RelayFormat {
	case types.RelayFormatOpenAI:
		openaiResponse := ResponseClaude2OpenAI(&claudeResponse, c.GetString(jsonSchemaToolNameKey))
		openaiResponse.Usage = buildOpenAIStyleUsageFromClaudeUsage(claudeInfo.Usage)
		responseData, err = json.Marshal(openaiResponse)
		if err != nil {
			return types.NewError(err, types.ErrorCodeBadResponseBody)
		}
	case types.RelayFormatClaude:
		responseData = data
	}

	if claudeResponse.Usage != nil && claudeResponse.Usage.ServerToolUse != nil && claudeResponse.Usage.ServerToolUse.WebSearchRequests > 0 {
		c.Set("claude_web_search_requests", claudeResponse.Usage.ServerToolUse.WebSearchRequests)
	}

	service.IOCopyBytesGracefully(c, httpResp, responseData)
	return nil
}

func ClaudeHandler(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (*dto.Usage, *types.NewAPIError) {
	defer service.CloseResponseBodyGracefully(resp)

	claudeInfo := &ClaudeResponseInfo{
		ResponseId:   helper.GetResponseID(c),
		Created:      common.GetTimestamp(),
		Model:        info.UpstreamModelName,
		ResponseText: strings.Builder{},
		Usage:        &dto.Usage{},
	}
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
	}
	if common.DebugEnabled {
		println("responseBody: ", string(responseBody))
	}
	var claudeResponse dto.ClaudeResponse
	err = common.Unmarshal(responseBody, &claudeResponse)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
	}
	info.UpstreamResponseId = claudeResponse.Id

	handleErr := HandleClaudeResponseData(c, info, claudeInfo, resp, responseBody)
	if handleErr != nil {
		return nil, handleErr
	}
	return claudeInfo.Usage, nil
}

func mapToolChoice(toolChoice any, parallelToolCalls *bool) *dto.ClaudeToolChoice {
	var claudeToolChoice *dto.ClaudeToolChoice

	// 处理 tool_choice 字符串值
	if toolChoiceStr, ok := toolChoice.(string); ok {
		switch toolChoiceStr {
		case "auto":
			claudeToolChoice = &dto.ClaudeToolChoice{
				Type: "auto",
			}
		case "required":
			claudeToolChoice = &dto.ClaudeToolChoice{
				Type: "any",
			}
		case "none":
			claudeToolChoice = &dto.ClaudeToolChoice{
				Type: "none",
			}
		}
	} else if toolChoiceMap, ok := toolChoice.(map[string]interface{}); ok {
		// 处理 tool_choice 对象值
		if function, ok := toolChoiceMap["function"].(map[string]interface{}); ok {
			if toolName, ok := function["name"].(string); ok {
				claudeToolChoice = &dto.ClaudeToolChoice{
					Type: "tool",
					Name: toolName,
				}
			}
		}
	}

	// 处理 parallel_tool_calls
	if parallelToolCalls != nil {
		if claudeToolChoice == nil {
			// 如果没有 tool_choice，但有 parallel_tool_calls，创建默认的 auto 类型
			claudeToolChoice = &dto.ClaudeToolChoice{
				Type: "auto",
			}
		}

		// Anthropic schema: tool_choice.type=none does not accept extra fields.
		// When tools are disabled, parallel_tool_calls is irrelevant, so we drop it.
		if claudeToolChoice.Type != "none" {
			// 如果 parallel_tool_calls 为 true，则 disable_parallel_tool_use 为 false
			claudeToolChoice.DisableParallelToolUse = !*parallelToolCalls
		}
	}

	return claudeToolChoice
}
