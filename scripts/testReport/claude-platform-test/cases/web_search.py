#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""Anthropic Server Web Search Tool 用例。

★ 判定策略：只测不评分。
  联网搜索能力取决于资源来源——Anthropic 官方支持 web_search 服务端工具，
  而 AWS Bedrock 版 Claude 不提供该工具。资源来源不固定，测不通过并不代表
  该资源有缺陷，所以这两个用例一律记为「跳过（仅供参考）」，不计入通过率，
  但请求照常发出，实测结果完整写进报告供人工判断。
"""

from __future__ import annotations

from typing import Any

from client import Client, build_curl
from .base import TestCase, TestOutcome, SKIP, fail, ok, safe_run

CATEGORY = "联网搜索"
WEB_SEARCH_TOOL = {"type": "web_search_20250305", "name": "web_search"}
SEARCH_PROMPT = (
    "Search for the current prices of AAPL and GOOGL, then calculate which "
    "has a better P/E ratio. Cite the sources used."
)


def build_web_search_payload(model: str, stream: bool) -> dict[str, Any]:
    return {
        "model": model,
        "max_tokens": 4096,
        "stream": stream,
        "messages": [{"role": "user", "content": SEARCH_PROMPT}],
        "tools": [dict(WEB_SEARCH_TOOL)],
    }


def _run_web_search(client: Client, model: str, stream: bool) -> TestOutcome:
    payload = build_web_search_payload(model, stream)
    curl = build_curl(payload)
    resp = client.stream_message(payload) if stream else client.create_message(payload)
    if not resp.ok:
        return fail(
            "HTTP 200 且返回 server_tool_use 与 web_search_tool_result",
            f"HTTP {resp.status_code}: {resp.error_message or '无错误详情'}",
            "联网搜索请求失败；如果是 Web Search 未启用，应由平台配置开启",
            resp=resp,
            curl=curl,
            status_code=resp.status_code,
            metrics={"server_tool": WEB_SEARCH_TOOL, "search_prompt": SEARCH_PROMPT},
        )

    server_uses = list(getattr(resp, "server_tool_uses", []) or [])
    server_results = list(getattr(resp, "server_tool_results", []) or [])
    has_search_use = any(item.get("name") == "web_search" for item in server_uses)
    has_search_result = bool(server_results)
    event_types = set(getattr(resp, "event_types", []) or [])
    if stream:
        has_search_result = has_search_result or "content_block_start" in event_types and any(
            "web_search_tool_result" in line for line in getattr(resp, "raw_lines", [])
        )
    hit = has_search_use and has_search_result and bool(getattr(resp, "text", "").strip())
    actual = (
        f"server_tool_use={len(server_uses)}; web_search_tool_result={len(server_results)}; "
        f"events={sorted(event_types) if stream else 'non_stream'}; text_length={len(resp.text)}"
    )
    metrics = {
        "server_tool": WEB_SEARCH_TOOL,
        "server_tool_use_count": len(server_uses),
        "web_search_result_count": len(server_results),
        "search_prompt": SEARCH_PROMPT,
    }
    return (ok if hit else fail)(
        "HTTP 200 且返回 server_tool_use、web_search_tool_result 和带引用的文本",
        actual,
        "联网搜索工具调用和结果均已返回" if hit else "HTTP 200 但未观察到完整联网搜索结果",
        resp=resp,
        curl=curl,
        status_code=resp.status_code,
        metrics=metrics,
    )


def _as_reference_only(outcome: TestOutcome) -> TestOutcome:
    """把实测结论降级为「跳过（仅供参考）」，不计入通过率。"""
    available = outcome.verdict == "PASS"
    outcome.verdict = SKIP
    outcome.expected = "仅记录实测情况，不判定通过与否"
    outcome.reason = (
        f"实测{'可用' if available else '不可用'}（仅供参考，不计入通过率）：{outcome.reason}。"
        "联网搜索取决于资源来源——Anthropic 官方支持 web_search 服务端工具，"
        "AWS Bedrock 版 Claude 不提供该工具，故本项不计入通过率。"
    )
    return outcome


def _web_search_non_stream(client: Client, model: str) -> TestOutcome:
    return _as_reference_only(_run_web_search(client, model, stream=False))


def _web_search_stream(client: Client, model: str) -> TestOutcome:
    return _as_reference_only(_run_web_search(client, model, stream=True))


CASES = [
    TestCase("AN-WEB-SEARCH-001", CATEGORY, "非流式 server Web Search（仅参考，不计通过率）", "P1",
             safe_run(_web_search_non_stream)),
    TestCase("AN-WEB-SEARCH-002", CATEGORY, "流式 server Web Search（仅参考，不计通过率）", "P1",
             safe_run(_web_search_stream)),
]
