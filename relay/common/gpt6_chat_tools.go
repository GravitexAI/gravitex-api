package common

import (
	"strings"

	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

var gpt6ChatFunctionToolModels = map[string]struct{}{
	"gpt-6-sol":  {},
	"gpt-6-luna": {},
}

// SanitizeGPT6ChatToolReasoningEffort applies the GPT-6 Chat Completions
// compatibility rule. GPT-6.1 Sol rejects "none" as an effort and rejects
// function tools with any supported effort, so Chat Completions requests for
// that model must omit function tools. Sol and Luna accept an explicit "none"
// to disable reasoning while preserving function tools.
func SanitizeGPT6ChatToolReasoningEffort(data []byte, info *RelayInfo) ([]byte, bool, error) {
	if info == nil || info.RelayMode != relayconstant.RelayModeChatCompletions {
		return data, false, nil
	}

	model := strings.TrimSpace(gjson.GetBytes(data, "model").String())
	effort := strings.TrimSpace(gjson.GetBytes(data, "reasoning_effort").String())
	if model == "gpt-6.1-sol" {
		if !hasOpenAIFunctionTool(data) && !strings.EqualFold(effort, "none") {
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
		if strings.EqualFold(effort, "none") {
			result, err = sjson.DeleteBytes(result, "reasoning_effort")
			if err != nil {
				return nil, false, err
			}
		}
		info.SetReasoningEffort(strings.TrimSpace(gjson.GetBytes(result, "reasoning_effort").String()))
		return result, true, nil
	}

	if _, matched := gpt6ChatFunctionToolModels[model]; !matched {
		return data, false, nil
	}
	if strings.EqualFold(effort, "none") || !hasOpenAIFunctionTool(data) {
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
