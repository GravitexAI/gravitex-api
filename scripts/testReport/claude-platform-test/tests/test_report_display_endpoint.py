"""报告展示地址/鉴权方式与实际调用地址解耦：实际请求永远走 BASE_URL / AUTH_MODE。"""

import config
from client import Client, build_curl


def test_report_helpers_fall_back_to_actual_values(monkeypatch):
    monkeypatch.setattr(config, "REPORT_BASE_URL", "")
    monkeypatch.setattr(config, "REPORT_AUTH_MODE", "")

    assert config.report_base_url() == config.BASE_URL
    assert config.report_auth_mode() == config.AUTH_MODE
    assert config.report_differs_from_actual() is False


def test_curl_shows_the_report_endpoint_and_auth(monkeypatch):
    monkeypatch.setattr(config, "BASE_URL", "https://api.gravitex.ai")
    monkeypatch.setattr(config, "AUTH_MODE", "bearer")
    monkeypatch.setattr(config, "REPORT_BASE_URL", "https://api.vendor.example")
    monkeypatch.setattr(config, "REPORT_AUTH_MODE", "anthropic")

    curl = build_curl({"model": "claude-opus-5", "max_tokens": 16})

    assert "https://api.vendor.example/v1/messages" in curl
    assert "https://api.gravitex.ai" not in curl
    assert "x-api-key: $API_KEY" in curl          # anthropic 模式的请求头
    assert "Authorization: Bearer" not in curl
    assert config.report_differs_from_actual() is True


def test_actual_request_still_goes_to_base_url(monkeypatch):
    """展示值只影响报告，真实请求必须打到 BASE_URL 并按 AUTH_MODE 组装鉴权头。"""
    monkeypatch.setattr(config, "BASE_URL", "https://api.gravitex.ai")
    monkeypatch.setattr(config, "AUTH_MODE", "bearer")
    monkeypatch.setattr(config, "REPORT_BASE_URL", "https://api.vendor.example")
    monkeypatch.setattr(config, "REPORT_AUTH_MODE", "anthropic")
    monkeypatch.setattr(config, "PRINT_HTTP", False)

    sent = {}

    class _FakeSession:
        def post(self, url, headers=None, json=None, timeout=None, stream=False):
            sent["url"] = url
            sent["headers"] = headers

            class _Resp:
                status_code = 200
                ok = True

                @staticmethod
                def json():
                    return {"id": "msg_1", "type": "message", "content": []}

            return _Resp()

    client = Client()
    monkeypatch.setattr(type(client), "session", property(lambda self: _FakeSession()))
    client.base_url = config.BASE_URL

    client.create_message({"model": "claude-opus-5", "max_tokens": 16, "messages": []})

    assert sent["url"] == "https://api.gravitex.ai/v1/messages"
    assert sent["headers"]["Authorization"].startswith("Bearer ")
    assert "x-api-key" not in sent["headers"]
