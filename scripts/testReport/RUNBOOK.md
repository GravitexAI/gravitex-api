# 渠道测试报告 · 操作速查

> 按场景查，找到你要做的事直接抄命令。
> 配置项、排障、安全边界见文末附录。

## 先记住一件事

服务器上**只有一条命令**需要记：

```bash
bash /workplace/py/testReport/after-upload.sh
```

**三种场景用的都是它**。它是幂等的，随便跑多少次都安全，会自动判断要做什么：
清 macOS 垃圾 → 该建的 venv 建上 → 装依赖 → 装/更新 systemd 单元 → 配置校验 → 重启 → 验收。

当前两台机器：

| 环境 | 地址 | 部署路径 |
|---|---|---|
| 生产 | `101.47.158.158` | `/workplace/py/testReport/` |
| 测试 | `101.47.154.214` | `/workplace/py/testReport/` |

---

## 场景 1：服务器上什么都没有，从零部署

### 1.1 先检查环境（只读，不改任何东西）

```bash
ssh root@<服务器>
python3 --version          # 需要 >= 3.9
python3 -c "import venv"   # 不报错即可用
systemctl --version        # 需要 systemd
ss -lntp | grep 8900       # 应该没输出（端口空闲）
```

缺 venv 模块：CentOS/RHEL `yum install -y python3-devel`，Debian/Ubuntu `apt install -y python3-venv`。

### 1.2 建目录

```bash
ssh root@<服务器> 'mkdir -p /workplace/py/testReport'
```

### 1.3 传代码

用你习惯的方式（scp / SFTP / zip 解压）把这三个目录传到 `/workplace/py/testReport/` 下：

```
/workplace/py/testReport/
├── claude-platform-test/     ← 渠道测试脚本
├── claude-test-service/      ← 常驻服务（同时提供渠道测试和错误诊断接口）
└── error-diagnosis/          ← 错误账单诊断工具
```

> **三个目录必须同级**，服务靠相对路径 `../claude-platform-test`、`../error-diagnosis` 找脚本。
>
> **`.venv` 带不带都行** —— 带上来了下一步会自动重建（Mac 的 venv 在 Linux 上必坏），
> 不带更快（能省 20+MB 和上千个文件）。
>
> **`error-diagnosis` 不需要 venv**，它只用标准库，跑的时候直接借服务自己的解释器。
>
> ⚠️ **别把本地的 `error-diagnosis/.env` 覆盖上去** —— 里面是占位值，会把线上配好的
> AI 默认地址和模型冲掉（密钥不受影响，那个是管理员在弹窗里现填的）。脚本检测到
> 占位值会提醒你。
>
> `error-diagnosis/output/` 和 `claude-test-service/diagnosis/` 是服务端的运行产物目录，
> 本地那份不用传。

### 1.4 在服务器上执行

```bash
bash /workplace/py/testReport/after-upload.sh
```

它会建两个 venv、装依赖、装 systemd 单元、设开机自启、启动服务并验收。

### 1.5 确认成功

看到这个就成了：

```
    运行状态: active
    开机自启: enabled
    健康检查: {"ok":true}
    模型/分类: 6 模型 / 12 分类 / 37 用例 / 缓存轮数 20
    错误诊断: 可用（默认模型 glm-5.3-flash）

✅ 全部就绪，可以去管理端用了
```

> `错误诊断: 不可用 — ...` 不影响渠道测试，只是管理端账单日志页的「错误诊断」按钮用不了，
> 后面那句话会直接说明缺什么。

### 1.6 最后一步：Java 侧配置

确认 Java 的 `application.yml` 里：

```yaml
gravitex:
  channel-test:
    base-url: http://127.0.0.1:8900              # 本服务
    platform-base-url: https://api.gravitex.ai   # 平台地址
```

⚠️ **`platform-base-url` 必须和管理端渠道列表的来源是同一个平台**，否则会出现
"渠道 ID 是这个平台的、测试请求打到另一个平台"的错位。详见文末「排障」T10。

> **错误诊断不需要额外配置** —— 它复用上面这个 `base-url`（同一个服务进程的
> `/diagnosis/*` 接口），Java 侧没有新增配置项。

---

## 场景 2：日常更新测试脚本（改了 `claude-platform-test`）

这是最常见的操作，两步：

1. 把改好的 `claude-platform-test` 传到 `/workplace/py/testReport/` 下（覆盖原有的）
2. 在服务器上执行：

```bash
bash /workplace/py/testReport/after-upload.sh
```

完事。`.venv` 带上来了也没关系，脚本会自动重建。

### ⚠️ 为什么改完必须重启（不能只传文件）

跑测试的是**每次新起的子进程**，读磁盘上最新代码。但 `/meta`（给管理端弹窗提供
模型清单、分类清单、`cache_hit_rounds`）是**常驻服务进程启动时读进内存的**。

不重启不是"改动不生效"，而是**更糟的新旧各一半**。实测：

| 步骤 | `config.py` 里的值 | `/meta` 返回 |
|---|---|---|
| 改之前 | 20 | 20 |
| 改成 99，**不重启** | 99 | **20** ← 还是旧的 |
| 重启后 | 99 | 99 ✅ |

后果：弹窗按旧的 `20` 算"预计 385 次请求"，子进程按新的 `99` 实际跑，真发 **1985 次**。
**额度按实际请求烧。**

`after-upload.sh` 会自动重启，所以用它就不会踩。

### 改了 `requirements.txt` 怎么办

不用额外操作，`after-upload.sh` 每次都会跑 `pip install -r requirements.txt`。

### 加新模型呢

**通常不用改脚本**，往 `config.py` 的 `MODELS` 里加一行就行，`family` 和 `thinking`
会从模型名自动推导。只有接 `thinking=manual` 的老模型才需要写 `MODEL_OVERRIDES`
例外表。详见文末「配置项」C4。

---

## 场景 3：改了 `claude-test-service`，或者服务挂了

### 3.1 改了服务代码（新增文件、改 `app.py` / `runner.py` 等）

和场景 2 完全一样：传代码 → 跑这条。

```bash
bash /workplace/py/testReport/after-upload.sh
```

**新增了 .py 文件也不用特殊处理** —— 传上去跑这条就行，不需要改 systemd、不需要
重建 venv（除非新文件引入了新依赖，那就改 `requirements.txt`，脚本会自动装）。

**改了 `claude-test-service.service`（比如换端口）也不用手动装** —— 脚本会检测到
单元文件有变化并自动 `cp` + `daemon-reload`。换了端口记得同步改 Java 的 `base-url`。

### 3.2 服务挂了 / 起不来

**第一步永远是看健康检查**，它会把原因直接告诉你：

```bash
curl -s http://127.0.0.1:8900/health
```

| 返回 | 含义 | 怎么办 |
|---|---|---|
| `{"ok":true}` | 服务正常 | 问题在别处，看 3.3 |
| `{"ok":false,"script_python":"..."}` | 脚本 venv 坏了（多半是从 Mac 传上来的） | 跑 `after-upload.sh` 自动修 |
| 连不上 / 无返回 | 服务没在跑 | 看下面 |

服务没在跑时：

```bash
systemctl status claude-test-service --no-pager -l   # 看状态和最近日志
journalctl -u claude-test-service -n 100 --no-pager  # 看详细日志
systemctl restart claude-test-service                # 试着重启
```

**大部分情况直接跑这条就能修好**（它会重建坏掉的 venv、补装缺失的 systemd 单元）：

```bash
bash /workplace/py/testReport/after-upload.sh
```

### 3.3 常用运维命令

```bash
systemctl start   claude-test-service     # 启动
systemctl stop    claude-test-service     # 停止
systemctl restart claude-test-service     # 重启
systemctl status  claude-test-service     # 查状态
systemctl enable  claude-test-service     # 开机自启（部署时已设）
journalctl -u claude-test-service -f      # 实时看日志
```

服务配了 `Restart=always`、`RestartSec=5`，进程崩了 5 秒后自动拉起
（已实测：`kill -9` 后会自己起来）。

### ⚠️ 重启会杀掉正在跑的任务

`systemctl stop/restart` 会带走整个 cgroup，包括正在跑测试或诊断的子进程。表现是那次
任务直接消失，管理端轮询拿到失败。**渠道测试已消耗的额度不退。**

**`after-upload.sh` 会自己挡住这种情况**，检测到有任务在跑就直接退出并提示你等一等。
确认要强制继续（比如那个任务已经卡死了）：

```bash
FORCE=1 bash /workplace/py/testReport/after-upload.sh
```

想自己先看一眼在跑什么：

```bash
pgrep -af '[r]un_tests\.py|[e]rror_diagnosis\.py|[a]i_analysis\.py'   # 无输出 = 没任务在跑
```

> 手动 `systemctl restart` 没有这道保护，重启前自己确认。

---

## 附：两个脚本分别是干嘛的

| 脚本 | 在哪跑 | 干什么 |
|---|---|---|
| `after-upload.sh` | **服务器** | 传完代码后跑这条。挡住在跑的任务、清 macOS 垃圾、修/建 venv、装依赖、自检诊断工具、装 systemd、重启、验收。**幂等**，跑多少次都安全 |
| `run_tests.py --validate-config` | 服务器 | 只校验配置，**不发任何网络请求、不花钱**。想确认配置对不对就跑它 |
| `error_diagnosis.py --help` | 服务器 | 诊断工具能不能跑起来。`after-upload.sh` 已经帮你跑过了 |

## 附：一定不要做的两件事

1. **不要把 Mac 上的 `.venv` 当成能用的东西传上去**
   venv 里全是写死的绝对路径，`bin/python` 会指向 `/Library/Developer/...`，
   在 Linux 上是断链。带上来也没关系（`after-upload.sh` 会重建），但别指望它能用。

2. **不要只传文件不重启**
   原因见场景 2 的那张表 —— 会出现弹窗和实际执行用不同配置，直接影响额度。

---

## 附录 C：配置项在哪改

改完都要 `systemctl restart claude-test-service`。

### C1 服务侧：`claude-test-service/app.py`

| 配置 | 行号附近 | 默认 | 说明 |
|---|---|---|---|
| `REPORT_TTL_SECONDS` | 42 | `24 * 3600` | 报告文件保留时长 |
| `CLEANUP_INTERVAL_SECONDS` | 43 | `3600` | 多久扫一次过期报告 |

### C2 服务侧：监听端口 / 地址

改 `/etc/systemd/system/claude-test-service.service` 的 `ExecStart` 里
`--host` / `--port`，然后：

```bash
systemctl daemon-reload && systemctl restart claude-test-service
```

改了端口记得同步 Java 的 `gravitex.channel-test.base-url`。

### C3 脚本侧：`claude-platform-test/config.py`

这些是**跑测试的行为参数**，改了立刻对下一次测试生效（脚本是每次新起进程读配置）：

| 配置 | 行号 | 默认 | 说明 |
|---|---|---|---|
| `MAX_WORKERS` | 287 | `8` | `[模型 × 用例]` 的并发线程数。调大出报告快，但越容易撞上游限流(429)导致用例误判。建议 4~10 |
| `TIMEOUT_SECONDS` | 283 | `500` | 单次 HTTP 请求超时 |
| `REQUEST_INTERVAL_SECONDS` | 290 | `0.3` | 同线程内两个用例之间的间隔，给限流窗口留余量 |
| `CACHE_HIT_ROUNDS` | 274 | `20` | 缓存用例的读取轮数。**调小会让理论命中率上限下降**（20 轮上限 95.2%，5 轮只有 83.3%，低于 85% 阈值会让完美资源也不通过），不要随意调 |
| `CACHE_HIT_PASS_RATIO` | 275 | `0.85` | 缓存命中率的通过阈值 |

`PLATFORM_NAME` / `BASE_URL` / `API_KEY` / `MODELS` **不用改** —— 它们每次都由服务
通过环境变量注入（管理端弹窗填什么就用什么），`config.py` 里的字面量只是本地手工
跑脚本时的默认值。

### C4 增删可测模型（已改造，通常不用改脚本）

**加模型只需往 `config.py` 的 `MODELS` 列表里加一行**，或者干脆不改文件、用环境变量：

```bash
CLAUDE_TEST_MODELS="claude-opus-5,claude-sonnet-6" .venv/bin/python run_tests.py --validate-config
```

`family` 和 `thinking` 会**从模型名自动推导**：

| 模型名 | 推导出的 family | 推导出的 thinking |
|---|---|---|
| `claude-opus-5` | opus | adaptive |
| `claude-fable-5-1` | fable | adaptive |
| `claude-sonnet-6`（还没发布的） | sonnet | adaptive |
| `claude-haiku-4-5-20251001` | haiku | **manual**（例外表里写了） |

规则：**默认所有模型都按"新模型"处理**（`thinking=adaptive`、采样参数预期 4xx），
只有不符合这个形态的写进 `MODEL_OVERRIDES` 例外表。目前例外只有一个 haiku-4-5。

**什么时候才需要改脚本**：接一个 `thinking` 是 `manual` 的老模型时，往
`MODEL_OVERRIDES` 加一项：

```python
MODEL_OVERRIDES = {
    "claude-haiku-4-5-20251001": {
        "thinking": "manual",
        "sampling_parameters": "supported_without_thinking",
    },
    # 新的老模型往这里加
}
```

> **为什么 `thinking` 和 `sampling_parameters` 是两个独立字段、不互相推导**：
> 官方矩阵里 Opus 4.6 / Sonnet 4.6 那一代是 `thinking=adaptive` 但采样参数预期
> 2xx（`supported_without_thinking`），两个轴在那一代是独立的。写死
> "adaptive ⇒ expected_4xx" 将来接 4.6 系会把正常行为误判成缺陷。

改完用这条验证（不发任何网络请求）：

```bash
cd /workplace/py/testReport/claude-platform-test
.venv/bin/python run_tests.py --validate-config
```

**管理端弹窗的模型选项不受这里影响** —— 它直接读该渠道 `models` 字段里配置的模型
并默认全选，也支持手打任意模型名（tags 模式）。后端不再做模型白名单校验。

---

## 附录 T：排障手册

### T1 管理端点「生成测试报告」报「测试服务不可达」

```bash
systemctl is-active claude-test-service      # 服务活着吗
curl -s http://127.0.0.1:8900/health         # 端口通吗
journalctl -u claude-test-service -n 50      # 看有没有启动失败的堆栈
```

如果 Java 和本服务不同机，还要查 Java 那边的 `base-url` 配置和网络可达性。

### T2 报「启动测试进程失败：[Errno 2] No such file or directory: '.../.venv/bin/python'」

**服务活着、`/health` 正常，但一提交任务就报这个** —— 说明
`claude-platform-test/.venv` 被从 macOS 传上来覆盖了，`bin/python` 变成指向
`/Library/Developer/...` 的断链。**这个坑已经发生三次**（见「踩过的坑」）。

一条命令确认：

```bash
ls -l /workplace/py/testReport/claude-platform-test/.venv/bin/python
# 指向 /Library/... 或 /Users/... = 中招
```

修复（只重建 venv，代码不动）：

```bash
cd /workplace/py/testReport/claude-platform-test
python3 -m venv --clear .venv
.venv/bin/pip install -r requirements.txt
systemctl restart claude-test-service
```

顺手清掉一起被传上来的 macOS 垃圾（通常上千个）：

```bash
cd /workplace/py/testReport
rm -rf __MACOSX && find . -name '._*' -delete && find . -name '.DS_Store' -delete
```

**现在服务会主动提示这个问题**，不用再靠猜：
- `GET /health` 在 venv 断链时返回 `{"ok": false, "script_python": "..."}`，文案里
  直接点明"这是 macOS 路径，说明 .venv 是从 Mac 传上来的"，并给出重建命令
- `POST /run` 返回 **503**（不是 500）+ 同样的可照做提示

**所以排查时先 `curl -s http://127.0.0.1:8900/health`** —— 它会把原因和修复命令一起告诉你。

### T2b 服务起不来：`No such file or directory` 但文件明明在

**99% 是 venv 的 shebang 问题**（见「一定不要做的两件事」）。检查：

```bash
head -1 /workplace/py/testReport/claude-test-service/.venv/bin/uvicorn
```

如果输出的路径和当前实际路径不一致（比如带着 `/Users/...` 或旧目录名），
跑 `after-upload.sh` 自动重建 venv。

### T3 pytest 报 `TypeError: the 'package' argument is required`

传输时带上了 macOS 的 `._xxx` 附属文件。清理：

```bash
find /workplace/py/testReport -name "._*" -delete
find /workplace/py/testReport -name ".DS_Store" -delete
```

下次传输记得加 `COPYFILE_DISABLE=1`（见「场景 1」的传代码步骤）。

### T4 提交任务立刻返回「已有测试任务在执行中」

这是**设计行为**：服务同一时刻只跑一个任务（单槽）。

> **为什么限 1**：测试脚本内部已经是 8 线程并发，再叠加多任务必然打爆上游限流，
> 429 会被用例误判成"能力缺失"，报告就没有参考价值了。

等前一个跑完即可。想知道还要多久，看日志里的进度：

```bash
journalctl -u claude-test-service -f | grep PROGRESS
```

如果确认是卡死了（比如子进程僵掉），重启服务会强制清掉：

```bash
systemctl restart claude-test-service
```

### T5 报告下载不了 / 报告文件堆积

报告在 `claude-test-service/reports/`，24 小时自动清理（服务内的后台线程）。

```bash
ls -lh /workplace/py/testReport/claude-test-service/reports/
du -sh /workplace/py/testReport/claude-test-service/reports/
```

清理逻辑有两层：内存里的任务记录 + 按文件 mtime 扫盘（后者是为了兜住服务重启前
遗留的孤儿文件）。要手工清空：

```bash
find /workplace/py/testReport/claude-test-service/reports/ -name "*.xlsx" -mtime +1 -delete
```

### T6 测试跑完但用例全是 ERROR

先用管理端弹窗里的「测试」按钮验一下 Token 连通性。常见原因：
- Token 无效或已过期
- Token 不是**管理员**账号的（普通用户 token 无法指定渠道，会被 403）
- 目标渠道本身已禁用或上游挂了

### T7 磁盘占用排查

```bash
du -sh /workplace/py/testReport/*
du -sh /workplace/py/testReport/*/.venv          # venv 是大头，两个加起来约 84MB
du -sh /workplace/py/testReport/claude-test-service/reports/
```

代码本身只有 0.6MB，占空间的是 venv 和历史报告。如果总大小接近 100MB 而 venv 只占
一部分，大概率是 zip 传输带上了 macOS 垃圾，见 T8。

### T8 建 venv 报 `No such file or directory: '.../.venv/bin/python3'`

**这是用 zip / Finder 传代码导致的**，生产首次部署实际发生过。先确认现场：

```bash
cd /workplace/py/testReport
ls -la claude-platform-test/.venv/ | grep Python      # 若有指向 /Library/... 的软链 = 中招
find . -name "._*" | wc -l                            # 若是个大数（实测 1490）= 中招
ls -d __MACOSX 2>/dev/null                            # 若存在 = Finder zip
du -sh .                                              # 若接近 100M（正常 0.6M）= 中招
```

**补救（代码文件不会动，只清垃圾）：**

```bash
cd /workplace/py/testReport

# 1. 清掉 macOS 残留和坏 venv
rm -rf __MACOSX
rm -rf claude-platform-test/.venv claude-test-service/.venv
find . -name "._*" -delete
find . -name ".DS_Store" -delete
find . -name "__pycache__" -type d -exec rm -rf {} + 2>/dev/null
find . -name ".pytest_cache" -type d -exec rm -rf {} + 2>/dev/null
rm -rf claude-platform-test/.idea

# 2. 确认清干净了（三条都应为 0）
find . -name "._*" | wc -l
find . -name ".venv" | wc -l
find . -name "__MACOSX" | wc -l

# 3. 代码文件应该还在
ls claude-platform-test/*.py claude-test-service/*.py
```

然后跑 `after-upload.sh` 自动重建。**同时建议把代码重新传一遍** ——
zip 传上来的版本很可能不是最新的（生产那次就缺了一个安全修复）。

### T9 渠道列表打不开，报 `You need to enable JavaScript to run this app.`

**这不是本服务的问题**，是 nginx 的 `/llm-api/` 反代配置。

该文案是 new-api（Go 后端）内嵌的 React SPA 首页兜底内容。意思是：一个期望拿 JSON
的请求拿回了 HTML —— 因为 `/llm-api` 前缀没被剥掉，Go 匹配不到 API 路由，走了
`NoRoute` 兜底返回 SPA 首页。

**根因**：nginx 的 `proxy_pass` 少了结尾斜杠。

```nginx
proxy_pass http://127.0.0.1:3003/;   # ✅ 带斜杠 → 剥掉 location 前缀
proxy_pass http://127.0.0.1:3003;    # ❌ 不带  → 原样透传 /llm-api/... 给 Go
```

**定位方法**（在 nginx 所在机器上跑，四组对照）：

```bash
# A. 直接打 Go，路径正确 → 应返回 JSON（401 也算正常，说明路由通了）
curl -s http://127.0.0.1:3003/api/channel/ | head -c 100
# B. 直接打 Go，带 /llm-api 前缀 → 应返回 HTML（复现故障）
curl -s http://127.0.0.1:3003/llm-api/api/channel/ | head -c 100
# C. 走 nginx（用户实际路径）→ 返回 HTML 就是中招了
curl -s http://127.0.0.1:<管理端端口>/llm-api/api/channel/ | head -c 100
```

**查配置**（注意可能在 `conf.d/` 里而不是主配置文件）：

```bash
nginx -T | grep -A3 "location.*llm-api" | grep -E "location|proxy_pass"
```

改完 `nginx -t && systemctl reload nginx`（reload 不断连）。

### T10 Token 连通性测试一直报 `HTTP 401: Invalid token`

先确认 **Token 本身**：必须是 **new-api 控制台**里用**管理员账号**建的令牌
（`sk-xxxxxxxx`）。普通用户的令牌带渠道后缀会被 **403** 拒掉，报
`specified channels are not supported for regular users`。

若 Token 确定没问题，那就是**打错了平台**：

```bash
# Java 配的 platform-base-url 指向哪个平台？
# 那个平台的 new-api 数据库里有没有这个 token？
getent hosts <platform-base-url 的域名>     # 看解析到哪台机
```

**典型错误**：测试环境的 Java 把 `platform-base-url` 配成了生产的
`https://api.gravitex.ai`，而 Token 是在测试环境建的 → 生产不认识这个 token。

**更隐蔽的连带问题**：渠道列表是从**本环境**的 Go 后端读的，但测试请求打到**另一个**
平台。就算 token 对了，页面上看到的渠道 ID 是本环境的编号，拿到另一个平台去指定，
要么不存在、要么指到完全不相干的渠道，还会烧那个平台的额度。

**规则：渠道列表的来源平台，必须和 `platform-base-url` 是同一个平台。**

不重新构建 jar 的覆盖方式：

```bash
java -jar ruoyi-admin.jar -Dgravitex.channel-test.platform-base-url=http://127.0.0.1:3000
```

### T11 单元测试红了，但代码没动过

先看是不是这两个：

```
tests/test_env_override.py::test_business_constants_are_wired_through_env_layer
tests/test_run_tests_cli.py::test_progress_json_emits_start_event_on_validate
```

这两个测试**已经改造成对 `config.py` 的配置改动免疫**了（不再断言 `PLATFORM_NAME`
的具体值、不再写死模型数量）。如果它们还是红的，那是真的坏了，不是配置问题。

> 背景：最早的版本把 `PLATFORM_NAME == "baoyun_f5"`、`total == 5` 这类字面值写进了
> 断言。`config.py` 的设计初衷是"换平台时只改这里"，结果一次正常的换平台就让测试
> 变红了。现在改成只校验**机制**（环境变量覆盖是否生效、回落是否正确）和**类型**，
> 不校验业务取值。

### T12 管理端「错误诊断」按钮点了没反应 / 报服务不可用

先看服务怎么说，它会直接给出原因：

```bash
curl -s http://127.0.0.1:8900/diagnosis/meta
```

| 返回 | 含义 | 怎么办 |
|---|---|---|
| `"available":true` | 服务这边正常 | 问题在 Java 或前端，看下一条 |
| `"unavailable_reason":"诊断脚本不存在：..."` | `error-diagnosis` 目录没传上来 | 传上去，跑 `after-upload.sh` |
| `"unavailable_reason":"诊断网页模板缺失：..."` | 传漏了 `diagnostics/dashboard.html` | 整个目录重传一次 |
| 404 | 服务是旧版本，还没有这组接口 | 传新的 `claude-test-service`，跑 `after-upload.sh` |

`available:true` 但管理端还是不行，多半是 Java 侧：确认 `gravitex.channel-test.base-url`
指向 `http://127.0.0.1:8900`（错误诊断复用同一个配置），以及 Java 已经更新到含
`ErrorDiagnosisController` 的版本。

### T13 诊断提交后立刻报「超过单次诊断上限」

Java 侧有 50 万行的硬上限（`ErrorDiagnosisService.MAX_ROWS`）。缩小时间范围，或者加
用户名/模型/渠道条件。弹窗里的「预检行数」可以提前看到命中多少条，不用等提交才知道。

> 为什么要有这个上限：35 万行产出的 HTML 已经 3 MB 左右，再往上网页本身就大到打不开了。
> 与其让人等十几分钟拿到一个卡死的看板，不如在提交前挡住。

### T14 诊断跑完了，但看板里一条数据都没有

CSV 列头对不上。Java 端 `ErrorDiagnosisService.CSV_HEADER` 写的八个列名，必须能被
`error-diagnosis/diagnostics/ingest.py` 的 `ALIASES` 认出来；对不上时诊断会正常跑完、
正常出网页，**只是什么都没统计到，不报错**。

两边各有一个测试钉住这个契约，改了任意一端都要同步：

```bash
# 服务端
cd /workplace/py/testReport/claude-test-service
.venv/bin/python -m pytest tests/test_diagnosis.py -k csv_header -q
```

### T15 看板出来了，但没有 AI 分析报告

任务状态里的 `ai_error` 会说明原因（管理端也会弹一条黄色提示）。**这是刻意设计的**：
AI 接口挂了不影响本地统计，看板照样可用，不会因为 AI 失败就把整个任务判失败。

常见原因：密钥填错、模型名不存在、接口地址不通。看板本身不需要 AI，重跑时不勾
「生成 AI 分析报告」就行。

---

---

## 附录 S：安全边界（重要）

### S1 本服务必须只监听 127.0.0.1

`/run` 接口的入参里带**明文管理员 Token**，而本服务**没有任何鉴权层**——
鉴权完全由 Java 后端负责。暴露到公网等于把 Token 提交入口裸奔给所有人。

验证：

```bash
ss -lntp | grep 8900
# 必须是 127.0.0.1:8900，不能是 0.0.0.0:8900 或 *:8900
```

### S2 Token 不会落盘、不会进日志

- 管理端弹窗**不做任何持久化**（不存 localStorage/sessionStorage），每次重填
- Java 只把 Token 放进转发请求的 body，不写日志
- 本服务把它作为环境变量传给子进程，不写日志
- 子进程强制 `CLAUDE_TEST_PRINT_HTTP=0`，且脚本本身对 Authorization 头做了脱敏

### S3 如果 Java 和本服务不同机

改 uvicorn 的 `--host` 为内网网卡 IP，**并且必须加防火墙规则限制来源 IP**：

```bash
firewall-cmd --permanent --add-rich-rule='rule family="ipv4" source address="<Java机器IP>" port port="8900" protocol="tcp" accept'
firewall-cmd --permanent --add-port=8900/tcp --remove-port=8900/tcp   # 确保没有全网放行
firewall-cmd --reload
```

---

---

## 附录 P：这套东西踩过的坑

写在这里是为了让后来的人不用重踩。每条都是真实发生过、有现场记录的。

| # | 现象 | 根因 | 对策 |
|---|---|---|---|
| 1 | 生产建 venv 报 `No such file or directory: .venv/bin/python3` | 用 Finder zip 传代码，把 macOS 的 `.venv` 和 1490 个 `._*` 一起打包上传；`python3 -m venv` 在已存在的坏 venv 上不清空重建 | T8；传输时别带 `.venv` |
| 2 | 生产服务端测试只有 27 passed（应为 28） | zip 传的是旧快照，缺了「409 文案不回显 task_id」这个越权修复 | 每次部署后核对测试通过数 |
| 3 | 两个单元测试无故变红 | 测试把 `config.py` 的业务默认值写死进断言，一次正常换平台就红 | 已改造成只校验机制不校验取值，见 T11 |
| 4 | 测试环境渠道列表打不开，`You need to enable JavaScript to run this app.` | nginx `proxy_pass` 少结尾斜杠，`/llm-api` 前缀没剥掉，Go 走 NoRoute 返回 SPA 首页 | T9 |
| 5 | Token 连通性测试恒 401 `Invalid token` | Java 的 `platform-base-url` 指向生产，token 在测试环境建的 | T10 |
| 6 | 本地目录改名后服务起不来 | venv 里 console script 的 shebang 是写死的绝对路径 | 见「一定不要做的两件事」第 1 条 |
| 7 | 服务端 pytest 报 `TypeError: the 'package' argument is required` | 传输带上了 macOS 的 `._latency_test.py`，被 pytest 当测试文件收集 | T3；传输时别带 macOS 元数据 |
| 8 | 改了 `config.py` 但 `/meta` 还返回旧值 | `app.py` 启动时 import config，Python 模块进程级缓存 | 「场景 2」；改完必须重启 |
| 9 | `after-upload.sh` 的"有测试在跑"护栏恒定误报 | `pgrep -f` 匹配到了自己那条命令行 | 模式写成 `[r]un_tests`；`pgrep -c` 无匹配时既打印 0 又返回非零码，改用 `\| wc -l` |
| 10 | 提交任务报 `Errno 2 ... .venv/bin/python`（**第三次**同一根因） | 又一份 macOS `.venv` 被传上来覆盖，`bin/python` 指向 `/Library/Developer/...` 断链；同批还夹带 1474 个 `._*` | T2；已加 `/health` 主动检测 + `/run` 返 503 带修复命令 |

**七条里有五条都跟"传输方式"有关。** 所以传代码时**千万别带 `.venv`、别带 macOS 元数据** ——
这两条都是拿故障换来的。
