# 视频生成 API 响应格式说明

> 本文档说明 Gravitex 平台视频生成接口的统一响应格式，覆盖创建任务（POST `/v1/video/generations`）与查询任务状态（GET `/v1/video/generations/{task_id}`），适用于平台接入的所有视频生成渠道（doubao、vertex、ali、vidu、kling、hailuo、jimeng、azurevideo 等）。

---

## 1. 任务状态枚举

| 状态 (`status`) | 说明 |
|---|---|
| `queued` | 任务已提交，排队中 |
| `in_progress` | 正在生成 |
| `completed` | 生成成功，`video_url` 字段可用 |
| `failed` | 生成失败，详见 `error.message` |

状态流转：

```
queued → in_progress → completed / failed
```

---

## 2. `metadata` 字段说明

`metadata` 是平台用来**透传上游原厂返回字段**的容器。所有接入的视频生成渠道（doubao、vertex、ali、vidu、kling、hailuo、jimeng、azurevideo 等）返回的原始字段，都会被原样放进 `metadata` 里，便于客户端读取上游的"完整原貌"。

设计原则：

- **请求体**：客户端可以把"上游原厂参数"放在 `metadata` 里（例如 `metadata.content`、`metadata.resolution`、`metadata.generate_audio` 等）。
- **响应体**：平台把"上游原厂返回字段"也放在 `metadata` 里。

由于不同厂商上游响应字段不一致，`metadata` 的内容会**因渠道而异**。客户端业务逻辑应使用平台标准化字段（顶层 `status` / `progress` / `video_url` 等），`metadata` 仅推荐用于排查、日志展示和厂商专属增量字段读取。

---

## 3. 创建视频生成任务

**POST** `https://api.gravitex.ai/v1/video/generations`

### 请求示例

```json
{
    "model": "<model_name>",
    "prompt": "测试一下",
    "duration": 4,
    "metadata": {
        // 厂商专属参数原样透传，字段视渠道而定
    }
}
```

### 响应（成功）

提交成功时**直接返回 OpenAIVideo 结构**（无 `code / message / data` 外层信封）：

```json
{
    "id": "<task_id>",
    "task_id": "<task_id>",
    "object": "video",
    "model": "<model_name>",
    "status": "queued",
    "progress": 0,
    "created_at": 1779793721,
    "metadata": {
        // 上游原厂提交阶段返回的字段，因渠道而异
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
| `metadata` | object | 上游原厂返回的原始字段。提交阶段一般只包含上游任务 ID 等少量字段 |

### 响应（上游报错，例如审核拦截 / 参数非法）

当上游返回非 200 错误（如审核拦截、参数非法、内容安全等），平台返回错误信封：

```json
{
    "code": "fail_to_fetch_task",
    "message": "{...上游原厂错误体（JSON 字符串化）...}",
    "data": null
}
```

| 字段 | 说明 |
|---|---|
| `code` | 平台错误码，提交失败时常见为 `fail_to_fetch_task` |
| `message` | 上游原厂错误体（**整段被 JSON 字符串化**，客户端需要二次 `JSON.parse(message)` 才能拿到结构化对象） |
| `data` | 失败时为 `null` |

> **客户端二次解析示例**：
>
> ```js
> if (resp.code === "fail_to_fetch_task") {
>   const upstream = JSON.parse(resp.message);
>   // 上游原厂错误结构因渠道而异，常见形态为 { error: { code, message, ... } }
>   console.log(upstream.error?.code);
>   console.log(upstream.error?.message);
> }
> ```

不同厂商的上游错误结构略有差异，请按各家文档解析。

---

## 4. 查询任务状态

**GET** `https://api.gravitex.ai/v1/video/generations/{task_id}`

查询接口的响应**带 `code / message / data` 外层信封**，实际任务信息在 `data` 字段里。

### 响应（生成中）

```json
{
    "code": "success",
    "message": "",
    "data": {
        "id": "<task_id>",
        "task_id": "<task_id>",
        "object": "video",
        "model": "<model_name>",
        "status": "in_progress",
        "progress": 50,
        "created_at": 1779793721,
        "metadata": {
            "status": "<上游原厂状态文本>",
            "...": "..."
        }
    }
}
```

| 关键字段 | 说明 |
|---|---|
| `data.status` | `queued` / `in_progress` —— **以此字段为准判断任务状态** |
| `data.progress` | 进度百分比 |
| `data.metadata.status` | 上游原厂的状态文本（不同渠道枚举不同，例如 `running / pending / processing / succeeded` 等），属于"原貌透传"。客户端展示**应使用 `data.status`，不要使用 `data.metadata.status`** |
| `data.url` / `data.video_url` | 生成中暂为空 |

> ⚠️ **不要混淆**：`data.status` 是平台标准化后的状态（`queued / in_progress / completed / failed`），`data.metadata.status` 是上游原厂的状态文本（不同厂商不同）。两者并存是为了"既给标准答案、也保留原貌"。**业务逻辑请用 `data.status`。**

### 响应（生成成功）

```json
{
    "code": "success",
    "message": "",
    "data": {
        "id": "<task_id>",
        "task_id": "<task_id>",
        "object": "video",
        "model": "<model_name>",
        "status": "completed",
        "progress": 100,
        "created_at": 1779793721,
        "completed_at": 1779793822,
        "seconds": "4",
        "url": "https://.../video.mp4?...",
        "video_url": "https://.../video.mp4?...",
        "metadata": {
            "status": "<上游原厂状态文本>",
            "...": "..."
        }
    }
}
```

| 关键字段 | 说明 |
|---|---|
| `data.status` | `completed` |
| `data.progress` | `100` |
| `data.url` / `data.video_url` | **生成结果视频 URL，二者同值**，建议优先使用 `data.video_url`。URL 通常带有时效性签名（具体有效期因渠道而异） |
| `data.seconds` | 生成视频时长（秒） |
| `data.completed_at` | 完成时间戳（秒） |
| `data.metadata` | 上游原厂完成阶段返回的完整字段（不同渠道字段不一致） |

### 响应（生成失败）

```json
{
    "code": "success",
    "message": "",
    "data": {
        "id": "<task_id>",
        "task_id": "<task_id>",
        "object": "video",
        "model": "<model_name>",
        "status": "failed",
        "progress": 100,
        "created_at": 1779793721,
        "completed_at": 1779793822,
        "error": {
            "message": "task failed",
            "code": "failed"
        },
        "metadata": {
            "status": "<上游原厂失败状态文本>",
            "...": "..."
        }
    }
}
```

| 关键字段 | 说明 |
|---|---|
| `data.status` | `failed` |
| `data.error.message` | 失败原因 |
| `data.metadata` | 上游原厂返回的完整失败上下文，便于排查 |

---

## 5. 字段一览（建议客户端按此对接）

### 顶层标准字段（稳定契约）

| 来源 | 字段 | 类型 | 用途 |
|---|---|---|---|
| 顶层 / `data.` | `id`, `task_id` | string | 任务 ID |
| 顶层 / `data.` | `object` | string | 固定 `"video"` |
| 顶层 / `data.` | `model` | string | 提交所用模型 |
| 顶层 / `data.` | `status` | string | **平台标准化状态**：`queued / in_progress / completed / failed` |
| 顶层 / `data.` | `progress` | integer | 进度 0~100 |
| 顶层 / `data.` | `video_url`, `url` | string | 生成结果视频 URL（仅 `completed` 时非空） |
| 顶层 / `data.` | `seconds` | string | 生成视频时长（仅 `completed` 时填充） |
| 顶层 / `data.` | `created_at`, `completed_at` | integer | 创建 / 完成时间戳（秒） |
| 顶层 / `data.` | `error` | object | 失败时的标准错误对象 `{code, message}`（仅 `failed`） |
| 顶层 / `data.` | `metadata` | object | **上游原厂返回字段透传** |

### `metadata` 内部字段（因渠道而异，仅作参考）

`metadata` 字段名与上游厂商一一对应，不同渠道结构差别较大。常见字段示意（具体以实际渠道响应为准）：

| metadata 字段 | 说明 |
|---|---|
| `id` | 上游任务 ID |
| `model` | 上游实际使用的模型版本号 |
| `status` | 上游原厂状态文本（不同渠道枚举不同） |
| `created_at` / `updated_at` | 上游记录的创建 / 更新时间戳 |
| `seed` | 实际使用的随机种子（如有） |
| `resolution` / `ratio` / `duration` / `framespersecond` | 实际生成参数（如有） |
| `usage` | 上游 token 或资源用量统计（如有） |
| `content` / `url` / `video_url` | 上游生成内容信息（不同渠道结构不同） |
| 厂商专属字段 | 因渠道而异，例如 operation name、`expires_at`、`task_status_msg` 等 |

> ⚠️ 请勿把 `metadata` 当作稳定契约对接。**业务逻辑请仅依赖顶层标准字段**，`metadata` 用于排查、日志展示和厂商专属增量信息读取。

---

## 6. 客户端最简对接示例

```js
// 1. 提交任务
const submit = await fetch("https://api.gravitex.ai/v1/video/generations", {
  method: "POST",
  headers: {
    "Authorization": "Bearer sk-your_token_key",
    "Content-Type": "application/json"
  },
  body: JSON.stringify({
    model: "<your_model>",
    prompt: "测试一下",
    duration: 4,
    metadata: { /* 厂商专属参数 */ }
  })
}).then(r => r.json());

// 提交报错
if (submit.code === "fail_to_fetch_task") {
  const upstream = JSON.parse(submit.message);
  throw new Error(`上游拒绝：${upstream.error?.code || ""} — ${upstream.error?.message || submit.message}`);
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

## 7. 注意事项

- 当前提交失败响应里上游错误体被 JSON 字符串化塞进 `message`，客户端需二次 `JSON.parse` 解析。后续版本会改造为与成功响应同构的 `OpenAIVideo` 结构（`status: "failed"` + `error: {code, message}` + `metadata: 上游原始错误体`），届时本说明会一并更新。
- `metadata.status` 与顶层 `status` 现阶段双轨并存。客户端业务判断**统一使用顶层 `status`**，`metadata.status` 仅供排查或日志展示。
- `metadata` 内字段命名与上游厂商一一对应，**不同渠道字段不同**，对接厂商专属能力前请参考对应厂商文档查看 `metadata` 详细字段。
