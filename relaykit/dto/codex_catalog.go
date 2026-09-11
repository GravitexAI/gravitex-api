package dto

// Codex 客户端（CLI / Desktop / IDE 扩展）的模型目录（model catalog）格式。
//
// 与 OpenAI 经典的 /v1/models 是两套完全不同的信封：
//
//	OpenAI : {"object":"list","data":[{"id":"gpt-6-astra", ...}]}
//	Codex  : {"models":[{"slug":"gpt-6-astra","context_window":272000, ...}]}
//
// 顶层键、主键名都不一样，Codex 侧用 serde 反序列化，对不上就整个目录解析失败，
// 客户端拿不到上下文窗口，会话直接起不来。
//
// 字段以 openai/codex 仓库 codex-rs/models-manager/models.json 为准：下面这份结构体
// 是该文件里 9 个模型条目**共有字段的完整超集**。serde 默认忽略多余字段但对缺失字段
// 未必有 default，所以这里宁可多发不可少发，可空字段一律用指针且不加 omitempty
// （要发 null 而不是省略键）。
//
// 该 schema 属于 Codex 内部约定，会随客户端版本变化，升级客户端后需要复核。

// CodexCatalog 是 catalog 文件与 Codex 方言 /v1/models 响应的顶层信封。
type CodexCatalog struct {
	Models []CodexModel `json:"models"`
}

// CodexModel 是目录中的单个模型条目。
type CodexModel struct {
	Slug        string `json:"slug"`
	DisplayName string `json:"display_name"`
	Description string `json:"description"`

	// 上下文窗口。注意这是 **Codex 会话侧**的窗口，与模型在 OpenAI API 侧标称的
	// 窗口不是一个概念（例如 gpt-6-astra：API 标称 1,050,000，Codex 目录是
	// 272,000 / 872,000）。填大了客户端会塞超上游能接受的量并拿到 400，
	// 填小了则每轮都触发 auto-compact。
	ContextWindow         int  `json:"context_window"`
	MaxContextWindow      int  `json:"max_context_window"`
	AutoCompactTokenLimit *int `json:"auto_compact_token_limit"`

	// 推理档位。非推理模型发 null，Codex 就不会在 UI 上给它推理档位选择器。
	DefaultReasoningLevel             *string               `json:"default_reasoning_level"`
	SupportedReasoningLevels          []CodexReasoningLevel `json:"supported_reasoning_levels"`
	DefaultReasoningSummary           string                `json:"default_reasoning_summary"`
	SupportsReasoningSummaries        bool                  `json:"supports_reasoning_summaries"`
	SupportsReasoningSummaryParameter bool                  `json:"supports_reasoning_summary_parameter"`

	// Codex 只认 text / image 两种输入模态，file / audio 必须在构建时过滤掉。
	InputModalities             []string `json:"input_modalities"`
	SupportsImageDetailOriginal bool     `json:"supports_image_detail_original"`

	// 工具与 shell
	ApplyPatchToolType         string                `json:"apply_patch_tool_type"`
	WebSearchToolType          string                `json:"web_search_tool_type"`
	SupportsSearchTool         bool                  `json:"supports_search_tool"`
	ShellType                  string                `json:"shell_type"`
	ToolMode                   *string               `json:"tool_mode"`
	TruncationPolicy           CodexTruncationPolicy `json:"truncation_policy"`
	SupportsParallelToolCalls  bool                  `json:"supports_parallel_tool_calls"`
	ExperimentalSupportedTools []string              `json:"experimental_supported_tools"`

	// 输出冗长度
	SupportVerbosity bool   `json:"support_verbosity"`
	DefaultVerbosity string `json:"default_verbosity"`

	// 传输与提示词注入开关
	PreferWebsockets               bool `json:"prefer_websockets"`
	UseResponsesLite               bool `json:"use_responses_lite"`
	IncludeSkillsUsageInstructions bool `json:"include_skills_usage_instructions"`
	IncludeAppsUsageInstructions   bool `json:"include_apps_usage_instructions"`
	IncludePluginUsageInstructions bool `json:"include_plugin_usage_instructions"`

	// Node REPL / 自动 review
	NodeReplAutoReviewRequired bool    `json:"node_repl_auto_review_required"`
	NodeReplDisabled           bool    `json:"node_repl_disabled"`
	AutoReviewModelOverride    *string `json:"auto_review_model_override"`

	// 多智能体与专长（第三方模型一律为 null）
	MultiAgentVersion *string `json:"multi_agent_version"`
	ModelSpecialty    *string `json:"model_specialty"`

	// 服务档位（第三方网关没有 priority/flex 之分，留空）
	DefaultServiceTier   *string            `json:"default_service_tier"`
	ServiceTiers         []CodexServiceTier `json:"service_tiers"`
	AdditionalSpeedTiers []string           `json:"additional_speed_tiers"`

	// 展示与可用性
	// visibility：list = 在模型选择器里列出；hide = 可用但不展示。
	Visibility           string  `json:"visibility"`
	SupportedInApi       bool    `json:"supported_in_api"`
	Priority             int     `json:"priority"`
	MinimalClientVersion string  `json:"minimal_client_version"`
	AvailabilityNux      *string `json:"availability_nux"`
	Upgrade              *string `json:"upgrade"`

	// available_in_plans 是 Codex 按 ChatGPT 套餐做的展示门控。第三方 API Key
	// 没有套餐概念，发一个对不上的列表反而会让模型被过滤掉，所以整个键省略，
	// 让 Codex 走「不做套餐门控」的默认分支。model_messages 同理：它会覆盖
	// Codex 内置的 instructions 模板，我们没有能力提供等价内容，省略更安全。
	AvailableInPlans []string            `json:"available_in_plans,omitempty"`
	ModelMessages    *CodexModelMessages `json:"model_messages,omitempty"`

	// 官方用来给内置 prompt 做缓存分桶，第三方无意义但字段要在。
	CompHash string `json:"comp_hash"`
}

// CodexReasoningLevel 是 supported_reasoning_levels 的条目。
type CodexReasoningLevel struct {
	Effort      string `json:"effort"`
	Description string `json:"description"`
}

// CodexTruncationPolicy 控制 Codex 对单条工具输出的截断策略。
type CodexTruncationPolicy struct {
	Mode  string `json:"mode"`
	Limit int    `json:"limit"`
}

// CodexServiceTier 是 service_tiers 的条目。
type CodexServiceTier struct {
	Id          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// CodexModelMessages 承载模型专属的 instructions 模板。
type CodexModelMessages struct {
	InstructionsTemplate string `json:"instructions_template"`
}
