#!/usr/bin/env bash
# 【在服务器上执行】手动上传新代码之后，跑这一条就行：
#
#     bash /workplace/py/testReport/after-upload.sh
#
# 它是幂等的，随便跑多少次都安全。做五件事：
#   0. 确认没有任务正在跑（重启会连子进程一起杀掉，已消耗的额度不退）
#   1. 清掉 macOS 传输夹带的垃圾（._* / __MACOSX / .DS_Store / __pycache__）
#   2. 确保两个 venv 可用 —— 坏了或没有才重建，好的就不动（省时间）
#   3. 装/更新依赖（这样改了 requirements.txt 也不会漏）
#   4. 自检错误诊断工具（它不需要 venv，用服务自己的解释器跑）
#   5. 装/更新 systemd 单元，重启服务并验收
#
# 为什么需要这个脚本：venv 里全是写死的绝对路径，Mac 上的那份传到 Linux 必坏
# （bin/python 会指向 /Library/Developer/...）。而 systemctl restart 只是重启
# 服务，修不了 venv。详见 RUNBOOK.md 场景 3 和「一定不要做的两件事」。

set -u

DIR="/workplace/py/testReport"
SERVICE="claude-test-service"
DIAG_DIR="$DIR/error-diagnosis"
SERVICE_PY="$DIR/claude-test-service/.venv/bin/python"

cd "$DIR" || { echo "❌ 找不到 $DIR"; exit 1; }

# ---------- 0. 先确认没有任务正在跑 ----------
# 重启会带走整个 cgroup，包括正在跑的测试/诊断子进程：那次任务直接消失，
# 管理端轮询拿到失败，渠道测试已消耗的额度不退。
#
# 模式写成 '[r]un_tests' 是为了避免 pgrep 匹配到自己：pgrep -f 比对的是完整命令行，
# 而执行这条命令的 shell 自身的命令行里就含这个模式串。正则 [r]un_tests 能匹配
# "run_tests"，但命令行里的字面量是 "[r]un_tests"，不含 "run_tests"，自匹配被消掉。
#
# 用 `| wc -l` 而不是 `pgrep -c ... || echo 0`：pgrep 无匹配时既打印 0 又返回非零
# 退出码，`|| echo 0` 会跟着触发，拼出 "0\n0" 让判断恒为真。（两处都实测踩过。）
BUSY=$(pgrep -f '[r]un_tests\.py|[e]rror_diagnosis\.py|[a]i_analysis\.py' 2>/dev/null | wc -l | tr -d ' ')
if [ "$BUSY" != "0" ]; then
  echo "⚠️  检测到 $BUSY 个任务子进程正在跑（渠道测试 / 错误诊断）。"
  echo "    现在重启会把它们杀掉，报告拿不到，渠道测试已消耗的额度不退。"
  echo "    等它跑完再来，或确认要强制继续就加 FORCE=1 重跑："
  echo "        FORCE=1 bash $DIR/after-upload.sh"
  [ "${FORCE:-}" = "1" ] || exit 1
  echo "    FORCE=1 已设置，继续。"
else
  echo "==> 无任务在跑，可以安全重启"
fi

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

# ---------- 4. 自检错误诊断工具 ----------
# 它只用标准库，所以刻意不给它建 venv —— 直接用服务自己的解释器跑，
# 顺带绕开了「Mac 的 venv 传上来会断链」这一整类问题。
#
# 缺了它不影响渠道测试，所以这里只警告不退出：服务照常起，
# 管理端点「错误诊断」时 /diagnosis/meta 会如实回报 available=false 和原因。
DIAG_OK=""
if [ ! -f "$DIAG_DIR/error_diagnosis.py" ]; then
  echo "⚠️  没找到 $DIAG_DIR —— 管理端的「错误诊断」不可用（渠道测试不受影响）"
  echo "    要用的话把 error-diagnosis 目录传到 $DIR/ 下，再跑一次本脚本"
elif "$SERVICE_PY" "$DIAG_DIR/error_diagnosis.py" --help >/dev/null 2>&1; then
  echo "==> 错误诊断工具正常（$("$SERVICE_PY" --version 2>&1)）"
  DIAG_OK="1"
else
  echo "⚠️  错误诊断工具跑不起来（渠道测试不受影响），报错如下："
  "$SERVICE_PY" "$DIAG_DIR/error_diagnosis.py" --help 2>&1 | tail -5 | sed 's/^/    /'
  echo "    诊断脚本需要 Python 3.10+，先确认服务 venv 的版本"
fi

# .env 只提供 AI 分析的默认接口地址和模型；密钥由管理员在弹窗里现填，不从这里读。
# 从 Mac 直接覆盖上来会把线上配置换成占位值，这里点出来，免得到时候对着
# "模型不存在" 的报错查半天。
if [ -n "$DIAG_OK" ]; then
  if [ ! -f "$DIAG_DIR/.env" ]; then
    echo "    提示：$DIAG_DIR/.env 不存在，AI 分析没有默认模型，得在弹窗里手填"
  elif grep -q 'sk-xxx\|你的密钥\|你的接口地址' "$DIAG_DIR/.env" 2>/dev/null; then
    echo "    ⚠️  $DIAG_DIR/.env 里还是占位值，多半是被本地文件覆盖了，确认一下"
  fi
fi

# ---------- 3.5 确保 systemd 单元已安装且是最新的 ----------
# 放在这里而不是让人手敲，是为了让「从零部署」和「日常更新」用同一条命令。
# 单元文件内容变了也会自动更新（比如改了端口）。
UNIT_SRC="$DIR/claude-test-service/claude-test-service.service"
UNIT_DST="/etc/systemd/system/$SERVICE.service"
if [ ! -f "$UNIT_SRC" ]; then
  echo "❌ 找不到 $UNIT_SRC"; exit 1
fi
if [ ! -f "$UNIT_DST" ]; then
  echo "==> systemd 单元未安装，安装中"
  cp "$UNIT_SRC" "$UNIT_DST"
  systemctl daemon-reload
  systemctl enable "$SERVICE" >/dev/null 2>&1
elif ! cmp -s "$UNIT_SRC" "$UNIT_DST"; then
  echo "==> systemd 单元有更新，同步中"
  cp "$UNIT_SRC" "$UNIT_DST"
  systemctl daemon-reload
  systemctl enable "$SERVICE" >/dev/null 2>&1
else
  echo "==> systemd 单元已是最新"
fi

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

printf "    错误诊断: "
curl -s --max-time 5 http://127.0.0.1:8900/diagnosis/meta > /tmp/_diag.json 2>/dev/null
"$SERVICE_PY" - <<'PY' 2>/dev/null || echo "(取不到 /diagnosis/meta)"
import json
d = json.load(open('/tmp/_diag.json'))
print('可用（默认模型 {}）'.format(d['default_model'] or '未配置')
      if d['available'] else '不可用 — ' + d['unavailable_reason'])
PY
rm -f /tmp/_diag.json

case "$HEALTH" in
  *'"ok":true'*) echo "" ; echo "✅ 全部就绪，可以去管理端用了" ;;
  *)             echo "" ; echo "❌ 健康检查未通过，看上面 /health 的提示（它会说明原因和修复命令）"; exit 1 ;;
esac
