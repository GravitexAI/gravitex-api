#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""视觉理解类用例。base64 用例自带图片；URL 用例需在 config 填链接，否则 SKIP。

URL 图片用例统一走 media_case.run_url_media_case：base64 与 URL 直链两种传输方式
都试一遍，任一成功即算通过（部分 Claude 资源只认 base64 且限 5MB，部分两种都认）。
"""

from __future__ import annotations

import config
import fixtures
from client import Client, build_curl
from .base import TestCase, TestOutcome, ok, fail, safe_run
from .media_case import run_url_media_case

CATEGORY = "视觉理解"


def _image_base64(client: Client, model: str) -> TestOutcome:
    payload = {
        "model": model,
        "max_tokens": 2048,
        "messages": [
            {
                "role": "user",
                "content": [
                    {
                        "type": "image",
                        "source": {
                            "type": "base64",
                            "media_type": "image/png",
                            "data": fixtures.small_png_base64(),
                        },
                    },
                    {"type": "text", "text": "这张图片主要是什么颜色？只回答颜色。"},
                ],
            }
        ],
    }
    curl = build_curl({**payload, "messages": "<含base64图片，已省略>"})
    resp = client.create_message(payload)
    if not resp.ok:
        return fail("识别出图片颜色", f"HTTP {resp.status_code}: {resp.error_message}",
                    "base64 图片请求失败", resp=resp, curl=curl, status_code=resp.status_code)
    hit = any(k in resp.text for k in ("红", "red", "Red"))
    return (ok if hit else fail)(
        "回复含红色", resp.text[:100],
        "正确识别纯色图" if hit else "未识别出图片颜色（模型可读但答错）",
        resp=resp, curl=curl, status_code=resp.status_code, metrics=resp.usage)


def _image_url_small(client: Client, model: str) -> TestOutcome:
    return run_url_media_case(
        client, model,
        block_type="image",
        urls=[config.get_image_url_small()],
        prompt="请简要描述这张图片的内容。",
        max_tokens=2048,
        expected="base64 或 URL 直链任一方式返回非空图片描述",
    )


def _image_url_large(client: Client, model: str) -> TestOutcome:
    """> 5MB 大图：任一传输方式成功即通过；全部被 4xx 明确拒绝也算符合边界预期。"""
    return run_url_media_case(
        client, model,
        block_type="image",
        urls=[config.get_image_url_large()],
        prompt="请简要描述这张大图片的内容。",
        max_tokens=2048,
        expected="任一方式处理成功，或明确返回 4xx 大小超限",
        allow_size_limit=True,
    )


def _multi_image(client: Client, model: str) -> TestOutcome:
    return run_url_media_case(
        client, model,
        block_type="image",
        urls=list(config.IMAGE_URLS_MULTI),
        prompt="请对比这几张图片的异同。",
        max_tokens=2048,
        expected="base64 或 URL 直链任一方式返回非空多图对比结果",
    )


def _oversized_image_base64(client: Client, model: str) -> TestOutcome:
    """验证 >5MB 图片边界。

    两种结果都算正常：资源限 5MB 时明确返回 4xx；资源不限 5MB 时直接读图成功。
    base64 路径既没成功也没给出 4xx 时，再用 URL 路径复核。
    """
    payload = {
        "model": model,
        "max_tokens": 2048,
        "messages": [
            {
                "role": "user",
                "content": [
                    {
                        "type": "image",
                        "source": {
                            "type": "base64",
                            "media_type": "image/png",
                            "data": fixtures.oversized_png_base64(),
                        },
                    },
                    {"type": "text", "text": "描述这张图。"},
                ],
            }
        ],
    }
    curl = build_curl({**payload, "messages": "<含>5MB超大base64图片，已省略>"})
    expected = "成功处理，或明确返回 4xx 大小超限"
    resp = client.create_message(payload)
    if resp.status_code and 400 <= resp.status_code < 500:
        return ok(expected, f"HTTP {resp.status_code}: {resp.error_message}",
                  "资源限制 5MB，已正确拒绝超大图片",
                  resp=resp, curl=curl, status_code=resp.status_code)
    if resp.ok and resp.text.strip():
        return ok(expected, resp.text[:150],
                  "资源不限 5MB，>5MB base64 图片直接读取成功",
                  resp=resp, curl=curl, status_code=resp.status_code, metrics=resp.usage)
    # 既没成功也没给 4xx（5xx / 超时 / 空回复）时，用同一边界的 URL 路径复核，
    # 避免把单一传输方式的限制误报成资源能力缺失。
    if config.get_image_url_large():
        url_outcome = _image_url_large(client, model)
        if url_outcome.verdict == "PASS":
            url_outcome.expected = expected
            url_outcome.actual = (
                f"base64 路径既未成功也未返回 4xx；URL 路径通过。\n{url_outcome.actual}"
            )
            url_outcome.reason = "URL 图片路径符合超大图片边界预期"
            return url_outcome
    return fail(expected,
                f"HTTP {resp.status_code}: {resp.error_message or resp.text[:80]}",
                "既未成功读取也未返回明确 4xx 大小超限错误",
                resp=resp, curl=curl, status_code=resp.status_code)


def _need_small_url() -> tuple[bool, str]:
    return (bool(config.get_image_url_small()),
            "config.IMAGE_URL_SMALL / IMAGE_URL_SINGLE 未填写图片链接")


def _need_large_url() -> tuple[bool, str]:
    return (bool(config.get_image_url_large()),
            "config.IMAGE_URL_LARGE 未填写超大图片链接")


def _need_multi_url() -> tuple[bool, str]:
    return (len(config.IMAGE_URLS_MULTI) >= 2, "config.IMAGE_URLS_MULTI 图片链接不足 2 张")


CASES = [
    TestCase("AN-VISION-001", CATEGORY, "base64 图片理解", "P0", safe_run(_image_base64)),
    TestCase("AN-VISION-002", CATEGORY, "URL 小图（≤5MB）理解（base64/URL 任一通过）", "P1",
             safe_run(_image_url_small), requires=_need_small_url),
    TestCase("AN-VISION-003", CATEGORY, "多图对比（base64/URL 任一通过）", "P2",
             safe_run(_multi_image), requires=_need_multi_url),
    TestCase("AN-VISION-004", CATEGORY, "base64 超大图片（>5MB）读取或明确超限", "P1",
             safe_run(_oversized_image_base64)),
    TestCase("AN-VISION-005", CATEGORY, "URL 大图（>5MB）读取/超限（base64/URL 任一通过）", "P1",
             safe_run(_image_url_large), requires=_need_large_url),
]
