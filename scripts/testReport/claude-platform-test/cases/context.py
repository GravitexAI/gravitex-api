#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""上下文窗口类用例：验证超大上下文能被处理，或被明确拒绝。"""

from __future__ import annotations

import fixtures
from client import Client, build_curl
from .base import TestCase, TestOutcome, ok, fail, safe_run

CATEGORY = "上下文窗口"


def _large_context(client: Client, model: str) -> TestOutcome:
    big = fixtures.large_context_text()
    payload = {
        "model": model,
        "max_tokens": 128,
        "messages": [
            {"role": "user", "content": big + "\n\n以上是长文本，请只回复『已读取』三个字。"}
        ],
    }
    curl = build_curl({**payload, "messages": f"<约{len(big)}字符超大上下文，已省略>"})
    resp = client.create_message(payload)
    # 预期：要么成功处理返回文本，要么返回明确的上下文超限 4xx；
    # 不应超时或 5xx（对照 110004）
    if resp.ok and resp.text.strip():
        return ok("成功处理或明确超限", resp.text[:80],
                  "超大上下文处理成功", resp=resp, curl=curl,
                  status_code=resp.status_code, metrics=resp.usage)
    if resp.status_code and 400 <= resp.status_code < 500:
        return ok("成功处理或明确超限（4xx）",
                  f"HTTP {resp.status_code}: {resp.error_message}",
                  "正确返回上下文超限错误", resp=resp, curl=curl, status_code=resp.status_code)
    return fail("成功处理或返回 4xx 超限",
                f"HTTP {resp.status_code}: {resp.error_message or resp.text[:80]}",
                "既未成功读取也未返回明确 4xx 上下文超限错误",
                resp=resp, curl=curl, status_code=resp.status_code)


CASES = [
    TestCase("AN-CTX-001", CATEGORY, "超大上下文处理/明确超限", "P1", safe_run(_large_context)),
]
