# Codex 接入 Gravitex 配置指南

> 适用：Codex CLI / Codex Desktop（ChatGPT.app 内置）/ Codex IDE 扩展
> 实测环境：`codex-cli 0.154.0-alpha.6.2` + Desktop `26.908.40834` + macOS 26.5.2
> 文中标 **【实测】** 的结论是在真机上跑出来的，标 **【文档】** 的来自官方文档或社区验证
> 最后更新：2026-09-14

---

## 目录

1. [开始之前：三件必须知道的事](#一开始之前三件必须知道的事)
2. [API Key 的三种配置方式](#二api-key-的三种配置方式)
3. [完整配置模板](#三完整配置模板)
4. [参数逐项说明](#四参数逐项说明)
5. [模型目录（model_catalog_json）](#五模型目录model_catalog_json)
6. [验证清单](#六验证清单)
7. [故障排查](#七故障排查)
8. [已知坑位速查](#八已知坑位速查)

---

## 一、开始之前：三件必须知道的事

### 1.1 Codex 只走 Responses API

Codex 在 2026-02 移除了 `chat/completions` 通路，现在**只能**用 `wire_api = "responses"`。
配成 `"chat"` 会直接启动失败。

Gravitex 的 Responses 端点是 `POST /v1/responses`。

### 1.2 Base URL 必须带 `/v1`

Codex 是拿 `base_url` 直接拼 `/responses`：

| `base_url` 写法 | 实际请求 | 结果 |
|---|---|---|
| `https://api.gravitex.ai/v1` | `https://api.gravitex.ai/v1/responses` | ✅ |
| `https://api.gravitex.ai` | `https://api.gravitex.ai/responses` | ❌ 404 |
| `https://api.gravitex.ai/v1/` | 尾斜杠可能导致双斜杠 | ⚠️ 不要带 |

> ⚠️ 网上有些中转站的教程写「Base URL 不要加 `/v1`」——**那是他们的网关把端点挂在根路径**。
> Gravitex 必须加 `/v1`，别照抄。

### 1.3 配置文件必须是用户级

| 路径 | provider 定义 | 说明 |
|---|---|---|
| `~/.codex/config.toml` | ✅ 生效 | 唯一能定义 provider 和认证的地方 |
| `<项目>/.codex/config.toml` | ❌ 被忽略 | 只打一个启动警告。这是安全边界：clone 下来的仓库不能改你的 provider 和 auth |

项目级配置只能放 instructions、trust_level 这类非安全相关的键。

---

## 二、API Key 的三种配置方式

这是最容易踩坑的地方，因为 **CLI 和 Desktop 的进程环境完全不同**。

### 场景对照表

| 方式 | Key 存哪 | CLI | Desktop | 适用场景 |
|---|---|---|---|---|
| **A. `requires_openai_auth`** | 系统钥匙串 / `~/.codex/auth.json` | ✅ | ✅ | **推荐**。个人日常使用、给客户的标准方案 |
| **B. `env_key`** | 环境变量 | ✅ | ❌ 需额外桥接 | CI / 容器 / 脚本化；Key 由密码管理器动态注入 |
| **C. `http_headers` 写死** | 明文在 config.toml 里 | ✅ | ✅ | 内网机器、一次性验证、需要附加自定义头的场景 |

---

### 方式 A：不写死 —— `requires_openai_auth`（推荐）⭐

Key 交给 Codex 自己管，存进**系统钥匙串**或 `~/.codex/auth.json`，配置文件里一个字都不出现。
**CLI 和 Desktop 共用同一份凭据**，不需要碰环境变量。

**配置：**

```toml
cli_auth_credentials_store = "keyring"

[model_providers.gravitex]
name = "Gravitex"
base_url = "https://api.gravitex.ai/v1"
wire_api = "responses"
requires_openai_auth = true
```

**存 Key（终端跑一次，Desktop 也会用这份）：**

```bash
read -s OPENAI_API_KEY          # 粘贴 sk-xxx 后回车，不回显
printf '\n'
printf '%s' "$OPENAI_API_KEY" | codex login --with-api-key
unset OPENAI_API_KEY
codex login status              # 应显示 Logged in using an API key - sk-xxx***xxx
```

PowerShell：

```powershell
$secureKey = Read-Host "API Key" -AsSecureString
$plainKey = [System.Net.NetworkCredential]::new("", $secureKey).Password
$plainKey | codex login --with-api-key
$plainKey = $null
codex login status
```

**换 Key**：重跑一次 `codex login --with-api-key`
**清除**：`codex logout`

> ⚠️ **`requires_openai_auth = true` 和 `env_key` 是二选一**，同时写的话 Codex 会**忽略** `env_key`。
> ⚠️ `~/.codex/auth.json` **不要手工编辑**，由 Codex 自己维护。

**【实测】** `codex doctor` 确认：

```
✓ auth         auth is configured
    auth storage mode        Keyring
    stored auth mode         api_key
    stored API key           true
```

---

### 方式 B：不写死 —— `env_key`（环境变量）

Key 从进程环境读，配置文件里只写变量名。

```toml
[model_providers.gravitex]
name = "Gravitex"
base_url = "https://api.gravitex.ai/v1"
wire_api = "responses"
env_key = "GRAVITEX_API_KEY"
```

```bash
export GRAVITEX_API_KEY="sk-xxxx"     # 写进 ~/.zshrc
codex
```

#### ⚠️ 这个方式在 Desktop 端不生效

Codex Desktop 跑在 **ChatGPT.app** 里，你从 Dock / Spotlight 点开它 → macOS 通过 **LaunchServices** 启动 → **LaunchServices 不加载 `~/.zshrc`**。

结果就是：同一份 `config.toml`，CLI 能跑，Desktop 报 `Missing environment variable: GRAVITEX_API_KEY`。

一条命令验证：

```bash
printenv GRAVITEX_API_KEY | wc -c          # shell 世界
launchctl getenv GRAVITEX_API_KEY | wc -c  # GUI 世界
# 第一个非 0、第二个是 0 → 就是这个问题
```

**桥接办法（macOS）：**

```bash
launchctl setenv GRAVITEX_API_KEY "sk-xxxx"
# 然后 Cmd+Q 完全退出 ChatGPT.app 再重开
```

> ⚠️ `launchctl setenv` 是**会话级**的，**重启后失效**。要持久化得写 LaunchAgent 开机自动执行。
> 给客户用太重，这也是为什么推荐方式 A。

**什么时候该用 B**：CI / Docker / 需要从密码管理器（gopass、1Password CLI）动态注入 Key 的场景，这些本来就是纯 CLI 环境，没有 GUI 问题。

---

### 方式 C：写死 —— `http_headers`

Key 直接明文写进 config.toml 的自定义请求头。

```toml
[model_providers.gravitex]
name = "Gravitex"
base_url = "https://api.gravitex.ai/v1"
wire_api = "responses"

[model_providers.gravitex.http_headers]
Authorization = "Bearer sk-xxxx"
```

**优点**：CLI / Desktop 都直接能用，零额外步骤。
**缺点**：Key 明文落盘。备份、同步 dotfiles、误提交 git 都会泄漏。

**什么时候该用 C**：
- 内网隔离机器，泄漏风险可控
- 快速验证联通性，用完就删
- 需要给上游带**额外自定义头**（这时 `http_headers` 本来就是唯一选择，但 Key 仍建议走方式 A）

> ⚠️ **不要手工设 `Content-Type`**。Codex 自己会设，手工覆盖在传附件（multipart）时会出问题。
> 如果一定要用方式 C，`http_headers` 里只留 `Authorization` 一项。

---

## 三、完整配置模板

### 3.1 推荐配置（方式 A）

```toml
# ============ 模型 ============
model = "gpt-6-astra"
review_model = "gpt-6-astra"
model_reasoning_effort = "high"

# ============ Provider ============
model_provider = "gravitex"
cli_auth_credentials_store = "keyring"

# ============ 模型目录 ============
# 用 scripts/codex/gen_codex_catalog.py 生成，路径必须是绝对路径
model_catalog_json = "/Users/你的用户名/.codex/gravitex-codex-catalog.json"

# ============ 第三方网关建议项 ============
disable_response_storage = true

[model_providers.gravitex]
name = "Gravitex"
base_url = "https://api.gravitex.ai/v1"
wire_api = "responses"
requires_openai_auth = true
```

配合终端跑一次 `codex login --with-api-key` 存 Key。

> ⚠️ **TOML 的顺序陷阱**：所有顶层键（`model`、`model_catalog_json`、`disable_response_storage` 等）
> 必须写在 `[model_providers.*]` 这类 table **之前**。写在后面会被 TOML 当成那个 table 的子键，
> Codex 读不到，但**不会报错**——静默失效，非常难查。

### 3.2 纯 CLI / CI 配置（方式 B）

```toml
model = "gpt-6-astra"
review_model = "gpt-6-astra"
model_provider = "gravitex"
model_catalog_json = "/root/.codex/gravitex-codex-catalog.json"
disable_response_storage = true

[model_providers.gravitex]
name = "Gravitex"
base_url = "https://api.gravitex.ai/v1"
wire_api = "responses"
env_key = "GRAVITEX_API_KEY"
```

### 3.3 写死 Key 配置（方式 C）

```toml
model = "gpt-6-astra"
review_model = "gpt-6-astra"
model_provider = "gravitex"
model_catalog_json = "/Users/你的用户名/.codex/gravitex-codex-catalog.json"
disable_response_storage = true

[model_providers.gravitex]
name = "Gravitex"
base_url = "https://api.gravitex.ai/v1"
wire_api = "responses"

[model_providers.gravitex.http_headers]
Authorization = "Bearer sk-xxxx"
```

---

## 四、参数逐项说明

### 4.1 顶层参数

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `model` | string | ✅ | 默认模型的 slug，必须与模型目录里的 `slug` 和 Gravitex 的模型名**完全一致**。<br>⚠️ **【实测】Desktop 的模型选择器会把选中的模型回写到这一行**，见 [8.1](#81-desktop-会回写-model) |
| `review_model` | string | 建议填 | `/review` 命令和 auto-review 用的模型。**【文档】** [openai/codex#8809](https://github.com/openai/codex/issues/8809) 确认 review 不走 `model`，走这个。<br>不填 → 回落到 Codex 内置默认模型 → 出现「配了 A 却打到 B」 |
| `model_provider` | string | ✅ | 指向下面 `[model_providers.X]` 的 X。名字随便起，只要两边一致 |
| `model_reasoning_effort` | string | 否 | 推理强度：`none` / `low` / `medium` / `high` / `xhigh`。**必须是目标模型 `supported_reasoning_levels` 里有的值**，否则上游会 400。<br>非推理模型（chat 类）填 `none` |
| `model_catalog_json` | string | ✅ | 本地模型目录文件的**绝对路径**。见 [第五节](#五模型目录model_catalog_json)。<br>**不配这项是最常见的故障根因** |
| `cli_auth_credentials_store` | string | 方式 A 必填 | `keyring`（系统钥匙串，最安全）/ `file`（`~/.codex/auth.json`）/ `auto`（有钥匙串用钥匙串，否则用文件） |
| `disable_response_storage` | bool | 建议 `true` | 关闭服务端会话存储，每次发完整上下文而不依赖 `previous_response_id`。<br>第三方网关一般不实现 Responses 的服务端存储，不关会话会断。**【实测】0.154 接受这个参数，不报错** |
| `plan_mode_reasoning_effort` | string | 否 | 计划模式单独的推理强度 |
| `personality` | string | 否 | 输出风格，如 `pragmatic` |
| `notify` | array | 否 | 回合结束的通知回调，Desktop 自动写入，不用手动配 |

### 4.2 `[model_providers.X]` 段

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `name` | string | ✅ | 显示名，会出现在 Codex 的状态栏和 `/status` 里 |
| `base_url` | string | ✅ | **必须以 `/v1` 结尾，不带尾斜杠**。见 [1.2](#12-base-url-必须带-v1) |
| `wire_api` | string | ✅ | **只能是 `"responses"`**。`"chat"` 已于 2026-02 移除 |
| `requires_openai_auth` | bool | 方式 A | `true` 时用 `codex login --with-api-key` 存的凭据，**并忽略 `env_key`** |
| `env_key` | string | 方式 B | 读哪个环境变量当 Key。**Desktop 端不生效**，见 [方式 B](#方式-b不写死--env_key环境变量) |

### 4.3 `[model_providers.X.http_headers]` 段

| 参数 | 说明 |
|---|---|
| `Authorization` | `Bearer sk-xxxx`。方式 C 用 |
| ~~`Content-Type`~~ | ⚠️ **不要设**，Codex 自己会设，手工覆盖会破坏 multipart 上传 |

### 4.4 建议关闭的功能（接第三方网关时）

```toml
[features]
multi_agent = false     # 子 agent 会派生额外的模型调用
apps = false
plugins = false
```

官方插件（`browser` / `computer-use` / `documents` / `pdf` / `spreadsheets` / `presentations` / `visualize`）会往 `/v1/responses` 请求里塞 OpenAI 内置工具定义，上游不支持会直接 400。

建议**先全关跑通，再一个个开回来**定位。

---

## 五、模型目录（model_catalog_json）

### 5.1 为什么必须配

Codex 的模型元数据（上下文窗口、能力位、推理档位）来自「模型目录」，有三层，优先级从低到高：

| 层 | 来源 | 第三方 Key 能不能用 |
|---|---|---|
| 1. 内置目录 | 编译进 Codex 二进制 | 能读，但有门控（见下） |
| 2. 远端目录 | 官方账号拉取，缓存在 `~/.codex/models_cache.json` | ❌ 拉不到 |
| 3. **`model_catalog_json`** | 你本地的文件 | ✅ **覆盖上面两层** |

**不配第 3 层**，Codex 就用内置目录，而内置目录里有两道门控会卡住第三方 Key：

| 门控字段 | 内置目录里的值 | 后果 |
|---|---|---|
| `minimal_client_version` | `gpt-6-astra` 要 `0.153.0`（全表最高）<br>`gpt-5.6-*` 只要 `0.144.0` | 客户端版本不够 → gpt-6 被过滤 → **静默回落到 gpt-5.6** |
| `available_in_plans` | ChatGPT 套餐白名单 | 第三方 API Key 没有套餐 → 模型被过滤 |

我们生成的目录里这两项分别是 `"0.0.0"` 和**整个省略**，正好把两道门都拆掉。

### 5.2 生成目录

```bash
# 方式 A/C（Key 已存进钥匙串或写死在配置里）也需要给脚本单独传一次 Key，
# 因为脚本是直接调 Gravitex 的 /v1/models，不走 Codex
export GRAVITEX_API_KEY="sk-xxxx"

python3 scripts/codex/gen_codex_catalog.py \
    --base-url https://api.gravitex.ai/v1 \
    --output ~/.codex/gravitex-codex-catalog.json

# 可选：指定默认模型
python3 scripts/codex/gen_codex_catalog.py \
    --base-url https://api.gravitex.ai/v1 \
    --model gpt-6-astra
```

脚本会打印一段可直接粘贴的 config.toml 片段。

### 5.3 目录的硬约束（【实测】违反任意一条整份目录报废）

Codex 解析目录时做严格校验，**一个条目出错，整份目录失效，所有模型都不可用**：

| 约束 | 报错信息 |
|---|---|
| `supported_reasoning_levels` 必须是数组，**不能是 `null`** | `invalid type: null, expected a sequence at line N` |
| 每个条目必须有 `base_instructions` **或** `model_messages.instructions_template` | ``model `x` is missing both `base_instructions` and `model_messages.instructions_template` `` |
| `slug` 必须与实际发给上游的模型名一致 | 不报错，但会打到错的模型 |

### 5.4 什么模型会进目录

Gravitex 侧的准入规则（`service/codex_catalog.go`）：

| 条件 | 说明 |
|---|---|
| `io_schema.primary_mode` ∈ {`chat`, `reasoning`} | 图像 / 视频 / 向量 / 语音模型排除 |
| `io_schema.endpoint_types` 含 `openai-response` | Codex 只走 Responses API，不支持的放进去一选就 404 |
| 有有效的 `context_window`（≥ 8192） | 缺失就跳过，不猜值 |

模型没出现在列表里，按这三条去管理后台查该模型的扩展配置。

### 5.5 改完目录要做的事

```bash
# 1. 完全退出 Codex Desktop（Cmd+Q，不是关窗口），目录只在启动时读一次
# 2. 列表还是旧的就删缓存强制重建
rm -f ~/.codex/models_cache.json
# 3. 重开 Desktop
```

---

## 六、验证清单

按顺序做，每一步都能定位到具体是哪一层的问题。

### 步骤 1：确认路由通（与 Codex 无关）

```bash
curl -X POST https://api.gravitex.ai/v1/responses \
  -H "Authorization: Bearer $GRAVITEX_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model":"gpt-6-astra","input":"hi"}'
```

401 / 404 → Key 或 base_url 错了，**跟目录无关，别去改 catalog**。

### 步骤 2：`codex doctor` 全面体检

```bash
codex doctor
```

重点看这几段：

```
Configuration
  ✓ config       loaded
      model                    gpt-6-astra · gravitex     ← 模型和 provider 对不对
      config.toml parse        ok                          ← 配置语法有没有问题

  ✓ auth         auth is configured
      auth storage mode        Keyring                     ← 方式 A 应显示 Keyring/File
      stored auth mode         api_key

Connectivity
  ✓ reachability active provider endpoints are reachable over HTTP
      custom API route probe   https://api.gravitex.ai/v1/<redacted> route exists (HTTP 401)
```

> `route exists (HTTP 401)` 是**正常的** —— 探测请求没带 Key，401 恰好证明路由存在。

### 步骤 3：CLI 实跑

```bash
codex exec --skip-git-repo-check "只回复：OK"
```

看输出头部：

```
model: gpt-6-astra          ← 必须是你配的那个
provider: gravitex
reasoning effort: high
```

### 步骤 4：验证模型列表

```bash
codex        # 进交互界面后输入
/model       # 应列出目录里的全部模型
/status      # 看当前模型、provider、认证方式
```

> CLI 的 `/model` **不受** Desktop 那个已知过滤 bug 影响，所以它是判断「目录到底加载成功没有」的权威手段。

### 步骤 5：Desktop 验证

完全退出再重开，在模型选择器里确认能看到并选中目标模型。

---

## 七、故障排查

| 现象 | 原因 | 解决 |
|---|---|---|
| Desktop 报 `Missing environment variable` | 用了 `env_key`，GUI 读不到环境变量 | 改用方式 A，或 `launchctl setenv` 桥接 |
| 启动报 `failed to parse model_catalog_json` | 目录格式不合法 | 看报错行号，对照 [5.3](#53-目录的硬约束实测违反任意一条整份目录报废) |
| 每次请求 404 | `base_url` 少了 `/v1`，或 `wire_api` 不是 `responses` | 见 [1.2](#12-base-url-必须带-v1) |
| 每次请求 401 | Key 没存 / 存错 / 已过期 | `codex login status` 确认；重跑 `codex login --with-api-key` |
| 启动警告「provider 被忽略」 | provider 定义在项目级 `.codex/config.toml` | 挪到 `~/.codex/config.toml` |
| **配了 A 却打到 B** | 见 [8.1](#81-desktop-会回写-model) / [8.2](#82-review-走的是-review_model) / [8.3](#83-内置目录的门控) | 三条都查一遍 |
| CLI 能列模型，Desktop 列不出 | **Codex 官方 bug** [#19694](https://github.com/openai/codex/issues/19694)（至今 Open），Desktop 的 picker 有额外过滤 | 无解。用 `model = "xxx"` 内联写死绕过，picker 会显示「Custom」但请求正常 |
| 模型列表是旧的 | `models_cache.json` 没刷新 | `rm ~/.codex/models_cache.json` 后完全重启 |
| 请求 400、提到 tool / web_search | 官方插件往请求里塞了上游不支持的内置工具 | 关掉 `[features]` 里的 plugins / apps，或逐个禁用 `[plugins.*]` |

---

## 八、已知坑位速查

### 8.1 Desktop 会回写 `model`

**【实测】** Codex Desktop 的模型选择器会把选中的模型**写回 `config.toml` 的 `model` 字段**。

实测过程中，`model` 在几分钟内自己变了两次：

```
gpt-6-astra  →  gpt-5.6-sol  →  seed-1-8-251228
```

**这是「配了 gpt-6 却打到 gpt-5.6」最常见的原因。**

> ✅ 正确做法：改完 `config.toml` **还要在 Desktop 的 picker 里选中同一个模型**，否则下次启动又被覆盖。
> 排查时先 `cat ~/.codex/config.toml | head -5` 看 `model` 现在到底是什么。

### 8.2 review 走的是 `review_model`

**【文档】** `/review` 命令和 auto-review **不用** `model`，用 `review_model`。不配就回落到 Codex 内置默认模型。

### 8.3 内置目录的门控

没配 `model_catalog_json` 时，内置目录的 `minimal_client_version` 和 `available_in_plans` 会过滤掉模型，Codex 静默回落到门槛更低的模型。`gpt-6-astra` 门槛（`0.153.0`）恰好比 `gpt-5.6-*`（`0.144.0`）高一档，所以回落目标经常就是 5.6。

### 8.4 目录级的 `upgrade` 自动改模型

**【实测】** 官方内置目录里，退役模型会带 `upgrade` 字段做静默重定向。例如 `gpt-5.4`：

```json
"upgrade": {
  "model": "gpt-5.6-terra",
  "migration_markdown": "GPT-5.4 is no longer available\nCodex now uses GPT-5.6 Terra in place of GPT-5.4...",
  "retirement_at": "2026-08-31T19:00:00Z"
}
```

我们生成的目录里 `upgrade` 恒为 `null`，不受影响。

### 8.5 子 agent 会派生额外调用

Codex 的 multi-agent 机制（`spawn_agent` / `followup_task`）会让子 agent 各自发起模型调用。排查账单异常时要考虑这部分。建议接第三方网关时先 `features.multi_agent = false`。

### 8.6 Key 安全

- 方式 C 的 Key 明文落盘，**不要把 `config.toml` 提交进 git**
- 排查问题时如果把配置贴给别人，**记得先脱敏并事后换 Key**
- 方式 A 用 `keyring` 时 Key 在系统钥匙串里，不落文件

---

## 附：一份可直接改用户名就用的完整配置

```toml
# ~/.codex/config.toml

# ============ 模型 ============
# 注意：Desktop 的模型选择器会回写这一行，改完这里也要在 picker 里选同一个模型
model = "gpt-6-astra"
review_model = "gpt-6-astra"
model_reasoning_effort = "high"

# ============ Provider ============
model_provider = "gravitex"
cli_auth_credentials_store = "keyring"

# ============ 模型目录 ============
# ⚠️ 所有顶层键都必须写在 [model_providers.*] 这类 table 之前，
#    TOML 会把 table 之后的键算进那个 table 里
model_catalog_json = "/Users/你的用户名/.codex/gravitex-codex-catalog.json"

# ============ 第三方网关建议项 ============
disable_response_storage = true

[model_providers.gravitex]
name = "Gravitex"
base_url = "https://api.gravitex.ai/v1"   # 必须带 /v1，不带尾斜杠
wire_api = "responses"                     # 只能是 responses
requires_openai_auth = true                # Key 走 codex login 存储，不写进本文件

[features]
multi_agent = false
apps = false
plugins = false
```

配套命令：

```bash
# 1. 生成模型目录
export GRAVITEX_API_KEY="sk-xxxx"
python3 scripts/codex/gen_codex_catalog.py \
    --base-url https://api.gravitex.ai/v1 \
    --output ~/.codex/gravitex-codex-catalog.json

# 2. 存 Key
printf '%s' "$GRAVITEX_API_KEY" | codex login --with-api-key
unset GRAVITEX_API_KEY

# 3. 验证
codex doctor
codex exec --skip-git-repo-check "只回复：OK"

# 4. 完全退出 Desktop（Cmd+Q）再重开
```
