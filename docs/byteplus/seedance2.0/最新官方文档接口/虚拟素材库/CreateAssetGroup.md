`POST https://ark.ap-southeast-1.byteplusapi.com/?Action=CreateAssetGroup&Version=2024-01-01`

Create an Asset Group for asset management.

<div data-tips="true" data-tips-type="tip" data-tips-is-title="true">Tip</div>


<div data-tips="true" data-tips-type="tip">Before creating your first Asset Group, you must sign the authorization letter in the console. See <a href="https://docs.byteplus.com/en/docs/ModelArk/2333565">Private digital asset library (invited users only)</a>.</div>



<Tabs>
<Tab zoneid="MEPTON9Wai" title="Quick Links">
<TabTitle>Quick Links</TabTitle>

<span>![图片](https://portal.volccdn.com/obj/volcfe/cloud-universal-doc/upload_57d0bca8e0d122ab1191b40101b5df75.png) </span> [Tutorial](https://docs.byteplus.com/en/docs/ModelArk/2333565) <span>![图片](https://portal.volccdn.com/obj/volcfe/cloud-universal-doc/upload_f45b5cd5863d1eed3bc3c81b9af54407.png) </span> [API List](https://docs.byteplus.com/en/docs/ModelArk/2333601) <span>![图片](https://portal.volccdn.com/obj/volcfe/cloud-universal-doc/upload_bef4bc3de3535ee19d0c5d6c37b0ffdd.png) </span> [Enable Model](https://console.byteplus.com/ark/region:ark+ap-southeast-1/openManagement?LLM=%7B%7D&OpenTokenDrawer=false)


</Tab>
<Tab zoneid="GMEmInWOOd" title="Authentication">
<TabTitle>Authentication</TabTitle>

This API only supports Access Key (AK/SK) authentication.


</Tab>
</Tabs>



---



<span id="EEkeiDmP"></span>
## Request parameters

<span id="jCGW8AVv"></span>
### Request body


---



**Name** `string` `Required`

The name of the Asset Group, up to 64 characters.


---



**Description** `string`

The description of the Asset Group, up to 300 characters.


---



**GroupType** `string`

The type of the Asset Group. Valid values:


* `AIGC`: Digital characters (currently the only supported value).



---



**ProjectName** `string`

The name of the project to which the resource belongs. The default value is default.

If the resource is not in the default project, you must enter the correct project name. For more information about project, see the related [IAM docs](https://docs.byteplus.com/en/docs/byteplus-platform/docs-managing-projects).

<span id="o8Kt6Q9s"></span>
## Response parameters


---



**Id** `string`

The Asset Group ID.


---



<span id="Y3v2D8VB"></span>
## Request example

```text
POST /?Action=CreateAssetGroup&Version=2024-01-01 HTTP/1.1
Host: ark.ap-southeast-1.byteplusapi.com
Content-Type: application/json
X-Date: 20260328T000000Z
X-Content-Sha256: 287e874e******d653b44d21e
Authorization: HMAC-SHA256 Credential=AKLTYz******/20260328/ap-southeast-1/ark/request, SignedHeaders=content-type;host;x-content-sha256;x-date, Signature=47a7d934******e41085f

{
  "Name": "test",
  "Description": "test",
  "GroupType": "AIGC"
}
```


<span id="4OXJzalX"></span>
## Response example

```JSON
{
  "ResponseMetadata": {
    "RequestId": "20260328000000000000000000000000",
    "Action": "CreateAssetGroup",
    "Version": "2024-01-01",
    "Service": "ark",
    "Region": "ap-southeast-1"
  },
  "Result": {
    "Id": "group-2026**********-*****"
  }
}
```




