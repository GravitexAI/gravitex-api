`POST https://ark.ap-southeast-1.byteplusapi.com/?Action=UpdateAssetGroup&Version=2024-01-01`

This document describes the request and response parameters of the Update Asset Group API. Currently, only `Name` and `Description` can be updated.


<Tabs>
<Tab zoneid="JXbZ9mRTKt" title="Quick Links">
<TabTitle>Quick Links</TabTitle>

<span>![图片](https://portal.volccdn.com/obj/volcfe/cloud-universal-doc/upload_57d0bca8e0d122ab1191b40101b5df75.png) </span> [Tutorial](https://docs.byteplus.com/en/docs/ModelArk/2333565) <span>![图片](https://portal.volccdn.com/obj/volcfe/cloud-universal-doc/upload_f45b5cd5863d1eed3bc3c81b9af54407.png) </span> [API List](https://docs.byteplus.com/en/docs/ModelArk/2333601) <span>![图片](https://portal.volccdn.com/obj/volcfe/cloud-universal-doc/upload_bef4bc3de3535ee19d0c5d6c37b0ffdd.png) </span> [Enable Model](https://console.byteplus.com/ark/region:ark+ap-southeast-1/openManagement?LLM=%7B%7D&OpenTokenDrawer=false)


</Tab>
<Tab zoneid="pFlpN6N562" title="Authentication">
<TabTitle>Authentication</TabTitle>

This API only supports Access Key (AK/SK) authentication.


</Tab>
</Tabs>



---



<span id="5BSVU6YT"></span>
## Request parameters

<span id="i4tVBTbF"></span>
### Request body


---



**Id** `string` `Required`

The ID of the Asset Group to update.


---



**Name** `string`

The new Asset Group name, up to 64 characters.


---



**Description** `string`

The new Asset Group description, up to 300 characters.


---



**ProjectName** `string`

The project name that the Asset Group belongs to. Default is `default`. If the resource is not in the default project, set the correct project name.

<span id="hup78hlb"></span>
## Response parameters


---



**Id** `string`

The Asset Group ID.


---



<span id="AwKScS2K"></span>
## Request example

```text
POST /?Action=UpdateAssetGroup&Version=2024-01-01 HTTP/1.1
Host: ark.ap-southeast-1.byteplusapi.com
Content-Type: application/json
X-Date: 20260328T000000Z
X-Content-Sha256: 287e874e******d653b44d21e
Authorization: HMAC-SHA256 Credential=AKLTYz******/20260328/ap-southeast-1/ark/request, SignedHeaders=content-type;host;x-content-sha256;x-date, Signature=47a7d934******e41085f

{
  "Id": "group-2026**********-*****",
  "Name": "new-name",
  "Description": "new-description"
}
```


<span id="UtHA9mNP"></span>
## Response example

```JSON
{
  "ResponseMetadata": {
    "RequestId": "20260328000000000000000000000000",
    "Action": "UpdateAssetGroup",
    "Version": "2024-01-01",
    "Service": "ark",
    "Region": "ap-southeast-1"
  },
  "Result": {
    "Id": "group-2026**********-*****"
  }
}
```




