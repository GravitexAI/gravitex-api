#!/usr/bin/env bash
# 【在服务器上执行】手动上传新代码之后，跑这一条就行：
#
#     bash /workplace/py/testReport/after-upload.sh
#
# 它是幂等的，随便跑多少次都安全。做四件事：
#   1. 清掉 macOS 传输夹带的垃圾（._* / __MACOSX / .DS_Store / __pycache__）
#   2. 确保两个 venv 可用 —— 坏了或没有才重建，好的就不动（省时间）
#   3. 装/更新依赖（这样改了 requirements.txt 也不会漏）
#   4. 重启服务并验收
#
# 为什么需要这个脚本：venv 里全是写死的绝对路径，Mac 上的那份传到 Linux 必坏
# （bin/python 会指向 /Library/Developer/...）。而 systemctl restart 只是重启
# 服务，修不了 venv。详见 OPERATIONS.md §1 和 §6.2。

set -u

DIR="/workplace/py/testReport"
SERVICE="claude-test-service"

cd "$DIR" || { echo "❌ 找不到 $DIR"; exit 1; }

# ---------- 1. 清 macOS 垃圾 ----------
JUNK=$(find . -name '._*' -o -name '__MACOSX' -o -name '.DS_Store' 2>/dev/null | wc -l | tr -d ' ')
if [ "$JUNK" != "0" ]; then
  echo "==> 清理 $JUNK 个 macOS 残留文件"
  rm -rf __MACOSX
  find . -name '._*' -delete 2>/dev/null
  find . -name '.DS_Store' -delete 2>/dev/null
else
  echo "==> 无 macOS 残留"
fi
find . -name '__pycache__' -type d -exec rm -rf {} + 2>/dev/null
find . -name '.pytest_cache' -type d -exec rm -rf {} + 2>/dev/null

# ---------- 2+3. 确保 venv 可用并装依赖 ----------
ensure_venv() {
  local name="$1"
  local d="$DIR/$name"

  [ -d "$d" ] || { echo "❌ 找不到目录 $d"; return 1; }
  cd "$d" || return 1

  # -x 对断链返回 false，再加一次真实调用，两重保险
  if [ -x ".venv/bin/python" ] && .venv/bin/python --version >/dev/null 2>&1; then
    echo "==> $name 的 venv 正常（$(.venv/bin/python --version 2>&1)），不重建"
  else
    if [ -e ".venv/bin/python" ] || [ -L ".venv/bin/python" ]; then
      echo "==> $name 的 venv 已损坏（多半是从 Mac 传上来的），重建中"
      echo "    当前指向: $(readlink .venv/bin/python 2>/dev/null || echo '(非软链)')"
    else
      echo "==> $name 没有 venv，新建中"
    fi
    python3 -m venv --clear .venv || return 1
    .venv/bin/pip install -q --upgrade pip 2>&1 | grep -v 'WARNING: Running pip' || true
  fi

  # 依赖每次都装一遍：改了 requirements.txt 不用记得额外操作，
  # 没变化时 pip 几秒就跳过了。
  .venv/bin/pip install -q -r requirements.txt 2>&1 | grep -v 'WARNING: Running pip' || true
  cd "$DIR" || return 1
}

ensure_venv claude-platform-test || exit 1
ensure_venv claude-test-service  || exit 1

# ---------- 冒烟：只校验配置，不发任何网络请求、不花钱 ----------
echo "==> 配置校验"
cd "$DIR/claude-platform-test" || exit 1
printf "    "
.venv/bin/python run_tests.py --validate-config || { echo "❌ 配置校验失败，先修配置再重启服务"; exit 1; }
cd "$DIR" || exit 1

# ---------- 4. 重启 + 验收 ----------
echo "==> 重启服务"
systemctl restart "$SERVICE"
sleep 4

echo "==> 验收"
echo "    运行状态: $(systemctl is-active "$SERVICE")"
echo "    开机自启: $(systemctl is-enabled "$SERVICE")"
printf "    健康检查: "
HEALTH=$(curl -s --max-time 5 http://127.0.0.1:8900/health)
echo "$HEALTH"

printf "    模型/分类: "
curl -s --max-time 5 http://127.0.0.1:8900/meta > /tmp/_meta.json 2>/dev/null
"$DIR/claude-test-service/.venv/bin/python" - <<'PY' 2>/dev/null || echo "(取不到 /meta)"
import json
d = json.load(open('/tmp/_meta.json'))
print('{} 模型 / {} 分类 / {} 用例 / 缓存轮数 {}'.format(
    len(d['models']), len(d['categories']),
    sum(c['count'] for c in d['categories']), d['cache_hit_rounds']))
PY
rm -f /tmp/_meta.json

case "$HEALTH" in
  *'"ok":true'*) echo "" ; echo "✅ 全部就绪，可以去管理端用了" ;;
  *)             echo "" ; echo "❌ 健康检查未通过，看上面 /health 的提示（它会说明原因和修复命令）"; exit 1 ;;
esac
