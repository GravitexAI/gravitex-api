#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""错误诊断接口契约 + CSV 列名契约。

CSV 列名那组是本文件最重要的测试：Java 端拼表头、error-diagnosis 端认表头，
两边靠约定连着，没有编译期检查。任何一边改了列名而不同步，线上表现是
"诊断跑完但一条数据都没有"，不会报错。这里用真子进程跑通，把它钉死。
"""

from __future__ import annotations

import sys
import time
from datetime import datetime, timedelta, timezone
from pathlib import Path

import pytest
from fastapi.testclient import TestClient

sys.path.insert(0, str(Path(__file__).resolve().parent.parent))

import app as app_module
from diagnosis_runner import (ArtifactNotReady, DiagnosisBusy, DiagnosisParams,
                              DiagnosisRunner, DiagnosisState)

TOOL_DIR = Path(__file__).resolve().parent.parent.parent / "error-diagnosis"
TZ8 = timezone(timedelta(hours=8))

# Java 端 ErrorDiagnosisService 写出的表头，必须和 error-diagnosis/diagnostics/ingest.py
# 的 ALIASES 对得上；改这里就要同步改 Java 端，反之亦然。
CSV_HEADER = "时间,用户名,模型,资源分组,渠道名称,错误信息,状态码,类型"


def bill_csv(rows):
    epoch = int(datetime(2026, 9, 14, 10, 0, tzinfo=TZ8).timestamp())
    lines = [CSV_HEADER]
    for i, (user, model, error, status) in enumerate(rows):
        lines.append(f"{epoch + i},{user},{model},default,供应商A,\"{error}\",{status},错误")
    return "\n".join(lines) + "\n"


def wait_done(runner, task_id, timeout=90):
    deadline = time.time() + timeout
    while time.time() < deadline:
        state = runner.get(task_id)
        if state.status in ("success", "failed"):
            return state
        time.sleep(0.1)
    pytest.fail(f"诊断任务 {task_id} 超过 {timeout}s 未结束")


# ────────────────────────── 真子进程：CSV 列名契约 ──────────────────────────


@pytest.fixture
def runner(tmp_path):
    return DiagnosisRunner(tool_dir=TOOL_DIR, work_root=tmp_path / "diagnosis",
                           python_bin=Path(sys.executable))


def run_sync(runner, csv_text, tmp_path, **overrides):
    task_id = runner.reserve()
    csv_path = runner.work_dir(task_id) / "input.csv"
    csv_path.write_text(csv_text, encoding="utf-8")
    runner.start(task_id, DiagnosisParams(csv_path=csv_path, **overrides))
    return task_id, wait_done(runner, task_id)


def test_java_csv_header_is_recognised_end_to_end(runner, tmp_path):
    """Java 约定的 8 个列头能被诊断脚本全部识别，错误总数对得上。"""
    csv_text = bill_csv([
        ("alice", "claude-opus-4", "Error: rate limit exceeded", 429),
        ("alice", "claude-opus-4", "Error: rate limit exceeded", 429),
        ("bob", "claude-sonnet-4", "upstream timeout", 504),
    ])
    task_id, state = run_sync(runner, csv_text, tmp_path)

    assert state.status == "success", state.error
    assert state.errors == 3
    html = runner.artifact(task_id, "html")
    assert html.name == "error_diagnosis_20260914.html"
    # 日期落在实际数据日期目录下，证明「时间」列被当成 Unix 秒解析了，
    # 没有退化成 unknown-date
    assert html.parent.name == "2026-09-14"
    text = html.read_text(encoding="utf-8")
    assert "claude-opus-4" in text and "alice" in text


def test_unknown_time_column_falls_back_to_english_dir(runner, tmp_path):
    """时间列全空时归档到英文目录，不再产出中文路径。"""
    csv_text = f"{CSV_HEADER}\n,alice,claude-opus-4,default,供应商A,\"boom\",500,错误\n"
    task_id, state = run_sync(runner, csv_text, tmp_path)

    assert state.status == "success", state.error
    html = runner.artifact(task_id, "html")
    assert html.parent.name == "unknown-date"
    assert html.name == "error_diagnosis_unknown_date.html"


def test_ai_failure_keeps_the_dashboard(runner, tmp_path):
    """AI 接口打不通时任务仍算成功，看板可用，失败原因单独记在 ai_error。"""
    csv_text = bill_csv([("alice", "claude-opus-4", "boom", 500)])
    task_id, state = run_sync(
        runner, csv_text, tmp_path,
        ai_enabled=True,
        # 1 端口必然连接被拒，不依赖外网，也不会真花钱
        ai_base_url="http://127.0.0.1:1/v1", ai_model="mock", ai_api_key="test-only",
    )

    assert state.status == "success", state.error
    assert state.ai_error
    assert runner.artifact(task_id, "html").exists()
    with pytest.raises(ArtifactNotReady):
        runner.artifact(task_id, "report")


def test_bad_csv_fails_the_task_with_stderr_detail(runner, tmp_path):
    """识别不到错误/状态/类型列时任务失败，错误文案带上脚本的原始提示。"""
    _, state = run_sync(runner, "甲,乙,丙\n1,2,3\n", tmp_path)

    assert state.status == "failed"
    assert "错误、状态或类型" in state.error


def test_concurrency_cap_and_cleanup(tmp_path):
    """并发槽用满后拒绝新任务；任务过期后工作目录被整个删掉。"""
    runner = DiagnosisRunner(tool_dir=TOOL_DIR, work_root=tmp_path / "diagnosis",
                             python_bin=Path(sys.executable), max_concurrent=2)
    first, second = runner.reserve(), runner.reserve()
    with pytest.raises(DiagnosisBusy):
        runner.reserve()

    work = runner.work_dir(first)
    runner.release(first, "测试释放")
    assert runner.get(first).status == "failed"
    runner.reserve()                                 # 槽位已放回，可以再拿

    runner.get(first).finished_at = time.time() - 10_000
    assert runner.cleanup_expired(3600) == 1
    assert not work.exists()
    assert runner.get(first) is None
    assert runner.get(second) is not None            # 未结束的任务不受清理影响


# ────────────────────────── HTTP 契约：假 runner，不起子进程 ──────────────────────────


class FakeDiagnosisRunner:
    max_concurrent = 2

    def __init__(self, tmp_path):
        self.tool_error = ""
        self.busy = False
        self.started = None
        self.released = None
        self.state = DiagnosisState(task_id="d1", status="running", stage="读取账单并聚合",
                                    done=100_000, total=300_000)
        self.root = tmp_path
        self.html = tmp_path / "error_diagnosis_20260914.html"
        self.html.write_text("<html>看板</html>", encoding="utf-8")

    def check_tool_dir(self):
        return self.tool_error

    def reserve(self):
        if self.busy:
            raise DiagnosisBusy("已有 2 个诊断任务在执行中（上限 2），请稍后重试")
        return "d1"

    def work_dir(self, task_id):
        path = self.root / task_id
        path.mkdir(parents=True, exist_ok=True)
        return path

    def start(self, task_id, params):
        self.started = params

    def release(self, task_id, error):
        self.released = error

    def get(self, task_id):
        return self.state if task_id == "d1" else None

    def artifact(self, task_id, kind):
        if kind == "html":
            return self.html
        raise ArtifactNotReady("任务 d1 没有生成 AI 分析报告")

    def cleanup_expired(self, ttl):
        return 0


@pytest.fixture
def client(tmp_path, monkeypatch):
    fake = FakeDiagnosisRunner(tmp_path)
    monkeypatch.setattr(app_module, "DIAGNOSIS_RUNNER", fake)
    monkeypatch.setattr(app_module, "DIAGNOSIS_WORK_DIR", tmp_path / "work")
    (tmp_path / "work").mkdir()
    with TestClient(app_module.app) as test_client:
        yield test_client, fake


def post_run(client, params_json, body=b"time,user\n1,a\n"):
    return client.post("/diagnosis/run", content=body,
                       headers={"X-Diagnosis-Params": params_json,
                                "Content-Type": "text/csv"})


def test_run_streams_body_and_passes_params(client):
    test_client, fake = client
    response = post_run(test_client, '{"total_rows": 300000, "max_buckets": 400000,'
                                     ' "ai_enabled": true, "ai_api_key": "sk-x",'
                                     ' "ai_model": "glm-5.3-flash"}')

    assert response.status_code == 200
    assert response.json() == {"task_id": "d1"}
    params = fake.started
    assert params.total_rows == 300_000
    assert params.max_buckets == 400_000
    assert params.samples_per_bucket == 2            # 未传的项回落默认值
    assert params.ai_enabled and params.ai_api_key == "sk-x"
    assert params.csv_path.read_bytes() == b"time,user\n1,a\n"


def test_run_rejects_bad_params(client):
    test_client, _ = client
    assert post_run(test_client, "{oops").status_code == 400
    assert post_run(test_client, '{"max_buckets": 0}').status_code == 400
    assert post_run(test_client, '{"max_samples": "abc"}').status_code == 400
    # 勾了 AI 却没给密钥：必须在提交前拦掉，不能让任务跑到一半才失败
    assert post_run(test_client, '{"ai_enabled": true}').status_code == 400


def test_run_rejects_empty_body_and_releases_slot(client):
    test_client, fake = client
    response = post_run(test_client, "{}", body=b"")

    assert response.status_code == 400
    assert fake.released, "空账单被拒后必须释放并发槽，否则槽位会被占死"


def test_run_surfaces_busy_and_missing_tool_dir(client):
    test_client, fake = client
    fake.busy = True
    assert post_run(test_client, "{}").status_code == 409

    fake.busy = False
    fake.tool_error = "诊断脚本不存在：/workplace/py/testReport/error-diagnosis/error_diagnosis.py"
    response = post_run(test_client, "{}")
    assert response.status_code == 503
    assert "error_diagnosis.py" in response.json()["detail"]


def test_status_and_artifacts(client):
    test_client, _ = client
    body = test_client.get("/diagnosis/tasks/d1").json()
    assert body["status"] == "running"
    assert body["done"] == 100_000 and body["total"] == 300_000
    assert body["html_ready"] is False

    assert test_client.get("/diagnosis/tasks/nope").status_code == 404

    html = test_client.get("/diagnosis/tasks/d1/html")
    assert html.status_code == 200
    assert html.headers["content-type"].startswith("text/html")
    assert html.text == "<html>看板</html>"

    # AI 没跑或跑失败时取 Markdown 给 409，不给 500
    assert test_client.get("/diagnosis/tasks/d1/report").status_code == 409


def test_meta_exposes_defaults_without_api_key(client, monkeypatch, tmp_path):
    test_client, _ = client
    env = tmp_path / "tool"
    env.mkdir()
    (env / ".env").write_text(
        "# 注释行\nBASE_URL=https://api.gravitex.ai/v1/chat/completions\n"
        "MODEL=glm-5.3-flash\nAPI_KEY=sk-secret\n", encoding="utf-8")
    monkeypatch.setattr(app_module, "DIAGNOSIS_DIR", env)

    body = test_client.get("/diagnosis/meta").json()
    assert body["default_base_url"] == "https://api.gravitex.ai/v1/chat/completions"
    assert body["default_model"] == "glm-5.3-flash"
    assert body["limits"]["max_buckets"] == 250000
    assert "sk-secret" not in str(body), "服务端密钥不能出现在 /meta 响应里"
