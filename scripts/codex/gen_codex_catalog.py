#!/usr/bin/env python3
"""为 Codex 客户端（CLI / Desktop / IDE 扩展）生成 Gravitex 的模型目录。

背景
----
Codex 自 2026-02 起只走 Responses API，并且它的模型元数据来自「模型目录」而不是
OpenAI 经典的 /v1/models。目录有三层，优先级从低到高：

    1. 编译进 Codex 二进制的内置目录
    2. 远端目录（只有官方 ChatGPT 账号拉得到，缓存在 ~/.codex/models_cache.json）
    3. config.toml 里 model_catalog_json 指向的本地文件  ← 本脚本生成这一层

第 3 层覆盖前两层，所以接第三方网关时它是唯一可靠的抓手。缺少 slug /
context_window 这些字段，Codex 会整段反序列化失败，拿不到上下文窗口，会话起不来。

用法
----
    python3 gen_codex_catalog.py \
        --base-url https://your-gravitex-host/v1 \
        --api-key  sk-xxxx

默认写到 ~/.codex/gravitex-codex-catalog.json，并打印需要贴进 config.toml 的片段。
"""

from __future__ import annotations

import argparse
import json
import os
import sys
import urllib.error
import urllib.request
from pathlib import Path
from typing import Any

# 网关按 Codex 方言返回目录的触发条件：带上 client_version。
# 其他客户端不会带这个参数，所以网关靠它区分要发哪种信封。
DEFAULT_CLIENT_VERSION = "0.153.0"

# 网关未升级、只会返回 OpenAI 经典格式时的兜底窗口。
# 与其让 Codex 拿不到值直接失败，不如给一个保守值先把会话跑起来。
FALLBACK_CONTEXT_WINDOW = 128000

REASONING_LEVELS = [
    {"effort": "low", "description": "Fast responses with lighter reasoning"},
    {"effort": "medium", "description": "Balances speed and reasoning depth for everyday tasks"},
    {"effort": "high", "description": "Greater reasoning depth for complex problems"},
    {"effort": "xhigh", "description": "Extra high reasoning depth for complex problems"},
]


def fetch_json(url: str, api_key: str) -> dict[str, Any]:
    request = urllib.request.Request(
        url,
        headers={
            "Authorization": f"Bearer {api_key}",
            "Accept": "application/json",
            "User-Agent": "gravitex-codex-catalog/1.0",
        },
    )
    try:
        with urllib.request.urlopen(request, timeout=60) as response:
            return json.loads(response.read().decode("utf-8"))
    except urllib.error.HTTPError as exc:
        detail = exc.read().decode("utf-8", "replace")[:500]
        raise SystemExit(f"请求 {url} 失败：HTTP {exc.code}\n{detail}") from exc
    except Exception as exc:
        raise SystemExit(f"请求 {url} 失败：{exc}") from exc


def convert_openai_payload(payload: dict[str, Any]) -> list[dict[str, Any]]:
    """把 OpenAI 经典的 {"object":"list","data":[...]} 转成最小可用的 Codex 条目。

    只有网关还没支持 Codex 方言时才会走到这里。经典格式里没有上下文窗口，
    只能退到 FALLBACK_CONTEXT_WINDOW，所以这条路径必须显式告警。
    """
    models = []
    for index, item in enumerate(payload.get("data") or []):
        slug = (item.get("id") or "").strip()
        if not slug:
            continue
        models.append(
            {
                "slug": slug,
                "display_name": slug,
                "description": "",
                "context_window": FALLBACK_CONTEXT_WINDOW,
                "max_context_window": FALLBACK_CONTEXT_WINDOW,
                "auto_compact_token_limit": None,
                "default_reasoning_level": None,
                "supported_reasoning_levels": [],
                "default_reasoning_summary": "none",
                "supports_reasoning_summaries": False,
                "supports_reasoning_summary_parameter": False,
                "input_modalities": ["text"],
                "supports_image_detail_original": False,
                "apply_patch_tool_type": "freeform",
                "web_search_tool_type": "text",
                "supports_search_tool": False,
                "shell_type": "shell_command",
                "tool_mode": None,
                "truncation_policy": {"mode": "tokens", "limit": 10000},
                "supports_parallel_tool_calls": False,
                "experimental_supported_tools": [],
                "support_verbosity": False,
                "default_verbosity": "low",
                "prefer_websockets": False,
                "use_responses_lite": False,
                "include_skills_usage_instructions": False,
                "include_apps_usage_instructions": False,
                "include_plugin_usage_instructions": False,
                "node_repl_auto_review_required": False,
                "node_repl_disabled": False,
                "auto_review_model_override": None,
                "multi_agent_version": None,
                "model_specialty": None,
                "default_service_tier": None,
                "service_tiers": [],
                "additional_speed_tiers": [],
                "visibility": "list",
                "supported_in_api": True,
                "priority": index + 1,
                "minimal_client_version": "0.0.0",
                "availability_nux": None,
                "upgrade": None,
                "comp_hash": "",
            }
        )
    return models


def build_catalog(base_url: str, api_key: str, client_version: str) -> tuple[list[dict[str, Any]], bool]:
    """返回 (模型条目, 是否走了 Codex 方言)。"""
    url = f"{base_url.rstrip('/')}/models?client_version={client_version}"
    payload = fetch_json(url, api_key)

    # 网关已支持 Codex 方言：顶层就是 models 数组，直接用。
    if isinstance(payload.get("models"), list):
        return payload["models"], True

    # 否则退回经典格式转换。
    return convert_openai_payload(payload), False


def main() -> int:
    parser = argparse.ArgumentParser(description="生成 Codex model_catalog_json")
    parser.add_argument("--base-url", required=True, help="Gravitex 接口地址，以 /v1 结尾，例如 https://host/v1")
    parser.add_argument("--api-key", default=os.environ.get("GRAVITEX_API_KEY", ""), help="API Key，也可用 GRAVITEX_API_KEY 环境变量")
    parser.add_argument("--client-version", default=DEFAULT_CLIENT_VERSION, help="上报的 Codex 客户端版本")
    parser.add_argument("--output", default=str(Path.home() / ".codex" / "gravitex-codex-catalog.json"))
    parser.add_argument("--provider-name", default="gravitex", help="写进 config.toml 的 provider 名")
    args = parser.parse_args()

    if not args.api_key:
        raise SystemExit("缺少 API Key：用 --api-key 传入，或设置 GRAVITEX_API_KEY 环境变量")

    models, native = build_catalog(args.base_url, args.api_key, args.client_version)
    if not models:
        raise SystemExit("网关没有返回任何可用模型，请先确认这个 Key 的分组里有启用模型")

    output_path = Path(os.path.expanduser(args.output))
    output_path.parent.mkdir(parents=True, exist_ok=True)
    output_path.write_text(json.dumps({"models": models}, indent=2, ensure_ascii=False), encoding="utf-8")

    print(f"✅ 已写入 {output_path}（{len(models)} 个模型）")
    if not native:
        print(
            "⚠️  网关返回的是 OpenAI 经典格式，没有上下文窗口信息，"
            f"已统一按 {FALLBACK_CONTEXT_WINDOW} 兜底。\n"
            "    这个值不准会导致 Codex 过早压缩上下文或请求超限，请让网关升级到支持 Codex 方言的版本。"
        )

    print("\n把下面这段贴进 ~/.codex/config.toml（必须是用户级，项目里的 .codex/ 不生效）：\n")
    print(f'model = "{models[0]["slug"]}"')
    print(f'model_provider = "{args.provider_name}"')
    print(f'model_catalog_json = "{output_path}"')
    print()
    print(f"[model_providers.{args.provider_name}]")
    print(f'name = "{args.provider_name}"')
    print(f'base_url = "{args.base_url.rstrip("/")}"')
    print('env_key = "GRAVITEX_API_KEY"')
    print('wire_api = "responses"')
    print(
        "\n改完之后：\n"
        "  1. export GRAVITEX_API_KEY=...（写进 shell profile）\n"
        "  2. 完全退出 Codex Desktop 再重开，目录只在启动时读一次\n"
        "  3. 列表还是旧的就删掉 ~/.codex/models_cache.json 强制重建\n"
        "  4. 终端里跑 codex 然后输入 /model 验证，CLI 不受 Desktop 那个已知过滤 bug 影响"
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
