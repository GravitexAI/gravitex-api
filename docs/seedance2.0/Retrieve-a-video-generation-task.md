`GET https://ark.ap-southeast.bytepluses.com/api/v3/contents/generations/tasks/{id}` [Try](https://api.byteplus.com/api-explorer/?action=GetContentsGenerationsTask&groupName=Video%20Generation%20API&serviceCode=ark&version=2024-01-01)
Retrieves the information of a video generation task.

```mixin-react
return (<Tabs>
<Tabs.TabPane title="Try" key="fq9yXaKY"><RenderMd content={`<APILink link="https://api.byteplus.com/api-explorer/?action=GetContentsGenerationsTask&groupName=Video%20Generation%20API&serviceCode=ark&version=2024-01-01" description="API Explorer 您可以通过 API Explorer 在线发起调用，无需关注签名生成过程，快速获取调用结果。"></APILink>
`}></RenderMd></Tabs.TabPane>
<Tabs.TabPane title="Authentication" key="3vCxpwty"><RenderMd content={`This interface only supports API Key authentication. Obtain a long\\-term API Key on the[ API Key management](https://console.byteplus.com/ark/region:ark+ap-southeast-1/apiKey?apikey=%7B%7D) page.
`}></RenderMd></Tabs.TabPane>
<Tabs.TabPane title="Quick Start" key="AmT6L8NRfI"><RenderMd content={` <span>![图片](https://portal.volccdn.com/obj/volcfe/cloud-universal-doc/upload_b9c82890e851fc10cc31f48f9065abc6.png =20x) </span>[Playground](https://console.byteplus.com/ark/region:ark+ap-southeast-1/experience/vision?projectName=default)<span>![图片](https://portal.volccdn.com/obj/volcfe/cloud-universal-doc/upload_2abecd05ca2779567c6d32f0ddc7874d.png =20x) </span>[Model List](https://docs.byteplus.com/en/docs/ModelArk/1330310)<span>![图片](https://portal.volccdn.com/obj/volcfe/cloud-universal-doc/upload_a5fdd3028d35cc512a10bd71b982b6eb.png =20x) </span>[Model Billing](https://docs.byteplus.com/en/docs/ModelArk/1544106#video-generation)<span>![图片](https://portal.volccdn.com/obj/volcfe/cloud-universal-doc/upload_afbcf38bdec05c05089d5de5c3fd8fc8.png =20x) </span>[API Key](https://console.byteplus.com/ark/region:ark+ap-southeast-1/apiKey?apikey=%7B%7D)
 <span>![图片](https://portal.volccdn.com/obj/volcfe/cloud-universal-doc/upload_57d0bca8e0d122ab1191b40101b5df75.png =20x) </span>[API Tutorial](https://docs.byteplus.com/en/docs/ModelArk/1366799)<span>![图片](https://portal.volccdn.com/obj/volcfe/cloud-universal-doc/upload_f45b5cd5863d1eed3bc3c81b9af54407.png =20x) </span>[API Reference](https://docs.byteplus.com/en/docs/ModelArk/Video_Generation_API)<span>![图片](https://portal.volccdn.com/obj/volcfe/cloud-universal-doc/upload_1609c71a747f84df24be1e6421ce58f0.png =20x) </span>[FAQs](https://docs.byteplus.com/en/docs/ModelArk/1359411)<span>![图片](https://portal.volccdn.com/obj/volcfe/cloud-universal-doc/upload_bef4bc3de3535ee19d0c5d6c37b0ffdd.png =20x) </span>[Model Activation](https://console.byteplus.com/ark/region:ark+ap-southeast-1/openManagement?LLM=%7B%7D&tab=ComputerVision)
`}></RenderMd></Tabs.TabPane></Tabs>);
```


---


<span id="RxN8G2nH"></span>
## Request parameters
> See [Response parameters](#7mi8G8RI)


---


<span id="aN7zwSry"></span>
### Path parameters
**id** `string` %%require%%
The ID of the video generation task to query.

---


<span id="7mi8G8RI"></span>
## 
<span id="7mi8G8RI"></span>
## Response parameters
> See [Request parameters](#RxN8G2nH)


---


**id ** `string`
The ID of the video generation task.

---


**model** `string`
The name and version of the model used by the task (`Model name-Version`)

---


**status** `string`
The status of the task. Valid values:

* `queued`
* `running`
* `cancelled` (Only tasks in the queued state can be canceled)
* `succeeded`
* `failed`
* `expired`


---



* **error** `object / null`

The error information. If the task succeeds, `null` is returned. If the task fails, the error information is returned. For more information, refer to [Error codes](https://docs.byteplus.com/en/docs/ModelArk/1299023).

Attributes

---


error.**code** `string`
The error code.

---


error.**message** `string`
The error message.


---


**created_at** `integer`
The time when the task was created. The value is a Unix timestamp in seconds.

---


**updated_at** `integer`
The time when the task was last updated. The value is a Unix timestamp in seconds.

---


**content** `object`
The output after the video generation task is completed.

Attributes

---


content.**video_url** `string`
The URL of the output video. For security purposes, the output video is deleted after 24 hours. Be sure to save it in time.

---


content.**last_frame_url ** `string`
URL of the last frame of the generated video. Valid for 24 hours. Be sure to save it in time.
Note: If you set `"return_last_frame": true` in the [creating a video generation task](https://docs.byteplus.com/en/docs/ModelArk/1520757) request, the parameter will be returned.


---


**seed** `integer`
The seed value (integer) used for this request.

---


**resolution **  `string` 
The resolution of the generated video.

---


**ratio ** `string`
The width\-to\-height ratio of the generated video.

---


**duration** `integer` 
The length of the generated video in seconds.
**Note: ** Only one of the **duration** and **frames** parameters is returned. If **frames** is not specified in the creating video generation request, **duration** will be returned.

---


**frames** `integer`  
Number of frames for the generated video.
**Note: ** Only one of the **duration** and **frames** parameters will be returned. If **frames** is specified in the creating video generation request, **frames** will be returned.

---


**framespersecond**  `integer` 
The frame rate of the generated video.

---


**generate_audio** `boolean`
Whether the generated video includes audio synchronized with the visuals. This parameter is only supported by seedance 2.0 & 2.0 fast, and seedance 1.5 pro.

* **true**: The model outputs a video with synchronized audio.
* **false**: The model outputs a silent video.


---


items.**safety_identifier==^new^==** `string`
A unique identifier for the end user. If you set this parameter when [creating a video generation task](https://docs.byteplus.com/en/docs/ModelArk/1520757), the API returns it unchanged.

---


**draft** `boolean`
Whether the generated video is a draft video. This parameter is only returned by seedance 1.5 pro.

* **true**: The current output is a draft video.
* **false**: The current output is a standard video.


---


**draft_task_id ** `string`
Draft video task ID. This parameter will be returned when generating an official video based on a draft video.

---


**service_tier ** `string`
The service tier actually used to process the task.

---


**execution_expires_after ** `integer`
The expiration threshold for the task, in seconds.

---


**usage** `object`
The token usage for the request.

Attributes

---


usage.**completion_tokens** `integer`
The number of tokens consumed for the video output by the model.

---


usage.**total_tokens** `integer`
Total tokens for this request. For video models, input tokens are always 0, therefore **total_tokens = completion_tokens**.


