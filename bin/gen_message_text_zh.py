#!/usr/bin/env python3
"""生成 common/message_text.zh-CN.json（alpha：logs.content 落中文用）。

扫描非测试 Go 代码里 common.NewMessage("...") 的字面量 key，从前端译文
web/src/i18n/locales/zh.json 取对应中文。合并官方代码或新增日志文案后重新执行：
    python3 bin/gen_message_text_zh.py
"""
import json
import re
import subprocess

files = subprocess.run(["git", "ls-files", "*.go"], capture_output=True, text=True, check=True).stdout.split()
keys = set()
for path in files:
    if path.endswith("_test.go") or path.startswith("web/"):
        continue
    with open(path, encoding="utf-8") as f:
        source = f.read()
    for match in re.finditer(r'NewMessage\(\s*"((?:[^"\\]|\\.)*)"', source):
        keys.add(json.loads('"' + match.group(1) + '"'))

with open("web/src/i18n/locales/zh.json", encoding="utf-8") as f:
    zh = json.load(f)["translation"]
table = {key: zh[key] for key in sorted(keys) if key in zh}
with open("common/message_text.zh-CN.json", "w", encoding="utf-8") as f:
    json.dump(table, f, ensure_ascii=False, indent=2)
    f.write("\n")
print(f"keys={len(keys)} translated={len(table)} untranslated={sorted(k for k in keys if k not in zh)}")
