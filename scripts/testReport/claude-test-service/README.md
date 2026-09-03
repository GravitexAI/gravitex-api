# Claude 渠道测试报告服务

管理端「渠道管理 → 生成测试报告」的后端执行器。接收 Java 转发过来的测试
参数，起子进程跑同级目录的 `claude-platform-test`，把生成的 Excel 报告回传。

## 为什么是独立服务

Java 镜像是 `eclipse-temurin:17-jre-jammy`、Go 镜像是 `debian:bookworm-slim`，
两个容器里都没有 Python。把 Python 塞进 Java 镜像会让脚本改一行就要重新构建
发布整个后端，所以拆成独立服务。

## 部署

部署路径：`/workplace/py/testReport/`。全链路不使用任何非 ASCII 路径——中文路径
会流经 systemd 单元解析、shell、`WorkingDirectory`，以及 venv 生成脚本的绝对
路径 shebang，某些环境下会出问题。

落地后的结构（**两个目录必须同级**，`app.py` 靠 `../claude-platform-test` 相对定位）：

```text
/workplace/py/testReport/
├── claude-platform-test/     # 测试脚本
└── claude-test-service/      # 本服务
```

```bash
# 1. 直传两个目录（在 scripts/testReport 下执行）
#    用 tar 管道而不是 scp -r：可以排除 .venv 等本地产物，且不落任何中转目录。
#    venv 必须排除——它生成的脚本 shebang 是绝对路径，换机器必坏，服务器上要重建。
#    带 venv 是 85MB，排除后只有 0.5MB。
#
#    两个坑（实测踩过）：
#    a) macOS 自带的 bsdtar，--exclude 是拿整条路径做匹配的，写 '.venv' 只能匹配
#       根级同名项，子目录里的一个都排不掉。必须写成 '*/.venv' 和 '*/.venv/*'。
#    b) COPYFILE_DISABLE=1 必须加，否则 bsdtar 会为每个文件生成 ._xxx 的
#       AppleDouble 附属文件一起打包，服务器上 pytest 会去收集 ._latency_test.py
#       然后报 "TypeError: the 'package' argument is required"。
ssh root@<server> 'mkdir -p /workplace/py/testReport'
COPYFILE_DISABLE=1 tar \
    --exclude='*/.venv' --exclude='*/.venv/*' \
    --exclude='*/__pycache__' --exclude='*/__pycache__/*' \
    --exclude='*/.pytest_cache' --exclude='*/.pytest_cache/*' \
    --exclude='*/.idea' --exclude='*/.idea/*' \
    --exclude='*/reports/*' --exclude='*.xlsx' --exclude='.DS_Store' \
    -czf - claude-platform-test claude-test-service \
  | ssh root@<server> 'tar -xzf - -C /workplace/py/testReport'

# 传完确认一遍没夹带（两条都应该输出 0）：
ssh root@<server> 'find /workplace/py/testReport -name ".venv" -o -name "._*" | wc -l'

# 2. 在服务器上重建两个 venv（脚本一个、服务一个）
cd /workplace/py/testReport/claude-platform-test
python3 -m venv .venv && .venv/bin/pip install -r requirements.txt

cd /workplace/py/testReport/claude-test-service
python3 -m venv .venv && .venv/bin/pip install -r requirements.txt

# 3. 冒烟：只校验配置，不发任何网络请求
cd /workplace/py/testReport/claude-platform-test
.venv/bin/python run_tests.py --validate-config
# 预期：配置校验通过：1 个模型，37 个用例

# 4. 装 systemd 服务
cp /workplace/py/testReport/claude-test-service/claude-test-service.service /etc/systemd/system/
systemctl daemon-reload
systemctl enable --now claude-test-service
curl -s http://127.0.0.1:8900/health
# 预期：{"ok":true}
```

更新脚本时重复第 1 步即可（`Restart=always`，改完 `systemctl restart claude-test-service`）。

## 安全约束

**只监听 127.0.0.1，不要暴露公网。** `/run` 的入参里带明文 API Key，服务本身
没有鉴权层，鉴权由 Java 后端负责。如果 Java 和本服务不同机，改成监听内网网卡
并用防火墙限制来源 IP。

## 并发

同一时刻只跑一个任务，第二个请求直接 409。脚本内部已经是 8 线程并发
（`config.MAX_WORKERS`），再叠加多任务会打爆上游限流，429 会被用例误判成
"能力缺失"。
