# doubao-seedream-5-0 图片生成全流程说明

本文档描述从 Gravitex 客户端发起图片生成（模型 `doubao-seedream-5-0-260128`）到 Go 后端、上游火山引擎 API、计费与日志的完整链路。确保**参数、计费、日志**均不缺失。

---

## 1. 调用链路概览

```
Gravitex 客户端 (gravitex-api-cli)
    │  POST /v1/images/generations  (Base: /llm-api → api.gravitex.ai)
    ▼
Gravitex Go 后端 (gravitex-api)
    │  router/relay-router.go → controller.Relay(RelayFormatOpenAIImage)
    │  → relay/helper 解析 → dto.ImageRequest（含 Extra）
    │  → relay/image_handler.go → VolcEngine Adaptor
    │  → 转发到火山引擎 /api/v3/images/generations
    ▼
上游响应 → 计费 postConsumeQuota → 写 consume_log
```

---

## 2. 前端请求体样例（客户端 → Go）

客户端 Chat 页构建的 `requestData` 会包含以下全部字段，并经由 `imageGenerateService.generateImage()` 序列化为 HTTP body。调用 `POST {apiBaseUrl}/v1/images/generations` 时请求体示例：

```json
{
  "model": "doubao-seedream-5-0-260128",
  "prompt": "一只在沙滩上的柴犬",
  "n": 2,
  "size": "4096x4096",
  "response_format": "url",
  "style": "vivid",
  "temperature": 1.0,
  "quality": "hd",
  "seed": 42,
  "watermark": false,
  "sequential_image_generation": "disabled",
  "sequential_image_generation_options": {
    "max_images": 4
  },
  "output_format": "png",
  "tools": [{ "type": "web_search" }]
}
```

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| model | string | 是 | 模型 ID |
| prompt | string | 是 | 提示词 |
| n | number | 否 | 生成数量 1–4，默认 1 |
| size | string | 否 | 如 4096x4096、2048x2048（2K/4K+比例） |
| response_format | string | 否 | url / b64_json，默认 url |
| style | string | 否 | 风格（如 vivid、natural），前端 imageStyle |
| temperature | number | 否 | 温度，前端 temperature，默认 1.0 |
| quality | string | 否 | 品质 standard / hd，前端 imageQuality |
| seed | number | 否 | 随机种子 |
| watermark | boolean | 否 | 是否加水印 |
| sequential_image_generation | string | 否 | "auto" \| "disabled" |
| sequential_image_generation_options | object | 否 | 组图配置（如 max_images） |
| optimize_prompt_options | object | 否 | 仅 4.0/4.5，5.0 不传 |
| **output_format** | string | 否 | **仅 5.0**：输出格式 `png` \| `jpeg`，默认 png |
| **tools** | array | 否 | **仅 5.0**：联网能力，开启联网时为 `[{ "type": "web_search" }]` |

未在 `dto.ImageRequest` 中定义的字段（如 `seed`、`sequential_image_generation`、`temperature`、`quality`、`output_format`、`tools` 等）会进入 **Extra**，需由 Go 在转发时一并带给上游。**Seedream 5.0 支持联网（tools.web_search）与 output_format。**

**说明**：同系列模型 `doubao-seedream-4-5-251128` 与 5.0 使用相同 2K/4K 尺寸与组图等参数，但**不支持** `output_format` 与 `tools`，前端对该模型不展示联网/输出格式选项且不传这两项。

---

## 3. Go 后端解析与适配

### 3.1 路由与解析

- **路由**：`router/relay-router.go` 注册 `POST /images/generations` → `controller.Relay(c, types.RelayFormatOpenAIImage)`。
- **解析**：`relay/helper/valid_request.go` 中 `GetAndValidOpenAIImageRequest` 使用 `common.UnmarshalBodyReusable(c, imageRequest)` 得到 `dto.ImageRequest`。
- **Extra**：`dto/openai_image.go` 中 `ImageRequest.UnmarshalJSON` 会把未知字段写入 `request.Extra`，因此 `seed`、`sequential_image_generation`、`sequential_image_generation_options` 等都会在 `Extra` 中。

### 3.2 请求体转发到上游（需包含 Extra）

- **image_handler**：`relay/image_handler.go` 调用 `adaptor.ConvertImageRequest(c, info, *request)`，得到 `convertedRequest` 后执行 `common.Marshal(convertedRequest)` 作为发往上游的 body。
- **注意**：`dto.ImageRequest.MarshalJSON` 当前**不会**把 `Extra` 写回 JSON，因此若 Adaptor 直接返回 `request`，Extra 会丢失。
- **正确做法**：VolcEngine Adaptor 在 `ConvertImageRequest` 中应返回「已知字段 + Extra」的 map（或等价结构），再序列化，这样发往上游的 body 才包含 `seed`、`sequential_image_generation` 等。详见下文「6. 已修复项」。

### 3.3 实际上游请求体样例（Go → 火山引擎）

目标：与前端语义一致，且包含扩展参数。示例（含 style、temperature、quality）：

```json
{
  "model": "doubao-seedream-5-0-260128",
  "prompt": "一只在沙滩上的柴犬",
  "n": 2,
  "size": "4096x4096",
  "response_format": "url",
  "style": "vivid",
  "temperature": 1.0,
  "quality": "hd",
  "seed": 42,
  "watermark": false,
  "sequential_image_generation": "disabled",
  "sequential_image_generation_options": {
    "max_images": 4
  },
  "output_format": "png",
  "tools": [{ "type": "web_search" }]
}
```

上游实际 URL：`{channel_base_url}/api/v3/images/generations`（VolcEngine 渠道）。

---

## 4. 计费（按张、数量、OEM 折扣）

### 4.1 按张计费与数量

- **Usage 构造**：`relay/image_handler.go` 中若上游未返回 usage，则用请求的 `N` 补全：
  - `usage.TotalTokens = int(request.N)`
  - `usage.PromptTokens = int(request.N)`
- **计费公式**（`relay/compatible_handler.go` 中 `postConsumeQuota`）：
  - 使用「倍率计费」时：`quota = (promptTokens + completion 相关) * modelRatio * groupRatio`，其中对图片生成等价为与 **N（张数）** 相关。
  - 使用「价格计费」时：若模型配置了 `ImagePriceRatio`（如 dall-e），则 `modelPrice = modelPrice * meta.ImagePriceRatio`；doubao 当前走倍率路径，**数量通过 N 体现**。
- **结论**：计费已按「张数」体现（N 写入 PromptTokens/TotalTokens），无需再改。

### 4.2 OEM 折扣

- **GroupRatio**：`relay/helper/price.go` 中 `ModelPriceHelper` 调用 `HandleGroupRatio(c, info)` 得到 `groupRatioInfo`，其中已包含 OEM 分组折扣。
- **OEM 用户折扣**：`service.GetOemUserDiscountForQuota(c, info.OriginModelName)` 在价格/倍率上会再乘用户级 OEM 折扣。
- 扣费时使用的 `ratio` 为 `modelRatio * groupRatio`（再乘用户折扣），因此 **OEM 折扣已参与计费**。

### 4.3 预扣费与结算

- **预扣费**：`controller/relay.go` 中根据 `priceData.QuotaToPreConsume` 调用 `service.PreConsumeBilling`。
- **后结算**：`postConsumeQuota` 内根据实际 usage 算出 `quota`，再调用 `service.SettleBilling(ctx, relayInfo, quota)`，与预扣费多退少补。

---

## 5. 日志记录

### 5.1 扣费与日志写入

- **postConsumeQuota** 最后会调用 `model.RecordConsumeLog`，写入一条消费记录。
- **日志内容**包括：`PromptTokens`、`CompletionTokens`、`ModelName`、`Quota`、`Content`（含「大小」「品质」「生成数量」等）、`TokenId`、`UseTimeSeconds`、`Group`、`Other`、`PriceChain`。

### 5.2 日志中的关键字段（Other）

- `image_ratio`：图片倍率（若有）。
- `image_generation_call` / `image_generation_call_price`：若为 GPT 图片按次计费则会有。
- `oem_code`：OEM 标识（如 gravitex）。
- `oem_user_discount`：用户 OEM 折扣。
- `image_output_tokens` / `effective_image_output_ratio` 等：Gemini 类图片输出时会写。

对 doubao-seedream-5-0，主要依赖通用 token 数（即 N）、model、group、price_chain 等即可满足对账与计费展示。

### 5.3 Debug 日志（请求体）

- `common.DebugEnabled == true` 时，`relay/image_handler.go` 会打日志：`image request body: %s`（序列化后的请求体）。可用于核对**实际发往上游的 body** 是否包含 Extra。

---

## 6. 已修复项与检查清单

### 6.1 VolcEngine 转发 Extra（已修复）

- **问题**：直接返回 `request` 时，`ImageRequest.MarshalJSON` 不包含 `Extra`，导致 `seed`、`sequential_image_generation` 等未发到上游。
- **修复**：在 `relay/channel/volcengine/adaptor.go` 的 `ConvertImageRequest`（`RelayModeImagesGenerations`）中，将 `request` 序列化为 map 后，把 `request.Extra` 合并进去，再返回该 map，使发往上游的 body 包含全部扩展参数。

### 6.2 检查清单

| 项 | 说明 | 状态 |
|----|------|------|
| 前端 body | model、prompt、n、size、response_format、seed、watermark、sequential_*、optimize_*（5.0 不传） | 已实现 |
| Go 解析 | dto.ImageRequest + Extra | 已支持 |
| 上游 body | 已知字段 + Extra 一起序列化 | 需 VolcEngine 合并 Extra |
| 计费按张 | Usage 用 N 填充，quota 与 N 相关 | 已实现 |
| OEM 折扣 | groupRatio + GetOemUserDiscountForQuota | 已实现 |
| 消费日志 | RecordConsumeLog（含 quota、tokens、other、price_chain） | 已实现 |
| Debug 日志 | image request body 打印 | 已实现 |

---

## 7. 上游响应与客户端

- 上游返回标准 OpenAI 风格：`{ "data": [ { "url": "..." }, ... ], "created": ... }`。
- Go 通过 VolcEngine Adaptor 的 `DoResponse` 解析并原样或标准化后返回给客户端。
- 客户端从 `result.data` 取图片 URL 或 b64_json 展示。

以上为 doubao-seedream-5-0 图片生成从前端参数、Go 适配、上游请求、计费到日志的完整流程说明；所有参数、按张计费与 OEM 折扣、日志均覆盖。
