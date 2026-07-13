#!/usr/bin/env python3
"""高并发流式压测脚本 - /v1/chat/completions 接口

判断在高并发下，有多少个请求能拿到完整的流式输出（即最终收到含 usage 字段的 chunk）。
测试完会把结果写入一个 txt 文件。

用法：
    pip3 install aiohttp
    python3 test_concurrent_stream.py
"""

import asyncio
import aiohttp
import time
import statistics
import json
import random
import base64
import os
import sys
from datetime import datetime

# ============== 配置 ==============
URL = "https://api.gravitex.ai/v1/chat/completions"
API_KEY = "sk-feUqFQxT7Q4ySkJbyYXzlGHuY4Y0uTluQOW0RqcXddRbZ0Fb"
MODEL = "gpt-5.4-nano"
TOTAL_REQUESTS = 10000        # 总请求数
REQUESTS_PER_SECOND = 100    # 每秒请求数
WAIT_MIN_MS = 10000           # 等待时间下限 (ms) — 拼在 URL 末尾传给测试接口
WAIT_MAX_MS = 20000           # 等待时间上限 (ms)
REQUEST_TIMEOUT = 600         # 单个请求超时时间 (秒)
MAX_TOKENS = 64               # 生成 token 上限（越小上游越快返回）
ENABLE_IMAGE = False          # 是否携带图片
IMAGE_URL = ""                # 图片地址（启用图片时必填, 本地路径或 URL 都可）
# IMAGE_URL = "/Users/caihongzhan/Desktop/sucai/下载.png"
OUTPUT_FILE = f"stream_test_result_{datetime.now().strftime('%Y%m%d_%H%M%S')}.txt"
# ==================================


async def fetch_image_base64(url):
    """读取图片并转为 base64 字符串，支持本地路径和远程 URL"""
    if os.path.isfile(url):
        with open(url, "rb") as f:
            return base64.b64encode(f.read()).decode("utf-8")
    async with aiohttp.ClientSession() as session:
        async with session.get(url) as resp:
            data = await resp.read()
            return base64.b64encode(data).decode("utf-8")


def build_url(wait_ms):
    """拼上 wait_ms 后缀（保持与非流式脚本一致的测试接口约定）"""
    return f"{URL}/{wait_ms}"


async def send_stream_request(session, request_id, headers, body):
    """发送一次流式请求，逐块解析 SSE，判断是否拿到完整 usage 信息"""
    wait_ms = random.randint(WAIT_MIN_MS, WAIT_MAX_MS)
    url = build_url(wait_ms)
    start = time.monotonic()
    first_chunk_time = None
    chunk_count = 0
    got_usage = False
    got_done = False
    usage_data = None
    finish_reason = None
    status_code = 0
    error_msg = None

    try:
        async with session.post(
            url,
            json=body,
            headers=headers,
            timeout=aiohttp.ClientTimeout(total=REQUEST_TIMEOUT),
        ) as resp:
            status_code = resp.status
            if resp.status != 200:
                text = await resp.text()
                error_msg = f"HTTP {resp.status}: {text[:200]}"
            else:
                async for raw_line in resp.content:
                    if not raw_line:
                        continue
                    line = raw_line.decode("utf-8", errors="replace").strip()
                    if not line or not line.startswith("data:"):
                        continue
                    data_str = line[5:].strip()
                    if data_str == "[DONE]":
                        got_done = True
                        continue

                    if first_chunk_time is None:
                        first_chunk_time = time.monotonic()
                    chunk_count += 1

                    try:
                        chunk = json.loads(data_str)
                    except json.JSONDecodeError:
                        continue

                    usage = chunk.get("usage")
                    if usage:
                        got_usage = True
                        usage_data = usage

                    choices = chunk.get("choices") or []
                    if choices:
                        fr = choices[0].get("finish_reason")
                        if fr:
                            finish_reason = fr
    except asyncio.TimeoutError:
        error_msg = "超时"
    except Exception as e:
        error_msg = f"{type(e).__name__}: {str(e)[:200]}"

    elapsed = time.monotonic() - start
    ttfb = (first_chunk_time - start) if first_chunk_time else None

    return {
        "id": request_id,
        "status": status_code,
        "elapsed": elapsed,
        "ttfb": ttfb,
        "wait_ms": wait_ms,
        "chunk_count": chunk_count,
        "got_usage": got_usage,
        "got_done": got_done,
        "finish_reason": finish_reason,
        "usage": usage_data,
        "error": error_msg,
    }


def classify(r):
    """把每个请求归到三类：complete / partial / failed"""
    if r["error"] is not None or r["status"] != 200:
        return "failed"
    if r["got_usage"]:
        return "complete"
    return "partial"


def build_report(results, test_elapsed):
    """把统计结果拼成一个多行字符串（同时用于 stdout 和文件）"""
    lines = []
    add = lines.append

    total = len(results)
    complete = [r for r in results if classify(r) == "complete"]
    partial = [r for r in results if classify(r) == "partial"]
    failed = [r for r in results if classify(r) == "failed"]

    add("=" * 60)
    add("流式压测结果")
    add("=" * 60)
    add(f"请求地址:             {URL}")
    add(f"模型:                 {MODEL}")
    add(f"总请求数:             {total}")
    add(f"每秒请求数:           {REQUESTS_PER_SECOND}")
    add(f"等待时间范围(ms):     {WAIT_MIN_MS} ~ {WAIT_MAX_MS}")
    add(f"请求超时(s):          {REQUEST_TIMEOUT}")
    add(f"max_tokens:           {MAX_TOKENS}")
    add(f"携带图片:             {'是' if ENABLE_IMAGE else '否'}")
    if ENABLE_IMAGE:
        add(f"图片地址:             {IMAGE_URL}")
    add(f"测试完成时间:         {datetime.now().strftime('%Y-%m-%d %H:%M:%S')}")
    add(f"总耗时:               {test_elapsed:.2f}s")
    add("")
    add("-" * 60)
    add("结果分类（以是否拿到 usage 为完整流式的判定标准）")
    add("-" * 60)
    add(f"完整流式(有 usage):   {len(complete)}  ({len(complete) / total * 100:.2f}%)")
    add(f"不完整(无 usage):     {len(partial)}  ({len(partial) / total * 100:.2f}%)")
    add(f"失败:                 {len(failed)}  ({len(failed) / total * 100:.2f}%)")
    add(f"完整率:               {len(complete) / total * 100:.2f}%")

    # 完整流式的耗时分布
    if complete:
        elapsed_times = [r["elapsed"] for r in complete]
        ttfbs = [r["ttfb"] for r in complete if r["ttfb"] is not None]
        chunk_counts = [r["chunk_count"] for r in complete]

        add("")
        add("-" * 60)
        add("完整流式请求耗时分布")
        add("-" * 60)
        sorted_e = sorted(elapsed_times)
        add(f"最小耗时:             {min(elapsed_times):.3f}s")
        add(f"最大耗时:             {max(elapsed_times):.3f}s")
        add(f"平均耗时:             {statistics.mean(elapsed_times):.3f}s")
        add(f"中位数:               {statistics.median(elapsed_times):.3f}s")
        add(f"P95 耗时:             {sorted_e[int(len(sorted_e) * 0.95)]:.3f}s")
        add(f"P99 耗时:             {sorted_e[min(int(len(sorted_e) * 0.99), len(sorted_e) - 1)]:.3f}s")
        if len(elapsed_times) >= 2:
            add(f"标准差:               {statistics.stdev(elapsed_times):.3f}s")
        add(f"吞吐量:               {len(complete) / test_elapsed:.2f} req/s")

        if ttfbs:
            sorted_t = sorted(ttfbs)
            add("")
            add(f"TTFB 最小:            {min(ttfbs):.3f}s")
            add(f"TTFB 最大:            {max(ttfbs):.3f}s")
            add(f"TTFB 平均:            {statistics.mean(ttfbs):.3f}s")
            add(f"TTFB 中位数:          {statistics.median(ttfbs):.3f}s")
            add(f"TTFB P95:             {sorted_t[int(len(sorted_t) * 0.95)]:.3f}s")
            add(f"TTFB P99:             {sorted_t[min(int(len(sorted_t) * 0.99), len(sorted_t) - 1)]:.3f}s")

        add("")
        add(f"平均 chunk 数:        {statistics.mean(chunk_counts):.1f}")
        add(f"最少 chunk 数:        {min(chunk_counts)}")
        add(f"最多 chunk 数:        {max(chunk_counts)}")

        # usage 聚合（如果字段存在）
        prompt_tokens = [r["usage"].get("prompt_tokens", 0) for r in complete if r["usage"]]
        completion_tokens = [r["usage"].get("completion_tokens", 0) for r in complete if r["usage"]]
        total_tokens = [r["usage"].get("total_tokens", 0) for r in complete if r["usage"]]
        if prompt_tokens:
            add("")
            add("-" * 60)
            add("Usage 聚合（仅完整流式请求）")
            add("-" * 60)
            add(f"prompt_tokens     累计: {sum(prompt_tokens)}     平均: {statistics.mean(prompt_tokens):.2f}")
            add(f"completion_tokens 累计: {sum(completion_tokens)} 平均: {statistics.mean(completion_tokens):.2f}")
            add(f"total_tokens      累计: {sum(total_tokens)}      平均: {statistics.mean(total_tokens):.2f}")

    # 不完整流式详情
    if partial:
        add("")
        add("-" * 60)
        add(f"不完整流式请求样本（HTTP 200 但没拿到 usage） - 共 {len(partial)} 个")
        add("-" * 60)
        # 按 finish_reason 分组
        fr_groups = {}
        for r in partial:
            key = f"finish_reason={r['finish_reason'] or 'None'}, got_done={r['got_done']}, chunks={r['chunk_count']}"
            fr_groups[key] = fr_groups.get(key, 0) + 1
        for k, v in sorted(fr_groups.items(), key=lambda x: -x[1]):
            add(f"  {k}: {v}")

    # 失败详情
    if failed:
        add("")
        add("-" * 60)
        add(f"失败请求 - 共 {len(failed)} 个")
        add("-" * 60)
        error_groups = {}
        for r in failed:
            key = f"HTTP {r['status']}" if r["error"] is None else r["error"][:120]
            error_groups[key] = error_groups.get(key, 0) + 1
        for err, count in sorted(error_groups.items(), key=lambda x: -x[1]):
            add(f"  {err}: {count}")

    return "\n".join(lines)


async def main():
    headers = {
        "Authorization": f"Bearer {API_KEY}",
        "Content-Type": "application/json",
        "Accept": "text/event-stream",
    }

    print(f"请求地址:             {URL}")
    print(f"模型:                 {MODEL}")
    print(f"总请求数:             {TOTAL_REQUESTS}")
    print(f"每秒请求数:           {REQUESTS_PER_SECOND}")
    print(f"等待时间范围(ms):     {WAIT_MIN_MS} ~ {WAIT_MAX_MS}")
    print(f"请求超时(s):          {REQUEST_TIMEOUT}")
    print(f"max_tokens:          {MAX_TOKENS}")
    print(f"携带图片:             {'是' if ENABLE_IMAGE else '否'}")
    if ENABLE_IMAGE:
        print(f"图片地址:             {IMAGE_URL}")
    print(f"输出文件:             {OUTPUT_FILE}")
    print("=" * 60)

    # 构造请求体（可选图片）
    if ENABLE_IMAGE:
        print("下载图片并转换 base64...")
        img_b64 = await fetch_image_base64(IMAGE_URL)
        messages = [{
            "role": "user",
            "content": [
                {"type": "text", "text": "描述这张图片"},
                {"type": "image_url", "image_url": {"url": f"data:image/png;base64,{img_b64}"}},
            ],
        }]
        print(f"图片 base64 大小:     {len(img_b64)} 字符")
    else:
        messages = [{"role": "user", "content": "HI"}]

    body = {
        "model": MODEL,
        "messages": messages,
        "max_tokens": MAX_TOKENS,
        "stream": True,
        "stream_options": {"include_usage": True},
    }

    interval = 1.0 / REQUESTS_PER_SECOND
    tasks = []

    connector = aiohttp.TCPConnector(limit=TOTAL_REQUESTS, limit_per_host=TOTAL_REQUESTS)
    async with aiohttp.ClientSession(connector=connector) as session:
        test_start = time.monotonic()

        for i in range(TOTAL_REQUESTS):
            task = asyncio.create_task(send_stream_request(session, i, headers, body))
            tasks.append(task)

            if i < TOTAL_REQUESTS - 1:
                await asyncio.sleep(interval)

            if (i + 1) % max(1, TOTAL_REQUESTS // 10) == 0:
                print(f"  已发送 {i + 1}/{TOTAL_REQUESTS} 个请求...")

        print("等待所有响应返回...")
        pending = set(tasks)
        last_print = time.monotonic()
        while pending:
            _, pending = await asyncio.wait(pending, timeout=5)
            now = time.monotonic()
            if now - last_print >= 5:
                completed_count = TOTAL_REQUESTS - len(pending)
                print(f"  已完成 {completed_count}/{TOTAL_REQUESTS}，耗时 {now - test_start:.1f}s...")
                last_print = now

        results = [t.result() for t in tasks]
        test_elapsed = time.monotonic() - test_start

    report = build_report(results, test_elapsed)
    print()
    print(report)

    try:
        with open(OUTPUT_FILE, "w", encoding="utf-8") as f:
            f.write(report)
            f.write("\n")
        print()
        print(f"结果已保存到: {OUTPUT_FILE}")
    except Exception as e:
        print(f"写入结果文件失败: {e}")

    # 有失败则非零退出
    failed_count = sum(1 for r in results if classify(r) == "failed")
    if failed_count > 0:
        sys.exit(1)


if __name__ == "__main__":
    asyncio.run(main())
