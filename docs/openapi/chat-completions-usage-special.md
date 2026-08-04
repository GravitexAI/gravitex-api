# `/v1/chat/completions` 返回字段说明（特殊场景专属字段）

> 承接《`chat-completions-usage-common.md`》。以下字段只在特定情况下才会出现非零值：
> 请求走的是 `/v1/chat/completions`，但网关内部实际把请求转换成了别的协议去调用上游（Claude Messages 协议 / Gemini 原生生图接口），转换回来的 usage 里就会带上这些"原生协议专属"的字段。
>
> 注意：`RelayMode` 只由请求的 URL 路径决定（见 `relay/constant/relay_mode.go` 的 `Path2RelayMode`），命中 `/v1/chat/completions` 就固定走 chat 处理逻辑，网关不会因为模型是"生图模型"就把请求悄悄转发到 `/v1/images/generations`。所以 OpenAI 的 `gpt-image` 系列**不能**通过 `/v1/chat/completions` 调用；下面场景二说的"生图模型"专指 Gemini 原生多模态输出模型（如 `gemini-2.5-flash-image`，俗称 nano-banana）。

## 场景一：底层实际调用 Claude（`/v1/chat/completions` → `/v1/messages`）

用 OpenAI 格式请求一个 Claude 模型时，网关会把请求转成 Anthropic Messages 协议发给上游，再把 Claude 返回的 usage 转换回 OpenAI 格式；Claude 的缓存机制比 OpenAI 更细（按 5 分钟 / 1 小时两档 TTL 计费），这部分信息用下面几个专属字段承载：

```json
"usage": {
  "prompt_tokens": 120,
  "completion_tokens": 85,
  "total_tokens": 205,
  "prompt_tokens_details": {
    "cached_tokens": 30,
    "cached_creation_tokens": 10
  },
  "completion_tokens_details": {
    "reasoning_tokens": 42
  },
  "claude_cache_creation_5_m_tokens": 8,
  "claude_cache_creation_1_h_tokens": 2
}
```

| 字段 | 说明 |
|---|---|
| `prompt_tokens_details.cached_creation_tokens` | 对应 Claude 的 `cache_creation_input_tokens`：本次请求为写入 prompt cache 而额外消耗的输入 token 数（这个总数会被下面两个字段按 TTL 拆分） |
| `claude_cache_creation_5_m_tokens` | 上面 `cached_creation_tokens` 中，按 **5 分钟** TTL 档写入缓存的 token 数 |
| `claude_cache_creation_1_h_tokens` | 上面 `cached_creation_tokens` 中，按 **1 小时** TTL 档写入缓存的 token 数（该档位单价更高） |

`prompt_tokens_details.cached_tokens`、`completion_tokens_details.reasoning_tokens` 在调用 Claude 模型时也会有值（分别对应 Claude 的缓存读取 token 数、扩展思考 token 数），但这两个字段是跨模型通用字段，已经在通用文档里说明过，这里不重复。

## 场景二：`/v1/chat/completions` 直接调用 Gemini 原生生图模型

Gemini 的生图能力（`gemini-2.5-flash-image` 等）本身就是 `generateContent` 接口的一种输出形态，不需要单独的图片接口，所以用户可以直接用 `/v1/chat/completions` + 该模型名对话式生成图片。网关会把 Gemini 返回的图片以 base64 内联数据的形式塞进 `choices[].message.content`（多模态数组，`{type: "image_url", image_url: {url: "data:image/...;base64,..."}}`），usage 则按下面的规则拆分：

```json
"usage": {
  "prompt_tokens": 50,
  "completion_tokens": 1300,
  "total_tokens": 1350,
  "prompt_tokens_details": {
    "text_tokens": 50,
    "image_tokens": 0
  },
  "completion_tokens_details": {
    "text_tokens": 10,
    "image_tokens": 1290,
    "reasoning_tokens": 0
  }
}
```

| 字段 | 说明 |
|---|---|
| `completion_tokens_details.image_tokens` | 本次输出中图片部分消耗的 token 数，按图片计费比例（`ImageCompletionRatio`）计费 |
| `completion_tokens_details.text_tokens` | 输出中文字部分（如图片描述、修订后的 prompt）消耗的 token 数 |
| `prompt_tokens_details.image_tokens` | 输入侧带图片时（图生图/图片编辑）消耗的图片 token 数 |

需要特别注意的两点，跟"纯文字对话模型"或"官方 `/v1/images/generations` 接口"都不一样：

1. **`text_tokens` 和 `image_tokens` 可能同时非零**：Gemini 生图模型常常在返回图片的同时附带一段文字（描述/修订 prompt），这段文字的 token 会计入 `completion_tokens_details.text_tokens`，图片部分计入 `image_tokens`，两者互不覆盖。这和 OpenAI 官方图片接口"输出只有图片"的假设不同。
2. **不会有 `generated_images` 字段**：`generated_images` 只在官方 `/v1/images/generations` 转 Gemini 的那条链路里才会设置；走 `/v1/chat/completions` 这条链路时，网关始终按 token 计费，不产出 `generated_images`。
3. **上游未返回 modality 拆分时的兜底策略**：如果 Gemini 响应里没有按模态拆分 token（`candidatesTokensDetails` 缺失），网关会根据这次回复里"是否真的产出过图片"来兜底判断——产出过图片就把全部输出 token 计入 `image_tokens`，否则计入 `text_tokens`；如果连总输出 token 数都没有，还会用"每张图片按 1400 token 估算"的方式兜底填 `completion_tokens`。这些都是估算值，不代表上游账单口径。

## 场景三：渠道协议差异导致的字段

这两个字段不是"转换"出来的，而是特定渠道原样透传上游响应字段，正常使用主流模型基本不会遇到：

| 字段 | 出现条件 | 说明 |
|---|---|---|
| `prompt_cache_hit_tokens` | 渠道是 DeepSeek，且上游按 DeepSeek 自己的字段名返回缓存信息 | DeepSeek 官方 API 用 `prompt_cache_hit_tokens` 而不是 `cached_tokens` 表达缓存命中；网关会把它同步映射进通用的 `prompt_tokens_details.cached_tokens`，但原始字段也会保留在顶层 |
| `cost` | 渠道是 OpenRouter，且上游在响应里带了美元成本 | OpenRouter 专属字段，普通渠道（官方 OpenAI/Claude/Gemini 等）不会出现 |
