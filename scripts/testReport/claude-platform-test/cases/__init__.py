#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""汇总所有测试用例。"""

from __future__ import annotations

from .base import TestCase, TestOutcome, PASS, FAIL, SKIP, ERROR
from . import (
    latency,
    basic,
    vision,
    documents,
    tools,
    thinking,
    caching,
    context,
    errors,
    streaming,
    session_isolation,
    parameters,
    web_search,
)

ALL_CASES: list[TestCase] = [
    *basic.CASES,
    *vision.CASES,
    *documents.CASES,
    *tools.CASES,
    *thinking.CASES,
    *caching.CASES,
    *context.CASES,
    *errors.CASES,
    *streaming.CASES,
    *session_isolation.CASES,
    *parameters.CASES,
    *web_search.CASES,
]

# 12 个分类名，按 ALL_CASES 的出现顺序去重（dict 保序）。
# 管理端弹窗的分类多选项就是这份列表。
ALL_CATEGORIES: list[str] = list(dict.fromkeys(case.category for case in ALL_CASES))

# 分类名 -> 该分类下的用例数，用于弹窗展示和预计请求次数计算。
CATEGORY_CASE_COUNTS: dict[str, int] = {
    category: sum(1 for case in ALL_CASES if case.category == category)
    for category in ALL_CATEGORIES
}


def filter_cases(categories: list[str]) -> list[TestCase]:
    """按分类过滤用例。categories 为空表示不过滤，返回全部用例。

    结果顺序始终是 ALL_CASES 的原始顺序，不受传入顺序影响——
    报告的行序依赖这个顺序（run_tests.py 用它建 order 索引）。
    """
    if not categories:
        return ALL_CASES
    wanted = set(categories)
    return [case for case in ALL_CASES if case.category in wanted]


__all__ = ["ALL_CASES", "ALL_CATEGORIES", "CATEGORY_CASE_COUNTS", "filter_cases",
           "TestCase", "TestOutcome", "PASS", "FAIL", "SKIP", "ERROR", "latency"]
