#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""响应延迟分位统计（P50 / P95 / P99）——只统计，不判定通过与否。

样本来源：**提示词缓存命中率用例**（AN-CACHE-001 / 002）的每一次请求，走**流式**调用。

主指标是**首字延迟**（从发出请求到收到第一个文本增量），而不是总耗时——
总耗时被输出长度主导，长短一变分位数就跟着漂；首字延迟才是用户实际感知的"快慢"，
也是流式场景唯一有意义的延迟口径。总耗时同时统计，作为参考列一并写进报告。
选它是因为这批调用的负载最整齐——同一段超长系统提示词、同样的 max_tokens、
每个模型固定 (1 种子轮 + N 读取轮) × 2 个 TTL 次调用，跨模型可以直接横向比较。
其他用例的负载差异极大（2M 字符超大上下文、6MB 图片、4 道复合推理题），
混进来算分位数没有意义。

只统计 HTTP 成功的调用；失败调用单独计数，不计入分位（失败通常是快速返回的
错误响应，混进来会把分位数拉低，反而掩盖真实延迟）。

分位算法与仓库里的 latency_test.py 保持一致：statistics.quantiles(inclusive)。
"""

from __future__ import annotations

import statistics
import threading

from .base import TestOutcome, SKIP

CASE_ID = "AN-LATENCY-001"
CATEGORY = "响应延迟"
NAME = "响应延迟分位 P50/P95/P99（仅统计，不判定）"
SEVERITY = "P0"

# model -> 成功调用的首字延迟（秒）
_SAMPLES: dict[str, list[float]] = {}
# model -> 成功调用的总耗时（秒）
_TOTALS: dict[str, list[float]] = {}
# model -> 失败调用次数
_FAILURES: dict[str, int] = {}
_LOCK = threading.Lock()


def record(model: str, first_token_seconds: float | None, succeeded: bool,
           total_seconds: float | None = None) -> None:
    """登记一次调用的耗时。并发安全，供缓存用例在每次请求后调用。

    first_token_seconds 为 None（比如非流式或流里没出过文本）时不计入首字分位，
    但只要请求成功，总耗时仍然记下来。
    """
    with _LOCK:
        if not succeeded:
            _FAILURES[model] = _FAILURES.get(model, 0) + 1
            return
        if first_token_seconds is not None:
            _SAMPLES.setdefault(model, []).append(first_token_seconds)
        if total_seconds is not None:
            _TOTALS.setdefault(model, []).append(total_seconds)


def reset() -> None:
    """清空样本（测试用）。"""
    with _LOCK:
        _SAMPLES.clear()
        _TOTALS.clear()
        _FAILURES.clear()


def percentile(values: list[float], pct: float) -> float:
    """分位数，算法对齐 latency_test.py。"""
    if not values:
        return 0.0
    data = sorted(values)
    if len(data) == 1:
        return data[0]
    quantiles = statistics.quantiles(data, n=100, method="inclusive")
    index = max(0, min(99, int(round(pct)) - 1))
    return quantiles[index]


def stats(model: str) -> dict[str, float | int]:
    """返回该模型的延迟统计。样本为空时各项为 0。"""
    with _LOCK:
        values = list(_SAMPLES.get(model, []))
        totals = list(_TOTALS.get(model, []))
        failures = _FAILURES.get(model, 0)
    return {
        "样本数": len(values),
        "失败调用数": failures,
        "首字P50(s)": round(percentile(values, 50), 3),
        "首字P95(s)": round(percentile(values, 95), 3),
        "首字P99(s)": round(percentile(values, 99), 3),
        "首字平均(s)": round(statistics.mean(values), 3) if values else 0.0,
        "首字最小(s)": round(min(values), 3) if values else 0.0,
        "首字最大(s)": round(max(values), 3) if values else 0.0,
        "总耗时P50(s)": round(percentile(totals, 50), 3),
        "总耗时P95(s)": round(percentile(totals, 95), 3),
    }


def build_outcome(model: str) -> TestOutcome:
    """构造测试明细里的延迟统计行：结论固定为「跳过」，不参与通过率。"""
    data = stats(model)
    expected = "仅统计响应延迟分位，不判定通过与否"
    if not data["样本数"]:
        return TestOutcome(
            verdict=SKIP, expected=expected,
            actual="无有效样本（缓存命中率用例未产生成功调用）",
            reason="缓存命中率用例没有成功调用，无法统计延迟分位",
            metrics=data,
        )
    actual = (f"首字延迟 P50={data['首字P50(s)']}s；P95={data['首字P95(s)']}s；"
              f"P99={data['首字P99(s)']}s；平均={data['首字平均(s)']}s；"
              f"区间 {data['首字最小(s)']}s ~ {data['首字最大(s)']}s"
              f"｜总耗时 P50={data['总耗时P50(s)']}s，P95={data['总耗时P95(s)']}s")
    return TestOutcome(
        verdict=SKIP, expected=expected, actual=actual,
        reason=(f"取自提示词缓存用例的 {data['样本数']} 次成功流式调用（首字延迟口径）"
                f"（另有 {data['失败调用数']} 次失败调用未计入）；仅作性能参考，不判定通过与否"),
        metrics=data,
    )
