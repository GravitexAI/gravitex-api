# 为什么 UpToken 需要独立渠道类型，而不能使用「自定义渠道」

## 结论

UpToken 视频生成 API **无法**使用「自定义渠道」（ChannelTypeCustom = 8），**必须**新增独立渠道类型（ChannelTypeUptoken = 59）。

---

## 一、自定义渠道的工作原理

自定义渠道（type 8）的设计目标是兼容 **OpenAI Chat/Completion 协议**的第三方服务。其工作方式：

1. `ChannelType2APIType(8)` 没有专门的 case，走 fallback 返回 `APITypeOpenAI`
2. `GetAdaptor(APITypeOpenAI)` 返回 `openai.Adaptor{}`
3. 请求通过 OpenAI 协议适配器转发到用户配置的 base_url

这意味着自定义渠道**只能处理文本对话类请求**（`/v1/chat/completions` 等），本质上是一个"可自定义 URL 的 OpenAI 兼容渠道"。

---

## 二、视频任务的路由机制

视频生成任务（`/v1/video/generations`、`/v1/videos`）走的是一条**完全不同的代码路径**，与文本对话互不相通：

```
文本对话：Router → Relay → GetAdaptor(apiType) → channel.Adaptor → 转发
视频任务：Router → RelayTaskSubmit → GetTaskAdaptor(platform) → channel.TaskAdaptor → 转发
```

关键区别：

| | 文本对话 | 视频任务 |
|---|---|---|
| 路由入口 | `/v1/chat/completions` | `/v1/video/generations` |
| 适配器查找 | `GetAdaptor(apiType)` | `GetTaskAdaptor(platform)` |
| 适配器接口 | `channel.Adaptor` | `channel.TaskAdaptor` |
| 自定义渠道支持 | 有 fallback 到 OpenAI | **无 fallback，返回 nil** |

---

## 三、自定义渠道用于视频任务时的失败链路

当尝试用自定义渠道（type 8）提交视频任务时，完整的失败路径：

```
1. 请求到达 /v1/video/generations
2. Distribute 中间件选中 type=8 的渠道，设置 channel_type=8
3. GetTaskPlatform(c) 读取 channel_type=8，返回 platform="8"
4. GetTaskAdaptor("8") 在 switch 中查找 case 8 → 不存在
5. 返回 nil
6. RelayTaskSubmit 判断 adaptor == nil
7. 返回 HTTP 400: "invalid api platform: 8"
```

`GetTaskAdaptor` 的注册表（`relay/relay_adaptor.go`）：

```go
func GetTaskAdaptor(platform constant.TaskPlatform) channel.TaskAdaptor {
    // ...
    switch channelType {
    case 17:  return &taskali.TaskAdaptor{}       // 阿里
    case 50:  return &kling.TaskAdaptor{}          // 可灵
    case 51:  return &taskjimeng.TaskAdaptor{}     // 即梦
    case 41:  return &taskvertex.TaskAdaptor{}     // Vertex AI
    case 52:  return &taskVidu.TaskAdaptor{}       // Vidu
    case 54,45: return &taskdoubao.TaskAdaptor{}   // 豆包视频
    case 55,1:  return &tasksora.TaskAdaptor{}     // Sora
    case 58,3:  return &taskazurevideo.TaskAdaptor{} // Azure Video
    case 24:  return &taskGemini.TaskAdaptor{}     // Gemini
    case 35:  return &hailuo.TaskAdaptor{}         // 海螺
    case 59:  return &taskuptoken.TaskAdaptor{}    // UpToken ← 新增
    // case 8: 不存在！
    }
    return nil  // ← 自定义渠道走到这里
}
```

**没有 fallback 机制**——与文本对话不同，视频任务路由要求每个渠道类型必须有对应的 `TaskAdaptor` 实现。

---

## 四、为什么不能给 type 8 加一个通用 TaskAdaptor

即使为自定义渠道注册一个 TaskAdaptor，仍然无法工作，原因如下：

### 4.1 TaskAdaptor 接口要求特定实现

`TaskAdaptor` 接口包含以下方法，每个上游 API 的实现都不同：

```go
type TaskAdaptor interface {
    Init(info *relaycommon.RelayInfo)
    ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError
    BuildRequestURL(info *relaycommon.RelayInfo) (string, error)
    BuildRequestHeader(c *gin.Context, req *http.Request, info *relaycommon.RelayInfo) error
    BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error)
    DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (*http.Response, error)
    DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (string, []byte, *dto.TaskError)
    FetchTask(baseUrl, key string, body map[string]any, proxy string) (*http.Response, error)
    ParseTaskResult(respBody []byte) (*relaycommon.TaskInfo, error)
    GetModelList() []string
    GetChannelName() string
}
```

这些方法涉及：
- **请求格式转换**：UpToken 使用 `content` 数组（含 `text`/`image_url`/`video_url`/`audio_url` 类型），与其他视频 API 完全不同
- **响应解析**：UpToken 返回 `{"id": "ut-xxx"}`，状态值为 `queued`/`running`/`succeeded`/`failed`
- **轮询机制**：`GET /v1/video/generations/{id}` 获取任务状态
- **计费数据提取**：从 `usage.total_tokens` 读取 token 数

每个上游 API 的协议差异很大，不存在"通用"的视频任务适配器。

### 4.2 各视频 API 的协议差异

| | UpToken | Doubao (豆包) | Sora | Vertex AI |
|---|---|---|---|---|
| 提交 URL | `/v1/video/generations` | `/api/v3/contents/generations/tasks` | OpenAI 格式 | Vertex 格式 |
| 轮询 URL | `/v1/video/generations/{id}` | `/api/v3/contents/generations/tasks/{id}` | OpenAI 格式 | Vertex 格式 |
| 状态值 | `queued`/`running`/`succeeded`/`failed` | `pending`/`processing`/`succeeded`/`failed` | OpenAI 状态 | Vertex 状态 |
| 视频内容类型 | `video_url` | `video` | N/A | N/A |
| 音频内容类型 | `audio_url` | 不支持 | N/A | N/A |
| 计费字段 | `usage.total_tokens` | `usage.total_tokens` | 按秒 | 按秒 |
| 认证方式 | `Bearer token` | 豆包签名 | OpenAI 格式 | OAuth2 |

### 4.3 计费维度不同

UpToken 需要区分**是否有视频输入**（`has_video_input`），这是一个全新的计费维度：

- 有视频输入：$4/M tokens
- 无视频输入：$6.6/M tokens

这需要在 adaptor 的 `BuildRequestBody` 中检测 content 是否包含 `video_url`，并将标记写入 gin context，最终保存到 `task.Data` 供轮询计费使用。这种业务逻辑不可能通过"通用"适配器实现。

---

## 五、新增渠道类型带来了什么

新增 `ChannelTypeUptoken = 59` 后的完整能力：

| 能力 | 自定义渠道 (type 8) | 独立渠道 (type 59) |
|---|---|---|
| 文本对话转发 | 通过 OpenAI 适配器 | 不适用 |
| 视频任务提交 | **不支持** (返回 400) | 通过 UpToken TaskAdaptor |
| 视频任务轮询 | **不支持** | 通过 FetchTask + ParseTaskResult |
| OpenAI Video 格式转换 | **不支持** | 通过 ConvertToOpenAIVideo |
| 按 token 计费 | **不支持** | 通过 VideoCompletionRatio 体系 |
| 视频/非视频输入差异计费 | **不支持** | 通过 `has_video_input` 维度 |
| 后台管理页面 | 通用图标 | 专属渠道类型标签 |
| model_mapping | 通用 | 支持 `seedance-2-0-pro → uptoken-2.0-pro` |

---

## 六、涉及的代码变更

| 文件 | 变更内容 |
|---|---|
| `constant/channel.go` | 新增 `ChannelTypeUptoken = 59`，base URL，渠道名 |
| `relay/channel/task/uptoken/constants.go` | 模型列表：`seedance-2-0-pro`、`seedance-2-0-fast` |
| `relay/channel/task/uptoken/adaptor.go` | 完整 TaskAdaptor + OpenAIVideoConverter 实现 |
| `relay/relay_adaptor.go` | 注册 `case 59 → taskuptoken.TaskAdaptor` |
| `setting/ratio_setting/model_ratio.go` | `VideoAudioPricing` 新增 `NoVideo`/`Video` 字段，新增 `GetVideoCompletionRatioVideoPricing` |
| `relay/relay_task.go` | `mergeVideoTokenRatioBillingData` 保存 `has_video_input` |
| `controller/task_video.go` | `handleVideoTokenRatioBilling` 支持 `has_video_input` 计费维度 |
| `web/src/constants/channel.constants.js` | 前端渠道选项新增 Uptoken |

---

## 七、VideoCompletionRatio 配置示例

在后台「运营设置」→「VideoCompletionRatio」中配置：

```json
{
    "seedance-2-0-pro": {"noVideo": 6.6, "video": 4.0},
    "seedance-2-0-fast": {"noVideo": 6.6, "video": 4.0}
}
```

- `noVideo: 6.6`：无视频输入时 $6.6/M tokens
- `video: 4.0`：有视频输入时 $4/M tokens

计费公式（价格模式，VideoRatio 不配置时）：
```
actualQuota = ($/M tokens) / 1,000,000 × tokens × QuotaPerUnit × groupRatio
```
