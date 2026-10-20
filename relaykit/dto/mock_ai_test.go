package dto

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// validMockAIConfig returns a fully valid config used as the base for each case.
func validMockAIConfig() MockAIConfig {
	profile := MockAIModelProfile{
		TextInput:            MockAIRange{Min: 100, Max: 2000},
		TextOutput:           MockAIRange{Min: 50, Max: 800},
		CacheRead:            MockAIRange{Min: 0, Max: 500},
		CacheCreation:        MockAIRange{Min: 0, Max: 300},
		CacheCreation1hRatio: 0.5,
		Reasoning:            &MockAIRange{Min: 10, Max: 100},
		LatencyMs:            MockAIRange{Min: 800, Max: 3000},
		FirstTokenMs:         MockAIRange{Min: 100, Max: 700},
	}
	return MockAIConfig{
		Endpoints: MockAIEndpoints{ChatCompletions: true, Responses: true, Messages: true},
		Default:   profile,
		Models:    map[string]MockAIModelProfile{"gpt-x": profile},
	}
}

func TestMockAIConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(c *MockAIConfig)
		wantErr bool
	}{
		{"valid full config", func(c *MockAIConfig) {}, false},
		{"text input min > max", func(c *MockAIConfig) { c.Default.TextInput = MockAIRange{Min: 10, Max: 5} }, true},
		{"text input min negative", func(c *MockAIConfig) { c.Default.TextInput = MockAIRange{Min: -1, Max: 5} }, true},
		{"cache creation 1h ratio negative", func(c *MockAIConfig) { c.Default.CacheCreation1hRatio = -0.1 }, true},
		{"cache creation 1h ratio above one", func(c *MockAIConfig) { c.Default.CacheCreation1hRatio = 1.1 }, true},
		{"latency min equals first token max", func(c *MockAIConfig) {
			c.Default.LatencyMs = MockAIRange{Min: 700, Max: 3000}
		}, true},
		{"latency min above first token max", func(c *MockAIConfig) {
			c.Default.LatencyMs = MockAIRange{Min: 701, Max: 3000}
		}, false},
		{"no endpoint enabled", func(c *MockAIConfig) { c.Endpoints = MockAIEndpoints{} }, true},
		{"text input max above token limit", func(c *MockAIConfig) {
			c.Default.TextInput = MockAIRange{Min: 0, Max: MockAIMaxTokens + 1}
		}, true},
		{"latency max above 600000", func(c *MockAIConfig) {
			c.Default.LatencyMs = MockAIRange{Min: 800, Max: 600001}
		}, true},
		{"per-model profile invalid", func(c *MockAIConfig) {
			p := c.Models["gpt-x"]
			p.TextOutput = MockAIRange{Min: 9, Max: 1}
			c.Models["gpt-x"] = p
		}, true},
		{"reasoning range invalid", func(c *MockAIConfig) { c.Default.Reasoning = &MockAIRange{Min: 5, Max: 1} }, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validMockAIConfig()
			tt.mutate(&cfg)
			err := cfg.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
