# Vertex AI 生图（Gemini / Imagen）安全拦截与计费 — 官方文档汇总

> 整理日期：2026-09-21
> 全部结论均来自 Google Cloud / Google AI 官方文档原文，文末附出处链接。
> ⚠️ 注意：Vertex AI 生成式 AI 文档已迁移到 **Gemini Enterprise Agent Platform** 域名，`cloud.google.com/vertex-ai/generative-ai/docs/*` 会 301 到 `docs.cloud.google.com/gemini-enterprise-agent-platform/*`，内容同源。

---

## 〇、先纠正一个常见误解

主人问的前提是「**输入被拦截不收费、输出图片被拦截收费**」。
按官方文档，**前半句对，后半句不对**：

| 场景 | 官方说法 | 是否收费 |
|---|---|---|
| 输入（prompt / 输入图）被拦截 | Imagen 返回 **HTTP 400**；Gemini 返回 **HTTP 200 + promptFeedback** | Imagen **不收费**；Gemini **收输入 token 费** |
| 输出图片被拦截（image block） | 官方原文：**"You will not be charged for blocked images."** | **图片本身不收费**，但输入 token / thinking token 仍计入 |

也就是说：**被拦截的图片，Google 明确说不收钱**；真正会产生费用的是「请求返回 200 时的 token 计数」那部分。下面逐条给出原文依据。

---

## 一、计费总则（最权威的一句话）

Vertex AI / Agent Platform 生成式 AI 定价页开头第一段：

> **"You're charged only for requests that return a 200 response code. Requests returning any other response codes, such as 4xx and 5xx codes, aren't charged for the input or output."**
> （只有返回 200 的请求才计费；返回 4xx / 5xx 的请求，输入和输出都不计费。）

同页另外两条相关原文：

> "If your request fails with a 400 or 500 error, you won't be charged for the tokens used."

> "Prediction requests that lead to filtered responses are charged for the input only."
> （被过滤掉的响应，**只收输入费用**。——这条写在字符计费的老模型段落，但体现了 Google 一贯的过滤计费口径。）

📌 **推论规则（记住这个就够了）**：
`HTTP 状态码 != 200` → 完全不收费；
`HTTP 200` → 按响应里 `usageMetadata` 实际报出来的 token 数收费，**没报出来的就不收**。

出处：<https://cloud.google.com/gemini-enterprise-agent-platform/generative-ai/pricing>

---

## 二、必须先区分两条产品线

Vertex 上「用 Gemini 生图」其实有两套完全不同的 API 和计费模型，拦截行为也不一样，**千万别混着看**：

| | **Imagen 系列** | **Gemini 原生生图（Nano Banana 系列）** |
|---|---|---|
| 模型 ID | `imagen-4.0-*` / `imagegeneration@*` | `gemini-3-pro-image`、`gemini-3.1-flash-image`、`gemini-2.5-flash-image` |
| 端点 | `:predict`（predictions 数组） | `:generateContent`（candidates 数组） |
| 计费单位 | **按张**（Price / 1 count） | **按 token**（图片折算成 output token） |
| 输入被拦截 | **HTTP 400** | HTTP 200 + `promptFeedback.blockReason` |
| 输出被拦截 | HTTP 200，图片从数组里剔除 + `raiFilteredReason` | HTTP 200 + `finishReason = IMAGE_SAFETY` |
| 安全参数 | `safetySetting` / `personGeneration` / `includeRaiReason` | `safetySettings`（HarmCategory + 阈值） |

---

## 三、Imagen 线：拦截行为与计费

### 3.1 输入被拦截 → HTTP 400 → **不收费**

官方原文：

> **"If your text input or input image (for editing) is filtered, you get a response with a `400` error code."**

响应体：

```json
{
  "error": {
    "code": 400,
    "message": "Image generation failed with the following error: The prompt could not be submitted. This prompt contains sensitive words that violate Google's Responsible AI practices. Try rephrasing the prompt. If you think this was an error, send feedback.",
    "status": "INVALID_ARGUMENT",
    "details": [ { "@type": "type.googleapis.com/google.rpc.DebugInfo", "detail": "[ORIGINAL ERROR] generic::invalid_argument: ..." } ]
  }
}
```

👉 400 → 按总则「4xx 不计费」→ **完全不收费**。✅

官方还给了一个人话判断法：

> "If the model responds to a request with an error message such as 'The prompt couldn't be submitted' or 'it might violate our policies', then the input is triggering a safety filter. **If fewer images than requested are returned, then some generated output are blocked** for not meeting safety requirements."

### 3.2 输出图片被拦截 → HTTP 200，但**被拦的图不收费**

这是「You will not be charged for blocked images」这句官方原话的出处。

**情况 A：`includeRaiReason` 不设 / 设为 false**

> "your response only includes generated image objects that aren't filtered. **Any filtered image objects are omitted from the `"predictions": []` array.**"

即：请求 `sampleCount: 4`，2 张被过滤 → predictions 里只剩 2 个对象 → **按 2 张计费**。

**情况 B：`includeRaiReason: true`**

```json
{
  "predictions": [
    { "bytesBase64Encoded": "/9j/4AAQSkZJRgABA[...]", "mimeType": "image/png" },
    { "bytesBase64Encoded": "/9j/4AAQSkZJRgABA[...]", "mimeType": "image/png" },
    {
      "raiFilteredReason": "Your current safety filter threshold filtered out 2 generated images. You will not be charged for blocked images. Try rephrasing the prompt. If you think this was an error, send feedback."
    }
  ],
  "deployedModelId": "MODEL_ID"
}
```

🔑 **关键原文**：
> **"Your current safety filter threshold filtered out 2 generated images. `You will not be charged for blocked images.` Try rephrasing the prompt."**

**情况 C：`includeSafetyAttributes: true`**

> "The image safety attributes are also added to each unfiltered output. **If an output image is filtered its safety attributes aren't returned.**"

**情况 D：全部图片都被过滤**
predictions 里没有任何图片对象，只有 `raiFilteredReason` → 一张都不收费。

### 3.3 Imagen 价格（Vertex 官方定价页）

| 模型 | 功能 | 输入 | 输出 | 价格 / 1 count |
|---|---|---|---|---|
| Imagen 4 Ultra | Image generation | Text prompt | Image | **$0.06** |
| Imagen 4 | Upscaling（2K/3K/4K） | Image | Image | **$0.06** |

> 计价单位是 "Price / 1 count"，count = **实际返回的图片张数**。

出处：
- <https://cloud.google.com/vertex-ai/generative-ai/docs/image/responsible-ai-imagen>（Responsible AI and usage guidelines for Imagen）
- <https://cloud.google.com/gemini-enterprise-agent-platform/generative-ai/pricing>

---

## 四、Gemini 原生生图线（Nano Banana）：拦截行为与计费

### 4.1 三种失败形态（官方明确区分）

官方文档 *Gemini image generation and responsible AI* 原文：

> "When provided with an unsafe prompt, the model might refuse to generate an image, or the prompt or generated response might be blocked by our safety filters.
> - **Model refusal**: If a prompt is potentially unsafe, the model might refuse to process the request. If this happens, the model usually gives a text response saying it can't generate unsafe images. The `FinishReason` is **`STOP`**.
> - **Safety filter blocking**:
>   - If the **prompt** is identified as potentially harmful by a safety filter, the API returns **`BlockedReason` in `PromptFeedback`**.
>   - If the **response** is identified as potentially harmful by a safety filter, the API response includes a **`FinishReason` of `IMAGE_SAFETY`, `IMAGE_PROHIBITED_CONTENT`, or similar**."

### 4.2 响应结构（*Process blocked responses* 官方页）

**A. 输入 prompt 被拦截**

> - "The `promptFeedback` field is populated. This field contains information about why the prompt is blocked."
> - "The `candidates` field **isn't set**."

实际报文（官方示例）：

```json
{"promptFeedback":{"blockReason":"PROHIBITED_CONTENT"},"usageMetadata":{"promptTokenCount":7,"totalTokenCount":7}}
```

```json
{"promptFeedback":{"blockReason":"BLOCKED_REASON_UNSPECIFIED"},"usageMetadata":{"promptTokenCount":7,"totalTokenCount":7}}
```

对照：未被拦截时 `{"promptFeedback":{}, "usageMetadata":{"promptTokenCount":7,...}}`

💰 **计费判断**：HTTP **200**，`usageMetadata.promptTokenCount` 有值、`candidatesTokenCount` 无 →
按总则 **收输入 token 费，不收输出费**。
→ 对应官方那句 "Prediction requests that lead to filtered responses are charged for the input only."

⚠️ 所以在 **Gemini 线上，「输入被拦截完全不收费」是不成立的**，只是输入 token 通常极少（示例里只有 7 个 token），金额可忽略；但如果输入带了参考图（每张 560～1120 token）就不一定可忽略了。

**B. 模型输出被拦截**

> - "The `promptFeedback` field **isn't set**."
> - "The `candidates` field **is set** with the following characteristics:
>   - The `content` field **isn't set**.
>   - The `finishReason` field is set and indicates why response generation stopped."

典型 Nano Banana Pro 被拦响应字段：

| 字段 | 值 | 含义 |
|---|---|---|
| `content.parts` | `null` | 图片没返回 |
| `finishReason` | `IMAGE_SAFETY` | 命中**输出侧**安全过滤 |
| `finishMessage` | "Unable to show… **You will not be charged for blocked images.**" | Google 在报文里直接声明不收图片费 |
| `promptTokenCount` | 276 | 输入 token（计费） |
| `candidatesTokenCount` | **0** | **输出 token 为 0 → 图片不计费** |
| `thoughtsTokenCount` | 173 | 思考 token（按 output 文本价计费） |

💰 **计费判断**：
- 图片 output token = 0 → **图片不收费** ✅（与报文里的 finishMessage 一致）
- 输入 token **照收**
- **thinking token 照收**（官方定价页明确 "Output price (**including thinking tokens**)"）

> 📌 这就是很多人「感觉输出被拦还是被扣钱了」的真实原因 —— 扣的不是图片钱，是 **input token + thinking token**。Gemini 3 Pro Image 的 thinking token 按文本 output 价计费，多轮重试会累积。

**C. 文本拒答（不是拦截）**
`finishReason = STOP` + 只有文本 parts → 这是模型**主动拒绝**，产生了真实文本输出，**输入 + 文本输出 token 全额计费**。

**D. `NO_IMAGE`**
模型接受了请求但没产出图 → 不是策略拦截，属于生成问题，同样按实际 token 计费。

### 4.3 枚举值速查

**`BlockReason`（prompt 侧，出现在 `promptFeedback.blockReason`）**

| 枚举 | 官方描述 |
|---|---|
| `BLOCK_REASON_UNSPECIFIED` | "Default value. This value is unused." |
| `SAFETY` | "Prompt was blocked due to safety reasons. Inspect `safetyRatings` to understand which safety category blocked it." |
| `BLOCKLIST` | "Prompt was blocked due to the terms which are included from the terminology blocklist." |
| `PROHIBITED_CONTENT` | "Prompt was blocked due to prohibited content."（Vertex 侧注明：**usually CSAM**，不可配置） |
| `IMAGE_SAFETY` | "Candidates blocked due to unsafe image generation content." |
| `OTHER` | "Prompt was blocked due to unknown reasons." |

**`FinishReason`（candidate 侧）生图相关值**

`IMAGE_SAFETY`、`IMAGE_PROHIBITED_CONTENT`、`IMAGE_RECITATION`、`IMAGE_OTHER`、`NO_IMAGE`，另有通用的 `STOP` / `MAX_TOKENS` / `SAFETY` / `RECITATION`。

⚠️ **同名不同层的坑**：`IMAGE_SAFETY` **既可能出现在 `BlockReason`（输入侧），也可能出现在 `FinishReason`（输出侧）**。
判别方法：看 `candidates` 是否存在 —— 不存在 = 输入侧拦截；存在但 `content` 为空 = 输出侧拦截。

### 4.4 Gemini 生图价格（Vertex 官方定价页脚注原文）

> **"Gemini 3 Pro Image charges 560 tokens per input image, with output image costs scaling by resolution: 1120 tokens ($0.134) for 1K and 2K (roughly 1MP and 4MP), and 2000 tokens ($0.24) for 4K (roughly 16MP)."**

> **"Gemini 3.1 Flash Image charges 1120 tokens per input image, with output image costs scaling by resolution: 747 tokens ($0.045) for 512 (roughly 0.25MP), 1120 tokens ($0.067) for 1K (roughly 1MP), 1680 tokens ($0.101) for 2K (roughly 4MP), and 2,520 tokens ($0.15) for 4K (roughly 16MP)."**

> **"Gemini 3.1 Flash-Lite Image charges 1120 tokens per input image, and 1120 tokens ($0.034) for 1K (roughly 1MP) output images."**

> **"A 1024x1024 image consumes 1290 tokens ($0.039 per 1K output image)."**（Gemini 2.5 Flash Image，Image output $30.00 / 1M tokens）

汇总表：

| 模型 | 输入图 token/张 | 输出图分辨率 | 输出 token | 单张价格 |
|---|---|---|---|---|
| gemini-3-pro-image | 560 | 1K / 2K | 1120 | $0.134 |
| gemini-3-pro-image | 560 | 4K | 2000 | $0.24 |
| gemini-3.1-flash-image | 1120 | 512 | 747 | $0.045 |
| gemini-3.1-flash-image | 1120 | 1K | 1120 | $0.067 |
| gemini-3.1-flash-image | 1120 | 2K | 1680 | $0.101 |
| gemini-3.1-flash-image | 1120 | 4K | 2520 | $0.15 |
| gemini-3.1-flash-lite-image | 1120 | 1K | 1120 | $0.034 |
| gemini-2.5-flash-image | — | 1024×1024 | 1290 | $0.039 |

出处：
- <https://docs.cloud.google.com/gemini-enterprise-agent-platform/models/capabilities/gemini-image-responsible-ai>
- <https://docs.cloud.google.com/gemini-enterprise-agent-platform/models/capabilities/process-blocked-responses>
- <https://docs.cloud.google.com/gemini-enterprise-agent-platform/models/gemini/3-pro-image>
- <https://cloud.google.com/gemini-enterprise-agent-platform/generative-ai/pricing>
- <https://ai.google.dev/gemini-api/docs/pricing>

---

## 五、安全过滤 support code 对照表（官方）

被拦时响应里可能带 `Support codes: 56562880` 这类数字码，官方映射如下：

| Error code | Safety category | 描述 | 出现位置 |
|---|---|---|---|
| 58061214 / 17301594 | Child | 检测到儿童内容 | input (prompt) |
| 29310472 / 15236754 | Celebrity | 名人写实形象 | input (prompt) |
| 64151117 | Celebrity or child | 违反政策的名人/儿童写实形象 | input (prompt) |
| 62263041 | Dangerous content | 危险内容 | input (prompt) |
| 57734940 / 22137204 | Hate | 仇恨内容 | input (prompt) |
| 74803281 / 29578790 / 42876398 | Other | 其它杂项安全问题 | **input + output (image)** |
| 92201652 | Personal information | PII（信用卡号、住址等） | input (prompt) |
| 89371032 / 49114662 / 72817394 | Prohibited content | 禁止内容 | input (prompt) |
| 90789179 / 63429089 / 43188360 | Sexual | 色情内容 | input (prompt) |
| 78610348 | Toxic | 恶意内容 | input (prompt) |
| 61493863 / 56562880 | Violence | 暴力内容 | input (prompt) / **output (image)** |
| 32635315 | Vulgar | 低俗内容 | input (prompt) |
| 39322892 | People/Face | 安全设置不允许出现人/人脸时检测到 | **output (image)** |

> 注意「出现位置」列：标 **output (image)** 的才是「出图后被拦」，标 input 的是「进都没进去」。

出处：<https://cloud.google.com/vertex-ai/generative-ai/docs/image/responsible-ai-imagen#safety-filter-code-categories>

---

## 六、安全过滤可配置性（官方）

- **不可配置过滤器（Non-configurable）**：CSAM、PII。`PROHIBITED_CONTENT` 属于此类。
- **可配置过滤器（Configurable）**：Hate Speech / Harassment / Sexually Explicit / Dangerous Content，阈值 `BLOCK_LOW_AND_ABOVE` > `BLOCK_MEDIUM_AND_ABOVE` > `BLOCK_ONLY_HIGH` > `BLOCK_NONE` / `OFF`。
- **Jailbreak 分类器**：默认 off，官方明确 **"Like other Gemini safety filters, using the jailbreak classifier is free."**（安全过滤本身不额外收费）
- ⚠️ 官方口径：`BLOCK_NONE` / `OFF` **只关掉可配置过滤器**，**关不掉输出侧的 image safety 兜底**。这就是「把安全设置全关了还是 `IMAGE_SAFETY`」的原因。

> "If a filter blocks the response, it clears the response's `Candidate.content` field. It does not provide any feedback to the model."

出处：<https://docs.cloud.google.com/gemini-enterprise-agent-platform/models/capabilities/configure-safety-filters>

---

## 七、给网关（本项目）的计费实现建议

基于上面的官方口径，中转网关侧的判定逻辑应该是：

```
1) 上游 HTTP != 200
   → 零计费（Google 自己也不收）

2) 上游 HTTP == 200，走 Gemini :generateContent
   → 一律以响应里的 usageMetadata 为准计费，不要自己按「有没有出图」猜：
     - promptTokenCount        → 输入计费（含参考图 560/1120 token）
     - thoughtsTokenCount      → 按文本 output 价计费（拦截时依然有值！）
     - candidatesTokenCount    → 图片 output 计费；被拦时为 0，自然就不收图片钱
   → 流式场景要确保最后一个 chunk 的 usageMetadata 被采集到，否则会漏计 thinking token

3) 上游 HTTP == 200，走 Imagen :predict
   → 按 predictions 数组里**真实含 bytesBase64Encoded / gcsUri 的对象数**计费
   → 带 raiFilteredReason 的对象**不计入张数**（Google 原文：not charged for blocked images）

4) 日志/错误透传
   → 保留 finishReason / finishMessage / promptFeedback.blockReason / raiFilteredReason
   → 不要把 IMAGE_SAFETY 压成通用 5xx，否则用户无法区分「策略拦截」和「网关故障」
```

---

## 八、参考链接（全部为官方）

| 主题 | 链接 |
|---|---|
| 生成式 AI 定价（计费总则 + Gemini Image / Imagen 价格） | <https://cloud.google.com/gemini-enterprise-agent-platform/generative-ai/pricing> |
| Gemini Developer API 定价 | <https://ai.google.dev/gemini-api/docs/pricing> |
| Gemini 生图与 Responsible AI（拦截三态、support code） | <https://docs.cloud.google.com/gemini-enterprise-agent-platform/models/capabilities/gemini-image-responsible-ai> |
| Process blocked responses（两类拦截的响应结构） | <https://docs.cloud.google.com/gemini-enterprise-agent-platform/models/capabilities/process-blocked-responses> |
| Safety and content filters（枚举、阈值、jailbreak 免费） | <https://docs.cloud.google.com/gemini-enterprise-agent-platform/models/capabilities/configure-safety-filters> |
| Responsible AI and usage guidelines for Imagen（400 / raiFilteredReason / "not charged"） | <https://cloud.google.com/vertex-ai/generative-ai/docs/image/responsible-ai-imagen> |
| Gemini 3 Pro Image 模型页（token 消耗） | <https://docs.cloud.google.com/gemini-enterprise-agent-platform/models/gemini/3-pro-image> |
| GenerateContentResponse REST 参考（FinishReason / BlockReason） | <https://ai.google.dev/api/generate-content> |
