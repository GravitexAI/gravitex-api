#!/usr/bin/env python3
"""一键测试入口：生成诊断网页；若 .env 已配置则自动调用 AI 生成报告。

命令行用法（周报可传多份文件）：
    python run.py "账单.xlsx"
    python run.py "周一.xlsx" "周二.xlsx" --skip-ai

不带参数直接运行（或双击 quick_start.bat）会转入交互式输入。
"""
from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path

from ai_analysis import analyze, config_value, read_env
from diagnostics.engine import generate_data
from error_diagnosis import write_html

ROOT = Path(__file__).resolve().parent


def ai_configured(env_path):
    config = read_env(env_path)
    keys = (
        config_value(config, "BASE_URL", "BASEURL", "OPENAI_BASE_URL"),
        config_value(config, "MODEL", "OPENAI_MODEL"),
        config_value(config, "API_KEY", "APIKEY", "OPENAI_API_KEY"),
    )
    return all(keys)


def prompt_paths():
    print("请输入账单文件路径（.xlsx/.csv/.tsv 等）。")
    print("- 周报可传多份文件，用英文逗号分隔")
    print("- 可直接把文件拖进本窗口，再按回车确认")
    print()
    while True:
        try:
            raw = input("账单路径: ").strip()
        except EOFError:
            raise ValueError("未读取到输入，已退出")
        if not raw:
            print("路径不能为空，请重新输入。")
            continue
        parts = [p.strip().strip('"').strip("'").lstrip("﻿​") for p in raw.split(",")]
        parts = [p for p in parts if p]
        if not parts:
            print("路径不能为空，请重新输入。")
            continue
        paths = [Path(p) for p in parts]
        missing = [str(p) for p in paths if not p.is_file()]
        if missing:
            print("以下路径不存在或不是文件，请检查后重新输入：" + "；".join(missing))
            continue
        return paths


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("inputs", nargs="*", type=Path, help="错误明细 XLSX/CSV；不填则进入交互式输入")
    parser.add_argument("--output", type=Path, default=ROOT / "output", help="归档根目录，默认本项目 output/")
    parser.add_argument("--env", type=Path, default=ROOT / ".env")
    parser.add_argument("--sheet", help="指定工作表；默认识别")
    parser.add_argument("--encoding", help="CSV 编码；默认识别")
    parser.add_argument("--column", action="append", default=[], metavar="FIELD=HEADER")
    parser.add_argument("--date", help="只为缺失/非法时间指定日期 YYYY-MM-DD")
    parser.add_argument("--skip-ai", action="store_true", help="只生成网页，不调用 AI")
    args = parser.parse_args()
    try:
        inputs = args.inputs or prompt_paths()
        columns = {}
        for item in args.column:
            if "=" not in item:
                raise ValueError("--column 格式应为 FIELD=HEADER")
            field, name = item.split("=", 1)
            columns[field.strip()] = name.strip()
        print("正在生成诊断网页……")
        data = generate_data(inputs, sheet=args.sheet, encoding=args.encoding, columns=columns,
                              fallback_date=args.date, progress=True)
        html_path = write_html(data, args.output)
        print(json.dumps(dict(html=str(html_path), errors=data["total"], rows=data["rows_read"],
                               buckets=len(data["records"])), ensure_ascii=False, indent=2))
        if args.skip_ai:
            print("已跳过 AI 分析（--skip-ai）。")
            return 0
        if not ai_configured(args.env):
            print("提示：.env 未填写 BASE_URL/MODEL/API_KEY，跳过 AI 分析；网页已生成，可稍后单独运行 ai_analysis.py。")
            return 0
        print("检测到 .env 已配置，正在调用 AI 生成报告……")
        report_path = analyze(html_path, args.env)
        print("AI 分析报告已生成：" + str(report_path))
        return 0
    except (ValueError, OSError) as exc:
        print("执行失败：" + str(exc), file=sys.stderr)
        return 2


if __name__ == "__main__":
    raise SystemExit(main())
