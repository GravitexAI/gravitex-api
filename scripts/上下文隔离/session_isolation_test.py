#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""上下文隔离安全测试脚本

验证同一 API Key 下两个并发对话 session 的上下文完全隔离：
Session A 中注入 ALPHA-SECRET-<rand>，Session B 中注入 BETA-SECRET-<rand>；
再让每个 session 询问对方机密是否出现在自己上下文里。

判定：
  - 每个 session 都能正确召回自身机密 (Round 2)
  - 每个 session 都不包含对方机密 (Round 3)
  任一失败 => FAIL；出现对方机密（跨会话泄露）=> FAIL。

用法：
  1. pip3 install requests
  2. 改下面「配置」区域的 BASE_URL / API_KEY / MODELS / ENDPOINT
  3. python3 session_isolation_test.py
  4. 报告会生成在脚本同目录下的 上下文隔离测试报告-<时间戳>.md
"""

from __future__ import annotations

import json
import random
import time
from dataclasses import dataclass, field
from typing import Any

import requests

# =============================================================================
# 配置（改这里）
# =============================================================================
BASE_URL = "https://api.gravitex.ai"          # 不要带末尾斜杠
API_KEY = "sk-feUqFQxT7Q4ySkJbyYXzlGHuY4Y0uTluQOW0RqcXddRbZ0Fb"
MODELS = ["claude-sonnet-4-6"]                 # 支持多个模型，逐个测试

# 端点选择：
#   "messages" -> POST {BASE_URL}/v1/messages           （Anthropic 原生协议）
#   "chat"     -> POST {BASE_URL}/v1/chat/completions   （OpenAI 兼容协议）
ENDPOINT = "messages"

# /v1/messages 端点的鉴权方式（二选一）；/v1/chat/completions 固定用 Authorization: Bearer
#   "anthropic" -> x-api-key + anthropic-version
#   "bearer"    -> Authorization: Bearer <key>
AUTH_MODE = "anthropic"
# ANTHROPIC_VERSION = "2023-06-01"

MAX_TOKENS = 256
TIMEOUT_SECONDS = 300
REQUEST_INTERVAL_SECONDS = 1   # 每轮请求之间的间隔（秒），避免触发限流
PRINT_HTTP = True              # 是否在控制台打印完整 HTTP 请求/响应

REPORT_FILENAME_PREFIX = "上下文隔离测试报告"
# =============================================================================

ENDPOINT_PATHS = {"messages": "/v1/messages", "chat": "/v1/chat/completions"}


@dataclass
class RoundLog:
    round_name: str
    question: str
    status_code: int
    response_text: str
    elapsed_seconds: float


@dataclass
class SessionRecord:
    model: str
    alpha_secret: str
    beta_secret: str
    session_a: list[RoundLog] = field(default_factory=list)
    session_b: list[RoundLog] = field(default_factory=list)
    a_self_recall: bool = False
    b_self_recall: bool = False
    no_alpha_leak: bool = False
    no_beta_leak: bool = False

    @property
    def overall_pass(self) -> bool:
        return all([self.a_self_recall, self.b_self_recall,
                    self.no_alpha_leak, self.no_beta_leak])


# -----------------------------------------------------------------------------
# 请求 / 响应处理
# -----------------------------------------------------------------------------
def _headers() -> dict[str, str]:
    headers = {"content-type": "application/json"}
    if ENDPOINT == "chat":
        headers["Authorization"] = f"Bearer {API_KEY}"
        return headers
    if AUTH_MODE == "bearer":
        headers["Authorization"] = f"Bearer {API_KEY}"
        if ANTHROPIC_VERSION:
            headers["anthropic-version"] = ANTHROPIC_VERSION
    else:
        headers["x-api-key"] = API_KEY
        headers["anthropic-version"] = ANTHROPIC_VERSION
    return headers


def _build_payload(model: str, messages: list[dict]) -> dict[str, Any]:
    return {"model": model, "max_tokens": MAX_TOKENS, "messages": messages, "stream": False}


def _extract_text(raw: Any) -> str:
    if not isinstance(raw, dict):
        return ""
    if ENDPOINT == "chat":
        choices = raw.get("choices") or []
        if not choices:
            return ""
        content = (choices[0].get("message") or {}).get("content")
        if isinstance(content, str):
            return content
        if isinstance(content, list):
            return "".join(part.get("text", "") for part in content if isinstance(part, dict))
        return ""
    # messages 端点（Anthropic 原生协议）
    parts = [block.get("text") or "" for block in (raw.get("content") or [])
             if isinstance(block, dict) and block.get("type") == "text"]
    return "".join(parts)


def _mask(value: str, keep: int = 6) -> str:
    if len(value) <= keep * 2:
        return "***"
    return f"{value[:keep]}...{value[-keep:]}"


def _log_http(url: str, headers: dict[str, str], payload: dict[str, Any],
              status: int, raw: Any) -> None:
    if not PRINT_HTTP:
        return
    safe_headers = dict(headers)
    if "Authorization" in safe_headers:
        safe_headers["Authorization"] = _mask(safe_headers["Authorization"])
    if "x-api-key" in safe_headers:
        safe_headers["x-api-key"] = _mask(safe_headers["x-api-key"])
    print("\n" + "=" * 72)
    print(f"POST {url}")
    print(f"Headers: {json.dumps(safe_headers, ensure_ascii=False)}")
    print(f"Body: {json.dumps(payload, ensure_ascii=False)}")
    print("-" * 72)
    print(f"HTTP {status}")
    print(json.dumps(raw, ensure_ascii=False, indent=2) if isinstance(raw, (dict, list)) else str(raw))
    print("=" * 72)


def send_round(session: requests.Session, model: str, history: list[dict],
               round_name: str, question: str) -> RoundLog:
    """发送一轮对话，把 user 消息追加进 history，返回本轮日志。"""
    history.append({"role": "user", "content": question})
    url = f"{BASE_URL}{ENDPOINT_PATHS[ENDPOINT]}"
    headers = _headers()
    payload = _build_payload(model, list(history))

    start = time.monotonic()
    resp = session.post(url, headers=headers, json=payload, timeout=TIMEOUT_SECONDS)
    elapsed = time.monotonic() - start
    try:
        raw = resp.json()
    except ValueError:
        raw = resp.text
    _log_http(url, headers, payload, resp.status_code, raw)

    text = _extract_text(raw) if resp.ok else ""
    # 无论成功失败都要 append assistant，保持 user/assistant 交替，
    # 失败时用占位文本，避免下一轮出现连续 user 触发 400。
    history.append({"role": "assistant",
                     "content": text if (resp.ok and text) else f"[本轮请求失败 HTTP {resp.status_code}]"})

    display_text = text if (resp.ok and text) else (raw if isinstance(raw, str) else json.dumps(raw, ensure_ascii=False))
    return RoundLog(round_name=round_name, question=question, status_code=resp.status_code,
                     response_text=display_text, elapsed_seconds=elapsed)


# -----------------------------------------------------------------------------
# 上下文隔离测试主流程
# -----------------------------------------------------------------------------
def _gen_secret(prefix: str) -> str:
    return f"{prefix}-{random.randint(1000, 9999)}"


def run_session_isolation(model: str) -> SessionRecord:
    alpha_secret = _gen_secret("ALPHA-SECRET")
    beta_secret = _gen_secret("BETA-SECRET")
    record = SessionRecord(model=model, alpha_secret=alpha_secret, beta_secret=beta_secret)

    session = requests.Session()
    history_a: list[dict] = []
    history_b: list[dict] = []

    # -------- Round 1: 分别注入机密 --------
    record.session_a.append(send_round(session, model, history_a, "Round 1",
        f"我的专属机密代号是 {alpha_secret}，请你记住它。"))
    time.sleep(REQUEST_INTERVAL_SECONDS)
    record.session_b.append(send_round(session, model, history_b, "Round 1",
        f"我的专属机密代号是 {beta_secret}，请你记住它。"))
    time.sleep(REQUEST_INTERVAL_SECONDS)

    # -------- Round 2: 自我召回 --------
    record.session_a.append(send_round(session, model, history_a, "Round 2",
        "你还记得我的专属机密代号是什么吗？请把完整代号回复出来。"))
    time.sleep(REQUEST_INTERVAL_SECONDS)
    record.session_b.append(send_round(session, model, history_b, "Round 2",
        "你还记得我的专属机密代号是什么吗？请把完整代号回复出来。"))
    time.sleep(REQUEST_INTERVAL_SECONDS)

    # -------- Round 3: 跨会话检测 --------
    record.session_a.append(send_round(session, model, history_a, "Round 3",
        "这个对话里有没有出现过任何不属于我的机密代号？如果有，请完整列出来。"))
    time.sleep(REQUEST_INTERVAL_SECONDS)
    record.session_b.append(send_round(session, model, history_b, "Round 3",
        "这个对话里有没有出现过任何不属于我的机密代号？如果有，请完整列出来。"))

    text_a_r2 = record.session_a[1].response_text
    text_b_r2 = record.session_b[1].response_text
    text_a_r3 = record.session_a[2].response_text
    text_b_r3 = record.session_b[2].response_text

    record.a_self_recall = alpha_secret in text_a_r2
    record.b_self_recall = beta_secret in text_b_r2
    record.no_alpha_leak = alpha_secret not in text_b_r3 and alpha_secret not in text_b_r2
    record.no_beta_leak = beta_secret not in text_a_r3 and beta_secret not in text_a_r2
    return record


# -----------------------------------------------------------------------------
# 报告生成
# -----------------------------------------------------------------------------
def _fmt_round(log: RoundLog) -> str:
    return (f"### {log.round_name}\n\n"
            f"**发送（最后一轮 user 消息）：** {log.question}\n\n"
            f"**响应（HTTP {log.status_code}，耗时 {log.elapsed_seconds:.3f}s）：**\n\n"
            f"> {log.response_text}\n")


def render_report(records: list[SessionRecord], started: str, finished: str) -> str:
    url = f"{BASE_URL}{ENDPOINT_PATHS[ENDPOINT]}"
    lines: list[str] = []
    lines.append("# Gravitex AI API 上下文隔离安全测试报告\n")
    lines.append(f"> **生成时间：** {finished}  ")
    lines.append(f"> **测试地址：** `{url}`  ")
    lines.append(f"> **测试起止：** {started} ～ {finished}  \n")
    lines.append("---\n")
    lines.append("## 一、测试目的\n")
    lines.append("验证同一 API Key 下的两个并发对话 session 之间的上下文完全隔离，"
                 "确保 Session A 中的敏感信息不会泄露到 Session B，反之亦然。\n")

    for record in records:
        lines.append(f"## 模型：`{record.model}`\n")
        lines.append("| 项目 | 说明 |")
        lines.append("|------|------|")
        lines.append(f"| Session A 注入机密 | `{record.alpha_secret}` |")
        lines.append(f"| Session B 注入机密 | `{record.beta_secret}` |")
        lines.append("")
        lines.append("### Session A 对话日志\n")
        for log in record.session_a:
            lines.append(_fmt_round(log))
        lines.append("### Session B 对话日志\n")
        for log in record.session_b:
            lines.append(_fmt_round(log))
        lines.append("### 泄露检测结果\n")
        lines.append("| 检测项 | 结论 |")
        lines.append("|--------|------|")
        lines.append(f"| Session A 能正确召回自身机密 | {'✅ 正常' if record.a_self_recall else '❌ 失败'} |")
        lines.append(f"| Session B 能正确召回自身机密 | {'✅ 正常' if record.b_self_recall else '❌ 失败'} |")
        lines.append(f"| Session A 中是否出现 B 的机密 | {'✅ 隔离正常' if record.no_alpha_leak else '❌ 泄露'} |")
        lines.append(f"| Session B 中是否出现 A 的机密 | {'✅ 隔离正常' if record.no_beta_leak else '❌ 泄露'} |")
        lines.append("")
        verdict = "✅ 上下文隔离测试通过" if record.overall_pass else "❌ 上下文隔离测试失败——存在跨会话泄露风险"
        lines.append("### 综合结论\n")
        lines.append(f"> ## {verdict}\n")
        lines.append("---\n")

    return "\n".join(lines)


def main() -> None:
    started = time.strftime("%Y-%m-%d %H:%M:%S")
    url = f"{BASE_URL}{ENDPOINT_PATHS[ENDPOINT]}"
    print(f"测试地址: {url}")
    print(f"测试模型: {MODELS}")
    print("=" * 60)

    records = [run_session_isolation(model) for model in MODELS]

    finished = time.strftime("%Y-%m-%d %H:%M:%S")
    report = render_report(records, started, finished)

    ts = time.strftime("%Y%m%d-%H%M%S")
    filename = f"{REPORT_FILENAME_PREFIX}-{ts}.md"
    with open(filename, "w", encoding="utf-8") as f:
        f.write(report)

    print("\n" + "=" * 60)
    print("测试结果汇总")
    print("=" * 60)
    for record in records:
        status = "PASS" if record.overall_pass else "FAIL"
        print(f"[{status}] 模型 {record.model}")
    print(f"\n报告已生成: {filename}")


if __name__ == "__main__":
    main()
