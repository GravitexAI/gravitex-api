#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
主入口：并发遍历 [模型 × 用例] 执行测试，打印进度，生成 Excel 报告。

并发：所有 (模型, 用例) 组合放进一个线程池（config.MAX_WORKERS），
      谁先跑完谁先打印；报告里的行序仍按 MODELS × ALL_CASES 的原始顺序还原。

用法：
    python run_tests.py                 # 用 config.py 里的配置
    # 或用环境变量临时覆盖（不改代码）：
    CLAUDE_TEST_BASE_URL=http://x \\
    CLAUDE_TEST_AUTH_MODE=bearer \\
    CLAUDE_TEST_API_KEY=sk-xxx \\
    CLAUDE_TEST_MODELS="claude-opus-4-8,claude-haiku-4-5-20251001" \\
    python run_tests.py
"""

from __future__ import annotations

import argparse
import json
import threading
import time
from concurrent.futures import ThreadPoolExecutor, as_completed

import config
from client import Client
from cases import (ALL_CASES, ALL_CATEGORIES, TestCase, PASS, FAIL, SKIP, ERROR,
                   filter_cases, latency)
from cases.base import skip as make_skip
from report import ResultRow, generate_report

_ICON = {PASS: "✅", FAIL: "❌", SKIP: "⏭️", ERROR: "💥"}

_PROGRESS_LOCK = threading.Lock()

# --progress-json 打开后，每个关键节点往 stdout 打一行 @@PROGRESS@@ {json}。
# 用前缀而不是纯 JSON，是因为 config.PRINT_HTTP 会把完整 HTTP 报文也打到
# stdout，测试服务需要一个不会和正文混淆的标记来提取进度。
_PROGRESS_MARKER = "@@PROGRESS@@ "
_progress_json_enabled = False


def _emit_progress(payload: dict) -> None:
    """输出一条机器可读的进度事件。未开启 --progress-json 时是空操作。"""
    if not _progress_json_enabled:
        return
    print(_PROGRESS_MARKER + json.dumps(payload, ensure_ascii=False), flush=True)


def _run_case(client: Client, model: str, case: TestCase) -> ResultRow:
    """跑一个 (模型, 用例)；前置条件不满足直接记 SKIP。"""
    if case.requires is not None:
        satisfied, reason = case.requires()
        if not satisfied:
            return ResultRow(model, case.case_id, case.category, case.name,
                             case.severity, make_skip(reason=reason))

    outcome = case.run(client, model)
    # 同一线程内两个用例之间留一点间隔，给平台限流窗口余量
    if config.REQUEST_INTERVAL_SECONDS > 0:
        time.sleep(config.REQUEST_INTERVAL_SECONDS)
    return ResultRow(model, case.case_id, case.category, case.name, case.severity, outcome)


def main() -> None:
    global _progress_json_enabled

    parser = argparse.ArgumentParser(description="Claude 平台能力测试")
    parser.add_argument("--validate-config", action="store_true", help="只校验模型和用例配置，不发请求")
    parser.add_argument("--categories", default="",
                        help="只跑这些用例分类，逗号分隔；留空跑全部。也可用 CLAUDE_TEST_CATEGORIES 环境变量")
    parser.add_argument("--output", default="",
                        help="报告输出路径（建议绝对路径）；留空写当前目录的 config.REPORT_FILENAME")
    parser.add_argument("--progress-json", action="store_true",
                        help="往 stdout 打 @@PROGRESS@@ 前缀的 JSON 进度事件，供测试服务解析")
    args = parser.parse_args()
    _progress_json_enabled = args.progress_json

    config.validate_config()

    # 命令行参数优先于环境变量；两者都没有则跑全部分类
    selected_categories = ([item.strip() for item in args.categories.split(",") if item.strip()]
                           if args.categories else list(config.CATEGORIES))
    unknown = [name for name in selected_categories if name not in ALL_CATEGORIES]
    if unknown:
        raise SystemExit(f"未知的用例分类：{unknown}，可选：{ALL_CATEGORIES}")
    selected_cases = filter_cases(selected_categories)
    if not selected_cases:
        raise SystemExit("过滤后没有任何用例可执行")

    _emit_progress({"event": "start",
                    "total": len(selected_cases) * len(config.MODELS),
                    "cases": len(selected_cases),
                    "models": len(config.MODELS)})

    if args.validate_config:
        print(f"配置校验通过：{len(config.MODELS)} 个模型，{len(selected_cases)} 个用例")
        return

    tasks = [(model, case) for model in config.MODELS for case in selected_cases]
    workers = max(1, min(int(config.MAX_WORKERS), len(tasks)))

    print("=" * 72)
    print(f"被测平台：{config.PLATFORM_NAME}")
    print(f"接口地址：{config.BASE_URL}/v1/messages"
          f"{'（实际调用）' if config.report_differs_from_actual() else ''}")
    print(f"鉴权方式：{config.AUTH_MODE}"
          f"{'（实际调用）' if config.report_differs_from_actual() else ''}")
    if config.report_differs_from_actual():
        print(f"报告展示：{config.report_base_url()}/v1/messages"
              f"  鉴权 {config.report_auth_mode()}（仅写进报告，不影响实际请求）")
    print(f"测试模型：{config.MODELS}")
    print("模型分组：" + ", ".join(
        f"{model}={config.model_family(model)}/{config.thinking_mode(model)}"
        for model in config.MODELS
    ))
    print(f"用例总数：{len(selected_cases)}  ×  模型数 {len(config.MODELS)} = {len(tasks)} 次")
    print(f"用例分类：{selected_categories or '全部'}")
    print(f"并发线程：{workers}（config.MAX_WORKERS）")
    print("=" * 72)

    client = Client()
    # 每个模型的用例序号里多留一位，给"响应延迟分位"统计行排在该模型末尾
    per_model = len(selected_cases) + 1
    order = {
        (model, case.case_id): model_index * per_model + case_index
        for model_index, model in enumerate(config.MODELS)
        for case_index, case in enumerate(selected_cases)
    }
    for model_index, model in enumerate(config.MODELS):
        order[(model, latency.CASE_ID)] = model_index * per_model + len(selected_cases)
    rows: list[ResultRow] = []
    started = time.monotonic()

    with ThreadPoolExecutor(max_workers=workers) as pool:
        futures = [pool.submit(_run_case, client, model, case) for model, case in tasks]
        for future in as_completed(futures):
            row = future.result()
            rows.append(row)
            icon = _ICON.get(row.outcome.verdict, "?")
            with _PROGRESS_LOCK:
                print(f"{icon} [{len(rows)}/{len(tasks)}] [{row.model}] "
                      f"{row.case_id} {row.name} -> {row.outcome.verdict}  "
                      f"{row.outcome.reason}", flush=True)
                _emit_progress({"event": "case", "done": len(rows), "total": len(tasks),
                                "model": row.model, "case_id": row.case_id,
                                "name": row.name, "verdict": row.outcome.verdict})

    # 全部跑完后再统计延迟分位：样本齐了才算得准，也避开并发下的执行顺序问题
    for model in config.MODELS:
        rows.append(ResultRow(model, latency.CASE_ID, latency.CATEGORY,
                              latency.NAME, latency.SEVERITY, latency.build_outcome(model)))

    # 还原成"按模型、按用例定义顺序"，报告不受并发完成顺序影响
    rows.sort(key=lambda row: order[(row.model, row.case_id)])
    elapsed = time.monotonic() - started

    # ---- 汇总 ----
    print("\n" + "=" * 72)
    print(f"测试汇总（总耗时 {elapsed:.1f}s）")
    print("=" * 72)
    for model in config.MODELS:
        mrows = [x for x in rows if x.model == model]
        n_pass = sum(1 for x in mrows if x.outcome.verdict == PASS)
        n_fail = sum(1 for x in mrows if x.outcome.verdict == FAIL)
        n_err = sum(1 for x in mrows if x.outcome.verdict == ERROR)
        n_skip = sum(1 for x in mrows if x.outcome.verdict == SKIP)
        counted = len(mrows) - n_skip
        rate = f"{(n_pass / counted * 100):.1f}%" if counted else "N/A"
        print(f"  {model}: 通过 {n_pass} / 不通过 {n_fail} / 异常 {n_err} / "
              f"跳过 {n_skip}  通过率 {rate}")

    print("\n按模型系列汇总")
    for family in sorted({config.model_family(model) for model in config.MODELS}):
        family_rows = [x for x in rows if config.model_family(x.model) == family]
        n_pass = sum(x.outcome.verdict == PASS for x in family_rows)
        n_fail = sum(x.outcome.verdict == FAIL for x in family_rows)
        n_err = sum(x.outcome.verdict == ERROR for x in family_rows)
        n_skip = sum(x.outcome.verdict == SKIP for x in family_rows)
        counted = len(family_rows) - n_skip
        rate = f"{n_pass / counted * 100:.1f}%" if counted else "N/A"
        print(f"  {family}: 模型数 {len({x.model for x in family_rows})} / "
              f"通过 {n_pass} / 不通过 {n_fail} / 异常 {n_err} / "
              f"跳过 {n_skip} / 通过率 {rate}")

    path = generate_report(rows, args.output or None)
    print(f"\n报告已生成：{path}")
    _emit_progress({"event": "done", "report": path})


if __name__ == "__main__":
    main()
