"""被缓存的系统提示词带「本次运行标记」：TTL 内重跑不会命中上一次残留的缓存。

不加标记时，固定的系统提示词会让重跑的种子轮直接读到上次的缓存
（cache_creation=0），测不出真实的写入→读取链路。
"""

import re
from collections import Counter

import pytest

import config
import fixtures
from cases import caching
from client import StreamResponse

NONCE_PATTERN = re.compile(r"^\d{8}-\d{6}-[0-9a-f]{8}$")


def test_run_nonce_looks_like_timestamp_plus_random_suffix():
    assert NONCE_PATTERN.match(caching.RUN_CACHE_NONCE), caching.RUN_CACHE_NONCE


def test_each_run_gets_a_different_nonce():
    """同秒内连续生成也必须不同（随机后缀的作用）。"""
    nonces = {caching._new_run_nonce() for _ in range(50)}

    assert len(nonces) == 50


def test_cached_prefix_carries_the_run_nonce_and_the_long_prompt():
    text = caching.cached_system_text("5分钟")

    assert caching.RUN_CACHE_NONCE in text
    assert fixtures.long_system_prompt() in text     # 长度仍然够触发缓存最小阈值
    assert text.startswith("[缓存测试运行标记")        # 标记在前缀最前面，key 立刻分叉


def test_the_two_ttl_cases_do_not_share_a_cache_key():
    """5 分钟档和 1 小时档必须逐字不同，否则后跑那档会直接读到前一档的缓存。

    首尾各有一处 TTL 标记：开头保证前缀立刻分叉，结尾防止中转层剥掉开头的标记行。
    """
    five, hour = caching.cached_system_text("5分钟"), caching.cached_system_text("1小时")

    assert five != hour
    assert five.splitlines()[0] != hour.splitlines()[0], "开头没分叉"
    assert five.splitlines()[-1] != hour.splitlines()[-1], "结尾没分叉"
    # 分叉点应当落在第一行（缓存是前缀匹配，越早分叉越彻底）
    diverge = next(i for i in range(min(len(five), len(hour))) if five[i] != hour[i])
    assert diverge < len(five.splitlines()[0])


class _RecordingClient:
    def __init__(self):
        self.system_texts = []

    def stream_message(self, payload):
        self.system_texts.append(payload["system"][0]["text"])
        return StreamResponse(status_code=200, ok=True, text="OK", first_text_seconds=0.3,
                              total_seconds=1.0, usage={"cache_read_input_tokens": 4096})


def test_seed_and_read_rounds_send_byte_identical_prefixes(monkeypatch):
    """标记必须在整轮里逐字一致——不一致就一次都命中不了。"""
    monkeypatch.setattr(config, "CACHE_SEED_WAIT_SECONDS", 0)
    monkeypatch.setattr(config, "CACHE_HIT_ROUNDS", 5)
    client = _RecordingClient()

    caching._cache_hit_rate(client, "claude-opus-5", ttl=None)

    assert len(client.system_texts) == 6            # 1 种子轮 + 5 读取轮
    assert len(set(client.system_texts)) == 1       # 全部逐字一致


def test_seed_round_without_creation_is_flagged(monkeypatch):
    """万一标记没起作用（种子轮读到了旧缓存），实际结果里要有告警。"""
    monkeypatch.setattr(config, "CACHE_SEED_WAIT_SECONDS", 0)
    monkeypatch.setattr(config, "CACHE_HIT_ROUNDS", 4)

    class _WarmCache:
        def stream_message(self, payload):
            return StreamResponse(status_code=200, ok=True, text="OK", first_text_seconds=0.3,
                                  total_seconds=1.0, usage={"cache_read_input_tokens": 4096})

    outcome = caching._cache_hit_rate(_WarmCache(), "claude-opus-5", ttl=None)

    assert "种子轮未写入缓存" in outcome.actual
    assert outcome.metrics["运行标记"] == caching.RUN_CACHE_NONCE


def test_long_prefix_clears_the_strictest_cache_minimum():
    """缓存最小前缀最严的是 4096 token（Opus 4.6 / Haiku 4.5），掉下去缓存根本不会形成。"""
    text = fixtures.long_system_prompt()

    assert len(text) > 4096 * 1.2, f"长前缀只有 {len(text)} 字符，逼近缓存下限"


def test_long_prefix_is_not_repetitive_spam():
    """早期版本把同一句规范重复 120 遍（只有编号不同），被判成拒答。

    现在的前缀是整份技术文档重复若干遍以够到缓存下限，所以按"抹掉数字后的句式"
    统计：任何句式的出现次数不应超过文档份数。
    """
    text = fixtures.long_system_prompt()
    copies = -(-fixtures._CACHE_PREFIX_MIN_CHARS // len(fixtures._LAMBDA_GUIDE))
    patterns = [re.sub(r"\d+", "#", s) for s in re.split(r"[。\n]", text) if len(s) > 12]

    most_common = Counter(patterns).most_common(1)[0]
    assert most_common[1] <= copies, (
        f"句式「{most_common[0][:24]}…」出现 {most_common[1]} 次，超过文档份数 {copies}")


def test_cache_rounds_use_streaming_with_a_real_question():
    """缓存用例必须走流式：延迟分位的主指标是首字延迟，非流式量不出来。

    请求体形状对齐 logs/缓存提示词.md：长文档放 system 带 cache_control，
    真实提问放 user，max_tokens=1024。
    """
    calls: list[dict] = []

    class _StreamOnly:
        def stream_message(self, payload):
            calls.append(payload)
            return StreamResponse(status_code=200, ok=True, text="按量计费。",
                                  first_text_seconds=0.4, total_seconds=1.5,
                                  usage={"cache_read_input_tokens": 4096})

        def create_message(self, payload):        # 一旦走非流式就报错
            raise AssertionError("缓存用例必须走流式")

    config.CACHE_SEED_WAIT_SECONDS = 0
    config.CACHE_HIT_ROUNDS = 3
    caching._cache_hit_rate(_StreamOnly(), "claude-opus-5", ttl="1h")

    assert len(calls) == 4                                   # 1 种子轮 + 3 读取轮
    first = calls[0]
    assert first["max_tokens"] == 1024
    assert first["system"][0]["cache_control"] == {"type": "ephemeral", "ttl": "1h"}
    assert caching.CACHE_QUESTION in first["messages"][0]["content"]


def test_five_minute_case_sends_no_ttl_field():
    """5 分钟档用默认 ephemeral（不带 ttl），1 小时档才带 ttl=1h。"""
    calls: list[dict] = []

    class _Recorder:
        def stream_message(self, payload):
            calls.append(payload)
            return StreamResponse(status_code=200, ok=True, text="OK", first_text_seconds=0.3,
                                  total_seconds=1.0, usage={"cache_read_input_tokens": 4096})

    config.CACHE_SEED_WAIT_SECONDS = 0
    config.CACHE_HIT_ROUNDS = 1
    caching._cache_hit_rate(_Recorder(), "claude-opus-5", ttl=None)

    assert calls[0]["system"][0]["cache_control"] == {"type": "ephemeral"}
