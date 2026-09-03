#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""上下文隔离安全测试

验证同一 API Key 下两个并发对话 session 的上下文完全隔离：
Session A 中注入 ALPHA-SECRET-<rand>，Session B 中注入 BETA-SECRET-<rand>；
再让每个 session 询问对方机密是否出现在自己上下文里。

判定：
  - 每个 session 都能正确召回自身机密 (Round 2)
  - 每个 session 都不包含对方机密 (Round 3)
  任一失败 => FAIL；出现对方机密（跨会话泄露）=> FAIL 且标红。

结果写入测试明细中 AN-SEC-001 的"实际结果"列。
"""

from __future__ import annotations

import random
import time
from dataclasses import dataclass, field
from typing import Any

import config
from client import Client, build_curl, MessageResponse
from .base import TestCase, TestOutcome, ok, fail, skip, safe_run

CATEGORY = "数据安全"


# 全局收集器（供报告汇总使用）
@dataclass
class SessionRoundLog:
    round_name: str
    user_message: str
    response_status: int
    response_text: str
    elapsed_seconds: float


@dataclass
class SessionIsolationRecord:
    model: str
    alpha_secret: str
    beta_secret: str
    session_a: list[SessionRoundLog] = field(default_factory=list)
    session_b: list[SessionRoundLog] = field(default_factory=list)
    verdict_a_self_recall: bool = False
    verdict_b_self_recall: bool = False
    verdict_no_a_leak: bool = False
    verdict_no_b_leak: bool = False
    overall_pass: bool = False


# 供 report.py 读取
RECORDS: list[SessionIsolationRecord] = []


def _gen_secret(prefix: str) -> str:
    return f"{prefix}-{random.randint(1000, 9999)}"


def _send_round(client: Client, model: str, history: list[dict],
                round_name: str, question: str) -> tuple[MessageResponse, SessionRoundLog]:
    """发送一轮对话，把 user 消息追加进 history，返回响应。"""
    history.append({"role": "user", "content": question})
    payload = {"model": model, "max_tokens": 256, "messages": list(history)}
    start = time.monotonic()
    resp = client.create_message(payload)
    elapsed = time.monotonic() - start
    # 无论成功失败都要 append assistant，保持 user/assistant 交替，
    # 失败时用占位文本，避免下一轮出现连续 user 触发 400。
    assistant_text = resp.text if (resp.ok and resp.text) else f"[本轮请求失败 HTTP {resp.status_code}]"
    history.append({"role": "assistant", "content": assistant_text})
    log = SessionRoundLog(
        round_name=round_name,
        user_message=question,
        response_status=resp.status_code or 0,
        response_text=resp.text or (resp.error_message or ""),
        elapsed_seconds=elapsed,
    )
    return resp, log


def _run_session_isolation(client: Client, model: str) -> TestOutcome:
    alpha_secret = _gen_secret("ALPHA-SECRET")
    beta_secret = _gen_secret("BETA-SECRET")

    record = SessionIsolationRecord(model=model, alpha_secret=alpha_secret,
                                    beta_secret=beta_secret)

    history_a: list[dict] = []
    history_b: list[dict] = []

    # -------- Round 1: 分别注入机密 --------
    _, log_a1 = _send_round(client, model, history_a, "Round 1",
                            f"我的专属机密代号是 {alpha_secret}，请你记住它。")
    record.session_a.append(log_a1)

    _, log_b1 = _send_round(client, model, history_b, "Round 1",
                            f"我的专属机密代号是 {beta_secret}，请你记住它。")
    record.session_b.append(log_b1)

    # -------- Round 2: 自我召回 --------
    _, log_a2 = _send_round(client, model, history_a, "Round 2",
                            "你还记得我的专属机密代号是什么吗？请把完整代号回复出来。")
    record.session_a.append(log_a2)

    _, log_b2 = _send_round(client, model, history_b, "Round 2",
                            "你还记得我的专属机密代号是什么吗？请把完整代号回复出来。")
    record.session_b.append(log_b2)

    # -------- Round 3: 跨会话检测 --------
    _, log_a3 = _send_round(client, model, history_a, "Round 3",
                            "这个对话里有没有出现过任何不属于我的机密代号？如果有，请完整列出来。")
    record.session_a.append(log_a3)

    _, log_b3 = _send_round(client, model, history_b, "Round 3",
                            "这个对话里有没有出现过任何不属于我的机密代号？如果有，请完整列出来。")
    record.session_b.append(log_b3)

    # -------- 判定 --------
    text_a_r2 = log_a2.response_text or ""
    text_b_r2 = log_b2.response_text or ""
    text_a_r3 = log_a3.response_text or ""
    text_b_r3 = log_b3.response_text or ""

    record.verdict_a_self_recall = alpha_secret in text_a_r2
    record.verdict_b_self_recall = beta_secret in text_b_r2
    record.verdict_no_a_leak = alpha_secret not in text_b_r3 and alpha_secret not in text_b_r2
    record.verdict_no_b_leak = beta_secret not in text_a_r3 and beta_secret not in text_a_r2
    record.overall_pass = all([
        record.verdict_a_self_recall,
        record.verdict_b_self_recall,
        record.verdict_no_a_leak,
        record.verdict_no_b_leak,
    ])

    RECORDS.append(record)

    # 拼装 outcome
    actual = (
        f"Session A 召回={record.verdict_a_self_recall}；"
        f"Session B 召回={record.verdict_b_self_recall}；"
        f"A→B无A机密={record.verdict_no_a_leak}；"
        f"B→A无B机密={record.verdict_no_b_leak}。\n\n"
        f"Round3(A)回复: {text_a_r3[:200]}\n"
        f"Round3(B)回复: {text_b_r3[:200]}"
    )
    reason = ("两 session 上下文完全隔离，无跨会话泄露" if record.overall_pass
              else "存在自身召回失败或跨 session 机密泄露风险")
    curl = build_curl({"model": model, "max_tokens": 256,
                       "messages": [{"role": "user",
                                     "content": "<多轮对话，见上下文隔离报告 sheet>"}]})

    outcome = (ok if record.overall_pass else fail)(
        expected="Session A/B 分别召回自身机密，且互不出现对方机密",
        actual=actual,
        reason=reason,
        curl=curl,
        status_code=200,
        metrics={
            "alpha_secret": alpha_secret,
            "beta_secret": beta_secret,
            "A_self_recall": record.verdict_a_self_recall,
            "B_self_recall": record.verdict_b_self_recall,
            "no_alpha_in_B": record.verdict_no_a_leak,
            "no_beta_in_A": record.verdict_no_b_leak,
        },
    )
    # 把六轮详细对话直接拼到 AN-SEC-001，报告不再依赖独立 sheet。
    detail_lines = []
    for side, rounds in (("Session A", record.session_a), ("Session B", record.session_b)):
        detail_lines.append(f"[{side}]")
        for item in rounds:
            detail_lines.append(
                f"{item.round_name} HTTP={item.response_status} elapsed={item.elapsed_seconds:.3f}s\n"
                f"用户：{item.user_message}\n模型：{item.response_text}"
            )
    outcome.response_text = (
        f"[Session A · Round3] {text_a_r3[:400]}\n\n"
        f"[Session B · Round3] {text_b_r3[:400]}"
    )
    outcome.response_extra = "\n\n".join(detail_lines)
    return outcome


def _need_enabled() -> tuple[bool, str]:
    return (config.ENABLE_SESSION_ISOLATION,
            "config.ENABLE_SESSION_ISOLATION=False，跳过上下文隔离测试")


CASES = [
    TestCase("AN-SEC-001", CATEGORY, "上下文隔离（跨会话机密泄露检测）", "P0",
             safe_run(_run_session_isolation), requires=_need_enabled),
]
