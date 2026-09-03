#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
Claude 平台延迟测试脚本 —— P50 / P95
====================================

只测一件事：/v1/messages 非流式请求的端到端耗时（发出请求到收到完整响应）。
对 MODELS 里的每个模型先跑 WARMUP_REQUESTS 次热身（丢弃），
再串行发送 REQUESTS_PER_MODEL 次请求，统计 P50 / P95 / 平均 / 最小 / 最大延迟。
P50/P95 只统计成功请求；失败请求单独计数，不污染延迟分布。

用法：
    python latency_test.py

依赖：仅 requests（pip install requests）
"""

from __future__ import annotations

import csv
import statistics
import threading
import time
from dataclasses import dataclass, field

import requests

# =============================================================================
# 配置项（换平台/模型只改这里）
# =============================================================================
PLATFORM_NAME = "gravitex"

# 接口地址（不要带末尾斜杠）
BASE_URL = "https://api.gravitex.ai"

# 鉴权方式：
#   "anthropic" -> 请求头 x-api-key + anthropic-version
#   "bearer"    -> 请求头 Authorization: Bearer <key>
AUTH_MODE = "bearer"

# 密钥（两种鉴权方式都用这一个值）
# 占位符，**不要把真实 key 写进来**（会进 git 历史）。用之前改成自己的 token。
API_KEY = "sk-REPLACE_ME"

# anthropic-version 头的值
ANTHROPIC_VERSION = "2023-06-01"

# 待测模型列表（数组形式，逐个串行跑完）
MODELS = [
    "claude-opus-5",
    "claude-fable-5"
]

# 每个模型发送的请求次数（次数越多，P95 越可信；P50/P95 只统计成功请求，失败单独计数）
REQUESTS_PER_MODEL = 50

# 每个模型正式计时前先跑几次热身请求（结果丢弃，不计入统计）
# 目的：去掉首次 TCP/TLS 握手、上游冷启动带来的偏差，让 P50/P95 更能反映稳态延迟
WARMUP_REQUESTS = 3

# 测试用的输入内容 / 输出上限（越小越省钱，且减少"生成耗时"对纯链路延迟的干扰）
PROMPT_TEXT = "请用一句话回答：1+1等于几？"
MAX_TOKENS = 32

# 推理强度档位：""(不传，用模型默认) / "low" / "medium" / "high" / "xhigh" / "max"
# 对应官方 output_config.effort 参数。Opus 5 系列默认是 high；调成 low 能显著压低延迟，
# 但会牺牲一部分推理质量（仅部分模型支持，Fable 5 是 always-on 自适应思考，可能不认这个参数）
EFFORT_LEVEL = "low"

# 是否显式禁用扩展思考：payload 里加 "thinking": {"type": "disabled"}
# 注意（官方文档）：Opus 5 上如果 effort 是 xhigh/max，disabled 会返回 400 错误；
# 搭配 low/medium/high（或不传 effort）通常可用。跟上面的 EFFORT_LEVEL 一起用时注意这个限制。
THINKING_DISABLED = True

# 单个请求超时时间（秒）
TIMEOUT_SECONDS = 60

# 同一模型两次请求之间的间隔（秒），避免触发限流
REQUEST_INTERVAL_SECONDS = 0.5

# 结果明细 CSV 文件名（留空则不导出）
REPORT_FILENAME = f"latency-report-{PLATFORM_NAME}.csv"

# 是否让 MODELS 里的多个模型同时并发跑（互不等待，整体更快出结果）
# 注意：这只影响"不同模型之间"是否并行；同一个模型内部仍然是串行发请求，
# 单个模型的 P50/P95 不会被"同模型并发排队"污染。
RUN_MODELS_IN_PARALLEL = False


# =============================================================================
# 以下为脚本逻辑，一般不需要修改
# =============================================================================
@dataclass
class RequestResult:
    ok: bool
    status: int
    elapsed: float
    error: str = ""


@dataclass
class ModelLatencyStats:
    model: str
    latencies: list[float] = field(default_factory=list)
    success: int = 0
    failed: int = 0
    errors: dict[str, int] = field(default_factory=dict)

    @property
    def total(self) -> int:
        return self.success + self.failed

    @property
    def success_rate(self) -> float:
        return self.success / self.total if self.total else 0.0

    def percentile(self, pct: float) -> float:
        if not self.latencies:
            return 0.0
        data = sorted(self.latencies)
        if len(data) == 1:
            return data[0]
        quantiles = statistics.quantiles(data, n=100, method="inclusive")
        idx = max(0, min(99, int(round(pct)) - 1))
        return quantiles[idx]

    @property
    def p50(self) -> float:
        return self.percentile(50)

    @property
    def p95(self) -> float:
        return self.percentile(95)

    @property
    def avg(self) -> float:
        return statistics.mean(self.latencies) if self.latencies else 0.0

    @property
    def min(self) -> float:
        return min(self.latencies) if self.latencies else 0.0

    @property
    def max(self) -> float:
        return max(self.latencies) if self.latencies else 0.0


_print_lock = threading.Lock()


def log(msg: str) -> None:
    """线程安全打印：并发跑多个模型时，避免不同线程的输出行互相截断交错。"""
    with _print_lock:
        print(msg)


def build_headers() -> dict[str, str]:
    headers = {"content-type": "application/json"}
    if AUTH_MODE == "bearer":
        headers["Authorization"] = f"Bearer {API_KEY}"
        if ANTHROPIC_VERSION:
            headers["anthropic-version"] = ANTHROPIC_VERSION
    else:
        headers["x-api-key"] = API_KEY
        headers["anthropic-version"] = ANTHROPIC_VERSION
    return headers


def build_payload(model: str) -> dict:
    payload = {
        "model": model,
        "max_tokens": MAX_TOKENS,
        "stream": False,
        "messages": [{"role": "user", "content": PROMPT_TEXT}],
    }
    if EFFORT_LEVEL:
        payload["output_config"] = {"effort": EFFORT_LEVEL}
    if THINKING_DISABLED:
        payload["thinking"] = {"type": "disabled"}
    return payload


def send_request(session: requests.Session, model: str) -> RequestResult:
    url = f"{BASE_URL}/v1/messages"
    headers = build_headers()
    payload = build_payload(model)
    start = time.perf_counter()
    try:
        resp = session.post(url, headers=headers, json=payload, timeout=TIMEOUT_SECONDS)
        elapsed = time.perf_counter() - start
        if resp.ok:
            return RequestResult(ok=True, status=resp.status_code, elapsed=elapsed)
        try:
            body = resp.json()
            err = (body or {}).get("error") or {}
            msg = err.get("message") or str(body)
        except ValueError:
            msg = resp.text
        return RequestResult(ok=False, status=resp.status_code, elapsed=elapsed, error=msg[:200])
    except requests.RequestException as e:
        elapsed = time.perf_counter() - start
        return RequestResult(ok=False, status=0, elapsed=elapsed, error=f"{type(e).__name__}: {e}"[:200])


def run_model(model: str) -> ModelLatencyStats:
    # 每个模型用独立 Session：并行跑多个模型时互不共享连接池，
    # 避免一个模型的连接复用/排队影响到另一个模型的延迟测量。
    session = requests.Session()

    for i in range(WARMUP_REQUESTS):
        result = send_request(session, model)
        log(f"  [{model}] [热身 {i + 1}/{WARMUP_REQUESTS}] "
            f"{'OK ' if result.ok else 'ERR'} status={result.status} "
            f"耗时={result.elapsed:.3f}s（不计入统计）")
        if REQUEST_INTERVAL_SECONDS > 0:
            time.sleep(REQUEST_INTERVAL_SECONDS)

    stats = ModelLatencyStats(model=model)
    for i in range(REQUESTS_PER_MODEL):
        result = send_request(session, model)
        if result.ok:
            stats.success += 1
            stats.latencies.append(result.elapsed)  # P50/P95 只统计成功请求
        else:
            stats.failed += 1
            key = f"HTTP {result.status}" if not result.error else result.error
            stats.errors[key] = stats.errors.get(key, 0) + 1
        log(f"  [{model}] [{i + 1}/{REQUESTS_PER_MODEL}] "
            f"{'OK ' if result.ok else 'ERR'} status={result.status} "
            f"耗时={result.elapsed:.3f}s"
            + ("" if result.ok else f"  错误={result.error}"))
        if i < REQUESTS_PER_MODEL - 1 and REQUEST_INTERVAL_SECONDS > 0:
            time.sleep(REQUEST_INTERVAL_SECONDS)
    return stats


def print_summary(all_stats: list[ModelLatencyStats]) -> None:
    print("\n" + "=" * 88)
    print("延迟测试汇总（非流式 /v1/messages 端到端耗时，单位：秒）")
    print("=" * 88)
    header = f"{'模型':<32}{'成功/总数':<10}{'P50':<8}{'P95':<8}{'平均':<8}{'最小':<8}{'最大':<8}"
    print(header)
    print("-" * 88)
    for s in all_stats:
        print(f"{s.model:<32}{f'{s.success}/{s.total}':<10}"
              f"{s.p50:<8.3f}{s.p95:<8.3f}{s.avg:<8.3f}{s.min:<8.3f}{s.max:<8.3f}")
        if s.errors:
            for err, cnt in sorted(s.errors.items(), key=lambda x: -x[1]):
                print(f"    错误 x{cnt}: {err}")
    print("=" * 88)


def write_csv(all_stats: list[ModelLatencyStats]) -> None:
    if not REPORT_FILENAME:
        return
    with open(REPORT_FILENAME, "w", newline="", encoding="utf-8-sig") as f:
        writer = csv.writer(f)
        writer.writerow(["模型", "成功数", "总数", "成功率", "P50(s)", "P95(s)", "平均(s)", "最小(s)", "最大(s)"])
        for s in all_stats:
            writer.writerow([s.model, s.success, s.total, f"{s.success_rate:.1%}",
                              f"{s.p50:.3f}", f"{s.p95:.3f}", f"{s.avg:.3f}", f"{s.min:.3f}", f"{s.max:.3f}"])
        writer.writerow([])
        writer.writerow(["模型", "第几次请求", "耗时(s)"])
        for s in all_stats:
            for i, latency in enumerate(s.latencies, start=1):
                writer.writerow([s.model, i, f"{latency:.3f}"])
    print(f"\n明细已导出：{REPORT_FILENAME}")


def main() -> None:
    print("=" * 88)
    print(f"被测平台：{PLATFORM_NAME}")
    print(f"接口地址：{BASE_URL}/v1/messages")
    print(f"鉴权方式：{AUTH_MODE}")
    print(f"待测模型：{MODELS}")
    print(f"每模型请求数：{REQUESTS_PER_MODEL}（另有热身 {WARMUP_REQUESTS} 次不计入统计）  请求间隔：{REQUEST_INTERVAL_SECONDS}s")
    print(f"多模型并行：{RUN_MODELS_IN_PARALLEL}")
    print(f"effort 档位：{EFFORT_LEVEL or '(未传，用模型默认)'}")
    print(f"禁用扩展思考：{THINKING_DISABLED}")
    print("=" * 88)

    if RUN_MODELS_IN_PARALLEL and len(MODELS) > 1:
        results: dict[str, ModelLatencyStats] = {}

        def _worker(m: str) -> None:
            log(f"\n########## 开始测试模型：{m} ##########")
            results[m] = run_model(m)

        threads = [threading.Thread(target=_worker, args=(model,)) for model in MODELS]
        for t in threads:
            t.start()
        for t in threads:
            t.join()
        all_stats = [results[model] for model in MODELS]
    else:
        all_stats = []
        for model in MODELS:
            print(f"\n########## 测试模型：{model} ##########")
            all_stats.append(run_model(model))

    print_summary(all_stats)
    write_csv(all_stats)


if __name__ == "__main__":
    main()
