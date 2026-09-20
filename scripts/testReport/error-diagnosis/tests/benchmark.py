"""Generate a multi-day synthetic XLSX and benchmark without retaining raw data."""
import argparse
import ctypes
import json
import sys
import tempfile
import time
from pathlib import Path
from xml.sax.saxutils import escape
from zipfile import ZIP_DEFLATED, ZipFile

sys.path.insert(0,str(Path(__file__).resolve().parents[1]))
from diagnostics.engine import generate_data
from error_diagnosis import write_html


def peak_mib():
    if sys.platform=="win32":
        class Memory(ctypes.Structure):
            _fields_=[("cb",ctypes.c_ulong),("PageFaultCount",ctypes.c_ulong)]+[(name,ctypes.c_size_t) for name in (
                "PeakWorkingSetSize","WorkingSetSize","QuotaPeakPagedPoolUsage","QuotaPagedPoolUsage","QuotaPeakNonPagedPoolUsage","QuotaNonPagedPoolUsage","PagefileUsage","PeakPagefileUsage","PrivateUsage")]
        memory=Memory();memory.cb=ctypes.sizeof(memory)
        ctypes.windll.kernel32.GetCurrentProcess.restype=ctypes.c_void_p
        reader=ctypes.windll.psapi.GetProcessMemoryInfo
        reader.argtypes=[ctypes.c_void_p,ctypes.POINTER(Memory),ctypes.c_ulong]
        reader.restype=ctypes.c_int
        handle=ctypes.windll.kernel32.GetCurrentProcess()
        if reader(handle,ctypes.byref(memory),memory.cb):
            return round(memory.PeakWorkingSetSize/1024**2,2)
        return None
    import resource
    scale=1024**2 if sys.platform=="darwin" else 1024
    return round(resource.getrusage(resource.RUSAGE_SELF).ru_maxrss/scale,2)


def fixture(path,n):
    ns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"
    with ZipFile(path,"w",compression=ZIP_DEFLATED,compresslevel=1) as z:
        z.writestr("[Content_Types].xml",'<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/><Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/></Types>')
        z.writestr("_rels/.rels",'<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>')
        z.writestr("xl/workbook.xml",f'<workbook xmlns="{ns}" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="错误明细" sheetId="1" r:id="rId1"/></sheets></workbook>')
        z.writestr("xl/_rels/workbook.xml.rels",'<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/></Relationships>')
        with z.open("xl/worksheets/sheet1.xml","w") as stream:
            stream.write(f'<worksheet xmlns="{ns}"><sheetData>'.encode())
            def row(number,values):
                xml=f'<row r="{number}">'+''.join(f'<c r="{chr(65+j)}{number}" t="inlineStr"><is><t>{escape(str(value))}</t></is></c>' for j,value in enumerate(values))+'</row>'
                stream.write(xml.encode())
            row(1,["时间","用户","服务/模型","资源分组","渠道名称","错误信息"])
            texts=["status_code=400 access disabled","status_code=429 rate limit","status_code=503 overloaded","status_code=400 invalid image format","status_code=400 prompt is too long: {limit} tokens > 200000 maximum"]
            per_day=max(1,(n+6)//7)
            for i in range(n):
                day=14+min(6,i//per_day)
                text=texts[i%5].format(limit=200001+i%10000)+f" Request id: req_{i:020d}"
                row(i+2,[f"2026-09-{day:02d} {(i//1000)%24:02d}:00:{i%60:02d}",f"U{i%80:03d}",f"M{i%12:02d}",f"G{i%3}",f"C{i%6}",text])
            stream.write(b"</sheetData></worksheet>")


def main():
    parser=argparse.ArgumentParser()
    parser.add_argument("--rows",type=int,default=350000)
    parser.add_argument("--output",type=Path,help="保留 HTML；不填则测试结束自动清理")
    args=parser.parse_args()
    with tempfile.TemporaryDirectory(prefix="errors-benchmark-") as temporary:
        root=Path(temporary);source=root/"synthetic-week.xlsx"
        fixture(source,args.rows)
        started=time.perf_counter()
        data=generate_data([source])
        html=write_html(data,args.output or root/"output")
        elapsed=time.perf_counter()-started
        assert data["total"]==args.rows==sum(r[7] for r in data["records"])
        print(json.dumps(dict(synthetic=True,rows=args.rows,period=data["period"],buckets=len(data["records"]),
                             samples=len(data["samples"]),elapsed_seconds=round(elapsed,3),
                             peak_working_set_mib=peak_mib(),xlsx_mib=round(source.stat().st_size/1024**2,2),
                             html_mib=round(html.stat().st_size/1024**2,2),html=str(html)),ensure_ascii=False,indent=2))


if __name__=="__main__":
    main()
