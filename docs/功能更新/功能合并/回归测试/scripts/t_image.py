#!/usr/bin/env python3
"""升级回归测试：生图（gpt-image 系列 + Gemini 生图），覆盖 images/generations、images/edits、chat、v1beta。
用法：python3 t_image.py <sk-令牌> <结果.json> <输入图片.jpg>   （会产生真实上游费用，统一用低质量 / 1K 档）
输入图片用于 edits / 图生图，任意风景 jpg 即可（测试机直连 Wikimedia 容易被限流，建议本地下载后上传）。
结果格式与 t_text.py 相同，可直接用 recon.py 对账。"""
import base64, json, sys, time, uuid, urllib.request, urllib.error

BASE = "http://localhost:3000"
KEY = sys.argv[1]
OUT = sys.argv[2]
IMG_FILE = sys.argv[3]
PROMPT = "A small red paper boat floating on a calm blue lake, minimal flat illustration"
R = []


def http(path, data, ctype):
    h = {"Authorization": f"Bearer {KEY}", "Content-Type": ctype}
    req = urllib.request.Request(BASE + path, data=data, headers=h, method="POST")
    t0 = time.time()
    try:
        with urllib.request.urlopen(req, timeout=600) as r:
            return r.status, r.headers.get("X-Api-Request-Id"), r.read().decode("utf-8", "replace"), int((time.time() - t0) * 1000)
    except urllib.error.HTTPError as e:
        return e.code, e.headers.get("X-Api-Request-Id"), e.read().decode("utf-8", "replace"), int((time.time() - t0) * 1000)


def count_images(js):
    """统计响应里返回的图片张数：OpenAI data[]、Gemini inlineData、chat 里的 markdown/data URI。"""
    n = 0
    for d in js.get("data") or []:
        n += 1 if (d.get("b64_json") or d.get("url")) else 0
    for c in js.get("candidates") or []:
        for p in (c.get("content") or {}).get("parts") or []:
            n += 1 if (p.get("inlineData") or p.get("inline_data")) else 0
    for c in js.get("choices") or []:
        msg = c.get("message") or {}
        content = msg.get("content") or ""
        if isinstance(content, str):
            n += content.count("data:image") + content.count("](http")
        n += len(msg.get("images") or [])
    return n


def record(name, status, rid, raw, ms):
    try:
        js = json.loads(raw)
    except Exception:
        js = {}
    usage = js.get("usage") or js.get("usageMetadata")
    rec = {"name": name, "status": status, "rid": rid, "ms": ms, "images": count_images(js), "usage": usage}
    if status != 200:
        rec["err"] = raw[:500]
    print(f"[{status}] {name} {ms}ms 图片数={rec['images']} usage={json.dumps(usage)[:260]}", flush=True)
    if status != 200:
        print("     ERR:", raw[:400], flush=True)
    R.append(rec)


def gen(name, body):
    record(name, *http("/v1/images/generations", json.dumps(body).encode(), "application/json"))


def jpost(name, path, body):
    record(name, *http(path, json.dumps(body).encode(), "application/json"))


def edit(name, model, image_bytes, extra=None):
    """multipart/form-data 调 /v1/images/edits。"""
    boundary = uuid.uuid4().hex
    fields = {"model": model, "prompt": "Add a small red paper boat on the lake", "quality": "low", "size": "1024x1024"}
    fields.update(extra or {})
    body = b""
    for k, v in fields.items():
        body += f"--{boundary}\r\nContent-Disposition: form-data; name=\"{k}\"\r\n\r\n{v}\r\n".encode()
    body += f"--{boundary}\r\nContent-Disposition: form-data; name=\"image\"; filename=\"in.jpg\"\r\nContent-Type: image/jpeg\r\n\r\n".encode() + image_bytes + b"\r\n"
    body += f"--{boundary}--\r\n".encode()
    record(name, *http("/v1/images/edits", body, f"multipart/form-data; boundary={boundary}"))


img = open(IMG_FILE, "rb").read()
img_b64 = base64.b64encode(img).decode()

# 生图上游有时很慢（单张可能几分钟），默认只跑轻量的 4 个用例：OpenAI 生成/编辑各一、Gemini 一个。
# 大更新需要更全时，把 FULL 设为 True，会把 gpt-image 2/2.5/2.6 全系、n=2、Gemini 多模型和多端点都测一遍。
FULL = False

gen("gpt-image-2 generations", {"model": "gpt-image-2", "prompt": PROMPT, "size": "1024x1024", "quality": "low", "n": 1})
edit("gpt-image-2 edits", "gpt-image-2", img)
gen("gpt-image-2.6-flare generations", {"model": "gpt-image-2.6-flare", "prompt": PROMPT, "size": "1024x1024", "quality": "low", "n": 1})
gbody = {"contents": [{"role": "user", "parts": [{"text": PROMPT}]}],
         "generationConfig": {"responseModalities": ["TEXT", "IMAGE"], "imageConfig": {"aspectRatio": "1:1"}}}
jpost("gemini-3.1-flash-image v1beta", "/v1beta/models/gemini-3.1-flash-image:generateContent", gbody)

if FULL:
    for m in ("gpt-image-2.5-flare", "gpt-image-2.5-sunburst", "gpt-image-2.6-sunburst"):
        gen(f"{m} generations", {"model": m, "prompt": PROMPT, "size": "1024x1024", "quality": "low", "n": 1})
    gen("gpt-image-2 generations n=2", {"model": "gpt-image-2", "prompt": PROMPT, "size": "1024x1024", "quality": "low", "n": 2})
    edit("gpt-image-2.6-flare edits", "gpt-image-2.6-flare", img)
    for m in ("gemini-nano-banana-2.1",):
        jpost(f"{m} v1beta", f"/v1beta/models/{m}:generateContent", gbody)
    g3 = json.loads(json.dumps(gbody)); g3["generationConfig"]["imageConfig"]["imageSize"] = "1K"
    jpost("gemini-3-pro-image v1beta 1K", "/v1beta/models/gemini-3-pro-image:generateContent", g3)
    gi = {"contents": [{"role": "user", "parts": [{"inlineData": {"mimeType": "image/jpeg", "data": img_b64}}, {"text": "Add a small red paper boat on the lake"}]}],
          "generationConfig": {"responseModalities": ["TEXT", "IMAGE"]}}
    jpost("gemini-3.1-flash-image v1beta 图生图", "/v1beta/models/gemini-3.1-flash-image:generateContent", gi)
    jpost("gemini-3.1-flash-image chat", "/v1/chat/completions", {"model": "gemini-3.1-flash-image", "messages": [{"role": "user", "content": PROMPT}]})

json.dump(R, open(OUT, "w"), ensure_ascii=False, indent=1)
print("done", len(R), "fail", sum(1 for r in R if r["status"] != 200))
