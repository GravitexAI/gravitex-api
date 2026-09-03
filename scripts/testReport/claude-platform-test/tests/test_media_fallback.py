"""URL 多模态用例：base64 / URL 直链任一成功即通过。"""

import pytest

import media
from cases.media_case import run_url_media_case
from client import MessageResponse


@pytest.fixture(autouse=True)
def _no_network_download(monkeypatch):
    """base64 方式不真的下载资源。"""
    monkeypatch.setattr(media, "_fetch_as_base64", lambda url, timeout=60: ("image/png", "QUFB"))


class _ScriptedClient:
    """按脚本依次返回响应，并记录每次请求用的 source 类型。"""

    def __init__(self, responses):
        self._responses = list(responses)
        self.source_types = []
        self.contents = []

    def create_message(self, payload):
        blocks = payload["messages"][0]["content"]
        self.contents.append(blocks)
        self.source_types.append(
            next(b["source"]["type"] for b in blocks if b["type"] != "text")
        )
        return self._responses.pop(0)


def _resp(status, ok, text="", error=None):
    return MessageResponse(status_code=status, ok=ok, text=text, error_message=error)


def test_url_mode_rescues_a_resource_that_rejects_base64():
    client = _ScriptedClient([
        _resp(400, False, error="base64 image not supported"),
        _resp(200, True, text="一只猫"),
    ])

    outcome = run_url_media_case(
        client, "claude-opus-5", block_type="image", urls=["https://oss/x.png"],
        prompt="描述图片", max_tokens=200, expected="任一方式成功",
    )

    assert outcome.verdict == "PASS"
    assert client.source_types == ["base64", "url"]
    assert "一只猫" in outcome.actual


def test_base64_success_short_circuits_the_url_attempt():
    client = _ScriptedClient([_resp(200, True, text="一份财报")])

    outcome = run_url_media_case(
        client, "claude-opus-5", block_type="document", urls=["https://oss/x.pdf"],
        prompt="概括文档", max_tokens=300, expected="任一方式成功",
    )

    assert outcome.verdict == "PASS"
    assert client.source_types == ["base64"]


def test_oversized_resource_passes_when_every_mode_returns_4xx():
    client = _ScriptedClient([
        _resp(413, False, error="image exceeds 5 MB"),
        _resp(413, False, error="image exceeds 5 MB"),
    ])

    outcome = run_url_media_case(
        client, "claude-opus-5", block_type="image", urls=["https://oss/big.png"],
        prompt="描述大图", max_tokens=200, expected="成功或明确超限",
        allow_size_limit=True,
    )

    assert outcome.verdict == "PASS"
    assert client.source_types == ["base64", "url"]


def test_server_error_on_every_mode_still_fails():
    client = _ScriptedClient([
        _resp(500, False, error="upstream error"),
        _resp(500, False, error="upstream error"),
    ])

    outcome = run_url_media_case(
        client, "claude-opus-5", block_type="image", urls=["https://oss/big.png"],
        prompt="描述大图", max_tokens=200, expected="成功或明确超限",
        allow_size_limit=True,
    )

    assert outcome.verdict == "FAIL"


def test_base64_download_failure_does_not_block_the_url_attempt(monkeypatch):
    def boom(url, timeout=60):
        raise OSError("connection reset")

    monkeypatch.setattr(media, "_fetch_as_base64", boom)
    client = _ScriptedClient([_resp(200, True, text="一张风景照")])

    outcome = run_url_media_case(
        client, "claude-opus-5", block_type="image", urls=["https://oss/x.png"],
        prompt="描述图片", max_tokens=200, expected="任一方式成功",
    )

    assert outcome.verdict == "PASS"
    assert client.source_types == ["url"]
    assert "资源下载或转码失败" in outcome.actual


def test_multi_resource_request_sends_every_resource_with_a_number_label():
    client = _ScriptedClient([_resp(200, True, text="两图差异如下")])

    outcome = run_url_media_case(
        client, "claude-opus-5", block_type="image",
        urls=["https://oss/a.png", "https://oss/b.png"],
        prompt="请对比这几张图片的异同。", max_tokens=300, expected="任一方式成功",
    )

    assert outcome.verdict == "PASS"
    blocks = client.contents[0]
    assert [b["type"] for b in blocks] == ["text", "image", "text", "image", "text"]
    assert [b["text"] for b in blocks if b["type"] == "text"] == [
        "资源1：", "资源2：", "请对比这几张图片的异同。",
    ]
