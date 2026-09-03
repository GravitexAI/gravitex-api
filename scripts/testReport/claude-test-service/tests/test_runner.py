#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""任务状态机与子进程执行器。

用一个假的 run_tests.py 桩脚本驱动，不发真实网络请求：
桩脚本按 @@PROGRESS@@ 协议打进度、写一个假 xlsx，然后退出。
"""

from __future__ import annotations

import sys
import time
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parent.parent))

import runner as runner_module
from runner import ReportNotReady, Runner, RunnerBusy, TaskParams

STUB = '''
import json, os, sys, time
out = ""
categories = ""
argv = sys.argv[1:]
for i, a in enumerate(argv):
    if a == "--output":
        out = argv[i + 1]
    if a == "--categories":
        categories = argv[i + 1]
print("@@PROGRESS@@ " + json.dumps({"event": "start", "total": 2}), flush=True)
print("noise line that must be ignored", flush=True)
print("@@PROGRESS@@ " + json.dumps({"event": "case", "done": 1, "total": 2,
                                    "model": "m", "case_id": "C1",
                                    "name": "n", "verdict": "PASS"}), flush=True)
time.sleep(0.2)
print("@@PROGRESS@@ " + json.dumps({"event": "case", "done": 2, "total": 2,
                                    "model": "m", "case_id": "C2",
                                    "name": "n", "verdict": "PASS"}), flush=True)
os.makedirs(os.path.dirname(out), exist_ok=True)
open(out, "wb").write(b"FAKE-XLSX")
print("@@PROGRESS@@ " + json.dumps({"event": "done", "report": out}), flush=True)
sys.stderr.write("CFG=%s|%s|%s|%s\\n" % (
    os.environ.get("CLAUDE_TEST_PLATFORM_NAME", ""),
    os.environ.get("CLAUDE_TEST_BASE_URL", ""),
    os.environ.get("CLAUDE_TEST_API_KEY", ""),
    os.environ.get("CLAUDE_TEST_MODELS", "")))
'''

FAILING_STUB = '''
import sys
sys.stderr.write("boom: config invalid\\n")
sys.exit(3)
'''

# 中文进度事件用 ensure_ascii=False 输出真实 UTF-8 字节（而非 \\uXXXX 转义），
# 用来验证 Popen 在非 UTF-8 系统 locale 下也能正确解码。
UNICODE_STUB = '''
import json, os, sys
out = ""
argv = sys.argv[1:]
for i, a in enumerate(argv):
    if a == "--output":
        out = argv[i + 1]
print("@@PROGRESS@@ " + json.dumps({"event": "start", "total": 1}, ensure_ascii=False), flush=True)
print("@@PROGRESS@@ " + json.dumps({"event": "case", "done": 1, "total": 1,
                                    "model": "claude-opus-5", "case_id": "C1",
                                    "name": "\\u4e2d\\u6587\\u7528\\u4f8b\\u540d\\u79f0",
                                    "verdict": "\\u901a\\u8fc7"}, ensure_ascii=False), flush=True)
os.makedirs(os.path.dirname(out), exist_ok=True)
open(out, "wb").write(b"FAKE-XLSX")
print("@@PROGRESS@@ " + json.dumps({"event": "done", "report": out}, ensure_ascii=False), flush=True)
'''


def _make_runner(tmp_path, stub_source=STUB):
    script_dir = tmp_path / "script"
    script_dir.mkdir()
    (script_dir / "run_tests.py").write_text(stub_source, encoding="utf-8")
    return Runner(script_dir=script_dir,
                  report_dir=tmp_path / "reports",
                  python_bin=Path(sys.executable))


def _params():
    return TaskParams(platform_name="acme", base_url="https://gw.example.com",
                      api_key="sk-test-202", report_base_url="https://vendor.example.com",
                      models=["claude-opus-5"], categories=["基础对话"])


def _wait(runner, task_id, timeout=15.0):
    deadline = time.time() + timeout
    while time.time() < deadline:
        state = runner.get(task_id)
        if state.status in ("success", "failed"):
            return state
        time.sleep(0.05)
    raise AssertionError(f"任务超时未结束，当前状态 {runner.get(task_id)}")


def test_submit_runs_to_success_and_tracks_progress(tmp_path):
    runner = _make_runner(tmp_path)
    task_id = runner.submit(_params())
    state = _wait(runner, task_id)
    assert state.status == "success"
    assert state.done == 2
    assert state.total == 2
    assert Path(state.report_path).read_bytes() == b"FAKE-XLSX"


def test_report_file_available_after_success(tmp_path):
    runner = _make_runner(tmp_path)
    task_id = runner.submit(_params())
    _wait(runner, task_id)
    assert runner.report_file(task_id).read_bytes() == b"FAKE-XLSX"


def test_report_not_ready_while_running_raises(tmp_path):
    runner = _make_runner(tmp_path)
    task_id = runner.submit(_params())
    with pytest.raises(ReportNotReady):
        runner.report_file(task_id)
    _wait(runner, task_id)


def test_second_submit_while_busy_raises(tmp_path):
    runner = _make_runner(tmp_path)
    first = runner.submit(_params())
    with pytest.raises(RunnerBusy):
        runner.submit(_params())
    _wait(runner, first)
    # 前一个跑完之后可以再提交
    second = runner.submit(_params())
    assert second != first
    _wait(runner, second)


def test_busy_message_does_not_leak_task_id(tmp_path):
    """冲突提示不得回显正在跑的 task_id。

    /tasks/{id} 和 /tasks/{id}/report 不校验调用方身份，task_id 就是取报告的
    唯一凭据。一旦 409 文案带上它，任何能触发冲突的人（哪怕填的是垃圾 token）
    都能拿到别人的报告下载凭据。uuid4 不可猜，不回显即堵死这条路。
    """
    runner = _make_runner(tmp_path)
    first = runner.submit(_params())
    with pytest.raises(RunnerBusy) as excinfo:
        runner.submit(_params())
    assert first not in str(excinfo.value)
    _wait(runner, first)


def test_failed_subprocess_records_error(tmp_path):
    runner = _make_runner(tmp_path, FAILING_STUB)
    task_id = runner.submit(_params())
    state = _wait(runner, task_id)
    assert state.status == "failed"
    assert "boom" in state.error


def test_unknown_task_returns_none(tmp_path):
    runner = _make_runner(tmp_path)
    assert runner.get("not-a-real-id") is None


def test_cleanup_expired_removes_old_reports(tmp_path):
    runner = _make_runner(tmp_path)
    task_id = runner.submit(_params())
    _wait(runner, task_id)
    assert runner.cleanup_expired(ttl_seconds=0) == 1
    assert runner.get(task_id) is None


def test_bad_params_release_slot_and_report_failure(tmp_path):
    """回归测试（Critical 槽泄漏修复）：_execute 提前抛异常也必须释放槽位。

    models 里塞一个非字符串元素，让 ",".join(params.models) 抛 TypeError；
    这行原来在 try 之外，异常会让后台线程静默死掉、_running_id 永久占用。
    """
    runner = _make_runner(tmp_path)
    bad_params = TaskParams(platform_name="acme", base_url="https://gw.example.com",
                            api_key="sk-test-202", report_base_url="https://vendor.example.com",
                            models=[1], categories=["基础对话"])
    task_id = runner.submit(bad_params)
    state = _wait(runner, task_id)
    assert state.status == "failed"
    assert state.error

    # 槽必须已经释放：紧接着能提交一个正常任务并跑到成功
    second = runner.submit(_params())
    assert second != task_id
    second_state = _wait(runner, second)
    assert second_state.status == "success"


def test_unicode_progress_not_mangled(tmp_path):
    """回归测试（encoding 修复）：子进程 stdout 的中文不能因为解码方式而乱码。

    注意：本测试在 UTF-8 locale 的机器上不会因删除 encoding 参数而失败——
    开发机的 locale.getpreferredencoding() 本来就是 UTF-8，删掉
    Popen(..., encoding="utf-8") 之后这里照样 PASSED，只有在
    LC_ALL=C/LANG=C 这类生产常见的非 UTF-8 locale 下才会暴露问题，而这个
    locale 只能在解释器启动前通过环境变量固定，测试内部改不了。真正守护
    encoding="utf-8"/errors="replace"/PYTHONIOENCODING 这三个参数的是
    test_popen_forces_utf8_encoding（下面那条契约测试）。这条测试保留是
    因为它验证了端到端中文链路本身仍然工作。
    """
    runner = _make_runner(tmp_path, UNICODE_STUB)
    task_id = runner.submit(_params())
    state = _wait(runner, task_id)
    assert state.status == "success"
    assert "中文用例名称" in state.current
    assert "通过" in state.current


class _CapturingPopen:
    """假 Popen：只记录调用参数就抛异常，不真正起子进程。"""

    def __init__(self):
        self.args = None
        self.kwargs = None

    def __call__(self, *args, **kwargs):
        self.args = args
        self.kwargs = kwargs
        raise RuntimeError("_CapturingPopen：故意不真的起子进程")


def test_popen_forces_utf8_encoding(tmp_path, monkeypatch):
    """契约测试（真正守护 F3 encoding 修复的测试）。

    test_unicode_progress_not_mangled 在 UTF-8 locale 的开发机上测不出
    "删掉 encoding 参数" 这种回归（见该测试的 docstring），因为父进程默认
    解码编码在解释器启动时就由 C 层 locale 定死，测试运行期改
    os.environ 改不动 locale.getpreferredencoding()，没有可观察的行为
    代理指标能在本机复现生产 C/POSIX locale 下的乱码。
    所以这里直接断言调用 subprocess.Popen 时传入的关键字参数——
    encoding="utf-8"、errors="replace"、env 里的 PYTHONIOENCODING=utf-8——
    这三个参数本身就是防住生产 locale 问题的契约，没有别的办法验证。
    """
    runner = _make_runner(tmp_path)
    capture = _CapturingPopen()
    monkeypatch.setattr(runner_module.subprocess, "Popen", capture)

    task_id = runner.submit(_params())
    state = _wait(runner, task_id)

    assert state.status == "failed"  # 假 Popen 抛异常，任务应记为失败而不是卡住
    assert capture.kwargs is not None, "Runner 必须调用 subprocess.Popen"
    assert capture.kwargs.get("encoding") == "utf-8"
    assert capture.kwargs.get("errors") == "replace"
    assert capture.kwargs.get("env", {}).get("PYTHONIOENCODING") == "utf-8"
