# GPT Image 2.6 接入说明

> 更新日期：2026-11-05（OpenAI 于 2026-11-04 发布 GPT Image 2.6）
> 适用渠道：OpenAI 渠道类型、Azure 渠道类型
> 网关原则：新参数**全部原样透传**给上游，网关不做取值校验，取值合法性由上游判断

---

## 一、模型概览

| 模型 ID | 定位 | 支持端点 |
|---|---|---|
| `gpt-image-2.6-flare` | 速度优先，低质量档最快约 6 秒出图，适合海报、商品图、快速出变体 | `/v1/images/generations`、`/v1/images/edits` |
| `gpt-image-2.6-sunburst` | 细节优先，在 medium/high 质量下多轮编辑一致性更好 | `/v1/images/generations`、`/v1/images/edits` |

和 2.5 相比，2.6 新增四项能力，两个模型都支持：

1. **complexity**：控制模型出图前的“规划量”，与 `quality` 相互独立
2. **markup 圈注编辑**：额外传一张透明 PNG 圈注层，标出要改的区域（2.5 模型也支持）
3. **SVG 输出**：`output_format: "svg"`，只能用于生成接口
4. **GIF 动图输出**：`output_format: "gif"`，配合 `frame_count`、`frame_duration_ms` 使用

不支持的端点：Batch、Responses、Chat Completions。Batch 不支持 2.6，只能用 2.5 或 `gpt-image-2`。

### 官方链接

| 内容 | 链接 |
|---|---|
| 发布公告 | https://openai.com/index/introducing-images-2-6/ |
| 生图指南（含 2.6 章节） | https://developers.openai.com/api/docs/guides/image-generation#gpt-image-26 |
| 生成接口参考 | https://developers.openai.com/api/reference/resources/images/methods/generate |
| 编辑接口参考 | https://developers.openai.com/api/reference/resources/images/methods/edit |
| Flare 模型页 | https://developers.openai.com/api/docs/models/gpt-image-2.6-flare |
| Sunburst 模型页 | https://developers.openai.com/api/docs/models/gpt-image-2.6-sunburst |
| 价格页 | https://developers.openai.com/api/docs/pricing |
| Azure 生图文档 | https://learn.microsoft.com/en-us/azure/foundry/openai/how-to/dall-e |

---

## 二、价格（OpenAI 官方，按 token 计费）

两个 2.6 模型价格相同，也和 2.5 相同：

| 类型 | 价格（$/1M tokens） |
|---|---|
| 文本输入 | $5.00 |
| 文本输入（缓存命中） | $1.25 |
| 图片输入 | $8.00 |
| 图片输入（缓存命中） | $2.00 |
| 图片输出 | $30.00 |

- `complexity` 不另外收费（官方原话：*Adjusting complexity has no separate fee*）。
- markup 圈注层按普通图片输入 token 计费。
- GIF 的多帧消耗体现在输出 token 里，帧越多输出 token 越多。
- **SVG 按两部分计费**：先按图片模型费率渲染一张参考图（计入 `usage`），再按“SVG 编写模型”的费率收编写 token（计入 `svg_usage`），平台两部分都会收，详见第六节。
- 第三方实测发现：low/medium 质量下，2.6 的输出 token 是 2.5 的 3～4 倍（Flare low：824 对 196），high 及以上两者一致。单价虽然相同，低档位单张实际更贵。

### 平台配置（options 表）

2.6 仍按 token 计费，和 `gpt-image-2` 用同一套五个倍率，计费公式见 [gpt-image-config.md](./gpt-image-config.md)。代码里的默认倍率**只预设了 `gpt-image-1` 和 `gpt-image-2`**，2.6 的两个模型需要在 **系统设置 > 运营设置** 里手动加上，每个模型名各加一条：

| 配置项 | `gpt-image-2.6-flare` / `gpt-image-2.6-sunburst` | 说明 |
|---|---|---|
| 模型倍率 ModelRatio | `2.5` | $5 文本输入 |
| 补全倍率 CompletionRatio | `2` | 2.6 没有文本输出，只是占位 |
| 图像输入倍率 ImageRatio | `1.6` | $8 / $5 |
| 图像输出倍率 ImageCompletionRatio | `6` | $30 / $5 |
| 缓存倍率 CacheRatio | `0.25` | $1.25 / $5 |

---

## 三、参数说明

本节标注依据：“官方参考”指 API Reference 原文；“官方指南”指生图指南；“第三方实测”是发布当晚外部测试的结论，仅供参考。

### 3.1 2.6 新增参数

| 参数 | 类型 | 适用端点 | 取值 / 默认值 | 说明 |
|---|---|---|---|---|
| `complexity` | string | 生成、编辑 | `low` / `medium` / `high`，不传时为 `high` | 出图前的规划量。low 适合换背景、改招牌这类局部改动；high 适合密集文字、复杂布局。调高会增加延迟，也可能引入不想要的改动 |
| `frame_count` | integer | 生成（GIF） | 默认 9 | GIF 帧数。官方参考写的是 2～16；官方指南被引用为 2～64；第三方实测超过 16 返回 400。**建议按 16 以内使用** |
| `frame_duration_ms` | integer | 生成（GIF） | 默认 200，最小 20 | 每帧时长，四舍五入到 10ms。指南被引用为 20～2000 |
| `svg` | object | 生成（SVG） | 例：`{"model":"<编写模型>","reasoning":{"effort":"low"}}` | 指定编写 SVG 的模型。第三方实测：不传 `svg.model` 会报错 |
| `markup` | object | 编辑 | `{"image_url": "https://... 或 data:image/png;base64,..."}` 或 `{"file_id": "..."}` | 叠加在**第一张**输入图上的透明 PNG 圈注层，尺寸必须和第一张图（旋正后）一致，且要包含透明像素。不能和 `mask` 同时使用 |
| `safety_identifier` | string | 生成、编辑 | 最长 128 字符 | 终端用户的稳定标识（建议传哈希值），用于安全监控。用来替代已废弃的 `user`，两者都传时以非空的 `safety_identifier` 为准 |
| `prompt_rewriter` | — | 生成、编辑 | — | 指南里提到它不能和 markup、SVG 一起用，但参考文档没有列出具体结构。网关原样透传 |

### 3.2 取值有变化的已有参数

| 参数 | 2.6 的变化 |
|---|---|
| `output_format` | 新增 `gif`、`svg`。官方原有：`png` / `jpeg` / `webp` |
| `quality` | `low` / `medium` / `high` / `xhigh` / `max` / `auto` |
| `size` | `auto` 或自定义 `宽x高`，两边都必须是 16 的倍数。GIF 所有帧共用像素预算，帧数越多，单帧尺寸上限越小 |
| `n` | SVG 只能为 1 |

### 3.3 能力限制（官方指南）

| 能力 | 生成接口 | 编辑接口 | 流式 / partial_images | 其它限制 |
|---|---|---|---|---|
| GIF | ✅ | 第三方说只能用生成接口（参考文档的编辑接口里也列了 frame_count） | ❌ | 不支持 mask，不支持自定义 output_compression |
| SVG | ✅ | ❌ | ❌ | n=1，不支持 output_compression 和 prompt_rewriter，不能传参考图。输出不含脚本、外部资源和内嵌位图 |
| markup | — | ✅ | — | 不能和 mask、prompt_rewriter 同时使用。单图最大 16,777,216 像素，圈注层文件最大 50MB |

### 3.4 其它常用参数（与 2.5 相同）

`model`、`prompt`、`background`（transparent/opaque/auto）、`moderation`（low/auto）、`output_compression`、`partial_images`（0～3）、`stream`、`response_format`、`user`（已废弃）、`images`（编辑接口，`[{image_url}|{file_id}]`）、`mask`、`input_fidelity`（gpt-image-2 及以后版本不要传）。

---

## 四、OpenAI 官方与 Azure 的差异

| 项目 | OpenAI 官方 | Azure OpenAI |
|---|---|---|
| 2.6 是否上线 | ✅ 2026-11-04 上线 | ❌ 截至 2026-11-05，Azure 文档的模型表只到 `GPT-Image-2.5-Sunburst/Flare`，还没有 2.6 |
| 编辑接口请求格式 | JSON（`images: [{image_url}]`）和 multipart 都支持 | **只稳定支持 multipart**：JSON 模式不接受字符串形式的 images |
| markup / complexity / GIF / SVG | 有文档 | Azure 文档还没写，需等 2.6 上线后实测 |
| quality 档位 | 包含 xhigh、max | 2.5 已支持 xhigh、max |
| 自定义尺寸 | 支持 | 2.5 支持：16 的倍数、宽高比 1:3～3:1、单边 ≤3840、总像素 655,360～8,294,400 |
| 计费 | OpenAI 账单 | 走 Azure 账单（价格以 Azure 定价页为准） |

---

## 五、网关处理方式

### 5.1 生成接口 `/v1/images/generations`（JSON）

请求体按字段原样转发给上游，上面列出的所有参数都会透传，包括 null、超范围值和类型不对的值。

> ⚠️ 已知限制：`ImageRequest` 的序列化**不会带上未在 DTO 中定义的字段**（`MarshalJSON` 里刻意没有合并 Extra）。以后 OpenAI 再加新参数，需要先把字段加进 `relaykit/dto/openai_image.go`，才能转发到上游。

### 5.2 编辑接口 `/v1/images/edits`

自 2026-06-25 起，gpt-image 系列的 JSON 编辑请求都会**先转成 multipart 再发给上游**。原因是 Azure 的 JSON 模式不可用，而 OpenAI 和第三方上游都支持 multipart。

| 客户端传入 | 网关处理 |
|---|---|
| JSON 中的 `images` / `mask` / `markup` 是 `image_url`（HTTPS） | **网关先下载**，再作为文件写入 multipart |
| JSON 中的 `images` / `mask` / `markup` 是 base64 data URL | 网关解码后作为文件写入 multipart |
| JSON 中的 `file_id` 引用 | 返回 400：网关拿不到 OpenAI File API 里的文件 |
| JSON 中的其余参数（complexity、frame_count、svg、safety_identifier 等） | 原样写成表单字段 |
| 原生 multipart 请求 | markup 文件和其它表单字段原样转发 |

关于 markup 为什么还要由网关下载：OpenAI 官方 JSON 模式本身支持 markup 的 HTTPS URL，但网关统一转成了 multipart，而 multipart 里 markup 只能是文件，所以必须先下载。markup 下载会走 SSRF 防护客户端，只允许 HTTPS（重定向也不能降级到 HTTP），文件上限 50MB，超时 30 秒。**网关不检查内容是否为 PNG、尺寸是否匹配**，这些交给上游判断。

---

## 六、计费注意事项

1. **普通生图、编辑、GIF、markup**：仍然按上游返回的 `usage` 计费，只要第二节的 options 配好就行，**没有新的计费逻辑**。
2. **SVG 编写费用（`svg_usage`）**：SVG 响应除了 `usage`（参考图渲染）之外，还会返回一个独立的 `svg_usage`：
   ```json
   "svg_usage": {"model": "<编写模型>", "input_tokens": 300, "output_tokens": 200, "reasoning_tokens": 150, "total_tokens": 650}
   ```
   这部分按**编写模型自身的费率**另外计费（OpenAI 原话：*the authoring model's rate*）。平台的处理方式：
   - 计费公式：`(input_tokens + 输出 token × 编写模型 CompletionRatio) × 编写模型 ModelRatio × 分组倍率`，结果叠加在图片费用之上。倍率计费、按次计费、表达式计费三条路径都会加上。
   - 输出 token 取 `output_tokens` 和 `total_tokens - input_tokens` 中较大的一个：如果推理 token 没算进 output_tokens、只体现在 total_tokens 里，也会按输出计费。
   - 编写模型的倍率读 options 表里**该模型自己的**模型倍率和补全倍率，**必须提前配好**。没配的话这部分不计费，后台会报错日志，消费日志的管理员字段里会记 `svg_usage_unpriced_model`。
   - 消费日志的 other 里会写入 `svg_usage`：`model`、`input_tokens`、`output_tokens`、`model_ratio`、`completion_ratio`、`group_ratio`、`quota_before_group`（分组倍率之前的额度）、`quota`（实际扣费）。
   - 展示与导出：Java 计费过程（MaaS 消费记录、企业日志、管理端账单日志，以及全部导出的“计费过程”列）新增“SVG编写输入/输出(模型)”两项，公式加起来等于总额；Go 自带管理界面（classic）和管理端的 `log/index.vue` 也新增了 SVG 编写那一行，合计包含这笔费用。日账单和供应商账单把这部分归入文本输入/输出。
3. 2.6 没有 Batch 价格（官方不支持 Batch）。

---

## 七、调用示例（Gravitex 平台）

以下示例中，`$GRAVITEX_API_KEY` 替换为平台令牌。

### 7.1 文生图（加 complexity）

```bash
curl https://api.gravitex.ai/v1/images/generations \
  -H "Authorization: Bearer $GRAVITEX_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-image-2.6-flare",
    "prompt": "Flat pictogram of a stopwatch, teal and amber, white background",
    "size": "1024x1024",
    "quality": "low",
    "complexity": "low",
    "safety_identifier": "hashed-user-id"
  }'
```

### 7.2 生成 GIF 动图

```bash
curl https://api.gravitex.ai/v1/images/generations \
  -H "Authorization: Bearer $GRAVITEX_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-image-2.6-flare",
    "prompt": "A fox cub mascot waving, looping animation, flat colors",
    "output_format": "gif",
    "frame_count": 12,
    "frame_duration_ms": 120,
    "size": "1024x1024",
    "quality": "medium"
  }'
```

### 7.3 生成 SVG（编写模型的倍率需提前配置，见第六节）

```bash
curl https://api.gravitex.ai/v1/images/generations \
  -H "Authorization: Bearer $GRAVITEX_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-image-2.6-sunburst",
    "prompt": "Stopwatch icon with clean paths, teal outline, amber lightning bolt",
    "output_format": "svg",
    "n": 1,
    "svg": {"model": "<编写模型>", "reasoning": {"effort": "low"}}
  }'
```

### 7.4 markup 圈注编辑：JSON 方式

```bash
curl https://api.gravitex.ai/v1/images/edits \
  -H "Authorization: Bearer $GRAVITEX_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-image-2.6-sunburst",
    "prompt": "Change the circled vase to deep blue. Keep everything else the same.",
    "images": [{"image_url": "https://example.com/kitchen.png"}],
    "markup": {"image_url": "https://example.com/kitchen-markup.png"},
    "complexity": "low"
  }'
```

`image_url` 也可以是 `data:image/png;base64,...`。

### 7.5 markup 圈注编辑：multipart 方式

```bash
curl https://api.gravitex.ai/v1/images/edits \
  -H "Authorization: Bearer $GRAVITEX_API_KEY" \
  -F model=gpt-image-2.6-sunburst \
  -F "prompt=Change the circled vase to deep blue. Keep everything else the same." \
  -F complexity=low \
  -F "image[]=@kitchen.png" \
  -F "markup=@kitchen-markup.png"
```

### 7.6 OpenAI 官方直连（对照用）

```bash
curl https://api.openai.com/v1/images/generations \
  -H "Authorization: Bearer $OPENAI_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model":"gpt-image-2.6-flare","prompt":"a cyberpunk cat","complexity":"medium"}'
```

---

## 八、已知坑（发布初期）

1. 官方指南里的示例代码用的还是 2.5 的模型名，照抄时要改成 2.6。
2. 帧数上限有冲突：官方参考写 16，指南写 64，**按 16 设计**。
3. SVG 不带 `svg.model` 会报错；发布当晚第三方测试中 15 次 SVG 请求有 14 次返回 502，要做好重试。
4. markup 和 mask 互斥；圈注层尺寸必须和第一张输入图完全一致。
5. Azure 暂时没有 2.6，Azure 渠道配置 2.6 模型会请求失败。
