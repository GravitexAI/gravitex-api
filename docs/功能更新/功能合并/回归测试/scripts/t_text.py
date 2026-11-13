#!/usr/bin/env python3
"""升级回归测试：文本类接口（chat/messages/responses/v1beta）流式与非流式。
用法：python3 t_text.py <sk-令牌> <结果.json>。模型名按环境里可用渠道调整。
每个用例记录 HTTP 状态、usage、响应模型、X-Api-Request-Id，结果写 JSON 供后续对账。"""
import json, sys, time, urllib.request, urllib.error

BASE = "http://localhost:3000"
KEY = sys.argv[1]
OUT = sys.argv[2]
LONG = ("You are a meticulous assistant. " + "The quick brown fox jumps over the lazy dog. " * 400)  # ~4k tokens，用于缓存测试


def call(name, path, body, stream=False, headers=None, method="POST"):
    h = {"Authorization": f"Bearer {KEY}", "Content-Type": "application/json"}
    if headers:
        h.update(headers)
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(BASE + path, data=data, headers=h, method=method)
    t0 = time.time()
    rec = {"name": name, "path": path, "stream": stream}
    try:
        with urllib.request.urlopen(req, timeout=300) as r:
            raw = r.read().decode("utf-8", "replace")
            rec["status"] = r.status
            rec["rid"] = r.headers.get("X-Api-Request-Id")
    except urllib.error.HTTPError as e:
        raw = e.read().decode("utf-8", "replace")
        rec["status"] = e.code
        rec["rid"] = e.headers.get("X-Api-Request-Id")
    except Exception as e:  # 网络/超时
        rec["status"] = -1
        raw = str(e)
    rec["ms"] = int((time.time() - t0) * 1000)
    rec["usage"], rec["model"], rec["text"] = parse(raw, stream)
    if rec["status"] != 200:
        rec["err"] = raw[:600]
    print(f"[{rec['status']}] {name} {rec['ms']}ms model={rec['model']} usage={json.dumps(rec['usage'])[:300]} text={rec['text'][:60]!r}", flush=True)
    if rec["status"] != 200:
        print("     ERR:", raw[:400], flush=True)
    return rec


def parse(raw, stream):
    """从非流式 JSON 或 SSE 中提取最后一个 usage、模型名和部分文本。"""
    usage, model, text = None, None, ""
    objs = []
    if stream:
        for line in raw.splitlines():
            line = line.strip()
            if line.startswith("data:"):
                p = line[5:].strip()
                if p and p != "[DONE]":
                    try:
                        objs.append(json.loads(p))
                    except Exception:
                        pass
    else:
        try:
            objs.append(json.loads(raw))
        except Exception:
            return None, None, raw[:100]
    for o in objs:
        if not isinstance(o, dict):
            continue
        for src in (o, o.get("response") or {}, o.get("message") or {}):
            if isinstance(src, dict):
                if src.get("usage"):
                    usage = src["usage"]
                if src.get("usageMetadata"):
                    usage = src["usageMetadata"]
                if src.get("model") or src.get("modelVersion"):
                    model = src.get("model") or src.get("modelVersion")
        # 文本片段
        for c in o.get("choices") or []:
            text += ((c.get("message") or {}).get("content") or (c.get("delta") or {}).get("content") or "") if isinstance(c, dict) else ""
        for c in o.get("content") or []:
            if isinstance(c, dict) and c.get("type") == "text":
                text += c.get("text", "")
        d = o.get("delta") or {}
        if isinstance(d, dict) and d.get("type") == "text_delta":
            text += d.get("text", "")
        if o.get("type") == "response.output_text.delta":
            text += o.get("delta", "")
        for cand in o.get("candidates") or []:
            for p in ((cand.get("content") or {}).get("parts") or []):
                text += p.get("text", "") if isinstance(p, dict) else ""
        if o.get("output_text"):
            text += o["output_text"]
    return usage, model, text


R = []
msg = [{"role": "user", "content": "Reply with exactly: pong"}]
so = {"include_usage": True}

# ---- GPT ----
R.append(call("gpt chat", "/v1/chat/completions", {"model": "gpt-5.6-sol", "messages": msg}))
R.append(call("gpt chat stream", "/v1/chat/completions", {"model": "gpt-5.6-sol", "messages": msg, "stream": True, "stream_options": so}, True))
R.append(call("gpt responses", "/v1/responses", {"model": "gpt-5.6-sol", "input": "Reply with exactly: pong"}))
R.append(call("gpt responses stream", "/v1/responses", {"model": "gpt-5.6-sol", "input": "Reply with exactly: pong", "stream": True}, True))
R.append(call("gpt messages(跨格式)", "/v1/messages", {"model": "gpt-5.6-sol", "max_tokens": 64, "messages": msg}))

# ---- Claude ----
R.append(call("claude chat", "/v1/chat/completions", {"model": "claude-sonnet-5", "messages": msg}))
R.append(call("claude chat stream", "/v1/chat/completions", {"model": "claude-sonnet-5", "messages": msg, "stream": True, "stream_options": so}, True))
R.append(call("claude messages", "/v1/messages", {"model": "claude-sonnet-5", "max_tokens": 64, "messages": msg}))
R.append(call("claude messages stream", "/v1/messages", {"model": "claude-sonnet-5", "max_tokens": 64, "messages": msg, "stream": True}, True))
# 缓存：5m 写入→读取；1h 写入
sys5 = [{"type": "text", "text": LONG, "cache_control": {"type": "ephemeral"}}]
R.append(call("claude cache5m write", "/v1/messages", {"model": "claude-sonnet-5", "max_tokens": 32, "system": sys5, "messages": msg}))
R.append(call("claude cache5m read", "/v1/messages", {"model": "claude-sonnet-5", "max_tokens": 32, "system": sys5, "messages": msg}))
sys1h = [{"type": "text", "text": "[1h] " + LONG, "cache_control": {"type": "ephemeral", "ttl": "1h"}}]
R.append(call("claude cache1h write", "/v1/messages", {"model": "claude-sonnet-5", "max_tokens": 32, "system": sys1h, "messages": msg}))
R.append(call("claude websearch", "/v1/messages", {"model": "claude-sonnet-5", "max_tokens": 256,
                                                   "tools": [{"type": "web_search_20250305", "name": "web_search", "max_uses": 1}],
                                                   "messages": [{"role": "user", "content": "Search the web: what is today's top headline? one sentence."}]}))

# ---- Gemini ----
R.append(call("gemini chat", "/v1/chat/completions", {"model": "gemini-3.6-flash", "messages": msg}))
R.append(call("gemini chat stream", "/v1/chat/completions", {"model": "gemini-3.6-flash", "messages": msg, "stream": True, "stream_options": so}, True))
gbody = {"contents": [{"role": "user", "parts": [{"text": "Reply with exactly: pong"}]}]}
R.append(call("gemini v1beta generateContent", "/v1beta/models/gemini-3.6-flash:generateContent", gbody))
R.append(call("gemini v1beta stream", "/v1beta/models/gemini-3.6-flash:streamGenerateContent?alt=sse", gbody, True))
R.append(call("gemini messages(跨格式)", "/v1/messages", {"model": "gemini-3.6-flash", "max_tokens": 64, "messages": msg}))
R.append(call("gemini google_search", "/v1beta/models/gemini-3.6-flash:generateContent",
              {"contents": [{"role": "user", "parts": [{"text": "Use Google Search: who won the most recent FIFA World Cup? one sentence."}]}],
               "tools": [{"google_search": {}}]}))

json.dump(R, open(OUT, "w"), ensure_ascii=False, indent=1)
print("done", len(R), "fail", sum(1 for r in R if r["status"] != 200))
