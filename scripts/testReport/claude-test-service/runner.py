#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""渠道测试任务的状态机与子进程执行器。

为什么必须起子进程而不是同进程调 run_tests.main()：
  claude-platform-test 有多处模块级全局状态——cases/latency.py 的 _SAMPLES、
  media.py 的下载缓存、config.py 的全部常量。同一个进程里跑两个任务会串数据，
  而且 config 的常量在 import 之后没法按请求切换。子进程 + 环境变量注入是
  唯一干净的做法。

为什么并发只开一个槽：
  脚本内部已经是 config.MAX_WORKERS=8 的线程池并发，再叠加多任务必然打爆
  上游限流，429 会被用例判成"能力缺失"，报告就没有参考价值了。
"""

from __future__ import annotations

import json
import os
import subprocess
import threading
import time
import uuid
from dataclasses import dataclass, field
from pathlib import Path
from typing import Optional

PROGRESS_MARKER = "@@PROGRESS@@ "

# 子进程 stderr 保留的尾部字符数，出错时回给前端定位问题
_ERROR_TAIL_CHARS = 2000


class RunnerBusy(Exception):
    """已有任务在执行中，拒绝新任务。"""


class ReportNotReady(Exception):
    """任务还没成功结束，报告文件不存在。"""


class PythonBinUnavailable(Exception):
    """跑测试用的 Python 解释器不可用（通常是 .venv 被跨机器复制过来了）。"""


@dataclass
class TaskParams:
    """一次测试任务的全部可变配置，逐项映射成 CLAUDE_TEST_* 环境变量。"""

    platform_name: str
    base_url: str
    api_key: str
    report_base_url: str
    models: list
    categories: list


@dataclass
class TaskState:
    task_id: str
    status: str = "pending"          # pending / running / success / failed
    done: int = 0
    total: int = 0
    current: str = ""
    error: str = ""
    report_path: str = ""
    created_at: float = field(default_factory=time.time)
    finished_at: float = 0.0

    def to_dict(self) -> dict:
        return {
            "task_id": self.task_id,
            "status": self.status,
            "done": self.done,
            "total": self.total,
            "current": self.current,
            "error": self.error,
            "report_ready": bool(self.report_path) and self.status == "success",
            "created_at": self.created_at,
            "finished_at": self.finished_at,
        }


class Runner:
    """单槽任务执行器：同一时刻最多跑一个测试任务。"""

    def __init__(self, script_dir: Path, report_dir: Path, python_bin: Path) -> None:
        self.script_dir = Path(script_dir)
        self.report_dir = Path(report_dir)
        self.python_bin = Path(python_bin)
        self.report_dir.mkdir(parents=True, exist_ok=True)
        self._lock = threading.Lock()
        self._tasks: dict = {}
        self._running_id: Optional[str] = None

    # ---- 对外接口 ----

    def check_python_bin(self) -> str:
        """检查跑测试用的 Python 解释器可用。可用返回空串，不可用返回可操作的错误文案。

        为什么要单独检查：这个解释器是 claude-platform-test/.venv/bin/python，而
        venv 里的软链和脚本 shebang 都是写死的绝对路径。只要有人把 macOS 上的
        .venv 传到服务器（zip/Finder/SFTP 都会带上它），这个软链就会指向
        /Library/Developer/... 变成断链。此时 Popen 只会抛一句
        "[Errno 2] No such file or directory: '.../.venv/bin/python'"——
        文件明明"在"（是个软链），报错却说找不到，排查起来很费时间。
        这个坑在本项目上已经发生三次，所以把它变成一条能照着做的提示。
        """
        path = self.python_bin
        if path.exists() and os.access(str(path), os.X_OK):
            return ""

        if path.is_symlink():
            target = os.readlink(str(path))
            reason = f"{path} 是断链，指向不存在的 {target}"
            if "/Library/" in target or "/Users/" in target:
                reason += "（这是 macOS 路径，说明 .venv 是从 Mac 传上来的）"
        elif not path.exists():
            reason = f"{path} 不存在"
        else:
            reason = f"{path} 存在但不可执行"

        return (
            f"测试脚本的 Python 环境不可用：{reason}。"
            f"venv 不能跨机器复制（里面全是绝对路径），必须在本机重建："
            f"cd {self.script_dir} && python3 -m venv --clear .venv "
            f"&& .venv/bin/pip install -r requirements.txt"
        )

    def submit(self, params: TaskParams) -> str:
        broken = self.check_python_bin()
        if broken:
            raise PythonBinUnavailable(broken)

        with self._lock:
            if self._running_id is not None:
                # 刻意不回显 task_id：本服务的 /tasks/{id} 和 /tasks/{id}/report
                # 不校验调用方身份，task_id 就是取报告的唯一凭据。把它写进冲突
                # 提示里，等于让任何能触发 409 的人拿到别人的报告下载凭据。
                # uuid4 本身不可猜，不回显即堵死这条路。
                raise RunnerBusy("已有测试任务在执行中，请等它结束")
            task_id = uuid.uuid4().hex
            self._tasks[task_id] = TaskState(task_id=task_id, status="pending")
            self._running_id = task_id

        thread = threading.Thread(target=self._execute, args=(task_id, params), daemon=True)
        thread.start()
        return task_id

    def get(self, task_id: str) -> Optional[TaskState]:
        with self._lock:
            return self._tasks.get(task_id)

    def report_file(self, task_id: str) -> Path:
        state = self.get(task_id)
        if state is None:
            raise ReportNotReady(f"任务 {task_id} 不存在")
        if state.status != "success" or not state.report_path:
            raise ReportNotReady(f"任务 {task_id} 当前状态 {state.status}，报告尚未生成")
        path = Path(state.report_path)
        if not path.exists():
            raise ReportNotReady(f"任务 {task_id} 的报告文件已被清理")
        return path

    def cleanup_expired(self, ttl_seconds: int) -> int:
        """删除结束时间超过 ttl 的任务记录和报告文件，返回清理的任务数。"""
        now = time.time()
        removed = 0
        with self._lock:
            for task_id in list(self._tasks):
                state = self._tasks[task_id]
                if state.status in ("pending", "running"):
                    continue
                if now - state.finished_at < ttl_seconds:
                    continue
                if state.report_path:
                    Path(state.report_path).unlink(missing_ok=True)
                del self._tasks[task_id]
                removed += 1
        return removed

    # ---- 内部实现 ----

    def _execute(self, task_id: str, params: TaskParams) -> None:
        self._update(task_id, status="running")
        process = None
        try:
            # 从这里开始的每一行都必须留在 try 内：任何未捕获异常都会让这个
            # 后台线程静默死掉而不释放 _running_id，导致服务永久拒绝新任务。
            report_path = self.report_dir / f"{task_id}.xlsx"
            command = [
                str(self.python_bin), "run_tests.py",
                "--output", str(report_path),
                "--progress-json",
            ]
            if params.categories:
                command += ["--categories", ",".join(params.categories)]

            env = dict(os.environ)
            env.update({
                "CLAUDE_TEST_PLATFORM_NAME": params.platform_name,
                "CLAUDE_TEST_BASE_URL": params.base_url,
                "CLAUDE_TEST_API_KEY": params.api_key,
                "CLAUDE_TEST_REPORT_BASE_URL": params.report_base_url,
                "CLAUDE_TEST_MODELS": ",".join(params.models),
                "CLAUDE_TEST_CATEGORIES": ",".join(params.categories),
                # 关掉 HTTP 报文打印：一次全量测试会打出几十 MB 正文，
                # 既拖慢管道读取，也淹没进度行
                "CLAUDE_TEST_PRINT_HTTP": "0",
                "PYTHONUNBUFFERED": "1",
                # 生产环境（Linux/Docker）常见 C/POSIX locale，双保险避免子进程
                # 用非 UTF-8 编码输出中文 JSON/横幅
                "PYTHONIOENCODING": "utf-8",
            })

            process = subprocess.Popen(
                command, cwd=str(self.script_dir), env=env,
                stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                text=True, bufsize=1,
                encoding="utf-8", errors="replace",
            )

            # stderr 必须在独立线程里同步读干净：子进程一旦把 OS 管道缓冲区
            # （约 64KB）写满就会阻塞在 write，若父进程这时还在死等 stdout
            # 的 EOF，就会互相等死，整个单槽服务卡死。
            stderr_result: list = []
            stderr_thread = threading.Thread(
                target=self._drain_stderr, args=(process, stderr_result), daemon=True,
            )
            stderr_thread.start()

            for line in process.stdout:
                if not line.startswith(PROGRESS_MARKER):
                    continue
                try:
                    payload = json.loads(line[len(PROGRESS_MARKER):])
                except ValueError:
                    continue
                self._apply_progress(task_id, payload)
            process.stdout.close()

            return_code = process.wait()
            stderr_thread.join()
            stderr_tail = stderr_result[0] if stderr_result else ""
        except Exception as exc:                       # noqa: BLE001 — 任何异常都要落到任务状态里
            if process is not None:
                try:
                    process.kill()
                    process.wait()
                except Exception:                       # noqa: BLE001 — kill 失败不能掩盖原始异常
                    pass
            self._finish(task_id, status="failed", error=f"启动测试进程失败：{exc}")
            return

        state = self.get(task_id)
        if return_code != 0:
            self._finish(task_id, status="failed",
                         error=f"测试进程退出码 {return_code}。{stderr_tail}".strip())
        elif not state.report_path or not Path(state.report_path).exists():
            self._finish(task_id, status="failed",
                         error=f"测试进程正常退出但没有生成报告。{stderr_tail}".strip())
        else:
            self._finish(task_id, status="success")

    @staticmethod
    def _drain_stderr(process: subprocess.Popen, result: list) -> None:
        """在独立线程里持续读空子进程 stderr，只保留尾部字符，避免管道写满死锁。"""
        tail = ""
        try:
            for line in process.stderr:
                tail = (tail + line)[-_ERROR_TAIL_CHARS:]
        finally:
            process.stderr.close()
            result.append(tail)

    def _apply_progress(self, task_id: str, payload: dict) -> None:
        event = payload.get("event")
        if event == "start":
            self._update(task_id, total=int(payload.get("total", 0)))
        elif event == "case":
            self._update(
                task_id,
                done=int(payload.get("done", 0)),
                total=int(payload.get("total", 0)),
                current=f"{payload.get('model', '')} {payload.get('case_id', '')} "
                        f"{payload.get('name', '')} -> {payload.get('verdict', '')}".strip(),
            )
        elif event == "done":
            self._update(task_id, report_path=str(payload.get("report", "")))

    def _update(self, task_id: str, **fields) -> None:
        with self._lock:
            state = self._tasks.get(task_id)
            if state is None:
                return
            for key, value in fields.items():
                setattr(state, key, value)

    def _finish(self, task_id: str, status: str, error: str = "") -> None:
        with self._lock:
            state = self._tasks.get(task_id)
            if state is not None:
                state.status = status
                state.error = error
                state.finished_at = time.time()
            if self._running_id == task_id:
                self._running_id = None
