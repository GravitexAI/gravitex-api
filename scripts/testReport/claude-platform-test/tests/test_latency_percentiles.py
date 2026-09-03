"""响应延迟分位：取自缓存用例的调用耗时，只统计不判定通过与否。"""

import pytest

import config
from cases import ALL_CASES, latency
from cases.caching import _cache_hit_rate
from client import StreamResponse


@pytest.fixture(autouse=True)
def _clean_samples():
    latency.reset()
    yield
    latency.reset()


def test_latency_row_is_p0_and_never_judged():
    latency.record("claude-opus-5", 1.0, succeeded=True, total_seconds=4.0)

    outcome = latency.build_outcome("claude-opus-5")

    assert latency.SEVERITY == "P0"
    assert outcome.verdict == "SKIP"           # 不计入通过率
    assert "不判定通过与否" in outcome.expected
    # 延迟统计不是常规用例，不能混进 ALL_CASES 被并发调度
    assert latency.CASE_ID not in {case.case_id for case in ALL_CASES}


def test_percentiles_are_computed_per_model():
    for value in range(1, 101):               # 1..100 秒
        latency.record("claude-opus-5", float(value), succeeded=True,
                       total_seconds=float(value) * 3)
    latency.record("claude-sonnet-5", 5.0, succeeded=True, total_seconds=15.0)

    opus = latency.stats("claude-opus-5")
    assert opus["样本数"] == 100
    assert opus["首字P50(s)"] == pytest.approx(50.5, abs=1.0)
    assert opus["首字P95(s)"] == pytest.approx(95.5, abs=1.0)
    assert opus["首字P99(s)"] == pytest.approx(99.5, abs=1.0)
    assert opus["首字最小(s)"] == 1.0 and opus["首字最大(s)"] == 100.0

    # 模型之间互不干扰
    assert latency.stats("claude-sonnet-5")["样本数"] == 1
    assert latency.stats("claude-sonnet-5")["首字P50(s)"] == 5.0


def test_failed_calls_are_counted_but_excluded_from_percentiles():
    latency.record("claude-opus-5", 10.0, succeeded=True, total_seconds=30.0)
    latency.record("claude-opus-5", 0.01, succeeded=False)   # 快速失败不该拉低分位

    data = latency.stats("claude-opus-5")
    assert data["样本数"] == 1
    assert data["失败调用数"] == 1
    assert data["首字P50(s)"] == 10.0
    assert "1 次失败调用未计入" in latency.build_outcome("claude-opus-5").reason


def test_no_samples_reports_insufficient_data():
    outcome = latency.build_outcome("claude-opus-5")

    assert outcome.verdict == "SKIP"
    assert "无有效样本" in outcome.actual


def test_cache_case_feeds_the_latency_samples(monkeypatch):
    """缓存用例的每次调用（种子轮 + 读取轮）都要登记耗时。"""
    monkeypatch.setattr(config, "CACHE_SEED_WAIT_SECONDS", 0)
    monkeypatch.setattr(config, "CACHE_HIT_ROUNDS", 4)

    class _Client:
        def stream_message(self, payload):
            return StreamResponse(status_code=200, ok=True, text="OK", first_text_seconds=0.5,
                                  total_seconds=2.0, usage={"cache_read_input_tokens": 4096})

    _cache_hit_rate(_Client(), "claude-opus-5", ttl=None)

    # 1 次种子轮 + 4 次读取轮
    assert latency.stats("claude-opus-5")["样本数"] == 5
