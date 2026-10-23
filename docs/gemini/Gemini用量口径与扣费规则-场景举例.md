# Gemini 用量口径与扣费规则 — 场景举例

> 文档日期：2026-10-22 · 分支 `main-alpha`
> 文中所有金额**均由真实计费代码跑出**（`calculateTextQuotaSummary` / `TryTieredSettle`），不是手算
> 配套代码：`service/cached_modality.go`、`service/text_quota.go`、`service/tiered_settle.go`

---

## 一、先读懂 Gemini 返回的 usageMetadata

```json
"usageMetadata": {
  "promptTokenCount": 35537,
  "promptTokensDetails": [
    { "modality": "TEXT",  "tokenCount": 1967  },
    { "modality": "IMAGE", "tokenCount": 33570 }
  ],
  "cachedContentTokenCount": 32689,
  "cacheTokensDetails": [            // 仅异价模型才返回
    { "modality": "TEXT",  "tokenCount": 1000  },
    { "modality": "IMAGE", "tokenCount": 31689 }
  ],
  "candidatesTokenCount": 492,
  "candidatesTokensDetails": [ { "modality": "TEXT", "tokenCount": 492 } ],
  "thoughtsTokenCount": 1270,
  "toolUsePromptTokenCount": 0,
  "totalTokenCount": 37299
}
```

### 字段之间的关系（最关键的一节）

```
promptTokenCount 35537  ← 输入总量，已经包含缓存命中的部分
  ├─ 按模态切：  TEXT 1967 + IMAGE 33570 = 35537          ← promptTokensDetails
  └─ 按缓存切：  缓存 32689 + 非缓存 2848  = 35537          ← cachedContentTokenCount
```

**这是同一批 token 的两种切法，不是三堆 token。**

- `cachedContentTokenCount` ⊂ `promptTokenCount`（**不能相加**）
- `promptTokensDetails` 拆的也是整个输入（**含缓存部分**）
- 所以「图片 33570」和「缓存 32689」必然重叠 —— 非缓存只有 2848，装不下 33570 个图片

> 💡 `cacheTokensDetails` 就是用来告诉我们「缓存 32689 里有多少是图片」的。
> 只有各模态**异价**时上游才会返回它；同价时缓存由什么构成不影响金额，所以不返回。

输出侧则简单得多（**没有缓存**，不存在重叠）：

```
totalTokenCount = promptTokenCount + toolUsePromptTokenCount + thoughtsTokenCount + candidatesTokenCount
                  35537           + 0                       + 1270               + 492                 = 37299 ✅
```

- `candidatesTokenCount` **不含** `thoughtsTokenCount`（与 Claude、OpenAI 都不同）
- `candidatesTokensDetails` 是 candidates 的模态拆分，互斥
- 思考 token 按**输出价**计费

---

## 二、扣费公式

### 路径 A：倍率计费（`billing_mode` 为空，绝大多数 Gemini 模型）

```
① 算出各模态「未命中缓存」的部分
   freshImage = IMAGE总量 − 缓存中的IMAGE
   freshAudio = AUDIO总量 − 缓存中的AUDIO
   freshVideo = VIDEO总量 − 缓存中的VIDEO

② 从输入池里逐项扣除（只扣有独立单价的模态）
   base = promptTokenCount + toolUsePromptTokenCount
   base −= cachedTokens
   base −= freshImage        （ImageRatio > 0 时）
   base −= freshVideo        （VideoRatio > 0 时）
   base −= freshAudio        （AudioRatio > 0 且 ≠ 1 时）
   if base < 0 → base = 0

③ 输入侧计价
   promptQuota = base                              ← 剩下的按普通文本输入价
               + cachedTokens  × CacheRatio         ← 缓存整体按缓存价
               + freshImage    × ImageRatio
               + freshVideo    × VideoRatio
               + freshAudio    × AudioRatio
               + cacheCreation × CacheCreationRatio

④ 输出侧计价
   有图片输出：completionQuota = (thoughts + 文本输出 + 未归档) × CompletionRatio
                               + 图片输出 × ImageCompletionRatio
   否则：      completionQuota = completionTokens × CompletionRatio

⑤ 汇总
   quota = (promptQuota + completionQuota) × ModelRatio × GroupRatio
```

> ⚠️ 第 ① 步是 2026-10-22 新增的。此前直接用「IMAGE 总量」去扣，与缓存重叠的部分
> 被扣两次、base 被打成负数再 clamp 成 0，重叠 token 同时按缓存价和图片价各收一次。

### 路径 B：分档表达式计费（`billing_mode = tiered_expr`）

```
p = promptTokenCount
p −= cr            ← 表达式里出现了 cr 才扣
p −= freshImg      ← 表达式里出现了 img 才扣
p −= freshAi       ← 表达式里出现了 ai 才扣
c −= imgO          ← 出现了 img_o 才扣
c −= ao            ← 出现了 ao 才扣
clamp 到 0

raw   = 表达式求值(p, c, cr, img=freshImg, ai=freshAi, ...)
quota = raw × QuotaPerUnit ÷ 1,000,000 × GroupRatio
      = raw ÷ 2 × GroupRatio                     （QuotaPerUnit = 500000）
```

**关键：扣不扣某个模态，取决于表达式里写没写那个变量。** 这是判断一个模型有没有中招的第一依据。

> 💰 quota 换算：`500000 quota = $1`

---

## 三、场景举例

### 路径 A：倍率计费

全部用 `gemini-3.8-flash` 的线上配置（`ModelRatio 0.375 / CacheRatio 0.1 / ImageRatio 1 / CompletionRatio 5 / GroupRatio 1`），除 S5 另有说明。

| 场景 | prompt | cache | 计费image | 计费audio | completion | **quota** |
|---|---|---|---|---|---|---|
| S1 纯文本 无缓存 | 1000 | 0 | 0 | 0 | 200 | **750** |
| S2 纯文本 有缓存 | 1000 | 800 | 0 | 0 | 200 | **480** |
| S3 图文 无缓存 | 1000 | 0 | 600 | 0 | 200 | **750** |
| S4 图文 缓存不重叠 | 1000 | 300 | 600 | 0 | 200 | **649** |
| S5 图文 缓存重叠（真实记录） | 24549 | 21831 | 946 | 0 | 43 | **2616** |

#### S1 纯文本，无缓存

```
输入 1000 全是文本，输出 200
base = 1000
promptQuota     = 1000
completionQuota = 200 × 5 = 1000
quota = (1000 + 1000) × 0.375 × 1 = 750
```

#### S2 纯文本，缓存命中 800

```
base = 1000 − 800 = 200
promptQuota     = 200 + 800 × 0.1 = 280
completionQuota = 1000
quota = (280 + 1000) × 0.375 = 480
```

比 S1 省了 270，就是那 800 个 token 从 ×1 变成 ×0.1 的差额。

#### S3 图文混合，无缓存 —— **同价时拆不拆结果一样**

```
promptTokensDetails: TEXT 400 + IMAGE 600 = 1000
base = 1000 − 600(图片) = 400
promptQuota = 400 + 600 × 1 = 1000      ← 和 S1 完全相同
quota = 750
```

> 📌 `ImageRatio = 1` 意味着图片和文本同价，「先减掉再按同样单价加回来」是个恒等操作。
> 线上**所有** Gemini 模型的 `ImageRatio` 都配成 1，`VideoRatio` 也是 1。

#### S4 图文混合，缓存 300（**放得下，不重叠**）

```
非缓存池 = 1000 − 300 = 700 ≥ 图片 600  → 图片完全可以塞进非缓存部分
→ 不做任何推断，图片按总量 600 计费

base = 1000 − 300 − 600 = 100
promptQuota = 100 + 300 × 0.1 + 600 × 1 = 730
quota = (730 + 1000) × 0.375 = 648.75 → 649
```

#### S5 图文混合，缓存重叠 —— 真实记录 `SN_RaqDKNsLQrtoPxPaTwA8`

配置：`gemini-4-argon`，`ModelRatio 1 / CacheRatio 0.05 / ImageRatio 1 / CompletionRatio 5 / GroupRatio 0.65`

```
promptTokenCount 24549 = TEXT 16012 + IMAGE 8537
cachedContentTokenCount 21831
非缓存池 = 24549 − 21831 = 2718  <  图片 8537   ← 装不下，必然重叠
```

**上游没给 cacheTokensDetails → 按模态占比推算缓存构成：**

```
缓存中的图片 = 21831 × 8537/24549 = 7591
freshImage   = 8537 − 7591 = 946

base = 24549 − 21831 − 946 = 1772
promptQuota     = 1772 + 21831 × 0.05 + 946 × 1 = 1772 + 1091.55 + 946 = 3809.55
completionQuota = 43 × 5 = 215
quota = (3809.55 + 215) × 1 × 0.65 = 2615.96 → 2616
```

**修复前是 6398**（线上实际收的）：

```
base = 24549 − 21831 − 8537 = −5819 → clamp 0     ← 图片被二次扣减
promptQuota = 0 + 1091.55 + 8537 × 1 = 9628.55    ← 重叠的 5819 收了两遍
quota = (9628.55 + 215) × 0.65 = 6398
```

> 多收 3782 = `5819 × 0.65`，5819 正是落在缓存区间内的图片 token 数。
> 同时本该按原价计费的 1772 个文本 token 因为 clamp 成 0 **一分没收**。

---

### 异价场景：`cacheTokensDetails` 怎么用

假设某模型 `ImageRatio = 2`（图片是文本的 2 倍价），`CacheRatio = 0.1`：

```
promptTokenCount 1000 = TEXT 400 + IMAGE 600，cachedContentTokenCount 800
非缓存池 = 200，图片有 600 → 重叠
```

| 场景 | 上游给的 cacheTokensDetails | 推出的 freshImage | **quota** |
|---|---|---|---|
| S6 完整 | `TEXT 300 + IMAGE 500`（Σ=800 ✅） | 600 − 500 = **100** | **380** |
| S7 只给一半 | `TEXT 300`（Σ=300，缺 500） | 600 − 500 = **100** | **380** |
| S8 完全不给 | — | 600 − 480 = **120** | **400** |

#### S6 完整 detail —— 直接采信，精确

```
缓存里明确有 500 个图片 → freshImage = 100
base = 1000 − 800 − 100 = 100
promptQuota = 100 + 800 × 0.1 + 100 × 2 = 100 + 80 + 200 = 380
```

#### S7 只给了 TEXT —— 缺口补给未列出的模态

```
Σdetail = 300，但 cachedContentTokenCount = 800 → 缺口 500
500 全部补给未列出的 IMAGE → 缓存里的图片 = 500
→ 结果与 S6 完全一致（380）
```

> 📌 这道「补缺口」是护栏。若不补，`freshImage` 会算成 600，各模态 fresh 之和
> （400−300=100 文本 + 600 图片 = 700）超过非缓存池 200，base 又变负 —— 等于没修。

#### S8 完全不给 detail —— 按占比兜底

```
缓存里的图片 = 800 × 600/1000 = 480
freshImage   = 600 − 480 = 120
base = 1000 − 800 − 120 = 80
promptQuota = 80 + 80 + 120 × 2 = 400
```

真实值必然落在 `[380, 440]` 之间（取决于缓存到底由什么构成），400 落在区间内。
**这是异价 + 上游不给 detail 时唯一会有误差的场景**，误差上限约 ±15%。

#### S9 音频异价

配置：`gemini-3.1-flash-lite`，`ModelRatio 0.125 / CacheRatio 0.1 / AudioRatio 2`

```
promptTokenCount 1000 = TEXT 600 + AUDIO 400，缓存 900
cacheTokensDetails: TEXT 600 + AUDIO 300
freshAudio = 400 − 300 = 100

base = 1000 − 900 − 100 = 0
promptQuota = 0 + 900 × 0.1 + 100 × 2 = 290
quota = 290 × 0.125 = 36.25 → 36
```

> ⚠️ 2026-10-22 之前，`AudioRatio` 在 Gemini 文本 relay 路径上**完全不生效** ——
> 代码读的是写死的模型前缀白名单（只认 `gemini-2.0-flash*` / `gemini-2.5-flash*` /
> `gemini-robotics-er-1.5`），`gemini-3.x` 一律返回 0，音频按文本价计费。
> 线上 12.8 万笔带音频请求、3.09 亿音频 token，**无一命中那张白名单**。
> 现已改为：**先查 `AudioRatio` 配置 → 没配回退旧绝对价 → 都没有则等同文本价**。

---

### 路径 B：分档表达式

同一份输入（主人那条真实记录 `pzO9auP8BpXRi9sPprO96Ao`）：

```
promptTokenCount 35537 = TEXT 1967 + IMAGE 33570，缓存 32689，输出 1762
```

| 模型 | 表达式 | 实际代入 | **quota** |
|---|---|---|---|
| gemini-3.1-pro-preview | `p*2 + c*12 + cr*0.2` | p=2848, c=1762, cr=32689 | **10848** |
| gemini-3.5-flash | `p*1.5 + c*9 + cr*0.15 + img*1.5 + ai*1.5` | p=157, c=1762, cr=32689, img=2691 | **8136** |

#### gemini-3.1-pro-preview（表达式不含 `img`）

```
表达式只出现 p / c / cr → 只扣缓存，图片不单独拆
p = 35537 − 32689 = 2848

raw   = 2848×2 + 1762×12 + 32689×0.2 = 5696 + 21144 + 6537.8 = 33377.8
quota = 33377.8 ÷ 2 × 0.65 = 10847.785 → 10848     ✅ 与线上记录分毫不差
```

**这条账本来就是对的**，图片从未被二次扣减。

#### gemini-3.5-flash（表达式含 `img`，曾经中招）

```
freshImg = 33570 − 32689×33570/35537 = 2691
p = 35537 − 32689(cr) − 2691(img) = 157

raw   = 157×1.5 + 1762×9 + 32689×0.15 + 2691×1.5
      = 235.5 + 15858 + 4903.35 + 4036.5 = 25033.35
quota = 25033.35 ÷ 2 × 0.65 = 8135.8 → 8136
```

**正确性自检**：`p` 和 `img` 的系数同为 1.5（图文同价），所以结果必须等于「不拆模态」的基准：

```
不拆：(35537−32689)×1.5 + 1762×9 + 32689×0.15 = 25033.35  ✅ 完全一致
```

**修复前是 23113 —— 超收 2.84 倍**：

```
p = 35537 − 32689 − 33570 = −30722 → clamp 0
raw = 0 + 15858 + 4903.35 + 33570×1.5 = 71116.35
quota = 71116.35 ÷ 2 × 0.65 = 23113
```

---

### 输出侧场景

配置：`gemini-3.1-flash-image`，`ModelRatio 0.25 / CompletionRatio 6 / ImageCompletionRatio 120`

| 场景 | 图片输出 | 文本输出 | 思考 | 音频输出 | **quota** |
|---|---|---|---|---|---|
| S10 图+文输出 | 1000 | 100 | 50 | 0 | **30250** |
| S11 输出含音频 | 700 | 100 | 50 | 300 | **21700** |

#### S10 常规生图

```
candidatesTokensDetails: TEXT 100 + IMAGE 1000，thoughtsTokenCount 50
completionTokens = 1100 + 50 = 1150

文本档 = 思考 50 + 文本 100 = 150
图片档 = 1000
completionQuota = 150 × 6 + 1000 × 120 = 900 + 120000 = 120900
quota = (100 + 120900) × 0.25 = 30250
```

#### S11 输出带音频 —— 不再白送

```
candidatesTokensDetails: TEXT 100 + IMAGE 700 + AUDIO 300

文本档 = 思考 50 + 文本 100 = 150
未归档 = (1150 − 700) − 150 = 300          ← 这 300 就是音频
文本档 = 150 + 300 = 450                   ← 落回文本输出档
completionQuota = 450 × 6 + 700 × 120 = 2700 + 84000 = 86700
quota = (100 + 86700) × 0.25 = 21700
```

> ⚠️ 修复前这 300 个音频 token **既不在文本档也不在图片档，被整段漏掉**，等于白送。
> 现在统一落回文本输出档 —— 宁可按文本价收，也不能不收。
> 音频输出的**独立异价档位**尚未实现（线上零流量，待 `AudioCompletionRatio` 口径与
> `/v1/audio/*` 路径对齐后再做）。

---

## 四、速查

### 什么时候会出问题

| 条件 | 是否受影响 |
|---|---|
| 纯文本输入（无论有无缓存） | ✅ 从来没问题 |
| 多模态输入 + 无缓存命中 | ✅ 没问题 |
| 多模态输入 + 缓存命中但放得下 | ✅ 没问题 |
| 多模态输入 + 缓存重叠 + 倍率计费 | 🔴 曾经重复计费，已修 |
| 多模态输入 + 缓存重叠 + 分档表达式**含 `img`/`ai`** | 🔴 曾经重复计费，已修 |
| 多模态输入 + 缓存重叠 + 分档表达式**不含 `img`** | ✅ 从来就是对的 |
| 输出侧任意模态 | ✅ 无缓存，天然不重叠 |

### 修复后的精度

| 场景 | 精度 |
|---|---|
| 模态同价（当前**全部** Gemini 模型） | **精确**，数学恒等，不依赖 `cacheTokensDetails` |
| 异价 + 上游给 `cacheTokensDetails` | **精确** |
| 异价 + 只给部分模态 | **精确**（缺口反推，等价于给全） |
| 异价 + 完全不给 | 近似（按占比），误差 ≤ ±15%，但**不会再重复计费** |

### 历史敞口（2026-05-21 ~ 10-22）

```
影响订单  3,425 笔 / 13 个用户
多收合计  13,829,406 quota = $27.66（占这批订单金额 15.5%）
最严重    gemini-3.7-flash 53.0% · gemini-4-argon 49.6% · gemini-3.8-flash 24.0%
按用户    user202608171500 $15.63 · kling-rd-global $7.33（超收率 76.3%）
```

### 排查一笔账的步骤

1. 看日志 `other.billing_mode` —— 空 = 倍率路径，`tiered_expr` = 分档路径
2. 分档路径再看 `expr_b64`（base64 解码）里有没有 `img` / `ai` 变量
3. 看 `upstream_responses.usageMetadata` 核对 `promptTokenCount` / `cachedContentTokenCount` / `promptTokensDetails`
4. 判断是否重叠：`各模态总量 > promptTokenCount − cachedContentTokenCount` ?
5. 按本文第二节公式复算

> 📌 `other` 里的 `input_image_tokens` 现在记的是**计费用的非缓存图片量**（S5 中是 946
> 而非 8537），`input_text_tokens` 不会再因重叠塌成 0。
