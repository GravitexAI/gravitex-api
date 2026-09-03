#!/usr/bin/env bash
# 一键部署/更新 Python 侧代码到服务器。
#
# 用法（在本文件所在目录执行）：
#   ./deploy.sh prod        # 生产 101.47.158.158
#   ./deploy.sh test        # 测试 101.47.154.214
#   ./deploy.sh <IP>        # 任意机器
#
# 做的事：传代码 → 自检没夹带垃圾 → 重启服务 → 验收。
# 不做的事：不动 venv、不动 systemd、不动 Java/nginx。
#
# 为什么必须重启：跑测试的是每次新起的子进程（读磁盘最新代码），但 /meta 的模型/
# 分类清单是常驻服务进程 import 进来的、Python 模块进程级缓存。不重启会出现
# "弹窗按旧配置估算请求次数、子进程按新配置实际跑"的不一致，额度按实际烧。

set -euo pipefail

KEY="${DEPLOY_KEY:-/Users/caihongzhan/gravitexSpace/se1se.pem}"
REMOTE_DIR="/workplace/py/testReport"
SERVICE="claude-test-service"

case "${1:-}" in
  prod) HOST="101.47.158.158" ;;
  test) HOST="101.47.154.214" ;;
  "")   echo "用法: $0 {prod|test|<IP>}" >&2; exit 2 ;;
  *)    HOST="$1" ;;
esac

cd "$(dirname "$0")"
[ -d claude-platform-test ] && [ -d claude-test-service ] \
  || { echo "错误：当前目录下找不到 claude-platform-test / claude-test-service" >&2; exit 1; }

SSH="ssh -i $KEY -o BatchMode=yes -o ConnectTimeout=15 root@$HOST"

echo "==> 目标: $HOST:$REMOTE_DIR"

# ---- 0. 先确认没有正在跑的测试（重启会杀掉子进程）----
# 直接看有没有跑测试的子进程，而不是去 grep 日志——日志里的 POST /run 包含被
# 400/409 拒掉的请求，拿它判断会大量误报。
#
# 模式写成 '[r]un_tests' 是为了避免 pgrep 匹配到自己：pgrep -f 比对的是完整命令
# 行，而远端执行这条命令的 shell 自身的命令行里就含这个模式串。正则 [r]un_tests
# 能匹配 "run_tests"，但命令行里的字面量是 "[r]un_tests"，不含 "run_tests"，
# 于是自匹配被消掉。（实测踩过：不加中括号会恒定误报 1 个进程在跑。）
#
# 用 `| wc -l` 而不是 `pgrep -c ... || echo 0`：pgrep 无匹配时既打印 0 又返回
# 非零退出码，`|| echo 0` 会跟着触发，拼出 "0\n0" 让判断恒为真。wc -l 永远只
# 输出一个数字且退出码为 0。（实测踩过。）
echo "==> 检查是否有测试正在跑"
RUNNING=$($SSH "pgrep -f '[r]un_tests\.py' 2>/dev/null | wc -l" | tr -d ' \n')
if [ "$RUNNING" != "0" ]; then
  echo "    ⚠️  检测到 $RUNNING 个 run_tests.py 子进程正在跑测试。"
  echo "    重启会杀掉它（已消耗的额度不退，报告也拿不到）。"
  if [ -t 0 ]; then
    read -r -p "    确认继续？(yes/no) " ans
    [ "$ans" = "yes" ] || { echo "已取消"; exit 1; }
  else
    echo "    非交互环境，已中止。确认要强制部署请加 FORCE=1 重跑。"
    [ "${FORCE:-}" = "1" ] || exit 1
    echo "    FORCE=1 已设置，继续。"
  fi
else
  echo "    ✅ 无测试在跑，可以安全重启"
fi

# ---- 1. 传代码 ----
# COPYFILE_DISABLE=1：不然 macOS 的 bsdtar 会给每个文件生成 ._xxx 附属文件
# --exclude 必须写成 '*/xxx' 形式：bsdtar 拿整条路径匹配，写 'xxx' 只匹配根级
echo "==> 传输代码（排除 venv / 缓存 / 报告产物）"
COPYFILE_DISABLE=1 tar \
    --exclude='*/.venv' --exclude='*/.venv/*' \
    --exclude='*/__pycache__' --exclude='*/__pycache__/*' \
    --exclude='*/.pytest_cache' --exclude='*/.pytest_cache/*' \
    --exclude='*/.idea' --exclude='*/.idea/*' \
    --exclude='*/reports/*' --exclude='*.xlsx' --exclude='.DS_Store' \
    -czf - claude-platform-test claude-test-service 2>/dev/null \
  | $SSH "tar -xzf - -C $REMOTE_DIR"

# ---- 2. 自检：有没有夹带 macOS 垃圾 ----
echo "==> 自检传输结果"
JUNK=$($SSH "find $REMOTE_DIR -name '._*' -o -name '__MACOSX' -o -name '.DS_Store' | wc -l" | tr -d ' ')
if [ "$JUNK" != "0" ]; then
  echo "    ❌ 发现 $JUNK 个 macOS 残留文件，清理中"
  $SSH "cd $REMOTE_DIR && find . -name '._*' -delete; find . -name '.DS_Store' -delete; rm -rf __MACOSX"
  echo "    已清理"
else
  echo "    ✅ 无 macOS 残留"
fi

VENVS=$($SSH "find $REMOTE_DIR -maxdepth 2 -name '.venv' | wc -l" | tr -d ' ')
echo "    venv 完好: $VENVS 个（应为 2，被 tar 排除所以不受影响）"

# ---- 3. 重启 ----
echo "==> 重启服务"
$SSH "systemctl restart $SERVICE"
sleep 4

# ---- 4. 验收 ----
echo "==> 验收"
$SSH "
  echo \"    运行状态: \$(systemctl is-active $SERVICE)\"
  echo \"    开机自启: \$(systemctl is-enabled $SERVICE)\"
  printf '    健康检查: '; curl -s --max-time 5 http://127.0.0.1:8900/health || echo '失败'
  echo
  printf '    /meta:    '
  curl -s --max-time 5 http://127.0.0.1:8900/meta > /tmp/meta.json
  $REMOTE_DIR/claude-test-service/.venv/bin/python - <<'PYEOF'
import json
d = json.load(open('/tmp/meta.json'))
total = sum(c['count'] for c in d['categories'])
print('{} 模型 / {} 分类 / {} 用例 / 缓存轮数 {}'.format(
    len(d['models']), len(d['categories']), total, d['cache_hit_rounds']))
PYEOF
  rm -f /tmp/meta.json
  printf '    配置校验: '
  cd $REMOTE_DIR/claude-platform-test && .venv/bin/python run_tests.py --validate-config
"

echo "==> 完成。若刚才改了 requirements.txt，还需要手动装依赖："
echo "    ssh -i $KEY root@$HOST '"
echo "      cd $REMOTE_DIR/claude-platform-test && .venv/bin/pip install -r requirements.txt"
echo "      systemctl restart $SERVICE'"
