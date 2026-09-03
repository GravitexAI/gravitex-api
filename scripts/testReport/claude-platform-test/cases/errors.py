#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""错误处理与参数校验类用例：验证平台是否返回规范的 Anthropic 错误结构。"""

from __future__ import annotations

from client import Client, build_curl
from .base import TestCase, TestOutcome, ok, fail, safe_run

CATEGORY = "错误处理"


def _invalid_model(client: Client, model: str) -> TestOutcome:
    payload = {
        "model": "claude-does-not-exist-9999",
        "max_tokens": 32,
        "messages": [{"role": "user", "content": "hi"}],
    }
    curl = build_curl(payload)
    resp = client.create_message(payload)
    hit = resp.status_code and 400 <= resp.status_code < 500 and resp.error_type
    return (ok if hit else fail)(
        "4xx 且含 error.type", f"HTTP {resp.status_code}; type={resp.error_type}; msg={resp.error_message}",
        "非法模型名返回规范错误" if hit else "未返回规范的 4xx 错误结构",
        resp=resp, curl=curl, status_code=resp.status_code)


def _empty_messages(client: Client, model: str) -> TestOutcome:
    payload = {"model": model, "max_tokens": 32, "messages": []}
    curl = build_curl(payload)
    resp = client.create_message(payload)
    hit = resp.status_code and 400 <= resp.status_code < 500
    return (ok if hit else fail)(
        "4xx 参数校验错误", f"HTTP {resp.status_code}; type={resp.error_type}; msg={resp.error_message}",
        "空 messages 被正确校验" if hit else "空 messages 未被校验",
        resp=resp, curl=curl, status_code=resp.status_code)


def _bad_auth(client: Client, model: str) -> TestOutcome:
    """临时用错误密钥请求，验证 401/403。"""
    import config as _cfg

    original = _cfg.API_KEY
    _cfg.API_KEY = "sk-invalid-key-for-test-000000"
    try:
        payload = {"model": model, "max_tokens": 16,
                   "messages": [{"role": "user", "content": "hi"}]}
        curl = build_curl(payload)
        resp = client.create_message(payload)
    finally:
        _cfg.API_KEY = original
    hit = resp.status_code in (401, 403)
    return (ok if hit else fail)(
        "401/403 鉴权错误", f"HTTP {resp.status_code}; type={resp.error_type}",
        "错误密钥被正确拒绝" if hit else "错误密钥未返回 401/403",
        resp=resp, curl=curl, status_code=resp.status_code)


CASES = [
    TestCase("AN-ERR-001", CATEGORY, "非法模型名报错", "P1", safe_run(_invalid_model)),
    TestCase("AN-ERR-002", CATEGORY, "空 messages 校验", "P2", safe_run(_empty_messages)),
    TestCase("AN-ERR-003", CATEGORY, "错误密钥鉴权失败", "P1", safe_run(_bad_auth)),
]
