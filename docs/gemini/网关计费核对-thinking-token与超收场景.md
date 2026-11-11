# 网关计费核对：Gemini 生图 thinking token 与「官方不收费、平台收费」场景

> # ⛔ 本文多处结论已被实测推翻，不要作为依据
>
> **请改用 → [`测试证据_生图拦截计费_20260921.md`](测试证据_生图拦截计费_20260921.md)**
>
> 2026-09-21 在测试环境对真实 Vertex 上游实测 + 生产数据交叉验证后，以下结论**均不成立**：
>
> | 本文的结论 | 实测结果 |
> |---|---|
> | 「约 576 笔被按整张图收费」 | ❌ Vertex 对被拦图**不上报** `candidatesTokenCount`，从未按图片档计费 |
> | 「thinking token 被按图片档 60 倍率结算，10× 超收」 | ❌ thinking 始终走文本档；线上倍率与阶梯表达式均正确 |
> | 「8/9 月敞口 $9,000–$14,000」 | ❌ 实际净多收 **$42.81**（7–9 月合计） |
> | 「客户主张 $60.63 成立且偏保守」 | ❌ 该表金额与我方计费口径无关 |
>
> **唯一成立的缺陷**：图片模型返回纯文本时，该段文本被误按图片单价计费 —— 与 thinking token 无关。
>
> 错误根源：本文基于「上游在拦截时仍上报图片 token」这一**未经验证的假设**做反推。该假设已被证伪。
> 保留本文仅为记录排查过程；`gemini-3-pro-image` 走阶梯计费、倍率默认值订正等事实性内容仍然有效。

> 核对日期：2026-09-21 · 分支 `main-alpha`
> 对照基准：`docs/gemini/vertex-gemini-生图-安全拦截与计费-官方文档汇总.md`
> 结论分三档：**🔴 确认缺陷** / **🟡 需按线上配置核实** / **🟢 已正确**

> ## ⚠️ 适用范围更正
>
> `gemini-3-pro-image` 线上走的是 **billingexpr 分档表达式计费**（`ModelBillingMode = tiered_expr`）。
> 这条路径在 `service/text_quota.go:482-495` 由 `TryTieredSettle` **整体接管** `summary.Quota`，
> 倍率（ModelRatio / CompletionRatio / ImageCompletionRatio）**完全不参与**。
>
> 因此：
> - 第一节里关于「ImageCompletionRatio 回退到 CompletionRatio」的分析 **对 `gemini-3-pro-image` 不适用**，
>   只对仍走倍率计费的模型有效。代码默认值的修正仍然保留（对新部署/非分档模型有意义）。
> - 分档模型的计费真相只在**表达式字符串**里，见下方第 〇-B 节。

---

## 〇、580 次失败工单逐类对照（用户截图）

问题：这些失败我们是否也收费？是不是「非报错」？

**答：输出级拦截全部是 HTTP 200 非报错，且全部照常计费；prompt 级虽然给客户端返 400，也仍然计费（金额≈0）。**

判定分水岭只有一条：`len(geminiResponse.Candidates) == 0`（`relay/channel/gemini/relay-gemini.go:1822`）。

| 用户表格类别 | candidates | 网关处理 | 客户端看到 | 是否计费 | 计什么 |
|---|---|---|---|---|---|
| `IMAGE_PROHIBITED_CONTENT`（输出级） 239 次 | 有（`content` 空） | 走正常成功路径 | **HTTP 200**，空 content，`finish_reason: content_filter` | **✅ 计费** | input + thoughts(+text) |
| `IMAGE_SAFETY`（输出级） 165 次 | 有 | 同上 | HTTP 200 | ✅ 计费 | 同上 |
| `NO_IMAGE` 77 次 | 有 | 同上 | HTTP 200 | ✅ 计费 | 同上 |
| `IMAGE_RECITATION` 3 次 | 有 | 同上 | HTTP 200 | ✅ 计费 | 同上 |
| prompt 级 `PROHIBITED_CONTENT`/`OTHER` 80 次 | 空 + `promptFeedback` | `relay-gemini.go:1825-1856` 写 `ContextKeyAdminRejectReason`，给客户端返 **400** | HTTP 400 | **✅ 仍计费** | 仅 `promptTokenCount`（≈0） |
| prompt 级 `IMAGE_SAFETY` / 输出级 `PROHIBITED_CONTENT` 16 次 | 同上 | 同上 | HTTP 400 / 200 | ✅ 计费 | 同上 |

**为什么「非报错」**：`responseGeminiChat2OpenAI`（`relay-gemini.go:1396-1422`）的 finishReason 映射里，
`IMAGE_SAFETY` / `IMAGE_PROHIBITED_CONTENT` / `NO_IMAGE` / `IMAGE_RECITATION` 都没有单独 case，
统一落到 `default: choice.FinishReason = content_filter`，**不产生 error**。
handler 最后 `return &usage, nil` → relay 层判定成功 → `PostTextConsumeQuota` 正常结算。

**唯一例外**：走 `/v1/images/generations` 的 `GeminiImagineImageHandler`（`relay-gemini.go:2260-2273`）
在没出图时 `return nil, newAPIError`（400）→ relay 层判失败 → **全额退预扣，不计费**。
→ 反推：这 580 次既然扣到钱了，客户端走的一定是 **`/v1/chat/completions` 或原生 `:generateContent`**，
不是 `/v1/images/generations`。

**金额反推（重要线索）**：

| 类别 | 次数 | 实收 | 单次均价 |
|---|---|---|---|
| IMAGE_PROHIBITED_CONTENT | 239 | $34.67 | **$0.145** |
| IMAGE_SAFETY | 165 | $20.71 | **$0.126** |
| NO_IMAGE | 77 | $4.90 | $0.064 |
| IMAGE_RECITATION | 3 | $0.26 | $0.087 |
| prompt 级（合计） | 96 | $0.09 | $0.0009 ✅ 符合「只收 input」 |

按官方文本档算，一次拦截应该只有 `input ~500 tok × $2/M + thoughts ~1200 tok × $12/M ≈ $0.015`。
实收 **$0.145 ≈ 10 倍**，而且几乎正好等于 `1200 thoughts × $120/M`（图片档单价）。
→ **强烈指向 thinking token 被按图片档倍率结算**，即下面第一节的问题。
$0.145 也接近一张 2K 图的官方价 $0.134，容易被误读成「图片被计费」，但图片 token 其实是 0。

**请用一条真实日志确认**（`logs.other` 里有 `model_ratio` / `completion_ratio` / `reasoning_tokens`）：

```sql
SELECT id, model_name, prompt_tokens, completion_tokens, quota, other
FROM logs
WHERE model_name LIKE '%image%' AND created_at > <拦截时间戳>
ORDER BY id DESC LIMIT 20;
```

- `other.completion_ratio` = **60** → 就是本文第一节的问题，thinking 被按图片档收
- `other.completion_ratio` = **6** → 那金额另有来源，需要再看 `prompt_tokens` / `reasoning_tokens` 明细

---

## 〇-B、分档表达式路径下这笔钱是怎么算出来的

### 引擎侧：thinking token 没有独立变量

`pkg/billingexpr/run.go:56-67` 的完整变量表：`p / c / len / cr / cc / cc1h / img / img_o / ai / ao`。
**没有 reasoning / thinking 变量** —— thinking token 只能跟在 `c` 里。

`service/tiered_settle.go:66-110` 的取值：

```go
c := usage.CompletionTokens          // = candidatesTokenCount + thoughtsTokenCount
imgO := usage.CompletionTokenDetails.ImageTokens
if usedVars["img_o"] { c -= imgO }   // 仅当表达式用了 img_o 才扣
```

所以：

| 表达式写法 | 拦截时 `c` | 拦截时成本（p≈500, thoughts≈1200） |
|---|---|---|
| `tier("base", p*2 + c*12 + img_o*120)` ✅ | 1200 | 500×$2/M + 1200×$12/M ≈ **$0.015** |
| `tier("base", p*2 + c*120)` ❌ 没拆 `img_o` | 1200 | 500×$2/M + 1200×$120/M ≈ **$0.145** |
| `tier("base", p*2 + c*12)` | 1200 | $0.015，但正常出图时图片只收 $12/M → 严重少收 |

实测单价 $0.145 **精确命中第二行**。

> 这是最容易踩的写法：管理员看到「Nano Banana Pro 图片输出 $120/M」，直接写成 `c * 120`。
> 后果不只是拦截场景 —— **正常出图也在多收**：
> 一次成功生成 `c = 1120(图) + 1200(思考)`，全按 $120/M ≈ **$0.279**，
> 官方实际是 `1120×$120/M + 1200×$12/M ≈ $0.148`，接近翻倍。

### 官方价（Vertex 定价页原文，Gemini 3 Pro Image）

| 类型 | ≤200K | >200K |
|---|---|---|
| Input (text, image) | $2.00 | $4.00 |
| Text output (response **and reasoning**) | $12.00 | $18.00 |
| Image Output | $120.00 | $120.00 |

**推荐表达式**（`img` 不写 —— 输入图与文本输入同价 $2，留在 `p` 里正好）：

```
len <= 200000
  ? tier("standard",     p * 2 + c * 12 + img_o * 120)
  : tier("long_context", p * 4 + c * 18 + img_o * 120)
```

### 怎么确认线上到底配的是什么

表达式会**base64 写进每条日志**（`service/log_info_generate.go:399-414`）：

```
other.billing_mode = "tiered_expr"
other.expr_b64     = <base64(表达式原文)>
other.matched_tier = <命中档位>
```

在管理端日志详情里取任意一条 `gemini-3-pro-image` 的 `expr_b64`，`base64 -d` 即可看到真实表达式。

### 还要看一眼上游到底报了多少 token

$0.145 有两种可能来源，**必须看原始 usageMetadata 才能定**：

1. `c * 120` 把 1200 thinking 按图片价收 →（网关表达式问题）
2. 上游在拦截时**仍然上报了 `candidatesTokensDetails: [{IMAGE, 1120}]`** → 1120 × $120/M = $0.134
   →（网关照单全收了一张根本没交付的图）

网关已经把原始 usageMetadata 打到 INFO 日志（`relay-gemini.go:1817-1820` / `1621-1625`）：

```bash
# 注意：.gz 就地压在 pod 目录，别只 grep *.log
zgrep -h "gemini upstream usageMetadata" <pod日志目录>/*.log <pod日志目录>/*.gz \
  | grep "gemini-3-pro-image" | tail -50
```

看 `candidatesTokenCount` / `candidatesTokensDetails` / `thoughtsTokenCount` 三个字段即可区分。

### 🔴 无论哪种，这个护栏都该加：没交付图片就不计 img_o

`relay-gemini.go:1862-1875` 目前**无条件信任**上游的 `candidatesTokensDetails[IMAGE]`：

```go
for _, detail := range geminiResponse.UsageMetadata.CandidatesTokensDetails {
    if strings.EqualFold(mod, "IMAGE") { imageOutputTokens += detail.TokenCount }
}
// hasImagePart 检查只在「上游没给 details」的兜底分支里才跑
if imageOutputTokens == 0 && audioOutputTokens == 0 && textOutputTokens == 0 && CandidatesTokenCount > 0 {
    hasImagePart := ...
}
```

Google 官方明确 **"You will not be charged for blocked images."** ——
既然图没交付，`img_o` 就不该有值。建议把 `hasImagePart`（响应里是否真有 `inlineData: image/*`）
提升为**前置判断**：没有 image part 时，`imageOutputTokens` 归零、并入文本档。
这样无论上游怎么报，被拦的图都不会按 $120/M 计费。

> 观测盲区：`logs.other` 里没有图片/文本输出 token 的拆分
> （`service/log_info_generate.go:114-125` 只写 model_ratio / group_ratio / completion_ratio）。
> 分档模型下建议补 `img_o` / `c` 的实际取值，否则这类账单纠纷无法自证。

---

## 一、thinking token 有没有漏计？

### 🟢 结论：**没有漏计，链路完整**

| 环节 | 代码位置 | 行为 |
|---|---|---|
| 上游解析 | `relay/channel/gemini/relay-gemini.go:1257-1262` | `CompletionTokens = CandidatesTokenCount + ThoughtsTokenCount`，并写入 `CompletionTokenDetails.ReasoningTokens` |
| 流式采集 | `relay/channel/gemini/relay-gemini.go:1617-1657` | 每个带 `usageMetadata` 的 chunk 都重建 usage（Gemini 上报累计值，末包覆盖），最后一包不会丢 |
| 倍率计费 | `service/text_quota.go:404-414` | 拆分计费分支里 `textOutputTokens = ReasoningTokens + GeminiTextOutputTokens`，thinking 计入文本档 |
| 分档表达式计费 | `service/tiered_settle.go:66-110` | `c = CompletionTokens`（含 thoughts），仅当表达式用了 `img_o` 才 `c -= imgO`，thinking 留在 `c` |
| 模型级 output 口径 | `service/tiered_settle.go:39-47` | 非 Qwen 直接返回 `usage.CompletionTokens`（已含 thoughts） |

**输出被 `IMAGE_SAFETY` 拦截时**：上游 `candidatesTokenCount=0`、`thoughtsTokenCount=173` →
`CompletionTokens = 173` → `GeminiImageOutputTokens = 0` → 走 else 分支
`completionQuota = 173 × CompletionRatio`。thinking **有计到**，图片 token **没被计**。

> ✅ 这一点与 Google 官方 "You will not be charged for blocked images" 一致。

### 🔴 但是：thinking token 被**按图片档倍率**计价（结构性超收）

问题不在「漏计」，而在**计错档**。

**官方价格**（Vertex 定价页原文，Gemini 3 Pro Image / Nano Banana Pro）：

| 类型 | 单价 |
|---|---|
| Input (text, image) | **$2.00** /1M |
| **Text output (response and reasoning)** | **$12.00** /1M ← thinking token 在这一档 |
| Image Output | **$120.00** /1M |

换算成 new-api 倍率（modelRatio=1 对应 $2/M）：
- 正确 `CompletionRatio`（文本+thinking）= 12 / 2 = **6**
- 正确 `ImageCompletionRatio`（图片输出）= 120 / 2 = **60**

**代码里的默认值**：

```go
// setting/ratio_setting/model_ratio.go:640-643  —— getHardcodedCompletionModelRatio
} else if strings.HasPrefix(name, "gemini-3-pro") {
    if strings.HasPrefix(name, "gemini-3-pro-image") {
        return 60, false          // ← 这是 CompletionRatio（文本+thinking 档），却填了图片档的 60
    }
    return 6, false
}
```

```go
// setting/ratio_setting/model_ratio.go:732-736
var defaultImageCompletionRatio = map[string]float64{
    "gpt-image-1":                8,
    "gpt-image-2":                6,  // $30 / $5 = 6x  ✅ 换算正确
    "gemini-3-pro-image-preview": 6,  // ← 应为 120/2 = 60，填成了文本档的 6
}
```

```go
// setting/ratio_setting/model_ratio.go:759-771
// GetImageCompletionRatio 未配置时回退到 GetCompletionRatio
func GetImageCompletionRatio(name string) float64 {
    ...
    return GetCompletionRatio(name)   // ← 两个档共用同一个回退值
}
```

**净效果（仅在 options 表未覆盖时生效的硬编码默认值）**：

| 模型 | 图片输出倍率 | 文本 + thinking 倍率 | 偏差 |
|---|---|---|---|
| `gemini-3-pro-image` | 60（回退到 CompletionRatio） ✅ | **60**（应 6） | **thinking / 文本输出 10× 超收** |
| `gemini-3-pro-image-preview` | **6**（应 60） | **60**（应 6） | 图片 10× 少收 + 文本/thinking 10× 超收，两个值写反了 |
| `gemini-3.1-flash-image` | 4（默认兜底，应 60/0.25=**240**） | 4（应 3/0.25=**12**） | 图片严重少收 |

> 💡 **这就是「输出被拦截还是被扣钱了」的真正来源**：被拦时图片 token 确实是 0，
> 但剩下的 thinking token 被按 60 倍率（图片档）而不是 6 倍率（文本档）结算，多收 10 倍。
> 一次 Nano Banana Pro 拦截约 170~250 thinking token，单次差额 ≈ $0.0025 vs $0.00025，
> 量大 + 用户反复重试时会明显。

### ✅ 已修（2026-09-21，仅改代码默认值）

`setting/ratio_setting/model_ratio.go`：

```go
// getHardcodedCompletionModelRatio
} else if strings.HasPrefix(name, "gemini-3-pro") {
-   if strings.HasPrefix(name, "gemini-3-pro-image") {
-       return 60, false
-   }
    return 6, false        // 文本/思考输出 $12 / $2 = 6x，图片档不在这里
}

// defaultImageCompletionRatio —— 口径：图片输出单价 / 文本输入单价
-   "gemini-3-pro-image-preview": 6,
+   "gemini-3-pro-image":             60,   // $120 / $2
+   "gemini-3-pro-image-preview":     60,
+   "nano-banana-pro-preview":        60,
+   "gemini-3.1-flash-image":         120,  // $60 / $0.5
+   "gemini-3.1-flash-image-preview": 120,
+   "gemini-2.5-flash-image":         100,  // $30 / $0.3
+   "gemini-2.5-flash-image-preview": 100,
```

验证：`go build ./...` 通过，`go test ./setting/ratio_setting/ -count=1` 通过。

### 🔴 但这个代码改动**对已有部署无效**，必须同步改 options

两个原因：

1. `getHardcodedCompletionModelRatio` 返回 `contain=false`，options 表里的 `CompletionRatio` 优先。
2. 更关键：`types.LoadFromJsonString`（`types/rw_map.go:76-81`）是
   `m.data = make(map[K]V)` 后整体反序列化 —— **整张表替换，不是 merge**。
   所以只要 options 里存在 `ImageCompletionRatio` 这一行（管理端保存过一次就会有），
   新加的代码默认值一个都不会生效。
   > 附带发现：`model_ratio.go:396-397` 的注释「表里没有的 key 则保持此处默认值」对这些
   > 通过 `LoadFromJsonString` 加载的 map 是**不成立的**，容易误导。

**线上要做的事**（管理端 → 倍率设置）：

| 配置项 | gemini-3-pro-image 应为 |
|---|---|
| `ModelRatio` | 1（输入 $2/M） |
| `CompletionRatio` | **6**（文本+思考输出 $12/M） |
| `ImageCompletionRatio` | **60**（图片输出 $120/M） |

自检公式：
- `ModelRatio × CompletionRatio × $2 = 文本输出单价`
- `ModelRatio × ImageCompletionRatio × $2 = 图片输出单价`

> 💡 最容易踩的坑：早期 `ImageCompletionRatio` 没配时会回退到 `CompletionRatio`，
> 于是管理员把 `CompletionRatio` 直接设成 60 让图片价对上 —— 图片是对了，
> **但思考/文本输出也跟着按 60 收，翻了 10 倍**。这正是 580 次拦截扣出 $60 的机制。

### 🟡 潜在重复计费（未复现，建议加护栏）

`service/text_quota.go:405`：

```go
textOutputTokens := int64(summary.ReasoningTokens + summary.GeminiTextOutputTokens)
```

`GeminiTextOutputTokens` 来自上游 `candidatesTokensDetails[TEXT]`。按官方口径它拆分的是
`candidatesTokenCount`，**不含** `thoughtsTokenCount`，所以相加是对的。
但这里**没有上限钳制** —— 一旦某个上游/中转把 thoughts 也算进 TEXT modality，thinking 就会被计两遍。

建议加一行兜底（与已有的 `< 0` 钳制对称）：

```go
if maxText := int64(summary.CompletionTokens - summary.GeminiImageOutputTokens); maxText >= 0 && textOutputTokens > maxText {
    textOutputTokens = maxText
}
```

---

## 二、「官方不收费，但平台收费」的场景清单

### 🔴 #1 Imagen 部分图片被 RAI 过滤，仍按请求的 `n` 张计费

**最确凿的一处超收。**

官方：`raiFilteredReason` 原文 **"You will not be charged for blocked images."** —— 被拦的图不收钱，
`predictions` 数组里只返回未被过滤的图。

网关：

```go
// relay/channel/gemini/relay-gemini.go:1996-2002
for _, prediction := range geminiResponse.Predictions {
    if prediction.RaiFilteredReason != "" {
        continue // ✅ 正确跳过被过滤的图
    }
    openAIResponse.Data = append(...)
}
...
// relay/channel/gemini/relay-gemini.go:2016-2023
generatedImages := len(openAIResponse.Data)
usage := &dto.Usage{
    PromptTokens: imageTokens * generatedImages,
    ...
}                       // ❌ 唯独没有 usage.GeneratedImages = generatedImages
```

```go
// relay/image_handler.go:229-234
imageN := uint(1)
if u, ok := usage.(*dto.Usage); ok && u != nil && u.GeneratedImages > 0 {
    imageN = uint(u.GeneratedImages)      // 走不到：GeneratedImages 恒为 0
} else if request.N != nil && *request.N > 0 {
    imageN = *request.N                   // ← 回退到「请求张数」
}
```

**后果**：`n=4`、2 张被安全过滤 → Google 只收 2 张 → 网关按 **4 张**计费（`UsePrice` 按次价模式下）。

**影响范围**：`gemini` 渠道 + `vertex` 渠道的 `imagen*` 模型
（`relay/channel/gemini/adaptor.go:248` 与 `relay/channel/vertex/adaptor.go:381` 都调 `GeminiImageHandler`）。

**修复**：`relay-gemini.go:2019` 的 usage 结构体补一行，与兄弟函数
`GeminiImagineImageHandler`（`relay-gemini.go:2286` 已有 `usage.GeneratedImages = len(imageResponse.Data)`）对齐：

```go
usage := &dto.Usage{
    PromptTokens:    imageTokens * generatedImages,
    CompletionTokens: 0,
    TotalTokens:     imageTokens * generatedImages,
    GeneratedImages: generatedImages,   // ← 补这一行
}
```

> ⚠️ 注意：`UsePrice=false`（按 token 倍率）时不受影响，因为 `n` 的 OtherRatio 根本不会加；
> 但 imagen 线上通常配的是按次价，所以这个 bug 是实际生效的。

---

### 🔴 #2 Gemini 原生端点：纯文本响应被按图片单价计费（同一 bug 只修了一半）

OpenAI 格式入口 `GeminiChatHandler` 已经用 `hasImagePart`（看是否真有 `inlineData: image/*`）来判断，
并在注释里明确写了「不再使用基于模型名的兜底（会误伤纯文本响应）」：

```go
// relay/channel/gemini/relay-gemini.go:1876-1898  ✅ 已修
if imageOutputTokens == 0 && audioOutputTokens == 0 && textOutputTokens == 0 && CandidatesTokenCount > 0 {
    hasImagePart := ...  // 遍历 parts 看有没有 image inlineData
    if hasImagePart { imageOutputTokens = ... } else { textOutputTokens = ... }
}
```

但 **Gemini 原生格式入口** `GeminiTextGenerationHandler` 还是老逻辑：

```go
// relay/channel/gemini/relay-gemini-native.go:65-69  ❌ 未修
if imageOutputTokens == 0 && geminiResponse.UsageMetadata.CandidatesTokenCount > 0 &&
    strings.Contains(strings.ToLower(info.OriginModelName), "image") {
    imageOutputTokens = geminiResponse.UsageMetadata.CandidatesTokenCount
    textOutputTokens = 0
}
```

**命中场景恰好就是安全拒答**：官方文档写明「Model refusal → 模型返回一段文本说明不能生成，
`FinishReason` 是 `STOP`」。此时 `gemini-3-pro-image` 返回的是纯文本，
但因为模型名含 "image"，这段文本 token 被整体划进图片档计费。

`NO_IMAGE`、纯文本对话、function call 等场景同样中招。

**影响范围**：`RelayModeGemini`（客户端用 `/v1beta/models/xxx:generateContent` 原生格式调网关），
`gemini` 渠道（`adaptor.go:244`）+ `vertex` 渠道（`adaptor.go:378`）。
流式入口 `GeminiTextGenerationStreamHandler` 也建议一并核对。

**修复**：把 `relay-gemini.go:1878-1898` 的 `hasImagePart` 判断搬到 native handler，
顺带补上 `AUDIO` modality（native 版本目前只处理 IMAGE/TEXT，音频输出会落空）。

---

### 🟡 #3 上游 usageMetadata 缺失时用网关自己的估算值计费

```go
// relay/channel/gemini/relay-gemini.go:1822-1856
if len(geminiResponse.Candidates) == 0 {
    usage := buildUsageFromGeminiMetadata(geminiResponse.UsageMetadata, info.GetEstimatePromptTokens())
    ...
    return &usage, nil        // ← 注意：返回 nil error，relay 层视为「成功」，照常结算
}
```

`buildUsageFromGeminiMetadata` 在 `promptTokenCount <= 0` 时会回退到 `GetEstimatePromptTokens()`。

- **prompt 被拦截**（`promptFeedback.blockReason` + `usageMetadata.promptTokenCount=7`）
  → 按 7 个输入 token 计费。**这与官方一致**（上游是 HTTP 200，Google 同样收输入费），✅ 不算超收。
- **空响应且 usageMetadata 全空** → 网关用**自己估算的** prompt token 计费，
  而上游这种情况通常是异常/超时，Google 侧未必产生用量。🟡 建议：`usageMetadata` 完全为空时计 0。

另外 `service/text_quota.go:425-427` 有个保底：

```go
if !ratio.IsZero() && quotaCalculateDecimal.LessThanOrEqual(decimal.Zero) {
    quotaCalculateDecimal = decimal.NewFromInt(1)   // 最低 1 quota
}
```

即使算出来 0 也会收 1 quota。金额可忽略，但要知道「绝对零收费」在倍率模式下做不到。

---

### 🟡 #4 违规扣费：官方 4xx 不收费，平台额外收一笔

```go
// controller/relay.go:175-184
if newAPIError != nil {
    relayInfo.Billing.Refund(c)                              // ✅ 先全额退预扣
    service.ChargeViolationFeeIfNeeded(c, relayInfo, newAPIError)  // ⚠️ 再单独收违规费
}
```

触发条件（`service/violation_fee.go:22-38`）是错误消息里含这两个字符串之一：

| marker | 来源 |
|---|---|
| `Failed check: SAFETY_CHECK_TYPE` | xAI / Grok 的 CSAM 拦截 |
| `Content violates usage guidelines` | **通用字符串，任何上游命中都会扣** |

这是**平台主动的风控策略**（由 `GrokSettings.ViolationDeductionEnabled` 开关控制），
本身不是 bug；但它确实属于「Google 返回 4xx 不收费、平台收费」。

**需要留意**：Vertex/Gemini 的拦截文案是
*"Unable to show this content because it violates Gemini Enterprise's usage guidelines. You will not be charged for blocked images."*
—— 字面**不包含** `Content violates usage guidelines`（大小写和词序都不同），当前不会误触发。
但这个 marker 是裸子串匹配，上游文案一改就可能误伤。建议：
1. 把违规扣费的匹配收敛到渠道类型（只对 xAI 生效），或
2. 显式排除含 `You will not be charged` 的上游文案。

---

### 🟢 #5 上游 4xx / 5xx → 已正确全额退款

`controller/relay.go:175-181` 的 `defer` 里 `relayInfo.Billing.Refund(c)` 会退掉预扣额度，
与官方 *"Requests returning any other response codes, such as 4xx and 5xx codes, aren't charged"* 一致。
Imagen 输入被拦截返回 HTTP 400 也走这条路径，✅ 不收费。

---

### 🟢 #6 Imagen 输入图 token 倍率

`service/text_quota.go:374-377`：输入图片 token 从 baseTokens 扣出后乘 `ImageRatio`，
`GetImageRatio` 默认返回 1 → 与文本输入同价。✅ 符合 Gemini「输入图按输入价计费」的官方口径。

---

## 三、顺带发现：本包 2 个既存失败测试（非本次改动引入）

```
--- FAIL: TestGeminiChatHandlerPromptOnlyUsageMetadataEstimatesCompletionTokens
      relay_gemini_usage_test.go:529  "0" is not greater than "0"
--- FAIL: TestGeminiChatHandlerMissingUsageMetadataBuildsEstimatedBillingUsage
      relay_gemini_usage_test.go:416  Expected value not to be nil.
```

两个测试都在断言「上游漏报 `candidatesTokenCount` 时，网关应本地估算输出 token 并写 `usage.BillingUsage`」。
当前 `GeminiChatHandler` 既不本地估算、也不填 `BillingUsage` → 这类响应**输出侧按 0 计费**（少收方向）。
属于既存契约缺口，与本次核对的超收问题方向相反，一并记录。

复现：`go test ./relay/channel/gemini/ -run TestGemini -count=1`

---

## 四、建议修复优先级

| 优先级 | 项 | 位置 | 方向 |
|---|---|---|---|
| P0 | 倍率档位写反 / 回退共用 | `setting/ratio_setting/model_ratio.go:641,735` + 线上 options | thinking 10× 超收 |
| P0 | Imagen 按 `n` 而非实际返回张数计费 | `relay/channel/gemini/relay-gemini.go:2019` | 被拦图超收 |
| P1 | native handler 模型名兜底 | `relay/channel/gemini/relay-gemini-native.go:65` | 文本按图片价 |
| P2 | textOutputTokens 缺上限钳制 | `service/text_quota.go:405` | 潜在重复计费 |
| P2 | 违规扣费 marker 过于宽泛 | `service/violation_fee.go:23` | 误伤风险 |
| P3 | usageMetadata 全空时用估算值 | `relay/channel/gemini/relay-gemini.go:1823` | 无用量仍收费 |
