#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
测试夹具（Fixtures）
====================

- 纯标准库生成 PNG / PDF（base64 用例无需你提供文件）
- 4 道复杂推理题（扩展思考用例）
- 超长系统提示词（约 5000 token，缓存用例；取自实测可用的 AWS Lambda 指南）
- 超大上下文文本（最大上下文窗口用例）
"""

from __future__ import annotations

import base64
import struct
import zlib


# =============================================================================
# PNG 生成（纯标准库）
# =============================================================================
def _png_chunk(tag: bytes, data: bytes) -> bytes:
    chunk = tag + data
    return struct.pack(">I", len(data)) + chunk + struct.pack(">I", zlib.crc32(chunk) & 0xFFFFFFFF)


def make_png(width: int, height: int, rgb: tuple[int, int, int] = (220, 40, 40)) -> bytes:
    """生成一张纯色 RGB PNG。"""
    signature = b"\x89PNG\r\n\x1a\n"
    ihdr = struct.pack(">IIBBBBB", width, height, 8, 2, 0, 0, 0)  # 8bit, colortype=2(RGB)
    row = b"\x00" + bytes(rgb) * width  # 每行前缀 filter=0
    raw = row * height
    idat = zlib.compress(raw, 9)
    return (
        signature
        + _png_chunk(b"IHDR", ihdr)
        + _png_chunk(b"IDAT", idat)
        + _png_chunk(b"IEND", b"")
    )


def small_png_base64() -> str:
    """64x64 纯色小图，正常视觉用例。"""
    return base64.b64encode(make_png(64, 64)).decode("ascii")


def oversized_png_base64(target_bytes: int = 6 * 1024 * 1024) -> str:
    """生成 >5MB 的大图，用于触发单图大小超限（预期报错）。"""
    # 纯色压缩率极高，改用大尺寸并让每行像素略有变化以增大体积
    width = height = 1600
    signature = b"\x89PNG\r\n\x1a\n"
    ihdr = struct.pack(">IIBBBBB", width, height, 8, 2, 0, 0, 0)
    rows = bytearray()
    for y in range(height):
        rows.append(0)  # filter
        base = (y * 7) & 0xFF
        rows.extend(bytes(((base + x) & 0xFF for x in range(width * 3))))
    idat = zlib.compress(bytes(rows), 1)
    png = (
        signature
        + _png_chunk(b"IHDR", ihdr)
        + _png_chunk(b"IDAT", idat)
        + _png_chunk(b"IEND", b"")
    )
    return base64.b64encode(png).decode("ascii")


# =============================================================================
# PDF 生成（纯标准库，极简单页）
# =============================================================================
def make_pdf(text: str = "Gravitex Claude test PDF. Secret code: RAINBOW-7788.") -> bytes:
    """构造一个最小可用的单页 PDF，内含一行可被模型读取的文本。"""
    objects: list[bytes] = []
    objects.append(b"<< /Type /Catalog /Pages 2 0 R >>")
    objects.append(b"<< /Type /Pages /Kids [3 0 R] /Count 1 >>")
    objects.append(
        b"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] "
        b"/Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>"
    )
    safe = text.replace("(", r"\(").replace(")", r"\)")
    stream = f"BT /F1 18 Tf 72 700 Td ({safe}) Tj ET".encode("latin-1")
    objects.append(
        b"<< /Length " + str(len(stream)).encode() + b" >>\nstream\n" + stream + b"\nendstream"
    )
    objects.append(b"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>")

    pdf = bytearray(b"%PDF-1.4\n")
    offsets = [0]
    for i, obj in enumerate(objects, start=1):
        offsets.append(len(pdf))
        pdf += f"{i} 0 obj\n".encode() + obj + b"\nendobj\n"

    xref_pos = len(pdf)
    n = len(objects) + 1
    pdf += f"xref\n0 {n}\n".encode()
    pdf += b"0000000000 65535 f \n"
    for off in offsets[1:]:
        pdf += f"{off:010d} 00000 n \n".encode()
    pdf += b"trailer\n"
    pdf += f"<< /Size {n} /Root 1 0 R >>\n".encode()
    pdf += b"startxref\n" + str(xref_pos).encode() + b"\n%%EOF"
    return bytes(pdf)


def small_pdf_base64() -> str:
    return base64.b64encode(make_pdf()).decode("ascii")


PDF_SECRET_CODE = "RAINBOW-7788"  # 内置 PDF 里的验证串


# =============================================================================
# 复杂推理题（扩展思考用例）
# =============================================================================
REASONING_PROBLEMS: dict[str, str] = {
    "cubic": (
        "求所有满足 a^3+b^3+c^3=3abc 且 a+b+c=2025 的整数三元组 (a,b,c)，"
        "并完整证明分类讨论没有遗漏。"
    ),
    "truth": (
        "真假话三个人问题：A、B、C 一人说真话两人说假话。"
        "A 说：B 说谎；B 说：C 说谎；C 说：A 和 B 都说谎。"
        "根据三句话锁定谁真谁假，全分类枚举所有 3 种可能。"
    ),
    "balance": (
        "天平称重：12 个硬币有 1 个次品（不知轻重），只用 3 次天平找出次品并确定偏轻还是偏重；"
        "需要穷尽每次称量的所有分支。"
    ),
    "river": (
        "过河问题：人 + 狼 + 羊 + 菜，小船一次只能带一样（人必须划船）。"
        "狼和羊不能单独在一起，羊和菜不能单独在一起。"
        "全分类枚举可行路径，排除自相矛盾的方案。"
    ),
}


def hard_reasoning_prompt() -> str:
    """把 4 道题拼成一个需要长链推理的复合问题。"""
    parts = ["请依次严格求解以下 4 道题，每题都要完整分类讨论、证明不遗漏："]
    for i, key in enumerate(("cubic", "truth", "balance", "river"), start=1):
        parts.append(f"\n第{i}题：{REASONING_PROBLEMS[key]}")
    return "".join(parts)


# =============================================================================
# 超长系统提示词（用于 Prompt Caching，需 >1024 token）
# =============================================================================
# 缓存用例的长前缀：一份真实的技术文档（AWS Lambda 指南）。
#
# 内容取自 logs/缓存提示词.md —— 那是主人在平台上实测过、不会触发拒答的请求体。
# 历史踩坑：更早的版本是把同一句规范重复 120 遍凑长度，被 Claude Fable 5 的安全
# 分类器判成拒答（stop_reason=refusal）；拒答后每轮仍会上报 cache_creation，
# 等于白付缓存写入费。所以这里改用正常成文的技术文档。
_LAMBDA_GUIDE = """你是一个专业的文档分析助手。以下是一份详细的技术文档：

# AWS Lambda 完整指南

AWS Lambda 是一项无服务器计算服务，让您无需预置或管理服务器即可运行代码。您只需为您消耗的计算时间付费。使用 Lambda，您可以为几乎任何类型的应用程序或后端服务运行代码，而且无需执行任何管理。

## Lambda 的主要特性

1. **无服务器架构**：无需管理服务器基础设施，AWS 会自动处理所有计算资源的配置、扩展和管理。

2. **自动扩展**：Lambda 会自动扩展您的应用程序，从每天几个请求到每秒数千个请求都能处理。

3. **按使用付费**：您只需为代码运行所消耗的计算时间付费。代码不运行时不收费。

4. **内置高可用性**：Lambda 在多个可用区运行您的代码，确保高可用性。

5. **与 AWS 服务集成**：可以轻松与 Amazon S3、DynamoDB、Kinesis、SNS 和 CloudWatch 等服务集成。

## Lambda 函数基础

Lambda 函数是您上传到 AWS Lambda 的代码。每个函数都有相关的配置信息，包括名称、描述、入口点和资源要求。

### 支持的运行时
- Node.js (18.x, 20.x)
- Python (3.9, 3.10, 3.11, 3.12)
- Java (8, 11, 17, 21)
- .NET (6, 8)
- Go (1.x)
- Ruby (3.2, 3.3)
- 自定义运行时

### 函数配置
- **内存**：128 MB 到 10,240 MB，以 1 MB 为增量
- **超时**：最长 15 分钟
- **并发**：默认 1000 个并发执行
- **环境变量**：最多 4 KB
- **部署包大小**：压缩后 50 MB，解压后 250 MB

## Lambda 定价

### 请求定价
- 前 100 万次请求免费
- 之后每 100 万次请求 $0.20

### 持续时间定价
按 GB-秒计费：
- x86: $0.0000166667 per GB-秒
- ARM: $0.0000133334 per GB-秒

### 临时存储
- 512 MB 免费
- 超出部分 $0.0000000309 per GB-秒

## 最佳实践

1. **优化函数内存**：选择合适的内存配置以平衡性能和成本
2. **使用环境变量**：存储配置数据和密钥
3. **启用 X-Ray**：用于分布式跟踪和调试
4. **使用层**：共享代码和依赖项
5. **设置预留并发**：确保关键函数的性能
6. **冷启动优化**：保持部署包小，使用预配置并发
7. **错误处理**：实现重试逻辑和死信队列
8. **监控和日志**：使用 CloudWatch 进行监控

## 常见用例

### 1. 数据处理
- 实时文件处理
- 实时流处理
- 数据验证和清理
- ETL 作业

### 2. Web 应用
- Web API 后端
- 移动后端
- 单页应用 (SPA) 后端

### 3. IoT 后端
- 设备消息处理
- 设备状态更新
- 警报和通知

### 4. 自动化任务
- 定时任务
- 备份自动化
- 报告生成

## 安全性

### IAM 角色
每个 Lambda 函数都需要一个 IAM 执行角色，用于访问 AWS 服务。

### VPC 集成
Lambda 可以在 VPC 内运行，访问私有资源。

### 加密
- 环境变量使用 AWS KMS 加密
- 支持客户管理的密钥

## 性能优化

### 预配置并发
保持函数始终处于热状态，消除冷启动。

### 层
将依赖项打包到层中，减小部署包大小。

### 代码优化
- 最小化部署包
- 减少函数初始化时间
- 复用连接和客户端

以上内容涵盖了 AWS Lambda 的核心概念和最佳实践。"""

# 缓存最小前缀按模型不同是 512~4096 token（最严的是 Opus 4.6 / Haiku 4.5）。
# 单份文档约 1680 token 不够，按下面这个目标长度向上取整重复几份。
# 低于 4096 时缓存根本不会形成，用例会测出一个假的"零命中"。
_CACHE_PREFIX_MIN_CHARS = 5000


def long_system_prompt() -> str:
    """构造缓存用例的长前缀：内容固定，两次请求逐字一致才能命中缓存。"""
    copies = -(-_CACHE_PREFIX_MIN_CHARS // len(_LAMBDA_GUIDE))
    return "\n\n".join([_LAMBDA_GUIDE] * copies)


# =============================================================================
# 超大上下文文本（最大上下文窗口用例）
# =============================================================================
def large_context_text(target_chars: int = 1_997_730) -> str:
    unit = "这是一段用于逼近模型最大上下文窗口的测试文本。" * 20
    repeats = max(1, target_chars // len(unit) + 1)
    return (unit * repeats)[:target_chars]


# =============================================================================
# 工具定义（Anthropic 原生格式）
# =============================================================================
def weather_tool() -> dict:
    return {
        "name": "get_current_weather",
        "description": "查询指定城市的当前天气。",
        "input_schema": {
            "type": "object",
            "properties": {
                "location": {"type": "string", "description": "城市名，例如：北京"},
                "unit": {"type": "string", "enum": ["celsius", "fahrenheit"]},
            },
            "required": ["location"],
        },
    }


def time_tool() -> dict:
    return {
        "name": "get_current_time",
        "description": "获取当前时间。",
        "input_schema": {"type": "object", "properties": {}},
    }
