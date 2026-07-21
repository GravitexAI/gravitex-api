`POST https://ark.ap-southeast-1.byteplusapi.com/?Action=DeleteAsset&Version=2024-01-01`

This document describes the request and response parameters of the Delete Asset API.


<Tabs>
<Tab zoneid="RloyYNl1" title="Quick Links">
<TabTitle>Quick Links</TabTitle>

<span>![图片](https://portal.volccdn.com/obj/volcfe/cloud-universal-doc/upload_57d0bca8e0d122ab1191b40101b5df75.png) </span> [Tutorial](https://docs.byteplus.com/en/docs/ModelArk/2333565) <span>![图片](https://portal.volccdn.com/obj/volcfe/cloud-universal-doc/upload_f45b5cd5863d1eed3bc3c81b9af54407.png) </span> [API List](https://docs.byteplus.com/en/docs/ModelArk/2333601) <span>![图片](https://portal.volccdn.com/obj/volcfe/cloud-universal-doc/upload_bef4bc3de3535ee19d0c5d6c37b0ffdd.png) </span> [Enable Model](https://console.byteplus.com/ark/region:ark+ap-southeast-1/openManagement?LLM=%7B%7D&OpenTokenDrawer=false)


</Tab>
<Tab zoneid="xjOI3R0b" title="Authentication">
<TabTitle>Authentication</TabTitle>

This API only supports Access Key (AK/SK) authentication.


</Tab>
</Tabs>



---



<span id="OMPxUk7t"></span>
## Request Parameters

<span id="xsRAfGJH"></span>
### Request Body


---



**Id** `string` <span data-api-tag="require|mm4SVY">必选</span>

The Asset ID to delete.


---



**ProjectName** `string`

The project name that the Asset belongs to. Default is `default`. If the resource is not in the default project, set the correct project name.

<span id="yYqnxX36"></span>
## Response Parameters

<div data-tips="true" data-tips-type="tip" data-tips-is-title="true">Note</div>


<div data-tips="true" data-tips-type="tip">This API has no business\-specific response parameters.</div>



---



<span id="iZNxPOyD"></span>
## Request Example

```text
POST /?Action=DeleteAsset&Version=2024-01-01 HTTP/1.1
Host: ark.ap-southeast-1.byteplusapi.com
Content-Type: application/json
X-Date: 20260328T000000Z
X-Content-Sha256: 287e874e******d653b44d21e
Authorization: HMAC-SHA256 Credential=AKLTYz******/20260328/ap-southeast-1/ark/request, SignedHeaders=content-type;host;x-content-sha256;x-date, Signature=47a7d934******e41085f

{
  "Id": "Asset-2026**********-*****"
}
```


<span id="0JXotCBu"></span>
## Response Example

```json
{
  "ResponseMetadata": {
    "RequestId": "20260328000000000000000000000000",
    "Action": "DeleteAsset",
    "Version": "2024-01-01",
    "Service": "ark",
    "Region": "ap-southeast-1"
  },
  "Result": {}
}
```




