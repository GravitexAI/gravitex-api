#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""用例分类过滤。弹窗只勾选部分分类时，脚本只跑这些分类的用例。"""

from __future__ import annotations

import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent.parent))

import cases


def test_all_categories_covers_twelve_names():
    assert cases.ALL_CATEGORIES == [
        "基础对话", "视觉理解", "文档理解", "工具调用", "扩展思考",
        "提示词缓存", "上下文窗口", "错误处理", "流式响应", "数据安全",
        "参数边界", "联网搜索",
    ]


def test_category_case_counts_sum_to_all_cases():
    assert sum(cases.CATEGORY_CASE_COUNTS.values()) == len(cases.ALL_CASES)
    assert cases.CATEGORY_CASE_COUNTS["基础对话"] == 5
    assert cases.CATEGORY_CASE_COUNTS["提示词缓存"] == 2
    assert cases.CATEGORY_CASE_COUNTS["上下文窗口"] == 1


def test_empty_filter_returns_all():
    assert cases.filter_cases([]) == cases.ALL_CASES


def test_filter_keeps_only_selected_categories_and_original_order():
    selected = cases.filter_cases(["提示词缓存", "基础对话"])
    assert {case.category for case in selected} == {"基础对话", "提示词缓存"}
    assert len(selected) == 7
    # 顺序按 ALL_CASES 原始顺序，不按传入顺序
    assert [case.category for case in selected][:5] == ["基础对话"] * 5


def test_unknown_category_is_ignored():
    assert cases.filter_cases(["不存在的分类"]) == []
