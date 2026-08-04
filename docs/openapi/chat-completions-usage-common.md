# `/v1/chat/completions` 返回字段说明（对话模型通用）

> 适用范围：调用普通对话模型（GPT 系列、Claude 对话/思考模型、Gemini、DeepSeek 等经由 `/v1/chat/completions` 走文本对话场景）时会拿到的 `usage` 字段。
> 不包含：仅在底层实际调用的是 Claude Messages 协议或图片生成模型时才出现的字段——那些放在《`chat-completions-usage-special.md`》里。

## 样例

```json
{
  "id": "chatcmpl-abc123",
  "object": "chat.completion",
  "created": 1752345600,
  "model": "gpt-5.6",
  "choices": [
    {
      "index": 0,
      "message": {
        "role": "assistant",
        "content": "这是模型的回复内容"
      },
      "finish_reason": "stop"
    }
  ],
  "usage": {
    "prompt_tokens": 120,
    "completion_tokens": 85,
    "total_tokens": 205,

    "prompt_tokens_details": {
      "cached_tokens": 30,
      "cache_write_tokens": 5,
      "text_tokens": 0,
      "audio_tokens": 0,
      "image_tokens": 0
    },
    "completion_tokens_details": {
      "text_tokens": 0,
      "audio_tokens": 0,
      "image_tokens": 0,
      "reasoning_tokens": 42,
      "accepted_prediction_tokens": 0,
      "rejected_prediction_tokens": 0
    },

    "input_tokens": 120,
    "output_tokens": 85,
    "input_tokens_details": null
  }
}
```

## 字段说明

| 字段 | 什么时候会有值 | 说明 |
|---|---|---|
| `prompt_tokens` | 始终 | 输入 token 总数 |
| `completion_tokens` | 始终 | 输出 token 总数 |
| `total_tokens` | 始终 | `prompt_tokens + completion_tokens` |
| `prompt_tokens_details.cached_tokens` | 模型/渠道支持 prompt caching 时 | 命中缓存、按缓存价计费的输入 token 数（OpenAI 原生缓存、Gemini 隐式缓存、Claude 缓存读取都会体现在这个字段） |
| `prompt_tokens_details.cache_write_tokens` | 仅 GPT-5.6 及以上、开启显式 prompt cache 时 | 本次请求新写入缓存、按缓存写入价计费的 token 数 |
| `prompt_tokens_details.audio_tokens` | 仅使用音频输入模型（如 `gpt-4o-audio-preview`）时 | 输入中音频部分消耗的 token 数 |
| `prompt_tokens_details.text_tokens` / `image_tokens` | 多数纯文本对话模型下为 `0` | 输入中文本 / 图片部分的 token 拆分，纯文本对话场景通常不会被填充，只是字段本身始终存在 |
| `completion_tokens_details.reasoning_tokens` | 仅推理模型（GPT o1/o3/GPT-5 系列、Claude 开启扩展思考、Gemini thinking）时 | 模型内部思考消耗的 token 数，不会出现在最终回复文本里，但按输出价计费 |
| `completion_tokens_details.audio_tokens` | 仅使用音频输出模型时 | 输出中音频部分消耗的 token 数 |
| `completion_tokens_details.accepted_prediction_tokens` / `rejected_prediction_tokens` | 仅使用 OpenAI Predicted Outputs 功能时 | 命中 / 未命中预测内容的 token 数；未命中的部分仍按输出价计费 |
| `completion_tokens_details.text_tokens` / `image_tokens` | 纯文本对话场景下为 `0` | 见下方"关于 image_tokens"说明 |
| `input_tokens` / `output_tokens` | 始终 | 数值上等同于 `prompt_tokens` / `completion_tokens`，是为兼容部分上游协议保留的别名字段 |
| `input_tokens_details` | 目前对话场景下基本为 `null` | 保留字段，普通对话请求不会填充 |

## 说明

- `prompt_tokens_details` / `completion_tokens_details` 这两个对象在响应里**始终存在**，即使内部子字段全是 `0` 也不会被省略；`0` 不代表"不支持"，只代表"这次请求没用到"。
- `completion_tokens_details.image_tokens`（以及 `prompt_tokens_details.image_tokens`）留在通用文档里是因为字段结构固定存在，但**真正被赋非零值只发生在特殊场景**（Claude/图片生成模型），详见《`chat-completions-usage-special.md`》。
- `reasoning_tokens` 是跨模型通用概念：无论底层是 OpenAI 推理模型、Claude 扩展思考还是 Gemini thinking，只要该次调用启用了"思考"能力，都会体现在这一个字段里。
