"""报告不能把 base64 大 blob（思考签名、图片数据、搜索密文）原样写进 Excel。"""

from cases.base import TestOutcome
from report import _format_response

LONG_BLOB = "CAISlwIKjwEIERgCKkCQeXmebZIX76BdpeCyZWxizBV0KDIMlcHslvkv92DF2c4pEh53wAKk" * 3


def test_thinking_signature_is_replaced_with_a_length_note():
    outcome = TestOutcome(
        verdict="PASS",
        response_raw={
            "id": "msg_1",
            "content": [
                {"type": "thinking", "thinking": "", "signature": LONG_BLOB},
                {"type": "text", "text": "42"},
            ],
        },
    )

    rendered = _format_response(outcome)

    assert LONG_BLOB not in rendered
    assert "signature base64 数据已省略" in rendered
    assert '"text": "42"' in rendered      # 正常内容保持可读


def test_image_data_and_encrypted_content_are_replaced():
    outcome = TestOutcome(
        verdict="PASS",
        response_raw={
            "content": [
                {"type": "image", "source": {"type": "base64", "data": LONG_BLOB}},
                {"type": "web_search_tool_result", "encrypted_content": LONG_BLOB},
            ]
        },
    )

    rendered = _format_response(outcome)

    assert LONG_BLOB not in rendered
    assert "data base64 数据已省略" in rendered
    assert "encrypted_content base64 数据已省略" in rendered


def test_raw_sse_text_signature_is_replaced():
    outcome = TestOutcome(
        verdict="PASS",
        response_raw=(
            'event: content_block_start\n'
            'data: {"type":"content_block_start","content_block":'
            f'{{"type":"thinking","signature":"{LONG_BLOB}"}}}}'
        ),
    )

    rendered = _format_response(outcome)

    assert LONG_BLOB not in rendered
    assert "base64 数据已省略" in rendered


def test_short_data_values_are_left_alone():
    outcome = TestOutcome(verdict="PASS", response_raw={"content": [{"data": "QUFB"}]})

    assert '"data": "QUFB"' in _format_response(outcome)
