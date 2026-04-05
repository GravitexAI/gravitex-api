# pprof 性能分析使用指南

## 概述

本项目内置了 Go 标准库 `pprof` 性能分析功能，包含两部分：

1. **HTTP pprof 服务** — 通过 HTTP 端点实时采集各类 profile 数据
2. **CPU 自动监控** — 当 CPU 使用率超过 80% 时自动生成 CPU profile 文件

此外，项目还集成了 **Pyroscope** 持续性能分析（可选），详见本文末尾。

---

## 1. 启用 pprof

在 `.env` 文件或环境变量中设置：

```bash
ENABLE_PPROF=true
```

启动应用后会看到日志输出：

```
pprof enabled
```

pprof 功能默认**关闭**，仅在显式设置 `ENABLE_PPROF=true` 时启用。

---

## 2. HTTP pprof 端点

### 访问地址

pprof HTTP 服务运行在**独立端口 `8005`**上（与主应用端口分离），监听地址为 `0.0.0.0:8005`。

### 可用端点

| 端点 | 说明 |
|---|---|
| `http://<host>:8005/debug/pprof/` | pprof 索引页，查看所有可用 profile |
| `http://<host>:8005/debug/pprof/profile` | CPU profile（默认采集 30 秒） |
| `http://<host>:8005/debug/pprof/heap` | 堆内存分配信息 |
| `http://<host>:8005/debug/pprof/goroutine` | 当前所有 goroutine 的堆栈信息 |
| `http://<host>:8005/debug/pprof/allocs` | 内存分配采样 |
| `http://<host>:8005/debug/pprof/block` | 阻塞操作的堆栈信息 |
| `http://<host>:8005/debug/pprof/mutex` | 互斥锁竞争的堆栈信息 |
| `http://<host>:8005/debug/pprof/threadcreate` | 创建新线程的堆栈信息 |
| `http://<host>:8005/debug/pprof/cmdline` | 程序启动命令行参数 |
| `http://<host>:8005/debug/pprof/symbol` | 符号表查询 |
| `http://<host>:8005/debug/pprof/trace` | 执行追踪（trace） |

> **安全提示**：pprof 端口 `8005` **没有任何认证保护**。生产环境中请务必通过防火墙、安全组或网络策略限制对该端口的访问，仅允许运维人员访问。

---

## 3. 使用 `go tool pprof` 分析

### 3.1 交互式分析（实时连接）

```bash
# CPU profile（采集 30 秒）
go tool pprof http://<host>:8005/debug/pprof/profile

# 堆内存
go tool pprof http://<host>:8005/debug/pprof/heap

# goroutine
go tool pprof http://<host>:8005/debug/pprof/goroutine

# 指定 CPU 采集时长（例如 60 秒）
go tool pprof http://<host>:8005/debug/pprof/profile?seconds=60
```

进入交互式命令行后常用命令：

```
(pprof) top          # 查看占用最高的函数
(pprof) top 20       # 查看 Top 20
(pprof) list <func>  # 查看指定函数的逐行分析
(pprof) web          # 在浏览器中查看调用图（需安装 graphviz）
(pprof) png          # 生成 PNG 调用图
(pprof) quit         # 退出
```

### 3.2 先下载后分析

```bash
# 下载 CPU profile 到本地文件
curl -o cpu.pprof http://<host>:8005/debug/pprof/profile?seconds=30

# 下载堆内存 profile
curl -o heap.pprof http://<host>:8005/debug/pprof/heap

# 本地分析
go tool pprof cpu.pprof
```

### 3.3 Web UI 可视化（推荐）

```bash
# 启动 Web UI（自动打开浏览器）
go tool pprof -http=:8080 http://<host>:8005/debug/pprof/heap

# 对本地文件启动 Web UI
go tool pprof -http=:8080 cpu.pprof
```

Web UI 提供火焰图（Flame Graph）、调用图（Graph）、Top 列表等可视化视图，比命令行更直观。

### 3.4 对比分析

```bash
# 先后采集两次 heap profile
curl -o heap1.pprof http://<host>:8005/debug/pprof/heap
# ... 执行一些操作 ...
curl -o heap2.pprof http://<host>:8005/debug/pprof/heap

# 对比差异
go tool pprof -base heap1.pprof heap2.pprof
```

---

## 4. CPU 自动监控（文件输出）

启用 pprof 后，会同时启动 CPU 使用率监控协程（`common.Monitor()`），行为如下：

| 参数 | 值 |
|---|---|
| 检测间隔 | 每 30 秒 |
| 触发阈值 | CPU 使用率 > 80% |
| 采集时长 | 10 秒 |
| 输出目录 | `./pprof/` （应用工作目录下） |
| 文件命名 | `cpu-YYYYMMDDHHmmss.pprof` |

当 CPU 使用率持续高于 80% 时，系统会自动在 `./pprof/` 目录下生成 profile 文件。

### 分析自动生成的文件

```bash
# 交互式分析
go tool pprof ./pprof/cpu-20260327143022.pprof

# Web UI
go tool pprof -http=:8080 ./pprof/cpu-20260327143022.pprof
```

> **注意**：自动生成的文件不会自动清理，长时间运行后需要手动清理 `./pprof/` 目录，避免磁盘空间被占满。

---

## 5. 常见排查场景

### 5.1 CPU 占用过高

```bash
# 采集 60 秒 CPU profile
go tool pprof -http=:8080 'http://<host>:8005/debug/pprof/profile?seconds=60'
```

在 Web UI 中切换到 Flame Graph 视图，查看哪些函数占用 CPU 最多。

### 5.2 内存泄漏

```bash
# 查看当前在用的内存对象
go tool pprof -http=:8080 http://<host>:8005/debug/pprof/heap

# 查看累计分配（找出分配最频繁的位置）
go tool pprof -http=:8080 -alloc_space http://<host>:8005/debug/pprof/allocs
```

### 5.3 goroutine 泄漏

```bash
# 查看当前所有 goroutine
go tool pprof http://<host>:8005/debug/pprof/goroutine

# 或者直接在浏览器中查看文本格式
# 访问 http://<host>:8005/debug/pprof/goroutine?debug=1  （简要）
# 访问 http://<host>:8005/debug/pprof/goroutine?debug=2  （完整堆栈）
```

### 5.4 锁竞争

```bash
go tool pprof -http=:8080 http://<host>:8005/debug/pprof/mutex
```

### 5.5 执行追踪（Trace）

```bash
# 采集 5 秒的执行 trace
curl -o trace.out http://<host>:8005/debug/pprof/trace?seconds=5

# 使用 go tool trace 分析
go tool trace trace.out
```

---

## 6. Pyroscope 持续性能分析（可选）

项目同时集成了 [Pyroscope](https://pyroscope.io/) 持续性能分析，与 pprof 独立配置。

### 环境变量

| 环境变量 | 说明 | 默认值 |
|---|---|---|
| `PYROSCOPE_URL` | Pyroscope 服务端地址（为空则不启用） | `""` |
| `PYROSCOPE_APP_NAME` | 应用名称标识 | `new-api` |
| `PYROSCOPE_BASIC_AUTH_USER` | Basic Auth 用户名 | `""` |
| `PYROSCOPE_BASIC_AUTH_PASSWORD` | Basic Auth 密码 | `""` |
| `HOSTNAME` | 主机名标签 | `new-api` |
| `PYROSCOPE_MUTEX_RATE` | Mutex profile 采样率 | `5` |
| `PYROSCOPE_BLOCK_RATE` | Block profile 采样率 | `5` |

### 采集的 Profile 类型

- CPU
- Alloc Objects / Alloc Space
- Inuse Objects / Inuse Space
- Goroutines
- Mutex Count / Mutex Duration
- Block Count / Block Duration

### 启用示例

```bash
PYROSCOPE_URL=http://pyroscope-server:4040
PYROSCOPE_APP_NAME=gravitex-api
```

Pyroscope 适合生产环境的持续性能监控，可以查看历史性能数据和趋势。

---

## 7. 安全注意事项

1. **pprof 端口（8005）无认证**，切勿在公网直接暴露。使用以下方式限制访问：
   - 防火墙规则 / 安全组仅允许内网 IP
   - 通过 SSH 隧道访问：`ssh -L 8005:localhost:8005 user@server`
   - 使用 VPN
2. **生产环境慎用**：pprof 的 CPU profile 采集会带来一定性能开销，建议仅在排查问题时临时启用。
3. **磁盘空间**：自动 CPU 监控会持续生成文件，注意定期清理 `./pprof/` 目录。

---

## 8. 快速参考

```bash
# 启用 pprof
export ENABLE_PPROF=true

# 浏览器查看 pprof 索引
open http://localhost:8005/debug/pprof/

# 一键 CPU 火焰图分析
go tool pprof -http=:8080 'http://localhost:8005/debug/pprof/profile?seconds=30'

# 一键堆内存分析
go tool pprof -http=:8080 http://localhost:8005/debug/pprof/heap

# 一键 goroutine 分析
go tool pprof -http=:8080 http://localhost:8005/debug/pprof/goroutine

# SSH 隧道远程访问
ssh -L 8005:localhost:8005 user@remote-server
go tool pprof -http=:8080 http://localhost:8005/debug/pprof/heap
```
