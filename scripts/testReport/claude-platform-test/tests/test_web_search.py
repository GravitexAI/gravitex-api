from cases.web_search import CASES, build_web_search_payload
from client import MessageResponse, StreamResponse


def test_web_search_payload_uses_anthropic_server_tool_definition():
    payload = build_web_search_payload("claude-opus-4-8", stream=False)
    assert payload["model"] == "claude-opus-4-8"
    assert payload["max_tokens"] == 4096
    assert payload["stream"] is False
    assert payload["tools"] == [{"type": "web_search_20250305", "name": "web_search"}]
    assert payload["messages"][0]["content"].startswith("Search for the current prices")


def test_web_search_cases_cover_both_transport_modes():
    assert [case.case_id for case in CASES] == [
        "AN-WEB-SEARCH-001",
        "AN-WEB-SEARCH-002",
    ]


def test_web_search_cases_are_recorded_as_reference_only():
    """联网搜索能力依赖资源来源（官方支持 / Bedrock 不支持），一律不计入通过率。"""
    class FakeClient:
        def create_message(self, payload):
            return MessageResponse(
                status_code=200,
                ok=True,
                text="AAPL has the better P/E ratio. [source]",
                server_tool_uses=[{"name": "web_search", "type": "server_tool_use"}],
                server_tool_results=[{"type": "web_search_tool_result", "content": []}],
            )

        def stream_message(self, payload):
            return StreamResponse(
                status_code=200,
                ok=True,
                text="AAPL has the better P/E ratio. [source]",
                server_tool_uses=[{"name": "web_search", "type": "server_tool_use"}],
                server_tool_results=[{"type": "web_search_tool_result", "content": []}],
                event_types=["message_start", "content_block_start", "message_stop"],
            )

    for case in CASES:
        outcome = case.run(FakeClient(), "claude-opus-4-8")
        # 实测可用，但仍记为跳过，且判定说明里保留实测结论供人工判断
        assert outcome.verdict == "SKIP"
        assert "实测可用" in outcome.reason
        assert "不计入通过率" in outcome.reason
