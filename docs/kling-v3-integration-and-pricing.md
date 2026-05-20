# 可灵 Kling V3.0 接入与计费配置文档

## 一、为什么有 `/kling/v1/...` 这套接口？

系统存在**两套**可灵调用入口，用途不同：

### 路由 A：可灵原生格式（`/kling/v1/...`）

```
POST /kling/v1/videos/text2video
POST /kling/v1/videos/image2video
GET  /kling/v1/videos/text2video/:task_id
GET  /kling/v1/videos/image2video/:task_id
```

**存在原因**：供**外部第三方**（如使用可灵官方 SDK 的客户）以可灵原生 API 格式直接接入本系统，无需修改已有代码。
中间件 `KlingRequestConvert` 将原生格式**透明转换**为内部统一格式（改写 path 为 `/v1/video/generations`，包装 metadata），再走同一个 `controller.RelayTask`。

### 路由 B：OpenAI 兼容格式（`/v1/videos`）

```
POST /v1/videos                    → 提交视频生成（所有渠道统一入口）
GET  /v1/videos/:task_id           → 查询任务状态
POST /v1/video/generations         → 别名
GET  /v1/video/generations/:task_id
GET  /v1/videos/:task_id/content   → 视频内容代理下载
```

**用途**：内部前端、Go 后端、以及需要 OpenAI 格式兼容性的场景。

### 对比总结

| 维度 | 路径 A (`/kling/v1/...`) | 路径 B (`/v1/videos`) |
|------|--------------------------|----------------------|
| 目标用户 | 外部用可灵 SDK 直接对接的客户 | **内部前端 / 统一渠道调用** |
| 请求格式 | 可灵官方格式（`model_name`, `aspect_ratio` 等原生字段） | OpenAI 兼容格式（`model`, `size` 等） |
| 转换层 | `KlingRequestConvert` 中间件 | `kling/adaptor.go` 直接处理 |
| 内部流程 | 转换后 → 同一 RelayTask | 直接 → RelayTask |
| **前端推荐** | 不推荐 | **推荐，继续用此路径** |

**结论**：`/kling/v1/...` 是为了让外部调用者能用可灵官方 SDK 格式对接本系统。前端 Chat 视频模式应继续使用 `POST /v1/videos`，只需在 `model` 字段传入 `kling-v3` 或 `kling-v3-omni`。

---

## 二、Kling V3.0 官方 API 参数

> 以下根据官方文档整理：
> 文生视频: https://app.klingai.com/global/dev/document-api/apiReference/model/textToVideo
> 图生视频: https://app.klingai.com/global/dev/document-api/apiReference/model/imageToVideo
> 多图参考: https://app.klingai.com/global/dev/document-api/apiReference/model/multiImageToVideo
> 运动控制: https://app.klingai.com/global/dev/document-api/apiReference/model/motionControl

### 2.1 模型名称

| 场景 | 模型名 | 说明 |
|------|--------|------|
| 文生视频 | `kling-v3` | 纯文本提示生成视频 |
| 图生视频 / 多图参考 / 运动控制 | `kling-v3-omni` | 多模态，支持图片输入 |

> `kling-v3` = 文生视频专用；`kling-v3-omni` = 文生 + 图生 + 多图参考 + 运动控制全能

### 2.2 文生视频（Text to Video）

**Endpoint**（经本系统统一路由）：`POST /v1/videos`

**请求字段（通过 `metadata` 透传至可灵上游）**：

| 字段 | 类型 | 必填 | 说明 | 可选值 |
|------|------|------|------|--------|
| `model` | string | 是 | 模型名 | `kling-v3` |
| `prompt` | string | 是 | 正向提示词，最多 2500 字符 | — |
| `negative_prompt` | string | 否 | 负向提示词，最多 2500 字符 | — |
| `cfg_scale` | float | 否 | 生成视频的自由度，值越大越贴近 prompt（默认 0.5） | 0~1 |
| `mode` | string | 否 | 生成模式（默认 `std`） | `std`（标准）/ `pro`（专业） |
| `aspect_ratio` | string | 否 | 视频宽高比（默认 `16:9`） | `16:9` / `9:16` / `1:1` / `4:3` / `3:4` |
| `duration` | string | 否 | 视频时长（默认 `5`，单位秒） | `5` / `10` |
| `callback_url` | string | 否 | 任务完成后的回调地址 | — |
| `external_task_id` | string | 否 | 外部自定义任务 ID | — |

**请求示例**：

```json
{
  "model": "kling-v3",
  "prompt": "一只猫咪在草地上奔跑，阳光明媚，高清画质",
  "negative_prompt": "模糊, 低质量",
  "mode": "std",
  "duration": "5",
  "aspect_ratio": "16:9",
  "cfg_scale": 0.5
}
```

### 2.3 图生视频（Image to Video）

**Endpoint**：`POST /v1/videos`（`model` 使用 `kling-v3-omni`，请求体含 `image` 字段）

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `model` | string | 是 | `kling-v3-omni` |
| `image` | string | 是 | 首帧图片，支持 URL 或 Base64（JPG/PNG/WEBP，≤10MB，尺寸 ≥300px，宽高比 1:2.5 ~ 2.5:1）|
| `image_tail` | string | 否 | 尾帧图片（与首帧相同要求），用于首尾帧模式 |
| `prompt` | string | 否 | 提示词（最多 2500 字符）|
| `negative_prompt` | string | 否 | 负向提示词 |
| `cfg_scale` | float | 否 | 自由度，默认 0.5，范围 0~1 |
| `mode` | string | 否 | `std` / `pro`，默认 `std` |
| `duration` | string | 否 | `5` / `10`，默认 `5` |
| `aspect_ratio` | string | 否 | 宽高比（图生视频时建议与图片一致）|
| `static_mask` | string | 否 | 静态画面遮罩（Base64 PNG）|
| `dynamic_masks` | array | 否 | 动态运动遮罩列表（详见运动控制）|
| `callback_url` | string | 否 | 回调地址 |
| `external_task_id` | string | 否 | 外部任务 ID |

**请求示例（首帧图生视频）**：

```json
{
  "model": "kling-v3-omni",
  "image": "https://example.com/cat.jpg",
  "prompt": "猫咪慢慢站起来，回头看镜头",
  "mode": "std",
  "duration": "5"
}
```

**请求示例（首尾帧）**：

```json
{
  "model": "kling-v3-omni",
  "image": "https://example.com/start.jpg",
  "image_tail": "https://example.com/end.jpg",
  "prompt": "人物从左走到右"
}
```

### 2.4 多图参考（Multi-Image Reference）

仅 `kling-v3-omni` 支持。通过 `metadata.reference_images` 字段传入多张参考图（与 `image` 字段互斥，不能同时使用）：

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `model` | string | 是 | `kling-v3-omni` |
| `prompt` | string | 是 | 提示词 |
| `reference_images` | array | 是 | 参考图列表，每项含 `image`（URL/Base64）和 `reference_type`（`subject` 主体参考 / `scene` 场景参考）|
| `mode` / `duration` / `aspect_ratio` | … | 否 | 同文生视频 |

**请求示例**：

```json
{
  "model": "kling-v3-omni",
  "prompt": "人物在海边漫步",
  "reference_images": [
    { "image": "https://example.com/person.jpg", "reference_type": "subject" },
    { "image": "https://example.com/beach.jpg",  "reference_type": "scene" }
  ],
  "duration": "5",
  "aspect_ratio": "16:9"
}
```

### 2.5 运动控制（Motion Control）

仅 `kling-v3-omni` 支持。通过 `camera_control` 控制镜头运动，或通过 `dynamic_masks` 控制局部元素运动：

**镜头运动（camera_control）**：

```json
{
  "model": "kling-v3-omni",
  "image": "https://example.com/scene.jpg",
  "prompt": "场景缓慢向前推进",
  "camera_control": {
    "type": "simple",
    "config": {
      "horizontal": 0,
      "vertical": 0,
      "pan": 0,
      "tilt": 0,
      "roll": 0,
      "zoom": 5
    }
  }
}
```

`camera_control.type` 可选值：`simple`（自定义参数）/ `zoom_in` / `zoom_out` / `push_in` / `pull_out` / `pan_left` / `pan_right` / `tilt_up` / `tilt_down` / `roll_clockwise` / `roll_anti_clockwise`

**动态遮罩（dynamic_masks）**：

```json
{
  "model": "kling-v3-omni",
  "image": "https://example.com/scene.jpg",
  "dynamic_masks": [
    {
      "mask": "<Base64 PNG 遮罩>",
      "trajectories": [
        { "x": 100, "y": 200 },
        { "x": 300, "y": 400 }
      ]
    }
  ]
}
```

### 2.6 统一响应格式

**提交任务响应（HTTP 200）**：

```json
{
  "id": "task_abc123",
  "object": "video.generation",
  "status": "queued",
  "model": "kling-v3",
  "created_at": 1700000000
}
```

**查询任务状态响应**（`GET /v1/videos/:task_id`）：

```json
{
  "id": "task_abc123",
  "status": "succeeded",
  "model": "kling-v3",
  "metadata": {
    "url": "https://cdn.example.com/video.mp4"
  },
  "created_at": 1700000000,
  "completed_at": 1700000060
}
```

`status` 状态映射：

| 可灵上游状态 | 本系统 OpenAI 格式状态 |
|------------|----------------------|
| `submitted` | `queued` |
| `processing` | `in_progress` |
| `succeed` | `succeeded` |
| `failed` | `failed` |

---

## 三、官方定价（Kling V3.0）

> 来源：https://app.klingai.com/global/dev/document-api/productBilling/prePaidResourcePackage
> 单位：美元/秒

### 3.1 文生视频 / 图生视频定价

| 模式 | 是否含原生音频 | 每秒价格（USD/s） |
|------|--------------|-----------------|
| Standard（标准）| 不含音频 | **$0.084** |
| Standard（标准）| 含音频（无语音控制）| **$0.126** |
| Professional（专业）| 不含音频 | **$0.112** |
| Professional（专业）| 含音频（无语音控制）| **$0.168** |

### 3.2 运动控制定价

| 模式 | 每秒价格（USD/s）|
|------|----------------|
| Motion Control Standard | **$0.126** |
| Motion Control Professional | **$0.168** |

### 3.3 计费举例

| 场景 | 模式 | 时长 | 含音频 | 费用 |
|------|------|------|--------|------|
| 文生视频 | std | 5s | 否 | 0.084 × 5 = **$0.42** |
| 文生视频 | std | 10s | 否 | 0.084 × 10 = **$0.84** |
| 文生视频 | pro | 5s | 否 | 0.112 × 5 = **$0.56** |
| 文生视频 | std | 5s | 是 | 0.126 × 5 = **$0.63** |
| 运动控制 | std | 5s | — | 0.126 × 5 = **$0.63** |

---

## 四、系统计费配置

### 4.1 计费体系优先级

```
优先级 1（最高）: VideoModelPricePerSecond  → 按秒计费（推荐用于可灵）
优先级 2:         VideoRatio + VideoCompletionRatio
优先级 3:         ModelRatio + CompletionRatio（文本体系兜底）
```

### 4.2 推荐配置：`VideoModelPricePerSecond`

**kling-v3 系列按秒定价的特殊性**：可灵 V3 有标准/专业两种模式，且有含音频/不含音频区分。现有按秒计费体系支持 `noAudio`/`audio` 分档（`VideoAudioPricing`），但**不支持在同一模型名内的 std/pro 双模式分档**。

**处理方案（已采用）**：将 std/pro 拆为不同模型名（`kling-v3` vs `kling-v3-pro`），利用 `noAudio`/`audio` 覆盖音频差价：

```json
// 后台 → 系统设置 → VideoModelPricePerSecond
{
  "kling-v1":           0.014,
  "kling-v1-6":         0.028,
  "kling-v2-master":    0.028,
  "kling-v3":           { "noAudio": 0.084, "audio": 0.126 },
  "kling-v3-pro":       { "noAudio": 0.112, "audio": 0.168 },
  "kling-v3-omni":      { "noAudio": 0.084, "audio": 0.126 },
  "kling-v3-omni-pro":  { "noAudio": 0.112, "audio": 0.168 }
}
```

> `noAudio`/`audio` 分档由 `generate_audio` 字段决定（通过 `parseGenerateAudioFromUpstreamBody` 读取），不传则默认按 `noAudio` 计费。

**计费公式**：

```
扣费 quota = 官方价格($/s) × 渠道折扣 × 实际生成秒数 × 500 × 用户分组倍率
```

示例：生成 5s 标准模式视频（$0.084/s），无折扣，分组倍率 1：
```
0.084 × 5 × 500 × 1 = 210 quota 单位
```

### 4.3 全量计费参数速查

| 参数名 | 用途 | 格式 | 示例 |
|--------|------|------|------|
| `ModelRatio` | 文本输入倍率（基准 $2/M tokens）| `{"模型名": N}` | `{"gpt-4o": 2.5}` |
| `CompletionRatio` | 文本输出相对输入的倍率 | `{"模型名": N}` | `{"gpt-4o": 4}` |
| `AudioRatio` | 音频输入倍率（未配置回退 ModelRatio）| `{"模型名": N}` | — |
| `AudioCompletionRatio` | 音频输出倍率 | `{"模型名": N}` | — |
| `ImageRatio` | 图片输入倍率 | `{"模型名": N}` | — |
| `ImageCompletionRatio` | 图片输出倍率 | `{"模型名": N}` | — |
| `VideoRatio` | 视频倍率（VideoModelPricePerSecond 优先级更高）| `{"模型名": N}` | — |
| `VideoCompletionRatio` | 视频补全倍率/价格，支持 `noAudio`/`audio` 分档 | `{"模型名": N}` 或 `{"模型名": {"noAudio": N, "audio": N}}` | — |
| `ModelPrice` | 按次计费固定价格（美元）| `{"模型名": N}` | `{"dall-e-3": 0.04}` |
| `ImageModelPricePerImage` | 按张计费（美元/张）| `{"模型名": N}` | — |
| `VideoModelPricePerSecond` | **按秒计费（最高优先级，可灵推荐用此）** | `{"模型名": N}` | `{"kling-v3": 0.084}` |

**倍率换算示例**：

> 基准：$2/M tokens
> 若模型输入 $10/M tokens → ModelRatio = 10 ÷ 2 = **5**
> 若输出 $100/M tokens → CompletionRatio = 100 ÷ 10 = **10**（相对输入价格的倍数）

---

## 五、后端接入改动

### 5.1 `relay/channel/task/kling/adaptor.go`（已实现）

#### GetModelList()

现已注册 7 个模型名：

```go
func (a *TaskAdaptor) GetModelList() []string {
    return []string{
        "kling-v1",
        "kling-v1-6",
        "kling-v2-master",
        "kling-v3",            // 文生视频，标准模式
        "kling-v3-pro",        // 文生视频，专业模式
        "kling-v3-omni",       // 多模态（图/多图/运动控制），标准模式
        "kling-v3-omni-pro",   // 多模态，专业模式
    }
}
```

#### normalizeKlingModelAndMode() 辅助函数

```go
// 将 "-pro" 后缀模型名映射回真实上游模型名，并设置 mode 默认值
func normalizeKlingModelAndMode(model string) (realModel string, defaultMode string) {
    if strings.HasSuffix(model, "-pro") {
        return strings.TrimSuffix(model, "-pro"), "pro"
    }
    return model, "std"
}
```

| 前端传入 model | 上游 model_name | 默认 mode |
|---------------|----------------|----------|
| `kling-v3` | `kling-v3` | `std` |
| `kling-v3-pro` | `kling-v3` | `pro` |
| `kling-v3-omni` | `kling-v3-omni` | `std` |
| `kling-v3-omni-pro` | `kling-v3-omni` | `pro` |

#### requestPayload 新增 generate_audio 字段

```go
type requestPayload struct {
    // ... 原有字段 ...
    GenerateAudio  *bool  `json:"generate_audio,omitempty"`
}
```

`GenerateAudio` 直接从 `TaskSubmitReq.GenerateAudio` 透传，用于区分含/不含原生音频的计费路径。

#### mode 优先级

`convertToRequestPayload` 中：`req.Mode`（前端显式传入）> `normalizeKlingModelAndMode` 返回的默认值。
即：前端传 `mode: "pro"` 则使用 pro；不传则由模型名后缀决定。

### 5.2 后台配置

1. **渠道管理**：为现有可灵渠道勾选全部 7 个模型名
2. **系统设置 → VideoModelPricePerSecond**：添加 kling-v3 系列每秒价格（见第四章方案 B）

---

## 六、前端：Chat 视频模式接入

### 6.1 `pages/Chat/modelConstants.ts` 改动

**① 视频时长选项**（在 `getVideoDurationOptions` 中添加）：

```typescript
if (model.startsWith('kling-v3')) {
  return [5, 10];
}
```

**② 宽高比支持**（在 `getVideoRatios` 中确认 kling 系列排除 adaptive 和 21:9）：

kling-v3 系列支持：`16:9` / `9:16` / `1:1` / `4:3` / `3:4`
不支持：`21:9`、`adaptive`

**③ 图片上传支持**（在 `supportsImageUpload` 中配置）：

```typescript
// kling-v3：纯文生视频，不支持图片输入
if (model === 'kling-v3') return false;
// kling-v3-omni：支持图生视频 / 多图参考 / 运动控制，支持图片输入
// （默认 true 已覆盖，无需额外处理）
```

**④ 生成模式（mode）**：kling-v3 系列新增 `pro` 模式，如需在 UI 中暴露，需要在请求 metadata 中透传 `mode: "pro"`。

### 6.2 前端调用示例

```typescript
// 文生视频（kling-v3，标准模式）
const response = await videoGenerateService.submit({
  model: 'kling-v3',
  prompt: '城市夜晚的街道，霓虹灯闪烁',
  duration: 5,
  aspect_ratio: '16:9',
  // mode 字段通过 metadata 透传：
  metadata: { mode: 'std' }
});

// 图生视频（kling-v3-omni）
const response = await videoGenerateService.submit({
  model: 'kling-v3-omni',
  image: 'https://cdn.example.com/photo.jpg',
  prompt: '人物缓缓转身',
  duration: 5
});
```

---

## 七、模型广场（`/pages/Models`）配置

模型广场从后台拉取模型列表，只需在后台管理中添加模型元数据：

| 字段 | kling-v3 | kling-v3-pro | kling-v3-omni | kling-v3-omni-pro |
|------|----------|--------------|---------------|-------------------|
| `model_name` | `kling-v3` | `kling-v3-pro` | `kling-v3-omni` | `kling-v3-omni-pro` |
| `endpoint_type` | `video` | `video` | `video` | `video` |
| `category` | `video-generation` | `video-generation` | `video-generation` | `video-generation` |
| `quota_type` | `3`（按秒计费）| `3` | `3` | `3` |
| `tags` | `["可灵", "视频生成", "文生视频"]` | `["可灵", "视频生成", "文生视频", "专业模式"]` | `["可灵", "视频生成", "多模态", "图生视频"]` | `["可灵", "视频生成", "多模态", "专业模式"]` |

`quota_type` 与模型广场计费标签对应：

| quota_type | 展示 |
|-----------|------|
| 0 | 按量计费 |
| 1 | 按次计费 |
| 3 | 按秒计费 |
| 5 | 按张计费 |

---

## 八、完整调用链

```
前端 POST /v1/videos
{ model: "kling-v3-pro", prompt: "...", duration: 5, aspect_ratio: "16:9", generate_audio: false }
      │
      ▼
middleware: TokenAuth() → 校验 token
middleware: Distribute() → 按模型名匹配可灵渠道（ChannelType = Kling）
      │
      ▼
controller.RelayTask()
      │
      ▼
relay.RelayTaskSubmit()
  → GetTaskAdaptor("kling") → kling.TaskAdaptor
  → ValidateRequestAndSetAction()
  → BuildRequestBody()
      normalizeKlingModelAndMode("kling-v3-pro") → realModel="kling-v3", defaultMode="pro"
      requestPayload{
        model_name: "kling-v3",
        model:      "kling-v3",
        mode:       "pro",              // req.Mode 为空时使用 defaultMode="pro"
        duration:   "5",
        generate_audio: false,
        ...
      }
  → BuildRequestURL() → POST https://api.klingai.com/v1/videos/text2video
  → BuildRequestHeader() → Authorization: Bearer <JWT>
      │
      ▼
可灵上游 API 响应: { code: 0, data: { task_id: "xxx", task_status: "submitted" } }
      │
      ▼
写入 tasks 表 → 返回前端: { id: "xxx", status: "queued" }

[后台轮询，每 15 秒]
  kling.TaskAdaptor.FetchTask()
  → GET https://api.klingai.com/v1/videos/text2video/{task_id}
  → task_status = "succeed"
  → ParseTaskResult() → video.url, video.duration 取出
  → handleVideoPerSecondBilling()
     isVideoPerSecondModel("kling-v3-pro") = true（已配置 VideoModelPricePerSecond）
     parseGenerateAudioFromUpstreamBody() → generate_audio=false → noAudio
     GetVideoModelPricePerSecondForBillingWithResolution("kling-v3-pro", false, "") → 0.112
     quota = 0.112 × 5s × 500 × 分组倍率 = 280 quota 单位

前端 GET /v1/videos/:task_id → 返回 { status: "succeeded", metadata: { url: "..." } }
```

---

## 九、接入检查清单

### Go 后端
- [x] `relay/channel/task/kling/adaptor.go`：`GetModelList()` 已添加 `kling-v3`、`kling-v3-pro`、`kling-v3-omni`、`kling-v3-omni-pro`
- [x] `relay/channel/task/kling/adaptor.go`：`normalizeKlingModelAndMode()` 已实现 `-pro` 后缀剥离 + mode 注入
- [x] `relay/channel/task/kling/adaptor.go`：`requestPayload.GenerateAudio` 字段已添加并从 `TaskSubmitReq` 透传
- [ ] 后台 → 渠道管理：可灵渠道勾选全部 7 个模型名（含 4 个新增）
- [ ] 后台 → 系统设置 → `VideoModelPricePerSecond`：添加 kling-v3 系列 `{noAudio, audio}` 价格（见第四章）

### 前端（TypeScript）
- [ ] `modelConstants.ts`：`getVideoDurationOptions` 添加 kling-v3 系列时长 `[5, 10]`
- [ ] `modelConstants.ts`：`getVideoRatios` 确认排除 `adaptive` / `21:9`
- [ ] `modelConstants.ts`：`supportsImageUpload` 中 kling-v3/kling-v3-pro = false，kling-v3-omni/kling-v3-omni-pro = true
- [ ] 如需在 UI 暴露专业模式切换：直接使用 `kling-v3-pro`/`kling-v3-omni-pro` 模型名（无需在请求中单独传 mode）

### 模型广场
- [ ] 后台添加 4 个模型元数据（标签、计费类型 quota_type=3、图标）
