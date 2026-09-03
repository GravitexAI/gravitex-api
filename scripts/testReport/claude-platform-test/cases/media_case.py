#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""URL 多模态用例的多方式执行器（图片 / PDF 共用）。

一个 URL 资源按 media.MODES 依次尝试 base64 和 url 两种传输方式：
只要有一种方式读取成功，该用例就判定通过——因为部分 Claude 资源
（如 AWS Bedrock 中转）只认 base64，部分资源两种都认，
不该因为传输方式的差异把资源判成"不支持图片/PDF"。
"""

from __future__ import annotations

from typing import Any

import media
from client import Client, build_curl
from .base import TestOutcome, ok, fail

# 块类型 -> source 构造器
_SOURCE_BUILDER = {"image": media.image_source, "document": media.document_source}

_MODE_LABEL = {"base64": "base64", "url": "URL 直链"}


def _build_content(block_type: str, urls: list[str], mode: str, prompt: str) -> list[dict]:
    """拼多模态 content：多资源时给每个资源加序号文本，便于模型对比。"""
    content: list[dict] = []
    numbered = len(urls) > 1
    for index, url in enumerate(urls, start=1):
        if numbered:
            content.append({"type": "text", "text": f"资源{index}："})
        content.append({"type": block_type, "source": _SOURCE_BUILDER[block_type](url, mode)})
    content.append({"type": "text", "text": prompt})
    return content


def run_url_media_case(
    client: Client,
    model: str,
    *,
    block_type: str,
    urls: list[str],
    prompt: str,
    max_tokens: int,
    expected: str,
    allow_size_limit: bool = False,
) -> TestOutcome:
    """按 base64 / url 逐一尝试，任一成功即 PASS。

    allow_size_limit=True 用于 >5MB 的超大资源用例：所有方式都被平台以 4xx
    明确拒绝（大小/格式超限）也算符合预期，只有 5xx / 超时 / 空回复才算不通过。
    """
    attempts: list[str] = []
    last_resp: Any = None
    last_curl = ""
    saw_4xx = False

    for mode in media.MODES:
        label = _MODE_LABEL.get(mode, mode)
        try:
            content = _build_content(block_type, urls, mode, prompt)
        except Exception as exc:  # noqa: BLE001 —— 下载/转码失败不影响另一种方式
            attempts.append(f"[{label}] 资源下载或转码失败：{type(exc).__name__}: {exc}")
            continue

        payload = {"model": model, "max_tokens": max_tokens,
                   "messages": [{"role": "user", "content": content}]}
        curl = (build_curl({**payload, "messages": f"<含 base64 {block_type}(URL已转码)，已省略>"})
                if mode == "base64" else build_curl(payload))
        resp = client.create_message(payload)
        last_resp, last_curl = resp, curl

        if resp.ok and resp.text.strip():
            attempts.append(f"[{label}] 成功")
            return ok(expected,
                      "尝试记录：" + "；".join(attempts) + f"\n\n{resp.text[:150]}",
                      f"{label} 方式读取成功（多方式任一成功即通过）",
                      resp=resp, curl=curl, status_code=resp.status_code,
                      metrics=resp.usage)

        detail = resp.error_message or (resp.text[:80] if resp.text else "")
        attempts.append(f"[{label}] HTTP {resp.status_code}: {detail}")
        if resp.status_code and 400 <= resp.status_code < 500:
            saw_4xx = True

    summary = "尝试记录：" + "；".join(attempts)
    status = last_resp.status_code if last_resp is not None else None
    if allow_size_limit and saw_4xx:
        return ok(expected, summary, "各传输方式均以 4xx 明确拒绝，符合超大资源边界预期",
                  resp=last_resp, curl=last_curl, status_code=status)
    return fail(expected, summary, "base64 与 URL 直链两种方式都没能成功读取",
                resp=last_resp, curl=last_curl, status_code=status)
