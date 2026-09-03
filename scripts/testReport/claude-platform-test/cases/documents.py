#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""文档（PDF）理解类用例：只验证"能不能读懂 PDF"这一件事。

base64 用例自带内置 PDF（含可校验的 secret code）；URL 用例需 config 填链接。
URL 用例走 media_case.run_url_media_case：base64 与 URL 直链两种方式都试，
任一成功即算"能解析 PDF"——部分 Claude 资源只支持 base64，部分两种都支持。
"""

from __future__ import annotations

import base64

import config
import fixtures
from client import Client, build_curl
from .base import TestCase, TestOutcome, ok, fail, safe_run
from .media_case import run_url_media_case

CATEGORY = "文档理解"


def _pdf_base64(client: Client, model: str) -> TestOutcome:
    if config.PDF_FILE_PATH:
        with open(config.PDF_FILE_PATH, "rb") as f:
            data = base64.b64encode(f.read()).decode("ascii")
        secret_hint = "（使用你在 config 指定的 PDF）"
        expect_secret = None
    else:
        data = fixtures.small_pdf_base64()
        secret_hint = ""
        expect_secret = fixtures.PDF_SECRET_CODE

    question = "请提取文档中的 secret code（形如 XXX-数字）。" if expect_secret else "请概括这份文档的内容。"
    payload = {
        "model": model,
        # adaptive 思考会先占用输出预算，max_tokens 给小了答案会被截断成空，
        # 看起来像"读不懂文档"，实际是预算不够，所以这里留足空间。
        "max_tokens": 4096,
        "messages": [
            {
                "role": "user",
                "content": [
                    {
                        "type": "document",
                        "source": {
                            "type": "base64",
                            "media_type": "application/pdf",
                            "data": data,
                        },
                    },
                    {"type": "text", "text": question},
                ],
            }
        ],
    }
    curl = build_curl({**payload, "messages": f"<含base64 PDF{secret_hint}，已省略>"})
    resp = client.create_message(payload)
    if not resp.ok:
        return fail("成功读取 PDF", f"HTTP {resp.status_code}: {resp.error_message}",
                    "PDF base64 请求失败", resp=resp, curl=curl, status_code=resp.status_code)
    if expect_secret:
        hit = expect_secret in resp.text
        return (ok if hit else fail)(
            f"回复含 {expect_secret}", resp.text[:150],
            "正确读取 PDF 内文本" if hit else "未读到 PDF 内文本",
            resp=resp, curl=curl, status_code=resp.status_code, metrics=resp.usage)
    hit = bool(resp.text.strip())
    return (ok if hit else fail)(
        "非空文档摘要", resp.text[:150],
        "PDF 理解成功" if hit else "返回空摘要",
        resp=resp, curl=curl, status_code=resp.status_code, metrics=resp.usage)


def _pdf_url_small(client: Client, model: str) -> TestOutcome:
    return run_url_media_case(
        client, model,
        block_type="document",
        urls=[config.get_pdf_url_small()],
        prompt="请概括这份文档的核心内容。",
        max_tokens=4096,
        expected="base64 或 URL 直链任一方式返回非空文档摘要",
    )


def _pdf_url_large(client: Client, model: str) -> TestOutcome:
    """> 5MB 大 PDF：任一传输方式成功即通过；全部被 4xx 明确拒绝也算符合边界预期。"""
    return run_url_media_case(
        client, model,
        block_type="document",
        urls=[config.get_pdf_url_large()],
        prompt="请概括这份大文档的核心内容。",
        max_tokens=4096,
        expected="任一方式处理成功，或明确返回 4xx 大小超限",
        allow_size_limit=True,
    )


def _need_pdf_small() -> tuple[bool, str]:
    return (bool(config.get_pdf_url_small()),
            "config.PDF_URL_SMALL / PDF_URL 未填写 PDF 链接")


def _need_pdf_large() -> tuple[bool, str]:
    return (bool(config.get_pdf_url_large()),
            "config.PDF_URL_LARGE 未填写超大 PDF 链接")


CASES = [
    TestCase("AN-DOC-001", CATEGORY, "base64 PDF 理解", "P0", safe_run(_pdf_base64)),
    TestCase("AN-DOC-002", CATEGORY, "URL 小 PDF（≤5MB）理解（base64/URL 任一通过）", "P1",
             safe_run(_pdf_url_small), requires=_need_pdf_small),
    TestCase("AN-DOC-003", CATEGORY, "URL 大 PDF（>5MB）读取/超限（base64/URL 任一通过）", "P1",
             safe_run(_pdf_url_large), requires=_need_pdf_large),
]
