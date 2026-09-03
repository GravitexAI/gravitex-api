#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
Claude 资源测试脚本 —— 全局配置
================================

所有"平台相关"的配置都集中在本文件，换平台时只改这里即可。
直接修改下面的变量值就行。

仅测试 Anthropic 原生协议端点：POST {BASE_URL}/v1/messages

鉴权二选一（互斥）：
  - AUTH_MODE = "anthropic"  ->  x-api-key + anthropic-version
  - AUTH_MODE = "bearer"     ->  Authorization: Bearer <key>
"""

from __future__ import annotations

import os


# =============================================================================
# 0. 环境变量覆盖层
#
#    所有配置都可以用 CLAUDE_TEST_<常量名> 环境变量临时覆盖，不改代码。
#    留空或不设置 -> 用下面写死的默认值，行为与改造前完全一致。
#    渠道测试报告服务靠这一层给每个子进程注入"测哪个渠道"的参数。
# =============================================================================
def _env(name: str, default: str) -> str:
    """读 CLAUDE_TEST_<name>，未设置或空串则回落到 default。"""
    value = os.environ.get(f"CLAUDE_TEST_{name}")
    return value if value not in (None, "") else default


def _env_list(name: str, default: list) -> list:
    """读逗号分隔的环境变量，去空白、丢空项。未设置则回落到 default。"""
    value = os.environ.get(f"CLAUDE_TEST_{name}")
    if value in (None, ""):
        return default
    return [item.strip() for item in value.split(",") if item.strip()]


def _env_bool(name: str, default: bool) -> bool:
    """读布尔环境变量，"0"/"false"/"no" 视为 False（大小写不敏感）。"""
    value = os.environ.get(f"CLAUDE_TEST_{name}")
    if value in (None, ""):
        return default
    return value.strip().lower() not in ("0", "false", "no")


# =============================================================================
# 1. 被测平台标识（写进报告标题）
# =============================================================================
PLATFORM_NAME = _env("PLATFORM_NAME", "xftoken")

# =============================================================================
# 2. 接口地址（不要带末尾斜杠，脚本会自动拼 /v1/messages）
#
#    BASE_URL 是脚本**实际发起请求**的地址。
#    如果你是把别人家的资源接到自己平台上测（真实调用走自己平台，
#    但报告要给对方看资源方的原始地址），就填下面的 REPORT_BASE_URL。
# =============================================================================
BASE_URL = _env("BASE_URL", "https://api.gravitex.ai")

# —— 报告展示用地址（留空 = 跟 BASE_URL 一致）————————————————————————
#   只影响两处展示：①「测试汇总」的接口地址 ②「测试明细」的调用样例(curl)
#   实际请求依然发往 BASE_URL，不受此项影响。
REPORT_BASE_URL = _env("REPORT_BASE_URL", "https://api.xftoken.ctclouds.com")

# =============================================================================
# 3. 测试模型列表 —— 支持多个模型（opus / haiku / sonnet / fable 及未来任意系列）
#    报告里每个模型都会单独出结果，并在"测试汇总"里对比通过率。
#
#    模型能力矩阵（Anthropic 官方规则）：
#      - Opus 4.7 / 4.8, Sonnet 5, Fable/Mythos 5:  只支持 adaptive
#      - Opus 4.6, Sonnet 4.6:                       adaptive + manual(已废弃)
#      - Haiku 4.5 / Sonnet 4.5 及以下:              只支持 manual
#
#    以下 MODEL_METADATA / MODEL_CASES 不再手写，而是从 MODELS 按“新模型默认
#    形态”（adaptive thinking + 采样参数预期 4xx）自动推导；只有不符合这个
#    默认形态的模型才需要写进 MODEL_OVERRIDES 例外表。
#    **加模型只需要在 MODELS 里加一行（或用 CLAUDE_TEST_MODELS 环境变量），
#    不用改这个文件的其它地方。**
# =============================================================================
MODELS = _env_list("MODELS", [
    "claude-fable-5",
    "claude-fable-5-1",
    "claude-haiku-4-5-20251001",
    "claude-opus-4-8",
    "claude-sonnet-5",
    "claude-opus-5"
])

_DEFAULT_THINKING = "adaptive"
_DEFAULT_SAMPLING = "expected_4xx"


def _derive_family(model: str) -> str:
    """从模型名推断系列：claude-opus-4-8 → opus，claude-fable-5-1 → fable。

    取 "claude-" 前缀之后的第一个 "-" 分段作为系列名；不以 claude- 开头的
    名字直接取整个名字的第一段。这样版本号（4-8 / 5-1 等）不会被误认成系列名。
    """
    name = model[len("claude-"):] if model.startswith("claude-") else model
    return name.split("-")[0] if name else model


# 例外覆盖表：默认所有模型都按“新模型”处理（adaptive thinking + 采样参数预期 4xx）。
# 只有不符合这个形态的模型才写进来。
#
# 为什么 thinking 和 sampling_parameters 要分成两个独立字段而不是互相推导：
# 官方矩阵里 Opus 4.6 / Sonnet 4.6 那一代是 thinking=adaptive 但采样参数
# 预期 2xx(supported_without_thinking)，两个轴在那一代是独立的。写死
# "adaptive ⇒ expected_4xx" 将来接 4.6 系会把正常行为误判成缺陷。
MODEL_OVERRIDES = {
    "claude-haiku-4-5-20251001": {
        "thinking": "manual",
        "sampling_parameters": "supported_without_thinking",
    },
}

# 按模型系列和官方思考模式分组，报告和 thinking 用例共用这份矩阵。
MODEL_METADATA = {
    model: {
        "family": _derive_family(model),
        "thinking": MODEL_OVERRIDES.get(model, {}).get("thinking", _DEFAULT_THINKING),
    }
    for model in MODELS
}

# 每个模型一个独立的“模型能力 Case”。
# 这不是额外的 API 请求，而是报告中的官方预期基线；实际请求仍按
# ALL_CASES 逐模型执行，避免为登记模型能力而重复消耗配额。
#
# 依据：Anthropic Extended Thinking / Messages / Web Search 文档。
# sampling_parameters 表示本项目“不启用 thinking”的参数边界用例预期；
# 参数边界用例会单独记录每个模型的实际 HTTP 状态。
# case_id 按 MODELS 的顺序生成三位编号（MODEL-001…），与原来一致。
MODEL_CASES = {
    model: {
        "case_id": f"MODEL-{index:03d}",
        "family": MODEL_METADATA[model]["family"],
        "thinking": MODEL_METADATA[model]["thinking"],
        "sampling_parameters": MODEL_OVERRIDES.get(model, {}).get(
            "sampling_parameters", _DEFAULT_SAMPLING),
        "max_tokens_128001": "expected_4xx",
        "web_search": "test_stream_and_non_stream", "prompt_cache": {"5m", "1h"},
    }
    for index, model in enumerate(MODELS, start=1)
}


def model_family(model: str) -> str:
    return MODEL_METADATA[model]["family"]


def model_series(model: str) -> str:
    """返回报告使用的模型系列名称。Fable 保持独立系列。"""
    return MODEL_METADATA[model]["family"].capitalize()


def thinking_mode(model: str) -> str:
    return MODEL_METADATA[model]["thinking"]


def validate_config() -> None:
    """Validate local model metadata without making a network request.

    MODELS 可以自由增删（注释掉几个模型只跑其中一部分也行），只要求：
    非空、不重复，且每个模型都能推导出非空 family。MODEL_METADATA / MODEL_CASES
    现在是从 MODELS 自动生成的，因此"待测模型在字典里查得到"这类检查恒真，不再需要。

    MODEL_OVERRIDES 不要求它的 key 都出现在 MODELS 里——运维可能预置了尚未
    启用模型的覆盖项，那样报错反而碍事；只校验 override 本身写得对：
    字段名不能拼错，取值必须是合法枚举。
    """
    if not MODELS:
        raise ValueError("MODELS 不能为空，至少配置 1 个待测模型")
    if len(set(MODELS)) != len(MODELS):
        raise ValueError("MODELS 存在重复模型")
    empty_family = [model for model in MODELS if not _derive_family(model)]
    if empty_family:
        raise ValueError(f"以下模型无法推导出 family：{empty_family}")

    valid_thinking = {"manual", "adaptive"}
    valid_sampling = {"expected_4xx", "supported_without_thinking"}
    valid_override_fields = {"thinking", "sampling_parameters"}
    for model, override in MODEL_OVERRIDES.items():
        unknown_fields = set(override) - valid_override_fields
        if unknown_fields:
            raise ValueError(f"MODEL_OVERRIDES[{model!r}] 出现未知字段：{unknown_fields}")
        if "thinking" in override and override["thinking"] not in valid_thinking:
            raise ValueError(f"MODEL_OVERRIDES[{model!r}] 的 thinking 取值无效")
        if "sampling_parameters" in override and override["sampling_parameters"] not in valid_sampling:
            raise ValueError(f"MODEL_OVERRIDES[{model!r}] 的 sampling_parameters 取值无效")

    case_ids = [case["case_id"] for case in MODEL_CASES.values()]
    if len(set(case_ids)) != len(case_ids):
        raise ValueError("MODEL_CASES 的 case_id 必须唯一")

# =============================================================================
# 3-2. 用例分类白名单（空列表 = 跑全部分类）
#
#    12 个分类：基础对话 / 视觉理解 / 文档理解 / 工具调用 / 扩展思考 /
#              提示词缓存 / 上下文窗口 / 错误处理 / 流式响应 / 数据安全 /
#              参数边界 / 联网搜索
#    注意：响应延迟分位(⑦表)的样本全部来自「提示词缓存」用例，
#         不勾选提示词缓存时 ④ 和 ⑦ 两张表都会没有数据。
# =============================================================================
CATEGORIES = _env_list("CATEGORIES", [])

# =============================================================================
# 4. 鉴权方式（二选一）
# =============================================================================
# "anthropic" -> 请求头 x-api-key + anthropic-version
# "bearer"    -> 请求头 Authorization: Bearer <key>
AUTH_MODE = _env("AUTH_MODE", "bearer")

# —— 报告展示用鉴权方式（留空 = 跟 AUTH_MODE 一致）——————————————————
#   同 REPORT_BASE_URL：只影响「测试汇总」的鉴权方式和 curl 样例的请求头，
#   实际请求依然按 AUTH_MODE 组装。取值同样是 "anthropic" / "bearer"。
REPORT_AUTH_MODE = _env("REPORT_AUTH_MODE", "")

# 密钥（两种鉴权方式都用这一个值）
# 这里只是本地手工跑脚本时的占位默认值，**不要把真实 key 写进来**（会进 git 历史）。
# 正常链路由 claude-test-service 通过 CLAUDE_TEST_API_KEY 环境变量注入
# （值来自管理端弹窗里填的管理员 Token，拼上 -{渠道ID}）。
# 本地手工跑：export CLAUDE_TEST_API_KEY=sk-你的token
API_KEY = _env("API_KEY", "sk-REPLACE_ME")

# anthropic-version 头的值。anthropic 模式必带；bearer 模式若非空也会带上。
ANTHROPIC_VERSION = "2023-06-01"

# anthropic-beta 头（逗号分隔多个特性）。留空则不发送该头。
# 例："interleaved-thinking-2025-05-14"
ANTHROPIC_BETA = ""

# =============================================================================
# 5. 多模态输入资源（OSS 链接可留空，留空的用例会自动 SKIP，方便你后续填充）
#
#    base64 图片 / PDF 用例由脚本内置生成，无需你提供文件。
#
#    "小/大" 分界建议以 5MB 为界：
#      - IMAGE_URL_SMALL / PDF_URL_SMALL   —— 常规体积（≤ 5MB），验证正常读取
#      - IMAGE_URL_LARGE / PDF_URL_LARGE   —— 超大体积（> 5MB），验证平台限流/超限报错
#
#    以下所有 URL 都可以是 OSS 直链。
#
#    ★ URL 资源怎么传给模型：见下方 MEDIA_SOURCE_MODES（base64 / url 两种方式轮试）
# =============================================================================
# —— 图片资源 ————————————————————————————————————————————————————————
IMAGE_URL_SMALL = "https://gravitexgrayoss.tos-s3-ap-southeast-1.bytepluses.com/2026/04/17/e69aa86dbf3b435480b50fb674265e55.jpeg"       # ≤ 5MB 单图 OSS 链接（普通视觉理解用例）
IMAGE_URL_LARGE = "http://gravitexgrayoss.tos-s3-accelerate.bytepluses.com.cn/2026/07/10/5c7b67d4632e4a7eb0885ab22585680e.png"       # > 5MB 超大图 OSS 链接（大小超限用例）
IMAGE_URLS_MULTI = ["http://gravitexgrayoss.tos-s3-accelerate.bytepluses.com.cn/2026/07/10/3553d2a815a54d6bb7285ef8f415d897.png",
"https://gravitexgrayoss.tos-s3-ap-southeast-1.bytepluses.com/2026/04/17/e69aa86dbf3b435480b50fb674265e55.jpeg",
"http://gravitexgrayoss.tos-s3-accelerate.bytepluses.com.cn/2026/06/10/6352cd8d17f943f7b4ce4c6c8a3f215d.png"]      # 多图对比用例，至少 2 个 OSS 链接
# 旧变量兼容（若填了新的 SMALL 则优先用新变量；此变量将被视为 SMALL 的别名）
IMAGE_URL_SINGLE = ""

# —— PDF 资源 —————————————————————————————————————————————————————————
PDF_URL_SMALL = "http://gravitexgrayoss.tos-s3-accelerate.bytepluses.com.cn/2026/07/10/cf99a0498fb84d36a3f43293905d704d.pdf"         # ≤ 5MB PDF OSS 链接
PDF_URL_LARGE = "https://kcodehub.oss-cn-chengdu.aliyuncs.com/202602/Java.pdf"         # > 5MB 超大 PDF OSS 链接
# 旧变量兼容
PDF_URL = ""
PDF_FILE_PATH = ""         # 本地 PDF 路径（base64 用例，可选）

# —— URL 资源调用方式（多方式轮试，任一成功即算通过）————————————————————
#   Claude 资源上游能力不统一：
#     - 走 AWS Bedrock 中转的资源只认 base64，且单文件常限制 ≤ 5MB；
#     - 直连 Anthropic 官方的资源同时支持 url source，>5MB 也可能读得动。
#   所以图片 / PDF 的 URL 用例会按下面的顺序依次尝试，任意一种方式成功就判定通过，
#   不再因为某个资源只支持一种传输方式而误判为能力缺失。
#   base64 放前面：它是所有上游都支持的最小公分母，通常一次就成功。
MEDIA_SOURCE_MODES = ("base64", "url")

# =============================================================================
# 6. 上下文隔离安全测试
#    验证同一 API Key 下的两个并发会话上下文不会互相泄露。
# =============================================================================
ENABLE_SESSION_ISOLATION = True

# =============================================================================
# 7. Prompt Cache 命中率（5 分钟 / 1 小时缓存）
#
#    判定方式：先发 1 次"种子轮"把超长系统提示词写进缓存（cache_creation），
#    随后并发发 CACHE_HIT_ROUNDS 次完全相同前缀的请求，
#    统计其中 cache_read_input_tokens > 0 的比例 = 命中率。
#    命中率 ≥ CACHE_HIT_PASS_RATIO 即判定该 TTL 的缓存链路通过。
#
#    HTTP 失败（限流/网络错误）的轮次不计入分母，只在报告里单列，
#    但有效样本少于一半时直接判不通过（样本不足，结论不可信）。
# =============================================================================
CACHE_HIT_ROUNDS = 20                   # 种子轮之后的读取轮数
CACHE_HIT_PASS_RATIO = 0.85             # 命中率达到此比例算通过
CACHE_HIT_MAX_WORKERS = 5               # 读取轮的并发线程数
CACHE_SEED_WAIT_SECONDS = 2             # 种子轮之后等待缓存落地的秒数

# =============================================================================
# 8. 运行与输出
# =============================================================================
# 请求超时（秒）
TIMEOUT_SECONDS = 500

# [模型 × 用例] 的并发线程数。调大能显著缩短出报告的时间，
# 但并发越高越容易撞平台限流(429)导致用例误判，建议 4~10。
MAX_WORKERS = 8

# 同一线程内两个用例之间的间隔（秒），给平台限流窗口留一点余量
REQUEST_INTERVAL_SECONDS = 0.3

# 是否在控制台打印完整 HTTP 交互（请求/响应）
PRINT_HTTP = _env_bool("PRINT_HTTP", True)

# 报告输出文件名（.xlsx）
REPORT_FILENAME = f"claude-测试报告-{PLATFORM_NAME}.xlsx"

# 写进 Excel 的长文本（如超长提示词/curl）截断阈值：超过则省略中间
EXCEL_CELL_MAX_CHARS = 1200

# 模型响应文本截断阈值（"实际结果"列展示模型完整响应时）。超长截断并加提示。
RESPONSE_TEXT_MAX_CHARS = 2000


# ---- Excel 排版 ----
#   行高必须显式写死：openpyxl 不写行高时，Excel 打开会自行 autofit，
#   "实际结果"这种上千字符的单元格会被撑到 409.5 的上限，一行占满一屏没法扫读。
# 「测试明细」数据行的固定行高（磅），90 ≈ 6 行文字；调大能一屏看到更多正文。
#   想看某一行的全文：单独把那行拖高，或点单元格在编辑栏里读。
EXCEL_DETAIL_ROW_HEIGHT = 90
# 「测试汇总」行高上限（磅）。汇总各行按内容估算行高，不超过这个值。
EXCEL_SUMMARY_ROW_MAX_HEIGHT = 90

# =============================================================================
# 兼容 helper：读取"单图 URL"和"PDF URL"时，优先取新变量
# =============================================================================
def get_image_url_small() -> str:
    return IMAGE_URL_SMALL or IMAGE_URL_SINGLE


def get_image_url_large() -> str:
    return IMAGE_URL_LARGE


def get_pdf_url_small() -> str:
    return PDF_URL_SMALL or PDF_URL


def get_pdf_url_large() -> str:
    return PDF_URL_LARGE


# =============================================================================
# 报告展示 helper：报告里写哪个地址/鉴权方式（留空则回落到实际调用值）
# =============================================================================
def report_base_url() -> str:
    return REPORT_BASE_URL or BASE_URL


def report_auth_mode() -> str:
    return REPORT_AUTH_MODE or AUTH_MODE


def report_differs_from_actual() -> bool:
    """展示值和实际调用值是否不一致（用于在控制台和报告里注明）。"""
    return report_base_url() != BASE_URL or report_auth_mode() != AUTH_MODE
