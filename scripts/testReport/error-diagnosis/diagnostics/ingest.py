"""Streaming XLSX/CSV reader. XLSX shared strings are spooled to temporary disk."""
from __future__ import annotations

import csv
import codecs
import io
import posixpath
import re
import tempfile
from array import array
from collections import deque
from contextlib import contextmanager
from functools import lru_cache
from pathlib import Path
from xml.etree import ElementTree as ET
from zipfile import ZipFile

NS = "{http://schemas.openxmlformats.org/spreadsheetml/2006/main}"
REL = "{http://schemas.openxmlformats.org/officeDocument/2006/relationships}id"

ALIASES = {
    "time": ["时间", "日期时间", "创建时间", "请求时间", "发生时间", "日期", "timestamp", "created_at", "createdat", "datetime", "time", "date"],
    "user": ["用户", "用户名", "用户名称", "用户ID", "账号", "user", "username", "user_name", "user_id", "userid"],
    "model": ["服务/模型", "模型", "模型名称", "服务模型", "model", "model_name", "modelname"],
    "group": ["资源分组", "分组", "用户分组", "group", "resource_group", "group_name"],
    "channel": ["渠道名称", "渠道", "渠道ID", "通道", "channel", "channel_name", "channel_id", "channelname"],
    "error": ["计费过程", "错误信息", "报错信息", "错误内容", "错误详情", "失败原因", "error", "error_message", "message", "error_detail", "reason"],
    "status": ["状态码", "HTTP状态码", "http_status", "status_code", "httpstatus", "status"],
    "kind": ["类型", "日志类型", "记录类型", "type", "record_type", "log_type", "result"],
    "count": ["错误数", "错误次数", "记录数", "error_count", "occurrences"],
}


def header_key(value):
    return re.sub(r"[\s_/\\()（）-]", "", str(value or "")).casefold()


LOOKUP = {header_key(alias): field for field, aliases in ALIASES.items() for alias in aliases}


def map_headers(row, overrides=None):
    found = {}
    duplicates = []
    normalized = [header_key(c) for c in row]
    for i, value in enumerate(row):
        field = LOOKUP.get(header_key(value))
        if field:
            if field in found:
                duplicates.append(field)
            else:
                found[field] = i
    for field, name in (overrides or {}).items():
        if field not in ALIASES:
            raise ValueError(f"不支持的映射字段 {field}；可用：{', '.join(ALIASES)}")
        matches = [i for i, value in enumerate(normalized) if value == header_key(name)]
        if len(matches) == 1:
            found[field] = matches[0]
            duplicates = [d for d in duplicates if d != field]
        else:
            # It may be a title row rather than the header; resolved after detection.
            found.pop(field, None)
    return found, duplicates


def detect_header(rows, overrides=None):
    """Retain at most the first 30 rows; never materialize the worksheet."""
    buffer = []
    for _ in range(30):
        try:
            buffer.append(next(rows))
        except StopIteration:
            break
    candidates = []
    for n, values in buffer:
        mapping, dup = map_headers(values, overrides)
        if any(k in mapping for k in ("error", "status", "kind")):
            score = len(mapping) + (3 if "error" in mapping else 0)
            candidates.append((score, -n, n, values, mapping, dup))
    if not candidates:
        raise ValueError("前 30 行未识别到错误、状态或类型字段；请用 --column error=实际列名 指定")
    _, _, n, header, mapping, duplicates = max(candidates, key=lambda c: (c[0], c[1]))
    for field in (overrides or {}):
        if field not in mapping:
            raise ValueError(f"指定字段 {field}={overrides[field]} 在表头中不存在或重名")
    if duplicates:
        raise ValueError(f"表头别名歧义：{', '.join(sorted(set(duplicates)))}；请用 --column 明确选择列")

    def remaining():
        for rownum, row in buffer:
            if rownum > n:
                yield rownum, row
        yield from rows
    return header, mapping, remaining(), n


class SharedStrings:
    def __init__(self, archive):
        self.offsets = array("Q", [0])
        self.file = tempfile.TemporaryFile(prefix="error-strings-")
        self.get = lru_cache(maxsize=2048)(self._get)
        if "xl/sharedStrings.xml" not in archive.namelist():
            return
        with archive.open("xl/sharedStrings.xml") as stream:
            iterator = ET.iterparse(stream, events=("start", "end"))
            _, root = next(iterator)
            for event, element in iterator:
                if event == "end" and element.tag == NS + "si":
                    text = "".join(t.text or "" for t in element.iter(NS + "t"))
                    self.file.write(text.encode("utf-8"))
                    self.offsets.append(self.file.tell())
                    root.clear()

    def _get(self, index):
        if index < 0 or index + 1 >= len(self.offsets):
            raise ValueError(f"XLSX shared-string 索引无效：{index}")
        start, end = self.offsets[index], self.offsets[index + 1]
        self.file.seek(start)
        return self.file.read(end - start).decode("utf-8")

    def close(self):
        self.get.cache_clear()
        self.file.close()


def column_index(address):
    letters = re.match(r"[A-Z]+", address)
    if not letters:
        raise ValueError(f"非法 XLSX 单元格地址：{address}")
    n = 0
    for c in letters[0]:
        n = n * 26 + ord(c) - 64
    return n - 1


class Xlsx:
    def __init__(self, path):
        self.archive = ZipFile(path)
        try:
            workbook = ET.fromstring(self.archive.read("xl/workbook.xml"))
            props = workbook.find(NS + "workbookPr")
            self.date1904 = props is not None and props.get("date1904") in ("1", "true")
            relationships = ET.fromstring(self.archive.read("xl/_rels/workbook.xml.rels"))
            targets = {r.get("Id"): r.get("Target") for r in relationships if r.get("TargetMode") != "External"}
            self.sheets = {}
            for sheet in workbook.find(NS + "sheets"):
                target = targets.get(sheet.get(REL), "")
                member = target.lstrip("/") if target.startswith("/") else posixpath.normpath("xl/" + target)
                if member not in self.archive.namelist():
                    raise ValueError(f"工作表路径缺失：{sheet.get('name')}")
                self.sheets[sheet.get("name")] = member
            self.strings = SharedStrings(self.archive)
        except Exception:
            self.archive.close()
            raise

    def rows(self, sheet):
        with self.archive.open(self.sheets[sheet]) as stream:
            iterator = ET.iterparse(stream, events=("start", "end"))
            sheet_data = None
            last_row = 0
            for event, node in iterator:
                if event == "start" and node.tag == NS + "sheetData":
                    sheet_data = node
                if event != "end" or node.tag != NS + "row":
                    continue
                rownum = int(node.get("r", last_row + 1))
                last_row = rownum
                row = []
                for cell in node:
                    i = column_index(cell.get("r", "A1"))
                    if i > 16383:
                        raise ValueError("XLSX 列索引超出范围")
                    if i >= len(row):
                        row.extend([""] * (i + 1 - len(row)))
                    typ = cell.get("t", "")
                    value = cell.findtext(NS + "v", "")
                    if typ == "s":
                        value = self.strings.get(int(value)) if value else ""
                    elif typ == "inlineStr":
                        value = "".join(t.text or "" for t in cell.iter(NS + "t"))
                    # Numeric dates are interpreted only in the mapped timestamp field.
                    row[i] = value
                yield rownum, row
                node.clear()
                if sheet_data is not None:
                    sheet_data.clear()

    def close(self):
        self.strings.close()
        self.archive.close()


@contextmanager
def open_table(path, sheet=None, encoding=None, overrides=None):
    path = Path(path)
    if path.suffix.lower() in (".xlsx", ".xlsm"):
        workbook = Xlsx(path)
        try:
            if sheet and sheet not in workbook.sheets:
                raise ValueError(f"工作表 {sheet} 不存在；可用：{', '.join(workbook.sheets)}")
            chosen = sheet
            if not chosen:
                candidates = []
                for name in workbook.sheets:
                    iterator = workbook.rows(name)
                    try:
                        header, mapping, _, _ = detect_header(iterator, overrides)
                        priority = int(any(s in name.lower() for s in ("错误", "明细", "error", "detail")))
                        candidates.append((len(mapping), priority, name))
                    except ValueError:
                        pass
                    finally:
                        iterator.close()
                if not candidates:
                    raise ValueError(f"{path.name} 未找到可识别的错误明细表；可用 --sheet、--column")
                candidates.sort(reverse=True)
                if len(candidates) > 1 and candidates[0][:2] == candidates[1][:2]:
                    raise ValueError("存在多个同等匹配的工作表，请通过 --sheet 指定，避免误读或重复统计")
                chosen = candidates[0][2]
            iterator = workbook.rows(chosen)
            header, mapping, rows, header_row = detect_header(iterator, overrides)
            yield rows, dict(sheet=chosen, headers=header, mapping=mapping, header_row=header_row, date1904=workbook.date1904)
        finally:
            workbook.close()
    elif path.suffix.lower() in (".csv", ".tsv"):
        if not encoding:
            with path.open("rb") as stream:
                sample_bytes = stream.read(65536)
            if sample_bytes.startswith((b"\xff\xfe", b"\xfe\xff")):
                encoding = "utf-16"
            else:
                try:
                    codecs.getincrementaldecoder("utf-8-sig")().decode(sample_bytes,final=False)
                    encoding = "utf-8-sig"
                except UnicodeDecodeError:
                    encoding = "gb18030"
        with path.open("r", encoding=encoding, newline="") as stream:
            sample = stream.read(8192)
            stream.seek(0)
            try:
                dialect = csv.Sniffer().sniff(sample, delimiters=",\t;")
            except csv.Error:
                dialect = csv.excel_tab if path.suffix.lower() == ".tsv" else csv.excel
            reader = csv.reader(stream, dialect)
            header, mapping, rows, header_row = detect_header(iter(enumerate(reader, 1)), overrides)
            yield rows, dict(sheet="CSV", headers=header, mapping=mapping, header_row=header_row, date1904=False)
    else:
        raise ValueError("支持 .xlsx / .xlsm / .csv / .tsv；旧版 .xls 请另存为 .xlsx")
