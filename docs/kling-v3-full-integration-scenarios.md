# Kling V3 全链路集成场景文档

> 版本：2026-03-22 | 状态：已实现

---

## 一、已完成的改动汇总

| 层级 | 文件 | 改动内容 |
|------|------|---------|
| Go 后端 | `relay/channel/task/kling/adaptor.go` | GetModelList 加 4 个新模型；normalizeKlingModelAndMode；requestPayload.GenerateAudio；convertToRequestPayload 联动 |
| Go 后端 | `setting/ratio_setting/model_ratio.go` | defaultVideoAudioPricing 加 kling-v3/pro/omni/omni-pro，按秒计费开箱即用 |
| 前端常量 | `pages/Chat/modelConstants.ts` | getVideoDurationOptions、getVideoRatios、getVideoResolutions、supportsImageUpload、getAvailableImageToVideoModes 全部加入 kling-v3 系列 |
| 前端对话 | `pages/Chat/index.tsx` | handleVideoGeneration 加 kling-v3 分支；shouldUploadImageToOSSFirst 辅助函数；processImageFiles 对 kling-v3-omni / wan2.6 / seedream 启用 OSS-first 上传 |

---

## 二、模型能力矩阵

| 模型名 | 上游真实模型 | 默认 mode | 图片输入 | 支持时长 | 支持比例 | 分辨率 |
|--------|------------|----------|---------|---------|---------|--------|
| `kling-v3` | `kling-v3` | `std` | 否 | 5s / 10s | 16:9 / 9:16 / 1:1 / 4:3 / 3:4 | 720p / 1080p |
| `kling-v3-pro` | `kling-v3` | `pro` | 否 | 5s / 10s | 同上 | 同上 |
| `kling-v3-omni` | `kling-v3-omni` | `std` | 是（首帧/首尾帧） | 5s / 10s | 同上 | 同上 |
| `kling-v3-omni-pro` | `kling-v3-omni` | `pro` | 是（首帧/首尾帧） | 5s / 10s | 同上 | 同上 |

> **-pro 后缀说明**：Go adaptor 在 `normalizeKlingModelAndMode()` 中自动剥离 `-pro` 后缀、注入 `mode=pro`，前端无需单独传 mode 字段。

---

## 三、计费配置（已写入代码默认值）

kling-v3 系列已加入 Go 后端 `defaultVideoAudioPricing`，无需手动配置即可按秒计费。

| 模型 | 无音频 (USD/s) | 有音频 (USD/s) |
|------|--------------|--------------|
| `kling-v3` | **$0.084** | **$0.126** |
| `kling-v3-pro` | **$0.112** | **$0.168** |
| `kling-v3-omni` | **$0.084** | **$0.126** |
| `kling-v3-omni-pro` | **$0.112** | **$0.168** |

**计费触发条件**：`isVideoPerSecondModel("kling-v3-pro")` → `true`（已在 `videoModelAudioPricePerSecondMap` 中），轮询到 `succeed` 时由 `handleVideoPerSecondBilling` 自动扣费。

**默认 generate_audio = false**：前端 kling 分支固定传 `generate_audio: false`，对应 `noAudio` 定价。若未来需支持含音频版本，可在请求 metadata 中覆盖。

**计费公式**：
```
扣费 quota = 官方价格($/s) × 渠道折扣 × 实际生成秒数 × 500 × 用户分组倍率

示例（kling-v3-pro 无音频 5s）：
0.112 × 5 × 500 × 1 = 280 quota
```

> 若需要覆盖默认值，可在后台 → 系统设置 → VideoModelPricePerSecond 中配置 JSON 覆盖（优先级高于代码默认值）。

---

## 四、场景全链路流程

### 场景 A：文生视频（kling-v3-pro，5s，标准 16:9）

```
前端 Chat 页（视频模式）
  selectedModel = "kling-v3-pro"
  videoDuration = 5
  videoAspectRatio = "16:9"
  generate_audio = false（代码固定）
      │
      ▼ POST /v1/videos
{
  "model": "kling-v3-pro",
  "prompt": "城市夜晚的街道，霓虹灯闪烁",
  "duration": 5,
  "aspect_ratio": "16:9",
  "generate_audio": false
}
      │
      ▼ Go 后端 kling/adaptor.go
normalizeKlingModelAndMode("kling-v3-pro")
  → realModel = "kling-v3"
  → defaultMode = "pro"

requestPayload{
  model_name:     "kling-v3",
  model:          "kling-v3",
  mode:           "pro",        // req.Mode 为空 → 使用 defaultMode
  duration:       "5",
  aspect_ratio:   "16:9",
  generate_audio: false,
}
      │
      ▼ POST https://api.klingai.com/v1/videos/text2video
      │
      ▼ 可灵返回 { code:0, data: { task_id:"xxx", task_status:"submitted" } }
      │
      ▼ 写入 tasks 表 → 返回前端 { id:"xxx", status:"queued" }

[后台轮询，succeed 后]
handleVideoPerSecondBilling()
  → parseGenerateAudioFromUpstreamBody → generate_audio=false → noAudio
  → GetVideoModelPricePerSecondForBillingWithResolution("kling-v3-pro", false, "")
    → videoModelAudioPricePerSecondMap["kling-v3-pro"].NoAudio = 0.112
  → quota = 0.112 × 5 × 500 × groupRatio = 280 quota
```

---

### 场景 B：图生视频（kling-v3-omni，首帧模式，5s）

```
前端
  selectedModel = "kling-v3-omni"
  imageGenerationMode = "first_frame"
  uploadedImages[0] = "https://oss.cdn.example.com/photo.jpg"
      │
      ▼ OSS-first 上传（processImageFiles）
  shouldUploadImageToOSSFirst("kling-v3-omni", "video") = true
  → uploadService.uploadFile(file) → { url: "https://oss.cdn.example.com/photo.jpg" }
  → uploadedImages[0] = "https://oss.cdn.example.com/photo.jpg"（HTTP URL，非 base64）

      │
      ▼ handleVideoGeneration（kling 分支）
requestData = {
  model: "kling-v3-omni",
  prompt: "人物缓缓回头",
  duration: 5,
  aspect_ratio: "16:9",
  generate_audio: false,
  image: "https://oss.cdn.example.com/photo.jpg",
}
      │
      ▼ Go adaptor
normalizeKlingModelAndMode("kling-v3-omni")
  → realModel = "kling-v3-omni", defaultMode = "std"

requestPayload{
  model_name:     "kling-v3-omni",
  model:          "kling-v3-omni",
  mode:           "std",
  image:          "https://oss.cdn.example.com/photo.jpg",
  duration:       "5",
  aspect_ratio:   "16:9",
  generate_audio: false,
}
      │
      ▼ BuildRequestURL → image 非空 → POST .../v1/videos/image2video
      │
      ▼ 可灵处理首帧 → 返回 task_id
      │
      ▼ 轮询成功 → handleVideoPerSecondBilling
  → noAudio → 0.084 × 5 × 500 = 210 quota
```

---

### 场景 C：图生视频（kling-v3-omni-pro，首尾帧模式，10s）

```
uploadedImages = ["https://oss.../start.jpg", "https://oss.../end.jpg"]
imageGenerationMode = "first_last_frame"

requestData = {
  model: "kling-v3-omni-pro",
  prompt: "人物从左走到右",
  duration: 10,
  aspect_ratio: "16:9",
  generate_audio: false,
  image: "https://oss.../start.jpg",       // 首帧
  image_tail: "https://oss.../end.jpg",    // 尾帧
}

Go adaptor → mode="pro", model_name="kling-v3-omni"
→ 按秒计费：0.112 × 10 × 500 = 560 quota
```

---

## 五、OSS-first 图片上传逻辑

### 触发条件（`shouldUploadImageToOSSFirst`）

```typescript
// 在 processImageFiles 中，以下模型上传图片时优先走 OSS
model.startsWith('kling-v3-omni')  // kling-v3-omni / kling-v3-omni-pro
model.includes('wan2.6')           // wan2.6-i2v / wan2.6-r2v-flash 等
model.includes('seedream')         // doubao-seedream-4-0 等图生图模型
```

### 上传流程

```
用户选择本地图片
      │
      ▼ validateImageFile（格式/尺寸校验）
      │
      ├─ 验证失败 → toast.error，跳过
      │
      ├─ 有裁剪 base64 → 直接存 uploadedImages（跳过 OSS）
      │
      └─ shouldUploadImageToOSSFirst = true
           │
           ▼ uploadService.uploadFile(file)   // POST /resource/oss/upload，需登录 token
           │
           ├─ 成功 → uploadedImages[i] = "https://cdn.example.com/xxx.jpg"（HTTP URL）
           │         后续传给上游时直接用 HTTP URL，无大 base64 体积
           │
           └─ 失败（网络/权限）→ 降级 FileReader.readAsDataURL
                               → uploadedImages[i] = "data:image/png;base64,..."（base64）
```

**优先级原则**：
1. 裁剪 base64（来自 `validateImageFile` 的裁剪结果）→ 直接用，不走 OSS
2. OSS HTTP URL → 上游直接下载，避免 base64 体积膨胀（约 1.33×）
3. base64 fallback → OSS 失败时降级，保证功能可用

---

## 六、VideoModelPricePerSecond 后台配置

默认值已在代码中写入，若需要按实际成本/利润调整，在后台 → 系统设置中配置：

```json
// VideoModelPricePerSecond（覆盖代码默认值）
{
  "kling-v3":          { "noAudio": 0.084, "audio": 0.126 },
  "kling-v3-pro":      { "noAudio": 0.112, "audio": 0.168 },
  "kling-v3-omni":     { "noAudio": 0.084, "audio": 0.126 },
  "kling-v3-omni-pro": { "noAudio": 0.112, "audio": 0.168 }
}
```

解析路径（`buildVideoModelPriceCaches`）：
- 值为 `{noAudio: N, audio: N}` → `videoModelAudioPricePerSecondMap[key] = VideoAudioPricing{NoAudio, Audio}`
- `isVideoPerSecondModel` 返回 `true` → `handleVideoPerSecondBilling` 走按秒计费路径

---

## 七、模型广场展示配置

在后台管理 → 模型管理中添加如下元数据（无需改代码）：

| 字段 | kling-v3 | kling-v3-pro | kling-v3-omni | kling-v3-omni-pro |
|------|----------|--------------|---------------|-------------------|
| showTab | `3`（视频生成）| `3` | `3` | `3` |
| quotaType | `3`（按秒计费）| `3` | `3` | `3` |
| modelPrice | `0.084` | `0.112` | `0.084` | `0.112` |
| videoAudioPricing | `{"noAudio":0.084,"audio":0.126}` | `{"noAudio":0.112,"audio":0.168}` | 同左 | 同左 |
| tags | `["可灵","视频生成","文生视频"]` | `["可灵","视频生成","专业模式"]` | `["可灵","视频生成","图生视频","多模态"]` | `["可灵","视频生成","图生视频","专业模式"]` |

模型广场展示效果（`quotaType=3`）：
- 主价格卡片显示 `modelPrice`（无音频单价，例如 `$0.084 / 秒`）
- 若配置了 `videoAudioPricing` JSON，会展示 无音频 / 有音频 双档价格

---

## 八、接入检查清单（最终状态）

### Go 后端 ✅ 全部完成
- [x] `adaptor.go`：GetModelList 含 4 个 kling-v3 模型名
- [x] `adaptor.go`：normalizeKlingModelAndMode 实现 `-pro` 剥离
- [x] `adaptor.go`：requestPayload.GenerateAudio 字段透传
- [x] `model_ratio.go`：defaultVideoAudioPricing 加入 4 个 kling-v3 定价

### 前端 Chat 页 ✅ 全部完成
- [x] `modelConstants.ts`：getVideoDurationOptions — kling-v3* 返回 `[5, 10]`
- [x] `modelConstants.ts`：getVideoRatios — kling-v3* 过滤 `adaptive`/`21:9`
- [x] `modelConstants.ts`：getVideoResolutions — kling-v3* 仅 720p/1080p
- [x] `modelConstants.ts`：supportsImageUpload — kling-v3/pro = false；omni 系列 = true
- [x] `modelConstants.ts`：getAvailableImageToVideoModes — kling-v3-omni* 支持 first_frame/first_last_frame
- [x] `index.tsx`：handleVideoGeneration 加入 kling 分支（duration/aspect_ratio/generate_audio/image/image_tail）
- [x] `index.tsx`：shouldUploadImageToOSSFirst — kling-v3-omni/wan2.6/seedream 启用 OSS-first
- [x] `index.tsx`：processImageFiles — video/image 模式均加入 OSS-first + base64 fallback

### 后台管理配置 ⬜ 待操作
- [ ] 渠道管理：可灵渠道勾选 kling-v3 / kling-v3-pro / kling-v3-omni / kling-v3-omni-pro
- [ ] 模型广场：添加 4 个模型元数据（showTab=3, quotaType=3, videoAudioPricing JSON）

---

## 九、注意事项

1. **`-pro` 模型名不透传给可灵上游**：前端传 `kling-v3-pro`，Go adaptor 会自动剥离为 `kling-v3` + `mode=pro`，可灵 API 只接受原生模型名。

2. **billing_processed 幂等保护**：`handleVideoPerSecondBilling` 首先检查 `task.Data["billing_processed"]`，重复轮询不会重复扣费。

3. **OSS-first 仅对支持 token 的用户有效**：`uploadService.uploadFile` 调用 `POST /resource/oss/upload` 需要登录 token，若用户未登录会失败并自动降级为 base64，不影响功能可用性。

4. **generate_audio 默认 false**：kling 分支固定 `generate_audio: false`，对应 noAudio 定价。若未来产品需要支持用户切换含音频模式，需要在 UI 加一个 toggle 并将 `true` 透传到请求。

5. **aspect_ratio 字段名**：Kling 上游接受 `aspect_ratio`（snake_case），Go adaptor 的 `requestPayload.AspectRatio` 的 JSON tag 就是 `aspect_ratio`。前端传的 `requestData.aspect_ratio` 通过 `TaskSubmitReq.Metadata` 透传，在 `convertToRequestPayload` 末尾由 `json.Unmarshal(metaBytes, &r)` 写入，覆盖了 `getAspectRatio(req.Size)` 的默认值。
