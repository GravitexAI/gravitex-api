# 可灵 Kling-V3 `imageToVideo` 支持参数（请求体）

来源（Kling AI 文档）：[https://app.klingai.com/cn/dev/document-api/apiReference/model/imageToVideo](https://app.klingai.com/cn/dev/document-api/apiReference/model/imageToVideo)

接口：`POST /v1/videos/image2video`

> 说明：文档中参数可用性受 `model_name`、多镜头开关、首尾帧、运动笔刷、运镜控制等条件影响。本文件按页面参数区块整理，并保留关键互斥/必填/范围约束，重点覆盖 `kling-v3` 系列可见能力。

## 顶层参数

| 参数 | 类型 | 可选值/约束（取自文档） | 说明 |
|---|---|---|---|
| `model_name` | `string`（可选） | 页面示例含 `kling-v2-6`；文档提示统一使用 `model_name` | 模型名称（原 `model` 字段向前兼容）。 |
| `image` | `string`（可选） | URL 或 Base64；与 `image_tail` 至少二选一 | 参考图像（首帧或主要输入图）。 |
| `image_tail` | `string`（可选） | URL 或 Base64；与 `image` 至少二选一 | 尾帧控制图。 |
| `multi_shot` | `boolean`（可选） | `true/false` | 是否生成多镜头视频。`true` 时 `prompt` 无效且不支持首尾帧生视频；`false` 时 `shot_type` 和 `multi_prompt` 无效。 |
| `shot_type` | `string`（可选） | `customize`、`intelligence` | 分镜方式；当 `multi_shot=true` 时必填。 |
| `prompt` | `string`（可选） | 上限 `2500` 字符；`multi_shot=false` 或 `shot_type=intelligence` 时不得为空 | 正向文本提示词（支持 Omni 样式占位符）。 |
| `multi_prompt` | `array`（可选） | 最多 `6` 个分镜、最小 `1` 个；当 `multi_shot=true` 且 `shot_type=customize` 必填 | 多分镜提示词数组。 |
| `negative_prompt` | `string`（可选） | 上限 `2500` 字符 | 负向提示词。 |
| `element_list` | `array`（可选） | 最多 `3` 个参考主体（文档描述） | 主体库参考主体列表。 |
| `voice_list` | `array`（可选） | 至多引用 `2` 个音色；与 `element_list` 互斥 | 视频引用音色列表。 |
| `sound` | `string`（可选） | `on/off`（页面示例见 `on`） | 是否同时生成声音。 |
| `cfg_scale` | `float`（可选） | 范围 `[0,1]`；`kling-v2.x` 不支持 | 自由度参数；值越大与提示词相关性越强。 |
| `mode` | `string`（可选） | `std`、`pro` | 生成视频模式（标准/专家）。 |
| `static_mask` | `string`（可选） | URL 或 Base64；与 `dynamic_masks.mask` 分辨率需一致 | 静态笔刷涂抹区域。 |
| `dynamic_masks` | `array`（可选） | 最多 `6` 组，每组含 `mask` 与 `trajectories` | 动态笔刷配置列表。 |
| `camera_control` | `object`（可选） | 与 `image_tail`、`dynamic_masks/static_mask` 有互斥约束 | 摄像机运动控制协议。 |
| `duration` | `string`（可选） | 单位秒；支持范围依模型/模式，详见能力地图 | 生成视频时长。 |
| `watermark_info` | `object`（可选） | `watermark_info.enabled: true/false` | 是否生成含水印结果。 |
| `callback_url` | `string`（可选） | URL | 任务状态变更回调地址。 |
| `external_task_id` | `string`（可选） | 用户自定义任务 ID（单用户唯一） | 自定义任务标识，用于查询。 |

## `image` / `image_tail` 约束

- 支持传入图片 URL 或 Base64（确保可访问）。
- Base64 方式不应包含 `data:image/...;base64,` 前缀，只传纯 Base64 字符串。
- 格式：`.jpg/.jpeg/.png`。
- 大小：不超过 `10MB`。
- 尺寸：宽高均不小于 `300px`，宽高比介于 `1:2.5 ~ 2.5:1`。
- `image` 与 `image_tail` 至少二选一，不能同时为空。
- 文档给出互斥：`image_tail`、`dynamic_masks/static_mask`、`camera_control` 三选一，不可同时使用（以页面规则为准）。

## `prompt` / `multi_prompt` / `negative_prompt`

`prompt` 要点：

- 不能超过 `2500` 个字符。
- 当 `multi_shot=false` 或 `shot_type=intelligence` 时不得为空。
- 可用 `<<<element_1>>>`、`<<<image_1>>>`、`<<<video_1>>>` 指定引用对象。
- 当引用 `voice_list` 中音色时，`sound` 必须为 `on`，且至多引用 2 个音色。

`multi_prompt` 结构（文档描述）：

- 元素字段：`index`、`prompt`、`duration`

`multi_prompt` 约束：

- 最多 `6` 个分镜、最少 `1` 个分镜；
- 每个分镜相关内容最大长度不超过 `512`；
- 每个分镜时长不小于 `1` 且不大于任务总时长；
- 所有分镜时长和等于任务总时长；
- `multi_shot=true` 且 `shot_type=customize` 时必填。

`negative_prompt`：

- 上限 `2500` 字符；
- 建议以正向提示词中的负向句子补充负向信息。

## `element_list` / `voice_list`

`element_list`（主体参考）：

- 基于主体库主体 ID；
- 页面描述最多支持 `3` 个参考主体；
- `element_list` 与 `voice_list` 互斥，不能共存。

`voice_list`（音色引用）：

- 一次任务至多引用 `2` 个音色；
- `voice_id` 来源于音色定制接口返回或系统预置音色（页面提供“音色定制相关 API”说明）；
- 当 `voice_list` 非空且 `prompt` 中引用音色 ID 时，按“有指定音色”计费。

## 运动笔刷参数

### `static_mask`

- 静态笔刷 mask 图，支持 URL/Base64（格式要求同 `image`）。
- 与 `image` 长宽比需一致，否则任务失败。
- `static_mask` 与 `dynamic_masks.mask` 分辨率必须一致，否则任务失败。

### `dynamic_masks`

- 动态笔刷配置数组，最多 `6` 组；
- 每组包含：
  - `mask`：涂抹区域图（URL/Base64，格式要求同 `image`）
  - `trajectories`：运动轨迹坐标序列

轨迹规则（页面描述）：

- 5 秒视频时，轨迹点个数范围 `[2,77]`；
- 坐标系原点为图片左下角；
- 传入顺序决定轨迹方向与连接顺序。

## `camera_control` 对象

`type` 枚举：

- `simple`
- `down_back`
- `forward_up`
- `right_turn_forward`
- `left_turn_forward`

`config`（仅 `type=simple` 时填写）字段：

- `horizontal`
- `vertical`
- `pan`
- `tilt`
- `roll`
- `zoom`

共同规则：

- 范围均为 `[-10,10]`；
- 六字段 6 选 1：只能有 1 个字段非 0，其余为 0。

## 其他常用参数

- `cfg_scale`：范围 `[0,1]`，页面说明 `kling-v2.x` 不支持。
- `mode`：`std`（标准/基础）或 `pro`（专家/高品质）。
- `duration`：单位秒，支持范围与模型版本/模式相关（详见能力地图）。
- `watermark_info.enabled`：`true` 生成水印，`false` 不生成；暂不支持自定义水印。
- `callback_url`：任务状态变更回调地址。
- `external_task_id`：自定义任务 ID，不覆盖系统 task_id，但可用于查询（单用户需唯一）。

