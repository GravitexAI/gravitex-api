# 渠道测试报告 · 操作速查

> 按场景查，找到你要做的事直接抄命令。
> 原理、排障、配置项说明见同目录的 **OPERATIONS.md**（详细手册）。

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

**方式 A（推荐，自动排除垃圾）** —— 在**你的 Mac** 上、`gravitex-api/scripts/testReport` 目录下执行：

```bash
./deploy.sh prod        # 生产
./deploy.sh test        # 测试
./deploy.sh <IP>        # 其他机器
```

这条已经包含了传代码 + 重启 + 验收，**做完 1.3 就可以跳到 1.5 了**。

**方式 B（手动传）** —— 用你习惯的方式（scp / SFTP / zip）把这两个目录传到
`/workplace/py/testReport/` 下面：

```
/workplace/py/testReport/
├── claude-platform-test/     ← 测试脚本
└── claude-test-service/      ← 常驻服务
```

> **两个目录必须同级**，服务靠相对路径 `../claude-platform-test` 找脚本。
> `.venv` 带不带都行，下一步会自动处理。

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

✅ 全部就绪，可以去管理端用了
```

### 1.6 最后一步：Java 侧配置

确认 Java 的 `application.yml` 里：

```yaml
gravitex:
  channel-test:
    base-url: http://127.0.0.1:8900              # 本服务
    platform-base-url: https://api.gravitex.ai   # 平台地址
```

⚠️ **`platform-base-url` 必须和管理端渠道列表的来源是同一个平台**，否则会出现
"渠道 ID 是这个平台的、测试请求打到另一个平台"的错位。详见 OPERATIONS.md §6.10。

---

## 场景 2：日常更新测试脚本（改了 `claude-platform-test`）

这是最常见的操作。

### 方式 A：一键（推荐）

在**你的 Mac** 上、`gravitex-api/scripts/testReport` 目录下：

```bash
./deploy.sh prod
```

传代码 + 重启 + 验收全包了。它用 tar 管道传，**从设计上就排除 `.venv` 和 macOS 元数据**。

### 方式 B：自己传 + 一条命令

1. 用你习惯的方式把 `claude-platform-test` 传到 `/workplace/py/testReport/` 下
2. 在服务器上：

```bash
bash /workplace/py/testReport/after-upload.sh
```

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

`after-upload.sh` 和 `deploy.sh` 都会自动重启，所以用它们就不会踩。

### 改了 `requirements.txt` 怎么办

不用额外操作，`after-upload.sh` 每次都会跑 `pip install -r requirements.txt`。

### 加新模型呢

**通常不用改脚本**，往 `config.py` 的 `MODELS` 里加一行就行，`family` 和 `thinking`
会从模型名自动推导。只有接 `thinking=manual` 的老模型才需要写 `MODEL_OVERRIDES`
例外表。详见 OPERATIONS.md §5.4。

---

## 场景 3：改了 `claude-test-service`，或者服务挂了

### 3.1 改了服务代码（新增文件、改 `app.py` / `runner.py` 等）

和场景 2 完全一样：

```bash
# Mac 上
./deploy.sh prod

# 或者自己传完，服务器上
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

### ⚠️ 重启会杀掉正在跑的测试

`systemctl stop/restart` 会带走整个 cgroup，包括正在跑测试的子进程。表现是那次任务
直接消失，管理端轮询拿到失败。**已消耗的额度不退。**

重启前确认没人在跑：

```bash
pgrep -f '[r]un_tests\.py' | wc -l      # 输出 0 = 没有测试在跑
```

`deploy.sh` 会自动做这个检查并在有任务时中止（强制部署用 `FORCE=1 ./deploy.sh prod`）。

---

## 附：三个脚本分别是干嘛的

| 脚本 | 在哪跑 | 干什么 |
|---|---|---|
| `deploy.sh` | **你的 Mac** | 传代码 + 重启 + 验收，一条龙。传输自动排除 `.venv` 和 macOS 元数据 |
| `after-upload.sh` | **服务器** | 你自己传完代码后跑这条。修 venv、装依赖、装 systemd、重启、验收 |
| `run_tests.py --validate-config` | 服务器 | 只校验配置，**不发任何网络请求、不花钱**。想确认配置对不对就跑它 |

## 附：一定不要做的两件事

1. **不要把 Mac 上的 `.venv` 当成能用的东西传上去**
   venv 里全是写死的绝对路径，`bin/python` 会指向 `/Library/Developer/...`，
   在 Linux 上是断链。带上来也没关系（`after-upload.sh` 会重建），但别指望它能用。

2. **不要只传文件不重启**
   原因见场景 2 的那张表 —— 会出现弹窗和实际执行用不同配置，直接影响额度。
