from client import MessageResponse, StreamResponse, _fill_from_message
from cases.base import TestOutcome, _attach_response


def test_normal_response_preserves_message_id_and_raw_json():
    result = MessageResponse(status_code=200, ok=True)
    message = {
        "id": "msg_123",
        "type": "message",
        "content": [{"type": "text", "text": "ok"}],
    }
    result.raw = message
    _fill_from_message(result, message)
    outcome = _attach_response(TestOutcome(verdict="PASS"), result)

    assert result.response_id == "msg_123"
    assert outcome.response_id == "msg_123"
    assert outcome.response_raw == message


def test_stream_response_preserves_message_id_from_message_start():
    response = StreamResponse(status_code=200, ok=True)
    response.response_id = "msg_stream_123"
    outcome = _attach_response(TestOutcome(verdict="PASS"), response)

    assert outcome.response_id == "msg_stream_123"
