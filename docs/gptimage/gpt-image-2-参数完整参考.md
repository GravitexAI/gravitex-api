# GPT-Image 系列（含 gpt-image-2）参数完整参考

> 本文档基于 OpenAI 官方 API Reference（2026-05 抓取版本）+ Azure OpenAI / Microsoft Foundry 官方文档 + 实测报错交叉核实整理。
> **重点标注了 `gpt-image-2` 与旧版本（`gpt-image-1` / `gpt-image-1.5` / `gpt-image-1-mini`）之间参数支持的差异**，这些差异正是导致 400 报错的主要来源。
> 涉及"官方文档表述"与"Azure 实测行为"存在冲突的地方，本文档**都会明确标注两边的说法**，不做主观合并，避免误导。

---

## 一、涉及的端点

| 端点 | 方法 | 作用 | 支持的模型 |
|---|---|---|---|
| `/v1/images/generations` | POST | 文生图 | `dall-e-2`、`dall-e-3`、`gpt-image-1`、`gpt-image-1-mini`、`gpt-image-1.5`、`gpt-image-2`、`gpt-image-2-2026-04-21` |
| `/v1/images/edits` | POST | 图生图 / 编辑（inpainting、局部重绘、多图融合） | `dall-e-2`、`gpt-image-1`、`gpt-image-1-mini`、`gpt-image-1.5`、`gpt-image-2`、`gpt-image-2-2026-04-21`、`chatgpt-image-latest` |

**Azure OpenAI 实际请求 URL 形式**（与 OpenAI 官方 API 的 JSON body 字段完全一致，区别只是 URL 结构和鉴权头）：

```
# 经典部署名路由（最常见）
{endpoint}/openai/deployments/{deployment-name}/images/generations?api-version={api-version}
{endpoint}/openai/deployments/{deployment-name}/images/edits?api-version={api-version}

# 新版统一 v1 路由（部分区域/资源已支持，model 字段放在 body 里）
{endpoint}/openai/v1/images/generations
{endpoint}/openai/v1/images/edits
```

鉴权头用 `api-key: <AZURE_KEY>`（而不是 OpenAI 官方的 `Authorization: Bearer`）。`api-version` 请以你 Azure 资源当前支持的版本为准（常见如 `2025-04-01-preview`），gpt-image-2 GA 后的具体 api-version 建议直接查 Azure 门户里该部署的"支持的 API 版本"列表，不要死记某个固定值。

---

## 二、模型能力速查表

| 能力 / 参数 | gpt-image-1 | gpt-image-1-mini | gpt-image-1.5 | **gpt-image-2** |
|---|---|---|---|---|
| `input_fidelity`（编辑接口） | ✅ 支持 | ❌ 不支持 | ⚠️ 见下方专题说明 | ⚠️ 见下方专题说明 |
| `background=transparent`（透明背景） | ✅ 支持 | ✅ 支持 | ✅ 支持 | ❌ **不支持，官方文档明确写死会报错** |
| `size` 自由分辨率 | ❌ 仅 3 个固定尺寸 | ❌ 仅 3 个固定尺寸 | ❌ 仅 3 个固定尺寸 | ✅ 支持任意 `宽x高`，最高 4K |
| 人脸保留（face preservation） | 一般 | 无专门优化 | 增强 | 增强（官方称"advanced face preservation"） |
| 编辑能力（inpainting/多图融合） | 支持 | 支持 | 支持 | 支持，官方称编辑效果比 1.x 系列更好 |

---

## 三、`/images/generations`（文生图）完整参数表

| 参数 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `prompt` | string，必填 | - | 图片描述文本。GPT image 系列最长 32000 字符（`dall-e-2` 1000 字符，`dall-e-3` 4000 字符） |
| `model` | string | `dall-e-2`（未指定任何 GPT image 专属参数时的兜底值） | 使用的模型名。可选 `dall-e-2`、`dall-e-3`、`gpt-image-1`、`gpt-image-1-mini`、`gpt-image-1.5`、`gpt-image-2`、`gpt-image-2-2026-04-21` |
| `n` | number，1-10 | 1 | 生成图片数量。`dall-e-3` 只能填 1 |
| `size` | string | `auto` | 见下方【size 详解】 |
| `quality` | string | `auto` | 见下方【quality 详解】 |
| `background` | string | `auto` | 见下方【background 详解，⚠️ gpt-image-2 有硬性限制】 |
| `output_format` | `png` / `jpeg` / `webp` | `png` | 返回图片的编码格式。仅 GPT image 系列支持这个参数 |
| `output_compression` | number，0-100 | 100 | 压缩率，只在 `output_format` 为 `jpeg` 或 `webp` 时生效。数值越小压缩越强、文件越小、细节损失越多 |
| `moderation` | `low` / `auto` | `auto` | 见下方【moderation 详解】 |
| `stream` | boolean | `false` | 是否流式返回生成过程。仅 GPT image 系列支持 |
| `partial_images` | number，0-3 | - | 配合 `stream=true` 使用，指定流式返回几张"生成中"的过渡图。`0` 表示只在最后一次事件返回完整图 |
| `response_format` | `url` / `b64_json` | `url`（仅对 dall-e 生效） | **GPT image 系列完全不支持这个参数**，始终返回 base64（`b64_json`）。只对 `dall-e-2`/`dall-e-3` 有效，且 URL 只在生成后 60 分钟内有效 |
| `style` | `vivid` / `natural` | - | **只对 `dall-e-3` 生效**，GPT image 系列不支持 |
| `user` | string | - | 终端用户唯一标识，用于滥用检测，非必需 |

### size 详解

| 取值 | 适用模型 | 说明 |
|---|---|---|
| `1024x1024` | 全部 GPT image 系列 | 正方形，标准尺寸 |
| `1536x1024` | 全部 GPT image 系列 | 横版（landscape） |
| `1024x1536` | 全部 GPT image 系列 | 竖版（portrait） |
| `auto` | 支持自动定尺寸的模型 | 由模型自行决定最合适的尺寸 |
| **任意 `宽x高`**（如 `1536x864`） | **仅 `gpt-image-2` / `gpt-image-2-2026-04-21`** | 宽、高必须都是 **16 的倍数**；宽高比必须落在 **1:3 ~ 3:1** 之间；超过 `2560x1440` 属于实验性支持；官方给出的**最大支持分辨率是 `3840x2160`（4K）**；同时还要满足模型当前的像素总量与边长限制（旧版文档给出的经验值是总像素 655,360 ~ 8,294,400，供参考） |
| `256x256` / `512x512` | 仅 `dall-e-2` | — |
| `1792x1024` / `1024x1792` | 仅 `dall-e-3` | — |

### quality 详解

| 取值 | 说明 |
|---|---|
| `low` | 低质量，生成速度最快，token/费用消耗最低。gpt-image-2 官方说明中这个档位专门为"低延迟场景"做过优化 |
| `medium` | 中等质量，速度与效果的折中 |
| `high` | 高质量，细节最丰富，但耗时和 token 消耗最高 |
| `auto` | 由模型自动挑选最合适的质量档位（默认值） |
| `standard` / `hd` | **只对 `dall-e-3` 有效**，`standard` 是 `dall-e-2` 唯一可用档位 |

### background 详解 ⚠️ 这是你需要重点关注的参数

| 取值 | 说明 |
|---|---|
| `opaque` | 不透明背景，普通照片/插画的标准背景 |
| `transparent` | 透明背景（PNG/WebP 的 alpha 通道）。**要求 `output_format` 必须是 `png` 或 `webp`**（`jpeg` 不支持透明通道） |
| `auto` | 由模型自动判断该用哪种背景（默认值） |

**⚠️ 关键限制（官方文档原文，2026-05 更新）：**

> "`gpt-image-2` and `gpt-image-2-2026-04-21` do not support transparent backgrounds. Requests with `background` set to `transparent` will return an error for these models; use `opaque` or `auto` instead."
>
> 中文翻译：**`gpt-image-2` 和 `gpt-image-2-2026-04-21` 不支持透明背景。将 `background` 设为 `transparent` 会对这两个模型直接返回报错**，请改用 `opaque` 或 `auto`。

这与 `gpt-image-1` / `gpt-image-1-mini` / `gpt-image-1.5` 形成明显反差——它们都能原生输出真正带 alpha 通道的透明 PNG，`gpt-image-2` 反而**退化掉了这个能力**。业界普遍猜测原因是 `gpt-image-2` 训练目标转向"补全真实场景"而非"抠图"，导致底层没有 alpha 通道的监督信号，这是产品层面的取舍而非临时故障。

如果你的业务场景是电商抠图、贴纸、Logo 等**必须要透明背景**的需求，`gpt-image-2` 目前走不通，可选方案：

1. **换用 `gpt-image-1.5` 或更早版本**：改一个 `model` 字段即可直接拿到原生透明 PNG，成本最低
2. **`gpt-image-2` 生成 + 后处理抠图**：先用 `background=opaque` 生成实底图，再接一个抠图模型（如 RemBG、BiRefNet）去掉背景，保留 gpt-image-2 更强的真实感优势，但会增加一次调用延迟
3. **Chroma Key（绿幕法）**：prompt 里要求"纯绿色背景 #00ff00"，生成后代码做色键抠图，实现简单但主体本身含绿色时不适用

### moderation 详解

| 取值 | 说明 |
|---|---|
| `auto` | 默认审核严格度 |
| `low` | 审核更宽松，减少误拦截，仅 GPT image 系列支持 |

### stream / partial_images 详解

- `stream=true` 时接口以 SSE 事件流形式返回，事件类型包括 `image_generation.partial_image`（中间过渡图）和 `image_generation.completed`（最终完整图，附带 `usage`）
- `partial_images` 控制中间过渡图张数（0-3），设为 `0` 就只有最终一张完整图的事件，没有中间过程图

---

## 四、`/images/edits`（图生图/编辑）完整参数表

图生图相比文生图多了 `image`、`mask`、`input_fidelity` 三个专属参数，其余参数（`n`/`size`/`quality`/`background`/`output_format`/`output_compression`/`moderation`/`stream`/`partial_images`/`user`）的含义与取值范围与文生图接口**完全一致**，不再重复列出取值说明，只列出与文生图不同的地方。

| 参数 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `image` | file / array | 必填 | 待编辑的源图。GPT image 系列每张须为 `png`/`webp`/`jpg`，单张小于 50MB，**最多可传 16 张**（`dall-e-2` 只能传 1 张，且必须是正方形 png，小于 4MB） |
| `prompt` | string，必填 | - | 编辑描述文本 |
| `mask` | file | - | 遮罩图，**alpha 值为 0（全透明）的区域**表示"这部分要被编辑"。必须是合法 PNG，小于 4MB，且尺寸要与 `image` 一致。如果传了多张 `image`，mask 只作用在第一张上 |
| `model` | string | `gpt-image-1.5`（编辑接口默认值，与生成接口默认走 `dall-e-2` 不同） | 可选 `dall-e-2`、`gpt-image-1`、`gpt-image-1-mini`、`gpt-image-1.5`、`gpt-image-2`、`gpt-image-2-2026-04-21`、`chatgpt-image-latest` |
| `input_fidelity` | `high` / `low` | `low` | 见下方【input_fidelity 专题，⚠️ 官方文档与 Azure 实测存在冲突】 |
| `response_format` | `url` / `b64_json` | `url`（仅对 dall-e-2 生效） | 只对 `dall-e-2` 有效，GPT image 系列始终返回 base64 |

### input_fidelity 专题 ⚠️ 这正是你最初报错的那个参数

**含义**：控制模型编辑图片时，对输入图**细节还原程度**的努力程度——尤其是人脸、Logo、文字等需要精确保留的元素。

| 取值 | 说明 |
|---|---|
| `high` | 尽量精确还原输入图的细节/人脸/风格，但会消耗明显更多的图片 token（官方举例：方形图额外消耗约 4160 个输入 token，非方形约额外消耗 6240 个） |
| `low` | 默认值，允许模型更自由地重新诠释画面细节，token 消耗更低 |

**⚠️ 支持范围存在两种不同说法，务必留意：**

1. **OpenAI 官方 API Reference 最新文本（2026-05 抓取）**：

   > "This parameter is only supported for `gpt-image-1` and `gpt-image-1.5` and later models, unsupported for `gpt-image-1-mini`."
   >
   > 翻译：该参数仅 `gpt-image-1` 以及 `gpt-image-1.5` 及更晚的模型（按此表述应包含 gpt-image-2）支持，`gpt-image-1-mini` 不支持。

2. **Azure 实测 / 微软 Q&A 反馈（含你自己踩到的报错）**：
   - 你的原始报错：`status_code=400，The model 'gpt-image-2' does not support the 'input_fidelity' parameter.`（Azure 直接拒绝请求）
   - 微软官方 Q&A 关于 `gpt-image-1.5`：该参数不被识别，请求不会报错，但会被**静默忽略**并固定按 `high` 逻辑跑（导致 token 消耗更高但用户毫无感知）

**结论**：OpenAI 官方直连 API 的文档口径是"1.5 及更新模型都支持"，但**这份文档描述的是 OpenAI 自家 API 的行为**；而**你实际调用的是 Azure OpenAI**，Azure 侧的模型部署在参数支持上明显滞后于 OpenAI 官方，出现了两种不一致的失败模式：
- `gpt-image-1.5` 在 Azure 上：不报错，但参数被忽略、悄悄按 high 计费
- `gpt-image-2` 在 Azure 上：直接 400 硬报错，拒绝请求

**实操建议**：不管官方文档怎么写，**以 Azure 的实际返回结果为准**。调用 `gpt-image-2`（Azure）时**直接不要传 `input_fidelity` 字段**，避免 400；如果确实需要精细控制保真度，目前只有部署 `gpt-image-1` 是稳妥可用的。

---

## 五、响应字段说明（两个端点通用）

| 字段 | 类型 | 说明 |
|---|---|---|
| `created` | number | 创建时间的 Unix 时间戳 |
| `background` | `transparent` / `opaque` | 实际使用的背景类型（`gpt-image-2` 永远不会是 `transparent`） |
| `data[].b64_json` | string | base64 编码图片，GPT image 系列默认返回这个字段 |
| `data[].url` | string | 只有 `dall-e-2`/`dall-e-3` 且 `response_format=url` 时才有值，GPT image 系列不支持 |
| `data[].revised_prompt` | string | 只有 `dall-e-3` 会返回模型内部改写后的 prompt |
| `output_format` | `png`/`webp`/`jpeg` | 实际输出格式 |
| `quality` | `low`/`medium`/`high` | 实际使用的质量档位 |
| `size` | string | 实际输出尺寸 |
| `usage.input_tokens` | number | 输入 token 总数（文本+图片） |
| `usage.input_tokens_details.text_tokens` | number | 文本输入 token |
| `usage.input_tokens_details.image_tokens` | number | 图片输入 token（文生图时为 0） |
| `usage.output_tokens` | number | 输出 token（图片输出） |
| `usage.total_tokens` | number | 总 token |

> 官方文档字面写的是"For `gpt-image-1` only, the token usage information"，但从本项目 `gpt-image.md` 里贴出的实测响应样例看，`gpt-image-2` 同样会返回完整的 `usage` 字段，这句"仅 gpt-image-1"的措辞大概率是文档没跟上模型迭代、没及时更新，实测以能拿到 usage 为准。

---

## 六、与本项目现有文档的差异提醒

`docs/gptimage/gpt-image.md` 中的响应示例里出现过 `"background": "opaque"`，这与本文档结论一致（没有出现 transparent 的例子）；但该文档没有明确说明 `gpt-image-2` 不支持 `background=transparent`，也没有说明 `input_fidelity` 在 Azure 上对 `gpt-image-2` 会直接 400。建议后续维护该文档时补充这两点，避免有人照着文档传 `background: "transparent"` 或 `input_fidelity` 结果直接报错。

---

## 七、参考来源

- OpenAI API Reference - Create image：https://developers.openai.com/api/reference/python/resources/images/methods/generate
- OpenAI API Reference - Create image edit：https://developers.openai.com/api/reference/python/resources/images/methods/edit
- Azure/Microsoft Foundry - How to Use Image Generation Models from OpenAI：https://learn.microsoft.com/en-us/azure/foundry/openai/how-to/dall-e
- Microsoft Q&A - Changing input_fidelity on GPT-Image-1.5 to "low"：https://learn.microsoft.com/en-us/answers/questions/5752784/changing-input-fidelity-on-gpt-image-1-5-to-low
- Microsoft Foundry Blog - Introducing OpenAI's GPT-image-2 in Microsoft Foundry：https://techcommunity.microsoft.com/blog/azure-ai-foundry-blog/introducing-openais-gpt-image-2-in-microsoft-foundry/4500571
- OpenAI Developer Community - GPT Image 1 Input Fidelity：https://community.openai.com/t/gpt-image-1-input-fidelity/1317640
- 第三方实测博客（transparent background 失效现象与根因分析，非官方但与官方文档结论一致）：https://help.apiyi.com/en/gpt-image-2-transparent-background-not-supported-en.html

> 以上信息为 2026-08 联网检索汇总，OpenAI/Azure 双方文档仍在持续迭代，`gpt-image-2` 属于较新模型，参数支持范围可能随版本更新变化，建议关键参数（尤其 `input_fidelity`、`background`）上线前用你自己的 Azure 部署实测一次为准。
