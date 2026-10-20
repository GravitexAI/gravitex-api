package dto

import "fmt"

const (
	// MockAIMaxTokens is the upper bound for every token range of a Mock AI
	// profile. It mirrors maxTokensLimit (math.MaxInt32 / 2) in
	// relay/helper/valid_request.go. relaykit must not import the root module,
	// so the value is duplicated here; keep the two in sync.
	MockAIMaxTokens = 1073741823

	// MockAIMaxLatencyMs caps simulated latency so a misconfigured channel
	// cannot hold a connection open for an unreasonable time.
	MockAIMaxLatencyMs = 600000
)

// MockAIConfig configures a Mock AI channel (stored in ChannelOtherSettings.MockAI).
// Model profiles are looked up by the original (pre-mapping) model name and fall
// back to Default.
type MockAIConfig struct {
	Endpoints MockAIEndpoints               `json:"endpoints"`
	Default   MockAIModelProfile            `json:"default"`
	Models    map[string]MockAIModelProfile `json:"models,omitempty"`
}

// MockAIEndpoints selects which client-facing endpoints the channel serves.
type MockAIEndpoints struct {
	ChatCompletions bool `json:"chat_completions"`
	Responses       bool `json:"responses"`
	Messages        bool `json:"messages"`
}

// MockAIRange is an inclusive [Min, Max] range sampled per request.
type MockAIRange struct {
	Min int `json:"min"`
	Max int `json:"max"`
}

// MockAIModelProfile describes the synthetic usage and timing of one model.
type MockAIModelProfile struct {
	TextInput            MockAIRange  `json:"text_input"`
	TextOutput           MockAIRange  `json:"text_output"`
	CacheRead            MockAIRange  `json:"cache_read"`
	CacheCreation        MockAIRange  `json:"cache_creation"`          // only effective on /v1/messages
	CacheCreation1hRatio float64      `json:"cache_creation_1h_ratio"` // 0~1, default 0 means all counted as 5m
	Reasoning            *MockAIRange `json:"reasoning,omitempty"`     // nil = disabled
	LatencyMs            MockAIRange  `json:"latency_ms"`              // end-to-end total time
	FirstTokenMs         MockAIRange  `json:"first_token_ms"`          // streaming first token
}

// Validate checks the whole config; it is called when a channel is saved.
func (c *MockAIConfig) Validate() error {
	if c == nil {
		return fmt.Errorf("mock_ai config is required")
	}
	if !c.Endpoints.ChatCompletions && !c.Endpoints.Responses && !c.Endpoints.Messages {
		return fmt.Errorf("mock_ai: at least one endpoint must be enabled")
	}
	if err := c.Default.validate(); err != nil {
		return fmt.Errorf("mock_ai default profile: %w", err)
	}
	for name, profile := range c.Models {
		if err := profile.validate(); err != nil {
			return fmt.Errorf("mock_ai profile for model %q: %w", name, err)
		}
	}
	return nil
}

func (r MockAIRange) validate(name string, limit int) error {
	if r.Min < 0 {
		return fmt.Errorf("%s: min must be >= 0", name)
	}
	if r.Min > r.Max {
		return fmt.Errorf("%s: min must not exceed max", name)
	}
	if r.Max > limit {
		return fmt.Errorf("%s: max must not exceed %d", name, limit)
	}
	return nil
}

func (p MockAIModelProfile) validate() error {
	tokenRanges := []struct {
		name string
		r    MockAIRange
	}{
		{"text_input", p.TextInput},
		{"text_output", p.TextOutput},
		{"cache_read", p.CacheRead},
		{"cache_creation", p.CacheCreation},
	}
	for _, tr := range tokenRanges {
		if err := tr.r.validate(tr.name, MockAIMaxTokens); err != nil {
			return err
		}
	}
	if p.Reasoning != nil {
		if err := p.Reasoning.validate("reasoning", MockAIMaxTokens); err != nil {
			return err
		}
	}
	if err := p.LatencyMs.validate("latency_ms", MockAIMaxLatencyMs); err != nil {
		return err
	}
	if err := p.FirstTokenMs.validate("first_token_ms", MockAIMaxLatencyMs); err != nil {
		return err
	}
	if p.CacheCreation1hRatio < 0 || p.CacheCreation1hRatio > 1 {
		return fmt.Errorf("cache_creation_1h_ratio must be within [0, 1]")
	}
	// Strictly greater, lower bound vs upper bound: any random draw then
	// satisfies total latency > first-token time.
	if p.LatencyMs.Min <= p.FirstTokenMs.Max {
		return fmt.Errorf("latency_ms.min must be greater than first_token_ms.max")
	}
	return nil
}
