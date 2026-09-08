# Seedream 图生图 `/v1/images/edits` 调用文档

> 适用渠道：VolcEngine / BytePlus（渠道类型 45），Seedream 系列模型
> 网关行为对应代码：`relay/helper/valid_request.go`、`relay/channel/volcengine/adaptor.go`
> 上游文档：<https://www.volcengine.com/docs/82379/1824121>

---

## 1. 概述

| 项目 | 说明 |
|---|---|
| 网关端点 | `POST /v1/images/edits` |
| 实际转发的上游端点 | `{渠道 BaseUrl}/api/v3/images/generations` |
| 支持的请求体格式 | `application/json`、`multipart/form-data` |
| 上游接收的格式 | **始终是 JSON**（form-data 由网关转换） |
| 鉴权 | `Authorization: Bearer <你的 new-api 令牌>` |

Seedream 的图生图和文生图在火山侧是**同一个接口**，区别只是请求体里带不带 `image`。
所以 `/v1/images/edits` 和 `/v1/images/generations` 两个入口对 Seedream 是等价的，
网关会统一转发到 `/api/v3/images/generations`。

已知模型名（`relay/channel/volcengine/constants.go`）：

```
doubao-seedream-4-0-250828   /  seedream-4-0-250828
doubao-seedream-4-5-251128   /  seedream-4-5-251128
doubao-seedream-5-0-260128   /  seedream-5-0-260128
```

> 实际可用模型以你渠道配置的模型列表为准。

---

## 2. JSON 方式（推荐）

### 2.1 请求头

```
Content-Type: application/json
Authorization: Bearer sk-xxxxxxxx
```

### 2.2 字段说明

| 字段 | 类型 | 必填 | 说明 |
|---|---|:--:|---|
| `model` | string | ✅ | 模型名，为空网关直接返回 `model is required` |
| `prompt` | string | ✅ | 编辑指令 |
| `image` | string 或 string[] | ✅ | 参考图。单图用字符串，**多图用数组**。支持 HTTP(S) URL 和 `data:image/xxx;base64,...` |
| `size` | string | ➖ | 如 `2048x2048`、`1K`、`2K`。注意用小写 `x`，传全角 `×` 会被网关拒绝 |
| `n` | int | ➖ | 生成张数，范围 `1 ~ 128`（`dto.MaxImageN`），不传默认 `1` |
| `response_format` | string | ➖ | `url` 或 `b64_json`，透传给上游 |
| `seed` | int | ➖ | 透传给上游 |
| `watermark` | bool | ➖ | 是否加水印，透传给上游 |
| `sequential_image_generation` | string | ➖ | 组图生成开关，透传给上游 |
| `sequential_image_generation_options` | object | ➖ | 组图选项，透传给上游 |
| `stream` | bool | ➖ | 流式返回 |

> **未在网关 DTO 里定义的字段会原样透传给上游**（走 `Extra` 合并逻辑），
> 所以火山后续新增的参数不需要改网关就能用。具体取值范围以火山官方文档为准。

### 2.3 单图示例

```bash
curl -X POST "https://your-gateway.com/v1/images/edits" \
  -H "Authorization: Bearer sk-xxxxxxxx" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "doubao-seedream-4-0-250828",
    "prompt": "把背景换成雪山",
    "image": "https://example.com/cat.png",
    "size": "2048x2048",
    "response_format": "url",
    "watermark": false
  }'
```

### 2.4 多图示例（两张参考图）

```bash
curl -X POST "https://your-gateway.com/v1/images/edits" \
  -H "Authorization: Bearer sk-xxxxxxxx" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "doubao-seedream-4-0-250828",
    "prompt": "把第一张图里的人物放进第二张图的场景",
    "image": [
      "https://example.com/person.png",
      "https://example.com/scene.png"
    ],
    "size": "2048x2048"
  }'
```

### 2.5 base64 传图

```json
{
  "model": "doubao-seedream-4-0-250828",
  "prompt": "换个背景颜色",
  "image": [
    "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAA...",
    "data:image/jpeg;base64,/9j/4AAQSkZJRgABAQAAAQ..."
  ]
}
```

> base64 必须带 `data:<mime>;base64,` 前缀。

### 2.6 Python 示例

```python
import requests

resp = requests.post(
    "https://your-gateway.com/v1/images/edits",
    headers={"Authorization": "Bearer sk-xxxxxxxx"},
    json={
        "model": "doubao-seedream-4-0-250828",
        "prompt": "把第一张图里的人物放进第二张图的场景",
        "image": [
            "https://example.com/person.png",
            "https://example.com/scene.png",
        ],
        "size": "2048x2048",
        "response_format": "url",
    },
    timeout=180,
)
resp.raise_for_status()
print(resp.json())
```

---

## 3. form-data 方式

### 3.1 请求头

```
Content-Type: multipart/form-data; boundary=----xxxx
Authorization: Bearer sk-xxxxxxxx
```

> `Content-Type` 由 HTTP 客户端自动生成，**不要手写**，否则 boundary 对不上会解析失败。

### 3.2 图片字段：三种写法都支持

和 OpenAI 官方 `/v1/images/edits` 对齐，以下三种命名等价，且**都支持多图**：

| 写法 | 示例 |
|---|---|
| 重复的 `image` | `image=图1` + `image=图2` |
| `image[]` | `image[]=图1` + `image[]=图2` |
| 带下标 `image[N]` | `image[0]=图1` + `image[1]=图2` |

每一项既可以是**上传的文件**，也可以是**文本值**（URL 或 `data:...;base64,` 字符串），
两种可以混用。网关会把上传的文件读成 base64 data URI，再拼进发往上游的 JSON `image` 字段：
单张 → 字符串，多张 → 数组。

顺序保证：`image` → `image[]` → `image[N]`（`N` 按字符串排序）。
> 下标超过 9 时按字符串排序会出现 `image[10]` 排在 `image[2]` 前面，
> 需要严格顺序请用 `image[]` 按提交顺序传，或直接用 JSON 方式。

### 3.3 支持的表单字段

form-data 与 JSON 的参数能力**完全一致**：下表是网关会显式校验/处理的字段，
**其余任何字段都会原样透传给上游**（`seed`、`sequential_image_generation` 等都能用）。

| 表单字段 | 说明 |
|---|---|
| `model` | 必填 |
| `prompt` | 编辑指令 |
| `image` / `image[]` / `image[N]` | 参考图，文件或文本值，可多个 |
| `n` | 生成张数，`1 ~ 128`，非法值直接 400 |
| `size` | 输出尺寸 |
| `quality` | 质量（Seedream 一般不用） |
| `stream` | `true` / `false`，非法值直接 400 |
| `input_fidelity` | 保真度 |
| `watermark` | 传 `true` 为加水印，传任何其他值均视为 `false` |
| `response_format` | `url` 或 `b64_json` |
| **其他任意字段** | 原样透传给上游 |

### 3.4 值类型规则（重要）

表单只能传字符串，但上游要的是原始 JSON 类型，所以网关对**透传字段**做了还原：

| 你传的值 | 发往上游的值 | 说明 |
|---|---|---|
| `seed=42` | `"seed": 42` | 合法 JSON 数字 → 数字 |
| `disable_safety_checker=true` | `"disable_safety_checker": true` | 合法 JSON 布尔 → 布尔 |
| `sequential_image_generation_options={"max_images":4}` | `"sequential_image_generation_options": {"max_images": 4}` | 合法 JSON 对象 → 对象 |
| `sequential_image_generation=auto` | `"sequential_image_generation": "auto"` | 不是合法 JSON → 字符串 |

> 传**对象或数组**参数时，直接把 JSON 文本作为表单值即可，网关会解析成真正的对象/数组。
> 同名字段出现多次时会收敛成数组。
> 上表中的显式字段（`model`、`prompt`、`size` 等）走各自的解析逻辑，不受此规则影响。

### 3.5 上传文件示例

```bash
curl -X POST "https://your-gateway.com/v1/images/edits" \
  -H "Authorization: Bearer sk-xxxxxxxx" \
  -F "model=doubao-seedream-4-0-250828" \
  -F "prompt=把第一张图里的人物放进第二张图的场景" \
  -F "image[]=@/path/to/person.png" \
  -F "image[]=@/path/to/scene.png" \
  -F "size=2048x2048" \
  -F "n=1" \
  -F "response_format=url" \
  -F "seed=42" \
  -F "sequential_image_generation=auto" \
  -F 'sequential_image_generation_options={"max_images":4}'
```

### 3.6 传 URL 文本值示例

```bash
curl -X POST "https://your-gateway.com/v1/images/edits" \
  -H "Authorization: Bearer sk-xxxxxxxx" \
  -F "model=doubao-seedream-4-0-250828" \
  -F "prompt=把背景换成雪山" \
  -F "image[]=https://example.com/person.png" \
  -F "image[]=https://example.com/scene.png"
```

### 3.7 Python 示例（上传文件）

```python
import requests

files = [
    ("image[]", ("person.png", open("/path/to/person.png", "rb"), "image/png")),
    ("image[]", ("scene.png", open("/path/to/scene.png", "rb"), "image/png")),
]
data = {
    "model": "doubao-seedream-4-0-250828",
    "prompt": "把第一张图里的人物放进第二张图的场景",
    "size": "2048x2048",
    "n": "1",
    "response_format": "url",
    "seed": "42",
    "sequential_image_generation": "auto",
    # 对象参数直接传 JSON 文本，网关会还原成对象
    "sequential_image_generation_options": '{"max_images":4}',
}

resp = requests.post(
    "https://your-gateway.com/v1/images/edits",
    headers={"Authorization": "Bearer sk-xxxxxxxx"},  # 不要手写 Content-Type
    files=files,
    data=data,
    timeout=180,
)
resp.raise_for_status()
print(resp.json())
```

### 3.8 OpenAI SDK 示例

```python
from openai import OpenAI

client = OpenAI(api_key="sk-xxxxxxxx", base_url="https://your-gateway.com/v1")

result = client.images.edit(
    model="doubao-seedream-4-0-250828",
    prompt="把第一张图里的人物放进第二张图的场景",
    image=[
        open("/path/to/person.png", "rb"),
        open("/path/to/scene.png", "rb"),
    ],
)
print(result)
```

> SDK 传图片列表时会自动发成多个 `image[]` part，网关已支持。

---

## 4. 响应格式

响应是**上游原样透传**，即火山 `/api/v3/images/generations` 的返回结构：

```json
{
  "model": "doubao-seedream-4-0-250828",
  "created": 1757318400,
  "data": [
    {
      "url": "https://ark-content-generation.../image.png",
      "size": "2048x2048"
    }
  ],
  "usage": {
    "generated_images": 1,
    "output_tokens": 4096,
    "total_tokens": 4096
  }
}
```

`response_format: "b64_json"` 时 `data[].url` 变成 `data[].b64_json`。

计费相关：
- 输出张数优先取响应里的 `usage.generated_images`，取不到再回退到请求的 `n`，兜底 `1`。
- 输出尺寸优先取响应里的 `data[].size`，取不到再用请求的 `size`。
- 输入参考图张数按请求里的 `image` 计算。

---

## 5. 两种方式对比

| 维度 | JSON | form-data |
|---|---|---|
| 多图 | ✅ `image` 数组 | ✅ `image` / `image[]` / `image[N]` 重复字段 |
| 上传本地文件 | ❌ 需自己转 base64 | ✅ 直接上传 |
| 火山专有参数（`seed` / `sequential_image_generation` 等） | ✅ 全部透传 | ✅ 全部透传 |
| `response_format` | ✅ | ✅ |
| 嵌套对象参数 | ✅ 原生 JSON | ✅ 传 JSON 文本，网关自动还原 |
| 请求体大小 | base64 会膨胀约 33% | 二进制原样传输，更小 |
| **推荐场景** | 参数复杂、图片已在 OSS/CDN 上 | 直传本地文件、想省带宽 |

---

## 6. 常见问题排查

| 现象 | 原因 | 处理 |
|---|---|---|
| 多图只有第一张生效 | 网关旧版本 form-data 只取第一个 `image` 值 | 升级到包含本次修复的版本 |
| form-data 传图完全没生效 | 旧版本不读 multipart 文件 | 同上 |
| `model is required` | 请求里没有 `model` | JSON 补 `model`；form-data 确认 `model` 字段拼在表单里 |
| `size an unexpected error occurred...` | `size` 用了全角乘号 `×` | 改成小写字母 `x`，如 `2048x2048` |
| `n must be an integer between 1 and 128` | `n` 非法或超界 | 传 1~128 的整数 |
| `invalid stream value` | `stream` 不是合法布尔值 | 传 `true` / `false` |
| `failed to parse image edit form request` | Content-Type 手写导致 boundary 不匹配 | 删掉手写的 `Content-Type`，交给 HTTP 客户端生成 |
| form-data 下 `seed` 变成字符串被上游拒绝 | 网关旧版本不做类型还原 | 升级到包含本次修复的版本 |
| form-data 下对象参数不生效 | 值不是合法 JSON 文本 | 确认传的是 `{"max_images":4}` 这种完整 JSON，注意 shell 引号转义 |

---

## 7. 排查用日志关键字

网关会打印用户入参和最终发往上游的 body（长字符串已截断）：

```
image user request params: {...}
image upstream request body(size=...): {...}
```

对比这两条即可判断是「用户没传对」还是「网关转换丢了」。
