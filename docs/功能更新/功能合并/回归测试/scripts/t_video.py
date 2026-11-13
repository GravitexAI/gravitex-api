#!/usr/bin/env python3
"""升级回归测试：Seedance 视频（平台接口 + 官方镜像接口）与素材库（镜像 Action 接口 + 平台简化接口）。
用法：python3 t_video.py <sk-令牌> <结果.json>
注意：素材库镜像接口真实路径是 POST /api/v3/seedance?Action=...（调用文档里写的 /ark/seedance/v3 是错的）。
测试环境若关闭了后台轮询（ENABLE_TASK_POLLING=false），任务状态靠 GET 实时查询推进。"""
import json, sys, time, urllib.request, urllib.error

BASE = "http://localhost:3000"
KEY = sys.argv[1]
OUT = sys.argv[2]
IMG = "https://upload.wikimedia.org/wikipedia/commons/thumb/3/3f/Fronalpstock_big.jpg/1280px-Fronalpstock_big.jpg"
R = []


def req(name, method, path, body=None, quiet=False):
    h = {"Authorization": f"Bearer {KEY}", "Content-Type": "application/json"}
    data = json.dumps(body).encode() if body is not None else None
    r0 = urllib.request.Request(BASE + path, data=data, headers=h, method=method)
    try:
        with urllib.request.urlopen(r0, timeout=120) as r:
            status, raw = r.status, r.read().decode("utf-8", "replace")
    except urllib.error.HTTPError as e:
        status, raw = e.code, e.read().decode("utf-8", "replace")
    except Exception as e:
        status, raw = -1, str(e)
    try:
        js = json.loads(raw) if raw.strip() else {}
    except Exception:
        js = {"_raw": raw[:500]}
    if not quiet:
        print(f"[{status}] {name}: {raw[:500]}", flush=True)
    R.append({"name": name, "status": status, "body": raw[:2000]})
    return status, js


def action(name, body):
    return req(f"镜像 {name}", "POST", f"/api/v3/seedance?Action={name}&Version=2024-01-01", body)


def poll(name, path, done, timeout=900, every=15):
    t0 = time.time()
    while time.time() - t0 < timeout:
        st, js = req(name, "GET", path, quiet=True)
        s = done(js)
        if s:
            print(f"[{st}] {name} 最终({int(time.time()-t0)}s): {json.dumps(js, ensure_ascii=False)[:700]}", flush=True)
            R.append({"name": name + " final", "status": st, "body": json.dumps(js, ensure_ascii=False)[:2000]})
            return js
        time.sleep(every)
    print(f"[TIMEOUT] {name}: {json.dumps(js, ensure_ascii=False)[:500]}", flush=True)
    return js


# ---------- 素材库：官方镜像 Action 接口 ----------
st, js = action("CreateAssetGroup", {"Name": "merge-test-1113", "Description": "合并回归测试"})
gid = (js.get("Result") or {}).get("Id")
action("GetAssetGroup", {"Id": gid})
action("UpdateAssetGroup", {"Id": gid, "Name": "merge-test-1113-upd", "Description": "已修改"})
action("ListAssetGroups", {"Filter": {"GroupType": "AIGC", "GroupIds": [gid]}, "PageNumber": 1, "PageSize": 10})
st, js = action("CreateAsset", {"GroupId": gid, "URL": IMG, "AssetType": "Image", "Name": "alps.jpg"})
aid = (js.get("Result") or {}).get("Id")
t0 = time.time()
while aid and time.time() - t0 < 300:
    st, js = req("镜像 GetAsset", "POST", "/api/v3/seedance?Action=GetAsset&Version=2024-01-01", {"Id": aid}, quiet=True)
    status = (js.get("Result") or {}).get("Status")
    if status in ("Active", "Failed"):
        print(f"[{st}] 镜像 GetAsset 最终({int(time.time()-t0)}s): {json.dumps(js, ensure_ascii=False)[:600]}", flush=True)
        break
    time.sleep(10)
action("ListAssets", {"Filter": {"GroupIds": [gid], "GroupType": "AIGC"}, "PageNumber": 1, "PageSize": 10})
action("UpdateAsset", {"Id": aid, "Name": "alps-upd.jpg"})
req("平台 /v1/assets 能查到镜像创建的素材", "GET", f"/v1/assets/{aid}")

# ---------- 视频：平台接口 ----------
vids = {}
for model in ("seedance-2-0-mini", "seedance-2-0-mini-NSFW"):
    st, js = req(f"平台提交 {model}", "POST", "/v1/video/generations",
                 {"model": model, "prompt": "A calm lake at sunrise, gentle ripples, slow camera pan", "duration": 4, "resolution": "480p", "ratio": "16:9", "generate_audio": False})
    vids[model] = js.get("task_id") or js.get("id")
# 官方镜像提交，引用刚创建的素材
st, js = req("镜像提交 seedance-2-0-mini + asset://", "POST", "/api/v3/contents/generations/tasks",
             {"model": "seedance-2-0-mini", "content": [{"type": "text", "text": "Clouds drifting over the mountain, slow push in"},
                                                        {"type": "image_url", "image_url": {"url": f"asset://{aid}"}, "role": "reference_image"}],
              "duration": 4, "resolution": "480p", "ratio": "16:9", "generate_audio": False})
mirror_id = js.get("id")

for model, tid in vids.items():
    if tid:
        poll(f"平台查询 {model} {tid}", f"/v1/video/generations/{tid}",
             lambda j: (j.get("status") or (j.get("data") or {}).get("status")) in ("completed", "failed", "succeeded", "SUCCESS", "FAILURE", "cancelled"))
if mirror_id:
    poll(f"镜像查询 {mirror_id}", f"/api/v3/contents/generations/tasks/{mirror_id}",
         lambda j: j.get("status") in ("succeeded", "failed", "cancelled", "expired"))
    req("镜像删除已完成任务", "DELETE", f"/api/v3/contents/generations/tasks/{mirror_id}")
    req("镜像删除后查询", "GET", f"/api/v3/contents/generations/tasks/{mirror_id}")

# ---------- 素材库清理 ----------
action("DeleteAsset", {"Id": aid})
action("DeleteAssetGroup", {"Id": gid})

# ---------- 素材库：平台简化接口 ----------
st, js = req("平台 创建素材组", "POST", "/v1/asset-groups", {"name": "merge-test-1113-simple", "description": "简化接口"})
g2 = js.get("group_id")
req("平台 素材组列表", "GET", "/v1/asset-groups")
st, js = req("平台 创建素材", "POST", "/v1/assets", {"url": IMG, "group_id": g2, "asset_type": "Image", "name": "alps2.jpg"})
a2 = js.get("virtual_id")
time.sleep(20)
req("平台 素材列表", "GET", f"/v1/assets?group_id={g2}")
req("平台 查询素材", "GET", f"/v1/assets/{a2}")
req("平台 删除素材", "DELETE", f"/v1/assets/{a2}")
req("平台 删除素材组", "DELETE", f"/v1/asset-groups/{g2}")

json.dump({"R": R, "vids": vids, "mirror": mirror_id, "gid": gid, "aid": aid}, open(OUT, "w"), ensure_ascii=False, indent=1)
print("done")
