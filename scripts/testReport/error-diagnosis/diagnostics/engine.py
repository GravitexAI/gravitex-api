"""Exact dimension/time counters with bounded evidence; memory scales with buckets, not raw rows."""
from __future__ import annotations

import hashlib
import json
import math
import re
import sys
import time
from collections import Counter
from datetime import datetime, timedelta, timezone
from decimal import Decimal, InvalidOperation
from functools import lru_cache
from pathlib import Path

from .ingest import ALIASES, open_table
from .rules import RULE_VERSION, classify

TZ = timezone(timedelta(hours=8))
MISSING = "未提供"
DIMENSIONS = ["用户", "模型", "资源分组", "渠道", "报错类别"]
UNKNOWN_PERIOD = "未提供日期"


def archive_dir(period):
    """归档目录名。日期区间直接用；日期未知时用固定英文目录，避免中文路径。"""
    return "unknown-date" if period == UNKNOWN_PERIOD else period


def archive_tag(period):
    """产物文件名后缀：20260914 / 20260914_20260920 / unknown_date。"""
    return "unknown_date" if period == UNKNOWN_PERIOD else period.replace("-", "")


def redact(text):
    text = re.sub(r"^错误信息[：:]\s*", "", str(text).strip())
    text = re.sub(r"https?://[^\s<>\"'）]+", "[URL]", text, flags=re.I)
    text = re.sub(r"((?:request[\s_-]*id|trace[\s_-]*id|请求ID)[\"']?\s*[:：=]?\s*[\"']?)[A-Za-z0-9_-]{8,}", r"\1[ID]", text, flags=re.I)
    text = re.sub(r"\b(?:toolu_[A-Za-z0-9_-]+|rs_[A-Za-z0-9_-]{12,}|sk-[A-Za-z0-9_-]+)\b", "[ID]", text)
    text = re.sub(r'(?i)((?:authorization|api[_-]?key|access[_-]?token|secret)["\']?\s*[:=]\s*["\']?(?:bearer\s+)?)[^\s,}"\']+', r"\1[REDACTED]", text)
    text = re.sub(r"\b(?:\d{1,3}\.){3}\d{1,3}\b", "[IP]", text)
    text = re.sub(r"\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}\b", "[EMAIL]", text)
    text = re.sub(r"\[\d+\]", "[N]", text)
    return text or "未提供错误文本"


@lru_cache(maxsize=4096)
def parse_time(value, date1904=False):
    if not value:
        return None
    value = str(value).strip()
    try:
        if re.fullmatch(r"(?:19|20)\d{6}", value):
            return datetime.strptime(value,"%Y%m%d").replace(tzinfo=TZ)
        if re.fullmatch(r"\d{10}(?:\.\d+)?|\d{13}", value):
            epoch = float(value)
            if epoch > 1e12:
                epoch /= 1000
            return datetime.fromtimestamp(epoch, TZ)
        if re.fullmatch(r"\d+(?:\.\d+)?", value):
            serial = float(value)
            if not 1 <= serial < 100000:
                return None
            base = datetime(1904, 1, 1, tzinfo=TZ) if date1904 else datetime(1899, 12, 30, tzinfo=TZ)
            if not date1904 and serial < 60:
                serial += 1  # Excel's fictitious 1900-02-29.
            return base + timedelta(days=serial)
        normalized = value.replace("/", "-").replace("年", "-").replace("月", "-").replace("日", "").replace("Z", "+00:00")
        if re.fullmatch(r"\d{8}", normalized):
            dt = datetime.strptime(normalized, "%Y%m%d")
        else:
            dt = datetime.fromisoformat(normalized)
        return dt.replace(tzinfo=TZ) if not dt.tzinfo else dt.astimezone(TZ)
    except (ValueError, OverflowError, OSError):
        return None


def is_success(kind, status):
    kind = str(kind).strip().casefold()
    status = str(status).strip().casefold()
    if kind in ("消费", "成功", "充值", "退款", "success", "succeeded", "consumption", "consume"):
        return True
    if kind in ("错误", "失败", "error", "failed", "failure"):
        return False
    return status in ("success", "成功", "ok", "succeeded") or bool(re.fullmatch(r"[23]\d\d(?:\.0)?", status))


def count_value(value):
    if not str(value).strip():
        return 1
    try:
        n = Decimal(str(value).replace(",", ""))
        if not n.is_finite() or n < 0 or n != n.to_integral_value() or n > 2**53 - 1:
            raise ValueError
        return int(n)
    except (InvalidOperation, ValueError):
        raise ValueError(f"错误次数不是非负整数：{str(value)[:40]!r}")


class Aggregator:
    def __init__(self, *, max_buckets=250000, max_samples=4000, samples_per_bucket=2, evidence_chars=1600):
        self.limits = dict(max_buckets=max_buckets, max_samples=max_samples, samples_per_bucket=samples_per_bucket, evidence_chars=evidence_chars)
        self.labels = [[] for _ in DIMENSIONS]
        self.ids = [{} for _ in DIMENSIONS]
        self.buckets = {}
        self.sample_ids = {}
        self.samples = []
        self.total = 0
        self.quality = Counter()
        self.coverage = Counter()
        self.days = set()
        self.observed_days = set()
        self.sources = []
        self.rows_read = 0
        self.started = time.perf_counter()

    def intern(self, dimension, value):
        value = str(value).strip() if value is not None else ""
        value = value or MISSING
        mapping = self.ids[dimension]
        if value not in mapping:
            if len(value) > 512:
                raise ValueError(f"{DIMENSIONS[dimension]}值超过 512 字符，疑似字段映射错误")
            mapping[value] = len(self.labels[dimension])
            self.labels[dimension].append(value)
        return mapping[value]

    def add(self, fields, source_id, rownum, date1904=False, fallback_date=None):
        self.rows_read += 1
        fields=dict(fields)
        for field in ("time","user","model","group","channel","error"):
            if str(fields.get(field,"")).strip().casefold() in ("-","--","n/a","null","none","nan"):
                fields[field]=""
        dt = parse_time(fields.get("time", ""), date1904)
        if dt:
            self.observed_days.add(dt.date().isoformat())
        if is_success(fields.get("kind", ""), fields.get("status", "")):
            self.quality["非错误记录已排除"] += 1
            return
        try:
            count = count_value(fields.get("count", ""))
        except ValueError:
            self.quality["错误次数无效的行已排除"] += 1
            return
        if not count:
            self.quality["零次数记录已排除"] += 1
            return
        if "count" in fields and not fields["count"].strip():
            self.quality["次数空白按单条记录计数"] += 1
        if self.total + count > 2**53 - 1:
            raise ValueError("错误总数超过网页可精确表示的整数范围")
        text = str(fields.get("error") or "").strip()
        if not text:
            self.quality["错误文本缺失的行"] += 1
        # XLSX cells cap at 32767; cap unusually long CSV log entries explicitly.
        if len(text) > 32768:
            text = text[:32768]
            self.quality["过长错误文本被截取的行"] += 1
        status = fields.get("status", "")
        normalized = redact(("status_code=" + status + ", " if status else "") + text)
        category = classify(normalized)
        if not dt and fallback_date:
            # Explicit fallback supplies a date, never invents a precise event time.
            day = fallback_date
            hour, epoch = -1, 0
            self.quality["使用指定日期但时刻未知的错误数"] += count
        elif dt:
            day, hour, epoch = dt.date().isoformat(), dt.hour, int(dt.timestamp())
        else:
            day, hour, epoch = "", -1, 0
            self.quality["日期或时间未知的错误数"] += count
        if day:
            self.days.add(day)
        dims = []
        for i, field in enumerate(["user", "model", "group", "channel"]):
            value = fields.get(field, "")
            if not value or not value.strip():
                self.coverage[DIMENSIONS[i]] += count
            dims.append(self.intern(i, value))
        dims.append(self.intern(4, category))
        key = (*dims, day, hour)
        bucket = self.buckets.get(key)
        if bucket is None:
            if len(self.buckets) >= self.limits["max_buckets"]:
                raise ValueError(f"聚合桶超过 {self.limits['max_buckets']:,}；为防止内存/网页过大已停止，无数据被静默丢弃。请按更小时间范围拆分，或提高 --max-buckets。")
            bucket = self.buckets[key] = [0, epoch, epoch, []]
        bucket[0] += count
        if epoch:
            bucket[1] = min(bucket[1], epoch) if bucket[1] else epoch
            bucket[2] = max(bucket[2], epoch)
        self.total += count
        digest = hashlib.blake2b(normalized.encode("utf-8"), digest_size=16).digest()
        sid = self.sample_ids.get(digest)
        if sid is None and len(self.samples) < self.limits["max_samples"]:
            sid = len(self.samples)
            self.sample_ids[digest] = sid
            chars = self.limits["evidence_chars"]
            self.samples.append(dict(text=normalized[:chars], truncated=len(normalized) > chars, source=source_id, row=rownum))
        if sid is not None:
            saved = next((item for item in bucket[3] if item[0] == sid), None)
            if saved is not None:
                saved[1] += count
            elif len(bucket[3]) < self.limits["samples_per_bucket"]:
                # Source coordinates belong to this exact dimension/time bucket.
                bucket[3].append([sid, count, source_id, rownum])

    def finish(self):
        rows = [[*key, *values] for key, values in self.buckets.items()]
        sampled = sum(v[1] for row in rows for v in row[10])
        if sum(row[7] for row in rows) != self.total:
            raise AssertionError("聚合总数不一致")
        self.quality["未保存证据样本的错误数"] = self.total - sampled
        days = sorted(self.days or self.observed_days)
        period = (days[0] if days[0] == days[-1] else days[0] + "_" + days[-1]) if days else UNKNOWN_PERIOD
        return dict(version=2, rule_version=RULE_VERSION, period=period, start=days[0] if days else "", end=days[-1] if days else "",
                    timezone="UTC+8", total=self.total, rows_read=self.rows_read, labels=self.labels,
                    records=rows, samples=self.samples, sources=self.sources, quality=dict(self.quality),
                    missing=dict(self.coverage), limits=self.limits, build_seconds=round(time.perf_counter()-self.started,3))


def generate_data(inputs, *, sheet=None, encoding=None, columns=None, fallback_date=None, progress=True, **limits):
    if fallback_date:
        datetime.strptime(fallback_date, "%Y-%m-%d")
    paths = [Path(p).resolve() for p in inputs]
    if len(paths) != len(set(paths)):
        raise ValueError("输入文件路径重复，已停止以避免重复计数")
    agg = Aggregator(**limits)
    for path in paths:
        stat = path.stat()
        with open_table(path, sheet, encoding, columns) as (rows, info):
            sid = len(agg.sources)
            agg.sources.append(dict(name=path.name, sheet=info["sheet"], header_row=info["header_row"],
                                    columns={k: info["headers"][i] for k,i in info["mapping"].items()},
                                    absent=[f for f in ALIASES if f not in info["mapping"]]))
            before = agg.rows_read
            for rownum, row in rows:
                if not any(str(v).strip() for v in row):
                    continue
                fields = {field: str(row[i]) if i < len(row) and row[i] is not None else "" for field,i in info["mapping"].items()}
                agg.add(fields,sid,rownum,info["date1904"],fallback_date)
                if progress and agg.rows_read % 100000 == 0:
                    print(f"已读取 {agg.rows_read:,} 行，错误 {agg.total:,} 条，聚合桶 {len(agg.buckets):,}", file=sys.stderr)
            agg.sources[-1]["rows"] = agg.rows_read - before
        after = path.stat()
        if stat.st_size != after.st_size or stat.st_mtime_ns != after.st_mtime_ns:
            raise ValueError(f"处理期间源文件发生变化：{path.name}")
    if not agg.rows_read:
        agg.quality["空数据文件"] += 1
    return agg.finish()


def top_counts(data, dimension, limit=20):
    counts = Counter()
    for r in data["records"]:
        key = (r[3],r[1]) if dimension == "pair" else r[dimension]
        counts[key] += r[7]
    return [dict(label=(data["labels"][3][key[0]]+" / "+data["labels"][1][key[1]]) if dimension=="pair" else data["labels"][dimension][key],count=n)
            for key,n in counts.most_common(limit)]


def summary(data):
    daily = Counter()
    category_samples = {}
    for r in data["records"]:
        daily[r[5] or UNKNOWN_PERIOD] += r[7]
        category = data["labels"][4][r[4]]
        targets = category_samples.setdefault(category,{})
        for sid,n,source,row in r[10]:
            if sid in targets:
                targets[sid]["count"] += n
            elif len(targets) < 3:
                targets[sid] = dict(id=f"E{sid+1}",text=data["samples"][sid]["text"][:900],count=n,source=source,row=row)
    return dict(period=data["period"],timezone=data["timezone"],total=data["total"],rows_read=data["rows_read"],
                category_totals=top_counts(data,4,100),users=top_counts(data,0),pairs=top_counts(data,"pair"),
                daily=dict(sorted(daily.items())),quality=data["quality"],missing=data["missing"],
                sources=data["sources"],evidence={c:list(v.values()) for c,v in category_samples.items()},
                rule_version=data["rule_version"],sample_note="证据为有上限的保留样本，样本次数仅为保留桶内计数；所有总量均完整统计，未去重。",
                bucket_count=len(data["records"]),sample_count=len(data["samples"]),build_seconds=data["build_seconds"])


def factual_report(s):
    def table(headers, rows):
        escape = lambda v: str(v).replace("|", "\\|").replace("\n"," ").replace("<","&lt;").replace(">","&gt;")
        return "\n".join(["| "+" | ".join(headers)+" |","| "+" | ".join(["---"]*len(headers))+" |"]+
                         ["| "+" | ".join(escape(v) for v in r)+" |" for r in rows])
    total = s["total"]
    ranked = lambda items: [[v["label"],f'{v["count"]:,}',f'{v["count"]/total*100:.2f}%' if total else "—"] for v in items[:5]]
    lines = [f"# {s['period']} 错误诊断报告", "", "本报告由本地脚本按规则生成，未调用 AI。统计口径为错误记录量，不是请求失败率。", "",
             f"- 错误记录：**{total:,}**；读取行数：**{s['rows_read']:,}**。",
             f"- 时间：**{s['period']}**，UTC+8；分类规则版本：{s['rule_version']}。",
             "", "## 数据对比", "", "### 报错类别 TOP5", "", table(["类别","错误数","占全部错误"],ranked(s["category_totals"])),
             "", "### 实际渠道 / 模型 TOP5", "",table(["渠道 / 模型","错误数","占全部错误"],ranked(s["pairs"])),
             "", "### 用户 TOP5", "",table(["用户","错误数","占全部错误"],ranked(s["users"])),
             "", "## 数据质量与限制", ""]
    lines += [f"- {k}：{v:,}。" for k,v in s["quality"].items() if v]
    lines += [f"- {k}缺失：{v:,} 条，网页显示“未提供”。" for k,v in s["missing"].items() if v]
    lines += ["- 资源分组不等于实际渠道。错误文本分类不是已确认根因。",
              "- 重复记录原样计数，未计算去重请求数；多个文件需由使用者确保范围不重叠。",
              "- 证据样本有数量和长度上限；样本不是全部原始日志，源行可回查。计数与五维/时间统计不采样。",
              "- 缺少总请求分母，不能计算失败率；失败请求样本不代表总体响应性能。", "",
              "## 来源", ""]
    lines += [f"- {v['name']} / {v['sheet']}：{v['rows']:,} 行；识别字段：{', '.join(v['columns'])}。" for v in s["sources"]]
    lines += ["", "## AI 建议", "", "未生成。填写 .env 后单独运行 ai_analysis.py，可在确定性事实报告后生成待验证的分析与优化建议。", ""]
    return "\n".join(lines)
