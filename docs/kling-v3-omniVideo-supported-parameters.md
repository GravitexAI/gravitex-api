# 可灵 Kling-V3 OmniVideo 支持参数（请求体）

来源（Kling AI 文档）：[https://app.klingai.com/cn/dev/document-api/apiReference/model/OmniVideo](https://app.klingai.com/cn/dev/document-api/apiReference/model/OmniVideo)

接口：`POST /v1/videos/omni-video`

> 说明：文档中同一请求体参数会因 `model_name`、`multi_shot/shot_type`、以及是否使用 `video_list`（视频编辑/视频参考）而出现“必填/无效/不支持”的条件。本文件聚焦 `kling-v3-omni`（Omni 模型）对应的可用参数，并保留页面上关键约束。

## 顶层参数

| 参数 | 类型 | 可选值/约束（取自文档） | 说明 |
|---|---|---|---|
| `model_name` | `string`（可选） | 默认：`kling-video-01`；可选：`kling-v3-omni` | 模型名称。Omni 模型可选 `kling-v3-omni`。 |
| `multi_shot` | `boolean`（可选） | `true/false`（文档示例默认 `false`） | 是否生成多镜头视频。`true` 时：`prompt` 无效，且不支持设定首尾帧生视频。`false` 时：`shot_type` 及 `multi_prompt` 无效。 |
| `shot_type` | `string`（可选） | `customize`、`intelligence` | 分镜方式。当 `multi_shot=true` 时必填。 |
| `prompt` | `string`（可选） | 文档要求：不能超过 `2,500` 个字符；当 `multi_shot=false` 或 `shot_type=intelligence` 时不得为空 | 文本提示词（可包含正向描述/负向描述模板）。 |
| `multi_prompt` | `array`（可选） | 最多 `6` 个分镜，最小 `1` 个分镜（元素结构与约束见下方） | 分镜提示词数组（仅当 `multi_shot=true` 且 `shot_type=customize` 时必填）。 |
| `image_list` | `array`（可选） | 包括主体/场景/风格等参考图片；支持 `image_url`（URL 或 Base64），以及可选 `type`：`first_frame` / `end_frame` | 参考图列表，用于图生视频（含首帧/首尾帧）等多种能力。 |
| `element_list` | `array`（可选） | 主体参考列表（元素：`{ element_id: long }`）；文档给出主体数量限制（见下方） | 基于主体库中主体 ID 的参考主体列表。 |
| `video_list` | `array`（可选） | 参考视频列表（每段对象：`video_url`、`refer_type`、`keep_original_sound`） | 参考视频/视频编辑/视频延长等能力来源。 |
| `mode` | `string`（可选） | `std`、`pro` | 生成视频模式：`std` 标准/基础；`pro` 专家/高表现。 |
| `aspect_ratio` | `string`（可选） | `16:9`、`9:16`、`1:1` | 生成视频画面纵横比（宽:高）。 |
| `duration` | `string`（可选） | 文档页面给出可选示例值：`3..15`（单位秒） | 视频时长（单位 s）。若进行“视频编辑功能（refer_type=base）”，则文档注明该参数无效，输出与输入视频时长对齐。 |
| `watermark_info` | `object`（可选） | `watermark_info.enabled: true/false` | 是否生成含水印结果；文档提示暂不支持自定义水印。 |
| `callback_url` | `string`（可选） | URL | 任务结果回调通知地址；配置后服务端在状态变更时主动通知。 |
| `external_task_id` | `string`（可选） | 用户自定义任务 ID（需要单用户唯一性） | 不覆盖系统生成的任务 ID，但可通过该 ID 查询任务。 |

## `prompt` 约束要点

- 字符上限：不能超过 `2,500` 个字符。
- 当 `multi_shot=false` 或 `shot_type=intelligence` 时，当前 `prompt` 不得为空。
- Omni 模型提示词支持使用 `<<<>>>` 模板化方式，指定元素/图片/视频等，例如 `<<<image_1>>>`、`<<<video_1>>>`。

## `multi_prompt` 数组结构与约束

数组元素（文档描述字段）：

- `index`：分镜序号
- `prompt`：分镜提示词（可包含正向/负向描述模板）
- `duration`：分镜时长

关键约束（取自文档）：

- 最多支持 `6` 个分镜，最小支持 `1` 个分镜。
- 每个分镜相关内容最大长度不超过 `512`。
- 多分镜时：每个分镜时长要求、不大于任务总时长且不小于 `1`，并且所有分镜时长之和等于任务总时长。
- 仅当 `multi_shot=true` 且 `shot_type=customize` 时，`multi_prompt` 必填。

## `image_list` 数组结构与约束

每个数组元素支持的字段（文档给出 `image_url` 必填、`type` 可选）：

- `image_url`：图片 URL 或 Base64（不得为空）
- `type`（可选）：图片帧类型
  - `first_frame`：首帧
  - `end_frame`：尾帧

图片要求（取自文档）：

- 格式：`.jpg/.jpeg/.png`
- 单张文件大小：`<=10MB`
- 尺寸：宽高都不小于 `300px`，宽高比 `1:2.5 ~ 2.5:1`

数量/模型限制（取自文档）：

- 有参考视频时：参考图片数量与参考主体数量之和不得超过 `4`，且不支持使用视频角色主体。
- 无参考视频时：参考图片数量与参考主体数量之和不得超过 `7`。
- 首帧或首尾帧生视频时：
  - 文档提示：暂时不支持“仅尾帧”；有尾帧时必须有首帧图。
  - 首帧或首尾帧生视频时，不能使用视频编辑功能。
- 使用 `kling-video-o1` 模型时：数组中超过 `2` 张图片时，不支持设置尾帧。

首帧/首尾帧生视频的帧类型约束：

- `type` 仅在图片需要作为首帧/尾帧时填写；非首帧/尾帧请勿配置 `type`。

## `element_list`（主体参考）结构与限制

数组元素字段：

- `element_id: long`（主体库中的主体 ID）

主体数量限制（取自文档）：

- 当使用首帧或首尾帧生视频时，最多支持 `3` 个主体。
- 文档注明：使用首帧/首尾帧生视频时，`kling-v3-omni` 模型最多支持 `3` 个主体；`kling-video-o1` 不支持主体。

## `video_list`（视频参考/视频编辑）结构与约束

每段对象字段（文档示例）：

- `video_url`：视频 URL（不得为空）
- `refer_type`：视频类型
  - `feature`：特征参考视频
  - `base`：待编辑视频（视频编辑）
- `keep_original_sound`：保留原声
  - `yes`：保留
  - `no`：不保留

视频类型约束（取自文档）：

- `refer_type=base` 对应“视频编辑功能”：当使用该功能时，`duration` 参数无效，输出结果与输入视频时长相同（并按输入视频时长取整计量计费）。
- 文档提示：当存在参考视频时的声音策略受控（例如 `sound` 相关取值限制以页面说明为准）；这里以 `keep_original_sound` 字段为准。

视频要求（取自文档）：

- 格式：仅支持 `MP4/MOV`
- 时长：不少于 `3` 秒（上限与模型版本有关，详见能力地图）
- 分辨率：`720px-2160px`（宽高尺寸）
- 帧率：`24-60fps`（生成视频输出 `24fps`）
- 体积/大小：至多 `1` 段视频，`<=200MB`

## `watermark_info` 对象

- `watermark_info.enabled: true` 为生成，`false` 为不生成
- 暂不支持自定义水印

