import contextlib
import csv
import hashlib
import io
import json
import tempfile
import threading
import unittest
from unittest.mock import patch
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from zipfile import ZipFile

from ai_analysis import analyze, build_payload, endpoint, read_env, read_summary, request_analysis
from diagnostics.engine import Aggregator, generate_data, parse_time, redact, summary
from diagnostics.ingest import open_table
from error_diagnosis import write_html


class Fixtures(unittest.TestCase):
    def setUp(self):
        self.tmp=tempfile.TemporaryDirectory(prefix="errors-unit-")
        self.addCleanup(self.tmp.cleanup)
        self.root=Path(self.tmp.name)

    def csv(self,name,headers,rows,encoding="utf-8-sig"):
        path=self.root/name
        with path.open("w",encoding=encoding,newline="") as stream:
            writer=csv.writer(stream);writer.writerow(headers);writer.writerows(rows)
        return path

    def example(self):
        path=self.csv("errors.csv",["时间","用户名","模型名称","资源分组","渠道名称","错误信息","类型"],[
            ["2026-09-14 01:00:00","甲","M1","G1","C1","status_code=400 prompt is too long: 301 tokens > 200 maximum","错误"],
            ["2026-09-15 02:00:00","乙","M2","","C2","status_code=429 rate limit","错误"],
            ["bad","","M2","","C2","mystery </script><script>throw 1</script>","错误"],
            ["2026-09-15 03:00:00","甲","M1","G1","C1","ok","成功"]])
        return generate_data([path],progress=False)

    def test_week_missing_and_success(self):
        data=self.example()
        self.assertEqual(data["total"],3)
        self.assertEqual(data["rows_read"],4)
        self.assertEqual(data["period"],"2026-09-14_2026-09-15")
        self.assertEqual(data["missing"]["用户"],1)
        self.assertEqual(data["quality"]["日期或时间未知的错误数"],1)
        self.assertEqual(data["quality"]["非错误记录已排除"],1)
        html=write_html(data,self.root/"output")
        self.assertEqual(len(list(html.parent.iterdir())),1)
        self.assertNotIn("mystery </script><script>",html.read_text(encoding="utf-8"))
        self.assertEqual(read_summary(html)["total"],3)

    def test_custom_field_minimal_schema(self):
        p=self.csv("s.csv",["failure description"],[["unknown"],["unknown"]])
        d=generate_data([p],columns={"error":"failure description"},progress=False)
        self.assertEqual(d["period"],"未提供日期")
        self.assertEqual(d["total"],2)
        self.assertEqual(len(d["records"]),1)
        self.assertEqual(d["records"][0][10][0][1],2)
        fixed=generate_data([p],columns={"error":"failure description"},fallback_date="2026-09-14",progress=False)
        self.assertEqual(fixed["period"],"2026-09-14")
        self.assertEqual(fixed["records"][0][6],-1)

    def test_weighted_count_invalid_and_missing(self):
        p=self.csv("s.csv",["error","error_count"],[["a","5"],["a",""],["b","-2"],["b","bad"],["b","0"]])
        d=generate_data([p],progress=False)
        self.assertEqual(d["total"],6)
        self.assertEqual(d["quality"]["错误次数无效的行已排除"],2)
        self.assertEqual(d["quality"]["次数空白按单条记录计数"],1)
        self.assertEqual(d["quality"]["零次数记录已排除"],1)

    def test_sample_cap_never_loses_counts(self):
        p=self.csv("s.csv",["error","user"],[[f"unclassified {i}",f"u{i%2}"] for i in range(100)])
        d=generate_data([p],max_samples=2,samples_per_bucket=1,progress=False)
        self.assertEqual(d["total"],100)
        self.assertEqual(len(d["samples"]),2)
        self.assertEqual(d["quality"]["未保存证据样本的错误数"],98)
        with self.assertRaisesRegex(ValueError,"聚合桶超过"):
            generate_data([p],max_buckets=1,progress=False)

    def test_status_only_and_empty(self):
        p=self.csv("s.csv",["status_code"],[["200"],["503"],["403"]])
        d=generate_data([p],progress=False)
        self.assertEqual(d["total"],2)
        empty=self.csv("empty.csv",["错误信息"],[])
        d=generate_data([empty],progress=False)
        self.assertEqual(d["total"],0)
        self.assertEqual(read_summary(write_html(d,self.root/"out"))["total"],0)

    def test_multiple_files_and_duplicate_guard(self):
        a=self.csv("a.csv",["error","timestamp"],[["a","2026-09-14"]])
        b=self.csv("b.csv",["message","created_at"],[["b","2026-09-20"]])
        self.assertEqual(generate_data([a,b],progress=False)["period"],"2026-09-14_2026-09-20")
        with self.assertRaisesRegex(ValueError,"路径重复"):
            generate_data([a,a],progress=False)

    def test_ambiguous_headers_and_bad_schema(self):
        p=self.csv("s.csv",["error","message"],[["a","b"]])
        with self.assertRaises(ValueError):
            generate_data([p],progress=False)
        self.assertEqual(generate_data([p],columns={"error":"message"},progress=False)["total"],1)
        p=self.csv("bad.csv",["unrelated"],[["x"]])
        with self.assertRaisesRegex(ValueError,"未识别"):
            generate_data([p],progress=False)

    def test_dates_and_redaction(self):
        self.assertEqual(parse_time("2026-09-14T18:00:00Z").date().isoformat(),"2026-09-15")
        self.assertEqual(parse_time("20260914").date().isoformat(),"2026-09-14")
        self.assertEqual(parse_time("1",True).date().isoformat(),"1904-01-02")
        self.assertEqual(parse_time("1").date().isoformat(),"1900-01-01")
        self.assertIsNone(parse_time("bad"))
        text=redact('prompt 300 > 200 Request id: abcdef123456 https://example.com/private sk-secret12345 api_key="private123" 1.2.3.4')
        self.assertIn("300 > 200",text)
        for private in ["abcdef123456","example.com","private123","1.2.3.4"]:
            self.assertNotIn(private,text)

    def test_xlsx_sharedstrings_sparse_and_title(self):
        p=self.root/"fixture.xlsx"
        with ZipFile(p,"w") as z:
            z.writestr("xl/workbook.xml",'<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><workbookPr date1904="1"/><sheets><sheet name="明细" sheetId="1" r:id="rId1"/></sheets></workbook>')
            z.writestr("xl/_rels/workbook.xml.rels",'<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Target="worksheets/sheet1.xml"/></Relationships>')
            z.writestr("xl/sharedStrings.xml",'<sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><si><t>错误信息</t></si><si><t>时间</t></si><si><t>status_code=503 provider error</t></si></sst>')
            z.writestr("xl/worksheets/sheet1.xml",'<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData><row r="1"><c r="A1" t="inlineStr"><is><t>标题</t></is></c></row><row r="3"><c r="B3" t="s"><v>0</v></c><c r="D3" t="s"><v>1</v></c></row><row r="4"><c r="B4" t="s"><v>2</v></c><c r="D4"><v>44087</v></c></row></sheetData></worksheet>')
        d=generate_data([p],progress=False)
        self.assertEqual(d["total"],1)
        self.assertEqual(d["sources"][0]["header_row"],3)
        self.assertEqual(d["samples"][0]["row"],4)
        self.assertNotEqual(d["period"],"未提供日期")

    def test_gb_csv(self):
        p=self.csv("s.csv",["错误信息","渠道名称"],[["失败","中文渠道"]],"gb18030")
        d=generate_data([p],progress=False)
        self.assertEqual(d["labels"][3],["中文渠道"])

    def test_ai_payload_and_dry_run(self):
        facts=summary(self.example())
        payload=build_payload(facts)
        parsed=json.loads(payload)
        self.assertNotIn("sources",parsed)
        self.assertTrue(all(x["label"].startswith("用户") for x in parsed["users"]))
        self.assertEqual(parsed["total"],3)
        html=write_html(self.example(),self.root/"out")
        with contextlib.redirect_stdout(io.StringIO()) as output:
            self.assertIsNone(analyze(html,self.root/"absent.env",dry_run=True))
        self.assertEqual(json.loads(output.getvalue())["total"],3)
        self.assertEqual(list(self.root.rglob("*.md")),[])

    def test_env_and_urls(self):
        p=self.root/".env"
        p.write_text('BASE_URL="http://localhost:9876/v1"\nMODEL=mock\nAPI_KEY="test#key"\n',encoding="utf-8")
        env=read_env(p)
        self.assertEqual(env["API_KEY"],"test#key")
        p.write_text("baseurl=http://localhost:9876/v1\nmodel=mock\napikey=test\n",encoding="utf-8")
        self.assertEqual(read_env(p)["BASEURL"],"http://localhost:9876/v1")
        self.assertEqual(endpoint("https://example.com/v1"),"https://example.com/v1/chat/completions")
        self.assertEqual(endpoint("http://localhost:9876"),"http://localhost:9876/v1/chat/completions")
        with self.assertRaises(ValueError):
            endpoint("http://example.com")
        with self.assertRaises(ValueError):
            endpoint("https://example.com/\nsecret")

    def test_mock_api_success_and_failure_preserve_facts(self):
        received=[]
        mode={"status":200}
        class Handler(BaseHTTPRequestHandler):
            def do_POST(self):
                body=json.loads(self.rfile.read(int(self.headers["Content-Length"])))
                received.append((self.path,body,self.headers["Authorization"]))
                self.send_response(mode["status"]);self.send_header("Content-Type","application/json");self.end_headers()
                content={"choices":[{"finish_reason":"stop","message":{"content":"### 建议\n核查 E1 的参数边界。"}}]}
                self.wfile.write(json.dumps(content).encode())
            def log_message(self,*args):
                pass
        server=ThreadingHTTPServer(("127.0.0.1",0),Handler)
        thread=threading.Thread(target=server.serve_forever,daemon=True);thread.start()
        self.addCleanup(server.server_close);self.addCleanup(server.shutdown)
        env=self.root/".env"
        env.write_text(f"BASE_URL=http://127.0.0.1:{server.server_port}/v1\nMODEL=mock\nAPI_KEY=test-only\nAI_RETRIES=0\n",encoding="utf-8")
        html=write_html(self.example(),self.root/"external-html")
        report=self.root/"output"/"2026-09-14_2026-09-15"/"error_diagnosis_report_20260914_20260915.md"
        html_hash=hashlib.sha256(html.read_bytes()).digest()
        mode["status"]=401
        with patch("ai_analysis.ROOT",self.root), self.assertRaisesRegex(ValueError,"HTTP 401"):
            analyze(html,env)
        self.assertFalse(report.exists())
        mode["status"]=200
        with patch("ai_analysis.ROOT",self.root):
            self.assertEqual(analyze(html,env),report)
        content=report.read_text(encoding="utf-8")
        self.assertIn("AI 分析与优化建议",content)
        self.assertIn("数据对比",content)
        self.assertEqual(received[0][0],"/v1/chat/completions")
        self.assertEqual(received[0][2],"Bearer test-only")
        self.assertEqual(received[0][1]["model"],"mock")
        self.assertEqual(hashlib.sha256(html.read_bytes()).digest(),html_hash)
        mode["status"]=401
        with patch("ai_analysis.ROOT",self.root), self.assertRaisesRegex(ValueError,"HTTP 401"):
            analyze(html,env)
        self.assertEqual(report.read_text(encoding="utf-8"),content)


if __name__=="__main__":
    unittest.main()
