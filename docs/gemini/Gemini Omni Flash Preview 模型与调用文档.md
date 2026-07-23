# Gemini Omni Flash Preview（`gemini-omni-flash-preview`）模型说明与调用文档

> 本文档基于 Google 官方文档整理（2026-07-23 联网核实），信息来源见文末「参考链接」。
> 该模型于 **2026-06-30 进入公开预览（Public Preview）**，退役日期 **2027-06-30**，接口字段可能随预览期推进继续变化，使用前建议以官方页面为准做二次核实。
> 网上部分第三方教程（如声称走 `client.models.generate_video()` 或第三方转发平台"统一 API"的文章）与官方文档不一致，本文档**只采信** `docs.cloud.google.com`（Gemini Enterprise Agent Platform，即新版 Vertex AI 文档站）、`ai.google.dev`（Gemini API 官方文档）以及 `github.com/GoogleCloudPlatform/generative-ai` 官方示例仓库三处来源。

---

## 1. 模型概览

| 属性 | 值 |
| --- | --- |
| 模型 ID | `gemini-omni-flash-preview` |
| 定位 | 多模态视频生成与对话式视频编辑模型（文本 / 图片 / 视频输入 → 视频（含音频）+ 文本输出） |
| 发布阶段 | Preview（预览） |
| 发布日期 | 2026-06-30 |
| 退役日期 | 2027-06-30 |
| 可用区域 | `global`（全球端点，暂无区域端点） |
| 推荐调用方式 | **Interactions API**（`client.interactions.create()` / REST `POST .../interactions`） |
| legacy 兼容 | 官方文档提到 `generateContent` 端点"仍可用，但可能无法覆盖全部预览能力"，**不建议**新集成使用 |
| 水印 / 溯源 | 所有输出视频自动带 **SynthID** 水印 + **C2PA 内容凭据**（Content Credentials） |

---

## 2. 模态与能力矩阵

以下表格摘自 Gemini Enterprise Agent Platform 官方模型页（`docs.cloud.google.com/gemini-enterprise-agent-platform/models/gemini/omni-flash-preview`）：

### 2.1 模态支持

| 模态 | 支持情况 |
| --- | --- |
| 文本（Text） | 输入 + 输出 |
| 图片（Image） | 仅输入 |
| 音频（Audio） | 作为独立模态**不支持**单独输入/输出；但视频输出中**包含**同步生成的音效/配乐/语音（见 §2.2「Sound generation」） |
| 视频（Video） | 输入 + 输出 |

### 2.2 能力（Capabilities）

| 能力 | 支持情况 |
| --- | --- |
| Thinking（推理） | 支持 |
| System instructions | **不支持** |
| Gemini Live API | **不支持** |
| Structured output | **不支持** |
| Context caching | **不支持** |
| Count Tokens | 支持 |
| RAG Engine | **不支持** |
| Chat completions（OpenAI 兼容） | **不支持** |
| Tuning（微调） | **不支持** |
| URL context | **不支持** |
| Image generation / Edit images / Interleaved images and text | **均不支持**（这是视频模型，不产出独立图片） |
| Generate videos from text（文生视频） | 支持 |
| Reference to video（参考图/参考素材生视频） | 支持 |
| Sound generation（语音 / 音乐 / 音效） | 支持（作为视频输出的一部分自动生成） |
| Video editing（视频编辑） | 支持 |
| Content Credentials (C2PA) | 支持 |

### 2.3 工具（Tools）

| 工具 | 支持情况 |
| --- | --- |
| Grounding（基于搜索/地图接地） | **不支持** |
| Code execution | **不支持** |
| Function calling | **不支持** |
| Computer Use（preview） | **不支持** |

### 2.4 消费方式（Consumption options）

| 方式 | 支持情况 |
| --- | --- |
| Provisioned Throughput | **不支持** |
| Batch inference | **不支持** |
| Pay-as-you-go | 官方模型页标注**不支持**（但价格页已公布 PayGo 单价，见 §8；两处口径存在出入，实际以计费账单为准） |
| Fixed quota | 支持 |

---

## 3. Token 限制与参数默认值

| 项 | 值 |
| --- | --- |
| 最大输入 token（Vertex / Gemini Enterprise Agent Platform 口径） | 131,072 |
| 最大输出 token（Vertex 口径） | 57,920 |
| 上下文窗口（Gemini API / AI Studio 口径，`ai.google.dev` 页面标注） | 1,048,576 |

> 两个数字口径不同：`docs.cloud.google.com`（企业级 Vertex 通道）与 `ai.google.dev`（Gemini Developer API / AI Studio 通道）对同一模型给出了不同的限额说明，推测是两条产品线的配额策略不同。**调用 Vertex 时以 131,072 / 57,920 为准**。

| 参数 | 取值范围 | 默认值 |
| --- | --- | --- |
| `temperature` | 0.0 – 2.0 | 1.0 |
| `topP` | 0.0 – 1.0 | 0.95 |
| `candidateCount` | — | 1（固定） |

---

## 4. 技术规格（输入/输出限制）

### 4.1 图片输入

| 项 | 值 |
| --- | --- |
| 单次请求最大图片数 | 10 张 |
| 单文件最大体积（API / 控制台内联上传） | 20 GiB |
| 单文件最大体积（控制台直接上传） | 7 MB |
| 支持的宽高比 | `16:9`、`9:16` |
| 支持的分辨率 | 720p |
| 支持的 MIME 类型 | `image/png`、`image/jpeg`、`image/webp`、`image/heic`、`image/heif` |

### 4.2 文本输入

| 项 | 值 |
| --- | --- |
| 单文件最大体积（API / GCS 导入） | 50 MB |
| 单文件最大体积（控制台直接上传） | 7 MB |
| 支持的 MIME 类型 | `text/plain` |

### 4.3 视频输入/输出

| 项 | 值 |
| --- | --- |
| 最大视频长度（含音频） | 10 秒 |
| 最大视频长度（不含音频） | 10 秒 |
| 单次请求最大视频数 | 3 个 |
| 支持的 MIME 类型 | `video/x-flv`、`video/quicktime`、`video/mpeg`、`video/mpegs`、`video/mpg`、`video/mp4`、`video/webm`、`video/wmv`、`video/3gpp` |
| 输出时长 | 3 秒 – 10 秒 |
| 输出分辨率 | 720p，24 FPS |
| 输出宽高比 | `16:9`（默认，横屏）、`9:16`（竖屏） |
| 输出音频 | 与视频一同生成（不可单独关闭，通过 `interactions-api` 参考实现看到 `includeAudio`/`generateAudio` 字样目前用于 legacy Veo 通道，Omni Flash 的 Interactions API 尚未见到独立关闭音频的公开参数） |

> **已知限制（官方 Notebook 明确写出）：**
> - 视频/音频**参考输入（reference input）暂不支持**，只有图片可作为参考素材。
> - 音频参考、视频参考、指定尾帧（last frame）、场景延展（scene extension）、更高分辨率——官方标注"即将支持"（"will be available soon"）。

---

## 5. 调用方式：Interactions API

Gemini Omni Flash 是首批推荐通过 **Interactions API**（而非传统的 `generateContent`）调用的模型。Interactions API 是一个**扁平化的独立端点**（不像其它 Gemini 模型那样按 `models/{model}:generateContent` 路径调用），模型名放在请求体的 `model` 字段里。

### 5.1 两条访问路径

| 访问方式 | Endpoint | 鉴权 |
| --- | --- | --- |
| **Gemini Developer API**（AI Studio / 消费级 API Key） | `POST https://generativelanguage.googleapis.com/v1beta/interactions` | `?key=$API_KEY` 查询参数，或 `x-goog-api-key` 请求头 |
| **Vertex AI / Gemini Enterprise Agent Platform**（企业级，本文档重点） | `POST https://aiplatform.googleapis.com/v1beta1/projects/{project}/locations/global/interactions` | `Authorization: Bearer $(gcloud auth print-access-token)`（OAuth2 / 服务账号），且**必须**先在项目里启用 Agent Platform API（`aiplatform.googleapis.com`） |

> Vertex 侧目前 Interactions API **只支持 `locations/global`**，不支持按区域（region）调用。

### 5.2 Python SDK 初始化（Vertex 模式）

```python
import os
from google import genai
from google.genai import interactions

PROJECT_ID = "[your-project-id]"
LOCATION = os.environ.get("GOOGLE_CLOUD_REGION", "global")

# enterprise=True 表示走 Vertex AI / Gemini Enterprise Agent Platform 通道
client = genai.Client(enterprise=True, project=PROJECT_ID, location=LOCATION)

omni_model = "gemini-omni-flash-preview"
```

---

## 6. 请求参数完整清单

Interactions API 的请求体是通用结构，适配所有可通过它调用的模型（视频模型 Omni Flash / 音乐模型 Lyria / Deep Research Agent 等）。以下为**创建 Interaction**（`POST .../interactions`）的完整请求体字段：

### 6.1 顶层字段

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `model` | string（枚举） | 与 `agent` 二选一必填 | 目标模型名，本文场景固定为 `gemini-omni-flash-preview` |
| `agent` | string（枚举） | 与 `model` 二选一必填 | 目标 Agent 名（如 `deep-research-preview-04-2026`），与 `model` 互斥 |
| `input` | `Content` \| `Content[]` \| `Step[]` \| string | **必填** | 本次交互的输入；纯文本可直接传字符串，多模态需传 `Content` 数组，多轮编辑可整段回传上一轮的 `steps` |
| `system_instruction` | string | 可选 | 系统指令（**注意**：Omni Flash 模型能力矩阵标注 System instructions 不支持，实际是否生效以联调结果为准） |
| `tools` | `Tool[]` | 可选 | 工具声明列表（Omni Flash 不支持 function calling / grounding / code execution，此字段对该模型基本无效） |
| `response_format` | `ResponseFormat` \| `ResponseFormatList` | 可选 | 约束响应格式；视频生成场景下用来传 `VideoResponseFormat`（见 §6.4） |
| `response_mime_type` | string | 当设置 `response_format` 时必填 | 响应 MIME 类型 |
| `response_modalities` | enum: `text` \| `image` \| `audio` \| `video` \| `document` | 可选 | 期望的响应模态 |
| `stream` | boolean | 可选 | 是否以 SSE 流式返回（见 §7.2） |
| `store` | boolean | 可选 | 是否存储本次请求/响应供后续检索 |
| `background` | boolean | 可选 | 是否后台异步执行（配合轮询，见 §7.1） |
| `previous_interaction_id` | string | 可选 | 上一轮 interaction 的 `id`，用于多轮对话式编辑（有状态） |
| `generation_config` | `GenerationConfig` | 可选，与 `agent_config` 二选一 | 模型侧生成参数（仅当设置 `model` 时可用） |
| `agent_config` | object（`DynamicAgentConfig` 等多态类型） | 可选，与 `generation_config` 二选一 | Agent 侧配置（仅当设置 `agent` 时可用） |

### 6.2 `generation_config`（通用生成参数）

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `temperature` | number | 采样随机性 |
| `top_p` | number | nucleus sampling 累积概率上限 |
| `seed` | integer | 采样随机种子，用于结果复现 |
| `stop_sequences` | string[] | 停止序列 |
| `thinking_level` | enum: `minimal` \| `low` \| `medium` \| `high` | 思考强度 |
| `thinking_summaries` | enum: `auto` \| `none` | 是否在响应中附带思考摘要 |
| `max_output_tokens` | integer | 最大输出 token 数 |
| `speech_config` | `SpeechConfig` | 语音相关配置（`voice` / `language` / `speaker`），主要用于 TTS/音乐类模型 |
| `image_config` | `ImageConfig` | 图片相关配置（`aspect_ratio` 枚举含 `1:1`/`2:3`/`3:2`/`3:4`/`4:3`/`4:5`/`5:4`/`9:16`/`16:9`/`21:9`/`1:8`/`8:1`/`1:4`/`4:1`；`image_size` 枚举 `1K`/`2K`/`4K`/`512`），主要用于图片生成类模型 |
| `video_config` | `VideoConfig` | **视频生成核心配置**，见 §6.3（该字段未出现在通用 API 参考页，来自官方 Colab Notebook 示例，属于 Python SDK `google.genai.interactions` 模块的强类型封装） |
| `tool_choice` | `ToolChoiceConfig` \| `ToolChoiceType` | 工具选择策略 |

### 6.3 `VideoConfig`（视频生成配置，`generation_config.video_config`）

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `task` | enum: `text_to_video` \| `image_to_video` \| `reference_to_video` \| `edit` | **必须**根据输入内容与期望行为显式指定：<br>· `text_to_video`：纯文本生视频<br>· `image_to_video`：输入图片作为起始帧生成视频<br>· `reference_to_video`：输入图片作为风格/主体参考（非首帧）生成视频<br>· `edit`：对已有视频做指令编辑 |

### 6.4 `VideoResponseFormat`（响应格式配置，`response_format`）

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `type` | string | 固定为 `"video"`（可选，通常可省略） |
| `aspect_ratio` | enum: `9:16` \| `16:9` | 输出宽高比，默认 `16:9`（横屏） |
| `duration` | string，如 `"9s"` | 输出视频时长，3s – 10s |
| `delivery` | string，如 `"uri"` | 若设置为 `"uri"`，视频落地到 GCS 而非直接 base64 返回，需配合 `gcs_uri` |
| `gcs_uri` | string，如 `"gs://<BUCKET>"` | 配合 `delivery="uri"` 使用，指定输出视频写入的 GCS bucket 路径 |

### 6.5 `input` 中的内容块（`Content`）类型

`input` 可以是纯字符串（等价于单个 text 内容块），也可以是以下内容块组成的数组：

| 类型 | `type` 取值 | 关键字段 |
| --- | --- | --- |
| 文本 | `text` | `text`（string，必填） |
| 图片 | `image` | `data`（base64 string）或 `uri`；`mime_type` 枚举 `image/png`/`image/jpeg`/`image/webp`/`image/heic`/`image/heif`/`image/gif`/`image/bmp`/`image/tiff`；`resolution` 枚举 `low`/`medium`/`high`/`ultra_high` |
| 音频 | `audio` | `data`（base64）或 `uri`；`mime_type` 枚举 `audio/wav`/`audio/mp3`/`audio/aiff`/`audio/aac`/`audio/ogg`/`audio/flac`/`audio/mpeg`/`audio/m4a`/`audio/l16`/`audio/opus`/`audio/alaw`/`audio/mulaw`；`channels`、`sample_rate` |
| 文档 | `document` | `data`（base64）或 `uri`；`mime_type` 目前仅 `application/pdf` |
| 视频 | `video` | `data`（base64）或 `uri`；`mime_type` 枚举 `video/mp4`/`video/mpeg`/`video/mpg`/`video/mov`/`video/avi`/`video/x-flv`/`video/webm`/`video/wmv`/`video/3gpp`；`resolution` 枚举同图片 |

> **对 Omni Flash 而言**：图片可作为「首帧/编辑源」或「参考」传入（由 `video_config.task` 决定语义）；视频只能作为「编辑源」传入（`task=edit`），且**长度需 ≤ 10 秒**；音频当前不能作为参考输入。

---

## 7. 同步 / 异步 / 流式三种响应模式

### 7.1 同步（默认）

不设置 `background`/`stream`，请求会一直阻塞直到生成完成，直接返回完整 `Interaction` 资源（见 §8 响应结构）。适合短视频、低并发场景。

### 7.2 异步（后台任务 + 轮询）——`background: true`

```python
initial_interaction = client.interactions.create(
    model=omni_model,
    input=prompt,
    background=True,
)
interaction = initial_interaction
while interaction.status not in ["completed", "failed"]:
    time.sleep(10)
    interaction = client.interactions.get(id=initial_interaction.id)
```

REST 等价调用：

```bash
# 1) 提交（带 background: true），拿到返回体里的 id
curl -X POST \
  -H "Authorization: Bearer $(gcloud auth print-access-token)" \
  -H "Content-Type: application/json" \
  "https://aiplatform.googleapis.com/v1beta1/projects/$PROJECT_ID/locations/global/interactions" \
  -d '{"model": "gemini-omni-flash-preview", "input": "...", "background": true}'

# 2) 轮询
curl -X GET \
  -H "Authorization: Bearer $(gcloud auth print-access-token)" \
  "https://aiplatform.googleapis.com/v1beta1/projects/$PROJECT_ID/locations/global/interactions/{id}"
```

### 7.3 流式（SSE）——`stream: true`

`GET .../interactions/{id}?stream=true` 或创建时传 `stream: true`，服务端以 Server-Sent Events 推送以下事件类型：

| 事件 | 说明 |
| --- | --- |
| `interaction.created` | Interaction 已创建，返回初始 `id`/`status` |
| `interaction.status_update` | 状态变化通知 |
| `step.start` | 某个 step 开始输出 |
| `step.delta` | 增量内容（如逐字文本、逐块数据） |
| `step.stop` | 该 step 输出结束 |
| `interaction.completed` | 整个交互完成，附带最终 `usage` 等信息 |
| `done` | 流结束标记，`data: [DONE]` |

> 获取历史/单条 interaction 也支持 `last_event_id` 参数，可从某个事件之后继续续传（断线重连场景）。

---

## 8. 响应参数完整清单（`Interaction` 资源）

### 8.1 顶层字段

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `id` | string | Interaction 唯一 ID（Vertex 侧形如 `v1_ChdPU0F4...`） |
| `object` | string | 固定 `"interaction"` |
| `status` | enum: `in_progress` \| `requires_action` \| `completed` \| `failed` \| `cancelled` \| `incomplete` | 交互状态 |
| `role` | string | 通常为 `"model"` |
| `model` | string | 实际使用的模型名 |
| `created` / `updated` | string（ISO 8601，`YYYY-MM-DDThh:mm:ssZ`） | 创建/更新时间 |
| `system_instruction` | string | 回显请求中的系统指令 |
| `tools` | `Tool[]` | 回显请求中的工具声明 |
| `response_modalities` | enum（同 §6.1） | 回显请求中的期望响应模态 |
| `response_mime_type` | string | 回显响应 MIME 类型 |
| `previous_interaction_id` | string | 回显上一轮 interaction ID（多轮场景） |
| `steps` | `Step[]` | **核心字段**：本次交互的完整轨迹，见 §8.3 |
| `usage` | `Usage` | Token 用量统计，见 §8.2 |

### 8.2 `usage`（Token 用量）

| 字段 | 说明 |
| --- | --- |
| `total_input_tokens` | 输入（prompt/context）总 token 数 |
| `input_tokens_by_modality[]` | 按模态（`text`/`image`/`audio`/`video`/`document`）拆分的输入 token |
| `total_cached_tokens` / `cached_tokens_by_modality[]` | 缓存部分的 token 及模态拆分 |
| `total_output_tokens` / `output_tokens_by_modality[]` | 输出 token 及模态拆分 |
| `total_tool_use_tokens` / `tool_use_tokens_by_modality[]` | 工具调用相关 token 及模态拆分 |
| `total_thought_tokens` | 思考（reasoning）token 数（Thinking 模型特有） |
| `total_tokens` | 本次请求总 token（输入+输出+内部开销） |
| `grounding_tool_count[]` | 基于搜索/地图接地的工具调用次数（Omni Flash 不支持 grounding，通常为空） |

### 8.3 `steps[]`（交互轨迹）

每个 `Step` 通过 `type` 字段做多态区分：

| `type` | 说明 |
| --- | --- |
| `user_input` | 用户输入回显（`content: Content[]`） |
| `thought` | 模型思考过程（`content` 中 `type: "thought"`，仅当 `thinking_summaries != "none"` 时出现） |
| `model_output` | **模型最终输出**（`content: Content[]`），视频生成场景下需要从这里取 `type: "video"` 的内容块 |

> ⚠️ **重要**：SDK 提供了便捷字段 `interaction.output_video` / `interaction.output_image`，但这是 **SDK 独有的语法糖**。直接调用 REST API 时，必须自己遍历 `steps[]`，筛出 `type == "model_output"` 的 step，再从其 `content[]` 里取 `type == "video"` 的内容块的 `data`（base64）或 `uri` 字段。

### 8.4 REST 原始 JSON 结构示例（文生视频）

```json
{
  "steps": [
    { "type": "user_input", "content": [{"type": "text", "text": "..."}] },
    { "type": "thought", "content": [{"text": "...", "type": "thought"}] },
    {
      "type": "model_output",
      "content": [
        {
          "type": "video",
          "mime_type": "video/mp4",
          "data": "AAAAIGZ0eXBpc29t..."
        }
      ]
    }
  ],
  "id": "v1_...",
  "status": "completed",
  "model": "gemini-omni-flash-preview",
  "object": "interaction"
}
```

---

## 9. 完整调用示例

### 9.1 文生视频（Python，同步）

```python
import base64
from google import genai
from google.genai import interactions

client = genai.Client(enterprise=True, project=PROJECT_ID, location="global")

interaction = client.interactions.create(
    model="gemini-omni-flash-preview",
    input="A marble rolling fast on a chain reaction style track, continuous smooth shot.",
    generation_config=interactions.GenerationConfig(
        video_config=interactions.VideoConfig(task="text_to_video")
    ),
    response_format=interactions.VideoResponseFormat(
        aspect_ratio="16:9",
        duration="9s",
    ),
)

contents = [c for step in interaction.steps if step.type == "model_output" for c in step.content]
with open("output.mp4", "wb") as f:
    f.write(base64.b64decode(contents[0].data))
```

### 9.2 图生视频（起始帧）

```python
with open("start.png", "rb") as f:
    img_b64 = base64.b64encode(f.read()).decode("utf-8")

interaction = client.interactions.create(
    model="gemini-omni-flash-preview",
    input=[
        {"type": "text", "text": "A hard-shell suitcase rolling, stops and opens..."},
        {"type": "image", "mime_type": "image/png", "data": img_b64},
    ],
    generation_config=interactions.GenerationConfig(
        video_config=interactions.VideoConfig(task="image_to_video")
    ),
)
```

### 9.3 多图参考生视频（风格/主体参考，非首帧）

```python
interaction = client.interactions.create(
    model="gemini-omni-flash-preview",
    input=[
        {"type": "text", "text": "A woman walks up to the game console... 9:16 aspect ratio. 7 second video."},
        {"type": "image", "mime_type": "image/jpeg", "data": character_b64},
        {"type": "image", "mime_type": "image/jpeg", "data": product_b64},
    ],
    generation_config=interactions.GenerationConfig(
        video_config=interactions.VideoConfig(task="reference_to_video")
    ),
)
```

### 9.4 视频编辑（对已有视频做指令编辑）

```python
interaction = client.interactions.create(
    model="gemini-omni-flash-preview",
    input=[
        {"type": "text", "text": "Change the dog to the cat. Remove the backpack and add a propeller hat."},
        {"type": "image", "mime_type": "image/png", "uri": "gs://.../chair-cat.png"},
        {"type": "video", "mime_type": "video/mp4", "uri": "gs://.../dog_day1.mp4"},
    ],
    generation_config=interactions.GenerationConfig(
        video_config=interactions.VideoConfig(task="edit")
    ),
)
```

> 视频编辑源的时长限制与普通输入一致：**必须 ≤ 10 秒**。

### 9.5 多轮对话式编辑（有状态，携带上一轮 `steps`）

```python
interaction1 = client.interactions.create(model=omni_model, input=prompt1)

turn2_input = interaction1.steps + [
    {"type": "user_input", "content": [{"type": "text", "text": "Now make the same video in a doodle style."}]}
]
interaction2 = client.interactions.create(model=omni_model, input=turn2_input)
```

> 也可以只传 `previous_interaction_id=interaction1.id` 而不必手动拼回 `steps`（两种方式官方 Notebook 都出现过，`previous_interaction_id` 更轻量）。

### 9.6 REST 完整示例（curl，Vertex，异步轮询）

```bash
curl -X POST \
  -H "Authorization: Bearer $(gcloud auth print-access-token)" \
  -H "Content-Type: application/json" \
  "https://aiplatform.googleapis.com/v1beta1/projects/$PROJECT_ID/locations/global/interactions" \
  -d '{
    "model": "gemini-omni-flash-preview",
    "input": "A futuristic city with neon lights and flying cars, cyberpunk style",
    "generation_config": {
      "video_config": { "task": "text_to_video" }
    },
    "response_format": {
      "type": "video",
      "aspect_ratio": "9:16"
    },
    "background": true
  }'
```

---

## 10. 列表 / 获取接口

### 10.1 列出已创建的 interactions

```
GET https://aiplatform.googleapis.com/v1beta1/projects/{project}/locations/global/interactions?page_size=10&page_token=...
```

| 参数 | 说明 |
| --- | --- |
| `page_size` | 每页数量，默认 10，最大 500 |
| `page_token` | 分页游标 |

响应：`{"interaction_metadatas": [{"id": "..."}], "next_page_token": "..."}`

### 10.2 获取单个 interaction

```
GET https://aiplatform.googleapis.com/v1beta1/projects/{project}/locations/global/interactions/{id}?stream=<bool>&last_event_id=<string>
```

---

## 11. 计费（Vertex AI 口径）

| 资源 | 单价 |
| --- | --- |
| 输入（文本 / 图片 / 视频 / 音频，统一按 token 计） | $1.50 / 100 万 token |
| 文本输出（回答 + 推理） | $9 / 100 万 token |
| 视频输出 | $0.10 / 秒视频（等价 $17.50 / 100 万视频输出 token） |

Token 折算规则：

| 媒体 | 折算 |
| --- | --- |
| 图片输入 | 每张 2,040 token |
| 音频输入 | 每秒 32 token |
| 视频输入 | 每秒 5,792 token |
| 视频输出（720p，含音频） | 每秒 5,792 token |

> 例：一段 10 秒 720p 输出视频 = 57,920 输出 token，对应最大输出 token 限制（见 §3）刚好打满，也印证了"最大输出 token=57,920"就是"10 秒视频输出"的上限。

---

## 12. 已知限制小结

1. 预览模型，**不建议承载关键生产链路**，需有降级方案。
2. 不支持 system instruction、结构化输出、函数调用、grounding、context caching、tuning、RAG Engine。
3. 参考素材目前只支持图片，视频/音频参考"即将支持"。
4. 视频输入/编辑源最长 10 秒；输出最长 10 秒、720p、24fps。
5. Vertex 侧目前只有 `global` 区域端点。
6. 输出必带 SynthID 水印与 C2PA 内容凭据，无法关闭。
7. Interactions API 本身标注为 **experimental**（实验性），字段可能变化。

---

## 参考链接

1. https://docs.cloud.google.com/gemini-enterprise-agent-platform/models/gemini/omni-flash-preview — 官方模型规格页
2. https://docs.cloud.google.com/gemini-enterprise-agent-platform/reference/models/interactions-api — 官方 Interactions API 参考（请求/响应通用 schema）
3. https://ai.google.dev/gemini-api/docs/omni — Gemini Developer API 侧的 Omni Flash 使用文档（含 REST/Python/JS 示例）
4. https://ai.google.dev/gemini-api/docs/models/gemini-omni-flash — Gemini API 模型速览页
5. https://ai.google.dev/gemini-api/docs/video — 视频生成模型总览（Omni Flash vs Veo 3.1 选型对比）
6. https://github.com/GoogleCloudPlatform/generative-ai/blob/main/vision/getting-started/gemini_omni_flash_video_gen.ipynb — 官方 Vertex AI 调用 Notebook（`VideoConfig`/`VideoResponseFormat`/`previous_interaction_id`/`background` 轮询等用法均来自此处）
7. https://cloud.google.com/gemini-enterprise-agent-platform/generative-ai/pricing — 官方计费页（Gemini Omni 章节）
8. https://deepmind.google/models/model-cards/gemini-omni-flash/ — 官方 Model Card（模型架构/训练数据/安全说明）
