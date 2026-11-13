#!/bin/bash
# 用法：bash t_token.sh <系统访问令牌> <用户ID>
# 令牌管理接口回归：列表/搜索/创建/查询/取 key/修改/用量查询/禁用/删除
B=http://localhost:3000
AT="$1"; UID_="$2"
H=(-H "Authorization: $AT" -H "New-Api-User: $UID_" -H "Content-Type: application/json")
j() { python3 -c "import json,sys;d=json.load(sys.stdin);print(eval(sys.argv[1]))" "$1"; }
show() { echo "== $1"; echo "$2" | head -c ${3:-400}; echo; }

R=$(curl -s "${H[@]}" "$B/api/token/?p=1&size=3"); show "列表" "$R" 300
R=$(curl -s "${H[@]}" "$B/api/token/auto-groups"); show "auto-groups" "$R" 300
R=$(curl -s "${H[@]}" -X POST "$B/api/token/" -d '{"name":"merge-test-1113","remain_quota":200000,"unlimited_quota":false,"expired_time":-1,"group":"auto"}'); show "创建" "$R"
R=$(curl -s "${H[@]}" "$B/api/token/search?keyword=merge-test-1113"); show "搜索" "$R" 600
TID=$(echo "$R" | j "[t['id'] for t in (d['data'] if isinstance(d['data'],list) else d['data'].get('items',[]))][0]")
echo "TID=$TID"
R=$(curl -s "${H[@]}" "$B/api/token/$TID"); show "查询单个" "$R" 600
R=$(curl -s "${H[@]}" -X POST "$B/api/token/$TID/key"); KEY=$(echo "$R" | j "d['data']['key'] if isinstance(d['data'],dict) else d['data']"); show "取key" "$(echo "$R" | sed 's/[A-Za-z0-9]\{20,\}/***/g')"
case "$KEY" in sk-*) ;; *) KEY="sk-$KEY";; esac
R=$(curl -s "${H[@]}" -X PUT "$B/api/token/" -d "{\"id\":$TID,\"name\":\"merge-test-1113-upd\",\"remain_quota\":200000,\"unlimited_quota\":false,\"expired_time\":-1,\"group\":\"auto\",\"allow_user_quota_query\":true}"); show "修改(允许查用户额度)" "$R" 600
R=$(curl -s -H "Authorization: Bearer $KEY" "$B/api/usage/token/"); show "令牌用量 /api/usage/token" "$R" 800
R=$(curl -s -H "Authorization: Bearer $KEY" "$B/v1/dashboard/billing/subscription"); show "billing/subscription" "$R" 300
R=$(curl -s -H "Authorization: Bearer $KEY" -H "Content-Type: application/json" "$B/v1/chat/completions" -d '{"model":"gpt-5.6-sol","messages":[{"role":"user","content":"Reply with exactly: pong"}]}'); show "新令牌调用" "$R" 200
sleep 6
R=$(curl -s "${H[@]}" "$B/api/token/$TID"); show "调用后令牌额度" "$(echo "$R" | j "{k:d['data'][k] for k in ('remain_quota','used_quota','allow_user_quota_query','name','status')}")"
R=$(curl -s "${H[@]}" -X PUT "$B/api/token/?status_only=true" -d "{\"id\":$TID,\"status\":2}"); show "禁用" "$R" 300
R=$(curl -s -H "Authorization: Bearer $KEY" -H "Content-Type: application/json" "$B/v1/chat/completions" -d '{"model":"gpt-5.6-sol","messages":[{"role":"user","content":"hi"}]}'); show "禁用后调用(应拒绝)" "$R" 300
R=$(curl -s "${H[@]}" -X DELETE "$B/api/token/$TID"); show "删除" "$R" 300
R=$(curl -s "${H[@]}" "$B/api/token/$TID"); show "删除后查询(应不存在)" "$R" 300
R=$(curl -s -H "Authorization: Bearer $KEY" -H "Content-Type: application/json" "$B/v1/chat/completions" -d '{"model":"gpt-5.6-sol","messages":[{"role":"user","content":"hi"}]}'); show "删除后调用(应拒绝)" "$R" 300
