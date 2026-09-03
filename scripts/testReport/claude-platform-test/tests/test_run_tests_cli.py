#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""run_tests.py 的命令行参数与进度输出协议。

进度行用 @@PROGRESS@@ 前缀是因为 PRINT_HTTP 会往 stdout 打大量 HTTP 报文，
测试服务需要一个不会和正文混淆的标记来提取进度。
"""

from __future__ import annotations

import json
import subprocess
import sys
from pathlib import Path

SCRIPT_DIR = Path(__file__).resolve().parent.parent
PYTHON_BIN = SCRIPT_DIR / ".venv" / "bin" / "python"


def _run(args, env_extra=None):
    import os
    env = dict(os.environ)
    if env_extra:
        env.update(env_extra)
    return subprocess.run(
        [str(PYTHON_BIN), "run_tests.py", *args],
        cwd=str(SCRIPT_DIR), env=env, capture_output=True, text=True, timeout=120,
    )


def test_validate_config_reports_filtered_case_count():
    result = _run(["--validate-config", "--categories", "基础对话,提示词缓存"])
    assert result.returncode == 0, result.stderr
    assert "7 个用例" in result.stdout


def test_validate_config_full_case_count():
    result = _run(["--validate-config"])
    assert result.returncode == 0, result.stderr
    assert "37 个用例" in result.stdout


def test_categories_env_var_is_equivalent_to_flag():
    result = _run(["--validate-config"], {"CLAUDE_TEST_CATEGORIES": "基础对话"})
    assert result.returncode == 0, result.stderr
    assert "5 个用例" in result.stdout


def test_unknown_category_fails_fast():
    result = _run(["--validate-config", "--categories", "不存在的分类"])
    assert result.returncode != 0
    assert "未知的用例分类" in (result.stdout + result.stderr)


def test_progress_json_emits_start_event_on_validate(tmp_path):
    """--validate-config 不发请求，但仍应在 --progress-json 下打出 start 事件。

    total 用「模型数 × 该分类用例数」动态算，不写死数字——config.MODELS 里启用
    几个模型是运维随时会调的配置，写死会让正常换配置把测试搞红。
    """
    result = _run(["--validate-config", "--categories", "基础对话", "--progress-json"])
    assert result.returncode == 0, result.stderr
    lines = [ln for ln in result.stdout.splitlines() if ln.startswith("@@PROGRESS@@ ")]
    assert lines, result.stdout
    payload = json.loads(lines[0][len("@@PROGRESS@@ "):])
    assert payload["event"] == "start"

    sys.path.insert(0, str(SCRIPT_DIR))
    import cases
    import config
    basic_case_count = cases.CATEGORY_CASE_COUNTS["基础对话"]
    assert payload["cases"] == basic_case_count
    assert payload["models"] == len(config.MODELS)
    assert payload["total"] == basic_case_count * len(config.MODELS)
