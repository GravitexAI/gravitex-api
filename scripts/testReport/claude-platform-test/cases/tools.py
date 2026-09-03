#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""工具调用（Tool Use）类用例。"""

from __future__ import annotations

import fixtures
from client import Client, build_curl
from .base import TestCase, TestOutcome, ok, fail, safe_run

CATEGORY = "工具调用"


def _tool_auto(client: Client, model: str) -> TestOutcome:
    payload = {
        "model": model,
        "max_tokens": 512,
        "tools": [fixtures.weather_tool()],
        "messages": [{"role": "user", "content": "北京现在天气怎么样？"}],
    }
    curl = build_curl(payload)
    resp = client.create_message(payload)
    if not resp.ok:
        return fail("触发 tool_use", f"HTTP {resp.status_code}: {resp.error_message}",
                    "工具请求失败", resp=resp, curl=curl, status_code=resp.status_code)
    hit = resp.stop_reason == "tool_use" and any(
        t.get("name") == "get_current_weather" for t in resp.tool_uses)
    return (ok if hit else fail)(
        "stop_reason=tool_use 且调用 get_current_weather",
        f"stop_reason={resp.stop_reason}; tools={resp.tool_uses}",
        "自动工具选择正确" if hit else "未正确触发工具调用",
        resp=resp, curl=curl, status_code=resp.status_code, metrics=resp.usage)


def _tool_forced(client: Client, model: str) -> TestOutcome:
    payload = {
        "model": model,
        "max_tokens": 512,
        "tools": [fixtures.weather_tool()],
        "tool_choice": {"type": "tool", "name": "get_current_weather"},
        "messages": [{"role": "user", "content": "随便聊聊天吧。"}],
    }
    curl = build_curl(payload)
    resp = client.create_message(payload)
    if not resp.ok:
        return fail("强制调用指定工具", f"HTTP {resp.status_code}: {resp.error_message}",
                    "请求失败", resp=resp, curl=curl, status_code=resp.status_code)
    hit = any(t.get("name") == "get_current_weather" for t in resp.tool_uses)
    return (ok if hit else fail)(
        "即使闲聊也强制调用工具", f"tools={resp.tool_uses}; stop_reason={resp.stop_reason}",
        "tool_choice=tool 强制生效" if hit else "强制工具选择未生效",
        resp=resp, curl=curl, status_code=resp.status_code, metrics=resp.usage)


def _tool_result_loop(client: Client, model: str) -> TestOutcome:
    """完整工具回路：模型请求工具 -> 回传 tool_result -> 得到最终答案。"""
    first = {
        "model": model,
        "max_tokens": 512,
        "tools": [fixtures.weather_tool()],
        "messages": [{"role": "user", "content": "上海现在多少度？"}],
    }
    curl = build_curl(first)
    r1 = client.create_message(first)
    if not r1.ok or not r1.tool_uses:
        return fail("完成工具回路", f"首轮未产生工具调用: {r1.tool_uses}",
                    "首轮工具调用失败", resp=r1, curl=curl, status_code=r1.status_code)
    tu = r1.tool_uses[0]
    # 组装 assistant 的 tool_use 块 + user 的 tool_result 块
    assistant_content = [{"type": "tool_use", "id": tu["id"], "name": tu["name"],
                          "input": tu.get("input") or {}}]
    second = {
        "model": model,
        "max_tokens": 256,
        "tools": [fixtures.weather_tool()],
        "messages": [
            {"role": "user", "content": "上海现在多少度？"},
            {"role": "assistant", "content": assistant_content},
            {"role": "user", "content": [
                {"type": "tool_result", "tool_use_id": tu["id"],
                 "content": "上海当前 26 摄氏度，多云。"}]},
        ],
    }
    r2 = client.create_message(second)
    if not r2.ok:
        return fail("完成工具回路", f"HTTP {r2.status_code}: {r2.error_message}",
                    "二轮请求失败", resp=r2, curl=curl, status_code=r2.status_code)
    hit = "26" in r2.text
    return (ok if hit else fail)(
        "最终回答含工具返回值 26", r2.text[:120],
        "工具回路闭环成功" if hit else "未采用工具返回结果",
        resp=r2, curl=curl, status_code=r2.status_code, metrics=r2.usage)


def _parallel_tools(client: Client, model: str) -> TestOutcome:
    payload = {
        "model": model,
        "max_tokens": 512,
        "tools": [fixtures.weather_tool(), fixtures.time_tool()],
        "messages": [{"role": "user", "content": "现在几点了？另外北京天气怎么样？"}],
    }
    curl = build_curl(payload)
    resp = client.create_message(payload)
    if not resp.ok:
        return fail("并行调用两个工具", f"HTTP {resp.status_code}: {resp.error_message}",
                    "请求失败", resp=resp, curl=curl, status_code=resp.status_code)
    names = {t.get("name") for t in resp.tool_uses}
    count = len(resp.tool_uses)
    hit = count >= 2
    return (ok if hit else fail)(
        "一次返回≥2个 tool_use", f"工具数={count}; names={names}",
        "并行工具调用成功" if hit else "未并行返回多个工具（模型策略差异）",
        resp=resp, curl=curl, status_code=resp.status_code, metrics=resp.usage)


CASES = [
    TestCase("AN-TOOL-001", CATEGORY, "自动工具选择", "P0", safe_run(_tool_auto)),
    TestCase("AN-TOOL-002", CATEGORY, "强制指定工具 tool_choice", "P1", safe_run(_tool_forced)),
    TestCase("AN-TOOL-003", CATEGORY, "工具结果回传闭环", "P0", safe_run(_tool_result_loop)),
    TestCase("AN-TOOL-004", CATEGORY, "并行工具调用", "P2", safe_run(_parallel_tools)),
]
