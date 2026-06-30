package aws

import (
	"context"
	"encoding/json"
	"io"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/relay/channel/claude"
)

type AwsClaudeRequest struct {
	// AnthropicVersion should be "bedrock-2023-05-31"
	AnthropicVersion  string              `json:"anthropic_version"`
	AnthropicBeta     json.RawMessage     `json:"anthropic_beta,omitempty"`
	System            any                 `json:"system,omitempty"`
	Messages          []dto.ClaudeMessage `json:"messages"`
	MaxTokens         *uint               `json:"max_tokens,omitempty"`
	Temperature       *float64            `json:"temperature,omitempty"`
	TopP              *float64            `json:"top_p,omitempty"`
	TopK              *int                `json:"top_k,omitempty"`
	StopSequences     []string            `json:"stop_sequences,omitempty"`
	Tools             any                 `json:"tools,omitempty"`
	ToolChoice        any                 `json:"tool_choice,omitempty"`
	ContextManagement json.RawMessage     `json:"context_management,omitempty"`
	Thinking          *dto.Thinking       `json:"thinking,omitempty"`
	OutputConfig      json.RawMessage     `json:"output_config,omitempty"`
	//Metadata         json.RawMessage     `json:"metadata,omitempty"`
}

func formatRequest(requestBody io.Reader, requestHeader http.Header, requestID string) (*AwsClaudeRequest, error) {
	var awsClaudeRequest AwsClaudeRequest
	err := common.DecodeJson(requestBody, &awsClaudeRequest)
	if err != nil {
		return nil, err
	}
	awsClaudeRequest.AnthropicVersion = "bedrock-2023-05-31"

	// 把 anthropic-beta header 从客户端请求迁移到 Bedrock 的 body 数组，
	// 过程中按 Bedrock 白名单过滤 + 必要时重命名（详见 claude.FilterBetaFlags）。
	anthropicBetaValues := requestHeader.Get("anthropic-beta")
	filtered := claude.FilterBetaFlags(anthropicBetaValues, claude.TargetBedrock, requestID)
	if len(filtered) > 0 {
		betaJson, err := json.Marshal(filtered)
		if err != nil {
			return nil, err
		}
		awsClaudeRequest.AnthropicBeta = betaJson
	}

	// beta flag 与 body 顶级字段是绑定关系：beta 被过滤掉，对应 body 字段也必须清理，
	// 否则 Bedrock 会返回 "Extra inputs are not permitted"。
	awsClaudeRequest.stripBodyFieldsForDroppedBetas(filtered)

	logger.LogJson(context.Background(), "json", awsClaudeRequest)
	return &awsClaudeRequest, nil
}

// stripBodyFieldsForDroppedBetas 清理那些"对应 beta 未被保留"的顶级 body 字段。
// 后续发现新的 beta ↔ body 绑定关系时，在此追加映射即可。
func (r *AwsClaudeRequest) stripBodyFieldsForDroppedBetas(keptBetas []string) {
	kept := make(map[string]bool, len(keptBetas))
	for _, b := range keptBetas {
		kept[b] = true
	}
	// context_management 顶级字段依赖 context-management-2025-06-27 beta（仅 Converse 支持）
	if !kept["context-management-2025-06-27"] {
		r.ContextManagement = nil
	}
	// output_config: { effort } 依赖 effort-2025-11-24 beta
	if !kept["effort-2025-11-24"] {
		r.OutputConfig = nil
	}
}

// NovaMessage Nova模型使用messages-v1格式
type NovaMessage struct {
	Role    string        `json:"role"`
	Content []NovaContent `json:"content"`
}

type NovaContent struct {
	Text string `json:"text"`
}

type NovaRequest struct {
	SchemaVersion   string               `json:"schemaVersion"`             // 请求版本，例如 "1.0"
	Messages        []NovaMessage        `json:"messages"`                  // 对话消息列表
	InferenceConfig *NovaInferenceConfig `json:"inferenceConfig,omitempty"` // 推理配置，可选
}

type NovaInferenceConfig struct {
	MaxTokens     int      `json:"maxTokens,omitempty"`     // 最大生成的 token 数
	Temperature   float64  `json:"temperature,omitempty"`   // 随机性 (默认 0.7, 范围 0-1)
	TopP          float64  `json:"topP,omitempty"`          // nucleus sampling (默认 0.9, 范围 0-1)
	TopK          int      `json:"topK,omitempty"`          // 限制候选 token 数 (默认 50, 范围 0-128)
	StopSequences []string `json:"stopSequences,omitempty"` // 停止生成的序列
}

// 转换OpenAI请求为Nova格式
func convertToNovaRequest(req *dto.GeneralOpenAIRequest) *NovaRequest {
	novaMessages := make([]NovaMessage, len(req.Messages))
	for i, msg := range req.Messages {
		novaMessages[i] = NovaMessage{
			Role:    msg.Role,
			Content: []NovaContent{{Text: msg.StringContent()}},
		}
	}

	novaReq := &NovaRequest{
		SchemaVersion: "messages-v1",
		Messages:      novaMessages,
	}

	// 设置推理配置
	if (req.MaxTokens != nil && *req.MaxTokens != 0) || (req.Temperature != nil && *req.Temperature != 0) || (req.TopP != nil && *req.TopP != 0) || (req.TopK != nil && *req.TopK != 0) || req.Stop != nil {
		novaReq.InferenceConfig = &NovaInferenceConfig{}
		if req.MaxTokens != nil && *req.MaxTokens != 0 {
			novaReq.InferenceConfig.MaxTokens = int(*req.MaxTokens)
		}
		if req.Temperature != nil && *req.Temperature != 0 {
			novaReq.InferenceConfig.Temperature = *req.Temperature
		}
		if req.TopP != nil && *req.TopP != 0 {
			novaReq.InferenceConfig.TopP = *req.TopP
		}
		if req.TopK != nil && *req.TopK != 0 {
			novaReq.InferenceConfig.TopK = *req.TopK
		}
		if req.Stop != nil {
			if stopSequences := parseStopSequences(req.Stop); len(stopSequences) > 0 {
				novaReq.InferenceConfig.StopSequences = stopSequences
			}
		}
	}

	return novaReq
}

// parseStopSequences 解析停止序列，支持字符串或字符串数组
func parseStopSequences(stop any) []string {
	if stop == nil {
		return nil
	}

	switch v := stop.(type) {
	case string:
		if v != "" {
			return []string{v}
		}
	case []string:
		return v
	case []interface{}:
		var sequences []string
		for _, item := range v {
			if str, ok := item.(string); ok && str != "" {
				sequences = append(sequences, str)
			}
		}
		return sequences
	}
	return nil
}
