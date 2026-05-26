# Seedance 2.0 响应格式补充说明（含状态字段修订）

> 本文档是对 [`Seedance 2.0 视频生成 API.md`](Seedance%202.0%20%E8%A7%86%E9%A2%91%E7%94%9F%E6%88%90%20API.md) 中"创建任务 / 查询任务"接口响应格式的细化补充，重点澄清 `status` 字段取值以及 `metadata` 字段的含义。
>
> **请以本说明为准。**

---

## 0. 状态字段修订声明

之前的沟通中，**轮询查询时的"进行中"状态曾被误描述为 `processing`**，请以本说明给出的标准枚举为准：

| 状态 (`status`) | 说明 |
|---|---|
| `queued` | 任务已提交，排队中 |
| `in_progress` | 正在生成（之前误写为 `processing`，**实际为 `in_progress`**） |
| `completed` | 生成成功，`video_url` 字段可用 |
| `failed` | 生成失败，详见 `error.message` |

状态流转：

```
queued → in_progress → completed / failed
```

---

## 1. `metadata` 字段是什么

`metadata` 是网关用来**透传上游火山方舟（BytePlus / 豆包）原厂返回字段**的容器。任何上游原厂返回的字段（如 `seed / resolution / duration / framespersecond / service_tier / usage / draft / priority / execution_expires_after / created_at / updated_at / model / status / content` 等）都会原样放进 `metadata` 里，方便客户端按需读取上游的"完整原貌"。

设计原则与你提交任务时的请求体一致：

- **请求体**：客户端把"上游原厂参数"放在 `metadata` 里（例如 `metadata.content`、`metadata.resolution`、`metadata.generate_audio` 等）。
- **响应体**：网关把"上游原厂返回字段"也放在 `metadata` 里。

这意味着 `metadata` 在不同阶段的内容会随上游响应的丰富度递增。

---

## 2. 创建视频生成任务

**POST** `https://api.gravitex.ai/v1/video/generations`

### 请求示例

```json
{
    "model": "seedance-2-0-fast",
    "prompt": "测试一下",
    "duration": 4,
    "metadata": {
        "content": [
            {
                "type": "text",
                "text": "测试一下"
            }
        ],
        "resolution": "480p",
        "ratio": "16:9",
        "watermark": false,
        "generate_audio": true
    }
}
```

### 响应（成功）

提交成功时**直接返回 OpenAIVideo 结构**（无 `code / message / data` 外层包装）：

```json
{
    "id": "cgt-20260526190841-6v9xx",
    "task_id": "cgt-20260526190841-6v9xx",
    "object": "video",
    "model": "seedance-2-0-fast",
    "status": "queued",
    "progress": 0,
    "created_at": 1779793721,
    "metadata": {
        "id": "cgt-20260526190841-6v9xx"
    }
}
```

| 字段 | 类型 | 说明 |
|---|---|---|
| `id` / `task_id` | string | 任务 ID，后续用于查询任务状态。两者目前同值，`task_id` 为兼容旧客户端保留 |
| `object` | string | 固定为 `"video"` |
| `model` | string | 提交时所用模型（保留客户端传入值） |
| `status` | string | 提交后初始状态为 `queued`（排队中） |
| `progress` | integer | 进度百分比，提交时为 `0` |
| `created_at` | integer | 任务创建时间戳（秒） |
| `metadata` | object | 上游火山方舟返回的原始字段。提交阶段上游仅返回 `id`，故此处一般只包含 `{"id": "..."}` |

### 响应（上游报错，例如审核拦截 / 参数非法）

当上游返回非 200 错误（如**审核拦截、参数非法、内容安全等**），网关返回错误信封：

```json
{
    "code": "fail_to_fetch_task",
    "message": "{\"error\":{\"code\":\"InputImageSensitiveContentDetected.PrivacyInformation\",\"message\":\"The request failed because the input image may contain real person. Request id: 0217797957086008eedaf36e20f5141b2bc84e877dea1f723b478\",\"param\":\"\",\"type\":\"BadRequest\"}}",
    "data": null
}
```

| 字段 | 说明 |
|---|---|
| `code` | 网关错误码，提交失败时常见为 `fail_to_fetch_task`（其它情况见下方网关错误码表） |
| `message` | 上游原厂错误体（**整段被 JSON 字符串化**，客户端需要二次 `JSON.parse(message)` 才能拿到结构化对象 `{error: {code, message, param, type}}`） |
| `data` | 失败时为 `null` |

> **客户端二次解析示例**：
>
> ```js
> if (resp.code === "fail_to_fetch_task") {
>   const upstream = JSON.parse(resp.message);
>   console.log(upstream.error.code);    // "InputImageSensitiveContentDetected.PrivacyInformation"
>   console.log(upstream.error.message); // 人类可读错误描述
>   console.log(upstream.error.type);    // "BadRequest"
> }
> ```

**常见 BytePlus 上游错误码：**

| 上游 `error.code` | 触发原因 |
|---|---|
| `InputImageSensitiveContentDetected.*` | 输入图片审核拦截（含真人、敏感信息、版权等子分类） |
| `InvalidParameter` | 参数非法（如 `content[N].image_url.url` 类型不匹配、分辨率超限等） |
| `RateLimitExceeded` | 频率限制 |
| `InternalServiceError` | 上游内部错误 |

更详细的拦截分类查询请参考主文档 [`Seedance 2.0 视频生成 API.md`](Seedance%202.0%20%E8%A7%86%E9%A2%91%E7%94%9F%E6%88%90%20API.md) 的「查询审核拦截原因」章节。

---

## 3. 查询任务状态

**GET** `https://api.gravitex.ai/v1/video/generations/{task_id}`

查询接口的响应**带 `code / message / data` 外层信封**，实际任务信息在 `data` 字段里。

### 响应（生成中）

```json
{
    "code": "success",
    "message": "",
    "data": {
        "completed_at": 1779793721,
        "created_at": 1779793721,
        "id": "cgt-20260526190841-6v9xx",
        "metadata": {
            "created_at": 1779793721,
            "draft": false,
            "execution_expires_after": 172800,
            "id": "cgt-20260526190841-6v9xx",
            "model": "dreamina-seedance-2-0-fast-260128",
            "priority": 0,
            "service_tier": "default",
            "status": "running",
            "updated_at": 1779793721,
            "url": "",
            "video_url": ""
        },
        "model": "seedance-2-0-fast",
        "object": "video",
        "progress": 50,
        "status": "in_progress",
        "task_id": "cgt-20260526190841-6v9xx"
    }
}
```

| 关键字段 | 说明 |
|---|---|
| `data.status` | `queued` / `in_progress` —— **以此字段为准判断网关侧的任务状态**（不是 `processing`！） |
| `data.progress` | 进度百分比 |
| `data.metadata.status` | 上游原厂的状态文本（如 `running`、`queued`、`succeeded`），属于"原貌透传"。客户端展示时**应使用 `data.status`，不要使用 `data.metadata.status`** |
| `data.url` / `data.video_url` | 生成中暂为空 |

> ⚠️ **不要混淆**：`data.status` 是网关标准化后的状态（`queued / in_progress / completed / failed`），`data.metadata.status` 是上游原厂的状态文本（如 `running / succeeded`）。两者并存是为了"既给标准答案、也保留原貌"。**业务逻辑请用 `data.status`。**

### 响应（生成成功）

```json
{
    "code": "success",
    "message": "",
    "data": {
        "completed_at": 1779793822,
        "created_at": 1779793721,
        "id": "cgt-20260526190841-6v9xx",
        "metadata": {
            "content": {
                "video_url": "https://ark-acg-ap-southeast-1.tos-ap-southeast-1.volces.com/dreamina-seedance-2-0-fast/02177979372129400000000000000000000ffffc0a8aba6404d5b.mp4?X-Tos-..."
            },
            "created_at": 1779793721,
            "draft": false,
            "duration": 4,
            "execution_expires_after": 172800,
            "framespersecond": 24,
            "id": "cgt-20260526190841-6v9xx",
            "model": "dreamina-seedance-2-0-fast-260128",
            "priority": 0,
            "ratio": "16:9",
            "resolution": "480p",
            "seed": 90758,
            "service_tier": "default",
            "status": "succeeded",
            "updated_at": 1779793827,
            "url": "https://ark-acg-ap-southeast-1.tos-ap-southeast-1.volces.com/dreamina-seedance-2-0-fast/02177979372129400000000000000000000ffffc0a8aba6404d5b.mp4?X-Tos-...",
            "usage": {
                "completion_tokens": 40594,
                "total_tokens": 40594
            },
            "video_url": "https://ark-acg-ap-southeast-1.tos-ap-southeast-1.volces.com/dreamina-seedance-2-0-fast/02177979372129400000000000000000000ffffc0a8aba6404d5b.mp4?X-Tos-..."
        },
        "model": "seedance-2-0-fast",
        "object": "video",
        "progress": 100,
        "seconds": "4",
        "status": "completed",
        "task_id": "cgt-20260526190841-6v9xx",
        "url": "https://ark-acg-ap-southeast-1.tos-ap-southeast-1.volces.com/dreamina-seedance-2-0-fast/02177979372129400000000000000000000ffffc0a8aba6404d5b.mp4?X-Tos-...",
        "video_url": "https://ark-acg-ap-southeast-1.tos-ap-southeast-1.volces.com/dreamina-seedance-2-0-fast/02177979372129400000000000000000000ffffc0a8aba6404d5b.mp4?X-Tos-..."
    }
}
```

| 关键字段 | 说明 |
|---|---|
| `data.status` | `completed` |
| `data.progress` | `100` |
| `data.url` / `data.video_url` | **生成结果视频 URL，二者同值**，建议优先使用 `data.video_url`。URL 带 12~24h 有效期签名 |
| `data.seconds` | 生成视频时长（秒） |
| `data.completed_at` | 完成时间戳（秒） |
| `data.metadata.usage.total_tokens` | 上游计算的 token 用量（仅用于查看，不直接代表网关计费额度） |
| `data.metadata.seed` | 本次生成实际使用的随机种子 |
| `data.metadata.resolution / ratio / framespersecond / duration` | 上游确认的实际分辨率 / 画幅比例 / 帧率 / 时长 |

### 响应（生成失败）

```json
{
    "code": "success",
    "message": "",
    "data": {
        "id": "cgt-...",
        "task_id": "cgt-...",
        "object": "video",
        "model": "seedance-2-0-fast",
        "status": "failed",
        "progress": 100,
        "created_at": 1779793721,
        "completed_at": 1779793822,
        "error": {
            "message": "task failed",
            "code": "failed"
        },
        "metadata": {
            "status": "failed",
            ...
        }
    }
}
```

| 关键字段 | 说明 |
|---|---|
| `data.status` | `failed` |
| `data.error.message` | 失败原因。若是 Seedance 2.0 审核拦截，可调用「查询审核拦截原因」获取更细分类 |
| `data.metadata` | 上游原厂返回的完整失败上下文，便于排查 |

---

## 4. 字段一览（建议客户端按此对接）

| 来源 | 字段 | 类型 | 用途 |
|---|---|---|---|
| 顶层 / `data.` | `id`, `task_id` | string | 任务 ID |
| 顶层 / `data.` | `status` | string | **网关标准化状态**：`queued / in_progress / completed / failed` |
| 顶层 / `data.` | `progress` | integer | 进度 0~100 |
| 顶层 / `data.` | `video_url`, `url` | string | 生成结果视频 URL（仅 `completed` 时非空） |
| 顶层 / `data.` | `seconds` | string | 生成视频时长（仅 `completed` 时填充） |
| 顶层 / `data.` | `created_at`, `completed_at` | integer | 创建 / 完成时间戳（秒） |
| 顶层 / `data.` | `error` | object | 失败时的标准错误对象 `{code, message}`（仅 `failed`） |
| 顶层 / `data.` | `metadata` | object | **上游原厂返回字段透传** |

`metadata` 内部可能出现的字段（随上游响应变化）：

| metadata 字段 | 说明 |
|---|---|
| `id` | 上游任务 ID |
| `model` | 上游实际使用的模型版本号（如 `dreamina-seedance-2-0-fast-260128`） |
| `status` | 上游原厂状态文本（`queued / running / succeeded / failed`） |
| `seed` | 实际使用的随机种子 |
| `resolution` | 实际生成分辨率 |
| `ratio` | 实际画幅比例 |
| `duration` | 实际生成时长 |
| `framespersecond` | 帧率 |
| `service_tier` | 服务等级 |
| `usage` | 上游 token 用量统计 |
| `content` | 生成内容对象，含 `video_url` |
| `url` / `video_url` | 上游返回的视频 URL（与顶层 `data.url / data.video_url` 同值，冗余便于直接读取 metadata） |
| `created_at` / `updated_at` | 上游记录的创建 / 更新时间戳 |
| `draft` | 是否草稿模式 |
| `priority` | 上游优先级 |
| `execution_expires_after` | 任务执行超时秒数 |

---

## 5. 客户端最简对接示例

```js
// 1. 提交任务
const submit = await fetch("https://api.gravitex.ai/v1/video/generations", {
  method: "POST",
  headers: {
    "Authorization": "Bearer sk-your_token_key",
    "Content-Type": "application/json"
  },
  body: JSON.stringify({
    model: "seedance-2-0-fast",
    prompt: "测试一下",
    duration: 4,
    metadata: {
      content: [{ type: "text", text: "测试一下" }],
      resolution: "480p",
      ratio: "16:9",
      generate_audio: true
    }
  })
}).then(r => r.json());

// 提交报错时：submit.code === "fail_to_fetch_task"
if (submit.code === "fail_to_fetch_task") {
  const upstream = JSON.parse(submit.message);
  throw new Error(`上游拒绝：${upstream.error.code} — ${upstream.error.message}`);
}

const taskId = submit.id;

// 2. 轮询查询
while (true) {
  const resp = await fetch(`https://api.gravitex.ai/v1/video/generations/${taskId}`, {
    headers: { "Authorization": "Bearer sk-your_token_key" }
  }).then(r => r.json());

  const task = resp.data;
  console.log(`status=${task.status}, progress=${task.progress}`);

  if (task.status === "completed") {
    return task.video_url;          // 拿到视频 URL
  }
  if (task.status === "failed") {
    throw new Error(task.error?.message || "task failed");
  }
  // queued / in_progress 继续等待
  await new Promise(r => setTimeout(r, 5000));
}
```

---

## 6. 与之前口径的差异速查

| 口径项 | 之前可能告知 | 实际正确值 |
|---|---|---|
| 生成中状态 | `processing` | **`in_progress`** |
| 创建任务响应 | 不含 `metadata` | **含 `metadata: { id }`** |
| 轮询查询响应（生成中） | `metadata` 为 `null` | **`metadata` 为上游原厂字段对象**（含 `id / model / status / created_at / updated_at / ...`） |
| 轮询查询响应（完成） | 含完整 `metadata` | 不变，含完整 `metadata`（继续保持） |
| 提交报错响应 | 暂无明确说明 | 见本文「响应（上游报错）」小节，需要二次 `JSON.parse(message)` 拿到上游原厂错误结构 |

---

## 7. 后续规划（信息透露）

- 当前提交失败响应里上游错误体被 JSON 字符串化塞进 `message`，客户端需二次解析。后续版本会将其改造为与成功响应同构的 `OpenAIVideo` 结构（`status: "failed"` + `error: {code, message}` + `metadata: 上游原始错误体`），届时本说明会一并更新。
- `metadata.status` 与顶层 `status` 现阶段双轨并存。客户端业务判断**统一使用顶层 `status`**，`metadata.status` 仅供排查或日志展示。
