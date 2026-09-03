"""安全分类器拒答（stop_reason=refusal）的处理。

拒答是 HTTP 200 + content 为空，resp.ok 仍为 True，但用量里照样上报 cache_creation。
所以必须单独识别，否则会：
  1. 把"模型拒答"误报成"缓存坏了 / 能力缺失"；
  2. 继续把剩下的读取轮全发出去——每轮都被拒、每轮都白付一次缓存写入费。
"""

import pytest

import config
from cases import caching
from cases.base import fail, is_refusal, ok, skipped
from client import MessageResponse, StreamResponse

PREFIX = 4096


def _refusal(cache_creation=PREFIX):
    """拒答：HTTP 200 + 无内容，但用量里照样有 cache_creation。"""
    return StreamResponse(status_code=200, ok=True, text="", stop_reason="refusal",
                          first_text_seconds=None, total_seconds=0.8,
                          usage={"cache_creation_input_tokens": cache_creation})


def _normal(read=0, creation=0, stop_reason="end_turn"):
    return StreamResponse(status_code=200, ok=True, text="OK", stop_reason=stop_reason,
                          first_text_seconds=0.4, total_seconds=1.2,
                          usage={"cache_read_input_tokens": read,
                                 "cache_creation_input_tokens": creation})


@pytest.fixture(autouse=True)
def _fast(monkeypatch):
    monkeypatch.setattr(config, "CACHE_SEED_WAIT_SECONDS", 0)
    monkeypatch.setattr(config, "CACHE_HIT_ROUNDS", 20)


def test_is_refusal_detects_the_stop_reason():
    assert is_refusal(_refusal()) is True
    assert is_refusal(_normal()) is False
    assert is_refusal(MessageResponse(status_code=200, ok=True,
                                      stop_reason="refusal")) is True


def test_any_case_reporting_a_refusal_gets_an_explicit_note():
    """别的用例把拒答说成"未返回 thinking 内容"会误导，这里统一补一句说明。"""
    outcome = fail("响应含非空 thinking 块", "thinking长度=0", "未返回 thinking 内容",
                   resp=_refusal())

    assert "安全分类器" in outcome.reason
    assert "未返回 thinking 内容" in outcome.reason      # 原因保留，只是补充说明


def test_the_note_is_not_duplicated_when_already_mentioned():
    outcome = skipped("x", "y", "种子轮被安全分类器拒答", resp=_refusal())

    assert outcome.reason.count("安全分类器") == 1


class _CountingClient:
    def __init__(self, responses):
        self.responses = list(responses)
        self.calls = 0

    def stream_message(self, payload):
        self.calls += 1
        return self.responses[min(self.calls - 1, len(self.responses) - 1)]


def test_refused_seed_stops_immediately_instead_of_burning_20_more_rounds():
    """省钱主力：分类器判定是确定的，种子轮被拒 = 后面每轮必被拒且每轮都写缓存。"""
    client = _CountingClient([_refusal()])

    outcome = caching._cache_hit_rate(client, "claude-opus-5", ttl=None)

    assert client.calls == 1                       # 只发了种子轮，20 轮读取一次没发
    assert outcome.verdict == "SKIP"               # 不计入通过率——缓存能力没测到，不是坏了
    assert "安全分类器" in outcome.reason
    assert "已跳过后续 20 轮读取" in outcome.reason
    assert outcome.metrics[caching.METRIC_REFUSED_WASTE] == PREFIX


def test_refused_read_rounds_are_excluded_not_counted_as_misses():
    """零星被拒的读取轮不算"未命中"，单独计数并统计白写入的 token。"""
    responses = [_normal(creation=PREFIX)] + [_refusal()] * 20
    client = _CountingClient(responses)

    outcome = caching._cache_hit_rate(client, "claude-opus-5", ttl=None)

    assert outcome.verdict == "SKIP"               # 全被拒 → 有效样本不足，记跳过而非不通过
    assert outcome.metrics[caching.METRIC_REFUSED_ROUNDS] == 20
    assert outcome.metrics[caching.METRIC_REFUSED_WASTE] == PREFIX * 20
    assert "被安全分类器拒答" in outcome.reason


def test_a_few_refusals_do_not_poison_the_hit_rate():
    """多数轮正常时，被拒的那几轮只是剔出统计，不该把命中率拉低。"""
    responses = [_normal(creation=PREFIX)] + [_normal(read=PREFIX)] * 18 + [_refusal()] * 2
    client = _CountingClient(responses)

    outcome = caching._cache_hit_rate(client, "claude-opus-5", ttl=None)

    assert outcome.metrics[caching.METRIC_REFUSED_ROUNDS] == 2
    # 18 轮命中 ÷ (18 轮读 + 种子轮写) = 94.7%，被拒的 2 轮没进分母
    assert outcome.metrics[caching.METRIC_HIT_RATE] == "94.7%"
    assert outcome.verdict == "PASS"


def test_normal_runs_report_no_refusals():
    client = _CountingClient([_normal(creation=PREFIX)] + [_normal(read=PREFIX)] * 20)

    outcome = caching._cache_hit_rate(client, "claude-opus-5", ttl=None)

    assert outcome.metrics[caching.METRIC_REFUSED_ROUNDS] == 0
    assert outcome.metrics[caching.METRIC_REFUSED_WASTE] == 0
    assert "安全分类器" not in outcome.reason
    assert outcome.verdict == "PASS"
