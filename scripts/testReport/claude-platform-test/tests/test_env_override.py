#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""config.py 的环境变量覆盖层。

不设环境变量时必须与改造前完全一致；设了则覆盖，且 REPORT_FILENAME
要跟着新的 PLATFORM_NAME 走（它在模块级用 f-string 拼装）。
"""

from __future__ import annotations

import importlib
import sys
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parent.parent))

import config as config_module


@pytest.fixture
def reload_config(monkeypatch):
    """在设置环境变量后重新 import config，用完还原成无环境变量的状态。"""

    def _reload():
        return importlib.reload(config_module)

    yield _reload
    monkeypatch.undo()
    importlib.reload(config_module)


def test_env_helpers_fall_back_to_default_when_unset(monkeypatch, reload_config):
    """未设置环境变量时，三个 _env 辅助函数必须返回传入的字面默认值。

    这里用一个业务上不存在的变量名来验证"回落"这个机制本身，而**不是**去断言
    PLATFORM_NAME / BASE_URL 等业务常量的具体取值——config.py 的设计初衷就是
    "换平台时只改这里"，把业务默认值写进断言等于把配置文件锁死，任何一次正常的
    换平台都会让测试变红。机制对了，业务值改成什么都不影响正确性。
    """
    cfg = reload_config()
    monkeypatch.delenv("CLAUDE_TEST_NOT_A_REAL_KEY", raising=False)
    assert cfg._env("NOT_A_REAL_KEY", "fallback") == "fallback"
    assert cfg._env_list("NOT_A_REAL_KEY", ["a", "b"]) == ["a", "b"]
    assert cfg._env_bool("NOT_A_REAL_KEY", True) is True
    assert cfg._env_bool("NOT_A_REAL_KEY", False) is False


def test_business_constants_are_wired_through_env_layer(reload_config):
    """业务常量必须真的走了覆盖层，且类型/非空符合预期。

    只校验"接上了、类型对"，不校验具体值（见上一个测试的说明）。
    """
    cfg = reload_config()
    assert isinstance(cfg.PLATFORM_NAME, str) and cfg.PLATFORM_NAME
    assert isinstance(cfg.BASE_URL, str) and cfg.BASE_URL.startswith("http")
    assert cfg.AUTH_MODE in ("bearer", "anthropic")
    assert isinstance(cfg.MODELS, list) and cfg.MODELS
    assert cfg.CATEGORIES == []           # 未设环境变量时恒为空 = 跑全部分类
    assert cfg.REPORT_FILENAME == f"claude-测试报告-{cfg.PLATFORM_NAME}.xlsx"


def test_platform_name_override_also_changes_report_filename(monkeypatch, reload_config):
    monkeypatch.setenv("CLAUDE_TEST_PLATFORM_NAME", "acme")
    cfg = reload_config()
    assert cfg.PLATFORM_NAME == "acme"
    assert cfg.REPORT_FILENAME == "claude-测试报告-acme.xlsx"


def test_url_and_auth_overrides(monkeypatch, reload_config):
    monkeypatch.setenv("CLAUDE_TEST_BASE_URL", "https://gw.example.com")
    monkeypatch.setenv("CLAUDE_TEST_REPORT_BASE_URL", "https://vendor.example.com")
    monkeypatch.setenv("CLAUDE_TEST_AUTH_MODE", "anthropic")
    monkeypatch.setenv("CLAUDE_TEST_API_KEY", "sk-test-202")
    cfg = reload_config()
    assert cfg.BASE_URL == "https://gw.example.com"
    assert cfg.REPORT_BASE_URL == "https://vendor.example.com"
    assert cfg.AUTH_MODE == "anthropic"
    assert cfg.API_KEY == "sk-test-202"
    assert cfg.report_base_url() == "https://vendor.example.com"
    assert cfg.report_differs_from_actual() is True


def test_models_list_override_trims_and_drops_blanks(monkeypatch, reload_config):
    monkeypatch.setenv("CLAUDE_TEST_MODELS", " claude-opus-5 , claude-sonnet-5 ,, ")
    cfg = reload_config()
    assert cfg.MODELS == ["claude-opus-5", "claude-sonnet-5"]
    cfg.validate_config()


def test_categories_override(monkeypatch, reload_config):
    monkeypatch.setenv("CLAUDE_TEST_CATEGORIES", "基础对话,提示词缓存")
    cfg = reload_config()
    assert cfg.CATEGORIES == ["基础对话", "提示词缓存"]


def test_print_http_override(monkeypatch, reload_config):
    monkeypatch.setenv("CLAUDE_TEST_PRINT_HTTP", "0")
    cfg = reload_config()
    assert cfg.PRINT_HTTP is False


def test_adding_a_new_model_via_env_needs_no_script_change(monkeypatch, reload_config):
    """核心价值验证：往 MODELS 里加一个没写进 MODEL_OVERRIDES 的新模型，
    不用改 config.py 就能自动获得合理的 family / thinking / sampling_parameters / case_id。
    """
    monkeypatch.setenv("CLAUDE_TEST_MODELS", "claude-opus-5,claude-brandnew-9")
    cfg = reload_config()
    cfg.validate_config()  # 不应该报错

    assert cfg.MODELS == ["claude-opus-5", "claude-brandnew-9"]
    assert "claude-brandnew-9" not in cfg.MODEL_OVERRIDES

    meta = cfg.MODEL_METADATA["claude-brandnew-9"]
    assert meta["family"] == "brandnew"
    assert meta["thinking"] == "adaptive"

    case = cfg.MODEL_CASES["claude-brandnew-9"]
    assert case["sampling_parameters"] == "expected_4xx"
    assert case["case_id"] == "MODEL-002"


def test_validate_config_rejects_invalid_override_value(monkeypatch, reload_config):
    cfg = reload_config()
    monkeypatch.setattr(cfg, "MODEL_OVERRIDES", {cfg.MODELS[0]: {"thinking": "wrong"}})
    with pytest.raises(ValueError):
        cfg.validate_config()


def test_validate_config_rejects_unknown_override_field(monkeypatch, reload_config):
    cfg = reload_config()
    monkeypatch.setattr(cfg, "MODEL_OVERRIDES", {cfg.MODELS[0]: {"thinkng": "manual"}})
    with pytest.raises(ValueError):
        cfg.validate_config()
