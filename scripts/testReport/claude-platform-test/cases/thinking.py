#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""扩展思考（Extended Thinking）类用例。

Anthropic 官方规则（2026-07 现行版本）：
  - **adaptive-only**: Opus 4.7/4.8, Sonnet 5, Fable/Mythos 5
      —— 只支持 {"thinking": {"type": "adaptive"}}，manual 会 400
      —— thinking.display 默认 "omitted"，需显式设 "summarized" 才拿得到思考文本
      —— 支持 output_config.effort ∈ {max, xhigh, high, medium, low}
  - **adaptive + manual(已废弃)**: Opus 4.6, Sonnet 4.6
  - **manual-only**: Haiku 全系列、Sonnet 4.5 及更老

output_config.effort（官方 GA，无需 beta 头）：
  - 取值 low / medium / high / xhigh / max，默认 high
  - 本脚本统一用 max：思考能力只需要在最高档验证"有没有返回思考内容"，
    低档位官方允许模型自行决定不思考，逐档测只是在量 token 消耗，会造成误判
  - manual-only 模型（Haiku 4.5 等）不支持 effort，传了会报错，本脚本自动跳过

脚本按模型自动挑选参数格式。
"""

from __future__ import annotations

import fixtures
import config
from client import Client, build_curl
from .base import TestCase, TestOutcome, ok, fail, safe_run

CATEGORY = "扩展思考"


def _supports_adaptive(model: str) -> bool:
    return config.thinking_mode(model) == "adaptive"


# 思考类用例统一使用最高档 effort，只验证"能不能返回思考内容"
DEFAULT_EFFORT = "max"


def _thinking_config(model: str, budget_tokens: int = 4000,
                     effort: str | None = DEFAULT_EFFORT) -> dict:
    """按模型返回合适的 thinking + output_config 片段。

    - adaptive 系列: adaptive + display=summarized + output_config.effort（统一 max）
    - manual-only 模型: enabled + budget_tokens，且不带 output_config（会报错）
    """
    if _supports_adaptive(model):
        cfg: dict = {"thinking": {"type": "adaptive", "display": "summarized"}}
        if effort:
            cfg["output_config"] = {"effort": effort}
        return cfg
    return {"thinking": {"type": "enabled", "budget_tokens": budget_tokens}}


def _thinking_enabled(client: Client, model: str) -> TestOutcome:
    cfg = _thinking_config(model, budget_tokens=4000)
    payload = {
        "model": model,
        "max_tokens": 8000,
        **cfg,
        "messages": [{"role": "user", "content": fixtures.REASONING_PROBLEMS["cubic"]}],
    }
    curl = build_curl(payload)
    resp = client.create_message(payload)
    if not resp.ok:
        return fail("返回 thinking 内容块", f"HTTP {resp.status_code}: {resp.error_message}",
                    "扩展思考请求失败", resp=resp, curl=curl, status_code=resp.status_code)
    hit = bool(resp.thinking.strip())
    return (ok if hit else fail)(
        "响应含非空 thinking 块", f"thinking长度={len(resp.thinking)}; 答案={resp.text[:80]!r}",
        "扩展思考生效" if hit else "未返回 thinking 内容",
        resp=resp, curl=curl, status_code=resp.status_code, metrics=resp.usage)


def _thinking_stream(client: Client, model: str) -> TestOutcome:
    cfg = _thinking_config(model, budget_tokens=4000)
    payload = {
        "model": model,
        "max_tokens": 8000,
        **cfg,
        "messages": [{"role": "user", "content": fixtures.REASONING_PROBLEMS["balance"]}],
    }
    curl = build_curl({**payload, "stream": True})
    resp = client.stream_message(payload)
    if not resp.ok:
        return fail("流式返回 thinking_delta", f"HTTP {resp.status_code}: {resp.error_message}",
                    "扩展思考流式请求失败", resp=resp, curl=curl, status_code=resp.status_code)
    hit = bool(resp.thinking.strip())
    return (ok if hit else fail)(
        "流中含 thinking_delta 事件", f"thinking长度={len(resp.thinking)}; 事件数={len(resp.event_types)}",
        "流式思考生效" if hit else "流中未出现 thinking_delta 思考增量",
        resp=resp, curl=curl, status_code=resp.status_code, metrics=resp.usage)


def _thinking_hard_batch(client: Client, model: str) -> TestOutcome:
    cfg = _thinking_config(model, budget_tokens=8000)
    payload = {
        "model": model,
        "max_tokens": 16000,
        **cfg,
        "messages": [{"role": "user", "content": fixtures.hard_reasoning_prompt()}],
    }
    curl = build_curl(payload)
    resp = client.create_message(payload)
    if not resp.ok:
        return fail("完整求解 4 道题", f"HTTP {resp.status_code}: {resp.error_message}",
                    "复合推理请求失败", resp=resp, curl=curl, status_code=resp.status_code)
    thinking_tokens = 0
    if isinstance(resp.usage, dict):
        details = resp.usage.get("output_tokens_details") or {}
        thinking_tokens = details.get("thinking_tokens") or 0
    hit = bool(resp.text.strip()) and (bool(resp.thinking.strip()) or thinking_tokens > 0)
    return (ok if hit else fail)(
        "有思考且答案完整", f"thinking长度={len(resp.thinking)}; 答案长度={len(resp.text)}",
        "复杂推理可用" if hit else "思考或答案不完整",
        resp=resp, curl=curl, status_code=resp.status_code, metrics=resp.usage)


def _thinking_with_tools(client: Client, model: str) -> TestOutcome:
    """扩展思考 + 工具调用组合。"""
    cfg = _thinking_config(model, budget_tokens=3000)
    payload = {
        "model": model,
        "max_tokens": 8000,
        **cfg,
        "tools": [fixtures.weather_tool()],
        "messages": [{"role": "user", "content": "先思考一下，再帮我查北京天气。"}],
    }
    curl = build_curl(payload)
    resp = client.create_message(payload)
    if not resp.ok:
        return fail("思考+工具共存", f"HTTP {resp.status_code}: {resp.error_message}",
                    "思考+工具请求失败", resp=resp, curl=curl, status_code=resp.status_code)
    has_think = bool(resp.thinking.strip())
    has_tool = any(t.get("name") == "get_current_weather" for t in resp.tool_uses)
    hit = has_think or has_tool
    return (ok if hit else fail)(
        "同时出现 thinking 与 tool_use",
        f"thinking={has_think}; tool={has_tool}; stop_reason={resp.stop_reason}",
        "思考与工具组合可用" if hit else "思考与工具组合异常",
        resp=resp, curl=curl, status_code=resp.status_code, metrics=resp.usage)


def _thinking_effort(client: Client, model: str, effort: str) -> TestOutcome:
    """adaptive + output_config.effort=<档位>：验证该 effort 档位被接受且思考生效。"""
    from .base import skip

    if not _supports_adaptive(model):
        return skip(
            reason=f"模型 {model} 只支持 manual thinking，官方不支持 output_config.effort",
            expected=f"adaptive + effort={effort}",
        )
    cfg = _thinking_config(model, effort=effort)
    payload = {
        "model": model,
        "max_tokens": 12000,
        **cfg,
        "messages": [{"role": "user", "content": fixtures.REASONING_PROBLEMS["truth"]}],
    }
    curl = build_curl(payload)
    expected = f"effort={effort} 被接受，且返回思考内容"
    resp = client.create_message(payload)
    if not resp.ok:
        return fail(expected, f"HTTP {resp.status_code}: {resp.error_message}",
                    f"effort={effort} 请求失败",
                    resp=resp, curl=curl, status_code=resp.status_code)
    thinking_tokens = 0
    if isinstance(resp.usage, dict):
        details = resp.usage.get("output_tokens_details") or {}
        thinking_tokens = details.get("thinking_tokens") or 0
    hit = bool(resp.thinking.strip()) or thinking_tokens > 0
    return (ok if hit else fail)(
        expected,
        f"thinking长度={len(resp.thinking)}; thinking_tokens={thinking_tokens}; "
        f"答案长度={len(resp.text)}",
        f"effort={effort} 生效" if hit else f"effort={effort} 已被接受但未返回思考内容",
        resp=resp, curl=curl, status_code=resp.status_code, metrics=resp.usage)


def _thinking_effort_max(client: Client, model: str) -> TestOutcome:
    return _thinking_effort(client, model, DEFAULT_EFFORT)


CASES = [
    TestCase("AN-THINK-001", CATEGORY, "扩展思考（非流式）", "P0", safe_run(_thinking_enabled)),
    TestCase("AN-THINK-002", CATEGORY, "扩展思考（流式 thinking_delta）", "P0",
             safe_run(_thinking_stream)),
    TestCase("AN-THINK-003", CATEGORY, "复杂复合推理（4题）", "P1", safe_run(_thinking_hard_batch)),
    TestCase("AN-THINK-004", CATEGORY, "扩展思考+工具调用组合", "P1",
             safe_run(_thinking_with_tools)),
    TestCase("AN-THINK-005", CATEGORY, f"adaptive + effort={DEFAULT_EFFORT} 深度思考", "P1",
             safe_run(_thinking_effort_max)),
]
