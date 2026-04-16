The seedance models have excellent semantic understanding capabilities, and can quickly generate high\-quality video clips based on multimodal content such as text, images, videos, and audio input by users. This tutorial introduces the general basic capabilities of the video generation model, and explains how to generate videos with the [Video Generation API](https://docs.byteplus.com/en/docs/ModelArk/Video_Generation_API). To learn about the latest capabilities of the seedance 2.0 series models, see [Seedance 2.0 series tutorial](/docs/ModelArk/2291680).
&nbsp;
<span id="a06d249e"></span>
# Demo
Visit the [model card](https://console.byteplus.com/ark/region:ark+ap-southeast-1/model/detail?Id=dreamina-seedance-2-0) to view more examples.

|Domain |Input: prompt |Input: image, video, audio|Output|\
| | | | |
|---|---|---|---|
|**Multimodal reference**|Fashion outfit\-change short video. Overall pacing, editing rhythm, and transitions follow [Video1], with strong beat sync, fast cuts, and smooth match cuts. The same cat from the images appears sequentially across four scenes: [Image1], [Image2], [Image3], [Image4], changing outfits in each scene. Every shot must have continuous motion, no static frames allowed. The cat performs natural and cute dynamic actions in each scene, such as walking forward, jumping and landing, spinning, raising a paw to pose, wagging its tail, shaking fur, or light running. Movements should feel smooth, lively, and seamlessly connected. |&nbsp;|&nbsp;|\
|> Supports reference images, videos, and audio.| |<video src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/bd0fdc3728bb4a4ba743c67c279ae658" controls></video>|<video src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/0c500b8510944e73a3d827534c04bbc0" controls></video>|\
|| |||\
|<div style="text-align: center">| |&nbsp;|&nbsp;|\
|</div>| |<span>![图片](https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/b7c6b6127c6c4e428ee40a361c1264cd =1200x) </span>| |\
| | | | |
|**Video Editing**|Change all the fruits in [Video 1] into fresh fruits. |&nbsp;|&nbsp;|\
|> Supports replacing the primary subject, adding/removing/modifying objects within the video, and regional repainting/restoration | |<video src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/d246ca2ae8994500b4857c5ee82bdca5" controls></video>|<video src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/18e4df6a4ada4948a4be6127a9ea14bb" controls></video>|\
| | |||\
| | |&nbsp;|&nbsp;|\
| | |<span>![图片](https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/f1dc212609d04a1986fbaa1c67b5cf5c =1280x) </span>| |\
| | |&nbsp;| |\
| | |&nbsp;| |\
| | |&nbsp;| |\
| | | | |
|**Video Extension**|Based on and matching the style of [Video1], add a prequel opening sequence to complete the beginning of the video, showing a caterpillar transforming into a chrysalis. Keep the visual style, tone, and overall aesthetic consistent with the original footage. |&nbsp;|&nbsp;|\
|> Supports extending a video at the beginning or end, and concatenating multiple clips into one coherent video.| |<video src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/fea8a03251044ac48c2cb2c13e0c55aa" controls></video>|<video src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/44cbbacd822041c2818a3cde1c3b6faf" controls></video>|\
|| |||\
|<div style="text-align: center">| |&nbsp;| |\
|</div>| | | |\
| | | | |
|**Audio\-Video Generation** |A female opera performer sings on stage in a clear soprano voice. She begins singing calmly and maintains a steady pace. Her gaze slowly shifts in sequence: first looking into the distance, then lowering to the floor, and finally lifting to look directly into the camera. She sings the full lyric clearly and completely with a gentle, warm smile: “Hold on, let go, give trust, lend heart.” The line must be sung from beginning to end without interruption. The video must not cut or end before the final word is fully delivered. After finishing the last word, she holds her gaze and expression briefly before the scene ends.|<span>![图片](https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/c85ec4a84f4b495694b277e65f09dfc1 =930x) </span> |<video src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/b98110b1363043ee9429802aceb4276e" controls></video>|\
| | | | |
|**Multi\-Reference Image\-to\-Video**|A boy wearing glasses and a blue T\-shirt from [Image 1] and a corgi dog from [Image 2], sitting on the lawn from [Image 3], in 3D cartoon style |<span>![图片](https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/2a0691e5748e414c9a91837684d459d3 =1200x) </span>|<video src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/df7e477f9ef14531abce553c199101f3" controls></video>|\
|&nbsp;| | ||\
|&nbsp;| | | |\
| | | | |
|**First\-and\-Last Frame Video Generation**|Create a 360\-degree orbiting camera shot based on this photo|<span>![图片](https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/f8fc1008f23a4908b7c897e8b7eb87df =1160x) </span> |<video src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/8f6f11cbac5c404cab37a2b3e4a9fe90" controls></video>|\
|&nbsp;| | ||\
| | | | |

<span id="fd30cc1a"></span>
# Getting started
:::tip Description
This section explains in detail how to call the video generation API using different programming languages with code samples.

* If you have no programming experience, we recommend using the [Playground](https://console.byteplus.com/ark/region:ark+ap-southeast-1/experience) in the console. It has a rich template library that allows you to generate the same type of video with one click, so you can start creating quickly without writing any code.
* If you want to quickly experience API calls, we recommend using the [API Explorer](https://api.byteplus.com/api-explorer/?action=CreateContentsGenerationsTasks&groupName=Chat%20API&serviceCode=ark&version=2024-01-01). It has built\-in preset parameters that allow you to initiate API calls with one click. It also supports flexible parameter adjustment (such as setting video watermarks) to meet diverse testing and usage needs.
* If you want to actually start programming, but have difficulties with setting up the development environment, installing dependencies and other issues, see [Getting started](/docs/ModelArk/2291680#e000144b).

:::
Video generation is an asynchronous process:

1. After successfully calling the `POST /contents/generations/tasks` API, the API will return a task ID.
2. You can poll the `GET /contents/generations/tasks/{id}` API until the task status becomes `succeeded`, or use a webhook to automatically receive status changes of the video generation task.
3. After the task is completed, you can download the final generated MP4 file from the content.**video_url** parameter.

<span id="34b10d6d"></span>
## Step 1: Create a video generation task
Create a video generation task via `POST /contents/generations/tasks`.

```mixin-react
return (<Tabs>
<Tabs.TabPane title="Curl" key="whO77KoQNt"><RenderMd content={`\`\`\`Bash
curl https://ark.ap-southeast.bytepluses.com/api/v3/contents/generations/tasks \\
  -H "Content-Type: application/json" \\
  -H "Authorization: Bearer $ARK_API_KEY" \\
  -d '\{
    "model": "dreamina-seedance-2-0-260128",
    "content": [
        \{
            "type": "text",
            "text": "A girl holding a fox, the girl opens her eyes, looks gently at the camera, the fox hugs affectionately, the camera slowly pulls out, the girl’s hair is blown by the wind, and the sound of the wind can be heard"
        \},
        \{
            "type": "image_url",
            "image_url": \{
                "url": "https://ark-doc.tos-ap-southeast-1.bytepluses.com/doc_image/i2v_foxrgirl.png"
            \}
        \}
    ],
    "generate_audio": true,
    "ratio": "adaptive",
\}'
\`\`\`

`}></RenderMd></Tabs.TabPane>
<Tabs.TabPane title="Python" key="T5rAyqAK8N"><RenderMd content={`\`\`\`Python
import os
from byteplussdkarkruntime import Ark
 
# Get API Key: https://console.byteplus.com/ark/region:ark+ap-southeast-1/apikey
client = Ark(api_key=os.environ.get("ARK_API_KEY"))

if __name__ == "__main__":
    print("----- create request -----")
    resp = client.content_generation.tasks.create(
        model="dreamina-seedance-2-0-260128", #Replace with Model ID
        content=[
            \{
                "text": (
                    "A girl holding a fox, the girl opens her eyes, looks gently at the camera, the fox hugs affectionately, the camera slowly pulls out, the girl’s hair is blown by the wind"
                ),
                "type": "text"
            \},
            \{
                "image_url": \{
                    "url": (
                        "https://ark-doc.tos-ap-southeast-1.bytepluses.com/doc_image/i2v_foxrgirl.png"
                    )
                \},
                "type": "image_url"
            \}
        ],
        generate_audio=True,
        ratio="adaptive",
        duration=5,
        watermark=False,
    )
    
    print(resp)
\`\`\`

`}></RenderMd></Tabs.TabPane></Tabs>);
```

After the request is successful, the system will return a task ID.
```JSON
{
  "id": "cgt-2025******-****"
}
```

<span id="a4fa0cc8"></span>
## Step 2: Query video generation task
Using the ID returned from the video generation task, you can query the detailed status and results of the video generation task. This API returns the current status of the task (such as `queued`, `running`, `succeeded`, etc.) and information related to the generated video (such as video download link, resolution, duration, etc.).
:::tip Description
The video generation process may take a long time depending on the model, API load, and video output specifications. To manage this process efficiently, you can request status updates by polling the API (see the SDK examples in the [Basic usage](/docs/ModelArk/2298881#1bf58128) and [Advanced usage](/docs/ModelArk/2298881#2aa4e615) sections for details), or receive notifications via [Use Webhook notification](/docs/ModelArk/2298881#caf01f12).

:::
```mixin-react
return (<Tabs>
<Tabs.TabPane title="Curl" key="eUZcBtt28d"><RenderMd content={`\`\`\`Bash
# Replace cgt-2025**** with the ID acquired from "Create Video Generation Task".

curl https://ark.ap-southeast.bytepluses.com/api/v3/contents/generations/tasks/cgt-2025**** \\
  -H "Content-Type: application/json" \\
  -H "Authorization: Bearer $ARK_API_KEY" 
\`\`\`

`}></RenderMd></Tabs.TabPane>
<Tabs.TabPane title="Python" key="LnyYhZUTNY"><RenderMd content={`\`\`\`Python
import os
from byteplussdkarkruntime import Ark
 
client = Ark(api_key=os.environ.get("ARK_API_KEY"))

if __name__ == "__main__":
    resp = client.content_generation.tasks.get(
        task_id="cgt-2025****",
    )
    print(resp)
\`\`\`

`}></RenderMd></Tabs.TabPane></Tabs>);
```

After the task status changes to succeeded, you can download the final generated video file from the content.**video_url** parameter.
```JSON
{
    "id": "cgt-2025****",
    "model": "dreamina-seedance-2-0-260128",
    "status": "succeeded", 
    "content": {
        // Video download URL (file format is MP4)
        "video_url": "https://ark-content-generation-ap-southeast-1.tos-ap-southeast-1.volces.com/****" 
    },
    "usage": {
        "completion_tokens": 246840,
        "total_tokens": 246840
    },
    "created_at": 1765510475,
    "updated_at": 1765510559,
    "seed": 58944,
    "resolution": "1080p",
    "ratio": "16:9",
    "duration": 5,
    "framespersecond": 24,
    "service_tier": "default",
    "execution_expires_after": 172800
}
```

<span id="e7b4c498"></span>
# Model capabilities
This table lists all capabilities supported by seedance models for comparison and selection. For the latest usage instructions of the seedance 2.0 series models, see [Seedance 2.0 series tutorial](/docs/ModelArk/2291680).

|Model Name | |Seedance 2.0 |Seedance 2.0 fast |Seedance 1.5 pro |Seedance 1.0 pro |Seedance 1.0 pro fast |Seedance 1.0 lite i2v |Seedance 1.0 lite t2v |
|---|---|---|---|---|---|---|---|---|
|Model ID | |dreamina\-seedance\-2\-0\-260128 |dreamina\-seedance\-2\-0\-fast\-260128 |seedance\-1\-5\-pro\-251215 |seedance\-1\-0\-pro\-250528 |seedance\-1\-0\-pro\-fast\-251015 | seedance\-1\-0\-lite\-i2v\-250428 |seedance\-1\-0\-lite\-t2v\-250428 |
|Text\-to\-video | |<div style="text-align: center">|<div style="text-align: center">|<div style="text-align: center">|<div style="text-align: center">|<div style="text-align: center">|<div style="text-align: center">|<div style="text-align: center">|\
| | |<img src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/ee51ce32c1914aed81ff95080bb7db1d" width="20px" /></div>|<img src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/ee51ce32c1914aed81ff95080bb7db1d" width="20px" /></div>|<img src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/ee51ce32c1914aed81ff95080bb7db1d" width="20px" /></div>|<img src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/ee51ce32c1914aed81ff95080bb7db1d" width="20px" /></div>|<img src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/ee51ce32c1914aed81ff95080bb7db1d" width="20px" /></div>|<img src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/ee51ce32c1914aed81ff95080bb7db1d" width="20px" /></div>|<img src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/ee51ce32c1914aed81ff95080bb7db1d" width="20px" /></div>|\
| | | | | | | | | |
|Image\-to\-video (first frame) | |<div style="text-align: center">|<div style="text-align: center">|<div style="text-align: center">|<div style="text-align: center">|<div style="text-align: center">|<div style="text-align: center">|<div style="text-align: center">|\
| | |<img src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/ee51ce32c1914aed81ff95080bb7db1d" width="20px" /></div>|<img src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/ee51ce32c1914aed81ff95080bb7db1d" width="20px" /></div>|<img src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/ee51ce32c1914aed81ff95080bb7db1d" width="20px" /></div>|<img src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/ee51ce32c1914aed81ff95080bb7db1d" width="20px" /></div>|<img src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/ee51ce32c1914aed81ff95080bb7db1d" width="20px" /></div>|<img src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/ee51ce32c1914aed81ff95080bb7db1d" width="20px" /></div>|<img src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/f359753773c94d97885008ca1223c9bc" width="20px" /></div>|\
| | | | | | | | | |
|Image\-to\-video (first and last frame) | |<div style="text-align: center">|<div style="text-align: center">|<div style="text-align: center">|<div style="text-align: center">|<div style="text-align: center">|<div style="text-align: center">|<div style="text-align: center">|\
| | |<img src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/ee51ce32c1914aed81ff95080bb7db1d" width="20px" /></div>|<img src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/ee51ce32c1914aed81ff95080bb7db1d" width="20px" /></div>|<img src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/ee51ce32c1914aed81ff95080bb7db1d" width="20px" /></div>|<img src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/ee51ce32c1914aed81ff95080bb7db1d" width="20px" /></div>|<img src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/f359753773c94d97885008ca1223c9bc" width="20px" /></div>|<img src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/ee51ce32c1914aed81ff95080bb7db1d" width="20px" /></div>|<img src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/f359753773c94d97885008ca1223c9bc" width="20px" /></div>|\
| | | | | | | | | |
|Multimodal reference [New] |Image reference |<div style="text-align: center">|<div style="text-align: center">|<div style="text-align: center">|<div style="text-align: center">|<div style="text-align: center">|<div style="text-align: center">|<div style="text-align: center">|\
| | |<img src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/ee51ce32c1914aed81ff95080bb7db1d" width="20px" /></div>|<img src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/ee51ce32c1914aed81ff95080bb7db1d" width="20px" /></div>|<img src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/f359753773c94d97885008ca1223c9bc" width="20px" /></div>|<img src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/f359753773c94d97885008ca1223c9bc" width="20px" /></div>|<img src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/f359753773c94d97885008ca1223c9bc" width="20px" /></div>|<img src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/ee51ce32c1914aed81ff95080bb7db1d" width="20px" /></div>|<img src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/f359753773c94d97885008ca1223c9bc" width="20px" /></div>|\
| | | | | | | | | |
|^^|Video reference |<div style="text-align: center">|<div style="text-align: center">|<div style="text-align: center">|<div style="text-align: center">|<div style="text-align: center">|<div style="text-align: center">|<div style="text-align: center">|\
| | |<img src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/ee51ce32c1914aed81ff95080bb7db1d" width="20px" /></div>|<img src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/ee51ce32c1914aed81ff95080bb7db1d" width="20px" /></div>|<img src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/f359753773c94d97885008ca1223c9bc" width="20px" /></div>|<img src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/f359753773c94d97885008ca1223c9bc" width="20px" /></div>|<img src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/f359753773c94d97885008ca1223c9bc" width="20px" /></div>|<img src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/f359753773c94d97885008ca1223c9bc" width="20px" /></div>|<img src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/f359753773c94d97885008ca1223c9bc" width="20px" /></div>|\
| | | | | | | | | |
|^^|Combined reference|<div style="text-align: center">|<div style="text-align: center">|<div style="text-align: center">|<div style="text-align: center">|<div style="text-align: center">|<div style="text-align: center">|<div style="text-align: center">|\
| ||<img src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/ee51ce32c1914aed81ff95080bb7db1d" width="20px" /></div>|<img src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/ee51ce32c1914aed81ff95080bb7db1d" width="20px" /></div>|<img src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/f359753773c94d97885008ca1223c9bc" width="20px" /></div>|<img src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/f359753773c94d97885008ca1223c9bc" width="20px" /></div>|<img src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/f359753773c94d97885008ca1223c9bc" width="20px" /></div>|<img src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/f359753773c94d97885008ca1223c9bc" width="20px" /></div>|<img src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/f359753773c94d97885008ca1223c9bc" width="20px" /></div>|\
| |* Image + audio| | | | | | | |\
| |* Image + video| | | | | | | |\
| |* Video + audio| | | | | | | |\
| |* Image + video + audio | | | | | | | |
|Edit video [New] | |<div style="text-align: center">|<div style="text-align: center">|<div style="text-align: center">|<div style="text-align: center">|<div style="text-align: center">|<div style="text-align: center">|<div style="text-align: center">|\
| | |<img src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/ee51ce32c1914aed81ff95080bb7db1d" width="20px" /></div>|<img src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/ee51ce32c1914aed81ff95080bb7db1d" width="20px" /></div>|<img src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/f359753773c94d97885008ca1223c9bc" width="20px" /></div>|<img src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/f359753773c94d97885008ca1223c9bc" width="20px" /></div>|<img src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/f359753773c94d97885008ca1223c9bc" width="20px" /></div>|<img src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/f359753773c94d97885008ca1223c9bc" width="20px" /></div>|<img src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/f359753773c94d97885008ca1223c9bc" width="20px" /></div>|\
| | | | | | | | | |
|Extend video [New] | |<div style="text-align: center">|<div style="text-align: center">|<div style="text-align: center">|<div style="text-align: center">|<div style="text-align: center">|<div style="text-align: center">|<div style="text-align: center">|\
| | |<img src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/ee51ce32c1914aed81ff95080bb7db1d" width="20px" /></div>|<img src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/ee51ce32c1914aed81ff95080bb7db1d" width="20px" /></div>|<img src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/f359753773c94d97885008ca1223c9bc" width="20px" /></div>|<img src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/f359753773c94d97885008ca1223c9bc" width="20px" /></div>|<img src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/f359753773c94d97885008ca1223c9bc" width="20px" /></div>|<img src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/f359753773c94d97885008ca1223c9bc" width="20px" /></div>|<img src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/f359753773c94d97885008ca1223c9bc" width="20px" /></div>|\
| | | | | | | | | |
|Generate video with audio |  |<div style="text-align: center">|<div style="text-align: center">|<div style="text-align: center">|<div style="text-align: center">|<div style="text-align: center">|<div style="text-align: center">|<div style="text-align: center">|\
| | |<img src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/ee51ce32c1914aed81ff95080bb7db1d" width="20px" /></div>|<img src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/ee51ce32c1914aed81ff95080bb7db1d" width="20px" /></div>|<img src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/ee51ce32c1914aed81ff95080bb7db1d" width="20px" /></div>|<img src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/f359753773c94d97885008ca1223c9bc" width="20px" /></div>|<img src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/f359753773c94d97885008ca1223c9bc" width="20px" /></div>|<img src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/f359753773c94d97885008ca1223c9bc" width="20px" /></div>|<img src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/f359753773c94d97885008ca1223c9bc" width="20px" /></div>|\
| | | | | | | | | |
|Online search enhancement [New] |  |<div style="text-align: center">|<div style="text-align: center">|<div style="text-align: center">|<div style="text-align: center">|<div style="text-align: center">|<div style="text-align: center">|<div style="text-align: center">|\
| | |<img src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/ee51ce32c1914aed81ff95080bb7db1d" width="20px" /></div>|<img src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/ee51ce32c1914aed81ff95080bb7db1d" width="20px" /></div>|<img src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/f359753773c94d97885008ca1223c9bc" width="20px" /></div>|<img src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/f359753773c94d97885008ca1223c9bc" width="20px" /></div>|<img src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/f359753773c94d97885008ca1223c9bc" width="20px" /></div>|<img src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/f359753773c94d97885008ca1223c9bc" width="20px" /></div>|<img src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/f359753773c94d97885008ca1223c9bc" width="20px" /></div>|\
| | | | | | | | | |
|Sample mode |  |<div style="text-align: center">|<div style="text-align: center">|<div style="text-align: center">|<div style="text-align: center">|<div style="text-align: center">|<div style="text-align: center">|<div style="text-align: center">|\
| | |<img src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/f359753773c94d97885008ca1223c9bc" width="20px" /></div>|<img src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/f359753773c94d97885008ca1223c9bc" width="20px" /></div>|<img src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/ee51ce32c1914aed81ff95080bb7db1d" width="20px" /></div>|<img src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/f359753773c94d97885008ca1223c9bc" width="20px" /></div>|<img src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/f359753773c94d97885008ca1223c9bc" width="20px" /></div>|<img src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/f359753773c94d97885008ca1223c9bc" width="20px" /></div>|<img src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/f359753773c94d97885008ca1223c9bc" width="20px" /></div>|\
| | | | | | | | | |
|Return video last frame |  |<div style="text-align: center">|<div style="text-align: center">|<div style="text-align: center">|<div style="text-align: center">|<div style="text-align: center">|<div style="text-align: center">|<div style="text-align: center">|\
| | |<img src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/ee51ce32c1914aed81ff95080bb7db1d" width="20px" /></div>|<img src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/ee51ce32c1914aed81ff95080bb7db1d" width="20px" /></div>|<img src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/ee51ce32c1914aed81ff95080bb7db1d" width="20px" /></div>|<img src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/ee51ce32c1914aed81ff95080bb7db1d" width="20px" /></div>|<img src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/ee51ce32c1914aed81ff95080bb7db1d" width="20px" /></div>|<img src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/ee51ce32c1914aed81ff95080bb7db1d" width="20px" /></div>|<img src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/ee51ce32c1914aed81ff95080bb7db1d" width="20px" /></div>|\
| | | | | | | | | |
|Video output specifications |Output resolution |480p, 720p |480p, 720p |480p, 720p, 1080p |480p, 720p, 1080p |480p, 720p, 1080p |480p, 720p, 1080p |480p, 720p, 1080p |
| |Output aspect ratio |<div style="text-align: center">|||||||\
| | |21:9, 16:9, 4:3, 1:1, 3:4, 9:16</div>| | | | | | |\
| | | | | | | | | |
| |Output duration |4–15 seconds |4–15 seconds |4–12 seconds |2–12 seconds |2–12 seconds |2–12 seconds |2–12 seconds |
| |Video output format |mp4 |mp4 |mp4 |mp4 |mp4 |mp4 |mp4 |
|Offline inference |  |<div style="text-align: center">|<div style="text-align: center">|<div style="text-align: center">|<div style="text-align: center">|<div style="text-align: center">|<div style="text-align: center">|<div style="text-align: center">|\
| | |<img src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/f359753773c94d97885008ca1223c9bc" width="20px" /></div>|<img src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/f359753773c94d97885008ca1223c9bc" width="20px" /></div>|<img src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/ee51ce32c1914aed81ff95080bb7db1d" width="20px" /></div>|<img src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/ee51ce32c1914aed81ff95080bb7db1d" width="20px" /></div>|<img src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/ee51ce32c1914aed81ff95080bb7db1d" width="20px" /></div>|<img src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/ee51ce32c1914aed81ff95080bb7db1d" width="20px" /></div>|<img src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/ee51ce32c1914aed81ff95080bb7db1d" width="20px" /></div>|\
| | | | | | | | | |
|Online inference rate limits |RPM |600 |600 |600 |600 |600 |300 |300 |
|  |Concurrency |10 |10 |10 |10 |10 |5 |5 |
|Offline inference rate limits |TPD |\\- |\\- |500 billion |500 billion |500 billion |250 billion |250 billion |

<span id="1bf58128"></span>
# Basic usage
<span id="4e74bcee"></span>
## Text to video
Generates videos based on the prompts entered by users. The output is highly random, and can be used as a source of inspiration.

|Prompt |Output |
|---|---|
|Photorealistic style: Under a clear blue sky, a vast expanse of white daisy fields stretches out. The camera gradually zooms in and finally fixates on a close\-up of a single daisy, with several glistening dewdrops resting on its petals. |<video src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/b847f3e831c244b39f7b4d53d904988f" controls></video>|\
| ||\
| |&nbsp;|\
| | |


```mixin-react
return (<Tabs>
<Tabs.TabPane title="Python" key="BL8EAEHNaO"><RenderMd content={`\`\`\`Python
import os
import time  
# Install SDK:pip install byteplus-python-sdk-v2 
from byteplussdkarkruntime import Ark 

# Make sure that you have stored the API Key in the environment variable ARK_API_KEY
# Initialize the Ark client to read your API Key from an environment variable
client = Ark(
    # This is the default path. You can configure it based on the service location
    base_url="https://ark.ap-southeast.bytepluses.com/api/v3",
    # Get API Key: https://console.byteplus.com/ark/region:ark+ap-southeast-1/apikey
    api_key=os.environ.get("ARK_API_KEY"),
)

if __name__ == "__main__":
    print("----- create request -----")
    create_result = client.content_generation.tasks.create(
        model="dreamina-seedance-2-0-260128", #Replace with Model ID 
        content=[
            \{
                # Combination of text prompt and parameters
                "type": "text",
                "text": "Photorealistic style: Under a clear blue sky, a vast expanse of white daisy fields stretches out. The camera gradually zooms in and finally fixates on a close - up of a single daisy, with several glistening dewdrops resting on its petals."
            \}
        ],
        ratio="16:9",
        duration=5,
        watermark=True,
    )
    print(create_result)

    # Polling query section
    print("----- polling task status -----")
    task_id = create_result.id
    while True:
        get_result = client.content_generation.tasks.get(task_id=task_id)
        status = get_result.status
        if status == "succeeded":
            print("----- task succeeded -----")
            print(get_result)
            break
        elif status == "failed":
            print("----- task failed -----")
            print(f"Error: \{get_result.error\}")
            break
        else:
            print(f"Current status: \{status\}, Retrying after 10 seconds...")
            time.sleep(10)
\`\`\`

`}></RenderMd></Tabs.TabPane></Tabs>);
```

<span id="979b2d28"></span>
## Image\-to\-video—based on the first frame (`including audio`)
By specifying the first frame image of the video, the model can generate video content that is related to and visually coherent with the image.
Seedance 2.0 / seedance 1.5 pro can generate video with audio by setting the **generate_audio** parameter to `true`.

|Prompt |First frame |Output |
|---|---|---|
|A girl holding a fox, the girl opens her eyes and gently looks at the camera, the fox is held gently, the camera slowly pulls back, the girl's hair is blown by the wind, and the sound of the wind can be heard. |<span>![图片](https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/a28ec84ff9fc4287a0d98191020a3218 =230x) </span>|<video src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/f1f7b95a38ee4ee094c724233e4da4f8" controls></video>|\
| | ||\
| | | |


```mixin-react
return (<Tabs>
<Tabs.TabPane title="Python" key="vygt5TCJXm"><RenderMd content={`\`\`\`Python
import os
import time  
# Install SDK:pip install byteplus-python-sdk-v2 
from byteplussdkarkruntime import Ark 

# Make sure that you have stored the API Key in the environment variable ARK_API_KEY
# Initialize the Ark client to read your API Key from an environment variable
client = Ark(
    # This is the default path. You can configure it based on the service location
    base_url="https://ark.ap-southeast.bytepluses.com/api/v3",
    # Get API Key: https://console.byteplus.com/ark/region:ark+ap-southeast-1/apikey
    api_key=os.environ.get("ARK_API_KEY"),
)

if __name__ == "__main__":
    print("----- create request -----")
    create_result = client.content_generation.tasks.create(
        model="dreamina-seedance-2-0-260128", #Replace with Model ID
        content=[
            \{
                # Combination of text prompt and parameters
                "type": "text",
                "text": "A girl holding a fox, the girl opens her eyes, looks gently at the camera, the fox hugs affectionately, the camera slowly pulls out, the girl’s hair is blown by the wind, and the sound of the wind can be heard"             
            \},
            \{
                # The URL of the first frame image
                "type": "image_url",
                "image_url": \{
                    "url": "https://ark-doc.tos-ap-southeast-1.bytepluses.com/doc_image/i2v_foxrgirl.png"
                \}
            \}
        ],
        generate_audio=True,
        ratio="adaptive",
        duration=5,
        watermark=True,
    )
    print(create_result)

    # Polling query section
    print("----- polling task status -----")
    task_id = create_result.id
    while True:
        get_result = client.content_generation.tasks.get(task_id=task_id)
        status = get_result.status
        if status == "succeeded":
            print("----- task succeeded -----")
            print(get_result)
            break
        elif status == "failed":
            print("----- task failed -----")
            print(f"Error: \{get_result.error\}")
            break
        else:
            print(f"Current status: \{status\}, Retrying after 10 seconds...")
            time.sleep(10)
\`\`\`

`}></RenderMd></Tabs.TabPane></Tabs>);
```

<span id="0d55ca07"></span>
## Image\-to\-video – based on the first and last frames (`with audio`)
By specifying the starting and ending images of the video, the model can generate a video that smoothly connects the first and last frames, achieving natural and coherent transition effects between scenes.
For seedance 2.0 / seedance 1.5 pro, you can generate videos with audio by setting the **generate_audio** parameter to `true`.

|Prompt |First frame |Last frame |Output |
|---|---|---|---|
|The girl in the frame says "Cheese" to the camera, with a 360\-degree orbiting camera shot |<span>![图片](https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/649cb2057eae48d6a6eec872d912c75c =160x) </span>|<span>![图片](https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/e39fd8e500a34bbdad50d06659c4ea6b =160x) </span>|<video src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/3aa8c84b8a29408ab29e95992d61c559" controls></video>|\
| | | ||\
| | | | |


```mixin-react
return (<Tabs>
<Tabs.TabPane title="Python" key="bVnbQqPh5Q"><RenderMd content={`\`\`\`Python
import os
import time  
# Install SDK:pip install byteplus-python-sdk-v2 
from byteplussdkarkruntime import Ark 

# Make sure that you have stored the API Key in the environment variable ARK_API_KEY
# Initialize the Ark client to read your API Key from an environment variable
client = Ark(
    # This is the default path. You can configure it based on the service location
    base_url="https://ark.ap-southeast.bytepluses.com/api/v3",
    # Get API Key: https://console.byteplus.com/ark/region:ark+ap-southeast-1/apikey
    api_key=os.environ.get("ARK_API_KEY"),
)  


if __name__ == "__main__": 
    print("----- create request -----") 
    create_result = client.content_generation.tasks.create( 
        model="dreamina-seedance-2-0-260128", #Replace with Model ID
        content=[ 
            \{ 
                # Combination of text prompt and parameters
                "type": "text", 
                "text": "The girl in the frame says “Cheese” to the camera, with a 360-degree orbiting camera shot"
            \}, 
            \{ 
                # The URL of the first frame image
                "type": "image_url", 
                "image_url": \{ 
                    "url": "https://ark-doc.tos-ap-southeast-1.bytepluses.com/doc_image/seepro_first_frame.jpeg"
                \},
                "role": "first_frame"
            \}, 
            \{ 
                # The URL of the last frame image
                "type": "image_url", 
                "image_url": \{ 
                    "url": "https://ark-doc.tos-ap-southeast-1.bytepluses.com/doc_image/seepro_last_frame.jpeg"
                \},
                "role": "last_frame"  
            \} 
        ],
        generate_audio=True,
        ratio="adaptive",
        duration=5,
        watermark=True,
    ) 
    print(create_result)
    
    # Polling query section
    print("----- polling task status -----") 
    task_id = create_result.id 
    while True: 
        get_result = client.content_generation.tasks.get(task_id=task_id) 
        status = get_result.status 
        if status == "succeeded": 
            print("----- task succeeded -----") 
            print(get_result) 
            break 
        elif status == "failed": 
            print("----- task failed -----") 
            print(f"Error: \{get_result.error\}") 
            break 
        else: 
            print(f"Current status: \{status\}, Retrying after 10 seconds...") 
            time.sleep(10)
\`\`\`

`}></RenderMd></Tabs.TabPane></Tabs>);
```

<span id="c5f5f577"></span>
## Image\-to\-video – based on reference images
The model can accurately extract key features of various objects from reference images (1 to 4 input images are supported). Based on these features, it restores details such as the shape, color, and texture of the objects during the video generation process, ensuring that the generated video is consistent with the visual style of the reference images.

|Prompt |Reference image 1 |Reference image 2 |Reference image 3 |Output |
|---|---|---|---|---|
|A boy wearing glasses and a blue T\-shirt from [Image 1] and a corgi dog from [Image 2], sitting on the lawn from [Image 3], in 3D cartoon style. |<span>![图片](https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/2637ac87f1e64bd897bfc651fe7d0386 =90x) </span> |<span>![图片](https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/9450c9444b574112a9f228db9e81cdf4 =90x) </span> |<span>![图片](https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/574b8785f4b740ddaff791655e8633ba =90x) </span> |<video src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/4518edbf0c37482fbf8c28f970900d37" controls></video>|\
| | | | ||\
| | | | | |


```mixin-react
return (<Tabs>
<Tabs.TabPane title="Python" key="JvXznBIhhS"><RenderMd content={`\`\`\`Python
import os
import time  
# Install SDK:pip install byteplus-python-sdk-v2 
from byteplussdkarkruntime import Ark 

# Make sure that you have stored the API Key in the environment variable ARK_API_KEY
# Initialize the Ark client to read your API Key from an environment variable
client = Ark(
    # This is the default path. You can configure it based on the service location
    base_url="https://ark.ap-southeast.bytepluses.com/api/v3",
    # Get API Key: https://console.byteplus.com/ark/region:ark+ap-southeast-1/apikey
    api_key=os.environ.get("ARK_API_KEY"),
)
  
if __name__ == "__main__": 
    print("----- create request -----") 
    try:
        create_result = client.content_generation.tasks.create( 
            model="seedance-1-0-lite-i2v-250428",  #Replace with Model ID 
            content=[ 
                \{ 
                    # Combination of text prompt and parameters
                    "type": "text", 
                    "text": "A boy wearing glasses and a blue T-shirt from [Image 1] and a corgi dog from [Image 2], sitting on the lawn from [Image 3], in 3D cartoon style" 
                \},
                \{ 
                    # The URL of the first reference image
                    # 1-4 reference images need to be provided
                    "type": "image_url", 
                    "image_url": \{ 
                        "url": "https://ark-doc.tos-ap-southeast-1.bytepluses.com/doc_image/seelite_ref_1.png"
                    \},
                    "role": "reference_image"  
                \},
                \{ 
                    # The URL of the second reference image
                    "type": "image_url", 
                    "image_url": \{ 
                        "url": "https://ark-doc.tos-ap-southeast-1.bytepluses.com/doc_image/seelite_ref_2.png" 
                    \},
                    "role": "reference_image"  
                \},
                \{ 
                    # The URL of the third reference image
                    "type": "image_url", 
                    "image_url": \{ 
                        "url": "https://ark-doc.tos-ap-southeast-1.bytepluses.com/doc_image/seelite_ref_3.png" 
                    \},
                    "role": "reference_image"  
                \} 
            ],
            ratio="16:9",
            duration=5,
            watermark=True,
        ) 
        print(create_result) 
    
        # Polling query section
        print("----- polling task status -----") 
        task_id = create_result.id 
        while True: 
            get_result = client.content_generation.tasks.get(task_id=task_id) 
            status = get_result.status 
            if status == "succeeded": 
                print("----- task succeeded -----") 
                print(get_result) 
                break 
            elif status == "failed": 
                print("----- task failed -----") 
                print(f"Error: \{get_result.error\}") 
                break 
            else: 
                print(f"Current status: \{status\}, Retrying after 10 seconds...") 
                time.sleep(10)
    except Exception as e:
        print(f"An error occurred: \{e\}")
\`\`\`

`}></RenderMd></Tabs.TabPane></Tabs>);
```

<span id="68fd42bf"></span>
## Manage video tasks
<span id="360a1a86"></span>
### Query video generation task list
This API supports passing filter parameters to query the list of video generation tasks that meet the specified conditions.

```mixin-react
return (<Tabs>
<Tabs.TabPane title="Curl" key="Cf25t9Sq3R"><RenderMd content={`\`\`\`Bash
curl https://ark.ap-southeast.bytepluses.com/api/v3/contents/generations/tasks?page_size=2&filter.status=succeeded& \\
  -H "Content-Type: application/json" \\
  -H "Authorization: Bearer $ARK_API_KEY" 
\`\`\`

`}></RenderMd></Tabs.TabPane>
<Tabs.TabPane title="Python" key="lu7DrikeLd"><RenderMd content={`\`\`\`Python
import os
from byteplussdkarkruntime import Ark

client = Ark(api_key=os.environ.get("ARK_API_KEY"))

if __name__ == "__main__":
    resp = client.content_generation.tasks.list(
        page_size=3,
        status="succeeded",
    )
    print(resp)
\`\`\`

`}></RenderMd></Tabs.TabPane></Tabs>);
```

<span id="64914c89"></span>
### Delete or cancel video generation tasks
Cancel queued video generation tasks, or delete video generation task records.

```mixin-react
return (<Tabs>
<Tabs.TabPane title="Curl" key="GjuckvdMEs"><RenderMd content={`\`\`\`Bash
# Replace cgt-2025**** with the ID acquired from "Create Video Generation Task".

curl -X DELETE https://ark.ap-southeast.bytepluses.com/api/v3/contents/generations/tasks/cgt-2025**** \\
  -H "Content-Type: application/json" \\
  -H "Authorization: Bearer $ARK_API_KEY" 
\`\`\`

`}></RenderMd></Tabs.TabPane>
<Tabs.TabPane title="Python" key="Do67DdCK7o"><RenderMd content={`\`\`\`Python
import os
from byteplussdkarkruntime import Ark

client = Ark(api_key=os.environ.get("ARK_API_KEY"))

if __name__ == "__main__":
    try:
        client.content_generation.tasks.delete(
            task_id="cgt-2025****",
        )
    except Exception as e:
        print(f"failed to delete task: \{e\}")
\`\`\`

`}></RenderMd></Tabs.TabPane></Tabs>);
```

<span id="9fe4cce0"></span>
## Set video output specifications [New]
You can control the video output specifications via the **resolution, ratio, duration, frames, seed, camera_fixed, watermark** parameters.
:::warning Caution
Supported parameters and their options vary with models. See the table below for details. If the input parameters or options are not supported by the selected model, the parameter will be ignored or an error will be thrown.

* **New method:**  Pass parameters directly in the request body. This method uses **strong validation**. If parameters are filled incorrectly, the model will return an error.
* **Legacy method:**  Append \-\-[parameters] after the text prompt. This method uses **weak validation**. If parameters are filled incorrectly, the model will automatically use the default value and will not report an error.
:::
* **New method (recommended): Pass parameters directly ** **in** ** the request body**
   ```JSON
   ...
      // Strongly recommended
       "model": "dreamina-seedance-2-0-260128",
       "content": [
           {
               "type": "text",
               "text": "The kitten is yawning at the camera."
           }
       ],
       // All parameters must be written in full; abbreviations are not supported
       "resolution": "720p",
       "ratio":"16:9",
       "duration": 5,
       // "frames": 29, Either duration or frames is required
       "seed": 11,
       "camera_fixed": false,
   ...
   ```
   


&nbsp;
* **Legacy method: Append \-\-[parameters] after the text prompt**
   ```JSON
   ...
   // Specify the aspect ratio of the generated video as 16:9, duration as 5 seconds, resolution as 720p, seed as 11, and include a watermark. The camera is not fixed.
   "content": [
           {
               "type": "text",
               "text": "The kitten is yawning at the camera --rs 720p --rt 16:9 --dur 5 --seed 11 --cf false --wm true"
               // "text": "The kitten is yawning at the camera --resolution 720p --ratio 16:9 --duration 5 --seed 11 --camerafixed false --watermark true"
           }
    ]
    ...
   ```
   


| |[dreamina-seedance-2-0](https://console.byteplus.com/ark/region:ark+ap-southeast-1/model/detail?Id=dreamina-seedance-2-0)|[seedance-1-5-pro](https://console.byteplus.com/ark/region:ark+ap-southeast-1/model/detail?Id=seedance-1-5-pro) |[seedance-1-0-pro](https://console.byteplus.com/ark/region:ark+ap-southeast-1/model/detail?Id=seedance-1-0-pro)|[seedance-1-0-lite-t2v](https://console.byteplus.com/ark/region:ark+ap-southeast-1/model/detail?Id=seedance-1-0-lite-t2v)|\
| |[dreamina-seedance-2-0-fast](https://console.byteplus.com/ark/region:ark+ap-southeast-1/model/detail?Id=dreamina-seedance-2-0-fast) | |[seedance-1-0-pro-fast](https://console.byteplus.com/ark/region:ark+ap-southeast-1/model/detail?Id=seedance-1-0-pro-fast) |[seedance-1-0-lite-i2v](https://console.byteplus.com/ark/region:ark+ap-southeast-1/model/detail?Id=seedance-1-0-lite-i2v) |
|---|---|---|---|---|
|resolution|* 480p|* 480p|* 480p|* 480p|\
|Resolution |* 720p |* 720p|* 720p|* 720p|\
| | |*  1080p |*  1080p |* 1080p`Reference image scenario not supported` |
|ratio|* 16:9|* 16:9|* 16:9|* 16:9|\
|Aspect ratio |* 4:3|* 4:3|* 4:3|* 4:3|\
| |* 1:1|* 1:1|* 1:1|* 1:1|\
| |* 3:4|* 3:4|* 3:4|* 3:4|\
| |* 9:16|* 9:16|* 9:16|* 9:16|\
| |* 21:9|* 21:9|* 21:9|* 21:9|\
| |* adaptive|* adaptive|* adaptive`Text-to-video scenario not supported`|* adaptive`Reference image and text-to-video scenarios not supported`|\
| |||||\
| |||<div style="text-align: center">||\
| |---|---||---|\
| |||---||\
| |||||\
| |<div style="text-align: center">|<div style="text-align: center">|</div>|Pixel dimensions for each aspect ratio at 480p are as follows:|\
| |Pixel dimensions for each aspect ratio at 480p are as follows:</div>|Pixel dimensions for each aspect ratio at 480p are as follows:</div>|||\
| |||<div style="text-align: center">|* `16:9`: 864×480|\
| |||Pixel dimensions for each aspect ratio at 480p are as follows:</div>|* `4:3`: 736×544|\
| |* <div style="text-align: center">|* <div style="text-align: center">||* `1:1`: 640×640|\
| |<code>16:9</code>: 864×496</div>|<code>16:9</code>: 864×496</div>||* `3:4`: 544×736|\
| |||* <div style="text-align: center">|* `9:16`: 480×864|\
| |* <div style="text-align: center">|* <div style="text-align: center">|<code>16:9</code>: 864×480</div>|* `21:9`: 960×416|\
| |<code>4:3</code>: 752×560</div>|<code>4:3</code>: 752×560</div>|||\
| |||* <div style="text-align: center">||\
| |* <div style="text-align: center">|* <div style="text-align: center">|<code>4:3</code>: 736×544</div>|---|\
| |<code>1:1</code>: 640×640</div>|<code>1:1</code>: 640×640</div>|||\
| |||* <div style="text-align: center">||\
| |* <div style="text-align: center">|* <div style="text-align: center">|<code>1:1</code>: 640×640</div>|Pixel dimensions for each aspect ratio at 720p are as follows:|\
| |<code>3:4</code>: 560×752</div>|<code>3:4</code>: 560×752</div>|||\
| |||* <div style="text-align: center">|* `16:9`: 1248×704|\
| |* <div style="text-align: center">|* <div style="text-align: center">|<code>3:4</code>: 544×736</div>|* `4:3`: 1120×832|\
| |<code>9:16</code>: 496×864</div>|<code>9:16</code>: 496×864</div>||* `1:1`: 960×960|\
| |||* <div style="text-align: center">|* `3:4`: 832×1120|\
| |* <div style="text-align: center">|* <div style="text-align: center">|<code>9:16</code>: 480×864</div>|* `9:16`: 704×1248|\
| |<code>21:9</code>: 992×432</div>|<code>21:9</code>: 992×432</div>||* `21:9`: 1504×640|\
| |||* <div style="text-align: center">||\
| |||<code>21:9</code>: 960×416</div>||\
| |<div style="text-align: center">|<div style="text-align: center">||---|\
| |||||\
| |---|---|<div style="text-align: center">||\
| ||||Pixel dimensions for each aspect ratio at 1080p are as follows (`Reference image scenario not supported`):|\
| |</div>|</div>|---||\
| ||||* `16:9`: 1920×1088|\
| |<div style="text-align: center">|<div style="text-align: center">|</div>|* `4:3`: 1664×1248|\
| |Pixel dimensions for each aspect ratio at 720p are as follows:</div>|Pixel dimensions for each aspect ratio at 720p are as follows:</div>||* `1:1`: 1440×1440|\
| |||<div style="text-align: center">|* `3:4`: 1248×1664|\
| |||Pixel dimensions for each aspect ratio at 720p are as follows:</div>|* `9:16`: 1088×1920|\
| |* <div style="text-align: center">|* <div style="text-align: center">||* `21:9`: 2176×928 |\
| |<code>16:9</code>: 1280×720</div>|<code>16:9</code>: 1280×720</div>|| |\
| |||* <div style="text-align: center">| |\
| |* <div style="text-align: center">|* <div style="text-align: center">|<code>16:9</code>: 1248×704</div>| |\
| |<code>4:3</code>: 1112×834</div>|<code>4:3</code>: 1112×834</div>|| |\
| |||* <div style="text-align: center">| |\
| |* <div style="text-align: center">|* <div style="text-align: center">|<code>4:3</code>: 1120×832</div>| |\
| |<code>1:1</code>: 960×960</div>|<code>1:1</code>: 960×960</div>|| |\
| |||* <div style="text-align: center">| |\
| |* <div style="text-align: center">|* <div style="text-align: center">|<code>1:1</code>: 960×960</div>| |\
| |<code>3:4</code>: 834×1112</div>|<code>3:4</code>: 834×1112</div>|| |\
| |||* <div style="text-align: center">| |\
| |* <div style="text-align: center">|* <div style="text-align: center">|<code>3:4</code>: 832×1120</div>| |\
| |<code>9:16</code>: 720×1280</div>|<code>9:16</code>: 720×1280</div>|| |\
| |||* <div style="text-align: center">| |\
| |* <div style="text-align: center">|* <div style="text-align: center">|<code>9:16</code>: 704×1248</div>| |\
| |<code>21:9</code>: 1470×630</div>|<code>21:9</code>: 1470×630</div>|| |\
| |||* <div style="text-align: center">| |\
| |||<code>21:9</code>: 1504×640</div>| |\
| |||| |\
| |---|---|| |\
| |||<div style="text-align: center">| |\
| |||| |\
| |<div style="text-align: center">|<div style="text-align: center">|---| |\
| |Pixel dimensions for each aspect ratio at 1080p are as follows:</div>|Pixel dimensions for each aspect ratio at 1080p are as follows:</div>|| |\
| |||</div>| |\
| |||| |\
| |* <div style="text-align: center">|* <div style="text-align: center">|<div style="text-align: center">| |\
| |<code>16:9</code>: 1920×1080</div>|<code>16:9</code>: 1920×1080</div>|Pixel dimensions for each aspect ratio at 1080p are as follows:</div>| |\
| |||| |\
| |* <div style="text-align: center">|* <div style="text-align: center">|| |\
| |<code>4:3</code>: 1664×1248</div>|<code>4:3</code>: 1664×1248</div>|* <div style="text-align: center">| |\
| |||<code>16:9</code>: 1920×1088</div>| |\
| |* <div style="text-align: center">|* <div style="text-align: center">|| |\
| |<code>1:1</code>: 1440×1440</div>|<code>1:1</code>: 1440×1440</div>|* <div style="text-align: center">| |\
| |||<code>4:3</code>: 1664×1248</div>| |\
| |* <div style="text-align: center">|* <div style="text-align: center">|| |\
| |<code>3:4</code>: 1248×1664</div>|<code>3:4</code>: 1248×1664</div>|* <div style="text-align: center">| |\
| |||<code>1:1</code>: 1440×1440</div>| |\
| |* <div style="text-align: center">|* <div style="text-align: center">|| |\
| |<code>9:16</code>: 1080×1920</div>|<code>9:16</code>: 1080×1920</div>|* <div style="text-align: center">| |\
| |||<code>3:4</code>: 1248×1664</div>| |\
| |* <div style="text-align: center">|* <div style="text-align: center">|| |\
| |<code>21:9</code>: 2206×946</div>|<code>21:9</code>: 2206×946</div>|* <div style="text-align: center">| |\
| | | |<code>9:16</code>: 1088×1920</div>| |\
| | | || |\
| | | |* <div style="text-align: center">| |\
| | | |<code>21:9</code>: 2176×928</div>| |\
| | | | | |
|duration|<div style="text-align: center">|<div style="text-align: center">|<div style="text-align: center">|<div style="text-align: center">|\
|Output video duration (seconds) |4–15 seconds</div>|4–12 seconds</div>|2–12 seconds</div>|2–12 seconds</div>|\
| | | | | |
|frames|<div style="text-align: center">|<div style="text-align: center">|<div style="text-align: center">|<div style="text-align: center">|\
|Generated video frame count |<img src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/f359753773c94d97885008ca1223c9bc" width="20px" /></div>|<img src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/f359753773c94d97885008ca1223c9bc" width="20px" /></div>|All integer values in the range [29, 289] that fit the format 25 + 4n are supported, where n is a positive integer.</div>|All integer values in the range [29, 289] that fit the format 25 + 4n are supported, where n is a positive integer.</div>|\
| | | | | |
|seed|<div style="text-align: center">|<div style="text-align: center">|<div style="text-align: center">|<div style="text-align: center">|\
|Seed integer |<img src="https://p9-arcosite.byteimg.com/tos-cn-i-goo7wpa0wc/ee51ce32c1914aed81ff95080bb7db1d~tplv-goo7wpa0wc-image.image" width="20px" /></div>|<img src="https://p9-arcosite.byteimg.com/tos-cn-i-goo7wpa0wc/ee51ce32c1914aed81ff95080bb7db1d~tplv-goo7wpa0wc-image.image" width="20px" /></div>|<img src="https://p9-arcosite.byteimg.com/tos-cn-i-goo7wpa0wc/ee51ce32c1914aed81ff95080bb7db1d~tplv-goo7wpa0wc-image.image" width="20px" /></div>|<img src="https://p9-arcosite.byteimg.com/tos-cn-i-goo7wpa0wc/ee51ce32c1914aed81ff95080bb7db1d~tplv-goo7wpa0wc-image.image" width="20px" /></div>|\
| | | | | |
|camera_fixed|<div style="text-align: center">|<div style="text-align: center">|<div style="text-align: center">|<div style="text-align: center">|\
|Whether the camera is fixed |<img src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/f359753773c94d97885008ca1223c9bc" width="20px" /></div>|<img src="https://p9-arcosite.byteimg.com/tos-cn-i-goo7wpa0wc/ee51ce32c1914aed81ff95080bb7db1d~tplv-goo7wpa0wc-image.image" width="20px" /></div>|<img src="https://p9-arcosite.byteimg.com/tos-cn-i-goo7wpa0wc/ee51ce32c1914aed81ff95080bb7db1d~tplv-goo7wpa0wc-image.image" width="20px" /></div>|<img src="https://p9-arcosite.byteimg.com/tos-cn-i-goo7wpa0wc/ee51ce32c1914aed81ff95080bb7db1d~tplv-goo7wpa0wc-image.image" width="20px" /></div>|\
| | | | ||\
| | | | |<div style="text-align: center">|\
| | | | |<code>Reference image scenario not supported</code></div>|\
| | | | | |

<span id="44236b6a"></span>
## Prompt tips

* **Prompt = subject + motion, background + motion, camera + motion ...** 
* Describe what you want in concise and accurate natural language.
* If you have relatively clear expectations, it is recommended to first use an image generation model to generate images that meet your expectations, then generate video clips based on those images.
* The output of text\-to\-video generation is highly random, and can be used as a source of inspiration.
* When using image\-to\-video generation, please try to upload high\-definition, high\-quality images. The quality of the uploaded image has a significant impact on the image\-to\-video result.
* When the generated video does not meet expectations, it is recommended to modify the prompt, replace abstract descriptions with concrete ones, remove unimportant parts, and place important content at the front.

<span id="2aa4e615"></span>
# Advanced usage
<span id="a0badaae"></span>
## Offline inference
For scenarios with low inference latency sensitivity (e.g., hour\-level response), it is recommended to set **service_tier** to `flex` to switch to offline inference mode with one click. The cost is only 50% of that of online inference, which significantly reduces costs.
Note that you should set an appropriate timeout period according to your business needs. Tasks will be automatically terminated after the timeout period expires.

```mixin-react
return (<Tabs>
<Tabs.TabPane title="Python" key="T3oxrBAS7I"><RenderMd content={`\`\`\`Python
import os
import time  
# Install SDK:pip install byteplus-python-sdk-v2 
from byteplussdkarkruntime import Ark 

# Make sure that you have stored the API Key in the environment variable ARK_API_KEY
# Initialize the Ark client to read your API Key from an environment variable
client = Ark(
    # This is the default path. You can configure it based on the service location
    base_url="https://ark.ap-southeast.bytepluses.com/api/v3",
    # Get API Key: https://console.byteplus.com/ark/region:ark+ap-southeast-1/apikey
    api_key=os.environ.get("ARK_API_KEY"),
)

if __name__ == "__main__":
    print("----- create request -----")
    create_result = client.content_generation.tasks.create(
        model="seedance-1-5-pro-251215", #Replace with Model ID
        content=[
            \{
                # Combination of text prompt and parameters
                "type": "text",
                "text": "A girl holding a fox, the girl opens her eyes, looks gently at the camera, the fox hugs affectionately, the camera slowly pulls out, the girl’s hair is blown by the wind"             
            \},
            \{
                # The URL of the first frame image
                "type": "image_url",
                "image_url": \{
                    "url": "https://ark-doc.tos-ap-southeast-1.bytepluses.com/doc_image/i2v_foxrgirl.png" 
                \}
            \}
        ],
        ratio="adaptive",
        duration=5,
        watermark=False,
        service_tier="flex",
        execution_expires_after=172800,
    )
    print(create_result)

    # Polling query section
    print("----- polling task status -----")
    task_id = create_result.id
    while True:
        get_result = client.content_generation.tasks.get(task_id=task_id)
        status = get_result.status
        if status == "succeeded":
            print("----- task succeeded -----")
            print(get_result)
            break
        elif status == "failed":
            print("----- task failed -----")
            print(f"Error: \{get_result.error\}")
            break
        else:
            print(f"Current status: \{status\}, Retrying after 60 seconds...")
            time.sleep(60)
\`\`\`

`}></RenderMd></Tabs.TabPane></Tabs>);
```

<span id="5acd28c8"></span>
## Draft mode
Obtaining a production\-grade video that meets expectations usually requires multiple attempts, which is time\-consuming and labor\-intensive. The draft mode is an intermediate product visualization feature launched by ModelArk. After enabling this feature, a preview video will be generated to help you **verify at low cost** whether key elements such as the scene structure, shot scheduling, subject actions of the generated video and prompt intent meet expectations, so as to quickly adjust the direction. After confirming that it meets expectations, generate the final high\-quality video based on the draft video.

|Input |Draft video |Final video |
|---|---|---|
|<span>![图片](https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/ebb5217645b04cfc94209a6f7d36a523 =240x) </span>|<video src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/7c190b3a0ed34b29bc1192acbce2f4d2" controls></video>|<video src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/a82cd582a5d54f34a8adec10f2815081" controls></video>|\
|> Prompt: A girl holding a fox, the girl opens her eyes, looks gently at the camera, the fox hugs affectionately, the camera slowly pulls out, the girl’s hair is blown by the wind, and the sound of the wind can be heard. |||\
| |>  Generate a preview video to verify the result at low cost. |> Reuse the **model, prompt, input image, seed value, audio settings, video aspect ratio, video duration, etc.**  used for the draft video to generate the final video, to ensure that the key elements of the video are consistent. |

This feature can be used in two steps:
<span id="13ae3900"></span>
### Step 1: Generate a draft video

1. Set `"draft": true` and call the `POST /contents/generations/tasks` API to create a draft video generation task.
2. Call the `GET /contents/generations/tasks/{id}` API to query the generation status and result, download the draft video, and confirm whether it meets expectations.
   :::tip \*Description
   * Only seedance 1.5 pro supports this feature.
   * Only 480p resolution is supported (using other resolutions will cause an error). The return last frame feature and offline inference feature are not supported.
   * The unit price of tokens for draft videos remains the same, but fewer tokens are consumed.`Draft video token usage = Normal video token usage × Conversion coefficient`. Take seedance 1.5 pro as an example, the conversion coefficient for videos with audio is 0.6, so the cost of generating a draft video with audio is 0.6 times that of a normal video, which significantly reduces the cost.
   
   :::
   ```mixin-react
   return (<Tabs>
   <Tabs.TabPane title="Curl" key="Z4QeGC8Rj3"><RenderMd content={`1. Create a draft video generation task.
   
   \`\`\`Bash
   curl https://ark.ap-southeast.bytepluses.com/api/v3/contents/generations/tasks \\
     -H "Content-Type: application/json" \\
     -H "Authorization: Bearer $ARK_API_KEY" \\
     -d '\{
       "model": "seedance-1-5-pro-251215",
       "content": [
           \{
               "type": "text",
               "text": "A girl holding a fox, the girl opens her eyes, looks gently at the camera, the fox hugs affectionately, the camera slowly pulls out, the girl’s hair is blown by the wind"
           \},
           \{
               "type": "image_url",
               "image_url": \{
                   "url": "https://ark-doc.tos-ap-southeast-1.bytepluses.com/doc_image/i2v_foxrgirl.png"
               \}
           \}
       ],
       "seed": 20, 
       "duration": 6, 
       "draft": true
   \}'
   \`\`\`
   
   After the request succeeds, the system will return a task ID. This ID is the draft video task ID, which will be used to generate the final video later.
   
   2. Use the draft video task ID to query the generation status and result.
   
   \`\`\`Bash
   # Replace cgt-2026****-pzjqb with the ID acquired from last step
   
   curl https://ark.ap-southeast.bytepluses.com/api/v3/contents/generations/tasks/cgt-2026****-pzjqb \\
     -H "Content-Type: application/json" \\
     -H "Authorization: Bearer $ARK_API_KEY" 
   \`\`\`
   
   After the task status changes to succeeded, you can download the final generated draft video file from the content.**video_url** parameter, and check whether the result meets expectations. If it does not meet expectations, you can readjust the parameters and create a draft video generation task again. After confirming that the result of the draft video meets expectations, you can generate the final video according to the subsequent steps.
   `}></RenderMd></Tabs.TabPane>
   <Tabs.TabPane title="Python" key="mc6JfmVN6g"><RenderMd content={`1. Create a draft video task and poll the task status;
   2. After the task status changes to \`succeeded\`, you can download the final generated draft video file from the content.**video_url** parameter, and check whether the result meets expectations. If it does not meet expectations, you can readjust the parameters and create a draft video generation task again. After confirming that the result of the draft video meets expectations, you can generate the final video according to the subsequent steps.
   
   \`\`\`Python
   import os
   import time
   # Install SDK:pip install byteplus-python-sdk-v2 
   from byteplussdkarkruntime import Ark 
   
   # Make sure that you have stored the API Key in the environment variable ARK_API_KEY
   # Initialize the Ark client to read your API Key from an environment variable
   client = Ark(
       # This is the default path. You can configure it based on the service location
       base_url="https://ark.ap-southeast.bytepluses.com/api/v3",
       # Get API Key: https://console.byteplus.com/ark/region:ark+ap-southeast-1/apikey
       api_key=os.environ.get("ARK_API_KEY"),
   )
   
   if __name__ == "__main__":
       print("----- create request -----")
       create_result = client.content_generation.tasks.create(
           model="seedance-1-5-pro-251215", #Replace with Model ID
           content=[
               \{
                   # Combination of text prompt and parameters
                   "type": "text",
                   "text": "A girl holding a fox, the girl opens her eyes, looks gently at the camera, the fox hugs affectionately, the camera slowly pulls out, the girl’s hair is blown by the wind"         
               \},
               \{
                   # The URL of the first frame image
                   "type": "image_url",
                   "image_url": \{
                       "url": "https://ark-doc.tos-ap-southeast-1.bytepluses.com/doc_image/i2v_foxrgirl.png"
                   \}
               \}
           ],
           seed= 20,
           duration= 6,
           draft= True,
       )
       print(create_result)
       
       # Polling query section
       print("----- polling task status -----")
       task_id = create_result.id
       while True:
           get_result = client.content_generation.tasks.get(task_id=task_id)
           status = get_result.status
           if status == "succeeded":
               print("----- task succeeded -----")
               print(get_result)
               break
           elif status == "failed":
               print("----- task failed -----")
               print(f"Error: \{get_result.error\}")
               break
           else:
               print(f"Current status: \{status\}, Retrying after 10 seconds...")
               time.sleep(10)
   \`\`\`
   
   `}></RenderMd></Tabs.TabPane></Tabs>);
   ```
   

<span id="015173ef"></span>
### Step 2: Generate the final video based on the draft video
If you confirm that the draft video meets your expectations, you can call the `POST /contents/generations/tasks` API again based on the draft video task ID returned in Step 1 to generate the final video.
:::tip Description

* ModelArk will automatically reuse the user input used for the draft video (**model**, **content.text**, **content.image_url, generate_audio, seed, ratio, duration, camera_fixed**) to generate the final video.
* Other parameters can be specified manually. If not specified, the default values of this model will be used. For example: You can specify the resolution of the final video, whether to include a watermark, whether to use offline inference, whether to return the last frame, etc.
* Generating the final video from a draft video is a normal inference process, and will be billed based on the number of tokens consumed for normal video generation.
* The draft video task ID is valid for 7 days (calculated from the **created at** timestamp). After expiration, you can no longer use this draft video to generate the final video.


:::
```mixin-react
return (<Tabs>
<Tabs.TabPane title="Curl" key="bDUTU43Hcx"><RenderMd content={`1. Create a video generation task based on the \`content.draft_task.id\`.

\`\`\`Bash
curl https://ark.ap-southeast.bytepluses.com/api/v3/contents/generations/tasks \\
  -H "Content-Type: application/json" \\
  -H "Authorization: Bearer $ARK_API_KEY" \\
  -d '\{
    "model": "seedance-1-5-pro-251215",
    "content": [
        \{
            "type": "draft_task",
            "draft_task": \{"id": "cgt-2026****-pzjqb"\}
        \}
    ],
      "watermark": false,
      "resolution": "720p",
      "return_last_frame": true,
      "service_tier": "default"
  \}'  
\`\`\`

After the request succeeds, the system will return a task ID.

&nbsp;
2. Use the video task ID to query the generation status and results.

\`\`\`Bash
# Replace cgt-2026****-bn6zj with the ID acquired from last step

curl https://ark.ap-southeast.bytepluses.com/api/v3/contents/generations/tasks/cgt-2026****-bn6zj \\
  -H "Content-Type: application/json" \\
  -H "Authorization: Bearer $ARK_API_KEY" 
\`\`\`

After the task status changes to succeeded, you can download the generated video file from the content.**video_url** parameter.
`}></RenderMd></Tabs.TabPane>
<Tabs.TabPane title="Python" key="zFjaXGm5z0"><RenderMd content={`1. Create a video generation task based on \`content.draft_task.id\` (this ID is obtained from the response of Step 1) and poll to get the task status;
2. After the task status changes to \`succeeded\`, you can download the generated video file from the content.**video_url** parameter.

\`\`\`Python
import os
import time
# Install SDK:pip install byteplus-python-sdk-v2 
from byteplussdkarkruntime import Ark 

# Make sure that you have stored the API Key in the environment variable ARK_API_KEY
# Initialize the Ark client to read your API Key from an environment variable
client = Ark(
    # This is the default path. You can configure it based on the service location
    base_url="https://ark.ap-southeast.bytepluses.com/api/v3",
    # Get API Key: https://console.byteplus.com/ark/region:ark+ap-southeast-1/apikey
    api_key=os.environ.get("ARK_API_KEY"),
)

if __name__ == "__main__":
    print("----- create request -----")
    create_result = client.content_generation.tasks.create(
        model="seedance-1-5-pro-251215", #Replace with Model ID
        content=[
            \{
                "type": "draft_task",
                "draft_task": \{
                    "id": "cgt-2026****-pzjqb"
                \}
            \}
        ],
        watermark= False,
        resolution= "720p",
        return_last_frame= True,
        service_tier= "default",
    )
    print(create_result)
    
    # Polling query section
    print("----- polling task status -----")
    task_id = create_result.id
    while True:
        get_result = client.content_generation.tasks.get(task_id=task_id)
        status = get_result.status
        if status == "succeeded":
            print("----- task succeeded -----")
            print(get_result)
            break
        elif status == "failed":
            print("----- task failed -----")
            print(f"Error: \{get_result.error\}")
            break
        else:
            print(f"Current status: \{status\}, Retrying after 10 seconds...")
            time.sleep(10)
\`\`\`

`}></RenderMd></Tabs.TabPane></Tabs>);
```

<span id="141cf7fa"></span>
## Generate multiple consecutive videos
Use the last frame of the previously generated video as the first frame of the next video task, and generate multiple consecutive videos in a loop.
Afterward, you can use tools such as FFmpeg by yourself to stitch the generated multiple short videos into a complete long video.

|Output 1 |Output 2 |Output 3 |
|---|---|---|
|<video src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/c984894e448f43ca8a593babe411a078" controls></video>|<video src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/ccb8cebc70bd42738ba8d4bb894b69e6" controls></video>|<video src="https://p9-arcosite.byteimg.com/obj/tos-cn-i-goo7wpa0wc/b78ed8dd418a4c97ac94253cb0c00728" controls></video>|\
||||\
|> A girl holding a fox, the girl opens her eyes, looks gently at the camera, the fox hugs affectionately, the camera slowly pulls out, the girl's hair is blown by the wind |> A girl and a fox running on the grass, sunny weather, the girl's smile is brilliant, the fox jumps happily |> A girl and a fox resting under a tree, the girl gently strokes the fox's fur, the fox lies meekly on the girl's lap |

```Python
import os
import time  
# Install SDK:pip install byteplus-python-sdk-v2 
from byteplussdkarkruntime import Ark 

# Make sure that you have stored the API Key in the environment variable ARK_API_KEY
# Initialize the Ark client to read your API Key from an environment variable
client = Ark(
    # This is the default path. You can configure it based on the service location
    base_url="https://ark.ap-southeast.bytepluses.com/api/v3",
    # Get API Key: https://console.byteplus.com/ark/region:ark+ap-southeast-1/apikey
    api_key=os.environ.get("ARK_API_KEY"),
)

def generate_video_with_last_frame(prompt, initial_image_url=None):
    """
    Generate video and return video URL and last frame URL
    Parameters:
    prompt: Text prompt for video generation
    initial_image_url: Initial image URL (optional) 
    Returns:
    video_url: Generated video URL
    last_frame_url: URL of the last frame of the video
    """
    print(f"----- Generating video: {prompt} -----")
    
    # Build content list
    content = [{
        "text": prompt,
        "type": "text"
    }]
    
    # If initial image is provided, add to content
    if initial_image_url:
        content.append({
            "image_url": {
                "url": initial_image_url
            },
            "type": "image_url"
        })
    
    # Create video generation task
    create_result = client.content_generation.tasks.create(
        model="dreamina-seedance-2-0-260128", #Replace with Model ID
        content=content,
        return_last_frame=True, 
        ratio="adaptive",
        duration=5,
        watermark=False,
    )
    
    # Poll to check task status
    task_id = create_result.id
    while True:
        get_result = client.content_generation.tasks.get(task_id=task_id)
        status = get_result.status
        
        if get_result.status == "succeeded":
            print("Video generation succeeded")
            try:
                if hasattr(get_result, 'content') and hasattr(get_result.content, 'video_url') and hasattr(get_result.content, 'last_frame_url'):
                    return get_result.content.video_url, get_result.content.last_frame_url
                print("Failed to obtain video URL or last frame URL")
                return None, None
            except Exception as e:
                print(f"Error occurred while obtaining video URL and last frame URL: {e}")
                return None, None
        elif status == "failed":
            print(f"----- Video generation failed -----")
            print(f"Error: {get_result.error}")
            return None, None
        else:
            print(f"Current status: {status}, retrying in 10 seconds...")
            time.sleep(10)



if __name__ == "__main__":
    # Define 3 video prompts
    prompts = [
        "A girl holding a fox, the girl opens her eyes, looks gently at the camera, the fox hugs affectionately, the camera slowly pulls out, the girl's hair is blown by the wind",
        "A girl and a fox running on the grass, sunny weather, the girl's smile is brilliant, the fox jumps happily",
        "A girl and a fox resting under a tree, the girl gently strokes the fox's fur, the fox lies meekly on the girl's lap"
    ]
    
    # Store generated video URLs
    video_urls = []
    
    # Initial image URL
    initial_image_url = "https://ark-doc.tos-ap-southeast-1.bytepluses.com/doc_image/i2v_foxrgirl.png"
    
    # Generate 3 short videos
    for i, prompt in enumerate(prompts):
        print(f"Generating video {i+1}")
        video_url, last_frame_url = generate_video_with_last_frame(prompt, initial_image_url)
        
        if video_url and last_frame_url:
            video_urls.append(video_url)
            print(f"Video {i+1} URL: {video_url}")
            # Use the last frame of the current video as the first frame of the next video
            initial_image_url = last_frame_url
        else:
            print(f"Video {i+1} generation failed, exiting program")
            exit(1)
    
    print("All videos generated successfully!")
    print("Generated video URL list:")
    for i, url in enumerate(video_urls):
        print(f"Video {i+1}: {url}")
```

<span id="caf01f12"></span>
## Use Webhook notifications
You can specify a callback notification address via the **callback_url** parameter. When the status of a video generation task changes, ModelArk will send a POST request to this address, so you can get the latest status of the task in time. The request content structure is consistent with the response body of the [Retrieve a video generation task](https://docs.byteplus.com/en/docs/ModelArk/1521309) API.
```Bash
{
  "id": "cgt-2025****",
  "model": "dreamina-seedance-2-0-260128",
  "status": "running", # Possible status values: queued, running, succeeded, failed, expired
  "created_at": 1765434920,
  "updated_at": 1765434920,
  "service_tier": "default",
  "execution_expires_after": 172800
}
```

You need to build a publicly accessible web server on your own to receive Webhook notifications. See the following simple web server code sample for your reference.
```Python
# Building a Simple Web Server with Python Flask for Webhook Notification Processing

from flask import Flask, request, jsonify
import sqlite3
import logging
from datetime import datetime
import os

# === Basic Configuration ===
app = Flask(__name__)
# Configure logging
logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s - %(levelname)s - %(message)s',
    handlers=[logging.FileHandler('webhook.log'), logging.StreamHandler()]
)
# Database path
DB_PATH = 'video_tasks.db'

# === Database Initialization ===
def init_db():
    """Automatically create task table on first run, aligning fields with callback parameters"""
    conn = sqlite3.connect(DB_PATH)
    cursor = conn.cursor()
    # Create table: task_id as primary key for idempotent updates
    cursor.execute('''
    CREATE TABLE IF NOT EXISTS video_generation_tasks (
        task_id TEXT PRIMARY KEY,
        model TEXT NOT NULL,
        status TEXT NOT NULL,
        created_at INTEGER NOT NULL,
        updated_at INTEGER NOT NULL,
        service_tier TEXT NOT NULL,
        execution_expires_after INTEGER NOT NULL,
        last_callback_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
    )
    ''')
    conn.commit()
    conn.close()
    logging.info("Database initialized, table created/exists")

# === Core Webhook Interface ===
@app.route('/webhook/callback', methods=['POST'])
def video_task_callback():
    """Core interface for receiving Ark callback"""
    try:
        # 1. Parse callback request body (JSON format)
        callback_data = request.get_json()
        if not callback_data:
            logging.error("Callback request body empty or non-JSON format")
            return jsonify({"code": 400, "msg": "Invalid JSON data"}), 400

        # 2. Validate required fields
        required_fields = ['id', 'model', 'status', 'created_at', 'updated_at', 'service_tier', 'execution_expires_after']
        for field in required_fields:
            if field not in callback_data:
                logging.error(f"Callback data missing required field: {field}, data: {callback_data}")
                return jsonify({"code": 400, "msg": f"Missing field: {field}"}), 400

        # 3. Extract key information and log
        task_id = callback_data['id']
        status = callback_data['status']
        model = callback_data['model']
        logging.info(f"Received task callback | Task ID: {task_id} | Status: {status} | Model: {model}")
        print(f"[{datetime.now()}] Task {task_id} status updated to: {status}")  # Console output

        # 4. Database operation
        conn = sqlite3.connect(DB_PATH)
        cursor = conn.cursor()
        cursor.execute('''
        INSERT OR REPLACE INTO video_generation_tasks (
            task_id, model, status, created_at, updated_at, service_tier, execution_expires_after
        ) VALUES (?, ?, ?, ?, ?, ?, ?)
        ''', (
            task_id,
            model,
            status,
            callback_data['created_at'],
            callback_data['updated_at'],
            callback_data['service_tier'],
            callback_data['execution_expires_after']
        ))
        conn.commit()
        conn.close()
        logging.info(f"Task {task_id} database update successful")

        # 5. Return 200 response
        return jsonify({"code": 200, "msg": "Callback received successfully", "task_id": task_id}), 200

    except Exception as e:
        # Catch all exceptions to avoid returning 5xx
        logging.error(f"Callback processing failed: {str(e)}", exc_info=True)
        return jsonify({"code": 200, "msg": "Callback received successfully (internal processing exception)"}), 200

# === Helper Interface (Optional, for querying task status) ===
@app.route('/tasks/<task_id>', methods=['GET'])
def get_task_status(task_id):
    """Query latest status of specified task"""
    conn = sqlite3.connect(DB_PATH)
    cursor = conn.cursor()
    cursor.execute('SELECT * FROM video_generation_tasks WHERE task_id = ?', (task_id,))
    task = cursor.fetchone()
    conn.close()
    if not task:
        return jsonify({"code": 404, "msg": "Task not found"}), 404
    # Map field names for response
    fields = ['task_id', 'model', 'status', 'created_at', 'updated_at', 'service_tier', 'execution_expires_after', 'last_callback_at']
    task_dict = dict(zip(fields, task))
    return jsonify({"code": 200, "data": task_dict}), 200

# === Service Startup ===
if __name__ == '__main__':
    # Initialize database
    init_db()
    # Start Flask service (bind to 0.0.0.0 for public access, port customizable)
    # Test environment: debug=True; Production environment should disable debug and use gunicorn
    app.run(host='0.0.0.0', port=8080, debug=False)
```

<span id="66cb028f"></span>
# Limitations
<span id="63a97f09"></span>
## Multimodal input
:::warning Caution
Seedance 2.0 series models do not support direct upload of reference images or videos containing real human faces. A series of solutions are provided to make it easier for creatives to use portraits. For details, see [Create with ease](/docs/ModelArk/2291680#5c67c9a1).
:::
**Image requirements**

* Input method: URL or Base64 string.
* Format: .jpeg, .png, .webp, .bmp, .tiff, .gif. Note: seedance 1.5 pro adds support for .heic and .heif.
* Single image dimensions:
   * Aspect ratio (width/height): (0.4, 2.5)
   * Width and height (px): (300, 6000)
* Size: Single image is less than 30 MB. Request body size must not exceed 64 MB. Do not use Base64 encoding for large files.
* Number of images:
   * Image\-to\-video (first frame): 1 image
   * Image\-to\-video (first and last frames): 2 images
   * Seedance 2.0 & 2.0 fast multimodal\-reference\-to\-video: 1–9 images
   * Seedance 1.0 lite reference\-image\-to\-video: 1–4 images 

**Video requirements**

* Input method: URL.
* Video format: .mp4, .mov, see the table below for supported encoding formats.
* Resolution: 480p, 720p
* Duration: Single video duration [2, 15] seconds, maximum 3 reference videos can be uploaded, total duration of all videos shall not exceed 15 seconds.
* Single video dimensions:
   * Aspect ratio (width/height): [0.4, 2.5]
   * Width and height (px): [300, 6000]
   * Total pixels: [640×640=409600, 834×1112=927408], that is, the product of width and height must fall in the range of [409600, 927408].
* Size: The video shall not exceed 50 MB.
* Frame rate (FPS): [24, 60]


|**Container format** |**Common file extensions** |**MIME** |**Supported encoding** |
|---|---|---|---|
|MP4 |.mp4 |video/mp4 |video: H.264/AVC, H.265/HEVC|\
| | | |audio: AAC, MP3 |
|QuickTime |.mov |video/quicktime |video: H.264/AVC, H.265/HEVC|\
| | | |audio: AAC, MP3 |

**Audio requirements**

* Format: .wav, .mp3
* Duration: Single audio duration [2, 15] seconds, maximum 3 reference audio clips can be uploaded, total duration of all audios shall not exceed 15 seconds.
* Size: Each audio shall not exceed 15 MB, and the request body size shall not exceed 64 MB. Do not use Base64 encoding for large files.

<span id="2760a484"></span>
## Retention period
Task data (such as task status, video URL, etc.) is only retained for 24 hours, after which it will be automatically cleared. Be sure to save the generated video in time.
<span id="b25b1821"></span>
## Rate limits
<span id="516ef631"></span>
### **Model rate limits**
**default (online inference)** 

* RPM rate limit: The maximum number of tasks allowed to be created per minute for the same model (differentiated by model version) under the account. If this limit is exceeded, an error will be reported when creating a video generation task.
* Concurrency limit: The maximum number of tasks being processed at the same time for the same model (differentiated by model version) under the account. Tasks exceeding this limit will enter the queue to wait for processing.
* Limits vary with models, see [Video generation](/docs/ModelArk/1330310#2705b333) for details.

**flex (offline inference)** 

* TPD rate limit: The upper limit of total tokens for the same model (differentiated by model version) by the account within one day. Call requests exceeding this limit will be rejected. TPD rate limits vary with models. See [Video generation](/docs/ModelArk/1330310#2705b333) for details.

&nbsp;
<span id="f76aafc8"></span>
## Image cropping rules
**For image\-to\-video tasks using seedance series models, you can set the aspect ratio of the generated video.**  When the selected video aspect ratio is inconsistent with the aspect ratio of your uploaded image, ModelArk will crop your image, and the cropping will be centered. The detailed rules are as follows:
:::tip Description
For better video quality, it is recommended that the specified video aspect ratio (ratio) is as close as possible to the aspect ratio of the actual uploaded image.

:::
1. Input parameters:
   * The width of the original image is recorded as `W` (unit: pixels), and the height is recorded as `H` (unit: pixels).
   * The target ratio is recorded as `A:B` (for example, 21:9), which means the ratio of the width to height after cropping should be `A/B` (e.g. 21/9 ≈ 2.333).
2. Compare aspect ratios:
   * Calculate the aspect ratio of the original image `Ratio_original = W/H`.
   * Calculate the target ratio value `Ratio_target = A/B` (for example, the Ratio_target for 21:9 is 21/9 ≈ 2.333).
   * Determine the cropping benchmark based on the comparison result:
      * If `Ratio_original < Ratio_target` (that is, the original image is "too tall" or portrait\-oriented), crop based on the width.
      * If `Ratio_original > Ratio_target` (that is, the original image is "too wide" or landscape\-oriented), crop based on the height.
      * If they are equal, no cropping is required, and the full image is used directly.
3. Cropping size calculation (quantitative formula):
   * Based on width (applicable to portrait images):
      * Cropped width `Crop_W = W` (uses the entire original width).
      * Cropped height `Crop_H = (B/A) × W` (calculate the height proportionally according to the target ratio).
      * Starting coordinates of the cropping area (centered positioning):
         * X coordinate (horizontal): always 0 (since the full width is used, starting from the left edge).
         * Y coordinate (vertical): `(H - Crop_H)/2` (ensures vertical centering, starting from the top edge).
   * Based on height (applicable to landscape images):
      * Cropped height `Crop_H = H` (uses the entire original height).
      * Cropped width `Crop_W = (A/B) × H` (calculate the width proportionally according to the target ratio).
      * Starting coordinates of the cropping area (centered positioning):
         * X coordinate (horizontal): `(W − Crop_W)/2` (ensures horizontal centering, starting from the left edge).
         * Y coordinate (vertical): always 0 (since the full height is used, starting from the top edge).
4. Cropping result:
   * The final cropped image size is `Crop_W × Crop_H`, with a strict aspect ratio of `A:B`, and it is completely inside the original image with no black borders.
   * The cropping area is always based on the center of the original image, so the content is centered.

