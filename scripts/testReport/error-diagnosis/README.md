# 错误账单诊断工具

只输入错误明细账单，自动生成离线网页与事实报告。**本地生成无需 AI、无需数据库、无需第三方 Python 包**；用量分析已从这套脚本中剥离。原有历史双模块报告保留不变。

需要 Python 3.10 或更新版本。整个目录保留在一起，网页生成后可单独复制使用。

## 1. 生成诊断网页

```powershell
python error_diagnosis.py "C:\账单\错误明细.xlsx"
```

一周有多份账单时，直接传入多份；不会自动去重，使用前确保文件范围不重叠：

```powershell
python error_diagnosis.py "C:\账单\周一.xlsx" "C:\账单\周二.xlsx" "C:\账单\周三.xlsx"
```

支持 `.xlsx`、`.xlsm`、`.csv`、`.tsv`。默认从内容识别工作表、字段与数据日期，输出：

```text
output/
  2026-09-14/                     # 单日
    error_diagnosis_20260914.html
    error_diagnosis_report_20260914.md
  2026-09-14_2026-09-20/          # 跨日/周报：实际起止日期，不补齐虚构日期
    error_diagnosis_20260914_20260920.html
    error_diagnosis_report_20260914_20260920.md
```

同范围重跑更新这两个文件，不修改源账单或其他日期的文件。每个文件以原子替换方式写入；若写入中断，不会留下半截 HTML 或 Markdown。两文件不是跨文件事务。

网页包含：

- 类别、用户、实际渠道 / 模型 TOP5。
- 用户、模型、分组、渠道、类别五维联动候选；候选较多时可输入查找。
- 跨天日期范围、每日小时范围、维度关键词筛选。
- 同类报错一行、实际渠道 / 模型配对、观测首末时间。
- 限量归一化证据、来源文件/工作表/行号，以及缺失字段和排除记录说明。

统计均由本地确定性规则生成，**不会附带伪装成 AI 的优化建议**。报错类别是规则标签，不是已确认根因。

## 2. 字段兼容与缺失值

字段顺序可以变化，可识别前 30 行内的表头。中文/英文别名定义在 `diagnostics/ingest.py`：

| 语义字段 | 常见名称 | 缺失处理 |
| --- | --- | --- |
| `time` | 时间、日期时间、创建时间、timestamp、created_at | 保留记录，显示未知日期/时刻 |
| `user` | 用户、用户名、用户ID、username、user_id | 显示“未提供”，统计缺失量 |
| `model` | 服务/模型、模型、模型名称、model | 同上 |
| `group` | 资源分组、分组、resource_group | 同上，不当作渠道 |
| `channel` | 渠道名称、渠道、渠道ID、channel_name | 同上 |
| `error` | 计费过程、错误信息、失败原因、error_message、message | 有状态/类型列仍可统计；无文本标记为缺失 |
| `status` | 状态码、HTTP状态码、status_code、status | 不凭空补状态码 |
| `kind` | 类型、日志类型、record_type、result | 默认输入已是错误明细；明确消费/成功记录排除并计数 |
| `count` | 错误数、错误次数、error_count、occurrences | 无列则每行 1；空白按 1 并提示；非法或负数行排除并提示 |

至少能识别错误、状态、类型三者之一，否则停止并要求明确映射。存在两个同义列或多个同等匹配工作表时停止，不随意猜测。

自定义映射只需指定不同的字段：

```powershell
python error_diagnosis.py "账单.xlsx" --sheet "导出明细" --column error=失败描述 --column model=服务名称 --column time=发生时刻
```

时间支持 ISO 日期时间、带时区时间、Unix 秒/毫秒、Excel 日期序列及 1904 日期制，统一到 UTC+8。不读取公式表达式，仅使用文件中保存的单元格值。

如果文件完全没有日期，默认放到 `output/unknown-date/`。已知日期可显式补充：

```powershell
python error_diagnosis.py "无时间账单.csv" --date 2026-09-14
```

`--date` 只给无效/缺失时间补日期，不覆盖有效时间，也不伪造小时。网页会显示未知时刻数量；选择具体小时后不计入未知时刻。

其他选项：

```powershell
python error_diagnosis.py "账单.csv" --encoding gb18030 --output "D:\诊断报告"
python error_diagnosis.py --help
```

## 3. 大数据处理方式

- XLSX 使用 ZIP/XML 流式读取，不把整表放入内存。
- XLSX 的 sharedStrings 临时存入任务专属二进制文件，以偏移索引和有限缓存读取；无 SQLite 或其他数据库。临时文件在关闭时删除。
- 原始行读取后立即归并成“用户 × 模型 × 分组 × 渠道 × 类别 × 日期 × 小时”桶；维度字符串字典编码。
- 所有错误量与维度/时间统计完整保留，不以抽样代替总量。
- 默认最多保留 **4,000 种证据**、**每桶 2 个样本**、**每条 1,600 字符**；未保留部分明确标注，可回查源行。证据是按读取顺序保留的样本，不声称是完整频次排行。
- 浏览器用 **Web Worker** 建索引并在后台聚合；主线程只渲染 TOP5、分页类别和有限样本，避免把几十万原始行转成 DOM。
- 候选框最多渲染 200 项，但可输入关键词查找其余值；每类最多展示 20 个渠道 / 模型组合，计数仍含全部组合。

内存随**唯一聚合桶与唯一维度值数量**增长，不是绝对恒定。默认上限 **250,000 桶**，超限明确停止，不静默丢弃；可拆分输入范围或自行调高：

```powershell
python error_diagnosis.py "账单.xlsx" --max-buckets 400000 --max-samples 6000 --samples-per-bucket 3 --evidence-chars 2000
```

更多样本和更高桶上限会增加内存、HTML 大小与浏览器负担。关键词仅筛选维度，不对未保留的日志全文做不准确的计数搜索。

本机验收（2026-09-18，Windows / Python，非跨机器性能承诺）：

| 数据 | 规模 | 实测结果 |
| --- | --- | --- |
| 原始错误 XLSX | 96,987 行、1,797 个聚合桶 | 最终版本读取/归一化/聚合约 15.2 秒，总量对平 |
| 7 天合成 XLSX | 350,000 行、40,320 个桶、2,004 种样本 | 读取至成品约 14.5 秒；峰值工作集约 76 MiB；HTML 3.12 MiB |
| 合成周报浏览器 | 350,000 条统计 | 首次后台聚合约 23 ms；五维联动、日期/小时、空态、移动端均通过 |

合成表为 6 个业务字段、18.55 MiB，和真实文件的列数/字符串分布不同；不要简单按行数推算所有账单的耗时。

## 4. 独立 AI 分析

先编辑项目根目录 `.env`：

```dotenv
BASE_URL=https://你的接口地址/v1
MODEL=你的模型名称
API_KEY=你的密钥
```

然后针对已生成的 HTML 执行：

```powershell
python ai_analysis.py "output\2026-09-14\error_diagnosis_20260914.html"
```

默认更新配套 Markdown：保留本地统计事实，再添加单独标注“待人工核验”的 AI 问题分析、优化方案和验收建议。HTML 不连接 API，密钥不写入 HTML。

接口使用 OpenAI 兼容 **Chat Completions** 请求体（`model`、`messages`、`stream: false`）和 Bearer 鉴权。基础地址填写到版本路径，例如 `/v1`；也可填写完整 `/chat/completions` 地址。不同服务商仍需提供这一协议的文本响应。参考规范：`https://platform.openai.com/docs/api-reference/chat/create`。

**实际发送内容**：完整类别总量、TOP 用户/组合、时间分布、质量说明，以及有限脱敏证据。不会上传原始 XLSX、IP 列或令牌名称；用户榜默认匿名。证据会去除可识别的 URL、请求 ID、IP、邮箱和常见密钥格式，但不承诺识别所有业务敏感内容。

发送前可查看准确载荷；此命令不联网、不要求填写密钥：

```powershell
python ai_analysis.py "output\2026-09-14\error_diagnosis_20260914.html" --dry-run
```

可选 `.env` 参数：

| 参数 | 默认 | 用途 |
| --- | --- | --- |
| `AI_TIMEOUT` | 120 | 单次请求超时秒数 |
| `AI_RETRIES` | 1 | 429/部分 5xx 最多额外重试次数；网络超时不自动重复，以免重复计费 |
| `AI_MAX_INPUT_CHARS` | 48000 | JSON 载荷字符数上限，非 Token 数；超限优先减少证据，不截断统计 JSON |
| `AI_INCLUDE_USER_NAMES` | false | 是否发送用户榜真实名称 |
| `AI_ALLOW_HTTP` | false | 远程 HTTP 显式启用；localhost 默认可用 HTTP |
| `AI_MAX_TOKENS` | 空 | 不填则不发送该参数，兼容服务商默认值 |
| `AI_TOKEN_LIMIT_FIELD` | max_tokens | 可改成 max_completion_tokens |

支持 `OPENAI_BASE_URL`、`OPENAI_MODEL`、`OPENAI_API_KEY` 别名。进程环境中的同名变量覆盖 `.env`；实际密钥文件已在 `.gitignore` 中排除。API 错误、无效响应或输出被截断时，保留已有报告，不把失败结果当作有效分析。

## 5. 验证与目录

```powershell
python -m unittest discover -s tests -v
python tests/benchmark.py --rows 350000
```

浏览器回归测试需已有 Playwright 与 Chromium/Edge，仅用于开发，不是用户运行依赖：

```powershell
node tests/browser_check.cjs --html "报告.html" --modules "现有node_modules路径" --browser "msedge.exe路径"
```

```text
error_diagnosis.py       本地生成入口
ai_analysis.py          独立 AI 分析入口
.env                    用户填写的接口配置
diagnostics/            流式读取、归一化、聚合、网页模板
tests/                  字段兼容、API、浏览器、压力测试
output/                 最终 HTML / Markdown
```
