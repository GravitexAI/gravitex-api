package vertex

import (
	"encoding/json"

	"github.com/QuantumNous/new-api/relaykit/dto"
)

type VertexAIClaudeRequest struct {
	AnthropicVersion string              `json:"anthropic_version"`
	Messages         []dto.ClaudeMessage `json:"messages"`
	System           any                 `json:"system,omitempty"`
	MaxTokens        *uint               `json:"max_tokens,omitempty"`
	StopSequences    []string            `json:"stop_sequences,omitempty"`
	Stream           *bool               `json:"stream,omitempty"`
	Temperature      *float64            `json:"temperature,omitempty"`
	TopP             *float64            `json:"top_p,omitempty"`
	TopK             *int                `json:"top_k,omitempty"`
	Tools            any                 `json:"tools,omitempty"`
	ToolChoice       any                 `json:"tool_choice,omitempty"`
	Thinking         *dto.Thinking       `json:"thinking,omitempty"`
	OutputConfig     json.RawMessage     `json:"output_config,omitempty"`
	// 以下字段 Vertex 上的 Anthropic Messages 端均支持(structured outputs / metadata /
	// prompt caching),此前未透传会被静默丢弃。Container、McpServers 等 Vertex 不支持的
	// 字段仍不透传(传了上游也会 400)。
	OutputFormat json.RawMessage `json:"output_format,omitempty"`
	Metadata     json.RawMessage `json:"metadata,omitempty"`
	CacheControl json.RawMessage `json:"cache_control,omitempty"`
}

func copyRequest(req *dto.ClaudeRequest, version string) *VertexAIClaudeRequest {
	return &VertexAIClaudeRequest{
		AnthropicVersion: version,
		System:           req.System,
		Messages:         req.Messages,
		MaxTokens:        req.MaxTokens,
		Stream:           req.Stream,
		Temperature:      req.Temperature,
		TopP:             req.TopP,
		TopK:             req.TopK,
		StopSequences:    req.StopSequences,
		Tools:            req.Tools,
		ToolChoice:       req.ToolChoice,
		Thinking:         req.Thinking,
		OutputConfig:     req.OutputConfig,
		OutputFormat:     req.OutputFormat,
		Metadata:         req.Metadata,
		CacheControl:     req.CacheControl,
	}
}
