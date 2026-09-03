#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""测试用例基础类型。"""

from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any, Callable, Optional

from client import Client, MessageResponse, StreamResponse

# 测试结论取值
PASS = "PASS"
FAIL = "FAIL"
SKIP = "SKIP"
ERROR = "ERROR"

# 安全分类器拒答：HTTP 200 + stop_reason=refusal + content 为空。
# 它不是 HTTP 错误，resp.ok 仍为 True，所以每个用例都得单独识别，
# 否则会把"模型拒答"误判成"能力缺失"（例如把拒答说成"未返回 thinking 内容"）。
REFUSAL = "refusal"
_REFUSAL_NOTE = "本次请求被安全分类器拒绝（stop_reason=refusal，无输出内容）"


def is_refusal(resp: Any) -> bool:
    return getattr(resp, "stop_reason", None) == REFUSAL


@dataclass
class TestOutcome:
    """单次（模型 × 用例）测试的结果。"""

    verdict: str                          # PASS / FAIL / SKIP / ERROR
    expected: str = ""                    # 预期结果
    actual: str = ""                      # 实际结果摘要
    reason: str = ""                      # 判定说明 / 失败原因
    curl: str = ""                        # 调用样例
    status_code: Optional[int] = None
    metrics: dict[str, Any] = field(default_factory=dict)      # 关键指标（usage 等）
    # ---- 模型完整响应（用于报告"实际结果"列展示，超长截断） ----
    response_text: str = ""               # 模型返回的文本
    response_id: str | None = None         # Anthropic message id
    response_raw: Any = None               # 普通响应 JSON；流式为原始 SSE 文本
    response_thinking: str = ""           # 模型返回的 thinking 内容
    response_tool_uses: list[dict[str, Any]] = field(default_factory=list)
    response_server_tool_uses: list[dict[str, Any]] = field(default_factory=list)
    response_server_tool_results: list[dict[str, Any]] = field(default_factory=list)
    response_stop_reason: Optional[str] = None
    response_extra: str = ""              # 额外补充（如流事件序列、错误 body 等）
    response_usage: dict[str, Any] = field(default_factory=dict)  # 响应 usage 全量


@dataclass
class TestCase:
    """一个测试用例。run 接收 client 与 model，返回 TestOutcome。"""

    case_id: str
    category: str
    name: str
    severity: str                         # 严重级别：P0/P1/P2
    run: Callable[[Client, str], TestOutcome]
    # 前置条件：返回 (是否满足, 跳过原因)。不满足则整例 SKIP。
    requires: Optional[Callable[[], tuple[bool, str]]] = None
    # 参数边界用例的报告元数据。
    parameter_name: str = ""
    expected_error: bool = False
    expected_status: str = ""


def _attach_response(outcome: TestOutcome, resp: Any) -> TestOutcome:
    """把响应对象里的 text/thinking/tool_uses/stop_reason 附加到 outcome 上。"""
    if resp is None:
        return outcome
    outcome.response_text = getattr(resp, "text", "") or ""
    outcome.response_id = getattr(resp, "response_id", None)
    raw = getattr(resp, "raw", None)
    if raw is not None:
        outcome.response_raw = raw
    else:
        raw_lines = getattr(resp, "raw_lines", None)
        if raw_lines:
            outcome.response_raw = "\n".join(str(line) for line in raw_lines)
    outcome.response_thinking = getattr(resp, "thinking", "") or ""
    outcome.response_tool_uses = list(getattr(resp, "tool_uses", []) or [])
    outcome.response_server_tool_uses = list(getattr(resp, "server_tool_uses", []) or [])
    outcome.response_server_tool_results = list(getattr(resp, "server_tool_results", []) or [])
    outcome.response_stop_reason = getattr(resp, "stop_reason", None)
    if outcome.response_stop_reason == REFUSAL and "安全分类器" not in (outcome.reason or ""):
        outcome.reason = f"{outcome.reason}；{_REFUSAL_NOTE}" if outcome.reason else _REFUSAL_NOTE
    outcome.response_usage = dict(getattr(resp, "usage", {}) or {})
    # 流式响应带事件序列
    events = getattr(resp, "event_types", None)
    extra_parts: list[str] = []
    if events:
        extra_parts.append(f"事件序列: {events}")
    if outcome.response_server_tool_uses or outcome.response_server_tool_results:
        extra_parts.append(
            f"server_tool_use={len(outcome.response_server_tool_uses)}"
            f"; web_search_tool_result={len(outcome.response_server_tool_results)}"
        )
    if extra_parts:
        outcome.response_extra = "; ".join(extra_parts)
    return outcome


def ok(expected: str, actual: str, reason: str = "", resp: Any = None,
       **extra: Any) -> TestOutcome:
    outcome = TestOutcome(verdict=PASS, expected=expected, actual=actual, reason=reason, **extra)
    return _attach_response(outcome, resp)


def fail(expected: str, actual: str, reason: str, resp: Any = None,
         **extra: Any) -> TestOutcome:
    outcome = TestOutcome(verdict=FAIL, expected=expected, actual=actual, reason=reason, **extra)
    return _attach_response(outcome, resp)


def skip(reason: str, expected: str = "") -> TestOutcome:
    return TestOutcome(verdict=SKIP, expected=expected, actual="", reason=reason)


def skipped(expected: str, actual: str, reason: str, resp: Any = None,
            **extra: Any) -> TestOutcome:
    """请求已经发出、但结论不计入通过率（例如被安全分类器拒绝，能力没测到）。"""
    outcome = TestOutcome(verdict=SKIP, expected=expected, actual=actual, reason=reason, **extra)
    return _attach_response(outcome, resp)


def error(reason: str, expected: str = "", status_code: Optional[int] = None) -> TestOutcome:
    return TestOutcome(
        verdict=ERROR, expected=expected, actual="", reason=reason, status_code=status_code
    )


def safe_run(fn: Callable[[Client, str], TestOutcome]) -> Callable[[Client, str], TestOutcome]:
    """包装 run，捕获异常统一转为 ERROR。"""

    def wrapper(client: Client, model: str) -> TestOutcome:
        try:
            return fn(client, model)
        except Exception as exc:  # noqa: BLE001
            return error(reason=f"用例执行异常：{type(exc).__name__}: {exc}")

    return wrapper
