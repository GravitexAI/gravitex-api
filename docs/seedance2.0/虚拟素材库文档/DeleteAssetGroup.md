`POST https://ark.ap-southeast-1.byteplusapi.com/?Action=DeleteAssetGroup&Version=2024-01-01`

Delete asset group (Asset Group).

<div data-tips="true" data-tips-type="warning" data-tips-is-title="true">warning</div>



* <div data-tips="true" data-tips-type="warning">Deleting an asset group will also delete all assets in the group. This action is irreversible and cannot be undone. Proceed with caution.</div>


* <div data-tips="true" data-tips-type="warning">If the asset group to be deleted contains a large number of assets, the delete actions may take longer time to complete.</div>


* <div data-tips="true" data-tips-type="warning">For real\-human portrait asset groups created in the ModelArk console, <strong>only asset groups with expired authorization or asset groups whose authorization has been refused can be deleted;</strong> assets with valid authorization, assets whose authorization period has not started, or assets that have been accepted cannot be deleted.</div>




<Tabs>
<Tab zoneid="TviionpB" title="Quick Links">
<TabTitle>Quick Links</TabTitle>

<span>![图片](https://portal.volccdn.com/obj/volcfe/cloud-universal-doc/upload_57d0bca8e0d122ab1191b40101b5df75.png) </span> [Tutorial](https://docs.byteplus.com/en/docs/ModelArk/2333565) <span>![图片](https://portal.volccdn.com/obj/volcfe/cloud-universal-doc/upload_f45b5cd5863d1eed3bc3c81b9af54407.png) </span> [API List](https://docs.byteplus.com/en/docs/ModelArk/2333601) <span>![图片](https://portal.volccdn.com/obj/volcfe/cloud-universal-doc/upload_bef4bc3de3535ee19d0c5d6c37b0ffdd.png) </span> [Enable Model](https://console.byteplus.com/ark/region:ark+ap-southeast-1/openManagement?LLM=%7B%7D&OpenTokenDrawer=false)


</Tab>
<Tab zoneid="t6evJIFq" title="Authentication">
<TabTitle>Authentication</TabTitle>

This API only supports Access Key (AK/SK) authentication.


</Tab>
</Tabs>



---



<span id="IcpXw2Np"></span>
## Request parameters

<span id="rHDbPxhw"></span>
### Request body


---



**Id ** `string`<span data-api-tag="require|S4KDsq">必选</span>

ID of the asset group to delete.


---



**ProjectName ** `string`

The project name that the asset group belongs to. Default is `default`. If the resource is not in the default project, set the correct project name.

<span id="ZAGVNsph"></span>
## Response parameters

<div data-tips="true" data-tips-type="tip" data-tips-is-title="true">tip</div>


<div data-tips="true" data-tips-type="tip">This API does not return any business return parameters.</div>



---



<span id="Mdj0tlfJ"></span>
## Request example

```text
POST /?Action=DeleteAssetGroup&Version=2024-01-01 HTTP/1.1
Host: ark.ap-southeast-1.byteplusapi.com
Content-Type: application/json
X-Date: 20260328T000000Z
X-Content-Sha256: 287e874e******d653b44d21e
Authorization: HMAC-SHA256 Credential=AKLTYz******/20260328/cn-beijing/ark/request, SignedHeaders=content-type;host;x-content-sha256;x-date, Signature=47a7d934******e41085f

{
  "Id": "group-2026**********-*****"
}
```


<span id="eIq1tfFY"></span>
## Sample response

```json
{
  "ResponseMetadata": {
    "RequestId": "20260328000000000000000000000000",
    "Action": "DeleteAssetGroup",
    "Version": "2024-01-01",
    "Service": "ark",
    "Region": "cn-beijing"
  },
  "Result": {}
}
```




