#!/usr/bin/env python3
"""从错误账单流式生成离线 HTML 网页；不调用 AI，不生成 Markdown 报告。"""
from __future__ import annotations

import argparse
import html
import json
import os
import sys
import tempfile
from pathlib import Path
from xml.etree.ElementTree import ParseError
from zipfile import BadZipFile

from diagnostics.engine import archive_dir, archive_tag, generate_data, summary

ROOT = Path(__file__).resolve().parent


def safe_json(data):
    return json.dumps(data,ensure_ascii=False,separators=(",",":"),allow_nan=False).replace("<","\\u003c").replace(">","\\u003e").replace("\u2028","\\u2028").replace("\u2029","\\u2029")


def atomic_write(path, text):
    path = Path(path)
    path.parent.mkdir(parents=True,exist_ok=True)
    tmp = None
    try:
        with tempfile.NamedTemporaryFile("w",encoding="utf-8",dir=path.parent,prefix="."+path.name+".",suffix=".tmp",delete=False) as stream:
            tmp = Path(stream.name)
            stream.write(text)
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(tmp,path)
    finally:
        if tmp and tmp.exists():
            tmp.unlink()


def write_html(data, output):
    facts = summary(data)
    period = data["period"]
    folder = Path(output).resolve()/archive_dir(period)
    html_path = folder/f"error_diagnosis_{archive_tag(period)}.html"
    template = (ROOT/"diagnostics/dashboard.html").read_text(encoding="utf-8")
    rendered = template.replace("__PERIOD__",html.escape(period)).replace("__DATA__",safe_json(data)).replace("__SUMMARY__",safe_json(facts))
    atomic_write(html_path,rendered)
    return html_path


def positive(value):
    number = int(value)
    if number < 1:
        raise argparse.ArgumentTypeError("必须为正整数")
    return number


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("inputs",nargs="+",type=Path,help="错误明细 XLSX/CSV，可传多份组成周报")
    parser.add_argument("--output",type=Path,default=ROOT/"output",help="归档根目录")
    parser.add_argument("--sheet",help="指定工作表；默认识别")
    parser.add_argument("--encoding",help="CSV 编码；默认识别 UTF-8/UTF-16/GB18030")
    parser.add_argument("--column",action="append",default=[],metavar="FIELD=HEADER",help="覆盖列映射，如 --column error=失败描述")
    parser.add_argument("--date",help="只为缺失/非法时间指定日期 YYYY-MM-DD；不伪造时刻，不修改有效日期")
    parser.add_argument("--max-buckets",type=positive,default=250000)
    parser.add_argument("--max-samples",type=positive,default=4000)
    parser.add_argument("--samples-per-bucket",type=positive,default=2)
    parser.add_argument("--evidence-chars",type=positive,default=1600)
    parser.add_argument("--quiet",action="store_true")
    args=parser.parse_args()
    try:
        columns={}
        for item in args.column:
            if "=" not in item:
                raise ValueError("--column 格式应为 FIELD=HEADER")
            field,name=item.split("=",1)
            columns[field.strip()]=name.strip()
        data=generate_data(args.inputs,sheet=args.sheet,encoding=args.encoding,columns=columns,
                           fallback_date=args.date,progress=not args.quiet,max_buckets=args.max_buckets,
                           max_samples=args.max_samples,samples_per_bucket=args.samples_per_bucket,evidence_chars=args.evidence_chars)
        path=write_html(data,args.output)
        print(json.dumps(dict(html=str(path),errors=data["total"],rows=data["rows_read"],
                             buckets=len(data["records"]),seconds=data["build_seconds"],quality=data["quality"]),ensure_ascii=False,indent=2))
        return 0
    except (ValueError,OSError,KeyError,ParseError,BadZipFile,EOFError) as exc:
        print("生成失败："+str(exc),file=sys.stderr)
        return 2


if __name__=="__main__":
    raise SystemExit(main())
