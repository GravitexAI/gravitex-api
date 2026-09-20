#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""错误诊断任务的状态机与子进程执行器。

和渠道测试 Runner 的三点不同，都是被两边任务的形态差异逼出来的：

1. 并发允许两个槽而不是一个。诊断是纯本地 CPU 活，35 万行实测 ~15 秒，
   不打任何上游接口，不存在把上游打限流的问题；卡成单槽的话，一个人点了
   AI 分析（最长 120 秒）就会把另一个人挡在门外，收益为零。
2. 输入是调用方上传的 CSV，不是环境变量。每个任务独占一个工作目录，
   进去放 input.csv 和 output/，任务过期时整个目录删掉，互不干扰。
3. AI 失败不算任务失败。HTML 看板是本地确定性统计，AI 只是附加的一份
   Markdown；接口报错时看板照样可用，把整个任务判失败会让用户白等一遍。
   失败原因单独记在 ai_error 里，前端就地提示。
"""

from __future__ import annotations

import json
import os
import re
import shutil
import subprocess
import threading
import time
import uuid
from dataclasses import dataclass, field
from pathlib import Path
from typing import Optional

# diagnostics/engine.py 每读 10 万行往 stderr 打一行；拿它换算真实进度，
# 比凭空造一个匀速假进度条诚实。
PROGRESS_PATTERN = re.compile(r"已读取\s+([\d,]+)\s+行")

STAGE_AGGREGATING = "读取账单并聚合"
STAGE_AI = "调用 AI 生成分析报告"

# 子进程 stderr 保留的尾部字符数，出错时回给前端定位问题
_ERROR_TAIL_CHARS = 2000


class DiagnosisBusy(Exception):
    """并发槽已满，拒绝新任务。"""


class ArtifactNotReady(Exception):
    """任务还没产出对应文件。"""


@dataclass
class DiagnosisParams:
    """一次诊断任务的全部可变配置。"""

    csv_path: Path
    total_rows: int = 0
    max_buckets: int = 250000
    max_samples: int = 4000
    samples_per_bucket: int = 2
    evidence_chars: int = 1600
    ai_enabled: bool = False
    ai_base_url: str = ""
    ai_model: str = ""
    ai_api_key: str = ""
    ai_include_user_names: bool = False


@dataclass
class DiagnosisState:
    task_id: str
    status: str = "pending"          # pending / running / success / failed
    stage: str = ""
    done: int = 0                    # 已读取行数
    total: int = 0                   # 调用方声明的总行数
    error: str = ""
    ai_error: str = ""
    html_path: str = ""
    report_path: str = ""
    errors: int = 0                  # 错误条数（含 count 列权重）
    buckets: int = 0                 # 聚合桶数
    created_at: float = field(default_factory=time.time)
    finished_at: float = 0.0

    def to_dict(self) -> dict:
        return {
            "task_id": self.task_id,
            "status": self.status,
            "stage": self.stage,
            "done": self.done,
            "total": self.total,
            "error": self.error,
            "ai_error": self.ai_error,
            "errors": self.errors,
            "buckets": self.buckets,
            "html_ready": self.status == "success" and bool(self.html_path),
            "report_ready": self.status == "success" and bool(self.report_path),
            "created_at": self.created_at,
            "finished_at": self.finished_at,
        }


class DiagnosisRunner:
    """多槽任务执行器，每个任务独占一个工作目录。"""

    def __init__(self, tool_dir: Path, work_root: Path, python_bin: Path,
                 max_concurrent: int = 2) -> None:
        self.tool_dir = Path(tool_dir)
        self.work_root = Path(work_root)
        self.python_bin = Path(python_bin)
        self.max_concurrent = max_concurrent
        self.work_root.mkdir(parents=True, exist_ok=True)
        self._lock = threading.Lock()
        self._tasks: dict = {}
        self._running = 0

    # ---- 对外接口 ----

    def check_tool_dir(self) -> str:
        """检查诊断脚本目录完整。可用返回空串，不可用返回可操作的错误文案。"""
        entry = self.tool_dir / "error_diagnosis.py"
        if not entry.exists():
            return (f"诊断脚本不存在：{entry}。"
                    f"请把 error-diagnosis 目录传到服务同级，再跑一次 after-upload.sh")
        if not (self.tool_dir / "diagnostics" / "dashboard.html").exists():
            return f"诊断网页模板缺失：{self.tool_dir / 'diagnostics/dashboard.html'}"
        return ""

    def reserve(self) -> str:
        """占一个并发槽并登记任务，返回 task_id。

        和 submit 分开是因为调用方要先把上传的 CSV 落到任务工作目录里，
        而工作目录用 task_id 命名——先拿号，再落盘，最后才真正开跑。
        """
        with self._lock:
            if self._running >= self.max_concurrent:
                # 刻意不回显在跑的 task_id：/tasks/{id} 系列接口不校验调用方身份，
                # task_id 就是取产物的唯一凭据，回显等于把别人的凭据送出去。
                raise DiagnosisBusy(
                    f"已有 {self._running} 个诊断任务在执行中（上限 {self.max_concurrent}），请稍后重试")
            task_id = uuid.uuid4().hex
            self._tasks[task_id] = DiagnosisState(task_id=task_id, status="pending")
            self._running += 1
        return task_id

    def work_dir(self, task_id: str) -> Path:
        path = self.work_root / task_id
        path.mkdir(parents=True, exist_ok=True)
        return path

    def start(self, task_id: str, params: DiagnosisParams) -> None:
        """在 reserve 拿到的槽位上真正开跑。"""
        self._update(task_id, total=max(params.total_rows, 0))
        thread = threading.Thread(target=self._execute, args=(task_id, params), daemon=True)
        thread.start()

    def release(self, task_id: str, error: str) -> None:
        """reserve 之后、start 之前失败时释放槽位（例如上传落盘失败）。"""
        self._finish(task_id, status="failed", error=error)

    def get(self, task_id: str) -> Optional[DiagnosisState]:
        with self._lock:
            return self._tasks.get(task_id)

    def artifact(self, task_id: str, kind: str) -> Path:
        """取任务产物。kind 为 html（看板）或 report（AI Markdown）。"""
        state = self.get(task_id)
        if state is None:
            raise ArtifactNotReady(f"任务 {task_id} 不存在或已过期")
        if state.status != "success":
            raise ArtifactNotReady(f"任务 {task_id} 当前状态 {state.status}，产物尚未生成")
        raw = state.html_path if kind == "html" else state.report_path
        if not raw:
            missing = "诊断看板" if kind == "html" else "AI 分析报告"
            detail = f"：{state.ai_error}" if kind == "report" and state.ai_error else ""
            raise ArtifactNotReady(f"任务 {task_id} 没有生成{missing}{detail}")
        path = Path(raw)
        if not path.exists():
            raise ArtifactNotReady(f"任务 {task_id} 的产物文件已被清理")
        return path

    def cleanup_expired(self, ttl_seconds: int) -> int:
        """删除结束时间超过 ttl 的任务记录与工作目录，返回清理的任务数。"""
        now = time.time()
        expired = []
        with self._lock:
            for task_id in list(self._tasks):
                state = self._tasks[task_id]
                if state.status in ("pending", "running"):
                    continue
                if now - state.finished_at < ttl_seconds:
                    continue
                del self._tasks[task_id]
                expired.append(task_id)
        for task_id in expired:
            shutil.rmtree(self.work_root / task_id, ignore_errors=True)
        return len(expired)

    # ---- 内部实现 ----

    def _execute(self, task_id: str, params: DiagnosisParams) -> None:
        self._update(task_id, status="running", stage=STAGE_AGGREGATING)
        work = self.work_dir(task_id)
        try:
            # 从这里开始的每一行都必须留在 try 内：任何未捕获异常都会让这个
            # 后台线程静默死掉而不释放并发槽，导致服务逐渐拒绝所有新任务。
            summary, stderr_tail, code = self._run_diagnosis(task_id, params, work)
            if code != 0 or not summary:
                self._finish(task_id, status="failed",
                             error=f"诊断进程退出码 {code}。{stderr_tail}".strip())
                return
            html_path = Path(summary["html"])
            if not html_path.exists():
                self._finish(task_id, status="failed",
                             error=f"诊断进程正常退出但没有生成看板。{stderr_tail}".strip())
                return
            self._update(task_id, html_path=str(html_path),
                         errors=int(summary.get("errors", 0)),
                         buckets=int(summary.get("buckets", 0)))

            if params.ai_enabled:
                self._update(task_id, stage=STAGE_AI)
                report_path, ai_error = self._run_ai(params, work, html_path)
                # AI 失败只记错误，不推翻已经生成好的看板
                self._update(task_id, report_path=report_path, ai_error=ai_error)

            self._finish(task_id, status="success")
        except Exception as exc:                        # noqa: BLE001 — 任何异常都要落到任务状态里
            self._finish(task_id, status="failed", error=f"诊断任务执行失败：{exc}")

    def _run_diagnosis(self, task_id: str, params: DiagnosisParams, work: Path):
        """跑本地统计，返回 (stdout 里的 JSON 摘要, stderr 尾部, 退出码)。"""
        command = [
            str(self.python_bin), "error_diagnosis.py", str(params.csv_path),
            "--output", str(work / "output"),
            "--max-buckets", str(params.max_buckets),
            "--max-samples", str(params.max_samples),
            "--samples-per-bucket", str(params.samples_per_bucket),
            "--evidence-chars", str(params.evidence_chars),
        ]
        process = subprocess.Popen(
            command, cwd=str(self.tool_dir), env=self._base_env(),
            stdout=subprocess.PIPE, stderr=subprocess.PIPE,
            text=True, bufsize=1, encoding="utf-8", errors="replace",
        )
        # stderr 必须在独立线程里同步读干净：子进程把 OS 管道缓冲区（约 64KB）
        # 写满就会阻塞在 write，父进程这时若还在死等 stdout 的 EOF 就互相等死。
        stderr_result: list = []
        stderr_thread = threading.Thread(
            target=self._drain_progress, args=(task_id, process, stderr_result), daemon=True)
        stderr_thread.start()

        try:
            stdout = process.stdout.read()
            process.stdout.close()
            code = process.wait()
        except BaseException:
            # 读管道中途抛异常时子进程还活着，不杀掉就会变成占着 CPU 的孤儿进程
            process.kill()
            process.wait()
            raise
        stderr_thread.join()
        stderr_tail = stderr_result[0] if stderr_result else ""
        try:
            summary = json.loads(stdout) if stdout.strip() else None
        except ValueError:
            summary = None
        return summary, stderr_tail, code

    def _run_ai(self, params: DiagnosisParams, work: Path, html_path: Path):
        """跑 AI 分析，返回 (markdown 路径, 错误文案)。两者互斥，成功时错误为空。"""
        report_path = work / "ai_report.md"
        command = [
            str(self.python_bin), "ai_analysis.py", str(html_path),
            "--env", str(self.tool_dir / ".env"),
            "--output", str(report_path),
        ]
        env = self._base_env()
        # 只注入非空值：ai_analysis.read_env 会用进程环境覆盖 .env，写进空串
        # 等于把运维配好的默认地址/模型抹掉，反而让任务必然失败。
        for key, value in (("BASE_URL", params.ai_base_url),
                           ("MODEL", params.ai_model),
                           ("API_KEY", params.ai_api_key)):
            if value:
                env[key] = value
        env["AI_INCLUDE_USER_NAMES"] = "true" if params.ai_include_user_names else "false"

        result = subprocess.run(
            command, cwd=str(self.tool_dir), env=env, capture_output=True,
            text=True, encoding="utf-8", errors="replace",
        )
        if result.returncode != 0 or not report_path.exists():
            tail = (result.stderr or result.stdout or "").strip()[-_ERROR_TAIL_CHARS:]
            return "", f"AI 分析失败（看板不受影响）：{tail}" if tail else "AI 分析失败（看板不受影响）"
        return str(report_path), ""

    def _base_env(self) -> dict:
        env = dict(os.environ)
        env.update({
            "PYTHONUNBUFFERED": "1",
            # 生产环境常见 C/POSIX locale，避免子进程用非 UTF-8 编码输出中文
            "PYTHONIOENCODING": "utf-8",
        })
        # 防串配置：父进程可能为别的任务留有同名变量，AI 未开关时一律清掉
        for key in ("BASE_URL", "BASEURL", "MODEL", "API_KEY", "APIKEY",
                    "OPENAI_BASE_URL", "OPENAI_MODEL", "OPENAI_API_KEY"):
            env.pop(key, None)
        return env

    def _drain_progress(self, task_id: str, process: subprocess.Popen, result: list) -> None:
        """读空 stderr，顺带把「已读取 N 行」换算成任务进度；只保留尾部字符。"""
        tail = ""
        try:
            for line in process.stderr:
                tail = (tail + line)[-_ERROR_TAIL_CHARS:]
                match = PROGRESS_PATTERN.search(line)
                if match:
                    self._update(task_id, done=int(match.group(1).replace(",", "")))
        finally:
            process.stderr.close()
            result.append(tail)

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
            if state is None:
                return
            if state.status in ("success", "failed"):
                return                                  # 已结束，不重复释放并发槽
            state.status = status
            state.stage = ""
            if error:
                state.error = error
            state.finished_at = time.time()
            self._running = max(self._running - 1, 0)
