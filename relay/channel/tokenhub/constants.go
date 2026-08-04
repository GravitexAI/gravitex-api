package tokenhub

// ModelList lists the language models exposed by Tencent Cloud TokenHub.
// All entries support both OpenAI Chat Completions and Anthropic Messages APIs.
// Reference: docs/腾讯云tokenhub/语言模型调用概览.md
var ModelList = []string{
	// DeepSeek
	"deepseek-v4-flash-202605",
	"deepseek-v4-pro-202606",
	"deepseek-v4-flash",
	"deepseek-v4-pro",
	"deepseek-v3.2",
	// GLM
	"glm-5.1",
	"glm-5v-turbo",
	"glm-5-turbo",
	"glm-5",
	// Kimi
	"kimi-k2.6",
	"kimi-k2.5",
	// MiniMax
	"minimax-m3",
	"minimax-m2.7",
	"minimax-m2.5",
	// Tencent Hunyuan
	"hy-mt2-plus",
}

var ChannelName = "tencent_tokenhub"
