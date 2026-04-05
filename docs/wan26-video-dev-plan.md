# 万相 wan2.6 系列视频模型接入开发计划

## 一、概述

本文档规划在 `gravitex-api-cli`（前端）和 `gravitex-api`（Go 后端）中接入阿里万相 wan2.6 系列视频模型，共涉及 5 个模型：

| 模型名 | 类型 | 说明 |
|--------|------|------|
| `wan2.6-t2v` | 文生视频 | 支持 2-15 秒，720P/1080P，支持 shot_type |
| `wan2.6-i2v-flash` | 图生视频（快速版） | 支持 2-15 秒，720P/1080P，支持有声/无声切换 |
| `wan2.6-i2v` | 图生视频（标准版） | 支持 2-15 秒，720P/1080P |
| `wan2.6-r2v-flash` | 参考生视频（快速版） | 支持 2-10 秒，720P/1080P，支持有声/无声切换 |
| `wan2.6-r2v` | 参考生视频（标准版） | 支持 2-10 秒，720P/1080P |

**重要差异（相比 wan2.5）：**
- wan2.6 不支持 480P（仅 720P / 1080P）
- wan2.6-t2v 使用 `size` 字段（如 `1280*720`），与 wan2.5-t2v 一致
- wan2.6-i2v 使用 `resolution` 字段（如 `720P`），与 wan2.5-i2v 一致
- wan2.6-r2v 使用 `size` 字段（如 `1280*720`），不走 resolution
- 参考生视频通过 `reference_urls` 传入图像/视频数组（最多 5 个）
- `shot_type`（single/multi）：t2v 和 i2v 需要 `prompt_extend=true` 时才生效；r2v 无此限制
- wan2.6-i2v-flash 和 wan2.6-r2v-flash 支持 `audio` 参数控制是否生成有声视频
- **计费**：所有 wan2.6 模型均按 `duration（秒）× 单价` 计费，价格配置在 `VideoModelPricePerSecond`

---

## 二、API 接口参数速查

### 2.1 wan2.6-t2v（文生视频）

**请求：**
```json
{
  "model": "wan2.6-t2v",
  "input": {
    "prompt": "...",
    "audio_url": "https://..."  // 可选，自定义音频
  },
  "parameters": {
    "size": "1280*720",          // 必选，720P/1080P 的所有宽高比枚举值
    "duration": 5,               // [2, 15] 整数，默认 5
    "prompt_extend": true,       // 可选，默认 true
    "shot_type": "single",       // 可选，single/multi，需 prompt_extend=true
    "watermark": false,          // 可选
    "seed": 12345                // 可选
  }
}
```

**size 枚举（720P）：** `1280*720`(16:9)、`720*1280`(9:16)、`960*960`(1:1)、`1088*832`(4:3)、`832*1088`(3:4)

**size 枚举（1080P）：** `1920*1080`(16:9)、`1080*1920`(9:16)、`1440*1440`(1:1)、`1632*1248`(4:3)、`1248*1632`(3:4)

**usage 响应：**
```json
{
  "duration": 10, "size": "1280*720", "SR": 720,
  "input_video_duration": 0, "output_video_duration": 10, "video_count": 1
}
```

---

### 2.2 wan2.6-i2v-flash / wan2.6-i2v（图生视频）

**请求：**
```json
{
  "model": "wan2.6-i2v-flash",
  "input": {
    "prompt": "...",
    "img_url": "https://...",    // 必选，首帧图片 URL 或 base64
    "audio_url": "https://..."  // 可选
  },
  "parameters": {
    "resolution": "720P",        // 720P 或 1080P（i2v 用 resolution 不用 size）
    "duration": 5,               // [2, 15] 整数
    "prompt_extend": true,
    "shot_type": "single",       // 需 prompt_extend=true
    "audio": true,               // 仅 i2v-flash 支持，控制有声/无声
    "watermark": false,
    "seed": 12345
  }
}
```

**usage 响应：**
```json
{
  "duration": 10, "SR": 720, "audio": true,
  "input_video_duration": 0, "output_video_duration": 10, "video_count": 1
}
```

---

### 2.3 wan2.6-r2v-flash / wan2.6-r2v（参考生视频）

**请求：**
```json
{
  "model": "wan2.6-r2v-flash",
  "input": {
    "prompt": "character1在沙发上开心的看电影",  // 用 character1/character2 引用角色
    "reference_urls": [           // 必选，图像或视频 URL 数组（最多 5 个）
      "https://.../role1.mp4",    // 第1个 → character1
      "https://.../role2.jpg"     // 第2个 → character2
    ]
  },
  "parameters": {
    "size": "1280*720",           // 720P/1080P 枚举值（同 t2v 的 size 格式）
    "duration": 5,                // [2, 10] 整数（r2v 最长 10 秒）
    "shot_type": "single",        // single/multi，无需 prompt_extend
    "audio": true,                // 仅 r2v-flash 支持
    "watermark": false,
    "seed": 12345
  }
}
```

**usage 响应（含 input_video_duration）：**
```json
{
  "duration": 10.0, "size": "1280*720", "SR": 720,
  "input_video_duration": 5, "output_video_duration": 5, "video_count": 1
}
```

> **计费说明**：r2v 按 `duration = input_video_duration + output_video_duration` 收费，即参考视频时长也计入计费。

---

## 三、Go 后端开发计划

### 涉及文件

| 文件 | 改动类型 |
|------|---------|
| `relay/channel/task/ali/adaptor.go` | 新增 wan2.6 请求构造和 usage 解析分支 |
| `relay/channel/task/ali/constants.go` | 新增 wan2.6 模型名常量 |
| `relay/common/relay_info.go` | TaskSubmitReq 新增 wan2.6 专用字段 |
| `setting/ratio_setting/model_ratio.go` | VideoModelPricePerSecond 新增 wan2.6 价格 |

---

### Task 1：`relay/common/relay_info.go` — 扩展 TaskSubmitReq

在 `TaskSubmitReq` 结构体中新增以下字段（wan2.6 专用，i2v 和 r2v 各有侧重）：

```go
// wan2.6 i2v-flash 和 r2v-flash 专用：控制有声/无声（true=有声，false=无声，默认 true）
Audio *bool `json:"audio,omitempty"`

// wan2.6 r2v 专用：参考文件 URL 数组（图像或视频，最多 5 个）
ReferenceUrls []string `json:"reference_urls,omitempty"`

// wan2.6 / wan2.5 通用：镜头类型（single/multi）
ShotType string `json:"shot_type,omitempty"`

// wan2.6 通用：提示词改写开关（true/false）
PromptExtend *bool `json:"prompt_extend,omitempty"`
```

> 注：`InputReference`（现有字段）用于 i2v 的 `img_url`；`Size` 和 `Duration` 已有，可复用。

---

### Task 2：`relay/channel/task/ali/constants.go` — 新增模型常量

```go
const (
    ModelWan26T2V      = "wan2.6-t2v"
    ModelWan26I2VFlash = "wan2.6-i2v-flash"
    ModelWan26I2V      = "wan2.6-i2v"
    ModelWan26R2VFlash = "wan2.6-r2v-flash"
    ModelWan26R2V      = "wan2.6-r2v"
)
```

在 `GetModelList()` 中追加上述 5 个模型名。

---

### Task 3：`relay/channel/task/ali/adaptor.go` — 请求构造分支

在 `convertToAliRequest()` 函数中，参照 wan2.5 分支，**新增 wan2.6 分支**：

#### 3.1 wan2.6-t2v 构造（与 wan2.5-t2v 基本一致，复用 size 字段）

```go
case strings.Contains(req.Model, "wan2.6-t2v"):
    aliReq.Input.AudioUrl = req.AudioUrl      // 可选
    aliReq.Parameters.Size = req.Size          // 如 "1280*720"
    aliReq.Parameters.Duration = req.Duration
    aliReq.Parameters.PromptExtend = resolvePromptExtend(req.PromptExtend)  // 默认 true
    aliReq.Parameters.ShotType = req.ShotType  // 仅 prompt_extend=true 时有效
    aliReq.Parameters.Watermark = resolveWatermark(req.Watermark)
    aliReq.Parameters.Seed = req.Seed
```

#### 3.2 wan2.6-i2v 构造（用 resolution，与 wan2.5-i2v 相似，新增 audio/shot_type）

```go
case strings.Contains(req.Model, "wan2.6-i2v"):
    aliReq.Input.ImgUrl = req.InputReference   // 首帧图片 URL 或 base64
    aliReq.Input.AudioUrl = req.AudioUrl
    aliReq.Parameters.Resolution = normalizeResolution(req.Resolution)  // "720P" / "1080P"
    aliReq.Parameters.Duration = req.Duration
    aliReq.Parameters.PromptExtend = resolvePromptExtend(req.PromptExtend)
    aliReq.Parameters.ShotType = req.ShotType
    // audio 仅 i2v-flash 支持
    if strings.Contains(req.Model, "i2v-flash") && req.Audio != nil {
        aliReq.Parameters.Audio = req.Audio
    }
    aliReq.Parameters.Watermark = resolveWatermark(req.Watermark)
    aliReq.Parameters.Seed = req.Seed
```

#### 3.3 wan2.6-r2v 构造（新增 reference_urls，用 size 格式）

```go
case strings.Contains(req.Model, "wan2.6-r2v"):
    aliReq.Input.ReferenceUrls = req.ReferenceUrls   // 图像/视频 URL 数组
    aliReq.Parameters.Size = req.Size                 // 如 "1280*720"
    aliReq.Parameters.Duration = req.Duration
    aliReq.Parameters.ShotType = req.ShotType         // 无需 prompt_extend
    // audio 仅 r2v-flash 支持
    if strings.Contains(req.Model, "r2v-flash") && req.Audio != nil {
        aliReq.Parameters.Audio = req.Audio
    }
    aliReq.Parameters.Watermark = resolveWatermark(req.Watermark)
    aliReq.Parameters.Seed = req.Seed
```

#### 3.4 OtherRatios 计费秒数（全部 wan2.6 统一处理）

```go
// 在 ProcessAliOtherRatios 中新增 wan2.6 条目
// 价格直接配置在 VideoModelPricePerSecond，此处只传秒数
if strings.Contains(req.Model, "wan2.6") {
    info.PriceData.OtherRatios = map[string]float64{
        "seconds": float64(duration),
    }
}
```

---

### Task 4：`relay/channel/task/ali/adaptor.go` — 请求/响应结构体扩展

在阿里 DTO 结构体（`AliVideoInput`、`AliVideoParameters`）中补充字段：

```go
// AliVideoInput 新增
ImgUrl        string   `json:"img_url,omitempty"`       // i2v 首帧
AudioUrl      string   `json:"audio_url,omitempty"`     // 音频 URL
ReferenceUrls []string `json:"reference_urls,omitempty"` // r2v 参考文件数组

// AliVideoParameters 新增
ShotType      string `json:"shot_type,omitempty"`        // single/multi
Audio         *bool  `json:"audio,omitempty"`            // 有声/无声（flash 系列）
PromptExtend  *bool  `json:"prompt_extend,omitempty"`    // 提示词改写
```

---

### Task 5：`setting/ratio_setting/model_ratio.go` — 价格配置

在 `VideoModelPricePerSecond`（或新增 `defaultVideoModelPricePerSecond`）中添加 wan2.6 价格（**价格待确认，此处为占位**）：

```go
// wan2.6 系列按秒计费（价格单位：美元/秒，待填入实际价格）
"wan2.6-t2v":       0.xxx,   // 待确认：720P 和 1080P 价格不同，参考 wan2.5 分辨率倍率机制
"wan2.6-i2v-flash": 0.xxx,   // 有声/无声价格不同，需特殊处理
"wan2.6-i2v":       0.xxx,
"wan2.6-r2v-flash": 0.xxx,
"wan2.6-r2v":       0.xxx,
```

> **注意**：wan2.6 720P 和 1080P 价格不同（与 wan2.5 一样），实现时应沿用 wan2.5 的分辨率倍率机制（`ProcessAliOtherRatios`），而非把两个价格点都放在 `VideoModelPricePerSecond`。
> wan2.6-i2v-flash 和 wan2.6-r2v-flash 还有有声/无声的价格差异，需要仿照 Veo 系列的 `VideoAudioPricing` 机制处理。

---

## 四、前端开发计划

### 涉及文件

| 文件 | 改动类型 |
|------|---------|
| `pages/Chat/modelConstants.ts` | 新增 wan2.6 能力配置和参数选项 |
| `pages/Chat/index.tsx` | 新增 state、request 构造分支、UI 渲染 |
| `services/videoGenerateService.ts` | VideoGenerateRequest 接口扩展 |

---

### Task 6：`services/videoGenerateService.ts` — 接口字段扩展

在 `VideoGenerateRequest` 中新增以下字段：

```typescript
// wan2.6 通用
shot_type?: string;         // 'single' | 'multi'
prompt_extend?: boolean;    // 提示词改写开关

// wan2.6-i2v-flash / r2v-flash 专用
audio?: boolean;            // true=有声，false=无声

// wan2.6-r2v 专用
reference_urls?: string[];  // 参考文件 URL 数组（图像或视频）
```

---

### Task 7：`pages/Chat/modelConstants.ts` — 能力开关与参数选项

#### 7.1 `supportsSmartRewrite`（复用 prompt_extend 开关）

```typescript
supportsSmartRewrite: (model: string) => {
  return model.includes('wan2.5') || model.includes('wan2.6');
}
```

#### 7.2 `supportsAudioUpload`（音频文件上传）

```typescript
supportsAudioUpload: (model: string) => {
  // t2v 和 i2v 支持 audio_url；r2v 不支持
  return model.includes('wan2.5') ||
         model.includes('wan2.6-t2v') ||
         model.includes('wan2.6-i2v');
}
```

#### 7.3 `supportsWatermark`

```typescript
supportsWatermark: (model: string) => {
  return model.includes('doubao') || model.includes('seedance') || model.includes('wan2.6');
}
```

#### 7.4 `supportsAudioToggle`（新增：有声/无声开关，仅 flash 系列）

```typescript
supportsAudioToggle: (model: string) => {
  return model === 'wan2.6-i2v-flash' || model === 'wan2.6-r2v-flash';
}
```

#### 7.5 `supportsReferenceUrls`（新增：参考生视频媒体上传）

```typescript
supportsReferenceUrls: (model: string) => {
  return model.includes('wan2.6-r2v');
}
```

#### 7.6 `supportsShotType`（新增：shot_type 下拉）

```typescript
supportsShotType: (model: string) => {
  // r2v 无条件支持；t2v 和 i2v 需要 prompt_extend=true 时才生效（UI 层控制显隐）
  return model.includes('wan2.6');
}
```

#### 7.7 `getVideoDurationOptions`

```typescript
// wan2.6 t2v / i2v：2-15 秒
if (model.includes('wan2.6-t2v') || model.includes('wan2.6-i2v')) {
  return [2,3,4,5,6,7,8,9,10,11,12,13,14,15];
}
// wan2.6 r2v：2-10 秒
if (model.includes('wan2.6-r2v')) {
  return [2,3,4,5,6,7,8,9,10];
}
```

#### 7.8 `getVideoResolutions`

```typescript
// wan2.6 不支持 480P
if (model.includes('wan2.6')) {
  return VIDEO_RESOLUTIONS.filter(r => r.id !== '480p');
}
```

#### 7.9 `getVideoRatios`

```typescript
// wan2.6 不支持 adaptive 和 21:9
if (model.includes('wan2.6')) {
  return VIDEO_RATIOS.filter(r => r.id !== 'adaptive' && r.id !== '21:9');
}
```

#### 7.10 `supportsImageUpload`（video 模式）

```typescript
// wan2.6-t2v 和 wan2.6-r2v 不支持图片上传（t2v 纯文生，r2v 用 reference_urls 单独上传）
if (mode === 'video') {
  if (model === 'wan2.6-t2v') return false;
  if (model.includes('wan2.6-r2v')) return false;
  // wan2.6-i2v 支持首帧图片上传
  return true;
}
```

#### 7.11 `getWan26T2VSize`（新增：与 wan2.5-t2v 共用映射表，但无 480P）

```typescript
// wan2.6-t2v 和 wan2.6-r2v 使用同一 size 映射（720P/1080P）
// 可直接复用 getWan25T2VSize，只要 UI 层过滤掉 480P 分辨率选项即可
```

---

### Task 8：`pages/Chat/index.tsx` — State 声明

新增以下 state（参照 wan2.5 模式）：

```typescript
// wan2.6 通用参数（复用 wan25SmartRewrite / wan25AudioUrl / wan25AudioFile / wan25Seed 等）
const [wan26ShotType, setWan26ShotType] = useState<'single' | 'multi'>('single');
const [wan26AudioEnabled, setWan26AudioEnabled] = useState<boolean>(true); // 有声/无声（flash 专用）
const [wan26Resolution, setWan26Resolution] = useState<'720p' | '1080p'>('720p');
const [wan26AspectRatio, setWan26AspectRatio] = useState<'16:9' | '9:16' | '1:1' | '4:3' | '3:4'>('16:9');

// wan2.6-r2v 专用
const [wan26ReferenceFiles, setWan26ReferenceFiles] = useState<File[]>([]);
const [wan26ReferenceUrls, setWan26ReferenceUrls] = useState<string[]>([]);
```

> **建议**：wan2.6 的 `prompt_extend`（智能改写）和 `audio_url` 直接复用已有的 `wan25SmartRewrite` 和 `wan25AudioUrl` state，无需新增。`seed` 也复用 `wan25Seed`。

---

### Task 9：`pages/Chat/index.tsx` — Request 构造分支

在 `handleVideoGeneration()` 的 wan2.5 分支之后，**新增 wan2.6 分支**：

#### 9.1 wan2.6-t2v

```typescript
else if (selectedModel === 'wan2.6-t2v') {
  requestData.duration = videoDuration;
  requestData.size = ModelCapabilities.getWan25T2VSize(wan26Resolution, wan26AspectRatio);
  requestData.smart_rewrite = wan25SmartRewrite;   // 复用，对应 prompt_extend
  requestData.shot_type = wan26ShotType;            // 仅 prompt_extend=true 时后端生效
  requestData.watermark = watermark;
  requestData.audio_url = wan25AudioUrl || undefined;
  requestData.seed = wan25Seed && wan25Seed > 0 ? wan25Seed : undefined;
}
```

#### 9.2 wan2.6-i2v-flash / wan2.6-i2v

```typescript
else if (selectedModel.includes('wan2.6-i2v')) {
  requestData.resolution = wan26Resolution;         // "720p" / "1080p"（后端转大写 720P/1080P）
  requestData.duration = videoDuration;
  requestData.smart_rewrite = wan25SmartRewrite;
  requestData.shot_type = wan26ShotType;
  requestData.watermark = watermark;
  requestData.audio_url = wan25AudioUrl || undefined;
  requestData.seed = wan25Seed && wan25Seed > 0 ? wan25Seed : undefined;
  // i2v 需要首帧图片
  if (images && images.length > 0) {
    requestData.input_reference = images[0];        // base64 或 OSS URL
  }
  // flash 专用：有声/无声开关
  if (selectedModel === 'wan2.6-i2v-flash') {
    requestData.audio = wan26AudioEnabled;
  }
}
```

#### 9.3 wan2.6-r2v-flash / wan2.6-r2v

```typescript
else if (selectedModel.includes('wan2.6-r2v')) {
  requestData.size = ModelCapabilities.getWan25T2VSize(wan26Resolution, wan26AspectRatio);
  requestData.duration = videoDuration;
  requestData.shot_type = wan26ShotType;             // 无需 prompt_extend
  requestData.watermark = watermark;
  requestData.seed = wan25Seed && wan25Seed > 0 ? wan25Seed : undefined;
  requestData.reference_urls = wan26ReferenceUrls;   // 已上传的图片/视频 OSS URL 数组
  // flash 专用：有声/无声开关
  if (selectedModel === 'wan2.6-r2v-flash') {
    requestData.audio = wan26AudioEnabled;
  }
}
```

---

### Task 10：`pages/Chat/index.tsx` — UI 渲染

在视频设置面板中，按如下逻辑新增/扩展 UI 组件：

#### 10.1 分辨率选择器

```
复用现有分辨率 <select>，条件：
- selectedModel.includes('wan2.6') 时读写 wan26Resolution
- wan2.6 的选项由 getVideoResolutions('wan2.6-t2v') 过滤掉 480P 后生成
```

#### 10.2 宽高比选择器

```
复用现有宽高比 <select>，条件：
- selectedModel.includes('wan2.6-t2v') 或 wan2.6-r2v 时读写 wan26AspectRatio
- i2v 不显示宽高比（由首帧图片决定）
- wan2.6 的选项过滤掉 adaptive 和 21:9
```

#### 10.3 时长选择器

```
复用现有时长 <select>，由 getVideoDurationOptions(selectedModel) 自动适配
```

#### 10.4 Prompt Rewrite 开关

```
复用现有 wan25SmartRewrite 开关
条件：ModelCapabilities.supportsSmartRewrite(selectedModel)
```

#### 10.5 Shot Type 下拉（新增）

```tsx
{ModelCapabilities.supportsShotType(selectedModel) && (
  // r2v 始终显示；t2v/i2v 在 prompt_extend=true 时显示
  (!selectedModel.includes('wan2.6-r2v') ? wan25SmartRewrite : true) && (
    <div className="space-y-2">
      <label>Shot type</label>
      <select value={wan26ShotType} onChange={e => setWan26ShotType(e.target.value as 'single' | 'multi')}>
        <option value="single">Single shot（单镜头）</option>
        <option value="multi">Multi shot（多镜头）</option>
      </select>
    </div>
  )
)}
```

#### 10.6 有声/无声开关（新增，仅 flash 系列）

```tsx
{ModelCapabilities.supportsAudioToggle(selectedModel) && (
  <div className="flex items-center justify-between">
    <label>Generate audio</label>
    <input type="checkbox" checked={wan26AudioEnabled}
           onChange={e => setWan26AudioEnabled(e.target.checked)} />
  </div>
)}
```

#### 10.7 音频文件上传

```
复用现有 supportsAudioUpload 控制的 wan25AudioFile 上传区块
```

#### 10.8 参考文件上传（新增，仅 r2v）

```tsx
{ModelCapabilities.supportsReferenceUrls(selectedModel) && (
  <div className="space-y-2">
    <label>Reference files（参考图像/视频，最多 5 个）</label>
    {/* 支持多文件上传：图像（jpg/png/webp/bmp）或视频（mp4/mov） */}
    {/* 上传成功后写入 wan26ReferenceUrls（OSS URL 数组） */}
    {/* 显示已上传文件列表，支持逐个删除 */}
    {/* 在 prompt 中提示用：character1 = 第1个文件，character2 = 第2个，以此类推 */}
  </div>
)}
```

#### 10.9 水印开关

```
复用现有 supportsWatermark 控制的水印开关
```

#### 10.10 随机种子

```
复用现有 wan25Seed 的 input（条件扩展到 wan2.6）
```

---

## 五、开发顺序建议

```
后端：
1. Task 1 → relay_info.go 字段扩展
2. Task 2 → 模型常量注册
3. Task 3 → 请求构造分支（先 t2v，再 i2v，最后 r2v）
4. Task 4 → 阿里 DTO 结构体扩展
5. Task 5 → 价格配置（需确认实际价格后填入）

前端：
6. Task 6 → service 接口扩展
7. Task 7 → modelConstants 能力开关（批量更新）
8. Task 8 → index.tsx state 声明
9. Task 9 → index.tsx request 构造
10. Task 10 → index.tsx UI 渲染（依次：t2v → i2v → r2v）
```

---

## 六、关键注意事项

1. **r2v 计费包含参考视频时长**：`duration = input_video_duration + output_video_duration`，后端计费时用 `usage.duration` 而非请求参数的 `duration`。

2. **i2v 不传 size**：i2v 系列用 `resolution`（`720P`/`1080P`），后端会根据首帧图片宽高比自动决定输出分辨率；t2v 和 r2v 用 `size`（精确像素字符串）。

3. **shot_type 生效条件**：t2v 和 i2v 需要 `prompt_extend=true`，r2v 无此限制。前端在 t2v/i2v 时应仅在 smart_rewrite 开启时发送 shot_type（或始终发送，让后端处理）。

4. **r2v 的 prompt 写法**：用 `character1`、`character2` 等引用参考角色，需在前端 prompt 输入框旁增加说明文字。

5. **r2v 参考文件上传**：参考视频需要先上传到 OSS 获取临时 URL，然后通过 `reference_urls` 传递；图片可以传 base64 或 URL。上传逻辑可复用现有的 `handleAudioUpload`（OSS 上传）的模式。

6. **wan2.6 不支持 480P**：UI 层分辨率选项和 `getWan25T2VSize` 调用时需确保不传 480P 相关的 size 字符串。

7. **价格配置优先级**：wan2.6 有 720P/1080P 价格差异 + flash 系列有声/无声价格差异，建议优先复用 wan2.5 的分辨率倍率机制（`ProcessAliOtherRatios`），再为 flash 系列单独处理 audio 价格开关，参照 Veo 系列的 `VideoAudioPricing` 实现。
