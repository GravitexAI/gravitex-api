#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""基础对话类用例。"""

from __future__ import annotations

import re

from client import Client, build_curl
from .base import TestCase, TestOutcome, ok, fail, safe_run

CATEGORY = "基础对话"


def _basic_chat(client: Client, model: str) -> TestOutcome:
    payload = {
        "model": model,
        "max_tokens": 256,
        "messages": [{"role": "user", "content": "用一句话介绍你自己。"}],
    }
    curl = build_curl(payload)
    resp = client.create_message(payload)
    if not resp.ok:
        return fail("200 且返回文本", f"HTTP {resp.status_code}: {resp.error_message}",
                    "非流式基础对话请求失败", resp=resp, curl=curl, status_code=resp.status_code)
    if not resp.text.strip():
        return fail("非空文本回复", "空回复", "返回内容为空", resp=resp, curl=curl,
                    status_code=resp.status_code)
    return ok("200 且返回非空文本", resp.text[:200],
              f"stop_reason={resp.stop_reason}", resp=resp, curl=curl,
              status_code=resp.status_code, metrics=resp.usage)


def _system_prompt(client: Client, model: str) -> TestOutcome:
    payload = {
        "model": model,
        "max_tokens": 64,
        "system": "无论用户说什么，你都只能回答四个字：小c收到。",
        "messages": [{"role": "user", "content": "今天天气怎么样？"}],
    }
    curl = build_curl(payload)
    resp = client.create_message(payload)
    if not resp.ok:
        return fail("system 指令生效", f"HTTP {resp.status_code}: {resp.error_message}",
                    "请求失败", resp=resp, curl=curl, status_code=resp.status_code)
    hit = "小c收到" in resp.text
    return (ok if hit else fail)(
        "回复含『小c收到』", resp.text[:100],
        "system 指令生效" if hit else "system 指令未被遵守",
        resp=resp, curl=curl, status_code=resp.status_code, metrics=resp.usage)


def _multi_turn(client: Client, model: str) -> TestOutcome:
    payload = {
        "model": model,
        "max_tokens": 128,
        "messages": [
            {"role": "user", "content": "我最喜欢的数字是 42，请记住。"},
            {"role": "assistant", "content": "好的，你最喜欢的数字是 42。"},
            {"role": "user", "content": "我刚才说的数字是多少？只回答数字。"},
        ],
    }
    curl = build_curl(payload)
    resp = client.create_message(payload)
    if not resp.ok:
        return fail("上下文记忆正确", f"HTTP {resp.status_code}: {resp.error_message}",
                    "请求失败", resp=resp, curl=curl, status_code=resp.status_code)
    hit = "42" in resp.text
    return (ok if hit else fail)(
        "回复含 42", resp.text[:100],
        "多轮上下文正确" if hit else "未正确记住上下文",
        resp=resp, curl=curl, status_code=resp.status_code, metrics=resp.usage)


def _stop_sequence(client: Client, model: str) -> TestOutcome:
    payload = {
        "model": model,
        "max_tokens": 256,
        "stop_sequences": ["END"],
        "messages": [{"role": "user", "content": "请依次输出：A B C END D E F"}],
    }
    curl = build_curl(payload)
    resp = client.create_message(payload)
    if not resp.ok:
        return fail("stop_reason=stop_sequence", f"HTTP {resp.status_code}: {resp.error_message}",
                    "请求失败", resp=resp, curl=curl, status_code=resp.status_code)
    tokens = re.findall(r"\b[A-F]\b", resp.text)
    stopped_before_d = "A" in tokens and "B" in tokens and "C" in tokens and not any(
        token in tokens for token in ("D", "E", "F")
    )
    hit = resp.stop_reason == "stop_sequence" or stopped_before_d
    return (ok if hit else fail)(
        "stop_reason=stop_sequence", f"stop_reason={resp.stop_reason}; 文本={resp.text[:60]!r}",
        "停止序列生效或已在 END 前停止" if hit else "停止序列未生效",
        resp=resp, curl=curl, status_code=resp.status_code, metrics=resp.usage)


def _max_tokens_truncate(client: Client, model: str) -> TestOutcome:
    payload = {
        "model": model,
        "max_tokens": 16,
        "messages": [{"role": "user", "content": "写一篇 500 字的散文。"}],
    }
    curl = build_curl(payload)
    resp = client.create_message(payload)
    if not resp.ok:
        return fail("stop_reason=max_tokens", f"HTTP {resp.status_code}: {resp.error_message}",
                    "请求失败", resp=resp, curl=curl, status_code=resp.status_code)
    hit = resp.stop_reason == "max_tokens"
    return (ok if hit else fail)(
        "stop_reason=max_tokens", f"stop_reason={resp.stop_reason}",
        "max_tokens 截断生效" if hit else "未按 max_tokens 截断",
        resp=resp, curl=curl, status_code=resp.status_code, metrics=resp.usage)


CASES = [
    TestCase("AN-BASIC-001", CATEGORY, "非流式基础对话", "P0", safe_run(_basic_chat)),
    TestCase("AN-BASIC-002", CATEGORY, "system 系统提示词生效", "P1", safe_run(_system_prompt)),
    TestCase("AN-BASIC-003", CATEGORY, "多轮对话上下文记忆", "P0", safe_run(_multi_turn)),
    TestCase("AN-BASIC-004", CATEGORY, "stop_sequences 停止序列", "P2", safe_run(_stop_sequence)),
    TestCase("AN-BASIC-005", CATEGORY, "max_tokens 截断", "P2", safe_run(_max_tokens_truncate)),
]
