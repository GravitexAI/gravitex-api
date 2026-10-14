package common

import (
	"testing"

	"github.com/QuantumNous/new-api/relay/constant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSanitizeGPT6ChatToolReasoningEffort(t *testing.T) {
	tests := []struct {
		name              string
		request           string
		expected          string
		relayMode         int
		initialEffort     string
		expectedEffort    string
		expectedSanitized bool
	}{
		{
			name:              "removes function tools for gpt-6-astra while preserving supported reasoning",
			request:           `{"model":"gpt-6-astra","reasoning_effort":"high","tools":[{"type":"function","function":{"name":"read_file"}}],"tool_choice":"auto"}`,
			expected:          `{"model":"gpt-6-astra","reasoning_effort":"high"}`,
			relayMode:         constant.RelayModeChatCompletions,
			initialEffort:     "high",
			expectedEffort:    "high",
			expectedSanitized: true,
		},
		{
			name:              "disables reasoning for gpt-6-sol with function tools",
			request:           `{"model":"gpt-6-sol","reasoning_effort":"high","tools":[{"type":"function","function":{"name":"read_file"}}]}`,
			expected:          `{"model":"gpt-6-sol","reasoning_effort":"none","tools":[{"type":"function","function":{"name":"read_file"}}]}`,
			relayMode:         constant.RelayModeChatCompletions,
			initialEffort:     "high",
			expectedEffort:    "none",
			expectedSanitized: true,
		},
		{
			name:              "disables reasoning for gpt-6-luna with function tools",
			request:           `{"model":"gpt-6-luna","reasoning_effort":"high","tools":[{"type":"function","function":{"name":"read_file"}}]}`,
			expected:          `{"model":"gpt-6-luna","reasoning_effort":"none","tools":[{"type":"function","function":{"name":"read_file"}}]}`,
			relayMode:         constant.RelayModeChatCompletions,
			initialEffort:     "high",
			expectedEffort:    "none",
			expectedSanitized: true,
		},
		{
			name:              "removes function tools for gpt-6.1-sol while preserving supported reasoning",
			request:           `{"model":"gpt-6.1-sol","reasoning_effort":"high","tools":[{"type":"function","function":{"name":"read_file"}}],"tool_choice":"auto"}`,
			expected:          `{"model":"gpt-6.1-sol","reasoning_effort":"high"}`,
			relayMode:         constant.RelayModeChatCompletions,
			initialEffort:     "high",
			expectedEffort:    "high",
			expectedSanitized: true,
		},
		{
			name:              "removes function tools for gpt-6.1-sol without an explicit reasoning effort",
			request:           `{"model":"gpt-6.1-sol","tools":[{"type":"function","function":{"name":"read_file"}}]}`,
			expected:          `{"model":"gpt-6.1-sol"}`,
			relayMode:         constant.RelayModeChatCompletions,
			expectedEffort:    "",
			expectedSanitized: true,
		},
		{
			name:              "leaves GPT-6 reasoning untouched without function tools",
			request:           `{"model":"gpt-6-sol","reasoning_effort":"high","tools":[{"type":"web_search"}]}`,
			expected:          `{"model":"gpt-6-sol","reasoning_effort":"high","tools":[{"type":"web_search"}]}`,
			relayMode:         constant.RelayModeChatCompletions,
			initialEffort:     "high",
			expectedEffort:    "high",
			expectedSanitized: false,
		},
		{
			name:              "leaves other models untouched",
			request:           `{"model":"gpt-5.6-sol","reasoning_effort":"high","tools":[{"type":"function","function":{"name":"read_file"}}]}`,
			expected:          `{"model":"gpt-5.6-sol","reasoning_effort":"high","tools":[{"type":"function","function":{"name":"read_file"}}]}`,
			relayMode:         constant.RelayModeChatCompletions,
			initialEffort:     "high",
			expectedEffort:    "high",
			expectedSanitized: false,
		},
		{
			name:              "removes unsupported none and function tools for gpt-6.1-sol",
			request:           `{"model":"gpt-6.1-sol","reasoning_effort":"none","tools":[{"type":"function","function":{"name":"read_file"}}]}`,
			expected:          `{"model":"gpt-6.1-sol"}`,
			relayMode:         constant.RelayModeChatCompletions,
			initialEffort:     "none",
			expectedEffort:    "",
			expectedSanitized: true,
		},
		{
			name:              "leaves Responses requests untouched",
			request:           `{"model":"gpt-6.1-sol","reasoning_effort":"high","tools":[{"type":"function","function":{"name":"read_file"}}]}`,
			expected:          `{"model":"gpt-6.1-sol","reasoning_effort":"high","tools":[{"type":"function","function":{"name":"read_file"}}]}`,
			relayMode:         constant.RelayModeResponses,
			initialEffort:     "high",
			expectedEffort:    "high",
			expectedSanitized: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := &RelayInfo{
				RelayMode:       tt.relayMode,
				ReasoningEffort: tt.initialEffort,
			}

			actual, sanitized, err := SanitizeGPT6ChatToolReasoningEffort([]byte(tt.request), info)

			require.NoError(t, err)
			assert.JSONEq(t, tt.expected, string(actual))
			assert.Equal(t, tt.expectedSanitized, sanitized)
			assert.Equal(t, tt.expectedEffort, info.ReasoningEffort)
		})
	}
}
