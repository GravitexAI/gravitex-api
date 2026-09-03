"""Prompt Cache 命中率：按 token 算 —— cache_read ÷ (cache_read + cache_creation)，含种子轮。

两个要点：
  1. 官方多轮缓存里同一次请求可以既读缓存又写新缓存，所以不能按"这轮 read>0 就算命中"
     数轮次——那会把"命中了但又重写了一大半前缀"当成满分。
  2. 种子轮那笔写入也计入分母（1.25x 的写入费是真花掉的），
     因此完美资源的理论上限是 rounds/(rounds+1)。
"""

import threading

import pytest

import config
from cases.caching import (
    _cache_hit_rate, METRIC_HIT_RATE, METRIC_MAX_RATE, METRIC_UNCACHED_ROUNDS,
)
from client import StreamResponse

PREFIX = 4096          # 种子轮写入的前缀长度
ROUNDS = 20


def _stream(read=0, creation=0, stop_reason="end_turn"):
    """缓存用例走流式，所以桩返回 StreamResponse。"""
    return StreamResponse(status_code=200, ok=True, text="OK", stop_reason=stop_reason,
                          first_text_seconds=0.4, total_seconds=1.2,
                          usage={"cache_read_input_tokens": read,
                                 "cache_creation_input_tokens": creation})


@pytest.fixture(autouse=True)
def _fast_cache_probe(monkeypatch):
    monkeypatch.setattr(config, "CACHE_SEED_WAIT_SECONDS", 0)
    monkeypatch.setattr(config, "CACHE_HIT_ROUNDS", ROUNDS)
    monkeypatch.setattr(config, "CACHE_HIT_PASS_RATIO", 0.85)


class _CacheClient:
    """种子轮返回 cache_creation；读取轮按脚本逐轮返回 (read, creation)。"""

    def __init__(self, rounds, fail_rounds=0):
        self.rounds = list(rounds)
        self.fail_rounds = fail_rounds
        self.calls = 0
        self._lock = threading.Lock()

    def stream_message(self, payload):
        with self._lock:
            self.calls += 1
            if self.calls == 1:
                return _stream(creation=PREFIX)
            if self.fail_rounds > 0:
                self.fail_rounds -= 1
                return StreamResponse(status_code=429, ok=False, error_message="rate limited")
            read, creation = self.rounds.pop() if self.rounds else (0, 0)
        return _stream(read=read, creation=creation)


def _rate(outcome):
    return outcome.metrics[METRIC_HIT_RATE]


def test_perfect_reuse_is_capped_by_the_seed_round_write():
    """每轮整段命中，但种子轮那次写入进分母 → 上限 20/21 = 95.2%。"""
    client = _CacheClient([(PREFIX, 0)] * ROUNDS)

    outcome = _cache_hit_rate(client, "claude-opus-5", ttl=None)

    assert _rate(outcome) == "95.2%"
    assert outcome.metrics[METRIC_MAX_RATE] == "95.2%"      # 理论上限写进报告，避免困惑
    assert outcome.verdict == "PASS"


def test_rounds_that_also_write_cache_lower_the_rate():
    """关键场景：命中的同时又写了新缓存，轮次口径会算 100%，token 口径不会。"""
    # 读 3000×20=60000；写 1000×20 + 种子 4096 = 24096 → 60000/84096 = 71.3%
    client = _CacheClient([(3000, 1000)] * ROUNDS)

    outcome = _cache_hit_rate(client, "claude-opus-5", ttl=None)

    assert _rate(outcome) == "71.3%"
    assert outcome.verdict == "FAIL"            # 轮次口径下这会是 20/20 满分
    assert "24096 token 被重复写入缓存" in outcome.reason


def test_small_partial_rewrite_still_passes():
    # 读 3900×20=78000；写 100×20 + 4096 = 6096 → 78000/84096 = 92.8%
    client = _CacheClient([(3900, 100)] * ROUNDS)

    outcome = _cache_hit_rate(client, "claude-opus-5", ttl=None)

    assert _rate(outcome) == "92.8%"
    assert outcome.verdict == "PASS"


def test_rounds_without_any_cache_activity_count_as_misses():
    """读写全为 0 的轮次不能"隐身"，按种子轮前缀折算进分母。"""
    # 17 轮整段命中 + 3 轮完全没走缓存 → 17/(17+3+1 种子) = 81.0%
    client = _CacheClient([(PREFIX, 0)] * 17 + [(0, 0)] * 3)

    outcome = _cache_hit_rate(client, "claude-opus-5", ttl=None)

    assert _rate(outcome) == "81.0%"
    assert outcome.metrics[METRIC_UNCACHED_ROUNDS] == 3
    assert outcome.verdict == "FAIL"


def test_cache_only_written_never_read_is_zero():
    """实测踩到过的情况：每轮都在重写缓存，一次都没读到。"""
    client = _CacheClient([(0, PREFIX)] * ROUNDS)

    outcome = _cache_hit_rate(client, "claude-opus-5", ttl=None)

    assert _rate(outcome) == "0.0%"
    assert outcome.verdict == "FAIL"


def test_no_cache_activity_at_all_is_reported_explicitly():
    """种子轮也没缓存、读取轮全 0 —— 资源根本没透传 cache_control。"""

    class _NoCache:
        def stream_message(self, payload):
            return _stream()

    outcome = _cache_hit_rate(_NoCache(), "claude-opus-5", ttl=None)

    assert outcome.verdict == "FAIL"
    assert "未观测到任何缓存读写" in outcome.reason


def test_failed_rounds_are_excluded_from_the_token_math():
    client = _CacheClient([(PREFIX, 0)] * 8, fail_rounds=12)

    outcome = _cache_hit_rate(client, "claude-opus-5", ttl=None)

    assert outcome.metrics["成功轮次"] == 8
    assert outcome.metrics["失败轮次"] == 12
    # 有效样本不足一半 -> 结论不可信，判不通过
    assert outcome.verdict == "FAIL"
    assert "有效样本不足" in outcome.reason


def test_seed_round_tokens_are_included_in_the_totals():
    client = _CacheClient([(PREFIX, 0)] * ROUNDS)

    outcome = _cache_hit_rate(client, "claude-opus-5", ttl=None)

    from cases.caching import METRIC_CREATION_TOKENS, METRIC_READ_TOKENS
    assert outcome.metrics["种子轮_cache_creation"] == PREFIX
    assert outcome.metrics[METRIC_READ_TOKENS] == PREFIX * ROUNDS
    assert outcome.metrics[METRIC_CREATION_TOKENS] == PREFIX      # 只有种子轮那一笔
    assert "含种子轮合计" in outcome.actual


def test_fewer_rounds_lower_the_theoretical_ceiling(monkeypatch):
    """轮数调小会把理论上限压到阈值以下——这是配置上的坑，报告里必须写明。"""
    monkeypatch.setattr(config, "CACHE_HIT_ROUNDS", 5)
    client = _CacheClient([(PREFIX, 0)] * 5)

    outcome = _cache_hit_rate(client, "claude-opus-5", ttl=None)

    assert outcome.metrics[METRIC_MAX_RATE] == "83.3%"      # 5/6
    assert _rate(outcome) == "83.3%"
    assert outcome.verdict == "FAIL"                        # 完美资源却低于 85% 阈值


def test_seed_round_failure_fails_fast():
    class _SeedFails:
        def stream_message(self, payload):
            return StreamResponse(status_code=500, ok=False, error_message="upstream error")

    outcome = _cache_hit_rate(_SeedFails(), "claude-opus-5", ttl=None)

    assert outcome.verdict == "FAIL"
    assert "种子轮" in outcome.reason
