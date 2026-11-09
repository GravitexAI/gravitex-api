package common

import (
	"strings"

	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// gptChatToolRule describes how a GPT model family handles function tools on
// Chat Completions. Families that keep tools only accept them with
// reasoning_effort "none"; families that drop tools reject them with any effort.
// rejectsNoneEffort marks models without a "none" effort level.
type gptChatToolRule struct {
	modelPrefix       string
	dropTools         bool
	rejectsNoneEffort bool
}

// gptChatToolRules is matched in order, so specific models must precede their
// family prefix. A prefix matches the exact name or the name followed by "-"
// (e.g. dated Azure names like gpt-5.6-sol-2026-07-09).
var gptChatToolRules = []gptChatToolRule{
	{modelPrefix: "gpt-6-astra", dropTools: true, rejectsNoneEffort: true},
	{modelPrefix: "gpt-6.1-sol", dropTools: true, rejectsNoneEffort: true},
	{modelPrefix: "gpt-6.1", dropTools: true},
	{modelPrefix: "gpt-6-sol"},
	{modelPrefix: "gpt-6-luna"},
	{modelPrefix: "gpt-5.6"},
}

// matchGPTChatToolRule returns the rule of a GPT-5.6+ model, or nil for any
// other model.
func matchGPTChatToolRule(model string) *gptChatToolRule {
	model = strings.TrimSpace(model)
	for i := range gptChatToolRules {
		prefix := gptChatToolRules[i].modelPrefix
		if model == prefix || strings.HasPrefix(model, prefix+"-") {
			return &gptChatToolRules[i]
		}
	}
	return nil
}

// IsGPT56PlusModel reports whether the model belongs to the GPT-5.6+ families
// listed in gptChatToolRules.
func IsGPT56PlusModel(model string) bool {
	return matchGPTChatToolRule(model) != nil
}

// GPTModelRejectsNoneEffort reports whether the model rejects reasoning effort
// "none" (GPT-6 Astra, GPT-6.1 Sol).
func GPTModelRejectsNoneEffort(model string) bool {
	rule := matchGPTChatToolRule(model)
	return rule != nil && rule.rejectsNoneEffort
}

// SanitizeGPT6ChatToolReasoningEffort applies the GPT-5.6 / GPT-6 Chat
// Completions compatibility rule. GPT-6 Astra and all GPT-6.1 models reject
// function tools on Chat Completions, so those tools are removed; Astra and
// GPT-6.1 Sol additionally reject "none" as an effort, so it is lowered to
// "low", their lightest level, while GPT-6.1 Terra accepts "none". GPT-5.6, GPT-6 Sol and GPT-6 Luna accept function tools only with
// reasoning_effort "none", so the effort is forced to "none" to keep the tools.
func SanitizeGPT6ChatToolReasoningEffort(data []byte, info *RelayInfo) ([]byte, bool, error) {
	if info == nil || info.RelayMode != relayconstant.RelayModeChatCompletions {
		return data, false, nil
	}

	rule := matchGPTChatToolRule(gjson.GetBytes(data, "model").String())
	if rule == nil {
		return data, false, nil
	}

	effortIsNone := strings.EqualFold(strings.TrimSpace(gjson.GetBytes(data, "reasoning_effort").String()), "none")
	if rule.dropTools {
		lowerNone := rule.rejectsNoneEffort && effortIsNone
		if !hasOpenAIFunctionTool(data) && !lowerNone {
			return data, false, nil
		}

		result := data
		var err error
		for _, path := range []string{"tools", "functions", "tool_choice", "function_call"} {
			result, err = sjson.DeleteBytes(result, path)
			if err != nil {
				return nil, false, err
			}
		}
		if lowerNone {
			result, err = sjson.SetBytes(result, "reasoning_effort", "low")
			if err != nil {
				return nil, false, err
			}
		}
		info.SetReasoningEffort(strings.TrimSpace(gjson.GetBytes(result, "reasoning_effort").String()))
		return result, true, nil
	}

	if effortIsNone || !hasOpenAIFunctionTool(data) {
		return data, false, nil
	}

	result, err := sjson.SetBytes(data, "reasoning_effort", "none")
	if err != nil {
		return nil, false, err
	}
	info.SetReasoningEffort("none")
	return result, true, nil
}

func hasOpenAIFunctionTool(data []byte) bool {
	tools := gjson.GetBytes(data, "tools")
	if tools.IsArray() {
		for _, tool := range tools.Array() {
			if strings.EqualFold(strings.TrimSpace(tool.Get("type").String()), "function") {
				return true
			}
		}
	}

	functions := gjson.GetBytes(data, "functions")
	return functions.IsArray() && len(functions.Array()) > 0
}
