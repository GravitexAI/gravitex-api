# 可灵 Kling-V3 `textToVideo` 支持参数（请求体）

来源（Kling AI 文档）：[https://app.klingai.com/cn/dev/document-api/apiReference/model/textToVideo](https://app.klingai.com/cn/dev/document-api/apiReference/model/textToVideo)

接口：`POST /v1/videos/text2video`

> 说明：文档中同一请求体参数会因 `model_name`、`shot_type`、视频模式等而出现“必填/无效/不支持”的条件；本文件按页面中对 `kling-v3` 适用的参数区块整理，并保留了关键约束提示（如 `kling-v2.x 模型不支持此参数`、以及“详见能力地图”）。

## 顶层参数

| 参数 | 类型 | 可选值/约束（取自文档） | 说明 |
|---|---|---|---|
| `model_name` | `string`（可选） | `kling-v1`、`kling-v1_6`、`kling-v2-master`、`kling-v2_1-master`、`kling-v2_5-turbo`、`kling-v2_6`、`kling-v3` | 模型名称；文档提示命名统一使用 `model_name`（原 `model` 字段向前兼容）。 |
| `multi_shot` | `boolean`（可选） | `true/false` | 是否生成多镜头视频。`true` 时：`prompt` 无效，且不支持设定首尾帧生视频。`false` 时：`shot_type` 及 `multi_prompt` 无效。 |
| `shot_type` | `string`（可选） | `customize`、`intelligence` | 分镜方式。当 `multi_shot=true` 时必填。 |
| `prompt` | `string`（可选） | 非空条件见下方说明；字符上限见下方 | 正向文本提示词（Omni 模型可通过 Prompt 与主体/图片/视频等实现多种能力）。当 `multi_shot=false` 或 `shot_type=intelligence` 时不得为空。 |
| `multi_prompt` | `array`（可选） | 最多 6 个分镜，最小 1 个分镜；字符/时长约束见下方 | 分镜提示词数组（仅当多镜头/自定义分镜条件满足时使用）。当 `multi_shot=true` 且 `shot_type=customize` 时必填。 |
| `negative_prompt` | `string`（可选） | 字符上限 `2500` | 负向文本提示词。不能超过 2500 个字符；建议通过正向提示词中的负向句子补充负向提示信息。 |
| `sound` | `string`（可选） | `on/off`（示例：`off`） | 生成视频时是否同时生成声音。`sound=on` 时才可通过 `<<<voice_1>>>` 指定音色；并且当 `-voice_list` 不为空且 prompt 引用音色 ID 时按“有指定音色”计费。 |
| `cfg_scale` | `float`（可选） | 范围：`[0, 1]`（示例：`0.5`） | 生成视频的自由度；值越大，模型的自由度越小。文档注明：`kling-v2.x 模型不支持此参数`。 |
| `mode` | `string`（可选） | `std`、`pro` | 视频生成模式。`std`：标准/基础模式（性价比高）；`pro`：专家模式（高品质/高表现，生成视频质量更佳）。 |
| `camera_control` | `object`（可选） | `type`：`simple/down_back/forward_up/right_turn_forward/left_turn_forward` | 控制相机运动的条款（如不指定，模型将根据输入的文本/图片智能匹配）。详见“能力地图”提示。 |
| `aspect_ratio` | `string`（可选） | `16:9`、`9:16`、`1:1` | 生成视频帧的宽高比（宽:高）。 |
| `duration` | `string`（可选） | 取值：`3` 到 `15`（单位：秒） | 视频长度，单位：秒。 |
| `watermark_info` | `object`（可选） | `watermark_info.enabled`：`true/false` | 是否同时生成含水印的结果。通过 `enabled` 参数定义；文档提示暂不支持自定义水印。 |
| `callback_url` | `string`（可选） | URL | 任务结果回调通知地址；若配置，服务端会在任务状态发生变更时主动通知。通知消息 schema 见 Callback 协议。 |
| `external_task_id` | `string`（可选） | 用户自定义任务 ID | 自定义任务 ID。不会覆盖系统生成的任务 ID，但支持通过该 ID进行任务查询；单用户下需保证唯一性。 |

## `prompt` 约束要点

- 字符上限：不能超过 `2500` 个字符。
- Omni 模型提示：可用 `<<<element_1>>>`、`<<<image_1>>>`、`<<<video_1>>>` 等格式指定主体/图片/视频。
- 音色引用：用 `<<<voice_1>>>` 来指定音色（序号同 `voice_list`）；至多引用 `2` 个音色；指定音色时 `sound` 必须为 `on`。

## `multi_prompt` 数组结构与约束

数组元素（文档描述字段）：

- `index`：分镜序号
- `prompt`：分镜提示词（可包含正向描述和负向描述）
- `duration`：分镜时长（与任务总时长的关系见下方约束）

约束（取自文档）：

- 最多支持 `6` 个分镜，最小支持 `1` 个分镜；
- 每个分镜的相关内容最大长度不超过 `512`；
- 每个分镜的时长：不大于当前任务总时长，且不小于 `1`；
- 所有分镜的时长之和等于当前任务的总时长；
- 当 `multi_shot=true` 且 `shot_type=customize` 时，`multi_prompt` 必填；
- 多分镜自定义时的承载格式：通过 `key:value` 形式承载（文档给出示例说明）。

字符上限：

- `multi_prompt`（对应分镜文本相关内容）不能超过 `2500` 个字符（以页面提示为准）。

## `camera_control` 对象结构

`camera_control`（文档显示）：

- `type`：相机运动类型（必填）
- `config`：仅在 `type=simple` 时需要填写（其它运镜类型下不填）

`type` 枚举：

- `simple`：简单运镜；此类型下可在 `config` 中六选一进行运镜
- `down_back`：镜头下压并后退（此类型下 `config` 参数无需填写）
- `forward_up`：镜头前进并上仰（此类型下 `config` 参数无需填写）
- `right_turn_forward`：先右旋转后前进（此类型下 `config` 参数无需填写）
- `left_turn_forward`：先左旋并前进（此类型下 `config` 参数无需填写）

`config` 内的 6 个字段（文档显示为 `float`，范围均为 `[-10, 10]`）：

- `horizontal`：水平方向（水平轴平移）
- `vertical`：垂直方向（垂直轴平移）
- `pan`：摇摄（水平面内旋转；绕 y 轴旋转）
- `tilt`：俯仰（垂直面内旋转；绕 x 轴旋转）
- `roll`：翻滚（绕 z 轴旋转）
- `zoom`：缩放（焦距/视野变化）

进一步约束（取自文档）：

- `config` 的以下参数是“6 选 1”：只能有一个字段不为 `0`，其余字段为 `0`。

## `watermark_info` 对象

- `watermark_info.enabled`：`true` 为生成，`false` 为不生成
- 文档提示：暂不支持自定义水印

