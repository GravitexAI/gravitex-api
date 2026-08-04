`POST https://ark.ap-southeast-1.byteplusapi.com/?Action=ListAssetGroups&Version=2024-01-01`

Query the list of asset groups that meet the filter conditions.


<Tabs>
<Tab zoneid="YbBhxxdn8q" title="Quick Links">
<TabTitle>Quick Links</TabTitle>

<span>![图片](https://portal.volccdn.com/obj/volcfe/cloud-universal-doc/upload_57d0bca8e0d122ab1191b40101b5df75.png) </span> [Tutorial](https://docs.byteplus.com/en/docs/ModelArk/2333565) <span>![图片](https://portal.volccdn.com/obj/volcfe/cloud-universal-doc/upload_f45b5cd5863d1eed3bc3c81b9af54407.png) </span> [API List](https://docs.byteplus.com/en/docs/ModelArk/) <span>![图片](https://portal.volccdn.com/obj/volcfe/cloud-universal-doc/upload_bef4bc3de3535ee19d0c5d6c37b0ffdd.png) </span> [Enable Model](https://console.byteplus.com/ark/region:ark+ap-southeast-1/openManagement?LLM=%7B%7D&OpenTokenDrawer=false)


</Tab>
<Tab zoneid="bkAHJORNXh" title="Authentication">
<TabTitle>Authentication</TabTitle>

This API only supports Access Key (AK/SK) authentication.


</Tab>
</Tabs>



---



<span id="0Hf4mQGP"></span>
## Request parameters

<span id="lAkCFS0L"></span>
### Request body


---



**Filter** `object` `Required`

Filter conditions for search.


Attributes

Filter. **GroupIds** `array`

The Id of the asset group to which the asset belongs.


---



Filter. **GroupType** `string` `Required`

Asset Group type:


* `AIGC`: Digital characters.

* `LivenessFace`: Real\-person portrait



---



Filter. **Name** `string`

The name of the asset group, up to 64 characters.



---



**PageNumber** `integer (i64)`

The page number for pagination, starting from 1.


---



**PageSize** `integer (i64)`

The number of results per page, up to 100.


---



**SortBy** `string`

The field used for sorting. Default is `CreateTime`. Valid values:


* `CreateTime`: Sort by creation time

* `UpdateTime`: Sort by update time



---



**SortOrder** `string`

The sorting order. Default is `Desc`. Valid values:


* `Desc`: Descending

* `Asc`: Ascending



---



**ProjectName** `string`

The project name that the resource belongs to. Default is `default`. If the resource is not in the default project, set the correct project name.

<span id="QCBDgXij"></span>
## Response parameters


---



**TotalCount** `int (i64)`

Return the total count of asset groups.


---



**Items** `array[]`

Array of asset groups that meet the filter conditions.


---



Items. **Id** `string`

The Id of the asset group.


---



Items. **Name** `string`

The name of the asset group, up to 64 characters.


---



Items. **Title** `string`

The title of the asset group.

About to be deprecated; please use the Name parameter directly.


---



Items. **Description** `string`

The description of the asset group, up to 300 characters.


---



Items. **GroupType** `string`

The type of the asset group.

\* `AIGC`: Digital characters

\* `LivenessFace`: Real\-person portrait


---



Items. **ProjectName** `string`

The name of the project to which the resource belongs.


---



Items. **CreateTime** `string`

Created time.


---



Items. **UpdateTime** `string`

Updated time.


---



**PageNumber** `int (i64)`

Return the number of pages.


---



**PageSize** `int (i64)`

The number of search results per page, up to 100.


---



<span id="saOjG5Md"></span>
## Request example

```text
POST /?Action=ListAssetGroups&Version=2024-01-01 HTTP/1.1
Host: ark.ap-southeast-1.byteplusapi.com
Content-Type: application/json
X-Date: 20260328T000000Z
X-Content-Sha256: 287e874e******d653b44d21e
Authorization: HMAC-SHA256 Credential=AKLTYz******/20260328/ap-southeast-1/ark/request, SignedHeaders=content-type;host;x-content-sha256;x-date, Signature=47a7d934******e41085f

{
  "Filter": {
    "Name": "test",
    "GroupType": "AIGC"
  },
  "PageNumber": 1,
  "PageSize": 10,
  "SortBy": "CreateTime",
  "SortOrder": "Desc"
}
```


<span id="qoRws9Sg"></span>
## Response example

```JSON
{
  "ResponseMetadata": {
    "RequestId": "20260328000000000000000000000000",
    "Action": "ListAssetGroups",
    "Version": "2024-01-01",
    "Service": "ark",
    "Region": "ap-southeast-1"
  },
  "Result": {
    "TotalCount": 1,
    "Items": [
      {
        "Id": "group-2026**********-*****",
        "Name": "test",
        "Title": "test",
        "Description": "test",
        "GroupType": "AIGC",
        "ProjectName": "default",
        "CreateTime": "2026-03-28T00:00:00Z",
        "UpdateTime": "2026-03-28T00:00:00Z"
      }
    ],
    "PageNumber": 1,
    "PageSize": 10
  }
}
```




