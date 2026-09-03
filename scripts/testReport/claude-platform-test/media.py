#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""多模态资源 source 构造器（同一资源支持两种传输方式）。

背景：Anthropic 官方 /v1/messages 支持 image/document 的 url source；
但 AWS Bedrock 版 Claude 只接受 base64（不认 url），且常有 5MB 单文件上限。
被测平台背后是哪种资源事先并不知道，所以图片/PDF 的 URL 用例会按
config.MEDIA_SOURCE_MODES 的顺序把两种方式都试一遍，任一成功即算能力具备。

下载结果按 URL 缓存：同一个 OSS 链接会被多个模型、多个用例反复用到，
并发跑测试时只下载一次，省流量也省时间。
"""

from __future__ import annotations

import base64
import mimetypes
import threading
import urllib.request

import config

# 传输方式的尝试顺序（base64 是所有上游都支持的最小公分母，放前面）
MODES: tuple[str, ...] = tuple(getattr(config, "MEDIA_SOURCE_MODES", ("base64", "url")))

# URL -> (media_type, base64字符串)
_DOWNLOAD_CACHE: dict[str, tuple[str, str]] = {}
_DOWNLOAD_LOCK = threading.Lock()


def _fetch_as_base64(url: str, timeout: int = 60) -> tuple[str, str]:
    """下载 URL 资源，返回 (media_type, base64字符串)；同一 URL 只下载一次。"""
    with _DOWNLOAD_LOCK:
        cached = _DOWNLOAD_CACHE.get(url)
        if cached is not None:
            return cached
        req = urllib.request.Request(url, headers={"User-Agent": "claude-platform-test/1.0"})
        with urllib.request.urlopen(req, timeout=timeout) as resp:  # noqa: S310
            data = resp.read()
            ctype = (resp.headers.get("Content-Type") or "").split(";")[0].strip()
        if not ctype:
            ctype = mimetypes.guess_type(url)[0] or ""
        result = (ctype, base64.b64encode(data).decode("ascii"))
        _DOWNLOAD_CACHE[url] = result
        return result


def image_source(url: str, mode: str) -> dict:
    """构造图片 source。mode="base64" 时现场下载转码，mode="url" 时直接透传链接。"""
    if mode == "base64":
        media_type, b64 = _fetch_as_base64(url)
        return {"type": "base64", "media_type": media_type or "image/png", "data": b64}
    return {"type": "url", "url": url}


def document_source(url: str, mode: str) -> dict:
    """构造文档（PDF）source。mode="base64" 时现场下载转码。"""
    if mode == "base64":
        _media_type, b64 = _fetch_as_base64(url)
        return {"type": "base64", "media_type": "application/pdf", "data": b64}
    return {"type": "url", "url": url}
