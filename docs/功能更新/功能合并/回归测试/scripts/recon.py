#!/usr/bin/env python3
"""计费对账：按 t_text.py 结果里的 X-Api-Request-Id 找到对应消费日志，打印计费关键字段，并核对用户余额变化。
用法（在测试机 scripts 目录下）：
  python3 recon.py <结果.json> <用户ID> <开始时间戳> <开始前用户余额>
依赖同目录 db.sh（主库查余额、日志库查 logs）。"""
import base64, json, subprocess, sys

result_file, user_id, start_ts, quota_before = sys.argv[1], sys.argv[2], int(sys.argv[3]), int(sys.argv[4])
KEYS = ["billing_mode", "matched_tier", "model_ratio", "completion_ratio", "group_ratio", "cache_ratio",
        "cache_tokens", "cache_creation_tokens", "cache_creation_tokens_5m", "cache_creation_tokens_1h",
        "cache_creation_ratio_5m", "cache_creation_ratio_1h", "tool_surcharges", "upstream_model_fallback"]


def sql(which, q):
    return subprocess.check_output(["./db.sh", which, "-N", "-B", "-e", q], text=True)


rows = sql("log", f"select id,type,model_name,quota,prompt_tokens,completion_tokens,channel_id,is_stream,"
                  f"replace(to_base64(other),'\\n','') from logs where user_id={user_id} and created_at>={start_ts - 2} order by id")
logs, total = {}, 0
for line in rows.splitlines():
    f = line.split("\t")
    other = json.loads(base64.b64decode(f[8]).decode()) if f[8] not in ("", "NULL") else {}
    logs[other.get("system_request_id")] = (f, other)
    total += int(f[3])

for r in json.load(open(result_file)):
    hit = logs.get(r.get("rid"))
    if not hit:
        print(f"!! 没找到日志：{r['name']} rid={r.get('rid')} status={r['status']}")
        continue
    f, o = hit
    print(f"== {r['name']} | type={f[1]} quota={f[3]} p={f[4]} c={f[5]} 渠道={f[6]} 流式={f[7]}")
    print("   ", json.dumps({k: o[k] for k in KEYS if k in o}, ensure_ascii=False)[:600])
    if o.get("expr_b64"):
        print("    expr:", base64.b64decode(o["expr_b64"]).decode()[:240])

quota_after = int(sql("main", f"select quota from users where id={user_id}").strip())
print(f"\n日志合计扣费 {total}（含重试等 0 额度日志），余额变化 {quota_before - quota_after}，"
      f"{'一致 ✅' if total == quota_before - quota_after else '不一致 ❌（检查是否有其他请求同时在跑）'}")
