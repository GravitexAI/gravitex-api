#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""Messages 参数边界用例。

采样参数按官方模型规则判定：新模型的非默认值预期 4xx，4.5/4.6
模型在本用例“不启用 thinking”时预期 2xx。超大 max_tokens 对全部模型
预期 4xx。返回状态会写入每个模型的明细，避免把旧模型的合法 200 误报为失败。
"""

from __future__ import annotations

from typing import Any

import config
from client import Client, build_curl
from .base import TestCase, TestOutcome, fail, ok, safe_run

CATEGORY = "参数边界"
EXPECTED_ERROR_CASES: list[TestCase] = []


def _run_parameter(client: Client, model: str, parameter_name: str,
                   value: Any) -> TestOutcome:
    payload = {
        "model": model,
        "max_tokens": 32,
        "messages": [{"role": "user", "content": "参数边界校验：请回复 OK。"}],
        parameter_name: value,
    }
    if parameter_name == "max_tokens":
        payload["max_tokens"] = value

    curl = build_curl(payload)
    resp = client.create_message(payload)
    status = resp.status_code
    actual = f"HTTP {status}: {resp.error_message or resp.text[:120]!r}"
    if parameter_name == "max_tokens":
        expected_status = "4xx"
    elif config.MODEL_CASES[model]["sampling_parameters"] == "expected_4xx":
        expected_status = "4xx"
    else:
        expected_status = "2xx"
    expected_ok = (400 <= status < 500) if expected_status == "4xx" else (200 <= status < 300)
    if expected_ok:
        return ok(
            f"HTTP {expected_status}（官方模型规则）",
            actual,
            "符合官方模型参数预期",
            resp=resp,
            curl=curl,
            status_code=status,
            metrics={"parameter": parameter_name, "value": value, "expected_status": expected_status},
        )
    return fail(
        f"HTTP {expected_status}（官方模型规则）",
        actual,
        f"参数未按预期返回 {expected_status}",
        resp=resp,
        curl=curl,
        status_code=status,
        metrics={"parameter": parameter_name, "value": value, "expected_status": expected_status},
    )


def _temperature(client: Client, model: str) -> TestOutcome:
    return _run_parameter(client, model, "temperature", 0.2)


def _top_p(client: Client, model: str) -> TestOutcome:
    return _run_parameter(client, model, "top_p", 0.9)


def _top_k(client: Client, model: str) -> TestOutcome:
    return _run_parameter(client, model, "top_k", 40)


def _max_tokens(client: Client, model: str) -> TestOutcome:
    return _run_parameter(client, model, "max_tokens", 128001)


CASES = [
    TestCase(
        "AN-PARAM-001", CATEGORY, "temperature 非默认值（按模型预期）", "P0",
        safe_run(_temperature), parameter_name="temperature",
        expected_status="model-dependent",
    ),
    TestCase(
        "AN-PARAM-002", CATEGORY, "top_p 非默认值（按模型预期）", "P0",
        safe_run(_top_p), parameter_name="top_p",
        expected_status="model-dependent",
    ),
    TestCase(
        "AN-PARAM-003", CATEGORY, "top_k 非默认值（按模型预期）", "P0",
        safe_run(_top_k), parameter_name="top_k",
        expected_status="model-dependent",
    ),
    TestCase(
        "AN-PARAM-004", CATEGORY, "max_tokens=128001（预期 4xx）", "P0",
        safe_run(_max_tokens), parameter_name="max_tokens",
        expected_error=True, expected_status="4xx",
    ),
]

EXPECTED_ERROR_CASES.extend(CASES)
