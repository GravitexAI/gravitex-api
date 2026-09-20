# 错误诊断接入管理端 /logManage 设计

本文档只讲「这套 CLI 工具怎么变成管理端一个按钮」。工具本身的用法、字段兼容、
性能数据看 `README.md`；交付规则看 `AGENTS.md`。

## 1. 目标与非目标

**目标**：管理员在账单日志页选好筛选条件，点一下就能拿到一份离线可交互的错误诊断看板，
可选附一份 AI 分析 Markdown。

**非目标**：

- 不做历史归档。任务产物 24 小时后随工作目录一起删除，要留就自己下载。
- 不替代 `ai-diagnose-modal.vue`。那个出的是对话式 AI 报告，这个出的是本地确定性统计看板，
  两个按钮并列，各走各的。
- 不改 `error_diagnosis.py` / `ai_analysis.py` / `diagnostics/` 的任何业务逻辑。
  集成层只负责喂数据和收产物。

## 2. 整体链路

```
[Vue error-diagnosis-modal.vue]
   │ ① POST /gravitex/api/error-diagnosis/precheck   → 命中多少条错误日志
   │ ② POST /gravitex/api/error-diagnosis/run        {query + 诊断参数 + AI 配置}
   ▼
[Java ErrorDiagnosisController / ErrorDiagnosisService]
   │ ③ queryMaxId 钉住分页快照 → countForExport 校验上限 → keyset 分页
   │ ④ 边查边把错误日志写成 CSV，流式 POST 到 127.0.0.1:8900/diagnosis/run
   ▼
[Python claude-test-service/diagnosis_runner.py]
   │ ⑤ 请求体落盘成 input.csv → 子进程跑 error_diagnosis.py → 得到 HTML 看板
   │ ⑥ 勾了 AI 就再跑一次 ai_analysis.py，密钥经子进程环境变量注入
   ▼
   ⑦ 前端轮询 /tasks/{id}；完成后 fetch HTML 为 blob
      → window.open(URL.createObjectURL(blob))  新标签页看板
      → downloadByData 另存本地
```

## 3. 关键取舍

| 决策 | 选择 | 理由 |
| --- | --- | --- |
| 数据来源 | 后端按筛选条件直接取数 | 表结构自己控制，列映射/编码/工作表选择这些配置项全部不需要 |
| Java→Python 传数据 | CSV 流式上传 | 工具原生支持 `.csv`，列头直接命中 `ingest.py` 的别名表，**工具本体零改动** |
| 请求形态 | 原始 body + `X-Diagnosis-Params` 头 | multipart 需要 `python-multipart`，服务器漏装会让**整个服务起不来**，把渠道测试一起拖下水；参数不放 query string 是因为里面有 AI 密钥，会进访问日志 |
| 服务进程 | 复用 8900，独立 Runner | 诊断十几秒、渠道测试十几分钟，子进程互不抢锁，运维零新增 systemd unit |
| 新标签页鉴权 | 前端 blob URL | 看板是自包含离线页，blob 里完全能跑；不用做免鉴权 HTML 路由，也不用把 token 塞进 URL |
| 并发 | 2 个槽 | 诊断是纯本地 CPU 活，不打上游接口；卡成单槽的话一个人跑 AI 就能把别人挡 2 分钟 |
| AI 失败 | 任务仍算成功 | 看板是本地确定性统计，接口报错时照样可用；失败原因单独记在 `ai_error` |

## 4. CSV 契约（唯一的跨端约定）

Java 端 `ErrorDiagnosisService.CSV_HEADER` 写这八列，`diagnostics/ingest.py` 的 `ALIASES` 认这八列。
**两边没有编译期检查**：改错了线上表现是「诊断跑完但一条数据都没有」，不抛异常、不报错。

| CSV 列头 | Java 取值 | ingest 语义字段 |
| --- | --- | --- |
| `时间` | `createdAt`（Unix 秒） | `time` |
| `用户名` | `username` | `user` |
| `模型` | `modelName` | `model` |
| `资源分组` | `group` | `group` |
| `渠道名称` | `channelName` | `channel` |
| `错误信息` | `content`，截断到 4000 字符 | `error` |
| `状态码` | `other` JSON 的 `status_code` | `status` |
| `类型` | 固定 `错误` | `kind` |

钉住这个契约的测试：

- `claude-test-service/tests/test_diagnosis.py::test_java_csv_header_is_recognised_end_to_end`
  （真子进程跑通，验证八列全部被识别）
- `Gravitex-API-End/.../service/ErrorDiagnosisCsvTest`（列序、RFC 4180 转义、缺失值、状态码归一化）

改动任意一端的列名，这两个测试必须同步改。

## 5. 接口清单

### Python（claude-test-service，8900）

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/diagnosis/meta` | 可用性、默认 AI 地址与模型（**不含密钥**）、参数默认值 |
| POST | `/diagnosis/run` | body = CSV 原文，参数在 `X-Diagnosis-Params` 头 → `{task_id}` |
| GET | `/diagnosis/tasks/{id}` | `{status, stage, done, total, error, ai_error, errors, buckets, html_ready, report_ready}` |
| GET | `/diagnosis/tasks/{id}/html` | 离线看板 |
| GET | `/diagnosis/tasks/{id}/report` | AI 分析 Markdown |

`X-Diagnosis-Params` 的 JSON 字段：`total_rows`、`max_buckets`、`max_samples`、
`samples_per_bucket`、`evidence_chars`、`ai_enabled`、`ai_base_url`、`ai_model`、
`ai_api_key`、`ai_include_user_names`。

### Java（/gravitex/api/error-diagnosis）

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/meta` | 转发 |
| POST | `/precheck` | body = `LogsBo` → `{rowCount, maxRows, exceedsLimit}` |
| POST | `/run` | body = `{query: LogsBo, maxBuckets, ..., aiEnabled, aiApiKey, ...}` → `{task_id}` |
| GET | `/tasks/{id}` | 转发 |
| GET | `/tasks/{id}/html` \| `/report` | 二进制透传 |

复用配置 `gravitex.channel-test.base-url`，不新增配置项。

## 6. 边界与限制

- **行数硬上限 50 万**（`ErrorDiagnosisService.MAX_ROWS`）。依据是 README 的实测数据：
  35 万行约 14.5 秒、峰值内存约 76 MiB、HTML 约 3.12 MiB；再往上 HTML 本身会大到打不开。
  超限时 `/run` 直接报错让用户缩小范围，不会先跑十几分钟再失败。
- **日志类型强制为「错误」**（`type=5`）。前端锁死、后端 `forceErrorLogsOnly` 再覆盖一次，
  混进消费日志会让统计口径失真。
- **单条错误文本截断到 4000 字符**。个别上游会把整段响应体塞进 `content`，不截断的话
  50 万行能拼出几个 GB。工具默认只保留每条证据 1600 字符，前端高级参数上限也是 4000。
- **分页快照**：`run` 先 `queryMaxId` 钉住 `exportMaxId`，之后的 count 和分页基于同一批数据，
  否则诊断过程中新写入的错误日志会让总量和实际行数对不上。
- **进度粒度 10 万行**。`diagnostics/engine.py` 每读 10 万行往 stderr 打一行，服务层解析这行
  换算百分比。数据量小于 10 万时进度条会一直停在 0% 直到跑完——这是如实反映，不造假进度。
- **AI 密钥不落盘**。弹窗现填，随请求头传到 Python，再经子进程环境变量交给 `ai_analysis.py`；
  服务端 `.env` 只提供默认地址和模型，`/diagnosis/meta` 不回传密钥。

## 7. 部署

部署流程见 `scripts/testReport/RUNBOOK.md`：手动把目录传到 `/workplace/py/testReport/`，
然后在服务器上跑 `bash after-upload.sh`。

对本功能的要点：

- **`error-diagnosis` 要和另外两个目录同级**，服务靠相对路径 `../error-diagnosis` 找它。
- **不需要给它建 venv**，它只用标准库，跑的时候直接借 `claude-test-service/.venv` 的解释器
  （需要 Python 3.10+）。这样也绕开了「Mac 的 venv 传上来会断链」那一整类问题。
- **没有新增 Python 依赖**，不需要 pip install。这是当初不用 multipart 的直接原因。
- **别覆盖服务器上的 `error-diagnosis/.env`**，本地那份是占位值。`after-upload.sh` 检测到
  占位值会提醒。密钥不受影响——那个是管理员在弹窗里现填的。
- `after-upload.sh` 会自检诊断脚本能否跑起来，验收阶段多打印一行 `错误诊断:`，
  读的是 `/diagnosis/meta` 的 `available`；不可用时直接打印原因。
- 诊断子进程也被纳入了「重启前挡住正在跑的任务」的守卫，和渠道测试一视同仁。

## 8. 相关文件

| 端 | 文件 |
| --- | --- |
| Py 工具 | `error-diagnosis/`（本目录，逻辑未改，仅产物命名英文化） |
| Py 服务 | `claude-test-service/diagnosis_runner.py`、`app.py`、`tests/test_diagnosis.py` |
| Java | `controller/ErrorDiagnosisController.java`、`service/ErrorDiagnosisService.java`、`test/.../ErrorDiagnosisCsvTest.java` |
| 管理端 | `api/gravitex-api/errorDiagnosis.ts`、`views/gravitex-api/logManage/error-diagnosis-modal.vue`、`logManage/index.vue` |
| 部署 | `scripts/testReport/after-upload.sh`、`scripts/testReport/RUNBOOK.md` |
