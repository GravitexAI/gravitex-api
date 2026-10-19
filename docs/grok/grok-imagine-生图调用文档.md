# Grok Imagine 生图 / 图生图调用文档

- **Base URL**：`https://api.gravitex.ai`
- **鉴权**：`Authorization: Bearer <你的 API Key>`
- **Content-Type**：`application/json`（文生图、图生图都只支持 JSON，**不支持 multipart/form-data**）
- **模型**：`grok-imagine-image-2.0`、`grok-imagine-image`、`grok-imagine-image-quality`（别名 `grok-imagine-image-pro`），各模型参数支持差异见 [第 1 节](#1-支持的模型)
- 下文示例以 `grok-imagine-image-2.0` 为例，换其他模型只需改 `model` 字段

| 场景 | 端点 |
|---|---|
| 文生图 | `POST /v1/images/generations` |
| 图生图（单图 / 多图参考） | `POST /v1/images/edits` |

> 兼容说明：如果在 `/v1/images/generations` 的请求里带了 `image` 或 `images`，平台会自动按图生图处理（转发到 edits）。推荐直接调用 `/v1/images/edits`。

---

## 1. 支持的模型

| 模型 | 文生图 | 图生图 | `aspect_ratio` | `resolution` | `quality` |
|---|---|---|---|---|---|
| `grok-imagine-image-2.0` | ✅ | ✅ | ✅ | ✅ | ✅ |
| `grok-imagine-image` | ✅ | ✅ | ✅ | ✅ | ❌ |
| `grok-imagine-image-quality`（别名 `grok-imagine-image-pro`） | ✅ | ✅ | ✅ | ✅ | ❌ |

- `quality` 参数**只有 `grok-imagine-image-2.0` 支持**，其他模型请不要传。
- 各模型的价格以平台模型页为准。

---

## 2. 请求参数

所有参数都放在 JSON **顶层**，不要嵌套在 `extra_body` 或 `extra_body.xai` 里。

| 参数 | 类型 | 必填 | 取值 / 说明 |
|---|---|---|---|
| `model` | string | 是 | 见第 1 节模型列表 |
| `prompt` | string | 是 | 生成或编辑的文字描述 |
| `n` | integer | 否 | 生成张数，`1`–`10`，默认 `1` |
| `aspect_ratio` | string | 否 | 输出比例，默认 `auto`（由模型根据 prompt 选）。可选值见下表 |
| `resolution` | string | 否 | `1k`（默认）/ `2k` |
| `quality` | string | 否 | `low` / `medium` / `auto`（默认）。`auto` 时文生图用 `low`，图生图用 `medium`。**仅 `grok-imagine-image-2.0` 支持** |
| `response_format` | string | 否 | `url`（默认，临时链接，请尽快下载）/ `b64_json` |
| `image` | object | 图生图单图时必填 | `{"url": "<图片地址或 data URI>", "type": "image_url"}` |
| `images` | object[] | 图生图多图时必填 | 由多个 `image` 对象组成的数组，最多 5 张；与 `image` 二选一 |

**`aspect_ratio` 可选值**

| 比例 | 适用场景 |
|---|---|
| `1:1` | 头像、缩略图 |
| `16:9` / `9:16` | 横屏宽幅 / 竖屏、手机故事 |
| `4:3` / `3:4` | 演示文稿 / 人像 |
| `3:2` / `2:3` | 摄影 |
| `2:1` / `1:2` | 横幅、页眉 |
| `19.5:9` / `9:19.5` | iPhone 全面屏 |
| `20:9` / `9:20` | 安卓全面屏 |
| `21:9` | 电影宽银幕 |
| `5:2` | 宽横幅 |
| `auto` | 模型自动选择 |

> ⚠️ 不支持 OpenAI 的 `size` 参数（如 `"1024x1024"`），平台会忽略它。请用 `aspect_ratio` + `resolution` 控制尺寸。

**参考图 `url` 支持两种形式**

- 公网可访问的图片 URL：`https://example.com/photo.png`
- Base64 Data URI：`data:image/png;base64,iVBORw0KGgo...`（同一个请求里两种形式可以混用）

---

## 3. 文生图示例

### cURL

```bash
curl -X POST https://api.gravitex.ai/v1/images/generations \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $GRAVITEX_API_KEY" \
  -d '{
    "model": "grok-imagine-image-2.0",
    "prompt": "a cute orange cat sitting on a windowsill",
    "n": 1,
    "aspect_ratio": "9:16",
    "resolution": "2k",
    "quality": "medium",
    "response_format": "b64_json"
  }'
```

### Python（OpenAI SDK）

OpenAI SDK 没有 `aspect_ratio` / `resolution` 这些参数，要通过 `extra_body` 传。SDK 会把 `extra_body` 里的字段**平铺到请求体顶层**，所以这样写是正确的：

```python
from openai import OpenAI

client = OpenAI(base_url="https://api.gravitex.ai/v1", api_key="YOUR_API_KEY")

resp = client.images.generate(
    model="grok-imagine-image-2.0",
    prompt="a cute orange cat sitting on a windowsill",
    n=1,
    response_format="b64_json",
    extra_body={"aspect_ratio": "9:16", "resolution": "2k", "quality": "medium"},
)
print(resp.data[0].b64_json[:50])
```

> 注意：如果用 `requests` 或 cURL 直接发 HTTP 请求，**不要**在 JSON 里写 `"extra_body": {...}`，参数直接放在顶层。

---

## 4. 图生图示例（单张参考图）

### cURL（图片 URL）

```bash
curl -X POST https://api.gravitex.ai/v1/images/edits \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $GRAVITEX_API_KEY" \
  -d '{
    "model": "grok-imagine-image-2.0",
    "prompt": "Render this as a pencil sketch with detailed shading",
    "image": {
      "url": "https://docs.x.ai/assets/api-examples/images/style-realistic.png",
      "type": "image_url"
    },
    "resolution": "2k",
    "response_format": "url"
  }'
```

### Python（requests，本地图片转 Base64）

> OpenAI SDK 的 `client.images.edit()` 发的是 multipart/form-data，**不适用于本模型**。请像下面这样直接发 HTTP 请求。

```python
import base64
import requests

with open("photo.png", "rb") as f:
    b64 = base64.b64encode(f.read()).decode()

resp = requests.post(
    "https://api.gravitex.ai/v1/images/edits",
    headers={"Authorization": "Bearer YOUR_API_KEY"},
    json={
        "model": "grok-imagine-image-2.0",
        "prompt": "Render this as a pencil sketch with detailed shading",
        "image": {"url": f"data:image/png;base64,{b64}", "type": "image_url"},
        "aspect_ratio": "1:1",
        "resolution": "1k",
        "response_format": "b64_json",
    },
    timeout=300,
)
resp.raise_for_status()
img = base64.b64decode(resp.json()["data"][0]["b64_json"])
open("output.jpg", "wb").write(img)
```

不传 `aspect_ratio` 时，输出比例默认跟随参考图。

---

## 5. 图生图示例（多张参考图）

多张参考图用 `images` 数组，最多 5 张，按数组顺序编号，可以在 prompt 里用“第一张 / 第二张图”来指代。不传 `aspect_ratio` 时，输出比例跟随第一张图。

```bash
curl -X POST https://api.gravitex.ai/v1/images/edits \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $GRAVITEX_API_KEY" \
  -d '{
    "model": "grok-imagine-image-2.0",
    "prompt": "Put the cat from the first image into the room from the second image",
    "images": [
      {"url": "https://example.com/cat.png", "type": "image_url"},
      {"url": "data:image/png;base64,iVBORw0KGgo...", "type": "image_url"}
    ],
    "aspect_ratio": "16:9",
    "response_format": "url"
  }'
```

---

## 6. 响应格式

```json
{
  "data": [
    {
      "url": "https://<临时图片地址>.jpeg",
      "mime_type": "image/jpeg"
    }
  ],
  "usage": {
    "cost_in_usd_ticks": 400000000
  }
}
```

- `response_format` 为 `b64_json` 时，`data[i]` 里返回的是 `b64_json` 字段。
- `url` 是临时链接，请尽快下载保存。
- 输出格式固定为 JPEG。

---

## 7. 常见错误写法对照

| ❌ 错误写法 | ✅ 正确写法 |
|---|---|
| `"size": "1024x1024"` / `"size": "16:9"` | `"aspect_ratio": "1:1"` + `"resolution": "1k"` |
| 直接 HTTP 请求里写 `"extra_body": {"aspect_ratio": "9:16"}` | 在顶层写 `"aspect_ratio": "9:16"` |
| `"image": ["https://.../a.png"]`（字符串数组） | `"image": {"url": "https://.../a.png", "type": "image_url"}` |
| 多张图写成 `"image": [{...}, {...}]` | `"images": [{...}, {...}]` |
| 用 multipart/form-data 调 `/v1/images/edits` | 用 `application/json` |
