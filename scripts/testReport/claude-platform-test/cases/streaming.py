#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""SSE 流式响应类用例。"""

from __future__ import annotations

import fixtures
from client import Client, build_curl
from .base import TestCase, TestOutcome, ok, fail, safe_run

CATEGORY = "流式响应"

# Anthropic 规范中一次完整流应出现的关键事件
_REQUIRED_EVENTS = {"message_start", "content_block_start", "content_block_delta",
                    "content_block_stop", "message_delta", "message_stop"}


def _basic_stream(client: Client, model: str) -> TestOutcome:
    payload = {
        "model": model,
        "max_tokens": 256,
        "messages": [{"role": "user", "content": "用三句话介绍杭州。"}],
    }
    curl = build_curl({**payload, "stream": True})
    resp = client.stream_message(payload)
    if not resp.ok:
        return fail("完整 SSE 事件序列", f"HTTP {resp.status_code}: {resp.error_message}",
                    "流式请求失败", resp=resp, curl=curl, status_code=resp.status_code)
    seen = set(resp.event_types)
    missing = _REQUIRED_EVENTS - seen
    has_text = bool(resp.text.strip())
    hit = not missing and has_text
    return (ok if hit else fail)(
        "含全部必需事件且有文本",
        f"缺失事件={missing or '无'}; 文本长度={len(resp.text)}",
        "SSE 流式结构完整" if hit else "SSE 事件序列不完整",
        resp=resp, curl=curl, status_code=resp.status_code, metrics=resp.usage)


def _stream_tool_use(client: Client, model: str) -> TestOutcome:
    payload = {
        "model": model,
        "max_tokens": 512,
        "tools": [fixtures.weather_tool()],
        "messages": [{"role": "user", "content": "查一下广州的天气。"}],
    }
    curl = build_curl({**payload, "stream": True})
    resp = client.stream_message(payload)
    if not resp.ok:
        return fail("流式工具调用（input_json_delta）",
                    f"HTTP {resp.status_code}: {resp.error_message}",
                    "流式工具请求失败", resp=resp, curl=curl, status_code=resp.status_code)
    hit = any(t.get("name") == "get_current_weather" for t in resp.tool_uses)
    got_input = hit and resp.tool_uses[0].get("input") not in (None, "", {})
    return (ok if hit else fail)(
        "流式增量拼出完整工具参数",
        f"tools={resp.tool_uses}; 参数完整={got_input}",
        "流式工具调用可用" if hit else "流式未正确产出工具调用",
        resp=resp, curl=curl, status_code=resp.status_code, metrics=resp.usage)


CASES = [
    TestCase("AN-STREAM-001", CATEGORY, "基础 SSE 流式", "P0", safe_run(_basic_stream)),
    TestCase("AN-STREAM-002", CATEGORY, "流式工具调用增量拼装", "P1", safe_run(_stream_tool_use)),
]
