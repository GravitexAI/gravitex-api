`POST https://ark.ap-southeast-1.byteplusapi.com/?Action=GetAssetGroup&Version=2024-01-01`

Get information about a single asset group.


<Tabs>
<Tab zoneid="kdTLES0W1W" title="Quick Links">
<TabTitle>Quick Links</TabTitle>

<span>![图片](https://portal.volccdn.com/obj/volcfe/cloud-universal-doc/upload_57d0bca8e0d122ab1191b40101b5df75.png) </span> [Tutorial](https://docs.byteplus.com/en/docs/ModelArk/2333565) <span>![图片](https://portal.volccdn.com/obj/volcfe/cloud-universal-doc/upload_f45b5cd5863d1eed3bc3c81b9af54407.png) </span> [API List](https://docs.byteplus.com/en/docs/ModelArk/2333601) <span>![图片](https://portal.volccdn.com/obj/volcfe/cloud-universal-doc/upload_bef4bc3de3535ee19d0c5d6c37b0ffdd.png) </span> [Enable Model](https://console.byteplus.com/ark/region:ark+ap-southeast-1/openManagement?LLM=%7B%7D&OpenTokenDrawer=false)


</Tab>
<Tab zoneid="xOHP2Ea0xv" title="Authentication">
<TabTitle>Authentication</TabTitle>

This API only supports Access Key (AK/SK) authentication.


</Tab>
</Tabs>



---



<span id="zlM4SNro"></span>
## Request parameters

<span id="NQttRdMe"></span>
### Request body


---



**Id** `string` `Required`

The Asset Group ID.


---



**ProjectName** `string`

The project name that the Asset Group belongs to. Default is `default`. If the resource is not in the default project, set the correct project name.

<span id="qtgbn7uS"></span>
## Response parameters


---



**Id** `string`

The Asset Group ID.


---



**Name** `string`

The Asset Group name, up to 64 characters.


---



**Description** `string`

The Asset Group description, up to 300 characters.


---



**GroupType** `string`

The Asset Group type. Valid values:


* `AIGC`: Digital characters

* `LivenessFace`: Real\-person portrait



---



**ProjectName** `string`

The project name that the resource belongs to.


---



**CreateTime** `string`

Creation time.


---



**UpdateTime** `string`

Update time.


---



<span id="kVUaplSd"></span>
## Request example

```text
POST /?Action=GetAssetGroup&Version=2024-01-01 HTTP/1.1
Host: ark.ap-southeast-1.byteplusapi.com
Content-Type: application/json
X-Date: 20260328T000000Z
X-Content-Sha256: 287e874e******d653b44d21e
Authorization: HMAC-SHA256 Credential=AKLTYz******/20260328/ap-southeast-1/ark/request, SignedHeaders=content-type;host;x-content-sha256;x-date, Signature=47a7d934******e41085f

{
  "Id": "group-2026**********-*****"
}
```


<span id="kHH3diPl"></span>
## Response example

```JSON
{
  "ResponseMetadata": {
    "RequestId": "20260328000000000000000000000000",
    "Action": "GetAssetGroup",
    "Version": "2024-01-01",
    "Service": "ark",
    "Region": "ap-southeast-1"
  },
  "Result": {
    "Id": "group-2026**********-*****",
    "Name": "test",
    "Description": "test",
    "GroupType": "AIGC",
    "ProjectName": "default",
    "CreateTime": "2026-03-28T00:00:00Z",
    "UpdateTime": "2026-03-28T00:00:00Z"
  }
}
```




