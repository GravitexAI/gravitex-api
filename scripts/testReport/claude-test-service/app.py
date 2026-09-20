#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""渠道测试报告服务。

只监听内网，由 Java 后端转发调用，不直接暴露公网——
入参里带明文 API Key，没有鉴权层，公网暴露等于裸奔。

渠道测试三个核心接口：
  POST /run                    提交测试任务，返回 task_id
  GET  /tasks/{task_id}        查任务进度
  GET  /tasks/{task_id}/report 下载 xlsx 报告
另有 /health 和 /meta（给前端拉模型和分类清单）。

错误账单诊断四个接口（输入错误明细 CSV，产出离线 HTML 看板 + 可选 AI Markdown）：
  POST /diagnosis/run                    提交诊断任务，返回 task_id
  GET  /diagnosis/tasks/{task_id}        查任务进度
  GET  /diagnosis/tasks/{task_id}/html   取诊断看板
  GET  /diagnosis/tasks/{task_id}/report 取 AI 分析报告
另有 /diagnosis/meta（给前端拉默认地址/模型与参数上限）。
"""

from __future__ import annotations

import json
import logging
import shutil
import sys
import threading
import time
from pathlib import Path

from fastapi import FastAPI, HTTPException, Request
from fastapi.responses import FileResponse
from pydantic import BaseModel, Field

from diagnosis_runner import (ArtifactNotReady, DiagnosisBusy, DiagnosisParams,
                              DiagnosisRunner)
from runner import (PythonBinUnavailable, ReportNotReady, Runner, RunnerBusy,
                    TaskParams)

logger = logging.getLogger(__name__)

BASE_DIR = Path(__file__).resolve().parent
SCRIPT_DIR = BASE_DIR.parent / "claude-platform-test"
DIAGNOSIS_DIR = BASE_DIR.parent / "error-diagnosis"
REPORT_DIR = BASE_DIR / "reports"
DIAGNOSIS_WORK_DIR = BASE_DIR / "diagnosis"
PYTHON_BIN = SCRIPT_DIR / ".venv" / "bin" / "python"

# 让 config / cases 可以被 import，用来暴露模型和分类清单
sys.path.insert(0, str(SCRIPT_DIR))
import config as script_config          # noqa: E402
import cases as script_cases            # noqa: E402

# 报告文件保留时长：不存历史，只保证前端轮询完能下载到
REPORT_TTL_SECONDS = 24 * 3600
CLEANUP_INTERVAL_SECONDS = 3600

XLSX_MEDIA_TYPE = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"

app = FastAPI(title="Claude 渠道测试报告服务")

RUNNER = Runner(script_dir=SCRIPT_DIR, report_dir=REPORT_DIR, python_bin=PYTHON_BIN)

# 诊断脚本只用标准库（见 error-diagnosis/README.md），所以直接用本服务自己的
# 解释器，不碰 claude-platform-test/.venv——那个 venv 跨机器复制后会断链，
# 渠道测试为此专门做了 check_python_bin，诊断没必要重蹈覆辙。
DIAGNOSIS_RUNNER = DiagnosisRunner(
    tool_dir=DIAGNOSIS_DIR, work_root=DIAGNOSIS_WORK_DIR, python_bin=Path(sys.executable))


class RunRequest(BaseModel):
    platform_name: str = Field(min_length=1)
    base_url: str = Field(min_length=1)
    api_key: str = Field(min_length=1)
    report_base_url: str = ""
    models: list = Field(min_length=1)
    categories: list = []


@app.get("/health")
def health() -> dict:
    """健康检查。顺带暴露测试脚本的 Python 环境是否可用。

    单独报出来是因为：服务自己活得好好的，但跑测试的子进程解释器可能是断链
    （.venv 被从 macOS 传上来时必然如此）。不在这里暴露的话，要等到有人提交
    任务才发现，而那时候已经在弹窗里看到一句莫名的 Errno 2 了。
    """
    broken = RUNNER.check_python_bin()
    if broken:
        logger.warning("测试脚本 Python 环境不可用: %s", broken)
        return {"ok": False, "script_python": broken}
    return {"ok": True}


@app.get("/meta")
def meta() -> dict:
    """给前端弹窗用的选项清单：可测模型、用例分类和缓存轮数。

    models 是脚本默认配置的模型清单，仅供参考——测试脚本现在能按模型名
    自动推导 family/thinking，/run 不再拿它做白名单校验，任意模型名都能提交。
    """
    return {
        "models": list(script_config.MODEL_METADATA.keys()),
        "categories": [{"name": name, "count": script_cases.CATEGORY_CASE_COUNTS[name]}
                       for name in script_cases.ALL_CATEGORIES],
        "cache_hit_rounds": script_config.CACHE_HIT_ROUNDS,
    }


@app.post("/run")
def run(request: RunRequest) -> dict:
    unknown_categories = [c for c in request.categories if c not in script_cases.ALL_CATEGORIES]
    if unknown_categories:
        raise HTTPException(status_code=400,
                            detail=f"不支持的用例分类：{unknown_categories}，"
                                   f"可选：{script_cases.ALL_CATEGORIES}")

    base_url = request.base_url.rstrip("/")
    if not base_url:
        raise HTTPException(status_code=400, detail="base_url 不能为空或仅由斜杠组成")
    # 留空表示"报告地址与实际调用地址一致"。必须在这里显式填成 base_url，
    # 不能靠子进程的 config._env 回落——它的硬编码默认值是另一家供应商的域名，
    # 空值会静默产出一份写着错误接口地址的报告。
    report_base_url = request.report_base_url.rstrip("/") or base_url

    params = TaskParams(
        platform_name=request.platform_name,
        base_url=base_url,
        api_key=request.api_key,
        report_base_url=report_base_url,
        models=list(request.models),
        categories=list(request.categories),
    )
    try:
        task_id = RUNNER.submit(params)
    except PythonBinUnavailable as exc:
        # 503 而不是 500：这是环境问题、运维照着 detail 里的命令就能修，
        # 不是代码 bug，也不该让调用方以为是自己参数错了。
        raise HTTPException(status_code=503, detail=str(exc))
    except RunnerBusy as exc:
        raise HTTPException(status_code=409, detail=str(exc))
    return {"task_id": task_id}


@app.get("/tasks/{task_id}")
def task_status(task_id: str) -> dict:
    state = RUNNER.get(task_id)
    if state is None:
        raise HTTPException(status_code=404, detail=f"任务 {task_id} 不存在或已过期")
    return state.to_dict()


@app.get("/tasks/{task_id}/report")
def task_report(task_id: str):
    try:
        path = RUNNER.report_file(task_id)
    except ReportNotReady as exc:
        raise HTTPException(status_code=409, detail=str(exc))
    return FileResponse(str(path), media_type=XLSX_MEDIA_TYPE, filename=path.name)


# ────────────────────────── 错误账单诊断 ──────────────────────────
# 输入是调用方上传的错误明细 CSV，输出是离线 HTML 看板（可选附一份 AI Markdown）。
# 和渠道测试共用进程但不共用并发槽：诊断纯本地算，不打上游接口。


@app.get("/diagnosis/meta")
def diagnosis_meta() -> dict:
    """给前端弹窗用的默认值。只暴露地址和模型名，不暴露服务端配置的密钥。

    这里手抄一小段 .env 解析，而不是 import ai_analysis.read_env：后者会连带
    把整个 diagnostics 包拉进本服务进程，而 error-diagnosis 目录缺失恰恰是本
    接口要如实报告的情况之一（available=false），不能让它先把服务搞崩。
    """
    defaults = {"BASE_URL": "", "MODEL": ""}
    env_file = DIAGNOSIS_DIR / ".env"
    if env_file.exists():
        for line in env_file.read_text(encoding="utf-8-sig").splitlines():
            line = line.strip().removeprefix("export ").strip()
            key, sep, value = line.partition("=")
            if sep and not line.startswith("#") and key.strip() in defaults:
                defaults[key.strip()] = value.strip().strip("'\"")

    broken = DIAGNOSIS_RUNNER.check_tool_dir()
    return {
        "available": not broken,
        "unavailable_reason": broken,
        "default_base_url": defaults["BASE_URL"],
        "default_model": defaults["MODEL"],
        "max_concurrent": DIAGNOSIS_RUNNER.max_concurrent,
        "limits": {
            "max_buckets": 250000,
            "max_samples": 4000,
            "samples_per_bucket": 2,
            "evidence_chars": 1600,
        },
    }


@app.post("/diagnosis/run")
async def diagnosis_run(request: Request) -> dict:
    """请求体是错误明细 CSV 原文，参数放在 X-Diagnosis-Params 头里的一段 JSON。

    为什么不用 multipart 表单：FastAPI 的 UploadFile/Form 依赖 python-multipart，
    而这个包在路由「定义」时就被检查——服务器上一旦漏装，整个服务直接起不来，
    会把本来好好的渠道测试一起拖下水。为了一个内网单接口引入这种全局启动风险
    不划算。参数也不放 query string：里面有 AI 密钥，会被 uvicorn 访问日志
    原样记下来；请求头默认不入访问日志。
    """
    broken = DIAGNOSIS_RUNNER.check_tool_dir()
    if broken:
        # 503 而不是 500：环境问题，运维照着 detail 就能修，不是调用方参数错了
        raise HTTPException(status_code=503, detail=broken)
    try:
        params = json.loads(request.headers.get("X-Diagnosis-Params") or "{}")
    except ValueError as exc:
        raise HTTPException(status_code=400, detail=f"X-Diagnosis-Params 不是合法 JSON：{exc}")

    limits = {}
    for name, fallback in (("max_buckets", 250000), ("max_samples", 4000),
                           ("samples_per_bucket", 2), ("evidence_chars", 1600)):
        try:
            limits[name] = int(params.get(name, fallback))
        except (TypeError, ValueError):
            raise HTTPException(status_code=400, detail=f"{name} 必须为正整数")
        if limits[name] < 1:
            raise HTTPException(status_code=400, detail=f"{name} 必须为正整数")
    ai_enabled = bool(params.get("ai_enabled"))
    ai_api_key = str(params.get("ai_api_key") or "").strip()
    if ai_enabled and not ai_api_key:
        raise HTTPException(status_code=400, detail="已勾选 AI 分析，API Key 不能为空")

    try:
        task_id = DIAGNOSIS_RUNNER.reserve()
    except DiagnosisBusy as exc:
        raise HTTPException(status_code=409, detail=str(exc))

    # 落盘失败必须显式释放槽位，否则并发槽会被这个永远不会开跑的任务占死
    try:
        csv_path = DIAGNOSIS_RUNNER.work_dir(task_id) / "input.csv"
        with csv_path.open("wb") as stream:
            async for chunk in request.stream():
                stream.write(chunk)
    except Exception as exc:                            # noqa: BLE001
        logger.exception("[Diagnosis] 上传账单落盘失败")
        DIAGNOSIS_RUNNER.release(task_id, f"上传账单落盘失败：{exc}")
        raise HTTPException(status_code=500, detail=f"上传账单落盘失败：{exc}")

    if csv_path.stat().st_size == 0:
        DIAGNOSIS_RUNNER.release(task_id, "上传的错误明细为空")
        raise HTTPException(status_code=400, detail="上传的错误明细为空，没有可诊断的数据")

    DIAGNOSIS_RUNNER.start(task_id, DiagnosisParams(
        csv_path=csv_path,
        total_rows=max(int(params.get("total_rows") or 0), 0),
        ai_enabled=ai_enabled,
        ai_base_url=str(params.get("ai_base_url") or "").strip(),
        ai_model=str(params.get("ai_model") or "").strip(),
        ai_api_key=ai_api_key,
        ai_include_user_names=bool(params.get("ai_include_user_names")),
        **limits,
    ))
    return {"task_id": task_id}


@app.get("/diagnosis/tasks/{task_id}")
def diagnosis_status(task_id: str) -> dict:
    state = DIAGNOSIS_RUNNER.get(task_id)
    if state is None:
        raise HTTPException(status_code=404, detail=f"任务 {task_id} 不存在或已过期")
    return state.to_dict()


@app.get("/diagnosis/tasks/{task_id}/html")
def diagnosis_html(task_id: str):
    try:
        path = DIAGNOSIS_RUNNER.artifact(task_id, "html")
    except ArtifactNotReady as exc:
        raise HTTPException(status_code=409, detail=str(exc))
    return FileResponse(str(path), media_type="text/html; charset=utf-8", filename=path.name)


@app.get("/diagnosis/tasks/{task_id}/report")
def diagnosis_report(task_id: str):
    try:
        path = DIAGNOSIS_RUNNER.artifact(task_id, "report")
    except ArtifactNotReady as exc:
        raise HTTPException(status_code=409, detail=str(exc))
    return FileResponse(str(path), media_type="text/markdown; charset=utf-8", filename=path.name)


def _cleanup_orphan_reports() -> int:
    """按文件 mtime 兜底清理磁盘上的过期报告。

    Runner.cleanup_expired() 只遍历内存里的 self._tasks，而 Runner.__init__
    只做 mkdir、不扫描目录。systemd 是 Restart=always，每次重启/发版都会把
    当时 reports/ 里的所有报告变成内存态之外的孤儿文件，仅靠 _tasks 遍历会
    永久漏删它们，磁盘无上限增长。这里直接扫磁盘上的 mtime，不依赖进程内存。
    """
    removed = 0
    now = time.time()
    for path in REPORT_DIR.glob("*.xlsx"):
        try:
            if now - path.stat().st_mtime > REPORT_TTL_SECONDS:
                path.unlink()
                removed += 1
        except Exception:                          # noqa: BLE001 — 单个文件失败不能中断整个循环
            logger.exception("清理孤儿报告文件失败：%s", path)
    for path in DIAGNOSIS_WORK_DIR.glob("*"):
        try:
            if path.is_dir() and now - path.stat().st_mtime > REPORT_TTL_SECONDS:
                shutil.rmtree(path, ignore_errors=True)
                removed += 1
        except Exception:                          # noqa: BLE001 — 同上
            logger.exception("清理孤儿诊断目录失败：%s", path)
    return removed


def _cleanup_once() -> None:
    """执行一轮过期报告清理。抽成单独函数是为了能在测试里直接调用，不必等 sleep。"""
    try:
        removed = RUNNER.cleanup_expired(REPORT_TTL_SECONDS)
        removed += DIAGNOSIS_RUNNER.cleanup_expired(REPORT_TTL_SECONDS)
    except Exception:                              # noqa: BLE001 — 清理失败不能拖垮清理线程
        logger.exception("报告清理失败")
        return
    removed += _cleanup_orphan_reports()
    if removed:
        logger.info("已清理 %d 个过期报告", removed)


def _cleanup_loop() -> None:
    while True:
        time.sleep(CLEANUP_INTERVAL_SECONDS)
        _cleanup_once()


threading.Thread(target=_cleanup_loop, daemon=True).start()
