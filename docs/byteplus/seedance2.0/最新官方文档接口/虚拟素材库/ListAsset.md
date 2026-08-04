`POST https://ark.ap-southeast-1.byteplusapi.com/?Action=ListAssets&Version=2024-01-01`

This document describes the request and response parameters of the List Assets API. You can retrieve Assets that match specified filters.


<Tabs>
<Tab zoneid="I3VIeeFlri" title="Quick Links">
<TabTitle>Quick Links</TabTitle>

<span>![图片](https://portal.volccdn.com/obj/volcfe/cloud-universal-doc/upload_57d0bca8e0d122ab1191b40101b5df75.png) </span> [Tutorial](https://docs.byteplus.com/en/docs/ModelArk/2333565) <span>![图片](https://portal.volccdn.com/obj/volcfe/cloud-universal-doc/upload_f45b5cd5863d1eed3bc3c81b9af54407.png) </span> [API List](https://docs.byteplus.com/en/docs/ModelArk/2333601) <span>![图片](https://portal.volccdn.com/obj/volcfe/cloud-universal-doc/upload_bef4bc3de3535ee19d0c5d6c37b0ffdd.png) </span> [Enable Model](https://console.byteplus.com/ark/region:ark+ap-southeast-1/openManagement?LLM=%7B%7D&OpenTokenDrawer=false)


</Tab>
<Tab zoneid="G6jqL6JPvf" title="Authentication">
<TabTitle>Authentication</TabTitle>

This API only supports Access Key (AK/SK) authentication.


</Tab>
</Tabs>



---



<span id="cBcCiB1q"></span>
## Request parameters

> Go to [Response parameters](https://docs.byteplus.com/en/docs/ModelArk/2318273#7hRth93G)



---



<span id="7f8hIDhA"></span>
### Request body


---



**Filter** `object` `Required`

Filter conditions.


Attributes


---



Filter. **GroupIds** `string[]`

A list of Asset Group IDs that the Assets belong to.


---



Filter. **GroupType** `string` `Required`

The Asset Group type. Valid values:


* `AIGC`: Digital characters

* `LivenessFace`: Real\-person portrait



---



Filter. **Statuses** `string[]`

Asset status. Valid values:


* `Active`: Pre\-processing completed and can be used

* `Processing`: Pre\-processing in progress and cannot be used

* `Failed`: Pre\-processing failed



---



Filter. **Name** `string`

The Asset name, up to 64 characters.



---



**PageNumber** `integer (i64)` `Required`

The page number for pagination, starting from 1.


---



**PageSize** `integer (i64)` `Required`

The number of results per page, up to 100.


---



**SortBy** `string`

The field used for sorting. Default is `CreateTime`. Valid values:


* `CreateTime`: Sort by creation time

* `UpdateTime`: Sort by update time

* `GroupId`: Sort by Asset Group ID



---



**SortOrder** `string`

The sorting order. Default is `Desc`. Valid values:


* `Desc`: Descending

* `Asc`: Ascending



---



**ProjectName** `string`

The project name that the resource belongs to. Default is `default`. If the resource is not in the default project, set the correct project name.

<span id="7hRth93G"></span>
## Response parameters

> Go to [Request parameters](https://docs.byteplus.com/en/docs/ModelArk/2318273#cBcCiB1q)



---



**Items** `object[]`

An array of matching Assets.


Attributes


---



Items. **Id** `string`

The Asset ID.


---



Items. **Name** `string`

The Asset name, up to 64 characters.


---



Items. **URL** `string`

The public URL of the Asset. Valid for 12 hours. Save it in time.


---



Items. **GroupId** `string`

The Asset Group ID that the Asset belongs to.


---



Items. **AssetType** `string`

The Asset type. Valid values:


* `Image`

* `Video`

* `Audio`



---



Items. **Status** `string`

The Asset status. Valid values:


* `Active`

* `Processing`

* `Failed`



---



Items. **Error** `object`

Error information. Returned when `Status` is `Failed`.


Attributes


---



Items.Error. **Code** `string`

Error code.


---



Items.Error. **Message** `string`

Error message.



---



Items. **Moderation** `object`

Content Pre\-filter review related information.


Attributes


---



Items.Moderation. **Strategy** `string`

Indicates whether the Content Pre\-filter review is turned on for this asset. Available values:


* `Default`: Content Pre\-filter review is on.

* `Skip`: Most non\-baseline content security review policies are off.



---



Items. **ProjectName** `string`

The project name that the resource belongs to.


---



Items. **CreateTime** `string`

Creation time.


---



Items. **UpdateTime** `string`

Update time.



---



**TotalCount** `integer (i64)`

Total number of matching results.


---



**PageNumber** `integer (i64)`

The page number returned.


---



**PageSize** `integer (i64)`

The number of results per page, up to 100.


---



<span id="UXjzfxmT"></span>
## Request example

```text
POST /?Action=ListAssets&Version=2024-01-01 HTTP/1.1
Host: ark.ap-southeast-1.byteplusapi.com
Content-Type: application/json
X-Date: 20260328T000000Z
X-Content-Sha256: 287e874e******d653b44d21e
Authorization: HMAC-SHA256 Credential=AKLTYz******/20260328/ap-southeast-1/ark/request, SignedHeaders=content-type;host;x-content-sha256;x-date, Signature=47a7d934******e41085f

{
  "Filter": {
    "GroupIds": [
      "group-2026**********-*****"
    ],
    "GroupType": "AIGC",
    "Statuses": [
      "Active"
    ],
    "Name": "test"
  },
  "PageNumber": 1,
  "PageSize": 10,
  "SortBy": "CreateTime",
  "SortOrder": "Desc"
}
```


<span id="HBCxkDKx"></span>
## Response example

```JSON
{
  "ResponseMetadata": {
    "RequestId": "20260328000000000000000000000000",
    "Action": "ListAssets",
    "Version": "2024-01-01",
    "Service": "ark",
    "Region": "ap-southeast-1"
  },
  "Result": {
    "Items": [
      {
        "Id": "Asset-2026**********-*****",
        "Name": "test",
        "URL": "https://example.com/asset-url",
        "GroupId": "group-2026**********-*****",
        "AssetType": "Image",
        "Status": "Active",
        "Moderation": {
          "Strategy": "Default"
         },
        "ProjectName": "default",
        "CreateTime": "2026-03-28T00:00:00Z",
        "UpdateTime": "2026-03-28T00:00:00Z"
      }
    ],
    "TotalCount": 1,
    "PageNumber": 1,
    "PageSize": 10
  }
}
```




