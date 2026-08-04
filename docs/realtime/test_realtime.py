"""
Realtime API 交互测试脚本
- 文字：每轮键盘输入
- 音频：麦克风录音（按 Enter 开始，再按 Enter 停止）
- 图片：配置 IMAGE_SOURCE，每轮可选是否附带
- 支持任意组合：图片+文字 / 图片+录音 / 纯文字 / 纯录音

依赖安装：
    python3 -m pip install websockets pyaudio

运行：
    python3 test_realtime.py
"""

import asyncio
import base64
import json
import queue
import threading
import time
import urllib.request
from pathlib import Path

import websockets

# ============================================================
# 配置区 — 按需修改
# ============================================================
BASE_URL = "wss://api.gravitex.ai/v1/realtime"
API_KEY  = "sk-uzK3kA0iYAN9g5xxxxxxxxxx"
# MODEL    = "gpt-realtime-2"
MODEL    = "gpt-realtime-1.5"

# 图片文件（OSS URL 或本地绝对路径，留空则不使用图片）
# ⚠️  注意：Azure OpenAI Realtime API 目前不支持图片输入！
#    仅支持 input_text 和 input_audio，发送图片会触发服务端 500 错误。
#    Vision 支持仅在非 Realtime 的 Azure OpenAI 模型（Chat Completions / Responses）中可用。
#    官方说明：https://learn.microsoft.com/en-us/answers/questions/5706094/how-to-use-input-image-function-in-azure-gpt-realt
IMAGE_SOURCE = ""  # 暂不使用图片，留空
# IMAGE_SOURCE = "https://your-oss.aliyuncs.com/test.jpg"
# IMAGE_SOURCE = "/Users/you/test.jpg"

RESPONSE_TIMEOUT    = 30      # 等待回复超时（秒）
AUDIO_RATE          = 24000   # OpenAI Realtime 要求 24kHz PCM16
DEBUG               = True    # True 时打印所有原始事件，排查问题后改为 False

# ── 用量说明 ──────────────────────────────────────────────────
# 官方计费文档：https://developers.openai.com/api/docs/guides/realtime-costs
#
# ▌response.done 用量字段说明
#   每次 response.done 返回的是"本次 Response 的用量"，不是会话累计。
#   官方原文："The tokens used for a Response can be read from the response.done event"
#
#   usage 字段结构：
#     total_tokens                          = input_tokens + output_tokens
#     input_tokens                          发给模型的全部输入（含对话历史）
#       input_token_details.text_tokens     输入中的文字 token
#       input_token_details.audio_tokens    输入中的音频 token
#       input_token_details.image_tokens    输入中的图片 token
#       input_token_details.cached_tokens   ⚠️ input_tokens 的子集（不是额外叠加），
#                                           命中 prompt cache 的部分；
#                                           计费时此部分按缓存价打折
#         cached_tokens_details.text_tokens   缓存中属于文字的部分（OpenAI 原生有）
#         cached_tokens_details.audio_tokens  缓存中属于音频的部分（OpenAI 原生有）
#                                             Azure GA 目前不返回 cached_tokens_details，
#                                             网关会按文字/音频比例估算
#     output_tokens                         本轮生成的 token
#       output_token_details.text_tokens    输出中的文字 token（即音频回复的 transcript，
#                                           选 audio 模式也会产生，属正常计费）
#       output_token_details.audio_tokens   输出中的音频 token
#
#   每次 Response 都会把整个对话历史发给模型，所以越到后面 input_tokens 越大；
#   但前几轮的内容大概率命中 prompt cache，折扣很高（gpt-realtime-2 音频缓存 $0.40/1M，
#   相比音频输入 $32/1M 折扣 98.75%），实际增量成本远小于原始输入价格。
#   官方原文："turns later in the session will be more expensive"
#
#   本脚本做法：每轮打印"本轮用量 + 截至本轮的累计用量"；退出时再打印最终累计用量。
# ─────────────────────────────────────────────────────────────

# 输出模态：1=音频（扬声器播放+transcript）  2=文字（终端打印，不播放）
OUTPUT_MODE         = 1

# 音色（仅 OUTPUT_MODE=1 时有效）
# 可选：alloy / ash / ballad / coral / echo / sage / shimmer / verse / marin
VOICE               = "sage"

INSTRUCTIONS        = "You are a concise assistant. Reply in Chinese. 回答用中文回答"

# ============================================================
# 工具函数
# ============================================================

def load_as_base64(source: str):
    if not source:
        return None
    if source.startswith("http://") or source.startswith("https://"):
        print(f"  下载: {source}")
        with urllib.request.urlopen(source, timeout=30) as r:
            data = r.read()
    else:
        p = Path(source).expanduser()
        if not p.exists():
            print(f"  [警告] 文件不存在: {p}")
            return None
        data = p.read_bytes()
    return base64.b64encode(data).decode()


def image_media_type(source: str) -> str:
    return {".jpg": "image/jpeg", ".jpeg": "image/jpeg",
            ".png": "image/png", ".webp": "image/webp",
            ".gif": "image/gif"}.get(Path(source).suffix.lower(), "image/png")


def record_mic() -> bytes | None:
    """录音直到用户按 Enter，返回 PCM16 raw bytes（无 WAV 头）"""
    try:
        import pyaudio
    except ImportError:
        print("  [错误] 未安装 pyaudio，请运行: pip install pyaudio")
        return None

    p   = pyaudio.PyAudio()
    stm = p.open(format=pyaudio.paInt16, channels=1, rate=AUDIO_RATE,
                 input=True, frames_per_buffer=1024)
    frames, stop = [], threading.Event()

    def _rec():
        while not stop.is_set():
            frames.append(stm.read(1024, exception_on_overflow=False))

    t = threading.Thread(target=_rec, daemon=True)
    t.start()
    print("  🎙️  录音中... 按 Enter 停止")
    input()
    stop.set(); t.join(timeout=1)
    stm.stop_stream(); stm.close(); p.terminate()

    raw = b"".join(frames)
    secs = len(raw) / 2 / AUDIO_RATE
    print(f"  录音完成 {secs:.1f}s  {len(raw)//1024} KB")
    return raw


def fmt_usage(usage: dict) -> str:
    inp = usage.get("input_token_details", {})
    out = usage.get("output_token_details", {})
    cached_total = inp.get("cached_tokens", 0)
    cached_det   = inp.get("cached_tokens_details")  # OpenAI 原生有；Azure GA 无
    if cached_det:
        cached_text  = cached_det.get("text_tokens", 0)
        cached_audio = cached_det.get("audio_tokens", 0)
        cached_src   = "upstream"
    elif cached_total and (inp.get("text_tokens", 0) + inp.get("audio_tokens", 0)) > 0:
        total_in = inp.get("text_tokens", 0) + inp.get("audio_tokens", 0)
        cached_audio = int(cached_total * inp.get("audio_tokens", 0) / total_in)
        cached_text  = cached_total - cached_audio
        cached_src   = "estimated"
    else:
        cached_text = cached_audio = 0
        cached_src  = ""
    cached_line = f"    cached       : {cached_total}"
    if cached_total:
        cached_line += f"  (text={cached_text} audio={cached_audio}"
        if cached_src:
            cached_line += f" [{cached_src}]"
        cached_line += ")"
    return (
        f"  total          : {usage.get('total_tokens', 0)}\n"
        f"  input          : {usage.get('input_tokens', 0)}\n"
        f"    text         : {inp.get('text_tokens', 0)}\n"
        f"    audio        : {inp.get('audio_tokens', 0)}\n"
        f"    image        : {inp.get('image_tokens', 0)}\n"
        f"{cached_line}\n"
        f"  output         : {usage.get('output_tokens', 0)}\n"
        f"    text(transcript): {out.get('text_tokens', 0)}\n"
        f"    audio        : {out.get('audio_tokens', 0)}"
    )


class AudioPlayer:
    """后台线程队列播放 PCM16 音频，不阻塞 asyncio 事件循环"""

    def __init__(self):
        self._q: queue.Queue = queue.Queue()
        self._stop = threading.Event()
        self._p = None
        self._stream = None
        self._thread = None
        self._ok = False

    def start(self):
        try:
            import pyaudio
            self._p = pyaudio.PyAudio()
            self._stream = self._p.open(
                format=pyaudio.paInt16, channels=1,
                rate=AUDIO_RATE, output=True, frames_per_buffer=1024,
            )
            self._ok = True
        except Exception as e:
            print(f"  [播放] 无法打开音频输出: {e}")
            return
        self._stop.clear()
        self._thread = threading.Thread(target=self._worker, daemon=True)
        self._thread.start()

    def _worker(self):
        while not (self._stop.is_set() and self._q.empty()):
            try:
                chunk = self._q.get(timeout=0.05)
                self._stream.write(chunk)
            except queue.Empty:
                continue

    def play(self, pcm: bytes):
        if self._ok:
            self._q.put(pcm)

    def close(self):
        if not self._ok:
            return
        self._stop.set()
        if self._thread:
            self._thread.join(timeout=3)
        if self._stream:
            try:
                self._stream.stop_stream()
                self._stream.close()
            except Exception:
                pass
        if self._p:
            try:
                self._p.terminate()
            except Exception:
                pass
        self._ok = False


def merge_usage(acc: dict, usage: dict):
    for k in ("total_tokens", "input_tokens", "output_tokens"):
        acc[k] = acc.get(k, 0) + usage.get(k, 0)
    for section in ("input_token_details", "output_token_details"):
        a = acc.setdefault(section, {})
        for dk, dv in usage.get(section, {}).items():
            if isinstance(dv, int):
                a[dk] = a.get(dk, 0) + dv
            elif isinstance(dv, dict):
                # 合并嵌套 dict（如 cached_tokens_details）
                cd = a.setdefault(dk, {})
                for ck, cv in dv.items():
                    if isinstance(cv, int):
                        cd[ck] = cd.get(ck, 0) + cv


# ============================================================
# 主逻辑
# ============================================================

async def run():
    url = BASE_URL
    if url.startswith("http://"):
        url = "ws://" + url[7:]
    elif url.startswith("https://"):
        url = "wss://" + url[8:]
    # model 必须作为查询参数传入，否则网关返回 400
    if "model=" not in url:
        sep = "&" if "?" in url else "?"
        url = f"{url}{sep}model={MODEL}"

    # 预加载图片
    image_b64, image_type = None, "image/png"
    if IMAGE_SOURCE:
        print("[准备图片]")
        image_b64 = load_as_base64(IMAGE_SOURCE)
        if image_b64:
            image_type = image_media_type(IMAGE_SOURCE)
            raw_kb = len(image_b64) * 3 // 4 // 1024
            print(f"  {image_type}  {raw_kb} KB  (base64: {len(image_b64)} chars)")
            if len(image_b64) > 1_048_576:
                print(f"  [警告] 图片过大！base64 长度 {len(image_b64)} 超过 API 上限 1,048,576，请压缩到 ~750 KB 以内")
                image_b64 = None
            else:
                print()

    player = AudioPlayer()
    player.start()

    accumulated: dict = {}
    round_n = 0

    print("=" * 56)
    print(f"连接: {url}")
    print(f"模型: {MODEL}")
    print("=" * 56)

    async with websockets.connect(url, additional_headers={
        "Authorization": f"Bearer {API_KEY}",
        "OpenAI-Beta": "realtime=v1",
    }) as ws:
        print("✅ 已连接\n")

        # session.update：开启文字+音频双模态
        output_modalities = ["audio"] if OUTPUT_MODE == 1 else ["text"]
        session_cfg: dict = {
            "type": "realtime",
            "instructions": INSTRUCTIONS,
            "output_modalities": output_modalities,
        }
        if OUTPUT_MODE == 1:
            session_cfg["audio"] = {
                "input":  {"format": {"type": "audio/pcm", "rate": AUDIO_RATE}},
                "output": {"format": {"type": "audio/pcm", "rate": AUDIO_RATE}, "voice": VOICE},
            }
        await ws.send(json.dumps({"type": "session.update", "session": session_cfg}))

        # 等待会话就绪
        async for raw in ws:
            evt = json.loads(raw)
            t = evt.get("type", "")
            if t in ("session.created", "session.updated"):
                print(f"[←] {t}  会话就绪\n")
                break
            if t == "error":
                print(f"[ERROR] {json.dumps(evt.get('error', evt), ensure_ascii=False)}")
                return

        # ───── 交互主循环 ─────
        while True:
            print("─" * 40)

            # 是否附带图片（有图片时询问）
            use_image = False
            if image_b64:
                ans = input("附带图片? [y/n, 默认 y] > ").strip().lower()
                use_image = (ans != "n")

            # 输入模式
            mode = input("[t] 文字  [r] 录音  [q] 退出 > ").strip().lower()
            if mode == "q":
                print("退出")
                break

            # ── 构造 content ──
            content: list[dict] = []

            if use_image:
                content.append({
                    "type": "ms_image",
                    "image": f"data:{image_type};base64,{image_b64}",
                })

            if mode == "r":
                pcm = record_mic()
                if not pcm:
                    continue
                content.append({"type": "input_audio", "audio": base64.b64encode(pcm).decode()})
            else:
                text = input("消息 > ").strip()
                if not text:
                    continue
                content.append({"type": "input_text", "text": text})

            round_n += 1
            has = lambda tp: any(c["type"] == tp for c in content)
            print(f"\n[→] 第{round_n}轮  text={has('input_text')} audio={has('input_audio')} image={has('ms_image')}")

            try:
                await ws.send(json.dumps({
                    "type": "conversation.item.create",
                    "item": {"type": "message", "role": "user", "content": content},
                }))
                await ws.send(json.dumps({"type": "response.create"}))
            except websockets.exceptions.ConnectionClosed as e:
                print(f"\n[连接已断开] 无法发送消息: {e}")
                print("  提示：服务端错误后连接会被关闭，请重新运行脚本")
                break

            # ── 接收本轮回复 ──
            cur_round = round_n
            timed_out = False

            async def recv_round():
                nonlocal timed_out
                async for raw in ws:
                    evt = json.loads(raw)
                    t   = evt.get("type", "")

                    if DEBUG and t not in ("response.audio.delta", "response.output_audio.delta"):
                        print(f"\n[DBG] {json.dumps(evt, ensure_ascii=False)[:300]}")

                    if t in ("response.text.delta", "response.output_text.delta"):
                        print(evt.get("delta", ""), end="", flush=True)
                    elif t in ("response.audio_transcript.delta", "response.output_audio_transcript.delta"):
                        print(evt.get("delta", ""), end="", flush=True)
                    elif t in ("response.audio.delta", "response.output_audio.delta"):
                        audio_b64 = evt.get("audio", "") or evt.get("delta", "")
                        if audio_b64:
                            player.play(base64.b64decode(audio_b64))
                    elif t == "response.done":
                        print()
                        resp   = evt.get("response", evt)  # GA 可能结构不同
                        usage  = resp.get("usage")
                        if usage:
                            merge_usage(accumulated, usage)
                            print(f"\n[用量 第{cur_round}轮]\n{fmt_usage(usage)}")
                            print(f"\n[累计用量（共{cur_round}轮）]\n{fmt_usage(accumulated)}")
                        else:
                            print(f"  [无用量数据] response.done raw: {json.dumps(evt, ensure_ascii=False)[:200]}")
                        return
                    elif t == "error":
                        print(f"\n[ERROR] {json.dumps(evt, ensure_ascii=False)}")
                        return

            try:
                await asyncio.wait_for(recv_round(), timeout=RESPONSE_TIMEOUT)
            except asyncio.TimeoutError:
                print(f"\n[超时] {RESPONSE_TIMEOUT}s 内未收到回复")
            print()

    player.close()

    print("\n" + "=" * 56)
    print(f"共 {round_n} 轮  累计用量:")
    if accumulated:
        print(fmt_usage(accumulated))
    print("=" * 56)


if __name__ == "__main__":
    asyncio.run(run())
