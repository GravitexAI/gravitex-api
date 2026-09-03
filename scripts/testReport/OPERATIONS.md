# 渠道测试报告 · Python 侧运维手册

面向**不熟悉 Python 的运维/后端同学**。全文假设你懂 Linux、systemd、Java 部署，
但不了解 Python 的 venv、pip、模块机制——这些会在用到的地方解释清楚。

当前测试环境：`101.47.154.214`（GRAVITEX-TEST，CentOS Stream 9，Python 3.9.25），
部署路径 `/workplace/py/testReport/`。

---

## 0. 先理解这套东西是什么（3 分钟）

### 0.1 两个目录，各干什么

```
/workplace/py/testReport/
├── claude-platform-test/     ← 测试脚本本体：真正去打 Claude API 跑 37 个用例、生成 Excel
└── claude-test-service/      ← 常驻 HTTP 服务：接 Java 的请求，起子进程去跑上面那个脚本
```

**它们必须是同级目录。** 服务是靠相对路径 `../claude-platform-test` 找脚本的
（`app.py` 里 `SCRIPT_DIR = BASE_DIR.parent / "claude-platform-test"`）。
挪动其中任何一个都会让服务找不到脚本。

### 0.2 调用链路

```
管理端弹窗（管理员手填 Token）
   ↓ HTTP
Java 后端  ChannelTestReportController        ← 无状态，只转发
   ↓ HTTP  127.0.0.1:8900
claude-test-service（本手册的运维对象）        ← 常驻，systemd 托管
   ↓ 起子进程，靠环境变量传参
claude-platform-test/run_tests.py             ← 跑完就退出
   ↓
reports/{task_id}.xlsx  →  原路回传给浏览器下载
```

**关键点**：真正跑测试的是**一次性子进程**，不是常驻服务。服务只负责"收请求、起进程、
读进度、回传文件"。所以你在 `ps` 里看到的常驻进程只有一个 uvicorn。

### 0.3 Java 侧不需要配任何东西

Token 由管理员在弹窗里填，**不走服务端配置**。Java 只需要两项配置（`application.yml`）：

```yaml
gravitex:
  channel-test:
    base-url: http://127.0.0.1:8900        # 本服务地址
    platform-base-url: https://api.gravitex.ai   # 平台地址（脚本实际打这里）
```

如果 Java 和本服务不同机，改 `base-url`，并看 §7.3。

---

## 1. Python 概念速查（只讲你会用到的）

| 概念 | 一句话解释 | 类比 |
|---|---|---|
| **venv（虚拟环境）** | 一个自包含的 Python 运行环境，装在项目目录下的 `.venv/` 里。依赖装在它内部，不污染系统 | 类似给每个 Java 服务单独带一份 JRE + 所有 jar |
| **pip** | Python 的包管理器 | Maven |
| **`requirements.txt`** | 依赖清单 | `pom.xml` 的 dependencies 部分 |
| **`.venv/bin/python`** | 这个 venv 专属的 Python 解释器 | 那份专属 JRE 里的 `java` |
| **`python -m xxx`** | 以模块方式运行，而不是当脚本文件 | `java -jar` vs `java -cp` |

### ⚠️ venv 有一个必须知道的坑

venv 里生成的**可执行脚本**（比如 `.venv/bin/uvicorn`、`.venv/bin/pip`）第一行是
**写死的绝对路径**：

```bash
$ head -1 /workplace/py/testReport/claude-test-service/.venv/bin/uvicorn
#!/workplace/py/testReport/claude-test-service/.venv/bin/python3
```

**后果**：venv 目录一旦被移动、改名，或者从别的机器复制过来，这些脚本立刻失效
（报 `No such file or directory`，即使文件明明在）。

**规则：`.venv` 永远不要跨机器传输、不要跟着目录改名，必须在目标机器上现建。**

> 这个坑在本项目上真实发生过两次：一次是本地目录改名后服务起不来，一次是首次部署
> 时误把 macOS 的 venv 传上了 Linux。

---

## 2. 在新服务器上从零部署

### 2.1 前置检查

```bash
ssh root@<新服务器>
python3 --version          # 需要 >= 3.9
python3 -m venv --help     # 确认 venv 模块可用；CentOS 上如果缺，装 python3-venv
systemctl --version        # 需要 systemd
ss -lntp | grep 8900       # 确认 8900 端口空闲
```

CentOS/RHEL 系如果 `venv` 缺失：`yum install -y python3-devel`
Debian/Ubuntu 系：`apt install -y python3-venv`

### 2.2 传代码

> ### 🚫 千万不要用 zip / Finder 压缩后上传
>
> **生产环境首次部署就是这么翻的车**，症状是建 venv 时报：
> ```
> Error: [Errno 2] No such file or directory: '/workplace/py/.../.venv/bin/python3'
> ```
>
> 原因：Finder 打的 zip 会把 macOS 的 `.venv` 一起打进去，而且带上一堆元数据。
> 实测那次的现场：
> - `.venv/.Python` 软链指向 `/Library/Developer/CommandLineTools/...`（macOS 路径，Linux 上是断链）
> - `.venv/bin/` 里**根本没有 `python3`**，只剩 activate 脚本
> - `pyvenv.cfg` 里 `home = /bin`（被打乱了）
> - 多出一个 `__MACOSX/` 目录 + **1490 个 `._*` 附属文件**
> - 目录 95MB，其中 94MB 是垃圾（代码本身只有 0.6MB）
>
> 然后 `python3 -m venv .venv` 在**已存在**的 `.venv` 上不会清空重建，它试图复用，
> 而里面没有可用的解释器 → 就报了上面那个错。
>
> **补救办法**（如果已经用 zip 传了，见 §6.8）。**正确做法用下面的 tar 管道。**

**在本地开发机的 `gravitex-api/scripts/testReport` 目录下执行**（不是在服务器上）：

```bash
ssh root@<新服务器> 'mkdir -p /workplace/py/testReport'

COPYFILE_DISABLE=1 tar \
    --exclude='*/.venv' --exclude='*/.venv/*' \
    --exclude='*/__pycache__' --exclude='*/__pycache__/*' \
    --exclude='*/.pytest_cache' --exclude='*/.pytest_cache/*' \
    --exclude='*/.idea' --exclude='*/.idea/*' \
    --exclude='*/reports/*' --exclude='*.xlsx' --exclude='.DS_Store' \
    -czf - claude-platform-test claude-test-service \
  | ssh root@<新服务器> 'tar -xzf - -C /workplace/py/testReport'
```

**传完必须自检**（两条都应输出 `0`）：

```bash
ssh root@<新服务器> 'find /workplace/py/testReport -name "._*" | wc -l'      # macOS 附属文件
ssh root@<新服务器> 'find /workplace/py/testReport -name ".venv" | wc -l'     # 此时还不该有 venv
```

<details>
<summary>为什么这条 tar 命令写得这么啰嗦（点开看，都是实测踩过的坑）</summary>

1. **`--exclude='*/.venv'` 不能写成 `--exclude='.venv'`**
   macOS 自带的是 bsdtar，它的 `--exclude` 拿**整条路径**做匹配，写 `.venv` 只能匹配
   根级同名项，子目录里的一个都排不掉。实测结果：本该传 0.5MB，实际传了 92MB，
   而且那些 macOS venv 在 Linux 上完全不可用。

2. **`COPYFILE_DISABLE=1` 必须加**
   不加的话 bsdtar 会为每个文件额外打包一个 `._xxx` 的 AppleDouble 附属文件
   （macOS 的扩展属性载体）。传上去 20 个，然后 pytest 会去收集 `._latency_test.py`
   并报 `TypeError: the 'package' argument is required`。

3. **`--exclude='*/reports/*'`**
   `reports/` 是运行时产物目录，里面是历史 Excel 报告，没必要传。

如果你是从 Linux 机器传（不是 macOS），`COPYFILE_DISABLE` 无害但也不需要，
GNU tar 的 `--exclude` 语义更宽松，写 `.venv` 也能生效。
</details>

### 2.3 建两个 venv（**必须在服务器上建**）

```bash
# 脚本的 venv
cd /workplace/py/testReport/claude-platform-test
python3 -m venv .venv
.venv/bin/pip install --upgrade pip
.venv/bin/pip install -r requirements.txt

# 服务的 venv
cd /workplace/py/testReport/claude-test-service
python3 -m venv .venv
.venv/bin/pip install --upgrade pip
.venv/bin/pip install -r requirements.txt
```

> **为什么是两个而不是一个**：脚本只需要 `requests` + `openpyxl`；服务额外需要
> `fastapi` + `uvicorn`。分开的好处是脚本进程的依赖面更小，且升级服务框架时
> 不会动到跑测试的那套环境。

验证：

```bash
/workplace/py/testReport/claude-platform-test/.venv/bin/python --version   # 应输出 3.9.x
/workplace/py/testReport/claude-test-service/.venv/bin/python --version
head -1 /workplace/py/testReport/claude-test-service/.venv/bin/uvicorn      # shebang 应指向本机路径
```

### 2.4 冒烟测试（不发任何网络请求、不花钱）

```bash
cd /workplace/py/testReport/claude-platform-test
.venv/bin/python run_tests.py --validate-config
# 预期输出：配置校验通过：1 个模型，37 个用例
```

这一步只校验配置正确性，**不会发起任何 API 调用**。它跑通说明：Python 版本 OK、
依赖装好了、脚本代码完整。

### 2.4b 跑一遍单元测试（可选但建议）

脚本的 `requirements.txt` 只含运行时依赖，跑测试要单独装 pytest：

```bash
cd /workplace/py/testReport/claude-test-service
.venv/bin/python -m pytest -q          # 预期 28 passed

cd /workplace/py/testReport/claude-platform-test
.venv/bin/pip install pytest
.venv/bin/python -m pytest -q          # 预期 88 passed
```

**通过数不对就说明代码版本不是最新的**，回到 §2.2 重传。
（实测踩过：生产首次部署的包缺了一个安全修复，服务端测试是 27 而不是 28。）

### 2.5 装 systemd 服务

服务单元文件在代码目录里，直接复制：

```bash
cp /workplace/py/testReport/claude-test-service/claude-test-service.service /etc/systemd/system/
systemctl daemon-reload
systemctl enable --now claude-test-service
```

**如果部署路径不是 `/workplace/py/testReport`**，先改单元文件里的两处路径：

```ini
WorkingDirectory=/你的路径/claude-test-service
ExecStart=/你的路径/claude-test-service/.venv/bin/uvicorn app:app --host 127.0.0.1 --port 8900
```

### 2.6 验收

```bash
# 1) 服务状态 + 开机自启
systemctl is-active  claude-test-service       # 预期 active
systemctl is-enabled claude-test-service       # 预期 enabled  ← 开机自启的标志

# 2) 接口
curl -s http://127.0.0.1:8900/health           # 预期 {"ok":true}
curl -s http://127.0.0.1:8900/meta             # 预期 5 个模型、12 个分类、37 用例

# 3) 状态码语义
curl -s -o /dev/null -w "%{http_code}\n" http://127.0.0.1:8900/tasks/fake           # 预期 404
curl -s -o /dev/null -w "%{http_code}\n" http://127.0.0.1:8900/tasks/fake/report    # 预期 409

# 4) 参数校验（这两个都不会真发起测试、不花钱）
curl -s -o /dev/null -w "%{http_code}\n" -X POST http://127.0.0.1:8900/run \
  -H 'Content-Type: application/json' \
  -d '{"platform_name":"t","base_url":"http://127.0.0.1:1","api_key":"k","report_base_url":"","models":["gpt-4o"],"categories":[]}'   # 预期 400（未知模型）

# 5) 安全：必须只监听回环
ss -lntp | grep 8900                           # 必须是 127.0.0.1:8900，不能是 0.0.0.0

# 6) 自动重启真的生效吗（kill 掉主进程，5 秒后应自己起来）
PID=$(systemctl show claude-test-service -p MainPID --value)
kill -9 $PID && sleep 8
systemctl is-active claude-test-service        # 预期仍是 active
curl -s http://127.0.0.1:8900/health           # 预期 {"ok":true}，且 PID 已变
```

最后确认 Java 侧 `gravitex.channel-test.base-url` 指向 `http://127.0.0.1:8900`，重启 Java。

最后确认 Java 侧 `gravitex.channel-test.base-url` 指向 `http://127.0.0.1:8900`，重启 Java。

---

## 3. 日常操作

### 3.1 启停

```bash
systemctl start   claude-test-service     # 启动
systemctl stop    claude-test-service     # 停止
systemctl restart claude-test-service     # 重启
systemctl status  claude-test-service     # 查状态
systemctl enable  claude-test-service     # 开机自启（部署时已设）
systemctl disable claude-test-service     # 取消开机自启
```

单元文件配了 `Restart=always`、`RestartSec=5`，进程崩了 5 秒后自动拉起。

### 3.2 看日志

服务没有自己的日志文件，全部走 systemd journal：

```bash
journalctl -u claude-test-service -f              # 实时跟踪（相当于 tail -f）
journalctl -u claude-test-service -n 200          # 最近 200 行
journalctl -u claude-test-service --since "1 hour ago"
journalctl -u claude-test-service -p err          # 只看错误
```

日志里会有：
- uvicorn 的访问日志（`"POST /run HTTP/1.1" 200 OK`）
- 报告清理记录（`已清理 N 个过期报告`）
- 清理失败的异常堆栈（`报告清理失败`）

> **注意**：管理员填的 Token **不会**出现在日志里。请求体不打日志，只有响应体在
> 出错时会记。这是刻意设计。

### 3.3 停服务时正在跑的测试会怎样

`systemctl stop` 会带走整个 cgroup，包括正在跑测试的子进程。表现是：
那次测试任务直接消失，管理端弹窗轮询会拿到 404 然后显示失败。**已经消耗的额度不会退**。

所以**重启服务前最好确认没有任务在跑**：

```bash
journalctl -u claude-test-service -n 50 | grep "POST /run"    # 看最近有没有人提交
```

（本服务刻意不存任务历史，所以没有"当前任务列表"这种查询接口。）

---

## 4. 更新测试脚本 / 升级

### 4.0 改了 `claude-platform-test` 之后怎么更新（最常见）

**两步：把新文件放到服务器上 → 重启服务。**

```bash
# 在服务器上（生产 101.47.158.158）
systemctl restart claude-test-service
curl -s http://127.0.0.1:8900/health        # 预期 {"ok":true}
```

文件怎么传都行（scp / rsync / SFTP 工具），只要覆盖到
`/workplace/py/testReport/claude-platform-test/` 下面就行。

**唯一硬要求：传完必须 `systemctl restart claude-test-service`。**

原因：跑测试的是每次新起的子进程（读磁盘最新代码），但 `/meta` 给管理端弹窗提供的
模型清单、分类清单、`cache_hit_rounds` 是**服务启动时读进内存的**。不重启会新旧各
一半——弹窗按旧配置估算请求次数，子进程按新配置实际跑，额度按实际烧。

实测：把 `CACHE_HIT_ROUNDS` 从 20 改成 99，不重启时 `/meta` 仍返回 20，重启后才是 99。

### 4.0b 用 deploy.sh 一键更新（可选）

**在你自己的 Mac 上跑**（不是在服务器上跑，它是 ssh 过去的）：

```bash
cd gravitex-api/scripts/testReport
./deploy.sh prod        # 生产
./deploy.sh test        # 测试
```

自动做：检查有没有测试在跑 → 传代码 → 清 macOS 垃圾 → 重启 → 验收。
有测试正在跑会中止，强制部署用 `FORCE=1 ./deploy.sh prod`。

### 4.0c 如果改了 `requirements.txt`

多一步装依赖：

```bash
cd /workplace/py/testReport/claude-platform-test
.venv/bin/pip install -r requirements.txt
systemctl restart claude-test-service
```

**永远不需要**：重建 venv、动 Java、动 nginx、重装 systemd。

### 4.1 依赖装坏了 / 想彻底重来

删掉 venv 重建即可，**不影响代码**：

```bash
cd /workplace/py/testReport/claude-test-service
rm -rf .venv
python3 -m venv .venv
.venv/bin/pip install --upgrade pip
.venv/bin/pip install -r requirements.txt
systemctl restart claude-test-service
```

### 4.2 迁移到新服务器

不要 `scp -r` 整个目录（会带上 venv，见 §1）。正确做法就是把 §2 从头走一遍。
`reports/` 里的历史报告本来就是 24 小时过期的临时产物，不需要迁移。

---

## 5. 常用配置项在哪改

改完都要 `systemctl restart claude-test-service`。

### 5.1 服务侧：`claude-test-service/app.py`

| 配置 | 行号附近 | 默认 | 说明 |
|---|---|---|---|
| `REPORT_TTL_SECONDS` | 42 | `24 * 3600` | 报告文件保留时长 |
| `CLEANUP_INTERVAL_SECONDS` | 43 | `3600` | 多久扫一次过期报告 |

### 5.2 服务侧：监听端口 / 地址

改 `/etc/systemd/system/claude-test-service.service` 的 `ExecStart` 里
`--host` / `--port`，然后：

```bash
systemctl daemon-reload && systemctl restart claude-test-service
```

改了端口记得同步 Java 的 `gravitex.channel-test.base-url`。

### 5.3 脚本侧：`claude-platform-test/config.py`

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

### 5.4 增删可测模型（已改造，通常不用改脚本）

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

## 6. 排障手册

### 6.1 管理端点「生成测试报告」报「测试服务不可达」

```bash
systemctl is-active claude-test-service      # 服务活着吗
curl -s http://127.0.0.1:8900/health         # 端口通吗
journalctl -u claude-test-service -n 50      # 看有没有启动失败的堆栈
```

如果 Java 和本服务不同机，还要查 Java 那边的 `base-url` 配置和网络可达性。

### 6.2 报「启动测试进程失败：[Errno 2] No such file or directory: '.../.venv/bin/python'」

**服务活着、`/health` 正常，但一提交任务就报这个** —— 说明
`claude-platform-test/.venv` 被从 macOS 传上来覆盖了，`bin/python` 变成指向
`/Library/Developer/...` 的断链。**这个坑已经发生三次**（见附 B）。

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

### 6.2b 服务起不来：`No such file or directory` 但文件明明在

**99% 是 venv 的 shebang 问题**（见 §1）。检查：

```bash
head -1 /workplace/py/testReport/claude-test-service/.venv/bin/uvicorn
```

如果输出的路径和当前实际路径不一致（比如带着 `/Users/...` 或旧目录名），
按 §4.3 重建 venv。

### 6.3 pytest 报 `TypeError: the 'package' argument is required`

传输时带上了 macOS 的 `._xxx` 附属文件。清理：

```bash
find /workplace/py/testReport -name "._*" -delete
find /workplace/py/testReport -name ".DS_Store" -delete
```

下次传输记得加 `COPYFILE_DISABLE=1`（见 §2.2）。

### 6.4 提交任务立刻返回「已有测试任务在执行中」

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

### 6.5 报告下载不了 / 报告文件堆积

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

### 6.6 测试跑完但用例全是 ERROR

先用管理端弹窗里的「测试」按钮验一下 Token 连通性。常见原因：
- Token 无效或已过期
- Token 不是**管理员**账号的（普通用户 token 无法指定渠道，会被 403）
- 目标渠道本身已禁用或上游挂了

### 6.7 磁盘占用排查

```bash
du -sh /workplace/py/testReport/*
du -sh /workplace/py/testReport/*/.venv          # venv 是大头，两个加起来约 84MB
du -sh /workplace/py/testReport/claude-test-service/reports/
```

代码本身只有 0.6MB，占空间的是 venv 和历史报告。如果总大小接近 100MB 而 venv 只占
一部分，大概率是 zip 传输带上了 macOS 垃圾，见 §6.8。

### 6.8 建 venv 报 `No such file or directory: '.../.venv/bin/python3'`

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

然后回到 **§2.3** 重建 venv。**同时建议用 §2.2 的 tar 管道把代码重传一遍** ——
zip 传上来的版本很可能不是最新的（生产那次就缺了一个安全修复）。

### 6.9 渠道列表打不开，报 `You need to enable JavaScript to run this app.`

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

### 6.10 Token 连通性测试一直报 `HTTP 401: Invalid token`

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

### 6.11 单元测试红了，但代码没动过

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

---

## 7. 安全边界（重要）

### 7.1 本服务必须只监听 127.0.0.1

`/run` 接口的入参里带**明文管理员 Token**，而本服务**没有任何鉴权层**——
鉴权完全由 Java 后端负责。暴露到公网等于把 Token 提交入口裸奔给所有人。

验证：

```bash
ss -lntp | grep 8900
# 必须是 127.0.0.1:8900，不能是 0.0.0.0:8900 或 *:8900
```

### 7.2 Token 不会落盘、不会进日志

- 管理端弹窗**不做任何持久化**（不存 localStorage/sessionStorage），每次重填
- Java 只把 Token 放进转发请求的 body，不写日志
- 本服务把它作为环境变量传给子进程，不写日志
- 子进程强制 `CLAUDE_TEST_PRINT_HTTP=0`，且脚本本身对 Authorization 头做了脱敏

### 7.3 如果 Java 和本服务不同机

改 uvicorn 的 `--host` 为内网网卡 IP，**并且必须加防火墙规则限制来源 IP**：

```bash
firewall-cmd --permanent --add-rich-rule='rule family="ipv4" source address="<Java机器IP>" port port="8900" protocol="tcp" accept'
firewall-cmd --permanent --add-port=8900/tcp --remove-port=8900/tcp   # 确保没有全网放行
firewall-cmd --reload
```

---

## 8. 一页速查

```bash
# 状态
systemctl status claude-test-service
curl -s http://127.0.0.1:8900/health

# 启停重启
systemctl {start|stop|restart} claude-test-service

# 日志
journalctl -u claude-test-service -f

# 更新代码（本地 scripts/testReport 下执行，然后重启）
COPYFILE_DISABLE=1 tar --exclude='*/.venv' --exclude='*/.venv/*' \
  --exclude='*/__pycache__' --exclude='*/__pycache__/*' \
  --exclude='*/.pytest_cache' --exclude='*/.pytest_cache/*' \
  --exclude='*/.idea' --exclude='*/.idea/*' \
  --exclude='*/reports/*' --exclude='*.xlsx' --exclude='.DS_Store' \
  -czf - claude-platform-test claude-test-service \
  | ssh root@<服务器> 'tar -xzf - -C /workplace/py/testReport'
ssh root@<服务器> 'systemctl restart claude-test-service'

# 重建 venv（依赖坏了时）
cd /workplace/py/testReport/claude-test-service && rm -rf .venv \
  && python3 -m venv .venv && .venv/bin/pip install -r requirements.txt \
  && systemctl restart claude-test-service

# 配置校验（不花钱）
cd /workplace/py/testReport/claude-platform-test && .venv/bin/python run_tests.py --validate-config

# 跑单元测试（脚本 venv 需先装 pytest）
cd /workplace/py/testReport/claude-test-service && .venv/bin/python -m pytest -q     # 28 passed
cd /workplace/py/testReport/claude-platform-test && .venv/bin/python -m pytest -q    # 87 passed
```

---

## 附 A：两套环境实测基线

| 项 | 测试环境 | 生产环境 |
|---|---|---|
| 服务器 | `101.47.154.214`（GRAVITEX-TEST） | `101.47.158.158` |
| 系统 | CentOS Stream 9 | CentOS Stream 9 |
| Python | 3.9.25（系统自带） | 3.9.25（系统自带） |
| 部署路径 | `/workplace/py/testReport/` | `/workplace/py/testReport/` |
| 监听 | `127.0.0.1:8900` | `127.0.0.1:8900` |
| 开机自启 | enabled | enabled |
| 单元测试 | 服务 28 / 脚本 88 | 服务 28 / 脚本 88 |
| Go 服务（平台） | 本机 3003（nginx 3000 对外） | 本机 3003 |
| `platform-base-url` | ⚠️ 需指向**本环境**平台，见 §6.10 | `https://api.gravitex.ai`（自洽） |
| nginx `/llm-api/` | ⚠️ 5666 端口块少结尾斜杠，见 §6.9 | ✅ `conf.d/system.conf` 正确 |
| `config.py` 的 `PLATFORM_NAME` | — | `xftoken` |
| `config.py` 的 `REPORT_BASE_URL` | — | `https://api.xftoken.ctclouds.com` |

共同基线：代码 0.6 MB（不含 venv）、venv 约 84 MB（两个合计）、服务常驻内存约 37 MB。

依赖版本（两边一致）：fastapi 0.128.8、uvicorn 0.39.0、pydantic 2.13.4、
openpyxl 3.1.5、requests 2.32.5。

## 附 B：这套东西踩过的坑（按时间倒序）

写在这里是为了让后来的人不用重踩。每条都是真实发生过、有现场记录的。

| # | 现象 | 根因 | 对策 |
|---|---|---|---|
| 1 | 生产建 venv 报 `No such file or directory: .venv/bin/python3` | 用 Finder zip 传代码，把 macOS 的 `.venv` 和 1490 个 `._*` 一起打包上传；`python3 -m venv` 在已存在的坏 venv 上不清空重建 | §6.8；改用 §2.2 的 tar 管道 |
| 2 | 生产服务端测试只有 27 passed（应为 28） | zip 传的是旧快照，缺了「409 文案不回显 task_id」这个越权修复 | 每次部署后按 §2.4b 核对测试通过数 |
| 3 | 两个单元测试无故变红 | 测试把 `config.py` 的业务默认值写死进断言，一次正常换平台就红 | 已改造成只校验机制不校验取值，见 §6.11 |
| 4 | 测试环境渠道列表打不开，`You need to enable JavaScript to run this app.` | nginx `proxy_pass` 少结尾斜杠，`/llm-api` 前缀没剥掉，Go 走 NoRoute 返回 SPA 首页 | §6.9 |
| 5 | Token 连通性测试恒 401 `Invalid token` | Java 的 `platform-base-url` 指向生产，token 在测试环境建的 | §6.10 |
| 6 | 本地目录改名后服务起不来 | venv 里 console script 的 shebang 是写死的绝对路径 | §1 的规则：venv 不跨机、不跟改名 |
| 7 | 服务端 pytest 报 `TypeError: the 'package' argument is required` | 传输带上了 macOS 的 `._latency_test.py`，被 pytest 当测试文件收集 | §6.3；tar 加 `COPYFILE_DISABLE=1` |
| 8 | 改了 `config.py` 但 `/meta` 还返回旧值 | `app.py` 启动时 import config，Python 模块进程级缓存 | §4.1；改完必须重启 |
| 9 | `deploy.sh` 的"有测试在跑"护栏恒定误报 | `pgrep -f` 匹配到了自己那条命令行 | 模式写成 `[r]un_tests`；`pgrep -c` 无匹配时既打印 0 又返回非零码，改用 `\| wc -l` |
| 10 | 提交任务报 `Errno 2 ... .venv/bin/python`（**第三次**同一根因） | 又一份 macOS `.venv` 被传上来覆盖，`bin/python` 指向 `/Library/Developer/...` 断链；同批还夹带 1474 个 `._*` | §6.2；已加 `/health` 主动检测 + `/run` 返 503 带修复命令 |

**七条里有五条都跟"传输方式"有关。** 所以 §2.2 那条 tar 命令看着啰嗦，每个参数都是
拿故障换来的，别图省事换成 `scp -r` 或 zip。
