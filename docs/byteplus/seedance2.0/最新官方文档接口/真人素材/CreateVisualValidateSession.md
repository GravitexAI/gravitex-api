`POST https://ark.ap-southeast-1.byteplusapi.com/?Action=CreateVisualValidateSession&Version=2024-01-01`

Initiate the client\-side H5 real\-person verification page link.

After the end user completes real\-person authentication using H5Link and clicks the complete button, the CallbackURL link will open. You can obtain the real\-person authentication result by parsing the resultCode parameter appended to the CallbackURL address.

After the end user passes real\-person authentication ( **resultCode** is **10000** ), you can use the BytedToken returned by the API to query the Asset Group ID corresponding to the end user.


<Tabs>
<Tab zoneid="kf2zVWwWBJ" title="Quick start">
<TabTitle>Quick start</TabTitle>

<span>![图片](https://portal.volccdn.com/obj/volcfe/cloud-universal-doc/upload_57d0bca8e0d122ab1191b40101b5df75.png) </span> [Tutorial](https://docs.byteplus.com/en/docs/ModelArk/2333589)<span>![图片](https://portal.volccdn.com/obj/volcfe/cloud-universal-doc/upload_f45b5cd5863d1eed3bc3c81b9af54407.png) </span> [API list](https://docs.byteplus.com/en/docs/ModelArk/2333602)<span>![图片](https://portal.volccdn.com/obj/volcfe/cloud-universal-doc/upload_bef4bc3de3535ee19d0c5d6c37b0ffdd.png) </span> [Enable Model](https://console.byteplus.com/ark/region:ark+ap-southeast-1/openManagement?LLM=%7B%7D&OpenTokenDrawer=false)


</Tab>
<Tab zoneid="VRB7o8Fyft" title="Authentication">
<TabTitle>Authentication</TabTitle>

To call the Assets API interface, you must use Access Key authentication. For details, refer to [Obtain API access keys (AK/SK)](https://docs.byteplus.com/en/docs/byteplus-platform/docs-creating-an-accesskey).


</Tab>
</Tabs>



---



<span id="bnmEjtft"></span>
## Request parameters

<span id="Qk3uhxAk"></span>
### Request body


---



**CallbackURL** `string` `Required`

Accessible URL for redirection after authentication is completed.


---



**ProjectName** `string`

Name of the project to which the resource belongs. The default value is default (case sensitive).

If the resource is not in the default project, you must enter the correct project name.

<span id="BxyzH12J"></span>
## Response parameters


---



**BytedToken** `string`

The unique credential identifier for this authentication, used to obtain the Group ID created in this session via GetVisualValidateResult.

**Caution** : byted_token is valid for 30 minutes. Please use it promptly (authentication is supported only once; repeated authentication is prohibited).


---



**H5Link** `string`

The link becomes invalid after use. To perform another check, you must call CreateVisualValidateSession again to generate a new link.

**Caution** : You can specify the page language using the **Ing** field in the H5Link link suffix. Currently, Simplified Chinese (zh), English (en), and Traditional Chinese (zh\-Hant) are supported. The default value is zh.


---



**CallbackURL** `string`

Publicly accessible URL for redirection after authentication is completed.

<div data-tips="true" data-tips-type="tip" data-tips-is-title="true">Tip</div>


<div data-tips="true" data-tips-type="tip">After the end user completes real\-person authentication using H5Link and clicks the "Complete" button, the CallbackURL link will open. You can parse the parameters appended to the CallbackURL link to obtain the authentication result.</div>



* <div data-tips="true" data-tips-type="tip">Example of parameter concatenation:</div>


   <div data-tips="true" data-tips-type="tip"><code><CallbackURL>?bytedToken=&resultCode=10000&algorithmBaseRespCode=0&reqMeasureInfoValue=1&verify_type=real_time</code>   </div>
   

* <div data-tips="true" data-tips-type="tip">Detailed suffix parameters:</div>


   * <div data-tips="true" data-tips-type="tip">bytedToken: The unique credential identifier for this authentication, used to obtain the Group ID created in this session via GetVisualValidateResult.</div>


* <div data-tips="true" data-tips-type="tip">resultCode:</div>


   <div data-tips="true" data-tips-type="tip">\* <strong>When resultCode is 10000, the verification is successful</strong> .   </div>
   

   * <div data-tips="true" data-tips-type="tip">algorithmBaseRespCode: Sub\-error code from the server. It is recommended to check this field for the error type when resultCode is a server error code.</div>


   * <div data-tips="true" data-tips-type="tip">reqMeasureInfoValue: Indicates whether this action is billed. The value is 0 or 1. 0 means not billed, 1 means billed. <strong>Currently, real\-person authentication services are free for a limited time.</strong></div>


   * <div data-tips="true" data-tips-type="tip">verify_type: Authentication type. Currently, the fixed value is real_time.</div>




---



<span id="wptnOzit"></span>
## Request example

```text
POST /?Action=CreateAssetGroup&Version=2024-01-01 HTTP/1.1
Host: ark.ap-southeast-1.byteplusapi.com
Content-Type: application/json
X-Date: 20260328T000000Z
X-Content-Sha256: 287e874e******d653b44d21e
Authorization: HMAC-SHA256 Credential=AKLTYz******/20260328/cn-beijing/ark/request, SignedHeaders=content-type;host;x-content-sha256;x-date, Signature=47a7d934******e41085f

{
  "CallbackURL": "https://www.example.com/callback"
}
```


<span id="G8k4eQb1"></span>
## Sample response

```JSON
{
    "BytedToken": "2026070223064318B4CC874F89**************",
    "H5Link": "https://www.byteplus.com/en/liveness-face-manage/authorization?pl=****************-rbyPRi9JTpUHkyRO2VrZBqOLXOdaWs8Hv-t4_U8a-h-VCJ7R3lwyCd3EZcymKKUEn-jOFrwg2qDb-FZXHywdVy67dv8wJ_2qK-JjzUI6LI5MjwboTQXpC0Iu-k1UR1XZkYDB_3ilU22zxBZgWHQgYtkUU59uxvwxoM1QJfNnIhgG6ID6iaQ9CLQSqZgk5UKS1OUNDPgvOFH1siF8VaFVDtDc9-cASGrLfhxr6QFzHoMt5Q7P4C6ibTmYMY1vG2kz1NH8DmMKJoayeGjlwCgRMSw52xwnMU1Pp0OZtBrMXsyRvgykEiYc-hVaYN5AP8kZ2RbWwymDkcS1hjxIvndYdUaeo-09Z8tpGJumDSSKZ7i8jy6TvEi27Ceug9kjNejuMUbHA83a4HANrskZhKOkVKlMlFtlz2UDfSwZe2Qi0ywKxXFSmQ1U8RwLudBPI5IBd-UoFVfAq9ic0_4pEBqnGZewYBSl0rWZZbVYV5u8OGgT9lNBpi43h7FUukCm_pzJaxHmLyMbWBvSLiLaI0ee9SGBwmwRyNR7WpHSZIemUwt9xsVAYyUUWsMtDKyDWxscVylitBTUBJbqd1zA6yXNkd2v6S0zNnLzaUG-tZ_XMvYrFW5s_JDKaV0RxeTWFFi9aU2aKf6MbYkefFh_2gWAqPMhkNSBsuQUo8VnBTU1I1TxUZCZ-VoCWs6ZkTuHtogL-cHtyoAftsxlxSiPlbafjeVxzgw************************&uid=70b97842-****-****-****-df457e66f14f",
    "CallbackURL": "https://www.example.com/callback"
  }
```




