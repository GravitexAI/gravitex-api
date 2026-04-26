# GPT-Image-2 计费配置说明

## 概述

GPT-Image-2 采用 **token 计费**模式，基于上游 Azure OpenAI 返回的 usage tokens 计算费用。与文本模型不同，GPT-Image-2 的 token 区分**文本输入**、**图片输入**和**图片输出**三种类型，每种类型的单价不同。

---

## 上游定价

| 类型 | 官方价格 ($/1M tokens) |
|------|----------------------|
| Text Input Tokens | $5.00 |
| Image Input Tokens | $8.00 |
| Cached Text Input Tokens | $1.25 |
| Cached Image Input Tokens | $2.00 |
| Image Output Tokens | $30.00 |

> **说明**: GPT-Image-2 无文本输出，`output_tokens` 全部为图片输出 token。

---

## 倍率配置

GPT-Image-2 涉及 5 个倍率维度，均可在管理面板 **系统设置 > 运营设置** 中配置。

### 1. 模型倍率 (ModelRatio)

文本输入 token 的基础倍率，作为其它倍率的基准。

| 模型 | 倍率 | 对应价格 |
|------|------|---------|
| `gpt-image-2` | `2.5` | $5.00 / 1M tokens |

### 2. 补全倍率 (CompletionRatio)

文本输出 token 相对于文本输入 token 的倍率（GPT-Image-2 实际无文本输出，仅作保留配置）。

| 模型 | 倍率 | 说明 |
|------|------|------|
| `gpt-image-2` | `2` | $10 / $5 = 2x |

### 3. 图像输入倍率 (ImageRatio)

图像输入 token 相对于文本输入 token 的倍率。

| 模型 | 倍率 | 说明 |
|------|------|------|
| `gpt-image-2` | `1.6` | $8 / $5 = 1.6x |

### 4. 图像输出倍率 (ImageCompletionRatio)

图像输出 token 相对于文本输入 token 的倍率。**这是 GPT-Image-2 最关键的倍率**，因为 `output_tokens` 全部为图片输出。

| 模型 | 倍率 | 说明 |
|------|------|------|
| `gpt-image-2` | `6` | $30 / $5 = 6x |

### 5. 缓存倍率 (CacheRatio)

缓存命中时输入 token 的折扣倍率。

| 模型 | 倍率 | 说明 |
|------|------|------|
| `gpt-image-2` | `0.25` | $1.25 / $5 = 0.25x |

---

## 计费公式

系统基准单价 = `$2 / 1M tokens`（即 `0.002 / 1K tokens`）

### 文生图（无图片输入）

```
文本输入费用 = text_tokens × ModelRatio × 分组倍率 × 基准单价
图片输出费用 = output_tokens × ModelRatio × ImageCompletionRatio × 分组倍率 × 基准单价
总费用 = 文本输入费用 + 图片输出费用
```

### 图生图（含图片输入）

```
文本输入费用 = text_tokens × ModelRatio × 分组倍率 × 基准单价
图片输入费用 = image_tokens × ModelRatio × ImageRatio × 分组倍率 × 基准单价
图片输出费用 = output_tokens × ModelRatio × ImageCompletionRatio × 分组倍率 × 基准单价
总费用 = 文本输入费用 + 图片输入费用 + 图片输出费用
```

### 示例计算

以文生图为例，假设上游返回：
- `text_tokens` = 7, `image_tokens` = 0, `output_tokens` = 1415
- 分组倍率 = 1

```
文本输入费用 = 7 × 2.5 × 1 × (0.002/1000) = 0.000035
图片输出费用 = 1415 × 2.5 × 6 × 1 × (0.002/1000) = 0.04245
总费用 = 0.042485（约 $0.0425）
```

---

## 管理面板配置方式

在管理面板 **系统设置 > 运营设置** 中找到对应的 JSON 配置项，添加或修改倍率值。

**模型倍率** 配置框中添加：
```json
"gpt-image-2": 2.5
```

**补全倍率** 配置框中添加：
```json
"gpt-image-2": 2
```

**图像输入倍率** 配置框中添加：
```json
"gpt-image-2": 1.6
```

**图像输出倍率** 配置框中添加：
```json
"gpt-image-2": 6
```

**缓存倍率** 配置框中添加：
```json
"gpt-image-2": 0.25
```

> **注意**: 倍率已在代码中预设，如果使用默认配置无需在管理面板额外配置。仅当需要自定义定价（如加价销售）时才需要在面板中覆盖。

---

## 日志记录

每次请求完成后，日志中会记录以下 token 明细：

| 日志字段 | 来源 | 说明 |
|---------|------|------|
| `prompt_tokens` | `input_tokens` | 输入 token 总数 |
| `completion_tokens` | `output_tokens` | 输出 token 总数（图片输出） |
| `image_input_tokens` | `input_tokens_details.image_tokens` | 图片输入 token 数 |
| `image_output_tokens` | `output_tokens`（gpt-image 模型自动填充） | 图片输出 token 数 |

> 系统会自动将 gpt-image 模型的 `output_tokens` 识别为图片输出，无需额外配置。
