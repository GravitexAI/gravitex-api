#!/usr/bin/env python3
"""高并发压测脚本 - /v1/chat/completions 接口"""

  # 1. 装依赖（只需一次）
 # pip3 install aiohttp

  # 2. 改脚本顶部配置
 # vi test_concurrent.py

  # 3. 执行
 # python3 test_concurrent.py

#服务器上需要做的：

  # 1. 装 Python 依赖（只需一次）
 # pip3 install aiohttp

  # 2. 上传脚本
 # scp test_concurrent.py user@your-server:/path/to/

  # 3. 跑之前可以调一下内核参数（sudo）
 # sudo sysctl -w net.ipv4.ip_local_port_range="1024 65535"
 # sudo sysctl -w net.ipv4.tcp_tw_reuse=1
 # sudo sysctl -w net.core.somaxconn=65535

  # 4. 执行
 # python3 test_concurrent.py


import asyncio
import aiohttp
import time
import statistics
import random
import base64
import os
import sys

# ============== 配置 ==============
# URL = "http://101.47.158.99/v1/chat/completions"  # 生产IP
URL = "https://vke-api.gravitex.ai/v1/chat/completions"  # 测试环境
# URL = "https://api.gravitex.ai/v1/chat/completions"  # 生产
# API_KEY = "sk-feUqFQxT7Q4ySkJbyYXzlGHuY4Y0uTluQOW0RqcXddRbZ0Fb" # 生产-py脚本使用
API_KEY = "sk-mMxMBVAASP3S34Ib7Tm3YE5mNAO7PJwK94xNRBNTqoclSfae" # 测试环境
MODEL = "gpt-5.4-nano"
TOTAL_REQUESTS = 1000        # 总请求数
REQUESTS_PER_SECOND = 100    # 每秒请求数
WAIT_MIN_MS = 100          # 等待时间下限 (ms)
WAIT_MAX_MS = 200         # 等待时间上限 (ms)
REQUEST_TIMEOUT = 600       # 单个请求超时时间 (秒)
# ENABLE_IMAGE = False        # 是否携带图片
ENABLE_IMAGE = True        # 是否携带图片
# IMAGE_URL = ""              # 图片地址（启用图片时必填）
# IMAGE_URL = "/Users/caihongzhan/Desktop/sucai/下载.png"              # 图片地址（启用图片时必填, 本地、url都可）
IMAGE_URL = "http://gravitexgrayoss.tos-s3-accelerate.bytepluses.com.cn/2026/06/10/6352cd8d17f943f7b4ce4c6c8a3f215d.png"    # 图片地址（启用图片时必填, 本地、url都可）
ENABLE_METRICS = True      # 是否采集服务器 CPU/内存/Goroutine 指标
METRICS_INTERVAL = 2        # 指标采集间隔 (秒)
MAX_CONCURRENT = 500        # 最大并发上传数（仅限制 body 上传阶段的并发，body 传完立即释放；等待响应不占位）
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
    return f"{URL}/{wait_ms}"


def metrics_url():
    """构造 metrics 接口地址"""
    base = URL.rsplit("/v1/", 1)[0]
    return f"{base}/v1/test/metrics"


async def collect_metrics(stop_event, samples):
    """后台定时采集服务器指标（独立 session，避免被业务请求阻塞）"""
    url = metrics_url()
    async with aiohttp.ClientSession() as session:
        while not stop_event.is_set():
            try:
                async with session.get(url, timeout=aiohttp.ClientTimeout(total=5)) as resp:
                    if resp.status == 200:
                        data = await resp.json()
                        samples.append({
                            "time": time.monotonic(),
                            "hostname": data.get("hostname", ""),
                            "ip": data.get("ip", ""),
                            "cpu": data.get("cpu_usage", 0),
                            "mem": data.get("mem_usage", 0),
                            "heap_mb": data.get("heap_alloc_mb", 0),
                            "goroutine": data.get("num_goroutine", 0),
                            "gc": data.get("num_gc", 0),
                        })
            except Exception:
                pass
            # 可被 stop_event 立即中断的睡眠
            try:
                await asyncio.wait_for(stop_event.wait(), timeout=METRICS_INTERVAL)
            except asyncio.TimeoutError:
                pass


async def send_request(session, request_id, headers, body, semaphore):
    wait_ms = random.randint(WAIT_MIN_MS, WAIT_MAX_MS)
    url = build_url(wait_ms)
    # semaphore 仅控制 body 上传阶段的并发数，防止带宽饱和导致 ALB 408
    # body 发送完毕后立即释放空位，让下一个请求开始上传
    await semaphore.acquire()
    start = time.monotonic()
    try:
        resp = await session.post(url, json=body, headers=headers, timeout=aiohttp.ClientTimeout(total=REQUEST_TIMEOUT))
    except Exception as e:
        semaphore.release()
        elapsed = time.monotonic() - start
        error_msg = "超时" if isinstance(e, asyncio.TimeoutError) else str(e)
        return {"id": request_id, "status": 0, "elapsed": elapsed, "wait_ms": wait_ms, "error": error_msg}
    # body 已发送，释放 semaphore 空位（后续等待响应不再占用并发位）
    semaphore.release()
    try:
        await resp.read()
        elapsed = time.monotonic() - start
        return {
            "id": request_id,
            "status": resp.status,
            "elapsed": elapsed,
            "wait_ms": wait_ms,
            "error": None,
        }
    except asyncio.TimeoutError:
        elapsed = time.monotonic() - start
        return {"id": request_id, "status": 0, "elapsed": elapsed, "wait_ms": wait_ms, "error": "超时"}
    except Exception as e:
        elapsed = time.monotonic() - start
        return {"id": request_id, "status": 0, "elapsed": elapsed, "wait_ms": wait_ms, "error": str(e)}


async def main():
    headers = {
        "Authorization": f"Bearer {API_KEY}",
        "Content-Type": "application/json",
    }

    print(f"请求地址:             {URL}")
    print(f"模型:                 {MODEL}")
    print(f"总请求数:             {TOTAL_REQUESTS}")
    print(f"每秒请求数:           {REQUESTS_PER_SECOND}")
    print(f"等待时间范围(ms):     {WAIT_MIN_MS} ~ {WAIT_MAX_MS}")
    print(f"请求超时(s):          {REQUEST_TIMEOUT}")
    print(f"携带图片:             {'是' if ENABLE_IMAGE else '否'}")
    if ENABLE_IMAGE:
        print(f"图片地址:             {IMAGE_URL}")
    print(f"最大并发上传数:       {MAX_CONCURRENT}")
    print(f"采集指标:             {'是' if ENABLE_METRICS else '否'}")
    if ENABLE_METRICS:
        print(f"采集间隔(s):          {METRICS_INTERVAL}")
    print("=" * 60)

    results = []
    interval = 1.0 / REQUESTS_PER_SECOND

    # 构造请求体
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
        "max_tokens": 4,
        "stream": False,
    }

    # connector 连接池大小需要容纳所有在飞的连接（等待响应的连接不受 semaphore 限制）
    # semaphore 仅控制 body 上传并发，防止带宽饱和
    connector = aiohttp.TCPConnector(limit=TOTAL_REQUESTS, limit_per_host=TOTAL_REQUESTS)
    semaphore = asyncio.Semaphore(MAX_CONCURRENT)
    async with aiohttp.ClientSession(connector=connector) as session:
        # 启动指标采集（独立 session）
        stop_event = asyncio.Event()
        metric_samples = []
        metrics_task = None
        if ENABLE_METRICS:
            metrics_task = asyncio.create_task(collect_metrics(stop_event, metric_samples))
            print(f"指标采集已启动 ({metrics_url()})")

        test_start = time.monotonic()
        test_start_str = time.strftime("%Y-%m-%d %H:%M:%S")

        for i in range(TOTAL_REQUESTS):
            task = asyncio.create_task(send_request(session, i, headers, body, semaphore))
            task.add_done_callback(lambda t: None)
            results.append(task)

            # 限速：控制发送频率
            if i < TOTAL_REQUESTS - 1:
                await asyncio.sleep(interval)

            # 每 10% 打印进度
            if (i + 1) % max(1, TOTAL_REQUESTS // 10) == 0:
                print(f"  已发送 {i + 1}/{TOTAL_REQUESTS} 个请求...")

        print("等待所有响应返回...")
        # 带进度显示的等待
        pending = set(results)
        last_print = time.monotonic()
        while pending:
            done, pending = await asyncio.wait(pending, timeout=5)
            now = time.monotonic()
            if now - last_print >= 5:
                completed_count = TOTAL_REQUESTS - len(pending)
                elapsed_so_far = now - test_start
                print(f"  已完成 {completed_count}/{TOTAL_REQUESTS}，耗时 {elapsed_so_far:.1f}s...")
                last_print = now
        results = [t.result() for t in results]
        test_elapsed = time.monotonic() - test_start
        test_end_str = time.strftime("%Y-%m-%d %H:%M:%S")

        # 停止指标采集（带超时 + 强制取消）
        if metrics_task:
            stop_event.set()
            try:
                await asyncio.wait_for(metrics_task, timeout=10)
            except asyncio.TimeoutError:
                metrics_task.cancel()
                try:
                    await metrics_task
                except asyncio.CancelledError:
                    pass
            print(f"指标采集已停止 (共采集 {len(metric_samples)} 次)")

    # ---- 统计结果 ----
    success = [r for r in results if r["error"] is None and r["status"] == 200]
    failed = [r for r in results if r["error"] is not None or r["status"] != 200]
    elapsed_times = [r["elapsed"] for r in success]
    avg_time = statistics.mean(elapsed_times) if elapsed_times else 0

    # 构建报告（同时用于控制台输出和文件保存）
    R = []  # report lines

    R.append("")
    R.append("=" * 60)
    R.append("测试结果")
    R.append("=" * 60)
    R.append(f"总请求数:             {TOTAL_REQUESTS}")
    R.append(f"成功次数:             {len(success)}")
    R.append(f"失败次数:             {len(failed)}")
    R.append(f"平均耗时:             {avg_time:.3f}s")
    R.append(f"总耗时:               {test_elapsed:.2f}s")

    if elapsed_times:
        R.append(f"最小耗时:             {min(elapsed_times):.3f}s")
        R.append(f"最大耗时:             {max(elapsed_times):.3f}s")
        R.append(f"中位数:               {statistics.median(elapsed_times):.3f}s")
        sorted_t = sorted(elapsed_times)
        p95 = sorted_t[int(len(sorted_t) * 0.95)]
        p99 = sorted_t[min(int(len(sorted_t) * 0.99), len(sorted_t) - 1)]
        R.append(f"P95 耗时:             {p95:.3f}s")
        R.append(f"P99 耗时:             {p99:.3f}s")
        if len(elapsed_times) >= 2:
            R.append(f"标准差:               {statistics.stdev(elapsed_times):.3f}s")
        R.append(f"吞吐量:               {len(success) / test_elapsed:.2f} req/s")

    if failed:
        R.append(f"")
        R.append(f"失败请求 ({len(failed)}):")
        error_groups = {}
        for r in failed:
            key = f"HTTP {r['status']}" if r["error"] is None else r["error"][:100]
            error_groups[key] = error_groups.get(key, 0) + 1
        for err, count in sorted(error_groups.items(), key=lambda x: -x[1]):
            R.append(f"  {err}: {count}")

    # ---- 服务器指标 ----
    servers = {}
    if ENABLE_METRICS and metric_samples:
        # 按服务器分组（集群场景）
        for s in metric_samples:
            key = f"{s['hostname']}({s['ip']})" if s['hostname'] else "unknown"
            servers.setdefault(key, []).append(s)

        t0 = metric_samples[0]["time"]

        for server_name, server_samples in servers.items():
            cpus = [s["cpu"] for s in server_samples]
            mems = [s["mem"] for s in server_samples]
            heaps = [s["heap_mb"] for s in server_samples]
            goroutines = [s["goroutine"] for s in server_samples]
            R.append("")
            R.append("=" * 60)
            R.append(f"服务器指标 — {server_name} (共采集 {len(server_samples)} 次)")
            R.append("=" * 60)
            R.append(f"CPU 使用率(%):        最小 {min(cpus):.1f}  最大 {max(cpus):.1f}  平均 {statistics.mean(cpus):.1f}")
            R.append(f"内存使用率(%):        最小 {min(mems):.1f}  最大 {max(mems):.1f}  平均 {statistics.mean(mems):.1f}")
            R.append(f"堆内存(MB):           最小 {min(heaps):.1f}  最大 {max(heaps):.1f}  平均 {statistics.mean(heaps):.1f}")
            R.append(f"Goroutine 数量:       最小 {min(goroutines)}  最大 {max(goroutines)}  平均 {int(statistics.mean(goroutines))}")
            # 该服务器的采集明细（单独列出）
            R.append("")
            R.append(f"  采集明细 — {server_name}:")
            for i, s in enumerate(server_samples):
                elapsed = s["time"] - t0
                R.append(f"    [{i+1:3d}] +{elapsed:6.1f}s  CPU={s['cpu']:6.1f}%  MEM={s['mem']:5.1f}%  Heap={s['heap_mb']:7.1f}MB  Goroutine={s['goroutine']:6d}")

    # ---- 输出报告到控制台 ----
    report_text = "\n".join(R)
    print(report_text)

    # ---- 保存报告到 txt 文件（追加模式，多次压测结果叠加） ----
    filename = "test_results.txt"
    # 统计已有压测次数
    run_index = 1
    try:
        with open(filename, "r", encoding="utf-8") as f:
            for line in f:
                if line.startswith("# 第 ") and "次压测" in line:
                    run_index += 1
    except FileNotFoundError:
        pass
    with open(filename, "a", encoding="utf-8") as f:
        f.write(f"\n{'#' * 60}\n")
        f.write(f"# 第 {run_index} 次压测\n")
        f.write(f"# 开始: {test_start_str}    结束: {test_end_str}\n")
        f.write(f"{'#' * 60}\n\n")
        f.write(f"压测报告\n")
        f.write("=" * 60 + "\n")
        f.write("测试参数\n")
        f.write("=" * 60 + "\n")
        f.write(f"开始时间:             {test_start_str}\n")
        f.write(f"结束时间:             {test_end_str}\n")
        f.write(f"请求地址:             {URL}\n")
        f.write(f"模型:                 {MODEL}\n")
        f.write(f"总请求数:             {TOTAL_REQUESTS}\n")
        f.write(f"每秒请求数:           {REQUESTS_PER_SECOND}\n")
        f.write(f"最大并发上传数:       {MAX_CONCURRENT}\n")
        f.write(f"等待时间范围(ms):     {WAIT_MIN_MS} ~ {WAIT_MAX_MS}\n")
        f.write(f"请求超时(s):          {REQUEST_TIMEOUT}\n")
        f.write(f"携带图片:             {'是' if ENABLE_IMAGE else '否'}\n")
        if ENABLE_IMAGE:
            f.write(f"图片地址:             {IMAGE_URL}\n")
        f.write(f"采集指标:             {'是' if ENABLE_METRICS else '否'}\n")
        if ENABLE_METRICS:
            f.write(f"采集间隔(s):          {METRICS_INTERVAL}\n")
        f.write("\n")
        f.write(report_text)
        f.write("\n\n")
    print(f"\n报告已追加: {filename} (第 {run_index} 次)")

    # 有失败则返回非零退出码
    if len(failed) > 0:
        sys.exit(1)


if __name__ == "__main__":
    asyncio.run(main())
