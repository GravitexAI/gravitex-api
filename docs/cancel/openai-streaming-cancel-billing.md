# OpenAI GPT 流式响应取消时的扣费机制说明

> 文档目的：解答用户关于"在使用 OpenAI 流式接口时，如果中途取消，会不会减少扣费？怎么减少？"的疑问。
> 信息来源：OpenAI 官方文档、OpenAI Developer Community、OpenAI 官方 GitHub Issue（截至 2026 年 5 月）。

---

## 一、一句话结论

**会减少扣费，但前提是"真正切断了到 OpenAI 的连接"。** 中途取消后，OpenAI 只对**已经实际生成的 token** 收费，未生成的部分不会收费；但 prompt（输入）部分始终按全额计费。

---

## 二、扣费规则详解

### 2.1 输入 token（prompt）

- **始终全额计费**。
- 无论你是否取消，只要请求已经成功发送给 OpenAI，输入部分的费用就已经产生。

### 2.2 输出 token（completion）

- 只对**已经生成出来的 token** 收费。
- 取消时刻"未来要生成"的那些 token 不会被收费。
- 实际可能会比"客户端真正接收到的最后一个 token"多 **2~3 个在途 token**（因为推理流水线已经算出但还在传输中），这些也会被计费，属于正常误差范围。

OpenAI 官方在开发者社区的回答原文：

> "You're charged only for tokens generated up to the point you cut the connection, plus maybe 2-3 tokens in flight."

---

## 三、关键前提：必须真正断开"上游"连接

很多用户以为前端点了"停止"按钮就一定能省钱，其实并不一定，要看请求链路上谁真正断开了到 OpenAI 的连接。

| 场景 | 是否真的减少扣费 |
|------|-----------------|
| 前端 abort，但中间网关/代理仍在持续读取 OpenAI 的流 | ❌ 不会减少。OpenAI 仍然在继续推理并产生 token，照样计费。|
| 网关/代理检测到客户端断开后，**主动关闭了到 OpenAI 的连接** | ✅ 会减少。OpenAI 检测到下游断开会终止推理。|
| 使用 `background=true` + `/cancel` 端点 | ⚠️ 目前不可靠。详见下文已知问题。|

**结论：** 想要"取消即省钱"，必须保证整条链路（客户端 → 你的服务 → OpenAI）都把连接关掉，任何一环没断，OpenAI 都会继续生成、继续计费。

---

## 四、最大的痛点：取消后无法获取准确的 usage

这是目前 OpenAI 流式接口被开发者抱怨最多的问题，已在官方仓库登记为公开 Issue。

### 4.1 SSE 事件的发送顺序

```
delta chunks  →  finish_reason  →  usage chunk  →  [DONE]
                                     ↑
                            只在最后一刻才发出
```

只有当一次响应**完整结束**时，OpenAI 才会发送包含 `prompt_tokens` / `completion_tokens` / `total_tokens` 的 usage chunk。

### 4.2 后果

即使在请求里设置了：

```json
{
  "stream_options": { "include_usage": true }
}
```

只要中途断开（用户关页面、点取消、网络超时……），客户端就**永远收不到 usage 数据**。

### 4.3 OpenAI 提出但尚未实现的方案

- **方案 A**：检测到客户端断开时，发送一个标记为 `incomplete` 的部分 usage chunk。
- **方案 B**：在每个 chunk 中都附带 `completion_tokens_so_far` 这样的实时计数器。

两个方案至今（2026 年 5 月）**都还没有正式落地**。

---

## 五、`background` + `/cancel` 端点的已知问题

OpenAI 提供了 Background Mode（`background=true`），允许通过 `/cancel` 端点取消任务。但目前社区和 GitHub Issue 反馈的现象是：

- 调用 `/cancel` 后，响应状态会显示 `cancelled`，但 SSE 事件仍然会继续到达，响应最终会"正常完成"。
- 任务结束后查询 usage 仍会显示有费用产生，**等同于没取消**。

因此**现阶段不能依赖 `/cancel` 端点来减少扣费**，截断 TCP 连接仍是更可靠的方式。

---

## 六、实践建议（用户视角）

如果你想在使用 OpenAI 流式接口时尽量减少"用户取消但仍被扣费"的损失，可以考虑：

1. **尽早取消**：用户停止阅读时立刻断开，不要等流自己结束。
2. **设置 `max_tokens`**：给输出长度设上限，把"最坏情况"的费用控制住。
3. **选用合适的小模型**：在能满足需求的前提下，优先选 nano / mini 这类便宜的模型，单位 token 成本低，被取消时损失也小。
4. **利用 Prompt Caching**：命中缓存的输入 token 大约只要原价的 **10%**，能显著降低"输入贵、输出短"场景下的取消损失。
5. **用 Batch API 处理异步任务**：异步批处理价格大约是实时调用的 **50%**，适合不需要即时返回的场景。
6. **用结构化输出（Structured Outputs）**：减少格式错误导致的重试，从源头降低被取消/重发的概率。

---

## 七、常见疑问 FAQ

**Q1：我点了"停止"按钮，是不是就一定不再扣费了？**
A：不一定。要看你用的客户端/网关是否真正切断了到 OpenAI 的连接。前端的 abort 只是断了"客户端到网关"的连接，如果网关没有把"网关到 OpenAI"的连接也关掉，OpenAI 仍会把整条响应跑完并按全量计费。

**Q2：取消后，我要怎么知道实际被扣了多少 token？**
A：目前**没有可靠的官方手段**。usage chunk 只在响应完整结束时才发，中途取消就拿不到。一般做法是：
- 由网关/客户端**累计已经收到的 delta 文本**；
- 用 `tiktoken` 这类官方分词库对累计内容做估算；
- 注意这是**估算值**，对包含特殊字符、emoji、多字节文本时会有偏差。

**Q3：输入很长的对话历史，取消会便宜吗？**
A：**不会**。输入 token 一旦发给 OpenAI 就全额计费，无论是否取消。要想省输入费用，请优先利用 **Prompt Caching**。

**Q4：用 `background=true` 然后调 `/cancel` 能不能省钱？**
A：理论上可以，但目前（2026 年 5 月）官方实现存在 Bug，取消后仍会照常完成并计费。建议关注后续更新，**短期内不要依赖这个机制做计费控制**。

**Q5：OpenAI 之外的模型（Claude、Gemini 等）也是这样吗？**
A：大体逻辑一致——**只对已生成的内容收费、输入照常收费**——但每家在"取消后是否能拿到准确 usage"上的实现细节有差异，具体以各厂商当前文档为准。

---

## 八、参考资料

- [OpenAI Developer Community: Api Billing for streaming if I close connection midway](https://community.openai.com/t/api-billing-for-streaming-if-i-close-connection-midway/624323)
- [OpenAI Developer Community: If we stop streaming output stream before it finishes, do we still get billed?](https://community.openai.com/t/if-we-stop-streaming-output-stream-before-it-finishes-do-we-still-get-billed-for-the-tokens-that-werent-ouputted/859904)
- [GitHub Issue: [Streaming] Token usage not returned when stream is aborted mid-generation (openai/openai-openapi #539)](https://github.com/openai/openai-openapi/issues/539)
- [GitHub Issue: Cancel for streaming Responses (openai/openai-python #2643)](https://github.com/openai/openai-python/issues/2643)
- [OpenAI 官方文档：Background mode](https://platform.openai.com/docs/guides/background)
- [OpenAI 官方文档：Streaming API responses](https://developers.openai.com/api/docs/guides/streaming-responses)

---

> 最后更新：2026-05-18
