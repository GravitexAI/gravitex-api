package deepseek

import "github.com/QuantumNous/new-api/constant"

var ModelList = []string{
	"deepseek-chat", "deepseek-reasoner",
	"deepseek-r1-250528", "deepseek-r1-0528",
	"deepseek-v3-250324", "deepseek-v3-0324",
	"deepseek-v3-1-250821", "deepseek-v3.1-terminus",
	"deepseek-v3-2-251201", "deepseek-v3.2", "deepseek-v3.2-exp",
	"deepseek-ocr",
	"deepseek-v4-flash", "deepseek-v4-flash-none", "deepseek-v4-flash-max",
	"deepseek-v4-pro", "deepseek-v4-pro-none", "deepseek-v4-pro-max",
	"deepseek-flash", "deepseek-flash-none", "deepseek-flash-low", "deepseek-flash-max",
	"deepseek-v4-pro-low",
	constant.DeepSeekSearchModel, constant.DeepSeekFetchModel,
}

var ChannelName = "deepseek"

// Tool-price keys of the DeepSeek Search and Fetch APIs. They price the
// server-side searches and page fetches DeepSeek runs on its Anthropic and
// Responses endpoints.
const (
	SearchToolPriceKey = "deepseek_search"
	FetchToolPriceKey  = "deepseek_fetch"
)
