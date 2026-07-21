&nbsp;
`POST https://ark.ap-southeast-1.byteplusapi.com/?Action=CreateAsset&Version=2024-01-01`
Create an Asset within the specified asset group. 
:::warning Warning
This API is asynchronous. Processing may be queued, which can increase ingestion time. Upload\-time SLAs are not guaranteed. 
Higher latency is expected when uploading video assets.
After creation, poll GetAsset and use the Asset only after the status becomes `Active`. If the status becomes `Failed`, processing has failed.
&nbsp;

:::
```mixin-react
return (<Tabs>
<Tabs.TabPane title="Quick Links" key="mLwbCPYO"><RenderMd content={`<span>![图片](https://portal.volccdn.com/obj/volcfe/cloud-universal-doc/upload_57d0bca8e0d122ab1191b40101b5df75.png =20x) </span> [Tutorial](https://docs.byteplus.com/en/docs/ModelArk/2333565) <span>![图片](https://portal.volccdn.com/obj/volcfe/cloud-universal-doc/upload_f45b5cd5863d1eed3bc3c81b9af54407.png =20x) </span> [API List](https://docs.byteplus.com/en/docs/ModelArk/2333601) <span>![图片](https://portal.volccdn.com/obj/volcfe/cloud-universal-doc/upload_bef4bc3de3535ee19d0c5d6c37b0ffdd.png =20x) </span> [Enable Model](https://console.byteplus.com/ark/region:ark+ap-southeast-1/openManagement?LLM=%7B%7D&OpenTokenDrawer=false)
`}></RenderMd></Tabs.TabPane>
<Tabs.TabPane title="Authentication" key="hHI9cNLl"><RenderMd content={`This API only supports Access Key (AK/SK) authentication.
`}></RenderMd></Tabs.TabPane></Tabs>);
```


---


<span id="PrUYA8XZ"></span>
## Request Parameters
<span id="6M6y7aYy"></span>
### Request Body

---


**GroupId** `string` %%require%%
The ID of the Asset Group that the Asset belongs to.

---


**URL** `string` %%require%%
A publicly accessible URL of the Asset.

---


**Name** `string`
The name of the Asset, up to 64 characters. 
**Note**: This field is used only for fuzzy search when calling the ** ** ListAssets API and is not included in model inference. For details on generating videos with assets, see [Generate videos using portrait assets ](https://docs.byteplus.com/en/docs/ModelArk/2333565?lang=en#generate-video-using-portrait-assets)and [FAQ 4](https://docs.byteplus.com/en/docs/ModelArk/2333565?lang=en#faqs).

---


**AssetType** `string` %%require%%
The Asset type. Valid values:

* `Image`: Image
* `Video`: Video
* `Audio`: Audio

:::tip Note
**For image/video/audio assets, only URL upload is supported. Base64 is not supported.** 
**Requirements for a single image**

* Formats: jpeg, png, webp, bmp, tiff, gif, heic/heif
* Aspect ratio (W/H): (0.4, 2.5)
* Width/height (px): (300, 6000)
* Size: < 30 MB per image

**Requirements for a single video**

* Formats: mp4, mov
* Resolution: 480p, 720p
* Duration: [2, 15] seconds
* Dimensions:
   * Aspect ratio (W/H): [0.4, 2.5]
   * Width/height (px): [300, 6000]
   * Total pixels (W×H): [409600, 927408] (e.g., 640×640=409600, 834×1112=927408)
* Size: ≤ 50 MB per video
* FPS: [24, 60]

**Requirements for a single audio**

* Formats: wav, mp3
* Duration: [2, 15] seconds
* Size: ≤ 15 MB per audio


:::
---


**Moderation** `object`
Specifies whether to turn off the Content Pre\-filter review for the current asset. 
:::danger danger

* To ensure this setting takes effect, **first turn off the Secure Mode** on the asset management page ([Model Playground](https://console.byteplus.com/ark/region:ark+ap-southeast-1/experience/vision?modelId=seedance-2-0-260128&tab=GenVideo) ** \> My assets \> Manage assets** or [Model activation](https://console.byteplus.com/ark/region:ark+ap-southeast-1/openManagement?LLM=%7B%7D&advancedActiveKey=model) ** \> Assets library**). 
Otherwise, if the value is set to Skip, the API will return an error.
* **Please note the following impacts**:
   * **Console asset management will be permanently disabled.**  You will no longer be able to view or manage assets in the console. Assets can be managed **only via API**. 
   * You will **no longer be able to authorize** real\-human portrait assets to other users. 
   * This change applies to the **primary account and all sub\-accounts**. If you turn it off, it will be turned off for all. 
   * This operation is **irreversible**. Once disabled, **Secure Mode** cannot be re\-enabled.


:::
**Attribute**
**Strategy ** `string` %%require%%
Specifies the Content Pre\-filter review strategy for the current asset. 
Available values:

* `Default`: Content Pre\-filter review is on for the current asset.
* `Skip`: Skip most non\-baseline content security review policies.


---


**ProjectName** `string`
The name of the project to which the resource belongs. 
The default value is default. If the resource is not in the default project, you must enter the correct project name. For more information about project, see the related [IAM docs](https://docs.byteplus.com/en/docs/byteplus-platform/docs-managing-projects). 
**Note**: The **ProjectName **  must be consistent with the Asset Group to be created. 
<span id="wZDLhNZh"></span>
## Response Parameters

---


**Id** `string`
The ID of the asset. 

---


<span id="eKNScViV"></span>
## Request Example
```text
POST /?Action=CreateAsset&Version=2024-01-01 HTTP/1.1
Host: ark.ap-southeast-1.byteplusapi.com
Content-Type: application/json
X-Date: 20260328T000000Z
X-Content-Sha256: 287e874e******d653b44d21e
Authorization: HMAC-SHA256 Credential=AKLTYz******/20260328/ap-southeast-1/ark/request, SignedHeaders=content-type;host;x-content-sha256;x-date, Signature=47a7d934******e41085f

{
  "GroupId": "group-2026**********-*****",
  "URL": "https://example.com/image.jpg",
  "AssetType": "Image",
  "Moderation": {
      "Strategy": "Skip"
      }
}
```

<span id="2jgXYOYM"></span>
## Response Example
```json
{
  "ResponseMetadata": {
    "RequestId": "20260328000000000000000000000000",
    "Action": "CreateAsset",
    "Version": "2024-01-01",
    "Service": "ark",
    "Region": "ap-southeast-1"
  },
  "Result": {
    "Id": "Asset-2026**********-*****"
  }
}
```



