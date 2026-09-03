#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""Prompt Caching 类用例：用"缓存命中率"衡量 5 分钟 / 1 小时缓存链路。

判定方式：
  1. 种子轮：第一次请求把超长系统提示词写进缓存（cache_creation_input_tokens > 0）；
  2. 读取轮：并发发 config.CACHE_HIT_ROUNDS 次前缀完全相同的请求；
  3. 命中率 = Σcache_read ÷ (Σcache_read + Σcache_creation)，**种子轮和读取轮全都计入**，
     ≥ config.CACHE_HIT_PASS_RATIO（默认 85%）即通过。

★ 为什么按 token 算而不是按轮次算：
  官方多轮缓存里，**同一次请求可以既读缓存又写新缓存**（文档示例：
  cache_read_input_tokens=1800 的同时 cache_creation_input_tokens=248）。
  按"这一轮 read>0 就算命中"来数轮次，会把"命中了但又重写了一大半前缀"
  当成满分，掩盖真实的缓存复用率。token 口径能如实反映"可缓存的输入里，
  有多大比例真的是从缓存读出来的"。
  Anthropic 官方没有定义命中率公式（只说 "Regularly analyze cache hit rates"），
  cache_read / (cache_read + cache_creation) 是业界通行口径。

种子轮那笔写入也计入分母——1.25x 的写入费是真花掉的，算进去才是完整的 token 账。
副作用：完美资源的理论上限因此是 rounds/(rounds+1)（20 轮 → 95.2%），
所以 CACHE_HIT_ROUNDS 不要调太小，否则完美资源也会低于阈值。
理论上限一并写进报告指标，避免看数据时困惑。
HTTP 失败的轮次（限流/网络错误）不计入统计，只在报告里单列；
成功轮次不足一半时直接判不通过，避免样本太少得出虚高命中率。

★ 被缓存的系统提示词带「本次运行标记」（时间戳 + 随机后缀）：
  固定文本会让 TTL 内重跑脚本时，种子轮直接命中上一次运行残留的缓存
  （cache_creation=0），测不出真实的写入→读取链路。加运行标记后每次运行
  都是全新的缓存 key。TTL 标记同理，让 5 分钟档和 1 小时档互不串用。

★ 这批调用同时充当响应延迟分位（P50/P95/P99）的样本来源——负载最整齐，
  跨模型可直接横向比较，详见 cases/latency.py。
"""

from __future__ import annotations

import datetime
import time
import uuid
from concurrent.futures import ThreadPoolExecutor
from dataclasses import dataclass

import config
import fixtures
from client import Client, build_curl
from . import latency
from .base import TestCase, TestOutcome, is_refusal, ok, fail, safe_run, skipped

CATEGORY = "提示词缓存"


def _new_run_nonce() -> str:
    """本次运行的唯一标记：时间戳便于人看，随机后缀防同秒重跑撞车。"""
    return f"{datetime.datetime.now():%Y%m%d-%H%M%S}-{uuid.uuid4().hex[:8]}"


# 进程启动时算一次，本次运行的所有轮次共用——种子轮和读取轮必须逐字一致才能命中。
# 作用：把本次运行的缓存 key 和上一次运行区分开。否则在 5 分钟 / 1 小时 TTL 内
# 重跑脚本（比如上一次跑一半手动中断了），"种子轮"会直接命中上次残留的缓存，
# cache_creation 为 0，测到的就不是真实的「写入 → 读取」链路。
RUN_CACHE_NONCE = _new_run_nonce()


def cached_system_text(ttl_label: str) -> str:
    """被缓存的系统提示词：TTL 标记 + 长文档 + TTL 尾注。

    两档必须逐字不同，否则后跑的那档会直接读到前一档写好的缓存，
    它的种子轮就测不出"创建"行为，命中率也就失去意义。

    首尾各放一处 TTL 标记：
      - 开头：缓存是前缀匹配，从第一个字节就分叉，最彻底；
      - 结尾：万一中转层剥掉或归一化了开头的标记行，尾部还能兜住。
        （logs/缓存提示词.md 里的参考请求就是靠结尾差一个字符来区分两档的。）
    运行标记则保证跟上一次运行的缓存也不串。
    """
    return (f"[缓存测试运行标记 {RUN_CACHE_NONCE} / TTL={ttl_label}]\n"
            + fixtures.long_system_prompt()
            + f"\n\n（以上文档用于 {ttl_label} 缓存档位测试，与其他档位不共用缓存；"
              f"运行标记 {RUN_CACHE_NONCE}）")


CASE_5M = "AN-CACHE-001"
CASE_1H = "AN-CACHE-002"
# 报告的「各模型缓存命中率汇总」按这个顺序出列
TTL_LABELS = {CASE_5M: "5分钟", CASE_1H: "1小时"}

# metrics 里供报告汇总读取的键名（改名要同步 report.py 的缓存汇总块）
# 读取轮的提问：真实问题（对齐 logs/缓存提示词.md），比"请只回复 OK"更接近实际用法。
# 问题内容跟在缓存断点之后，不影响缓存 key。
CACHE_QUESTION = "Lambda 的定价模型是什么？请只用两句话回答。"

METRIC_HIT_RATE = "命中率(读取÷(读取+写入))"
METRIC_READ_TOKENS = "cache_read合计(含种子轮)"
METRIC_CREATION_TOKENS = "cache_creation合计(含种子轮)"
METRIC_MAX_RATE = "理论上限(种子轮写入不可避免)"
METRIC_HITS = "命中轮次(read>0，仅供参考)"
METRIC_UNCACHED_ROUNDS = "未走缓存轮次(读写全为0)"
METRIC_REFUSED_ROUNDS = "被拒轮次(stop_reason=refusal)"
METRIC_REFUSED_WASTE = "被拒轮白写入的cache_creation"
METRIC_OK_ROUNDS = "成功轮次"
METRIC_FAILED_ROUNDS = "失败轮次"


@dataclass
class _Round:
    """一次读取轮的结果。"""

    index: int
    status_code: int | None
    cache_read: int
    cache_creation: int
    error: str = ""
    refused: bool = False          # HTTP 200 但被安全分类器拒答

    @property
    def succeeded(self) -> bool:
        return self.error == "" and not self.refused

    @property
    def hit(self) -> bool:
        return self.succeeded and self.cache_read > 0


def _cache_usage(usage: dict) -> tuple[int, int]:
    """返回 (cache_creation, cache_read)。"""
    return (usage.get("cache_creation_input_tokens") or 0,
            usage.get("cache_read_input_tokens") or 0)


def _cache_hit_rate(client: Client, model: str, ttl: str | None) -> TestOutcome:
    """种子轮 + N 轮并发读取，统计缓存命中率。ttl=None 表示 5 分钟默认缓存。"""
    cache_control = {"type": "ephemeral"}
    label = "5分钟"
    if ttl:
        cache_control["ttl"] = ttl
        label = "1小时"

    system_block = [
        {"type": "text", "text": cached_system_text(label), "cache_control": cache_control}
    ]
    rounds = max(1, int(config.CACHE_HIT_ROUNDS))
    threshold = float(config.CACHE_HIT_PASS_RATIO)
    max_rate = rounds / (rounds + 1)
    expected = (f"{label}缓存命中率 ≥ {threshold:.0%}"
                f"（命中率 = cache_read ÷ (cache_read + cache_creation)，含种子轮；"
                f"种子轮写入 + {rounds} 轮读取，理论上限 {max_rate:.1%}）")

    def make_payload(user_msg: str) -> dict:
        return {
            "model": model,
            "max_tokens": 1024,
            "system": system_block,
            "messages": [{"role": "user", "content": user_msg}],
        }

    def timed_call(user_msg: str):
        """发一次**流式**请求并登记耗时，供响应延迟分位统计取样。

        走流式有两个原因：一是真实业务基本都用流式；二是延迟分位的主指标是
        首字延迟，非流式根本量不出来（只能拿到被输出长度主导的总耗时）。
        流式响应的 message_start 里带 cache_read / cache_creation，缓存统计不受影响。
        """
        resp = client.stream_message(make_payload(user_msg))
        latency.record(model, resp.first_text_seconds, succeeded=bool(resp.ok),
                       total_seconds=resp.total_seconds)
        return resp

    # ---- 种子轮（缓存创建 / 预热） ----
    seed_payload = make_payload(CACHE_QUESTION)
    # 完整写入 curl：超长系统提示词不省略，cache_control 缓存标记原样保留
    curl = build_curl({**seed_payload, "stream": True})
    seed = timed_call(CACHE_QUESTION)
    if not seed.ok:
        return fail(expected, f"HTTP {seed.status_code}: {seed.error_message}",
                    "种子轮（缓存创建）请求失败，后续命中率无法测量",
                    resp=seed, curl=curl, status_code=seed.status_code)
    seed_creation, seed_read = _cache_usage(seed.usage)
    if is_refusal(seed):
        # 分类器对同一段输入的判定是确定的：种子轮被拒 = 后面每一轮都会被拒，
        # 而每一轮又都会上报一次 cache_creation。直接收工，别白烧缓存写入费。
        return skipped(
            expected,
            f"种子轮 stop_reason=refusal（content 为空），已跳过 {rounds} 轮读取；"
            f"被拒请求仍上报 cache_creation={seed_creation} token",
            f"种子轮被安全分类器拒答，本次未能测量{label}缓存链路；"
            f"已跳过后续 {rounds} 轮读取，避免重复产生缓存写入费用",
            resp=seed, curl=curl, status_code=seed.status_code,
            metrics={"运行标记": RUN_CACHE_NONCE,
                     "种子轮_cache_creation": seed_creation,
                     "种子轮_cache_read": seed_read,
                     METRIC_REFUSED_ROUNDS: 1,
                     METRIC_REFUSED_WASTE: seed_creation})

    # 等缓存落地，否则紧随其后的读取轮可能读不到
    time.sleep(max(0.0, float(config.CACHE_SEED_WAIT_SECONDS)))

    # ---- 读取轮（并发） ----
    def probe(index: int) -> _Round:
        try:
            resp = timed_call(f"{CACHE_QUESTION}（第 {index} 次提问）")
        except Exception as exc:  # noqa: BLE001 —— 单轮异常不该毁掉整个命中率统计
            latency.record(model, None, succeeded=False)
            return _Round(index, None, 0, 0, error=f"{type(exc).__name__}: {exc}")
        creation, read = _cache_usage(resp.usage)
        error = "" if resp.ok else (resp.error_message or f"HTTP {resp.status_code}")
        return _Round(index, resp.status_code, read, creation,
                      error=error, refused=is_refusal(resp))

    workers = max(1, min(int(config.CACHE_HIT_MAX_WORKERS), rounds))
    with ThreadPoolExecutor(max_workers=workers) as pool:
        results = sorted(pool.map(probe, range(1, rounds + 1)), key=lambda r: r.index)

    succeeded = [r for r in results if r.succeeded]
    refused = [r for r in results if r.refused]
    failed = [r for r in results if r.error]
    hits = [r for r in succeeded if r.hit]
    # 被拒的轮次没产生有效缓存行为，但仍上报了 cache_creation ——
    # 这部分是纯浪费的写入费，单独统计出来给主人看。
    refused_waste = sum(r.cache_creation for r in refused)

    # 命中率按 token 算：可缓存的输入里，有多大比例真的是从缓存读出来的。
    # 同一轮既读又写时，读进分子、读+写进分母，能如实反映缓存复用率。
    # 种子轮也计入：它那笔写入是真金白银花掉的（1.25x），算进去才是完整的 token 账。
    read_tokens = seed_read + sum(r.cache_read for r in succeeded)
    creation_tokens = seed_creation + sum(r.cache_creation for r in succeeded)
    # 有的轮次 cache_read 和 cache_creation 全是 0——资源根本没把这次请求当可缓存的。
    # 这种轮在 token 口径下会"隐身"，反而抬高命中率，所以按种子轮观测到的前缀长度
    # 折算成"本该命中却没命中"计入分母。
    prefix_tokens = seed_creation or seed_read
    uncached_rounds = [r for r in succeeded if not r.cache_read and not r.cache_creation]
    missed_tokens = prefix_tokens * len(uncached_rounds)
    cacheable_tokens = read_tokens + creation_tokens + missed_tokens
    hit_rate = (read_tokens / cacheable_tokens) if cacheable_tokens else 0.0

    metrics = {
        "运行标记": RUN_CACHE_NONCE,
        "种子轮_cache_creation": seed_creation,
        "种子轮_cache_read": seed_read,
        "读取轮次": rounds,
        METRIC_OK_ROUNDS: len(succeeded),
        METRIC_READ_TOKENS: read_tokens,
        METRIC_CREATION_TOKENS: creation_tokens,
        METRIC_HIT_RATE: f"{hit_rate:.1%}",
        METRIC_MAX_RATE: f"{max_rate:.1%}",
        METRIC_HITS: len(hits),
        METRIC_UNCACHED_ROUNDS: len(uncached_rounds),
        METRIC_REFUSED_ROUNDS: len(refused),
        METRIC_REFUSED_WASTE: refused_waste,
        "通过阈值": f"{threshold:.0%}",
        METRIC_FAILED_ROUNDS: len(failed),
    }
    detail = "；".join(
        f"#{r.index} read={r.cache_read},create={r.cache_creation}" if r.succeeded
        else (f"#{r.index} 被拒(refusal,create={r.cache_creation})" if r.refused
              else f"#{r.index} 失败({r.error})")
        for r in results
    )
    seed_note = ("" if seed_creation else
                 "；⚠️ 种子轮未写入缓存（cache_creation=0），"
                 "本次运行标记没能区分出新缓存 key，命中率可能偏高")
    uncached_note = (f"，另有 {len(uncached_rounds)} 轮完全未走缓存，"
                     f"按前缀 {prefix_tokens} token/轮 折算 {missed_tokens} 计入分母"
                     if uncached_rounds else "")
    actual = (f"种子轮 creation={seed_creation}, read={seed_read}{seed_note}；"
              f"含种子轮合计 read={read_tokens}, creation={creation_tokens}{uncached_note}；"
              f"命中率 {read_tokens}/{cacheable_tokens}={hit_rate:.1%}"
              f"（理论上限 {max_rate:.1%}）"
              f"（read>0 的轮次 {len(hits)}/{len(succeeded)}，"
              f"被拒 {len(refused)} 轮，失败 {len(failed)} 轮）\n{detail}")

    if refused and len(succeeded) * 2 < rounds:
        return skipped(expected, actual,
                       f"{len(refused)}/{rounds} 轮被安全分类器拒答，有效样本不足，"
                       f"本次未能测量{label}缓存链路；被拒轮白写入 {refused_waste} token",
                       resp=seed, curl=curl, status_code=seed.status_code, metrics=metrics)
    if len(succeeded) * 2 < rounds:
        return fail(expected, actual,
                    f"有效样本不足（仅 {len(succeeded)}/{rounds} 轮成功），命中率结论不可信",
                    resp=seed, curl=curl, status_code=seed.status_code, metrics=metrics)
    if not cacheable_tokens:
        return fail(expected, actual,
                    f"{len(succeeded)} 轮读取里 cache_read 和 cache_creation 全为 0，"
                    "未观测到任何缓存读写（检查资源是否透传 cache_control）",
                    resp=seed, curl=curl, status_code=seed.status_code, metrics=metrics)

    passed = hit_rate >= threshold
    return (ok if passed else fail)(
        expected, actual,
        f"{label}缓存命中率 {hit_rate:.1%}，达到 {threshold:.0%} 阈值" if passed
        else f"{label}缓存命中率仅 {hit_rate:.1%}，低于 {threshold:.0%}"
             f"（{creation_tokens} token 被重复写入缓存、{missed_tokens} token 完全未走缓存）",
        resp=seed, curl=curl, status_code=seed.status_code, metrics=metrics)


def _cache_5m(client: Client, model: str) -> TestOutcome:
    return _cache_hit_rate(client, model, ttl=None)


def _cache_1h(client: Client, model: str) -> TestOutcome:
    return _cache_hit_rate(client, model, ttl="1h")


CASES = [
    TestCase(CASE_5M, CATEGORY, "5分钟缓存命中率", "P0", safe_run(_cache_5m)),
    TestCase(CASE_1H, CATEGORY, "1小时缓存命中率", "P0", safe_run(_cache_1h)),
]
