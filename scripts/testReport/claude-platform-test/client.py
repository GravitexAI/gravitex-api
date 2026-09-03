#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
Anthropic 原生协议客户端封装
============================

只对接一个端点：POST {BASE_URL}/v1/messages（普通 + SSE 流式）。

按 config.AUTH_MODE 组装鉴权头。

并发安全：requests.Session 不保证线程安全，这里按线程各持一个 Session；
HTTP 日志把「请求 + 响应」拼成一整块、加锁一次性打印，避免多线程交错串行。
"""

from __future__ import annotations

import json
import threading
import time
from dataclasses import dataclass, field
from typing import Any

import requests

import config


# -----------------------------------------------------------------------------
# 响应数据结构
# -----------------------------------------------------------------------------
@dataclass
class MessageResponse:
    """普通（非流式）响应的归一化结果。"""

    status_code: int
    ok: bool
    response_id: str | None = None
    raw: Any = None                       # 原始 JSON（dict）或文本
    text: str = ""                        # 拼接后的文本内容
    thinking: str = ""                    # 拼接后的思考内容
    tool_uses: list[dict[str, Any]] = field(default_factory=list)
    server_tool_uses: list[dict[str, Any]] = field(default_factory=list)
    server_tool_results: list[dict[str, Any]] = field(default_factory=list)
    stop_reason: str | None = None
    usage: dict[str, Any] = field(default_factory=dict)
    error_type: str | None = None
    error_message: str | None = None


@dataclass
class StreamResponse:
    """流式响应的归一化结果。"""

    status_code: int
    ok: bool
    response_id: str | None = None
    text: str = ""
    thinking: str = ""
    tool_uses: list[dict[str, Any]] = field(default_factory=list)
    server_tool_uses: list[dict[str, Any]] = field(default_factory=list)
    server_tool_results: list[dict[str, Any]] = field(default_factory=list)
    stop_reason: str | None = None
    usage: dict[str, Any] = field(default_factory=dict)
    event_types: list[str] = field(default_factory=list)   # 收到的事件类型序列
    raw_lines: list[str] = field(default_factory=list)
    # ---- 流式耗时（秒）：延迟分位统计要的是首字延迟，不是总耗时 ----
    first_event_seconds: float | None = None    # 收到第一个 SSE 事件（message_start）
    first_text_seconds: float | None = None     # 收到第一个文本增量（用户看到"开始出字"）
    total_seconds: float | None = None          # 整条流读完
    error_type: str | None = None
    error_message: str | None = None


# -----------------------------------------------------------------------------
# 客户端
# -----------------------------------------------------------------------------
class Client:
    def __init__(self) -> None:
        self.base_url = config.BASE_URL
        self._thread_state = threading.local()

    @property
    def session(self) -> requests.Session:
        """每个线程一个 Session：requests.Session 不是线程安全的。"""
        session = getattr(self._thread_state, "session", None)
        if session is None:
            session = requests.Session()
            self._thread_state.session = session
        return session

    # ---- 鉴权头 ----------------------------------------------------------
    def auth_headers(self) -> dict[str, str]:
        headers = {"content-type": "application/json"}
        if config.AUTH_MODE == "bearer":
            headers["Authorization"] = f"Bearer {config.API_KEY}"
            if config.ANTHROPIC_VERSION:
                headers["anthropic-version"] = config.ANTHROPIC_VERSION
        else:  # 默认 anthropic 原生
            headers["x-api-key"] = config.API_KEY
            headers["anthropic-version"] = config.ANTHROPIC_VERSION
        if config.ANTHROPIC_BETA:
            headers["anthropic-beta"] = config.ANTHROPIC_BETA
        return headers

    # ---- /v1/messages 普通请求 -------------------------------------------
    def create_message(self, payload: dict[str, Any]) -> MessageResponse:
        payload = dict(payload)
        payload["stream"] = False
        url = f"{self.base_url}/v1/messages"
        headers = self.auth_headers()
        resp = self.session.post(
            url, headers=headers, json=payload, timeout=config.TIMEOUT_SECONDS
        )
        try:
            raw = resp.json()
        except ValueError:
            raw = resp.text
        _log_exchange(url, headers, payload, resp.status_code, raw)

        result = MessageResponse(status_code=resp.status_code, ok=resp.ok, raw=raw)
        if isinstance(raw, dict):
            if raw.get("type") == "error" or "error" in raw:
                err = raw.get("error") or {}
                result.error_type = err.get("type")
                result.error_message = err.get("message")
            _fill_from_message(result, raw)
        return result

    # ---- /v1/messages 流式请求 -------------------------------------------
    def stream_message(self, payload: dict[str, Any]) -> StreamResponse:
        payload = dict(payload)
        payload["stream"] = True
        url = f"{self.base_url}/v1/messages"
        headers = self.auth_headers()
        started = time.monotonic()
        resp = self.session.post(
            url,
            headers=headers,
            json=payload,
            timeout=config.TIMEOUT_SECONDS,
            stream=True,
        )
        out = _parse_sse(resp, started)
        _log_stream_exchange(url, headers, payload, out)
        return out


# -----------------------------------------------------------------------------
# 解析辅助
# -----------------------------------------------------------------------------
def _fill_from_message(result: MessageResponse, msg: dict[str, Any]) -> None:
    result.response_id = msg.get("id")
    text_parts: list[str] = []
    thinking_parts: list[str] = []
    tool_uses: list[dict[str, Any]] = []
    server_tool_uses: list[dict[str, Any]] = []
    server_tool_results: list[dict[str, Any]] = []
    for block in msg.get("content") or []:
        if not isinstance(block, dict):
            continue
        btype = block.get("type")
        if btype == "text":
            text_parts.append(block.get("text") or "")
        elif btype == "thinking":
            thinking_parts.append(block.get("thinking") or "")
        elif btype == "tool_use":
            tool_uses.append(
                {
                    "id": block.get("id"),
                    "name": block.get("name"),
                    "input": block.get("input"),
                }
            )
        elif btype == "server_tool_use":
            server_tool_uses.append({
                "type": btype,
                "id": block.get("id"),
                "name": block.get("name"),
                "input": block.get("input"),
            })
        elif btype == "web_search_tool_result":
            server_tool_results.append(block)
    result.text = "".join(text_parts)
    result.thinking = "".join(thinking_parts)
    result.tool_uses = tool_uses
    result.server_tool_uses = server_tool_uses
    result.server_tool_results = server_tool_results
    result.stop_reason = msg.get("stop_reason")
    result.usage = msg.get("usage") or {}


def _parse_sse(resp: requests.Response, started: float | None = None) -> StreamResponse:
    # text/event-stream 通常不带 charset，requests 会按 ISO-8859-1 解码，
    # 导致中文变成 'å¹¿å·\x9e' 这种乱码。Anthropic SSE 固定 UTF-8，这里强制指定。
    resp.encoding = "utf-8"
    out = StreamResponse(status_code=resp.status_code, ok=resp.ok)
    if not resp.ok:
        try:
            body = resp.json()
            err = (body or {}).get("error") or {}
            out.error_type = err.get("type")
            out.error_message = err.get("message")
            out.raw_lines.append(json.dumps(body, ensure_ascii=False))
        except ValueError:
            out.error_message = resp.text
            out.raw_lines.append(resp.text)
        return out

    text_parts: list[str] = []
    thinking_parts: list[str] = []
    tool_uses: list[dict[str, Any]] = []
    server_tool_uses: list[dict[str, Any]] = []
    server_tool_results: list[dict[str, Any]] = []
    tool_input_buffers: dict[int, str] = {}

    for raw_line in resp.iter_lines(decode_unicode=True):
        if raw_line is None or raw_line == "":
            continue
        if started is not None and out.first_event_seconds is None:
            out.first_event_seconds = time.monotonic() - started
        out.raw_lines.append(raw_line)
        if not raw_line.startswith("data:"):
            continue
        data = raw_line[5:].strip()
        if data == "[DONE]":
            break
        try:
            event = json.loads(data)
        except json.JSONDecodeError:
            continue
        etype = event.get("type")
        if etype:
            out.event_types.append(etype)

        if etype == "message_start":
            message = event.get("message") or {}
            out.response_id = message.get("id")
            usage = message.get("usage")
            if usage:
                out.usage = usage
        elif etype == "content_block_start":
            block = event.get("content_block") or {}
            if block.get("type") == "tool_use":
                idx = event.get("index", len(tool_uses))
                tool_uses.append(
                    {"id": block.get("id"), "name": block.get("name"), "input": None}
                )
                tool_input_buffers[idx] = ""
            elif block.get("type") == "server_tool_use":
                server_tool_uses.append({
                    "type": "server_tool_use",
                    "id": block.get("id"),
                    "name": block.get("name"),
                    "input": block.get("input"),
                })
            elif block.get("type") == "web_search_tool_result":
                server_tool_results.append(block)
        elif etype == "content_block_delta":
            delta = event.get("delta") or {}
            dtype = delta.get("type")
            if dtype == "text_delta":
                if started is not None and out.first_text_seconds is None:
                    out.first_text_seconds = time.monotonic() - started
                text_parts.append(delta.get("text") or "")
            elif dtype == "thinking_delta":
                thinking_parts.append(delta.get("thinking") or "")
            elif dtype == "input_json_delta":
                idx = event.get("index", 0)
                tool_input_buffers[idx] = tool_input_buffers.get(idx, "") + (
                    delta.get("partial_json") or ""
                )
        elif etype == "message_delta":
            out.stop_reason = (event.get("delta") or {}).get("stop_reason") or out.stop_reason
            if event.get("usage"):
                out.usage = {**out.usage, **event["usage"]}

    # 回填流式工具调用参数
    for idx, buf in tool_input_buffers.items():
        if idx < len(tool_uses) and buf:
            try:
                tool_uses[idx]["input"] = json.loads(buf)
            except json.JSONDecodeError:
                tool_uses[idx]["input"] = buf

    out.text = "".join(text_parts)
    out.thinking = "".join(thinking_parts)
    out.tool_uses = tool_uses
    out.server_tool_uses = server_tool_uses
    out.server_tool_results = server_tool_results
    if started is not None:
        out.total_seconds = time.monotonic() - started
    return out


# -----------------------------------------------------------------------------
# curl 样例生成（写进报告"调用样例"列）
# -----------------------------------------------------------------------------
def build_curl(payload: dict[str, Any], endpoint: str = "/v1/messages") -> str:
    """生成写进报告的 curl 样例。

    地址和鉴权方式取 config.report_base_url() / report_auth_mode()——把别人家的
    资源接到自己平台上测时，实际请求走 BASE_URL/AUTH_MODE，但报告里展示资源方的
    原始地址。留空这两项时二者一致。
    """
    url = f"{config.report_base_url()}{endpoint}"
    lines = [f"curl -X POST {url} \\"]
    if config.report_auth_mode() == "bearer":
        lines.append('  -H "Authorization: Bearer $API_KEY" \\')
        if config.ANTHROPIC_VERSION:
            lines.append(f'  -H "anthropic-version: {config.ANTHROPIC_VERSION}" \\')
    else:
        lines.append('  -H "x-api-key: $API_KEY" \\')
        lines.append(f'  -H "anthropic-version: {config.ANTHROPIC_VERSION}" \\')
    if config.ANTHROPIC_BETA:
        lines.append(f'  -H "anthropic-beta: {config.ANTHROPIC_BETA}" \\')
    lines.append('  -H "content-type: application/json" \\')
    body = json.dumps(payload, ensure_ascii=False, indent=2)
    lines.append(f"  -d '{body}'")
    return "\n".join(lines)


# -----------------------------------------------------------------------------
# 控制台日志
# -----------------------------------------------------------------------------
def _mask(value: str, keep: int = 6) -> str:
    if len(value) <= keep * 2:
        return "***"
    return f"{value[:keep]}...{value[-keep:]}"


def _sanitize_headers(headers: dict[str, str]) -> dict[str, str]:
    out = dict(headers)
    if "Authorization" in out and out["Authorization"].startswith("Bearer "):
        out["Authorization"] = f"Bearer {_mask(out['Authorization'][7:])}"
    if "x-api-key" in out:
        out["x-api-key"] = _mask(out["x-api-key"])
    return out


def _truncate(text: str, limit: int = 2000) -> str:
    if len(text) <= limit:
        return text
    return f"<截断 len={len(text)}> {text[: limit // 2]} ... {text[-limit // 4:]}"


_PRINT_LOCK = threading.Lock()


def _log_exchange(url: str, headers: dict[str, str], payload: dict[str, Any],
                  status: int, raw: Any) -> None:
    """请求 + 响应拼成一块，一次 print 输出（加锁，避免多线程交错）。"""
    if not config.PRINT_HTTP:
        return
    block = _format_request(url, headers, payload) + "\n" + _format_response(status, raw)
    with _PRINT_LOCK:
        print(block, flush=True)


def _log_stream_exchange(url: str, headers: dict[str, str], payload: dict[str, Any],
                         out: StreamResponse) -> None:
    """流式请求 + 事件摘要拼成一块输出。"""
    if not config.PRINT_HTTP:
        return
    block = _format_request(url, headers, payload) + "\n" + _format_stream(out)
    with _PRINT_LOCK:
        print(block, flush=True)


def _format_request(url: str, headers: dict[str, str], payload: dict[str, Any]) -> str:
    thread = threading.current_thread().name
    return "\n".join([
        "",
        "=" * 72,
        f"HTTP 请求  [{payload.get('model', '-')} | {thread}]",
        "=" * 72,
        f"URL: {url}",
        "Headers:",
        json.dumps(_sanitize_headers(headers), ensure_ascii=False, indent=2),
        "Body:",
        _truncate(json.dumps(payload, ensure_ascii=False, indent=2)),
    ])


def _format_response(status: int, raw: Any) -> str:
    body = (json.dumps(raw, ensure_ascii=False, indent=2)
            if isinstance(raw, (dict, list)) else str(raw))
    return "\n".join([
        "-" * 72,
        f"HTTP 响应  status={status}",
        _truncate(body),
        "=" * 72,
        "",
    ])


def _format_stream(out: StreamResponse) -> str:
    lines = [
        "-" * 72,
        f"HTTP 流式响应  status={out.status_code}",
        f"事件序列: {out.event_types}",
        f"文本: {_truncate(out.text, 500)}",
    ]
    if out.thinking:
        lines.append(f"思考: {_truncate(out.thinking, 500)}")
    if out.tool_uses:
        lines.append(f"工具调用: {json.dumps(out.tool_uses, ensure_ascii=False)}")
    lines.append(f"stop_reason={out.stop_reason}  usage={out.usage}")
    lines.append("=" * 72)
    lines.append("")
    return "\n".join(lines)
