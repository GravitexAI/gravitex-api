#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""测试服务 HTTP 接口契约。用假 runner 替换真执行器，不起子进程。"""

from __future__ import annotations

import logging
import sys
import time
from pathlib import Path

import pytest
from fastapi.testclient import TestClient

sys.path.insert(0, str(Path(__file__).resolve().parent.parent))

import app as app_module
from runner import ReportNotReady, RunnerBusy, TaskState


class FakeRunner:
    def __init__(self, tmp_path):
        self.busy = False
        self.state = TaskState(task_id="t1", status="running", done=1, total=4)
        self.report = tmp_path / "t1.xlsx"
        self.report.write_bytes(b"FAKE-XLSX")
        self.last_params = None
        self.cleanup_error = None
        self.cleanup_result = 0

    def submit(self, params):
        if self.busy:
            raise RunnerBusy("已有测试任务 t0 在执行中，请等它结束")
        self.last_params = params
        return "t1"

    def get(self, task_id):
        return self.state if task_id == "t1" else None

    def report_file(self, task_id):
        if self.state.status != "success":
            raise ReportNotReady("报告尚未生成")
        return self.report

    def cleanup_expired(self, ttl_seconds):
        if self.cleanup_error is not None:
            raise self.cleanup_error
        return self.cleanup_result


@pytest.fixture
def client(tmp_path, monkeypatch):
    fake = FakeRunner(tmp_path)
    monkeypatch.setattr(app_module, "RUNNER", fake)
    # _cleanup_once() 现在还会扫 REPORT_DIR 做孤儿文件兜底清理，
    # 必须隔离到临时目录，否则会误删仓库里真实的 reports/*.xlsx。
    report_dir = tmp_path / "reports"
    report_dir.mkdir()
    monkeypatch.setattr(app_module, "REPORT_DIR", report_dir)
    test_client = TestClient(app_module.app)
    test_client.fake = fake
    return test_client


def test_health(client):
    assert client.get("/health").json() == {"ok": True}


def test_meta_lists_registered_models_and_twelve_categories(client):
    body = client.get("/meta").json()
    assert len(body["models"]) == len(app_module.script_config.MODEL_METADATA)
    assert "claude-opus-5" in body["models"]
    assert len(body["categories"]) == 12
    counts = {item["name"]: item["count"] for item in body["categories"]}
    assert counts["基础对话"] == 5
    assert counts["提示词缓存"] == 2
    assert body["cache_hit_rounds"] == 20


def test_run_returns_task_id_and_passes_params(client):
    payload = {
        "platform_name": "acme",
        "base_url": "https://gw.example.com",
        "api_key": "sk-test-202",
        "report_base_url": "https://vendor.example.com",
        "models": ["claude-opus-5"],
        "categories": ["基础对话"],
    }
    response = client.post("/run", json=payload)
    assert response.status_code == 200
    assert response.json() == {"task_id": "t1"}
    assert client.fake.last_params.platform_name == "acme"
    assert client.fake.last_params.models == ["claude-opus-5"]


def test_run_rejects_slash_only_base_url(client):
    payload = {
        "platform_name": "acme", "base_url": "/",
        "api_key": "sk-test-202", "report_base_url": "",
        "models": ["claude-opus-5"], "categories": [],
    }
    response = client.post("/run", json=payload)
    assert response.status_code == 400
    assert "不能为空" in response.json()["detail"]


def test_run_defaults_report_base_url_to_base_url_when_blank(client):
    payload = {
        "platform_name": "acme", "base_url": "https://gw.example.com",
        "api_key": "sk-test-202", "report_base_url": "",
        "models": ["claude-opus-5"], "categories": [],
    }
    response = client.post("/run", json=payload)
    assert response.status_code == 200
    assert client.fake.last_params.report_base_url == "https://gw.example.com"


def test_run_strips_trailing_slash_from_report_base_url(client):
    payload = {
        "platform_name": "acme", "base_url": "https://gw.example.com",
        "api_key": "sk-test-202", "report_base_url": "https://vendor.example.com/",
        "models": ["claude-opus-5"], "categories": [],
    }
    response = client.post("/run", json=payload)
    assert response.status_code == 200
    assert client.fake.last_params.report_base_url == "https://vendor.example.com"


def test_run_accepts_unknown_model(client):
    """脚本能按模型名自动推导 family/thinking，/run 不再校验模型白名单。"""
    payload = {
        "platform_name": "acme", "base_url": "https://gw.example.com",
        "api_key": "sk-test-202", "report_base_url": "",
        "models": ["gpt-4o"], "categories": [],
    }
    response = client.post("/run", json=payload)
    assert response.status_code == 200
    assert "gpt-4o" in client.fake.last_params.models


def test_run_rejects_unknown_category(client):
    payload = {
        "platform_name": "acme", "base_url": "https://gw.example.com",
        "api_key": "sk-test-202", "report_base_url": "",
        "models": ["claude-opus-5"], "categories": ["不存在的分类"],
    }
    response = client.post("/run", json=payload)
    assert response.status_code == 400
    assert "不存在的分类" in response.json()["detail"]


def test_run_returns_409_when_busy(client):
    client.fake.busy = True
    payload = {
        "platform_name": "acme", "base_url": "https://gw.example.com",
        "api_key": "sk-test-202", "report_base_url": "",
        "models": ["claude-opus-5"], "categories": [],
    }
    response = client.post("/run", json=payload)
    assert response.status_code == 409
    assert "已有测试任务" in response.json()["detail"]


def test_task_status(client):
    body = client.get("/tasks/t1").json()
    assert body["status"] == "running"
    assert body["done"] == 1
    assert body["total"] == 4
    assert body["report_ready"] is False


def test_unknown_task_404(client):
    assert client.get("/tasks/nope").status_code == 404


def test_report_409_when_not_ready(client):
    assert client.get("/tasks/t1/report").status_code == 409


def test_report_streams_xlsx_when_ready(client):
    client.fake.state.status = "success"
    response = client.get("/tasks/t1/report")
    assert response.status_code == 200
    assert response.content == b"FAKE-XLSX"
    assert "spreadsheetml" in response.headers["content-type"]


def test_cleanup_once_logs_exception_and_does_not_raise(client, caplog):
    """清理失败必须留痕（运维能发现），但不能让清理线程因此退出。"""
    client.fake.cleanup_error = RuntimeError("disk full")
    with caplog.at_level(logging.ERROR):
        app_module._cleanup_once()  # 不抛异常 == 循环不会因此退出
    assert "报告清理失败" in caplog.text


def test_cleanup_once_logs_info_when_reports_removed(client, caplog):
    client.fake.cleanup_result = 3
    with caplog.at_level(logging.INFO):
        app_module._cleanup_once()
    assert "已清理 3 个过期报告" in caplog.text


def test_cleanup_once_silent_when_nothing_removed(client, caplog):
    client.fake.cleanup_result = 0
    with caplog.at_level(logging.INFO):
        app_module._cleanup_once()
    assert "已清理" not in caplog.text


def test_cleanup_once_removes_orphan_files_by_mtime(client):
    """进程重启后 Runner._tasks 是空的，孤儿文件只能靠磁盘 mtime 兜底清理。"""
    import os

    old_file = app_module.REPORT_DIR / "old-task.xlsx"
    new_file = app_module.REPORT_DIR / "new-task.xlsx"
    old_file.write_bytes(b"OLD")
    new_file.write_bytes(b"NEW")

    twenty_five_hours_ago = time.time() - 25 * 3600
    os.utime(old_file, (twenty_five_hours_ago, twenty_five_hours_ago))

    app_module._cleanup_once()

    assert not old_file.exists()
    assert new_file.exists()
