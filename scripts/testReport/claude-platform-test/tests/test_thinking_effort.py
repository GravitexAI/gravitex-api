"""思考类用例统一走最高档 effort=max，并按模型能力自动跳过。"""

import config
from cases import ALL_CASES
from cases.thinking import CASES, DEFAULT_EFFORT, _thinking_config, _thinking_effort
from client import MessageResponse


def test_only_the_max_effort_case_exists():
    """低档位官方允许模型不思考，逐档测会误判，所以只保留 max 一个用例。"""
    assert DEFAULT_EFFORT == "max"
    assert [case.case_id for case in CASES] == [
        "AN-THINK-001", "AN-THINK-002", "AN-THINK-003", "AN-THINK-004", "AN-THINK-005",
    ]
    effort_cases = [case for case in CASES if "effort=" in case.name]
    assert [case.name for case in effort_cases] == ["adaptive + effort=max 深度思考"]


def test_every_thinking_case_sends_effort_max():
    cfg = _thinking_config("claude-opus-5")
    assert cfg == {
        "thinking": {"type": "adaptive", "display": "summarized"},
        "output_config": {"effort": "max"},
    }


def test_manual_thinking_models_never_send_output_config():
    cfg = _thinking_config("claude-haiku-4-5-20251001", budget_tokens=4000)
    assert cfg == {"thinking": {"type": "enabled", "budget_tokens": 4000}}
    assert "output_config" not in cfg


class _RecordingClient:
    def __init__(self):
        self.payloads = []

    def create_message(self, payload):
        self.payloads.append(payload)
        return MessageResponse(status_code=200, ok=True, thinking="推理过程",
                               text="答案", usage={"output_tokens": 10})


def test_max_effort_is_sent_verbatim():
    client = _RecordingClient()

    outcome = _thinking_effort(client, "claude-opus-5", DEFAULT_EFFORT)

    assert outcome.verdict == "PASS"
    assert client.payloads[0]["output_config"] == {"effort": "max"}


def test_thinking_tokens_alone_count_as_thinking():
    """有些资源不透传 thinking 文本，但 usage 里有 thinking_tokens，也算思考生效。"""

    class _TokensOnlyClient:
        def create_message(self, payload):
            return MessageResponse(
                status_code=200, ok=True, thinking="", text="答案",
                usage={"output_tokens": 100, "output_tokens_details": {"thinking_tokens": 42}},
            )

    assert _thinking_effort(_TokensOnlyClient(), "claude-opus-5", "max").verdict == "PASS"


def test_effort_is_skipped_for_manual_only_models():
    client = _RecordingClient()

    outcome = _thinking_effort(client, "claude-haiku-4-5-20251001", "max")

    assert outcome.verdict == "SKIP"
    assert client.payloads == []            # 不支持就别发请求浪费配额


def test_removed_cases_are_gone_from_the_suite():
    case_ids = {case.case_id for case in ALL_CASES}
    categories = {case.category for case in ALL_CASES}
    names = {case.name for case in ALL_CASES}
    assert "AN-TOKEN-001" not in case_ids      # count_tokens 计数已删除
    assert "Token计数" not in categories
    assert "缺失 max_tokens 校验" not in names
    # 引用溯源 citations 是模型自主行为（官方文档写的是 may/can），不作为判定项
    assert not any("citations" in name for name in names)
    # effort 阶梯已收敛为 max 单档
    assert not any(level in name for name in names
                   for level in ("effort=low", "effort=medium", "effort=high", "effort=xhigh"))
