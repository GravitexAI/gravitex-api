The Seedance model features strong semantic understanding capabilities and can quickly generate high-quality video clips based on user-provided text, images, and other inputs. This tutorial explains how to call <a href="https://docs.byteplus.com/en/docs/ModelArk/Video_Generation_API">Video Generation API</a> to generate videos.
:::warning
Seedance 2.0 **does not support API invocation** at this time. Please stay tuned for updates.
:::
<span id="a06d249e"></span>
# Showcases

| | | | | \
|Use cases |\
|<div style="width:100px"></div> |Input: prompt |\
| |<div style="width:300px"></div> |Input: image, video, audio |\
| | | |Output |\
| | | | |
|---|---|---|---|
| | | | | \
|**Audio-Video Generation** |A female opera performer sings on stage in a clear soprano voice. She begins singing calmly and maintains a steady pace. Her gaze slowly shifts in sequence: first looking into the distance, then lowering to the floor, and finally lifting to look directly into the camera. She sings the full lyric clearly and completely with a gentle, warm smile: “Hold on, let go, give trust, lend heart.” The line must be sung from beginning to end without interruption. The video must not cut or end before the final word is fully delivered. After finishing the last word, she holds her gaze and expression briefly before the scene ends. |\
| | |![Image](https://p9-arcosite.byteimg.com/tos-cn-i-goo7wpa0wc/c85ec4a84f4b495694b277e65f09dfc1~tplv-goo7wpa0wc-image.image =930x) |<BytedReactXgplayer config={{ url: 'https://p9-arcosite.byteimg.com/tos-cn-i-goo7wpa0wc/b98110b1363043ee9429802aceb4276e~tplv-goo7wpa0wc-image.image', poster: 'https://p9-arcosite.byteimg.com/tos-cn-i-goo7wpa0wc/b98110b1363043ee9429802aceb4276e~tplv-goo7wpa0wc-video-poster.jpeg' }} ></BytedReactXgplayer> |
| | | | | \
|**Multi-Reference Image-to-Video** |\
| |\
| |\
| |A boy wearing glasses and a blue T-shirt from [Image 1] and a corgi dog from [Image 2], sitting on the lawn from [Image 3], in 3D cartoon style |![Image](https://p9-arcosite.byteimg.com/tos-cn-i-goo7wpa0wc/2a0691e5748e414c9a91837684d459d3~tplv-goo7wpa0wc-image.image =1200x) |\
| | | |<BytedReactXgplayer config={{ url: 'https://p9-arcosite.byteimg.com/tos-cn-i-goo7wpa0wc/b1a47f5c4bc64a30b7ced1a7511233fb~tplv-goo7wpa0wc-image.image', poster: 'https://p9-arcosite.byteimg.com/tos-cn-i-goo7wpa0wc/b1a47f5c4bc64a30b7ced1a7511233fb~tplv-goo7wpa0wc-video-poster.jpeg' }} ></BytedReactXgplayer> |
| | | | | \
|**First-and-Last Frame Video Generation** |\
| |\
| |Create a 360-degree orbiting camera shot based on this photo |\
| | |![Image](https://p9-arcosite.byteimg.com/tos-cn-i-goo7wpa0wc/f8fc1008f23a4908b7c897e8b7eb87df~tplv-goo7wpa0wc-image.image =1160x) |<BytedReactXgplayer config={{ url: 'https://p9-arcosite.byteimg.com/tos-cn-i-goo7wpa0wc/4531a834472c4a2fb7e1d9ab7461d5cd~tplv-goo7wpa0wc-image.image', poster: 'https://p9-arcosite.byteimg.com/tos-cn-i-goo7wpa0wc/4531a834472c4a2fb7e1d9ab7461d5cd~tplv-goo7wpa0wc-video-poster.jpeg' }} ></BytedReactXgplayer> |

<span id="2c6a9e64"></span>
# Quick start
Video generation is an asynchronous process:

1. After successfully calling the `POST /contents/generations/tasks` endpoint, the API will return a task ID.
2. You can poll the `GET /contents/generations/tasks/{id}` endpoint until the task status changes to `succeeded`, or use a Webhook to automatically receive status updates for the video generation task.
3. After the task is completed, you can download the final generated MP4 file from the content.**video_url**  parameter.

<span id="34b10d6d"></span>
## Step 1: Create video generation task
Create a video generation task via `POST /contents/generations/tasks`.

```mixin-react
return (<Tabs>
<Tabs.TabPane title="Curl" key="BnGnrZLSGe"><RenderMd content={`\`\`\`Bash
curl https://ark.ap-southeast.bytepluses.com/api/v3/contents/generations/tasks \\
  -H "Content-Type: application/json" \\
  -H "Authorization: Bearer $ARK_API_KEY" \\
  -d '{
    "model": "seedance-1-5-pro-251215",
    "content": [
        {
            "type": "text",
            "text": "A girl holding a fox, the girl opens her eyes, looks gently at the camera, the fox hugs affectionately, the camera slowly pulls out, the girl’s hair is blown by the wind, and the sound of the wind can be heard"
        },
        {
            "type": "image_url",
            "image_url": {
                "url": "https://ark-doc.tos-ap-southeast-1.bytepluses.com/doc_image/i2v_foxrgirl.png"
            }
        }
    ],
    "generate_audio": true,
    "ratio": "adaptive",
    "duration": 5,
    "watermark": false
}'
\`\`\`

`}></RenderMd></Tabs.TabPane>
<Tabs.TabPane title="Python" key="UYft09Qu31"><RenderMd content={`\`\`\`Python
import os
from byteplussdkarkruntime import Ark
 
# Get API Key：https://console.byteplus.com/ark/region:ark+ap-southeast-1/apikey
client = Ark(api_key=os.environ.get("ARK_API_KEY"))

if __name__ == "__main__":
    print("----- create request -----")
    resp = client.content_generation.tasks.create(
        model="seedance-1-5-pro-251215", # Replace with Model ID
        content=[
            {
                "text": (
                    "A girl holding a fox, the girl opens her eyes, looks gently at the camera, the fox hugs affectionately, the camera slowly pulls out, the girl’s hair is blown by the wind"
                ),
                "type": "text"
            },
            {
                "image_url": {
                    "url": (
                        "https://ark-doc.tos-ap-southeast-1.bytepluses.com/doc_image/i2v_foxrgirl.png"
                    )
                },
                "type": "image_url"
            }
        ],
        generate_audio=True,
        ratio="adaptive",
        duration=5,
        watermark=False,
    )
    
    print(resp)
\`\`\`

`}></RenderMd></Tabs.TabPane>
<Tabs.TabPane title="Java" key="jYqAi5pFQo"><RenderMd content={`\`\`\`Java
package com.ark.sample;

import com.byteplus.ark.runtime.model.content.generation.*;
import com.byteplus.ark.runtime.model.content.generation.CreateContentGenerationTaskRequest.Content;
import com.byteplus.ark.runtime.service.ArkService;
import okhttp3.ConnectionPool;
import okhttp3.Dispatcher;

import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.TimeUnit;

public class ContentGenerationTaskExample {
    // Make sure that you have stored the API Key in the environment variable ARK_API_KEY
    // Initialize the Ark client to read your API Key from an environment variable
    static String apiKey = System.getenv("ARK_API_KEY");
    static ConnectionPool connectionPool = new ConnectionPool(5, 1, TimeUnit.SECONDS);
    static Dispatcher dispatcher = new Dispatcher();
    static ArkService service = ArkService.builder()
           .baseUrl("https://ark.ap-southeast.bytepluses.com/api/v3") // The base URL for model invocation
           .dispatcher(dispatcher)
           .connectionPool(connectionPool)
           .apiKey(apiKey)
           .build();
           
    public static void main(String[] args) {
        String model = "seedance-1-5-pro-251215"; // Replace with Model ID
        Boolean generateAudio = true;
        String ratio = "adaptive";
        Long duration = 5L;
        Boolean watermark = false;
        System.out.println("----- create request -----");
        List<Content> contents = new ArrayList<>();
        
        // Combination of text prompt and parameters
        contents.add(Content.builder()
                .type("text")
                .text("A girl holding a fox, the girl opens her eyes, looks gently at the camera, the fox hugs affectionately, the camera slowly pulls out, the girl’s hair is blown by the wind, and the sound of the wind can be heard")
                .build());
        // The URL of the first frame image
        contents.add(Content.builder()
                .type("image_url")
                .imageUrl(CreateContentGenerationTaskRequest.ImageUrl.builder()
                        .url("https://ark-doc.tos-ap-southeast-1.bytepluses.com/doc_image/i2v_foxrgirl.png")
                        .build())
                .build());

        // Create a video generation task
        CreateContentGenerationTaskRequest createRequest = CreateContentGenerationTaskRequest.builder()
                .model(model)
                .content(contents)
                .generateAudio(generateAudio)
                .ratio(ratio)
                .duration(duration)
                .watermark(watermark)
                .build();

        CreateContentGenerationTaskResult createResult = service.createContentGenerationTask(createRequest);
        System.out.println(createResult);

        service.shutdownExecutor(); 
    }
} 
\`\`\`

`}></RenderMd></Tabs.TabPane>
<Tabs.TabPane title="Go" key="y9qEbeCs7x"><RenderMd content={`\`\`\`Go
package main

import (
    "context"
    "fmt"
    "os"
    "time"

    "github.com/byteplus-sdk/byteplus-go-sdk-v2/service/arkruntime"
    "github.com/byteplus-sdk/byteplus-go-sdk-v2/service/arkruntime/model"
    "github.com/byteplus-sdk/byteplus-go-sdk-v2/byteplus"
)

func main() {
    // Make sure that you have stored the API Key in the environment variable ARK_API_KEY
    // Initialize the Ark client to read your API Key from an environment variable
    client := arkruntime.NewClientWithApiKey(
        // Get your API Key from the environment variable. This is the default mode and you can modify it as required
        os.Getenv("ARK_API_KEY"),
        // The base URL for model invocation
        arkruntime.WithBaseUrl("https://ark.ap-southeast.bytepluses.com/api/v3"),
    )
    ctx := context.Background()
    // Replace with Model ID
    modelEp := "seedance-1-5-pro-251215"

    // Generate a task
    fmt.Println("----- create request -----")
    createReq := model.CreateContentGenerationTaskRequest{
        Model: modelEp,
        GenerateAudio: byteplus.Bool(true),
        Ratio:         byteplus.String("adaptive"),
        Duration:      byteplus.Int64(5),
        Watermark:     byteplus.Bool(false),
        Content: []*model.CreateContentGenerationContentItem{
            {
                // Combination of text prompt and parameters
                Type: model.ContentGenerationContentItemTypeText,
                Text: byteplus.String("A girl holding a fox, the girl opens her eyes, looks gently at the camera, the fox hugs affectionately, the camera slowly pulls out, the girl’s hair is blown by the wind, and the sound of the wind can be heard"),
            },
            {
                // The URL of the first frame image
                Type: model.ContentGenerationContentItemTypeImage,
                ImageURL: &model.ImageURL{
                    URL: "https://ark-doc.tos-ap-southeast-1.bytepluses.com/doc_image/i2v_foxrgirl.png",
                },
            },
        },
    }
    createResp, err := client.CreateContentGenerationTask(ctx, createReq)
    if err != nil {
        fmt.Printf("create content generation error: %v", err)
        return
    }
    taskID := createResp.ID
    fmt.Printf("Task Created with ID: %s", taskID)
}
\`\`\`

`}></RenderMd></Tabs.TabPane></Tabs>);
 ```

After the request is successful, the system will return a task ID.
```Python
{
  "id": "cgt-2025******-****"
}
```

<span id="a4fa0cc8"></span>
## Step 2: Query video generation task
You can query the detailed status and result of the task using the ID returned in the response of the creating video generation task. This endpoint will return the current task status - `queued`, `running`, `succeeded`, etc. Information related to the generated video, like video download link, resolution, duration, will also be returned.
:::tip
* Because of variations in model capabilities, API load, and video output requirements, video generation may take a considerable amount of time.
* To efficiently manage this process, you can poll the API endpoint (see the SDK examples in [Basic usage](/docs/ModelArk/1366799#754e68e3) and [Advanced usage](/docs/ModelArk/1366799#e190e738)) to request status updates, or receive notifications via [Webhook](/docs/ModelArk/1366799#2ea4182d).
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

`}></RenderMd></Tabs.TabPane>
<Tabs.TabPane title="Java" key="xNsVhKAgA4"><RenderMd content={`\`\`\`Java
package com.ark.sample;

import com.fasterxml.jackson.core.JsonProcessingException;
import com.byteplus.ark.runtime.model.content.generation.GetContentGenerationTaskRequest;
import com.byteplus.ark.runtime.service.ArkService;
import java.util.concurrent.TimeUnit;
import okhttp3.ConnectionPool;
import okhttp3.Dispatcher;


public class Sample {

    static String apiKey = System.getenv("ARK_API_KEY");

    static ConnectionPool connectionPool = new ConnectionPool(5, 1, TimeUnit.SECONDS);
    static Dispatcher dispatcher = new Dispatcher();
    static ArkService service =
            ArkService.builder()
                    .dispatcher(dispatcher)
                    .connectionPool(connectionPool)
                    .apiKey(apiKey)
                    .build();

    public static void main(String[] args) throws JsonProcessingException {
        String taskId = "cgt-2025****";

        GetContentGenerationTaskRequest req = GetContentGenerationTaskRequest.builder()
                .taskId(taskId)
                .build();


        service.getContentGenerationTask(req).toString();
        System.out.println(service.getContentGenerationTask(req));

        service.shutdownExecutor();
    }
}
\`\`\`

`}></RenderMd></Tabs.TabPane>
<Tabs.TabPane title="Go" key="nuwms7RIL6"><RenderMd content={`\`\`\`Go
package main

import (
        "context"
        "fmt"
        "os"

        "github.com/byteplus-sdk/byteplus-go-sdk-v2/service/arkruntime"
        "github.com/byteplus-sdk/byteplus-go-sdk-v2/service/arkruntime/model"
)


func main() {
        client := arkruntime.NewClientWithApiKey(os.Getenv("ARK_API_KEY"))
        ctx := context.Background()

        req := model.GetContentGenerationTaskRequest{
                ID: "cgt-2025****", 
        }
        resp, err := client.GetContentGenerationTask(ctx, req)
        if err != nil {
                fmt.Printf("get content generation task error: %v\\n", err)
                return
        }
        fmt.Printf("%+v\\n", resp)
}
\`\`\`

`}></RenderMd></Tabs.TabPane></Tabs>);
 ```

When the task status changes to `succeeded`, you can download the final generated video file from the content.**video_url**  parameter.
```JSON
{
    "id": "cgt-2025****",
    "model": "seedance-1-5-pro-251215",
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
# Model list

* For the highest generation quality and native audio-visual synchronization, we recommend using the latest model, **Seedance 1.5 pro**.
* If you prioritize cost and generation speed over top-tier quality, **Seedance 1.0 pro fast** is the ideal choice.
* To create videos based on multiple reference images, we suggest using **Seedance 1.0 lite**.


| | | | | | | | | | \
|**Model Name** |\
|<div style="width:100px"></div> |**Version** |\
| |<div style="width:100px"></div> |**Model ID** |\
| | |<div style="width:150px"></div> |**Model capabilities** |\
| | | |<div style="width:150px"></div> |**Output Video Format** |\
| | | | |<div style="width:150px"></div> |**Requests Per Minute** |\
| | | | | |> default (Online Inference) |\
| | | | | |> flex (Offline Inference) |\
| | | | | | |\
| | | | | |<div style="width:150px"></div> |**Concurrency limit** |**Price** |\
| | | | | | | |(USD / M Tokens) |**Free Credit** |\
| | | | | | | | |(Token)  |
|---|---|---|---|---|---|---|---|---|
| | | | | | | | | | \
|[seedance-1.5-pro](https://console.byteplus.com/ark/region:ark+ap-southeast-1/model/detail?Id=seedance-1-5-pro) |251215 |\
| |`recommend` |seedance-1-5-pro-251215 |Image-to-Video - First and Last Frames |\
| | | |Image-to-Video - First Frame |\
| | | |Text-to-Video |Resolution:  |\
| | | | |480p, 720p,  |\
| | | | |1080p |\
| | | | |Frame Rate: 24 fps |\
| | | | |Duration: 4~12 s |\
| | | | |Video Format: mp4 |default：RPM 600 |\
| | | | | |flex：TPD 500B |default：10 |\
| | | | | | |flex：N/A |[ Video Generation](/docs/ModelArk/1099320#e13f68ed) |default：200M |\
| | | | | | | | |flex：N/A |
| | | | | | | | | | \
|[seedance-1.0-pro](/docs/ModelArk/1587798) |250528 |\
| |`recommend` |seedance-1-0-pro-250528 |Image-to-Video - First and Last Frames |\
| | | |Image-to-Video - First Frame |\
| | | |Text-to-Video |Resolution:  |\
| | | | |480p, |\
| | | | |720p, |\
| | | | |1080p`Reference image feature is not supported` |\
| | | | |Frame Rate: 24 fps |\
| | | | |Duration: 2~12 s |\
| | | | |Video Format: mp4 |default：RPM 600 |\
| | | | | |flex：TPD 500B |default：10 |\
| | | | | | |flex：N/A |[ Video Generation](/docs/ModelArk/1099320#e13f68ed) |default：200M |\
| | | | | | | | |flex：N/A |
| | | | |^^| | | | | \
|[seedance-1.0-pro-fast](/docs/ModelArk/1901652) |251015 |\
| |`recommend` |seedance-1-0-pro-fast-251015 |Image-to-Video - First Frame |\
| | | |Text-to-Video | |default：RPM 600 |\
| | | | | |flex：TPD 500B |default：10 |\
| | | | | | |flex：N/A |[ Video Generation](/docs/ModelArk/1099320#e13f68ed) |default：200M |\
| | | | | | | | |flex：N/A |
| | | | |^^| | | | | \
|[seedance-1.0-lite](/docs/ModelArk/1553576) |250428  |seedance-1-0-lite-t2v-250428 |Text-to-Video | |default：RPM 300 |\
| | | | | |flex：TPD 250B |default：5 |\
| | | | | | |flex：N/A |[ Video Generation](/docs/ModelArk/1099320#e13f68ed) |default：200M |\
| | | | | | | | |flex：N/A |
|^^| | | |^^| | | | | \
| |250428 |\
| | |seedance-1-0-lite-i2v-250428 |Image-to-Video - Reference Images |\
| | | |Image-to-Video - First and Last Frames |\
| | | |Image-to-Video - First Frame | |default：RPM 300 |\
| | | | | |flex：TPD 250B |default：5 |\
| | | | | | |flex：N/A |[ Video Generation](/docs/ModelArk/1099320#e13f68ed) |default：200M |\
| | | | | | | | |flex：N/A |

<span id="f5531933"></span>
# Prerequisites
:::tip
If you're new to ModelArk, see [Quick start](/docs/ModelArk/1399008) to get up and running quickly.
:::



<span id="754e68e3"></span>
# Basic usage


<span id="42750622"></span>
## Text-to-video
Generate video based on user-entered prompts; the results are highly random and can be used to inspire creative ideas.

| | | \
|Prompt |Output |
|---|---|
| | | \
|Photorealistic style: Under a clear blue sky, a vast expanse of white daisy fields stretches out. The camera gradually zooms in and finally fixates on a close-up of a single daisy, with several glistening dewdrops resting on its petals. |<BytedReactXgplayer config={{ url: 'https://p9-arcosite.byteimg.com/tos-cn-i-goo7wpa0wc/62699e55f1674309b6692d188c9ed492~tplv-goo7wpa0wc-image.image', poster: 'https://p9-arcosite.byteimg.com/tos-cn-i-goo7wpa0wc/62699e55f1674309b6692d188c9ed492~tplv-goo7wpa0wc-video-poster.jpeg' }} ></BytedReactXgplayer> |\
| | |


```mixin-react
return (<Tabs>
<Tabs.TabPane title="Python" key="fT2oe30WFK"><RenderMd content={`\`\`\`Python
import os
import time  
# Install SDK:  pip install byteplus-python-sdk-v2 
from byteplussdkarkruntime import Ark 

# Make sure that you have stored the API Key in the environment variable ARK_API_KEY
# Initialize the Ark client to read your API Key from an environment variable
client = Ark(
    # This is the default path. You can configure it based on the service location
    base_url="https://ark.ap-southeast.bytepluses.com/api/v3",
    # Get API Key：https://console.byteplus.com/ark/region:ark+ap-southeast-1/apikey
    api_key=os.environ.get("ARK_API_KEY"),
)

if __name__ == "__main__":
    print("----- create request -----")
    create_result = client.content_generation.tasks.create(
        model="seedance-1-5-pro-251215", # Replace with Model ID 
        content=[
            {
                # Combination of text prompt and parameters
                "type": "text",
                "text": "Photorealistic style: Under a clear blue sky, a vast expanse of white daisy fields stretches out. The camera gradually zooms in and finally fixates on a close - up of a single daisy, with several glistening dewdrops resting on its petals."
            }
        ],
        ratio="16:9",
        duration=5,
        watermark=False,
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
            print(f"Error: {get_result.error}")
            break
        else:
            print(f"Current status: {status}, Retrying after 10 seconds...")
            time.sleep(10)
\`\`\`

`}></RenderMd></Tabs.TabPane>
<Tabs.TabPane title="Java" key="XY3jQVYTLC"><RenderMd content={`\`\`\`Java
package com.ark.sample;

import com.byteplus.ark.runtime.model.content.generation.*;
import com.byteplus.ark.runtime.model.content.generation.CreateContentGenerationTaskRequest.Content;
import com.byteplus.ark.runtime.service.ArkService;
import okhttp3.ConnectionPool;
import okhttp3.Dispatcher;

import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.TimeUnit;

public class ContentGenerationTaskExample {
    // Make sure that you have stored the API Key in the environment variable ARK_API_KEY
    // Initialize the Ark client to read your API Key from an environment variable
    static String apiKey = System.getenv("ARK_API_KEY");
    static ConnectionPool connectionPool = new ConnectionPool(5, 1, TimeUnit.SECONDS);
    static Dispatcher dispatcher = new Dispatcher();
    static ArkService service = ArkService.builder()
           .baseUrl("https://ark.ap-southeast.bytepluses.com/api/v3") // The base URL for model invocation
           .dispatcher(dispatcher)
           .connectionPool(connectionPool)
           .apiKey(apiKey)
           .build();
           
    public static void main(String[] args) {
        String model = "seedance-1-5-pro-251215"; // Replace with Model ID
        String ratio = "16:9";
        Long duration = 5L;
        Boolean watermark = false;
        System.out.println("----- create request -----");
        List<Content> contents = new ArrayList<>();
        
        // Combination of text prompt and parameters
        contents.add(Content.builder()
                .type("text")
                .text("Photorealistic style: Under a clear blue sky, a vast expanse of white daisy fields stretches out. The camera gradually zooms in and finally fixates on a close - up of a single daisy, with several glistening dewdrops resting on its petals.")
                .build());

        // Create a video generation task
        CreateContentGenerationTaskRequest createRequest = CreateContentGenerationTaskRequest.builder()
                .model(model)
                .content(contents)
                .ratio(ratio)
                .duration(duration)
                .watermark(watermark)
                .build();

        CreateContentGenerationTaskResult createResult = service.createContentGenerationTask(createRequest);
        System.out.println(createResult);

        // Get the details of the task
        String taskId = createResult.getId();
        GetContentGenerationTaskRequest getRequest = GetContentGenerationTaskRequest.builder()
                .taskId(taskId)
                .build();
        
        // Polling query section
        System.out.println("----- polling task status -----");
        while (true) {
            try {
                GetContentGenerationTaskResponse getResponse = service.getContentGenerationTask(getRequest);
                String status = getResponse.getStatus();
                if ("succeeded".equalsIgnoreCase(status)) {
                    System.out.println("----- task succeeded -----");
                    System.out.println(getResponse);
                    break;
                } else if ("failed".equalsIgnoreCase(status)) {
                    System.out.println("----- task failed -----");
                    System.out.println("Error: " + getResponse.getStatus());
                    break;
                } else {
                    System.out.printf("Current status: %s, Retrying in 10 seconds...", status);
                    TimeUnit.SECONDS.sleep(10);
                }
            } catch (InterruptedException ie) {
                Thread.currentThread().interrupt();
                System.err.println("Polling interrupted");
                break;
            }
        }
    }
}
\`\`\`

`}></RenderMd></Tabs.TabPane>
<Tabs.TabPane title="Go" key="zHHpcLidea"><RenderMd content={`\`\`\`Go
package main

import (
    "context"
    "fmt"
    "os"
    "time"

    "github.com/byteplus-sdk/byteplus-go-sdk-v2/service/arkruntime"
    "github.com/byteplus-sdk/byteplus-go-sdk-v2/service/arkruntime/model"
    "github.com/byteplus-sdk/byteplus-go-sdk-v2/byteplus"
)

func main() {
    // Make sure that you have stored the API Key in the environment variable ARK_API_KEY
    // Initialize the Ark client to read your API Key from an environment variable
    client := arkruntime.NewClientWithApiKey(
        // Get your API Key from the environment variable. This is the default mode and you can modify it as required
        os.Getenv("ARK_API_KEY"),
        // The base URL for model invocation
        arkruntime.WithBaseUrl("https://ark.ap-southeast.bytepluses.com/api/v3"),
    )
    ctx := context.Background()
    // Replace with Model ID
    modelEp := "seedance-1-5-pro-251215"

    // Generate a task
    fmt.Println("----- create request -----")
    createReq := model.CreateContentGenerationTaskRequest{
        Model: modelEp,
        Ratio:         byteplus.String("16:9"),
        Duration:      byteplus.Int64(5),
        Watermark:     byteplus.Bool(false),
        Content: []*model.CreateContentGenerationContentItem{
            {
                // Combination of text prompt and parameters
                Type: model.ContentGenerationContentItemTypeText,
                Text: byteplus.String("Photorealistic style: Under a clear blue sky, a vast expanse of white daisy fields stretches out. The camera gradually zooms in and finally fixates on a close - up of a single daisy, with several glistening dewdrops resting on its petals."),
            },
        },
    }
    createResp, err := client.CreateContentGenerationTask(ctx, createReq)
    if err != nil {
        fmt.Printf("create content generation error: %v", err)
        return
    }
    taskID := createResp.ID
    fmt.Printf("Task Created with ID: %s", taskID)

    // Polling query section
    fmt.Println("----- polling task status -----")
    for {
        getReq := model.GetContentGenerationTaskRequest{ID: taskID}
        getResp, err := client.GetContentGenerationTask(ctx, getReq)
        if err != nil {
            fmt.Printf("get content generation task error: %v", err)
            return
        }

        status := getResp.Status
        if status == "succeeded" {
            fmt.Println("----- task succeeded -----")
            fmt.Printf("Task ID: %s \\n", getResp.ID)
            fmt.Printf("Model: %s \\n", getResp.Model)
            fmt.Printf("Video URL: %s \\n", getResp.Content.VideoURL)
            fmt.Printf("Completion Tokens: %d \\n", getResp.Usage.CompletionTokens)
            fmt.Printf("Created At: %d, Updated At: %d", getResp.CreatedAt, getResp.UpdatedAt)
            return
        } else if status == "failed" {
            fmt.Println("----- task failed -----")
            if getResp.Error != nil {
                fmt.Printf("Error Code: %s, Message: %s", getResp.Error.Code, getResp.Error.Message)
            }
            return
        } else {
            fmt.Printf("Current status: %s, Retrying in 10 seconds... \\n", status)
            time.Sleep(10 * time.Second)
        }
    }
}
\`\`\`

`}></RenderMd></Tabs.TabPane></Tabs>);
 ```

<span id="979b2d28"></span>
## **Image-to-video based on the first frame (w**ith audio)
By specifying the first-frame image of the video, the model can generate visually coherent and contextually related video content based on that image.
Seedance 1.5 pro can generate audio videos by setting the parameter **generate_audio** to `true`.

| | | | \
|Prompt |\
|<div style="width:150px"></div> |First frame |\
| |<div style="width:260px"></div> |Output |
|---|---|---|
| | | | \
|A girl holding a fox. She opens her eyes, looks gently at the camera. The fox hugs affectionately. The camera slowly pulls out, and the hair is blown by the wind. |![Image](https://p9-arcosite.byteimg.com/tos-cn-i-goo7wpa0wc/a28ec84ff9fc4287a0d98191020a3218~tplv-goo7wpa0wc-image.image =1456x) |\
| | |<BytedReactXgplayer config={{ url: 'https://p9-arcosite.byteimg.com/tos-cn-i-goo7wpa0wc/211c2a641cbd400e99298aa211155acc~tplv-goo7wpa0wc-image.image', poster: 'https://p9-arcosite.byteimg.com/tos-cn-i-goo7wpa0wc/211c2a641cbd400e99298aa211155acc~tplv-goo7wpa0wc-video-poster.jpeg' }} ></BytedReactXgplayer> |\
| | | |


```mixin-react
return (<Tabs>
<Tabs.TabPane title="Python" key="uUea5qfNwG"><RenderMd content={`\`\`\`Python
import os
import time  
# Install SDK:  pip install byteplus-python-sdk-v2 
from byteplussdkarkruntime import Ark 

# Make sure that you have stored the API Key in the environment variable ARK_API_KEY
# Initialize the Ark client to read your API Key from an environment variable
client = Ark(
    # This is the default path. You can configure it based on the service location
    base_url="https://ark.ap-southeast.bytepluses.com/api/v3",
    # Get API Key：https://console.byteplus.com/ark/region:ark+ap-southeast-1/apikey
    api_key=os.environ.get("ARK_API_KEY"),
)

if __name__ == "__main__":
    print("----- create request -----")
    create_result = client.content_generation.tasks.create(
        model="seedance-1-5-pro-251215", # Replace with Model ID
        content=[
            {
                # Combination of text prompt and parameters
                "type": "text",
                "text": "A girl holding a fox, the girl opens her eyes, looks gently at the camera, the fox hugs affectionately, the camera slowly pulls out, the girl’s hair is blown by the wind, and the sound of the wind can be heard"             
            },
            {
                # The URL of the first frame image
                "type": "image_url",
                "image_url": {
                    "url": "https://ark-doc.tos-ap-southeast-1.bytepluses.com/doc_image/i2v_foxrgirl.png"
                }
            }
        ],
        generate_audio=True,
        ratio="adaptive",
        duration=5,
        watermark=False,
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
            print(f"Error: {get_result.error}")
            break
        else:
            print(f"Current status: {status}, Retrying after 10 seconds...")
            time.sleep(10)
\`\`\`

`}></RenderMd></Tabs.TabPane>
<Tabs.TabPane title="Java" key="Cb42VjekdE"><RenderMd content={`\`\`\`Java
package com.ark.sample;

import com.byteplus.ark.runtime.model.content.generation.*;
import com.byteplus.ark.runtime.model.content.generation.CreateContentGenerationTaskRequest.Content;
import com.byteplus.ark.runtime.service.ArkService;
import okhttp3.ConnectionPool;
import okhttp3.Dispatcher;

import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.TimeUnit;

public class ContentGenerationTaskExample {
    // Make sure that you have stored the API Key in the environment variable ARK_API_KEY
    // Initialize the Ark client to read your API Key from an environment variable
    static String apiKey = System.getenv("ARK_API_KEY");
    static ConnectionPool connectionPool = new ConnectionPool(5, 1, TimeUnit.SECONDS);
    static Dispatcher dispatcher = new Dispatcher();
    static ArkService service = ArkService.builder()
           .baseUrl("https://ark.ap-southeast.bytepluses.com/api/v3") // The base URL for model invocation
           .dispatcher(dispatcher)
           .connectionPool(connectionPool)
           .apiKey(apiKey)
           .build();
           
    public static void main(String[] args) {
        String model = "seedance-1-5-pro-251215"; // Replace with Model ID
        Boolean generateAudio = true;
        String ratio = "adaptive";
        Long duration = 5L;
        Boolean watermark = false;
        System.out.println("----- create request -----");
        List<Content> contents = new ArrayList<>();
        
        // Combination of text prompt and parameters
        contents.add(Content.builder()
                .type("text")
                .text("A girl holding a fox, the girl opens her eyes, looks gently at the camera, the fox hugs affectionately, the camera slowly pulls out, the girl’s hair is blown by the wind, and the sound of the wind can be heard")
                .build());
        // The URL of the first frame image
        contents.add(Content.builder()
                .type("image_url")
                .imageUrl(CreateContentGenerationTaskRequest.ImageUrl.builder()
                        .url("https://ark-doc.tos-ap-southeast-1.bytepluses.com/doc_image/i2v_foxrgirl.png")
                        .build())
                .build());

        // Create a video generation task
        CreateContentGenerationTaskRequest createRequest = CreateContentGenerationTaskRequest.builder()
                .model(model)
                .content(contents)
                .generateAudio(generateAudio)
                .ratio(ratio)
                .duration(duration)
                .watermark(watermark)
                .build();

        CreateContentGenerationTaskResult createResult = service.createContentGenerationTask(createRequest);
        System.out.println(createResult);

        // Get the details of the task
        String taskId = createResult.getId();
        GetContentGenerationTaskRequest getRequest = GetContentGenerationTaskRequest.builder()
                .taskId(taskId)
                .build();
        
        // Polling query section
        System.out.println("----- polling task status -----");
        while (true) {
            try {
                GetContentGenerationTaskResponse getResponse = service.getContentGenerationTask(getRequest);
                String status = getResponse.getStatus();
                if ("succeeded".equalsIgnoreCase(status)) {
                    System.out.println("----- task succeeded -----");
                    System.out.println(getResponse);
                    break;
                } else if ("failed".equalsIgnoreCase(status)) {
                    System.out.println("----- task failed -----");
                    System.out.println("Error: " + getResponse.getStatus());
                    break;
                } else {
                    System.out.printf("Current status: %s, Retrying in 10 seconds...", status);
                    TimeUnit.SECONDS.sleep(10);
                }
            } catch (InterruptedException ie) {
                Thread.currentThread().interrupt();
                System.err.println("Polling interrupted");
                break;
            }
        }
    }
}
\`\`\`

`}></RenderMd></Tabs.TabPane>
<Tabs.TabPane title="Go" key="ZlzbyAKoL5"><RenderMd content={`\`\`\`Go
package main

import (
    "context"
    "fmt"
    "os"
    "time"

    "github.com/byteplus-sdk/byteplus-go-sdk-v2/service/arkruntime"
    "github.com/byteplus-sdk/byteplus-go-sdk-v2/service/arkruntime/model"
    "github.com/byteplus-sdk/byteplus-go-sdk-v2/byteplus"
)

func main() {
    // Make sure that you have stored the API Key in the environment variable ARK_API_KEY
    // Initialize the Ark client to read your API Key from an environment variable
    client := arkruntime.NewClientWithApiKey(
        // Get your API Key from the environment variable. This is the default mode and you can modify it as required
        os.Getenv("ARK_API_KEY"),
        // The base URL for model invocation
        arkruntime.WithBaseUrl("https://ark.ap-southeast.bytepluses.com/api/v3"),
    )
    ctx := context.Background()
    // Replace with Model ID
    modelEp := "seedance-1-5-pro-251215"

    // Generate a task
    fmt.Println("----- create request -----")
    createReq := model.CreateContentGenerationTaskRequest{
        Model: modelEp,
        GenerateAudio: byteplus.Bool(true),
        Ratio:         byteplus.String("adaptive"),
        Duration:      byteplus.Int64(5),
        Watermark:     byteplus.Bool(false),
        Content: []*model.CreateContentGenerationContentItem{
            {
                // Combination of text prompt and parameters
                Type: model.ContentGenerationContentItemTypeText,
                Text: byteplus.String("A girl holding a fox, the girl opens her eyes, looks gently at the camera, the fox hugs affectionately, the camera slowly pulls out, the girl’s hair is blown by the wind, and the sound of the wind can be heard"),
            },
            {
                // The URL of the first frame image
                Type: model.ContentGenerationContentItemTypeImage,
                ImageURL: &model.ImageURL{
                    URL: "https://ark-doc.tos-ap-southeast-1.bytepluses.com/doc_image/i2v_foxrgirl.png",
                },
            },
        },
    }
    createResp, err := client.CreateContentGenerationTask(ctx, createReq)
    if err != nil {
        fmt.Printf("create content generation error: %v", err)
        return
    }
    taskID := createResp.ID
    fmt.Printf("Task Created with ID: %s", taskID)

    // Polling query section
    fmt.Println("----- polling task status -----")
    for {
        getReq := model.GetContentGenerationTaskRequest{ID: taskID}
        getResp, err := client.GetContentGenerationTask(ctx, getReq)
        if err != nil {
            fmt.Printf("get content generation task error: %v", err)
            return
        }

        status := getResp.Status
        if status == "succeeded" {
            fmt.Println("----- task succeeded -----")
            fmt.Printf("Task ID: %s \\n", getResp.ID)
            fmt.Printf("Model: %s \\n", getResp.Model)
            fmt.Printf("Video URL: %s \\n", getResp.Content.VideoURL)
            fmt.Printf("Completion Tokens: %d \\n", getResp.Usage.CompletionTokens)
            fmt.Printf("Created At: %d, Updated At: %d", getResp.CreatedAt, getResp.UpdatedAt)
            return
        } else if status == "failed" {
            fmt.Println("----- task failed -----")
            if getResp.Error != nil {
                fmt.Printf("Error Code: %s, Message: %s", getResp.Error.Code, getResp.Error.Message)
            }
            return
        } else {
            fmt.Printf("Current status: %s, Retrying in 10 seconds... \\n", status)
            time.Sleep(10 * time.Second)
        }
    }
}
\`\`\`

`}></RenderMd></Tabs.TabPane></Tabs>);
 ```

<span id="0d55ca07"></span>
## **Image-to-video based on the first and last frame (w**ith audio)
By specifying the starting and ending images of the video, the model can generate video that smoothly transitions between the first and last frames, achieving natural and coherent transitions between scenes.
Seedance 1.5 pro can generate audio videos by setting the parameter **generate_audio** to `true`.

| | | | | \
|Prompt |\
|<div style="width:150px"></div> |First frame |\
| |<div style="width:150px"></div> |Last frame |\
| | |<div style="width:150px"></div> |Output |
|---|---|---|---|
| | | | | \
|Create a 360-degree orbiting camera shot based on this photo. |![Image](https://p9-arcosite.byteimg.com/tos-cn-i-goo7wpa0wc/649cb2057eae48d6a6eec872d912c75c~tplv-goo7wpa0wc-image.image =2048x) |\
| | |![Image](https://p9-arcosite.byteimg.com/tos-cn-i-goo7wpa0wc/e39fd8e500a34bbdad50d06659c4ea6b~tplv-goo7wpa0wc-image.image =2048x) |\
| | | |<BytedReactXgplayer config={{ url: 'https://p9-arcosite.byteimg.com/tos-cn-i-goo7wpa0wc/fce78e0b735946f3b68fcca4f478befe~tplv-goo7wpa0wc-image.image', poster: 'https://p9-arcosite.byteimg.com/tos-cn-i-goo7wpa0wc/fce78e0b735946f3b68fcca4f478befe~tplv-goo7wpa0wc-video-poster.jpeg' }} ></BytedReactXgplayer> |\
| | | | |


```mixin-react
return (<Tabs>
<Tabs.TabPane title="Python" key="H45NmsbrqK"><RenderMd content={`\`\`\`Python
import os
import time  
# Install SDK:  pip install byteplus-python-sdk-v2 
from byteplussdkarkruntime import Ark 

# Make sure that you have stored the API Key in the environment variable ARK_API_KEY
# Initialize the Ark client to read your API Key from an environment variable
client = Ark(
    # This is the default path. You can configure it based on the service location
    base_url="https://ark.ap-southeast.bytepluses.com/api/v3",
    # Get API Key：https://console.byteplus.com/ark/region:ark+ap-southeast-1/apikey
    api_key=os.environ.get("ARK_API_KEY"),
)  


if __name__ == "__main__": 
    print("----- create request -----") 
    create_result = client.content_generation.tasks.create( 
        model="seedance-1-5-pro-251215", # Replace with Model ID
        content=[ 
            { 
                # Combination of text prompt and parameters
                "type": "text", 
                "text": "The girl in the frame says “Cheese” to the camera, with a 360-degree orbiting camera shot"
            }, 
            { 
                # The URL of the first frame image
                "type": "image_url", 
                "image_url": { 
                    "url": "https://ark-doc.tos-ap-southeast-1.bytepluses.com/doc_image/seepro_first_frame.jpeg"
                },
                "role": "first_frame"
            }, 
            { 
                # The URL of the last frame image  
                "type": "image_url", 
                "image_url": { 
                    "url": "https://ark-doc.tos-ap-southeast-1.bytepluses.com/doc_image/seepro_last_frame.jpeg"
                },
                "role": "last_frame"  
            } 
        ],
        generate_audio=True,
        ratio="adaptive",
        duration=5,
        watermark=False,
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
            print(f"Error: {get_result.error}") 
            break 
        else: 
            print(f"Current status: {status}, Retrying after 10 seconds...") 
            time.sleep(10)
\`\`\`

`}></RenderMd></Tabs.TabPane>
<Tabs.TabPane title="Java" key="EARpG98o0q"><RenderMd content={`\`\`\`Java
package com.ark.sample;

import com.byteplus.ark.runtime.model.content.generation.*;
import com.byteplus.ark.runtime.model.content.generation.CreateContentGenerationTaskRequest.Content;
import com.byteplus.ark.runtime.service.ArkService;
import okhttp3.ConnectionPool;
import okhttp3.Dispatcher;

import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.TimeUnit;

public class ContentGenerationTaskExample {
    // Make sure that you have stored the API Key in the environment variable ARK_API_KEY
    // Initialize the Ark client to read your API Key from an environment variable
    static String apiKey = System.getenv("ARK_API_KEY");
    static ConnectionPool connectionPool = new ConnectionPool(5, 1, TimeUnit.SECONDS);
    static Dispatcher dispatcher = new Dispatcher();
    static ArkService service = ArkService.builder()
           .baseUrl("https://ark.ap-southeast.bytepluses.com/api/v3") // The base URL for model invocation
           .dispatcher(dispatcher)
           .connectionPool(connectionPool)
           .apiKey(apiKey)
           .build();
           
    public static void main(String[] args) {
        String model = "seedance-1-5-pro-251215"; // Replace with Model ID
        Boolean generateAudio = true;
        String ratio = "adaptive";
        Long duration = 5L;
        Boolean watermark = false;
        System.out.println("----- create request -----");
        List<Content> contents = new ArrayList<>();
        
        // Combination of text prompt and parameters
        contents.add(Content.builder()
                .type("text")
                .text("The girl in the frame says “Cheese” to the camera, with a 360-degree orbiting camera shot")
                .build());
         // The URL of the first frame image
        contents.add(Content.builder()
                .type("image_url")
                .imageUrl(CreateContentGenerationTaskRequest.ImageUrl.builder()
                        .url("https://ark-doc.tos-ap-southeast-1.bytepluses.com/doc_image/seepro_first_frame.jpeg")
                        .build())
                .role("first_frame")
                .build());

        // The URL of the last frame image
        contents.add(Content.builder()
                .type("image_url")
                .imageUrl(CreateContentGenerationTaskRequest.ImageUrl.builder()
                        .url("https://ark-doc.tos-ap-southeast-1.bytepluses.com/doc_image/seepro_last_frame.jpeg")
                        .build())
                .role("last_frame")
                .build());

        // Create a video generation task
        CreateContentGenerationTaskRequest createRequest = CreateContentGenerationTaskRequest.builder()
                .model(model)
                .content(contents)
                .generateAudio(generateAudio)
                .ratio(ratio)
                .duration(duration)
                .watermark(watermark)
                .build();

        CreateContentGenerationTaskResult createResult = service.createContentGenerationTask(createRequest);
        System.out.println(createResult);

        // Get the details of the task
        String taskId = createResult.getId();
        GetContentGenerationTaskRequest getRequest = GetContentGenerationTaskRequest.builder()
                .taskId(taskId)
                .build();
        
        // Polling query section
        System.out.println("----- polling task status -----");
        while (true) {
            try {
                GetContentGenerationTaskResponse getResponse = service.getContentGenerationTask(getRequest);
                String status = getResponse.getStatus();
                if ("succeeded".equalsIgnoreCase(status)) {
                    System.out.println("----- task succeeded -----");
                    System.out.println(getResponse);
                    break;
                } else if ("failed".equalsIgnoreCase(status)) {
                    System.out.println("----- task failed -----");
                    System.out.println("Error: " + getResponse.getStatus());
                    break;
                } else {
                    System.out.printf("Current status: %s, Retrying in 10 seconds...", status);
                    TimeUnit.SECONDS.sleep(10);
                }
            } catch (InterruptedException ie) {
                Thread.currentThread().interrupt();
                System.err.println("Polling interrupted");
                break;
            }
        }
    }
} 
\`\`\`

`}></RenderMd></Tabs.TabPane>
<Tabs.TabPane title="Go" key="SJgEdY8w9n"><RenderMd content={`\`\`\`Go
package main

import (
    "context"
    "fmt"
    "os"
    "time"

    "github.com/byteplus-sdk/byteplus-go-sdk-v2/service/arkruntime"
    "github.com/byteplus-sdk/byteplus-go-sdk-v2/service/arkruntime/model"
    "github.com/byteplus-sdk/byteplus-go-sdk-v2/byteplus"
)

func main() {
    // Make sure that you have stored the API Key in the environment variable ARK_API_KEY
    // Initialize the Ark client to read your API Key from an environment variable
    client := arkruntime.NewClientWithApiKey(
        // Get your API Key from the environment variable. This is the default mode and you can modify it as required
        os.Getenv("ARK_API_KEY"),
        // The base URL for model invocation
        arkruntime.WithBaseUrl("https://ark.ap-southeast.bytepluses.com/api/v3"),
    )
    ctx := context.Background()
    // Replace with Model ID
    modelEp := "seedance-1-5-pro-251215"

    // Generate a task
    fmt.Println("----- create request -----")
    createReq := model.CreateContentGenerationTaskRequest{
        Model: modelEp,
        GenerateAudio: byteplus.Bool(true),
        Ratio:         byteplus.String("adaptive"),
        Duration:      byteplus.Int64(5),
        Watermark:     byteplus.Bool(false),
        Content: []*model.CreateContentGenerationContentItem{
            {
                // Combination of text prompt and parameters
                Type: model.ContentGenerationContentItemTypeText,
                Text: byteplus.String("The girl in the frame says “Cheese” to the camera, with a 360-degree orbiting camera shot"),
            },
            {
                // The URL of the first frame image
                Type: model.ContentGenerationContentItemTypeImage,
                ImageURL: &model.ImageURL{
                    URL: "https://ark-doc.tos-ap-southeast-1.bytepluses.com/doc_image/seepro_first_frame.jpeg", 
                },
                Role: byteplus.String("first_frame"),
            },
            {
                // The URL of the last frame image
                Type: model.ContentGenerationContentItemTypeImage,
                ImageURL: &model.ImageURL{
                    URL: "https://ark-doc.tos-ap-southeast-1.bytepluses.com/doc_image/seepro_last_frame.jpeg", 
                },
                Role: byteplus.String("last_frame"),
            },
        },
    }
    createResp, err := client.CreateContentGenerationTask(ctx, createReq)
    if err != nil {
        fmt.Printf("create content generation error: %v", err)
        return
    }
    taskID := createResp.ID
    fmt.Printf("Task Created with ID: %s", taskID)

    // Polling query section
    fmt.Println("----- polling task status -----")
    for {
        getReq := model.GetContentGenerationTaskRequest{ID: taskID}
        getResp, err := client.GetContentGenerationTask(ctx, getReq)
        if err != nil {
            fmt.Printf("get content generation task error: %v", err)
            return
        }

        status := getResp.Status
        if status == "succeeded" {
            fmt.Println("----- task succeeded -----")
            fmt.Printf("Task ID: %s \\n", getResp.ID)
            fmt.Printf("Model: %s \\n", getResp.Model)
            fmt.Printf("Video URL: %s \\n", getResp.Content.VideoURL)
            fmt.Printf("Completion Tokens: %d \\n", getResp.Usage.CompletionTokens)
            fmt.Printf("Created At: %d, Updated At: %d", getResp.CreatedAt, getResp.UpdatedAt)
            return
        } else if status == "failed" {
            fmt.Println("----- task failed -----")
            if getResp.Error != nil {
                fmt.Printf("Error Code: %s, Message: %s", getResp.Error.Code, getResp.Error.Message)
            }
            return
        } else {
            fmt.Printf("Current status: %s, Retrying in 10 seconds... \\n", status)
            time.Sleep(10 * time.Second)
        }
    }
}
\`\`\`

`}></RenderMd></Tabs.TabPane></Tabs>);
 ```

<span id="c5f5f577"></span>
## Image-to-video (based on image reference)
The model can accurately extract key features of objects from reference images (supporting 1 to 4 images). Based on these features, it faithfully reproduces object shapes, colors, and texture details during video generation, ensuring visual consistency with the style of the reference images.

| | | | | | \
|Prompt |\
|<div style="width:150px"></div> |Reference image 1 |\
| |<div style="width:100px"></div> |Reference image 2 |\
| | |<div style="width:100px"></div> |Reference image 3 |\
| | | |<div style="width:100px"></div> |Output |
|---|---|---|---|---|
| | | | | | \
|A boy wearing glasses and a blue T-shirt from [Image 1] and a corgi dog from [Image 2], sitting on the lawn from [Image 3], in 3D cartoon style |![Image](https://p9-arcosite.byteimg.com/tos-cn-i-goo7wpa0wc/2637ac87f1e64bd897bfc651fe7d0386~tplv-goo7wpa0wc-image.image =222x) |![Image](https://p9-arcosite.byteimg.com/tos-cn-i-goo7wpa0wc/9450c9444b574112a9f228db9e81cdf4~tplv-goo7wpa0wc-image.image =222x) |![Image](https://p9-arcosite.byteimg.com/tos-cn-i-goo7wpa0wc/574b8785f4b740ddaff791655e8633ba~tplv-goo7wpa0wc-image.image =222x) |<BytedReactXgplayer config={{ url: 'https://p9-arcosite.byteimg.com/tos-cn-i-goo7wpa0wc/2a7739fb339d4390ad79c40f450f8ac7~tplv-goo7wpa0wc-image.image', poster: 'https://p9-arcosite.byteimg.com/tos-cn-i-goo7wpa0wc/2a7739fb339d4390ad79c40f450f8ac7~tplv-goo7wpa0wc-video-poster.jpeg' }} ></BytedReactXgplayer> |\
| | | | | |


```mixin-react
return (<Tabs>
<Tabs.TabPane title="Python" key="CJ3lyrPzbh"><RenderMd content={`\`\`\`Python
import os
import time  
# Install SDK:  pip install byteplus-python-sdk-v2 
from byteplussdkarkruntime import Ark 

# Make sure that you have stored the API Key in the environment variable ARK_API_KEY
# Initialize the Ark client to read your API Key from an environment variable
client = Ark(
    # This is the default path. You can configure it based on the service location
    base_url="https://ark.ap-southeast.bytepluses.com/api/v3",
    # Get API Key：https://console.byteplus.com/ark/region:ark+ap-southeast-1/apikey
    api_key=os.environ.get("ARK_API_KEY"),
)
  
if __name__ == "__main__": 
    print("----- create request -----") 
    try:
        create_result = client.content_generation.tasks.create( 
            model="seedance-1-0-lite-i2v-250428",  # Replace with Model ID 
            content=[ 
                { 
                    # Combination of text prompt and parameters 
                    "type": "text", 
                    "text": "A boy wearing glasses and a blue T-shirt from [Image 1] and a corgi dog from [Image 2], sitting on the lawn from [Image 3], in 3D cartoon style" 
                },
                { 
                    # The URL of the first reference image  
                    # 1-4 reference images need to be provided
                    "type": "image_url", 
                    "image_url": { 
                        "url": "https://ark-doc.tos-ap-southeast-1.bytepluses.com/doc_image/seelite_ref_1.png"
                    },
                    "role": "reference_image"  
                },
                { 
                    # The URL of the second reference image  
                    "type": "image_url", 
                    "image_url": { 
                        "url": "https://ark-doc.tos-ap-southeast-1.bytepluses.com/doc_image/seelite_ref_2.png" 
                    },
                    "role": "reference_image"  
                },
                { 
                    # The URL of the third reference image  
                    "type": "image_url", 
                    "image_url": { 
                        "url": "https://ark-doc.tos-ap-southeast-1.bytepluses.com/doc_image/seelite_ref_3.png" 
                    },
                    "role": "reference_image"  
                } 
            ],
            ratio="16:9",
            duration=5,
            watermark=False,
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
                print(f"Error: {get_result.error}") 
                break 
            else: 
                print(f"Current status: {status}, Retrying after 10 seconds...") 
                time.sleep(10)
    except Exception as e:
        print(f"An error occurred: {e}")
\`\`\`

`}></RenderMd></Tabs.TabPane>
<Tabs.TabPane title="Java" key="rIx7CjXdsj"><RenderMd content={`\`\`\`Java
package com.ark.sample;

import com.byteplus.ark.runtime.model.content.generation.*;
import com.byteplus.ark.runtime.model.content.generation.CreateContentGenerationTaskRequest.Content;
import com.byteplus.ark.runtime.service.ArkService;
import okhttp3.ConnectionPool;
import okhttp3.Dispatcher;

import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.TimeUnit;


public class ContentGenerationTaskExample {
    // Make sure that you have stored the API Key in the environment variable ARK_API_KEY
    // Initialize the Ark client to read your API Key from an environment variable
    static String apiKey = System.getenv("ARK_API_KEY");
    static ConnectionPool connectionPool = new ConnectionPool(5, 1, TimeUnit.SECONDS);
    static Dispatcher dispatcher = new Dispatcher();
    static ArkService service = ArkService.builder()
           .baseUrl("https://ark.ap-southeast.bytepluses.com/api/v3") // The base URL for model invocation
           .dispatcher(dispatcher)
           .connectionPool(connectionPool)
           .apiKey(apiKey)
           .build();
           
    public static void main(String[] args) {
        String model = "seedance-1-0-lite-i2v-250428"; // Replace with Model ID
        String ratio = "16:9";
        Long duration = 5L;
        Boolean watermark = false;
        System.out.println("----- create request -----");
        List<Content> contents = new ArrayList<>();
        
        // Combination of text prompt and parameters
        contents.add(Content.builder()
                .type("text")
                .text("A boy wearing glasses and a blue T-shirt from [Image 1] and a corgi dog from [Image 2], sitting on the lawn from [Image 3], in 3D cartoon style") 
                .build());
        // The URL of the first reference image 
        contents.add(Content.builder() 
                .type("image_url") 
                .imageUrl(CreateContentGenerationTaskRequest.ImageUrl.builder() 
                        .url("https://ark-doc.tos-ap-southeast-1.bytepluses.com/doc_image/seelite_ref_1.png") 
                        .build()) 
                .role("reference_image") 
                .build()); 
        // The URL of the second reference image 
        contents.add(Content.builder() 
                .type("image_url") 
                .imageUrl(CreateContentGenerationTaskRequest.ImageUrl.builder() 
                        .url("https://ark-doc.tos-ap-southeast-1.bytepluses.com/doc_image/seelite_ref_2.png") 
                        .build()) 
                .role("reference_image") 
                .build()); 
        // The URL of the third reference image 
        contents.add(Content.builder() 
                .type("image_url") 
                .imageUrl(CreateContentGenerationTaskRequest.ImageUrl.builder() 
                        .url("https://ark-doc.tos-ap-southeast-1.bytepluses.com/doc_image/seelite_ref_3.png")
                        .build()) 
                .role("reference_image") 
                .build()); 

        // Create a video generation task
        CreateContentGenerationTaskRequest createRequest = CreateContentGenerationTaskRequest.builder()
                .model(model)
                .content(contents)
                .ratio(ratio)
                .duration(duration)
                .watermark(watermark)
                .build();

        CreateContentGenerationTaskResult createResult = service.createContentGenerationTask(createRequest);
        System.out.println(createResult);

        // Get the details of the task
        String taskId = createResult.getId();
        GetContentGenerationTaskRequest getRequest = GetContentGenerationTaskRequest.builder()
                .taskId(taskId)
                .build();
        
        // Polling query section
        System.out.println("----- polling task status -----");
        while (true) {
            try {
                GetContentGenerationTaskResponse getResponse = service.getContentGenerationTask(getRequest);
                String status = getResponse.getStatus();
                if ("succeeded".equalsIgnoreCase(status)) {
                    System.out.println("----- task succeeded -----");
                    System.out.println(getResponse);
                    break;
                } else if ("failed".equalsIgnoreCase(status)) {
                    System.out.println("----- task failed -----");
                    System.out.println("Error: " + getResponse.getStatus());
                    break;
                } else {
                    System.out.printf("Current status: %s, Retrying in 10 seconds...", status);
                    TimeUnit.SECONDS.sleep(10);
                }
            } catch (InterruptedException ie) {
                Thread.currentThread().interrupt();
                System.err.println("Polling interrupted");
                break;
            }
        }
    }
}
\`\`\`

`}></RenderMd></Tabs.TabPane>
<Tabs.TabPane title="Go" key="patfkMQVjq"><RenderMd content={`\`\`\`Go
package main

import (
    "context"
    "fmt"
    "os"
    "time"

    "github.com/byteplus-sdk/byteplus-go-sdk-v2/service/arkruntime"
    "github.com/byteplus-sdk/byteplus-go-sdk-v2/service/arkruntime/model"
    "github.com/byteplus-sdk/byteplus-go-sdk-v2/byteplus"
)

func main() {
    // Make sure that you have stored the API Key in the environment variable ARK_API_KEY
    // Initialize the Ark client to read your API Key from an environment variable
    client := arkruntime.NewClientWithApiKey(
        // Get your API Key from the environment variable. This is the default mode and you can modify it as required
        os.Getenv("ARK_API_KEY"),
        // The base URL for model invocation
        arkruntime.WithBaseUrl("https://ark.ap-southeast.bytepluses.com/api/v3"),
    )
    ctx := context.Background()
    // Replace with Model ID
    modelEp := "seedance-1-0-lite-i2v-250428"

    // Generate a task
    fmt.Println("----- create request -----")
    createReq := model.CreateContentGenerationTaskRequest{
        Model: modelEp,
        Ratio:         byteplus.String("16:9"),
        Duration:      byteplus.Int64(5),
        Watermark:     byteplus.Bool(false),
        Content: []*model.CreateContentGenerationContentItem{
            {
                // Combination of text prompt and parameters
                Type: model.ContentGenerationContentItemTypeText,
                Text: byteplus.String("A boy wearing glasses and a blue T-shirt from [Image 1] and a corgi dog from [Image 2], sitting on the lawn from [Image 3], in 3D cartoon style"),
            },
            {
                // The URL of the first reference image  
                // # 1-4 reference images need to be provided
                Type: model.ContentGenerationContentItemTypeImage,
                ImageURL: &model.ImageURL{
                    URL: "https://ark-doc.tos-ap-southeast-1.bytepluses.com/doc_image/seelite_ref_1.png",
                },
                Role: byteplus.String("reference_image"),
            },
            {
                // The URL of the second reference image  
                Type: model.ContentGenerationContentItemTypeImage,
                ImageURL: &model.ImageURL{
                    URL: "https://ark-doc.tos-ap-southeast-1.bytepluses.com/doc_image/seelite_ref_2.png",
                },
                Role: byteplus.String("reference_image"),
            },
            {
                // The URL of the third reference image  
                Type: model.ContentGenerationContentItemTypeImage,
                ImageURL: &model.ImageURL{
                    URL: "https://ark-doc.tos-ap-southeast-1.bytepluses.com/doc_image/seelite_ref_3.png",
                },
                Role: byteplus.String("reference_image"),
            },
        },
    }
    
    createResp, err := client.CreateContentGenerationTask(ctx, createReq)
    if err != nil {
        fmt.Printf("create content generation error: %v", err)
        return
    }
    taskID := createResp.ID
    fmt.Printf("Task Created with ID: %s", taskID)

    // Polling query section
    fmt.Println("----- polling task status -----")
    for {
        getReq := model.GetContentGenerationTaskRequest{ID: taskID}
        getResp, err := client.GetContentGenerationTask(ctx, getReq)
        if err != nil {
            fmt.Printf("get content generation task error: %v", err)
            return
        }

        status := getResp.Status
        if status == "succeeded" {
            fmt.Println("----- task succeeded -----")
            fmt.Printf("Task ID: %s \\n", getResp.ID)
            fmt.Printf("Model: %s \\n", getResp.Model)
            fmt.Printf("Video URL: %s \\n", getResp.Content.VideoURL)
            fmt.Printf("Completion Tokens: %d \\n", getResp.Usage.CompletionTokens)
            fmt.Printf("Created At: %d, Updated At: %d", getResp.CreatedAt, getResp.UpdatedAt)
            return
        } else if status == "failed" {
            fmt.Println("----- task failed -----")
            if getResp.Error != nil {
                fmt.Printf("Error Code: %s, Message: %s", getResp.Error.Code, getResp.Error.Message)
            }
            return
        } else {
            fmt.Printf("Current status: %s, Retrying in 10 seconds... \\n", status)
            time.Sleep(10 * time.Second)
        }
    }
}
\`\`\`

`}></RenderMd></Tabs.TabPane></Tabs>);
 ```


<span id="68fd42bf"></span>
## Manage video tasks
<span id="360a1a86"></span>
### Query a list of video generation tasks
This API supports passing filter parameters to query the list of video generation tasks that meet the specified criteria.

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

`}></RenderMd></Tabs.TabPane>
<Tabs.TabPane title="Java" key="NbZ6YoGJjO"><RenderMd content={`\`\`\`Java
package com.ark.sample;

import com.fasterxml.jackson.core.JsonProcessingException;
import com.byteplus.ark.runtime.model.content.generation.ListContentGenerationTasksRequest;
import com.byteplus.ark.runtime.service.ArkService;
import java.util.concurrent.TimeUnit;
import okhttp3.ConnectionPool;
import okhttp3.Dispatcher;


public class Sample {

    static String apiKey = System.getenv("ARK_API_KEY");

    static ConnectionPool connectionPool = new ConnectionPool(5, 1, TimeUnit.SECONDS);
    static Dispatcher dispatcher = new Dispatcher();
    static ArkService service =
            ArkService.builder()
                    .dispatcher(dispatcher)
                    .connectionPool(connectionPool)
                    .apiKey(apiKey)
                    .build();

    public static void main(String[] args) throws JsonProcessingException {

        ListContentGenerationTasksRequest req =
                ListContentGenerationTasksRequest.builder().pageSize(3).status("succeeded").build();

        service.listContentGenerationTasks(req).toString();
        System.out.println(service.getContentGenerationTask(req));

        // shutdown service after all requests is finished
        service.shutdownExecutor();
    }
}
\`\`\`

`}></RenderMd></Tabs.TabPane>
<Tabs.TabPane title="Go" key="dN8uLd8DWE"><RenderMd content={`\`\`\`Go
package main

import (
    "context"
    "fmt"
    "os"

    "github.com/byteplus-sdk/byteplus-go-sdk-v2/service/arkruntime"
    "github.com/byteplus-sdk/byteplus-go-sdk-v2/service/arkruntime/model"
    "github.com/byteplus-sdk/byteplus-go-sdk-v2/byteplus"
)

func main() {
        client := arkruntime.NewClientWithApiKey(os.Getenv("ARK_API_KEY"))
        ctx := context.Background()

        req := model.ListContentGenerationTasksRequest{
                PageSize: byteplus.Int(3),
                Filter: &model.ListContentGenerationTasksFilter{
                        Status: byteplus.String("succeeded"),
                },
        }

        resp, err := client.ListContentGenerationTasks(ctx, req)
        if err != nil {
                fmt.Printf("failed to list content generation tasks: %v\\n", err)
                return
        }
        fmt.Printf("%+v\\n", resp)
}
\`\`\`

`}></RenderMd></Tabs.TabPane></Tabs>);
 ```

<span id="64914c89"></span>
### Delete or cancel video generation tasks
Cancel video generation tasks in the queue, or delete video generation task records.

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
        print(f"failed to delete task: {e}")
\`\`\`

`}></RenderMd></Tabs.TabPane>
<Tabs.TabPane title="Java" key="mB4D2jmlnj"><RenderMd content={`\`\`\`Java
package com.ark.sample;

import com.fasterxml.jackson.core.JsonProcessingException;
import com.byteplus.ark.runtime.model.content.generation.DeleteContentGenerationTasksRequest;
import com.byteplus.ark.runtime.service.ArkService;
import java.util.concurrent.TimeUnit;
import okhttp3.ConnectionPool;
import okhttp3.Dispatcher;

public class Sample {

    static String apiKey = System.getenv("ARK_API_KEY");

    static ConnectionPool connectionPool = new ConnectionPool(5, 1, TimeUnit.SECONDS);
    static Dispatcher dispatcher = new Dispatcher();
    static ArkService service =
            ArkService.builder()
                    .dispatcher(dispatcher)
                    .connectionPool(connectionPool)
                    .apiKey(apiKey)
                    .build();

    public static void main(String[] args) throws JsonProcessingException {

        DeleteContentGenerationTaskRequest req =
                DeleteContentGenerationTaskRequest.builder()
                        .taskId("cgt-2025****")
                        .build();

        service.deleteContentGenerationTask(req).toString();

        service.shutdownExecutor();
    }
}
\`\`\`

`}></RenderMd></Tabs.TabPane>
<Tabs.TabPane title="Go" key="bSbtfIfSyV"><RenderMd content={`\`\`\`Go
package main

import (
        "context"
        "fmt"
        "os"

        "github.com/byteplus-sdk/byteplus-go-sdk-v2/service/arkruntime"
        "github.com/byteplus-sdk/byteplus-go-sdk-v2/service/arkruntime/model"
)


func main() {
        client := arkruntime.NewClientWithApiKey(os.Getenv("ARK_API_KEY"))
        ctx := context.Background()

        req := model.DeleteContentGenerationTaskRequest{
                ID: "cgt-2025****",
        }
        err := client.DeleteContentGenerationTask(ctx, req)
        if err != nil {
                fmt.Printf("delete content generation task error: %v\\n", err)
                return
        }
}
\`\`\`

`}></RenderMd></Tabs.TabPane></Tabs>);
 ```

<span id="9fe4cce0"></span>
## **Configure video output specifications [New]**
 You can control video output specifications using parameters such as `resolution`, `ratio`, `duration`, `frames`, `seed`, `camera_fixed`, and `watermark`. 
:::warning
The supported parameters may vary slightly by model, as detailed in the table below. When the input parameters or values do not match the selected model, the parameter will either be ignored or trigger an error.

* **New method: Pass the** **paramters directly** **in the request body.** This method uses strict validation—if a parameter is incorrect, the model will return an error prompt.
* **Legacy method: Append --[parameter] after the text prompt.** This method uses lenient validation—if a parameter is incorrect, the model will automatically use the default value without raising an error.
:::

* **New method (Recommended): Pass the** **paramters directly** **in the request body**
   ```JSON
   ...
      // Strongly recommended
      // Specify the aspect ratio of the generated video as 16:9, duration as 5 seconds, resolution as 720p, seed as 11, and include a watermark. The camera is not fixed.
       "model": "seedance-1-5-pro-251215",
       "content": [
           {
               "type": "text",
               "text": "The kitten is yawning at the camera"
           }
       ],
       // All parameters must be written in full; abbreviations are not supported
       "resolution": "720p",
       "ratio":"16:9",
       "duration": 5,
       // "frames": 29, Either duration or frames is required
       "seed": 11,
       "camera_fixed": false,
       "watermark": true
   ...
   ```

* **Legacy method: Append --[parameter] to the text prompt**
   ```JSON
   ...
   // Specify the aspect ratio of the generated video to 16:9, duration to 5 seconds, resolution to 720p, seed to 11, and include a watermark. The camera is not fixed.
   "content": [
           {
               "type": "text",
               "text": "The kitten is yawning at the camera --rs 720p --rt 16:9 --dur 5 --seed 11 --cf false --wm true"
               // "text": "The kitten is yawning at the camera --resolution 720p --ratio 16:9 --duration 5 --seed 11 --camerafixed false --watermark true"
           }
    ]
    ...
   ```




| | | | | \
|<div style="width:100px"></div> |seedance-1-5-pro |\
| |<div style="width:150px"></div> |seedance-1-0-pro |\
| | |seedance-1-0-pro-fast |\
| | |<div style="width:150px"></div> |seedance-1-0-lite-t2v |\
| | | |seedance-1-0-lite-i2v |\
| | | |<div style="width:150px"></div> |
|---|---|---|---|
| | | | | \
|**resolution** |\
| |* 480p |\
| |* 720p |\
| |* 1080p |* 480p |\
| | |* 720p |\
| | |* 1080p |* 480P |\
| | | |* 720P |\
| | | |* 1080p`Not supported by the reference image feature` |
| | | | | \
|**ratio** |\
| |* 16:9  |\
| |* 4:3 |\
| |* 1:1 |\
| |* 3:4 |\
| |* 9:16 |\
| |* 21:9 |\
| |* adaptive |\
| | |\
| | |\
| |--- |\
| | |\
| | |\
| | |\
| |* 480p: The resolutions corresponding to different video aspect ratios: |\
| |   * `16:9`：864×496  |\
| |   * `4:3`：752×560 |\
| |   * `1:1`：640×640  |\
| |   * `3:4`：560×752  |\
| |   * `9:16`：496×864  |\
| |   * `21:9`：992×432 |\
| | |\
| | |\
| |--- |\
| | |\
| | |\
| | |\
| |* 720p: The resolutions corresponding to different video aspect ratios: |\
| |   * `16:9`：1280×720 |\
| |   * `4:3`：1112×834 |\
| |   * `1:1`：960×960 |\
| |   * `3:4`：834×1112 |\
| |   * `9:16`：720×1280 |\
| |   * `21:9`：1470×630 |\
| | |\
| | |\
| |--- |\
| | |\
| | |\
| | |\
| |* 1080p: The resolutions corresponding to different video aspect ratios: |\
| |* `16:9`：1920×1080 |\
| |* `4:3`：1664×1248 |\
| |* `1:1`：1440×1440 |\
| |* `3:4`：1248×1664 |\
| |* `9:16`：1080×1920 |\
| |* `21:9`：2206×946 |* 16:9  |\
| | |* 4:3 |\
| | |* 1:1 |\
| | |* 3:4 |\
| | |* 9:16 |\
| | |* 21:9 |\
| | |* adaptive`Not supported by the text-to-video feature` |\
| | | |\
| | | |\
| | |--- |\
| | | |\
| | | |\
| | | |\
| | |* 480p: The resolutions corresponding to different video aspect ratios: |\
| | |   * `16:9`：864×480 |\
| | |   * `4:3`：736×544 |\
| | |   * `1:1`：640×640 |\
| | |   * `3:4`：544×736 |\
| | |   * `9:16`：480×864 |\
| | |   * `21:9`：960×416 |\
| | | |\
| | | |\
| | |--- |\
| | | |\
| | | |\
| | | |\
| | |* 720p: The resolutions corresponding to different video aspect ratios: |\
| | |   * `16:9`: 1248 × 704 |\
| | |   * `4:3`: 1120 × 832 |\
| | |   * `1:1`: 960 × 960 |\
| | |   * `3:4`: 832 × 1120 |\
| | |   * `9:16`: 704 × 1248 |\
| | |   * `21:9`：1504×640 |\
| | | |\
| | | |\
| | |--- |\
| | | |\
| | | |\
| | | |\
| | |* 1080p: The resolutions corresponding to different video aspect ratios: |\
| | |   * `16:9`：1920×1088 |\
| | |   * `4:3`：1664×1248 |\
| | |   * `1:1`：1440×1440 |\
| | |   * `3:4`：1248×1664 |\
| | |   * `9:16`：1088×1920 |\
| | |   * `21:9`：2176×928 |* 16:9  |\
| | | |* 4:3 |\
| | | |* 1:1 |\
| | | |* 3:4 |\
| | | |* 9:16 |\
| | | |* 21:9 |\
| | | |* adaptive`Not supported by the text-to-video and reference image feature` |\
| | | | |\
| | | | |\
| | | |--- |\
| | | | |\
| | | | |\
| | | | |\
| | | |* 480p: The resolutions corresponding to different video aspect ratios: |\
| | | |   * `16:9`: 864 × 480 |\
| | | |   * `4:3`: 736 × 544 |\
| | | |   * `1:1`: 640 × 640 |\
| | | |   * `3:4`: 544 × 736 |\
| | | |   * `9:16`: 480 × 864 |\
| | | |   * `21:9`：960×416 |\
| | | | |\
| | | | |\
| | | |--- |\
| | | | |\
| | | | |\
| | | | |\
| | | |* 720p: The resolutions corresponding to different video aspect ratios: |\
| | | |   * `16:9`: 1248 × 704 |\
| | | |   * `4:3`: 1120 × 832 |\
| | | |   * `1:1`: 960 × 960 |\
| | | |   * `3:4`: 832 × 1120 |\
| | | |   * `9:16`: 704 × 1248 |\
| | | |   * `21:9`：1504×640 |\
| | | | |\
| | | | |\
| | | |--- |\
| | | | |\
| | | | |\
| | | | |\
| | | |* 1080p: The resolutions corresponding to different video aspect ratios:  |\
| | | |   **Note**: Not supported by the reference image feature |\
| | | |   * `16:9`：1920×1088 |\
| | | |   * `4:3`：1664×1248 |\
| | | |   * `1:1`：1440×1440 |\
| | | |   * `3:4`：1248×1664 |\
| | | |   * `9:16`：1088×1920 |\
| | | |   * `21:9`：2176×928 |
| | | | | \
|**duration** |\
|Duration of the generated video (seconds)  |4 ~12 s | 2~12 s |2~12 s |
| | | | | \
|**frames** |\
|Number of frames |![Image](https://p9-arcosite.byteimg.com/tos-cn-i-goo7wpa0wc/96a134db51ea4e8d83b5c9dccff686c3~tplv-goo7wpa0wc-image.image =25x) |Supports all integer values within the range [29, 289] that conform to the format **25 + 4n**, where **n** is a positive integer. |Supports all integer values within the range [29, 289] that conform to the format **25 + 4n**, where **n** is a positive integer. |
| | | | | \
|**framespersecond** |\
|Frame rate |24 |24 |24 |
| | | | | \
|**seed** |\
|Seed integer |![Image](https://p9-arcosite.byteimg.com/tos-cn-i-goo7wpa0wc/aba4522e4aab46318574c8c3e460d20b~tplv-goo7wpa0wc-image.image =20x) |![Image](https://p9-arcosite.byteimg.com/tos-cn-i-goo7wpa0wc/aba4522e4aab46318574c8c3e460d20b~tplv-goo7wpa0wc-image.image =20x) |![Image](https://p9-arcosite.byteimg.com/tos-cn-i-goo7wpa0wc/aba4522e4aab46318574c8c3e460d20b~tplv-goo7wpa0wc-image.image =20x) |
| | | | | \
|**camerafixed** |\
|Whether to fix the camera |![Image](https://p9-arcosite.byteimg.com/tos-cn-i-goo7wpa0wc/aba4522e4aab46318574c8c3e460d20b~tplv-goo7wpa0wc-image.image =20x) |![Image](https://p9-arcosite.byteimg.com/tos-cn-i-goo7wpa0wc/aba4522e4aab46318574c8c3e460d20b~tplv-goo7wpa0wc-image.image =20x) |![Image](https://p9-arcosite.byteimg.com/tos-cn-i-goo7wpa0wc/aba4522e4aab46318574c8c3e460d20b~tplv-goo7wpa0wc-image.image =20x) |\
| | | | |\
| | | |   **Note**: Not supported by the reference image feature |

<span id="44236b6a"></span>
## Prompt suggestions

* **Prompt = subject + movement , background + movement , camera + movement ...**
* Describe the desired effect using concise and precise natural language.
* For clearer expected results, it is recommended to first use the image generation model to create images that meet the expected criteria, then use image-to-video to generate video clips.
* Text-to-video generation produces highly variable results and can be used to inspire creative ideas.
* When using image-to-video, upload high-definition, high-quality images, because the quality of the uploaded images has a significant impact on image-to-video results.
* When the generated video does not meet expectations, it is recommended to modify the prompt by replacing abstract descriptions with concrete ones, removing unimportant parts, and placing important content first.
* For more prompt usage tips, please refer to [Seedance-1.5-pro prompt guide](/docs/ModelArk/2168087), [Seedance-1.0-pro&pro-fast prompt guide](/docs/ModelArk/1631633), [Seedance-1.0-lite prompt guide](/docs/ModelArk/1587797).

<span id="e190e738"></span>
# Advanced usage
<span id="df12eb34"></span>
## Offline inference
For scenarios where inference latency sensitivity is low (for example, response times on the order of hours), it is recommended to set **service_tier** to `flex` to switch to offline inference mode with one click—the price is only 50% of online inference, significantly reducing business costs.
Set an appropriate timeout according to the business scenario; tasks will be automatically terminated if they exceed this time.

```mixin-react
return (<Tabs>
<Tabs.TabPane title="Python" key="XewL8xxWV3"><RenderMd content={`\`\`\`Python
import os
import time  
# Install SDK:  pip install byteplus-python-sdk-v2 
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
        model="seedance-1-5-pro-251215", # Replace with Model ID
        content=[
            {
                # Combination of text prompt and parameters
                "type": "text",
                "text": "A girl holding a fox, the girl opens her eyes, looks gently at the camera, the fox hugs affectionately, the camera slowly pulls out, the girl’s hair is blown by the wind"         
            },
            {
                # The URL of the first frame image
                "type": "image_url",
                "image_url": {
                    "url": "https://ark-doc.tos-ap-southeast-1.bytepluses.com/doc_image/i2v_foxrgirl.png" 
                }
            }
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
            print(f"Error: {get_result.error}")
            break
        else:
            print(f"Current status: {status}, Retrying after 60 seconds...")
            time.sleep(60)
\`\`\`

`}></RenderMd></Tabs.TabPane>
<Tabs.TabPane title="Java" key="N3VUOMBOce"><RenderMd content={`\`\`\`Java
package com.ark.sample;

import com.byteplus.ark.runtime.model.content.generation.*;
import com.byteplus.ark.runtime.model.content.generation.CreateContentGenerationTaskRequest.Content;
import com.byteplus.ark.runtime.service.ArkService;
import okhttp3.ConnectionPool;
import okhttp3.Dispatcher;

import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.TimeUnit;

public class ContentGenerationTaskExample {
    // Make sure that you have stored the API Key in the environment variable ARK_API_KEY
    // Initialize the Ark client to read your API Key from an environment variable
    static String apiKey = System.getenv("ARK_API_KEY");
    static ConnectionPool connectionPool = new ConnectionPool(5, 1, TimeUnit.SECONDS);
    static Dispatcher dispatcher = new Dispatcher();
    static ArkService service = ArkService.builder()
           .baseUrl("https://ark.ap-southeast.bytepluses.com/api/v3") // The base URL for model invocation
           .dispatcher(dispatcher)
           .connectionPool(connectionPool)
           .apiKey(apiKey)
           .build();
           
    public static void main(String[] args) {
        String model = "seedance-1-5-pro-251215"; // Replace with Model ID
        String ratio = "adaptive";
        Long duration = 5L;
        Boolean watermark = false;
        String serviceTier = "flex";
        Long executionExpiresAfter = 172800L;
        System.out.println("----- create request -----");
        List<Content> contents = new ArrayList<>();
        
        // Combination of text prompt and parameters
        contents.add(Content.builder()
                .type("text")
                .text("A girl holding a fox, the girl opens her eyes, looks gently at the camera, the fox hugs affectionately, the camera slowly pulls out, the girl's hair is blown by the wind")
                .build());
        // The URL of the first frame image
        contents.add(Content.builder()
                .type("image_url")
                .imageUrl(CreateContentGenerationTaskRequest.ImageUrl.builder()
                        .url("https://ark-doc.tos-ap-southeast-1.bytepluses.com/doc_image/i2v_foxrgirl.png")
                        .build())
                .build());

        // Create a video generation task
        CreateContentGenerationTaskRequest createRequest = CreateContentGenerationTaskRequest.builder()
                .model(model)
                .content(contents)
                .ratio(ratio)
                .duration(duration)
                .watermark(watermark)
                .serviceTier(serviceTier)
                .executionExpiresAfter(executionExpiresAfter)
                .build();

        CreateContentGenerationTaskResult createResult = service.createContentGenerationTask(createRequest);
        System.out.println(createResult);

        // Get the details of the task
        String taskId = createResult.getId();
        GetContentGenerationTaskRequest getRequest = GetContentGenerationTaskRequest.builder()
                .taskId(taskId)
                .build();
        
        // Polling query section
        System.out.println("----- polling task status -----");
        while (true) {
            try {
                GetContentGenerationTaskResponse getResponse = service.getContentGenerationTask(getRequest);
                String status = getResponse.getStatus();
                if ("succeeded".equalsIgnoreCase(status)) {
                    System.out.println("----- task succeeded -----");
                    System.out.println(getResponse);
                    break;
                } else if ("failed".equalsIgnoreCase(status)) {
                    System.out.println("----- task failed -----");
                    System.out.println("Error: " + getResponse.getStatus());
                    break;
                } else {
                    System.out.printf("Current status: %s, Retrying in 60 seconds...", status);
                    TimeUnit.SECONDS.sleep(60);
                }
            } catch (InterruptedException ie) {
                Thread.currentThread().interrupt();
                System.err.println("Polling interrupted");
                break;
            }
        }
    }
}
\`\`\`

`}></RenderMd></Tabs.TabPane>
<Tabs.TabPane title="Go" key="NjaBaFqCMd"><RenderMd content={`\`\`\`Go
package main

import (
    "context"
    "fmt"
    "os"
    "time"

    "github.com/byteplus-sdk/byteplus-go-sdk-v2/service/arkruntime"
    "github.com/byteplus-sdk/byteplus-go-sdk-v2/service/arkruntime/model"
    "github.com/byteplus-sdk/byteplus-go-sdk-v2/byteplus"
)

func main() {
    // Make sure that you have stored the API Key in the environment variable ARK_API_KEY
    // Initialize the Ark client to read your API Key from an environment variable
    client := arkruntime.NewClientWithApiKey(
        // Get your API Key from the environment variable. This is the default mode and you can modify it as required
        os.Getenv("ARK_API_KEY"),
        // The base URL for model invocation
        arkruntime.WithBaseUrl("https://ark.ap-southeast.bytepluses.com/api/v3"),
    )
    ctx := context.Background()
    // Replace with Model ID
    modelEp := "seedance-1-5-pro-251215"

    // Generate a task
    fmt.Println("----- create request -----")
    createReq := model.CreateContentGenerationTaskRequest{
        Model: modelEp,
        Ratio:                 byteplus.String("adaptive"),
        Duration:              byteplus.Int64(5),
        Watermark:             byteplus.Bool(false),
        ServiceTier:           byteplus.String("flex"),
        ExecutionExpiresAfter: byteplus.Int64(172800),
        Content: []*model.CreateContentGenerationContentItem{
            {
                // Combination of text prompt and parameters
                Type: model.ContentGenerationContentItemTypeText,
                Text: byteplus.String("A girl holding a fox, the girl opens her eyes, looks gently at the camera, the fox hugs affectionately, the camera slowly pulls out, the girl's hair is blown by the wind"),
            },
            {
                // The URL of the first frame image
                Type: model.ContentGenerationContentItemTypeImage,
                ImageURL: &model.ImageURL{
                    URL: "https://ark-doc.tos-ap-southeast-1.bytepluses.com/doc_image/i2v_foxrgirl.png", 
                },
            },
        },
    }
    createResp, err := client.CreateContentGenerationTask(ctx, createReq)
    if err != nil {
        fmt.Printf("create content generation error: %v", err)
        return
    }
    taskID := createResp.ID
    fmt.Printf("Task Created with ID: %s", taskID)

    // Polling query section
    fmt.Println("----- polling task status -----")
    for {
        getReq := model.GetContentGenerationTaskRequest{ID: taskID}
        getResp, err := client.GetContentGenerationTask(ctx, getReq)
        if err != nil {
            fmt.Printf("get content generation task error: %v", err)
            return
        }

        status := getResp.Status
        if status == "succeeded" {
            fmt.Println("----- task succeeded -----")
            fmt.Printf("Task ID: %s \\n", getResp.ID)
            fmt.Printf("Model: %s \\n", getResp.Model)
            fmt.Printf("Video URL: %s \\n", getResp.Content.VideoURL)
            fmt.Printf("Completion Tokens: %d \\n", getResp.Usage.CompletionTokens)
            fmt.Printf("Created At: %d, Updated At: %d", getResp.CreatedAt, getResp.UpdatedAt)
            return
        } else if status == "failed" {
            fmt.Println("----- task failed -----")
            if getResp.Error != nil {
                fmt.Printf("Error Code: %s, Message: %s", getResp.Error.Code, getResp.Error.Message)
            }
            return
        } else {
            fmt.Printf("Current status: %s, Retrying in 60 seconds... \\n", status)
            time.Sleep(60 * time.Second)
        }
    }
}
\`\`\`

`}></RenderMd></Tabs.TabPane></Tabs>);
 ```

<span id="75568695"></span>
## Draft sample mode
Producing a production-quality video that meets expectations typically requires multiple generation attempts, which can be time-consuming and resource-intensive. Draft sample mode is an intermediate visualization feature introduced by ModelArk to address this challenge.
When enabled, Modelark generates a preview video that allows users to validate key aspects at low cost, such as scene structure, camera movement, subject actions, and alignment with the prompt intent. This enables rapid iteration and direction adjustment.
Once the preview meets expectations, users can then generate the final high-quality video based on the Draft output.

| | | | \
|Input |\
|<div style="width:260px"></div> |Draft video |Official video |
|---|---|---|
| | | | \
|![Image](https://p9-arcosite.byteimg.com/tos-cn-i-goo7wpa0wc/ebb5217645b04cfc94209a6f7d36a523~tplv-goo7wpa0wc-image.image =240x) |\
|> Prompt：A girl holding a fox, the girl opens her eyes, looks gently at the camera, the fox hugs affectionately, the camera slowly pulls out, the girl’s hair is blown by the wind, and the sound of the wind can be heard |<BytedReactXgplayer config={{ url: 'https://p9-arcosite.byteimg.com/tos-cn-i-goo7wpa0wc/1c92b2d2fb1749d28a21ad56d46bd2d1~tplv-goo7wpa0wc-image.image', poster: 'https://p9-arcosite.byteimg.com/tos-cn-i-goo7wpa0wc/1c92b2d2fb1749d28a21ad56d46bd2d1~tplv-goo7wpa0wc-video-poster.jpeg' }} ></BytedReactXgplayer> |\
| |> Generates a preview video to help users **validate at low cost** |<BytedReactXgplayer config={{ url: 'https://p9-arcosite.byteimg.com/tos-cn-i-goo7wpa0wc/1aa51109dee44b8a9b263f713754dd40~tplv-goo7wpa0wc-image.image', poster: 'https://p9-arcosite.byteimg.com/tos-cn-i-goo7wpa0wc/1aa51109dee44b8a9b263f713754dd40~tplv-goo7wpa0wc-video-poster.jpeg' }} ></BytedReactXgplayer> |\
| | |> Generate the final video by reusing the draft video’s **model**, **prompt**, **input images**, **seed value**, **audio settings**, **video aspect ratio**, and **video duration**, ensuring consistency across key video elements. |


Take the steps below to use this feature:
<span id="ba79c508"></span>
### Step 1: Generate a draft video

1. Set `"draft": true` and call the `POST /contents/generations/tasks` API to create a draft video generation task.
2. Call the `GET /contents/generations/tasks/{id}` API to query the generation status and results, download the draft video, and confirm whether it meets your expectations.
   :::tip
   * This feature is supported only by Seedance 1.5 Pro.
   * Only 480p resolution is supported. Using any other resolution will result in an error. Returning the last frame is not supported, and offline inference is not supported.
   * The unit price per token for draft videos remains the same, but fewer tokens are consumed. **** 
      draft video token usage is calculated as: `draft video token usage = Standard video token usage × Conversion factor`
      Taking **Seedance 1.5 Pro** as an example: For **videos with audio**, the conversion factor is **0.6**. Therefore, generating a draft video with audio costs **60% of the price of a standard video**, significantly reducing overall cost.
   
    
   :::
   
   ```mixin-react
   return (<Tabs>
   <Tabs.TabPane title="Curl" key="P8mVT0oGs8"><RenderMd content={`1. Create a draft video generation task.
   
   \`\`\`Bash
   curl https://ark.ap-southeast.bytepluses.com/api/v3/contents/generations/tasks \\
     -H "Content-Type: application/json" \\
     -H "Authorization: Bearer $ARK_API_KEY" \\
     -d '{
       "model": "seedance-1-5-pro-251215",
       "content": [
           {
               "type": "text",
               "text": "A girl holding a fox, the girl opens her eyes, looks gently at the camera, the fox hugs affectionately, the camera slowly pulls out, the girl’s hair is blown by the wind"
           },
           {
               "type": "image_url",
               "image_url": {
                   "url": "https://ark-doc.tos-ap-southeast-1.bytepluses.com/doc_image/i2v_foxrgirl.png"
               }
           }
       ],
       "seed": 20, 
       "duration": 6, 
       "draft": true
   }'
   \`\`\`
   
   Upon a successful request, the system will return a task ID. This ID is the **draft video Task ID**, which is required for generating the final video in subsequent steps.
   
   
   2. Use the draft video task ID to query the generation status and results.
      \`\`\`Bash
      # Replace cgt-2026****-pzjqb with the ID acquired from last step
      
      curl https://ark.ap-southeast.bytepluses.com/api/v3/contents/generations/tasks/cgt-2026****-pzjqb \\
        -H "Content-Type: application/json" \\
        -H "Authorization: Bearer $ARK_API_KEY" 
      \`\`\`
   
   
   Once the task status changes to \`succeeded\`, you can download the generated draft video from the content.**video_url** field to review whether the result meets your expectations.
   
   * If the result does not meet expectations, you can adjust the parameters and create a new draft video generation task.
   * After confirming that the draft video meets your expectations, proceed with the subsequent steps to generate the final video. 
   `}></RenderMd></Tabs.TabPane>
   <Tabs.TabPane title="Python" key="qwvYx3tHGY"><RenderMd content={`1. Create a draft video task and poll for its status.
   2. Once the task status changes to \`succeeded\`, you can download the generated draft video from the content.**video_url** field to review whether the result meets your expectations.
      * If the result does not meet expectations, you can adjust the parameters and create a new draft video generation task.
      * After confirming that the draft video meets your expectations, proceed with the subsequent steps to generate the final video. 
      \`\`\`Python
      import os
      import time
      # Install SDK:  pip install byteplus-python-sdk-v2 
      from byteplussdkarkruntime import Ark 
      
      # Make sure that you have stored the API Key in the environment variable ARK_API_KEY
      # Initialize the Ark client to read your API Key from an environment variable
      client = Ark(
          # This is the default path. You can configure it based on the service location
          base_url="https://ark.ap-southeast.bytepluses.com/api/v3",
          # Get API Key：https://console.byteplus.com/ark/region:ark+ap-southeast-1/apikey
          api_key=os.environ.get("ARK_API_KEY"),
      )
      
      if __name__ == "__main__":
          print("----- create request -----")
          create_result = client.content_generation.tasks.create(
              model="seedance-1-5-pro-251215", # Replace with Model ID
              content=[
                  {
                      # Combination of text prompt and parameters
                      "type": "text",
                      "text": "A girl holding a fox, the girl opens her eyes, looks gently at the camera, the fox hugs affectionately, the camera slowly pulls out, the girl’s hair is blown by the wind"             
                  },
                  {
                      # The URL of the first frame image
                      "type": "image_url",
                      "image_url": {
                          "url": "https://ark-doc.tos-ap-southeast-1.bytepluses.com/doc_image/i2v_foxrgirl.png"
                      }
                  }
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
                  print(f"Error: {get_result.error}")
                  break
              else:
                  print(f"Current status: {status}, Retrying after 10 seconds...")
                  time.sleep(10)
      \`\`\`
   
   `}></RenderMd></Tabs.TabPane>
   <Tabs.TabPane title="Java" key="kP7pnv7rAx"><RenderMd content={`1. Create a draft video task and poll for its status.
   2. Once the task status changes to \`succeeded\`, you can download the generated draft video from the content.**video_url** field to review whether the result meets your expectations.
      * If the result does not meet expectations, you can adjust the parameters and create a new draft video generation task.
      * After confirming that the draft video meets your expectations, proceed with the subsequent steps to generate the final video. 
      \`\`\`Java
      package com.ark.sample;
      
      import com.byteplus.ark.runtime.model.content.generation.*;
      import com.byteplus.ark.runtime.model.content.generation.CreateContentGenerationTaskRequest.Content;
      import com.byteplus.ark.runtime.service.ArkService;
      import okhttp3.ConnectionPool;
      import okhttp3.Dispatcher;
      
      import java.util.ArrayList;
      import java.util.List;
      import java.util.concurrent.TimeUnit;
      
      public class ContentGenerationTaskExample {
          // Make sure that you have stored the API Key in the environment variable ARK_API_KEY
          // Initialize the Ark client to read your API Key from an environment variable
          static String apiKey = System.getenv("ARK_API_KEY");
          static ConnectionPool connectionPool = new ConnectionPool(5, 1, TimeUnit.SECONDS);
          static Dispatcher dispatcher = new Dispatcher();
          static ArkService service = ArkService.builder()
                 .baseUrl("https://ark.ap-southeast.bytepluses.com/api/v3") // The base URL for model invocation
                 .dispatcher(dispatcher)
                 .connectionPool(connectionPool)
                 .apiKey(apiKey)
                 .build();
                 
          public static void main(String[] args) {
              String model = "seedance-1-5-pro-251215"; // Replace with Model ID
              Long seed = 20L;
              Long duration = 6L;
              Boolean draft = true;
              System.out.println("----- create request -----");
              List<Content> contents = new ArrayList<>();
              
              // Combination of text prompt and parameters
              contents.add(Content.builder()
                      .type("text")
                      .text("A girl holding a fox, the girl opens her eyes, looks gently at the camera, the fox hugs affectionately, the camera slowly pulls out, the girl’s hair is blown by the wind")
                      .build());
              // The URL of the first frame image
              contents.add(Content.builder()
                      .type("image_url")
                      .imageUrl(CreateContentGenerationTaskRequest.ImageUrl.builder()
                              .url("https://ark-doc.tos-ap-southeast-1.bytepluses.com/doc_image/i2v_foxrgirl.png")
                              .build())
                      .build());
      
              // Create a video generation task
              CreateContentGenerationTaskRequest createRequest = CreateContentGenerationTaskRequest.builder()
                      .model(model)
                      .content(contents)
                      .seed(seed)
                      .duration(duration)
                      .draft(draft)
                      .build();
      
              CreateContentGenerationTaskResult createResult = service.createContentGenerationTask(createRequest);
              System.out.println(createResult);
      
              // Get the details of the task
              String taskId = createResult.getId();
              GetContentGenerationTaskRequest getRequest = GetContentGenerationTaskRequest.builder()
                      .taskId(taskId)
                      .build();
      
              // Polling query section
              System.out.println("----- polling task status -----");
              while (true) {
                  try {
                      GetContentGenerationTaskResponse getResponse = service.getContentGenerationTask(getRequest);
                      String status = getResponse.getStatus();
                      if ("succeeded".equalsIgnoreCase(status)) {
                          System.out.println("----- task succeeded -----");
                          System.out.println(getResponse);
                          break;
                      } else if ("failed".equalsIgnoreCase(status)) {
                          System.out.println("----- task failed -----");
                          System.out.println("Error: " + getResponse.getStatus());
                          break;
                      } else {
                          System.out.printf("Current status: %s, Retrying in 10 seconds...", status);
                          TimeUnit.SECONDS.sleep(10);
                      }
                  } catch (InterruptedException ie) {
                      Thread.currentThread().interrupt();
                      System.err.println("Polling interrupted");
                      break;
                  }
              }
          }
      }
      \`\`\`
   
   `}></RenderMd></Tabs.TabPane>
   <Tabs.TabPane title="Go" key="yra4ufIeDb"><RenderMd content={`1. Create a draft video task and poll for its status.
   2. Once the task status changes to \`succeeded\`, you can download the generated draft video from the content.**video_url** field to review whether the result meets your expectations.
      * If the result does not meet expectations, you can adjust the parameters and create a new draft video generation task.
      * After confirming that the draft video meets your expectations, proceed with the subsequent steps to generate the final video. 
      \`\`\`Go
      package main
      
      import (
          "context"
          "fmt"
          "time"
          "os"
      
          "github.com/byteplus-sdk/byteplus-go-sdk-v2/service/arkruntime"
          "github.com/byteplus-sdk/byteplus-go-sdk-v2/service/arkruntime/model"
          "github.com/byteplus-sdk/byteplus-go-sdk-v2/byteplus"
      )
      
      func main() {
          // Make sure that you have stored the API Key in the environment variable ARK_API_KEY
          // Initialize the Ark client to read your API Key from an environment variable
          client := arkruntime.NewClientWithApiKey(
              // Get your API Key from the environment variable. This is the default mode and you can modify it as required
              os.Getenv("ARK_API_KEY"),
              // The base URL for model invocation
              arkruntime.WithBaseUrl("https://ark.ap-southeast.bytepluses.com/api/v3"),
          )
          ctx := context.Background()
          // Replace with Model ID
          modelEp := "seedance-1-5-pro-251215"
      
          // Generate a task
          fmt.Println("----- create request -----")
          createReq := model.CreateContentGenerationTaskRequest{
              Model: modelEp,
              Seed:          byteplus.Int64(20),
              Duration:      byteplus.Int64(6),
              Draft:         byteplus.Bool(true),
              Content: []*model.CreateContentGenerationContentItem{
                  {
                      // Combination of text prompt and parameters
                      Type: model.ContentGenerationContentItemTypeText,
                      Text: byteplus.String("A girl holding a fox, the girl opens her eyes, looks gently at the camera, the fox hugs affectionately, the camera slowly pulls out, the girl’s hair is blown by the wind"),
                  },
                  {
                      // The URL of the first frame image
                      Type: model.ContentGenerationContentItemTypeImage,
                      ImageURL: &model.ImageURL{
                          URL: "https://ark-doc.tos-ap-southeast-1.bytepluses.com/doc_image/i2v_foxrgirl.png",
                      },
                  },
              },
          }
          createResp, err := client.CreateContentGenerationTask(ctx, createReq)
          if err != nil {
              fmt.Printf("create content generation error: %v", err)
              return
          }
          taskID := createResp.ID
          fmt.Printf("Task Created with ID: %s", taskID)
      
          // Polling query section
          fmt.Println("----- polling task status -----")
          for {
              getReq := model.GetContentGenerationTaskRequest{ID: taskID}
              getResp, err := client.GetContentGenerationTask(ctx, getReq)
              if err != nil {
                  fmt.Printf("get content generation task error: %v", err)
                  return
              }
      
              status := getResp.Status
              if status == "succeeded" {
                  fmt.Println("----- task succeeded -----")
                  fmt.Printf("Task ID: %s \\n", getResp.ID)
                  fmt.Printf("Model: %s \\n", getResp.Model)
                  fmt.Printf("Video URL: %s \\n", getResp.Content.VideoURL)
                  fmt.Printf("Completion Tokens: %d \\n", getResp.Usage.CompletionTokens)
                  fmt.Printf("Created At: %d, Updated At: %d", getResp.CreatedAt, getResp.UpdatedAt)
                  return
              } else if status == "failed" {
                  fmt.Println("----- task failed -----")
                  if getResp.Error != nil {
                      fmt.Printf("Error Code: %s, Message: %s", getResp.Error.Code, getResp.Error.Message)
                  }
                  return
              } else {
                  fmt.Printf("Current status: %s, Retrying in 10 seconds... \\n", status)
                  time.Sleep(10 * time.Second)
              }
          }
      }
      \`\`\`
   
   `}></RenderMd></Tabs.TabPane></Tabs>);
    ```


<span id="0524d4a2"></span>
### Step 2: Generate the official video based on the draft video
If the draft video meets your expectations, you can call the `POST /contents/generations/tasks` API again with the draft video task ID returned in Step 1 to generate the final video.
:::tip
* ModelArk will automatically reuse the user inputs applied by the draft video (including `model`, `content.text`, `content.image_url`, `generate_audio`, `seed`, `ratio`, `duration`, `camera_fixed`) to generate the final video.
* Other parameters can be specified as needed. If not specified, the model’s default values will be used.
   For example, you can configure the output resolution of the final video, whether to include a watermark, whether to use offline inference, and whether to return the last frame.
* Generating the final video based on the Draft is a standard inference process, which will be billed according to the token consumption rate of standard videos.
* The validity period of the draft video task ID is 7 days (calculated from the `created at` timestamp). Once expired, this draft video cannot be used to generate the official video.
:::

```mixin-react
return (<Tabs>
<Tabs.TabPane title="Curl" key="SxUNRNt3ik"><RenderMd content={`1. Create a video generation task based on **content.draft_task.id**.
   \`\`\`Bash
   curl https://ark.ap-southeast.bytepluses.com/api/v3/contents/generations/tasks \\
     -H "Content-Type: application/json" \\
     -H "Authorization: Bearer $ARK_API_KEY" \\
     -d '{
       "model": "seedance-1-5-pro-251215",
       "content": [
           {
               "type": "draft_task",
               "draft_task": {"id": "cgt-2026****-pzjqb"}
           }
       ],
         "watermark": false,
         "resolution": "720p",
         "return_last_frame": true,
         "service_tier": "default"
     }'  
   \`\`\`


Upon a successful request, a task ID is returned.


2. Use the **video task ID** to query the generation status and results.
   \`\`\`Bash
   # Replace cgt-2026****-bn6zj with the ID acquired from last step
   
   curl https://ark.ap-southeast.bytepluses.com/api/v3/contents/generations/tasks/cgt-2026****-bn6zj \
     -H "Content-Type: application/json" \
     -H "Authorization: Bearer $ARK_API_KEY" 
   \`\`\`


When the task status changes to \`succeeded\`, you can download the generated video file from the **content.video_url** parameter.
`}></RenderMd></Tabs.TabPane>
<Tabs.TabPane title="Python" key="WiTU0LBbMS"><RenderMd content={`1. Using the \`content.draft_task.id\`  (obtained from the response in Step 1), initiate a video generation task and continue polling until the task status is retrieved.
2. Once the task status changes to \`succeeded\`, you can download the generated video file from the content.**video_url** field.
   \`\`\`Python
   import os
   import time
   # Install SDK:  pip install byteplus-python-sdk-v2 
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
           model="seedance-1-5-pro-251215", # Replace with Model ID
           content=[
               {
                   "type": "draft_task",
                   "draft_task": {
                       "id": "cgt-2026****-pzjqb"
                   }
               }
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
               print(f"Error: {get_result.error}")
               break
           else:
               print(f"Current status: {status}, Retrying after 10 seconds...")
               time.sleep(10)
   \`\`\`

`}></RenderMd></Tabs.TabPane>
<Tabs.TabPane title="Java" key="SlxdA50UY5"><RenderMd content={`1. Using the \`content.draft_task.id\`  (obtained from the response in Step 1), initiate a video generation task and continue polling until the task status is retrieved.
2. Once the task status changes to \`succeeded\`, you can download the generated video file from the content.**video_url** field.
   \`\`\`Java
   package com.ark.sample;
   
   import com.byteplus.ark.runtime.model.content.generation.*;
   import com.byteplus.ark.runtime.model.content.generation.CreateContentGenerationTaskRequest.Content;
   import com.byteplus.ark.runtime.service.ArkService;
   import okhttp3.ConnectionPool;
   import okhttp3.Dispatcher;
   
   import java.util.ArrayList;
   import java.util.List;
   import java.util.concurrent.TimeUnit;
   
   public class ContentGenerationTaskExample {
       // Make sure that you have stored the API Key in the environment variable ARK_API_KEY
       // Initialize the Ark client to read your API Key from an environment variable
       static String apiKey = System.getenv("ARK_API_KEY");
       static ConnectionPool connectionPool = new ConnectionPool(5, 1, TimeUnit.SECONDS);
       static Dispatcher dispatcher = new Dispatcher();
       static ArkService service = ArkService.builder()
              .baseUrl("https://ark.ap-southeast.bytepluses.com/api/v3") // The base URL for model invocation
              .dispatcher(dispatcher)
              .connectionPool(connectionPool)
              .apiKey(apiKey)
              .build();
              
       public static void main(String[] args) {
           String model = "seedance-1-5-pro-251215"; // Replace with Model ID
           Boolean watermark = false;
           String resolution = "720p";
           Boolean returnLastFrame = true;
           String serviceTier = "default";
           System.out.println("----- create request -----");
           List<Content> contents = new ArrayList<>();
           
           // Combination of text prompt and parameters
           contents.add(Content.builder()
                   .type("draft_task")
                   .draftTask(CreateContentGenerationTaskRequest.DraftTask.builder()
                           .id("cgt-2026****-pzjqb")
                           .build())
                    .build());
                           
   
           // Create a video generation task
           CreateContentGenerationTaskRequest createRequest = CreateContentGenerationTaskRequest.builder()
                   .model(model)
                   .content(contents)
                   .watermark(watermark)
                   .resolution(resolution)
                   .returnLastFrame(returnLastFrame)
                   .serviceTier(serviceTier)
                   .build();
   
           CreateContentGenerationTaskResult createResult = service.createContentGenerationTask(createRequest);
           System.out.println(createResult);
   
           // Get the details of the task
           String taskId = createResult.getId();
           GetContentGenerationTaskRequest getRequest = GetContentGenerationTaskRequest.builder()
                   .taskId(taskId)
                   .build();
   
           // Polling query section
           System.out.println("----- polling task status -----");
           while (true) {
               try {
                   GetContentGenerationTaskResponse getResponse = service.getContentGenerationTask(getRequest);
                   String status = getResponse.getStatus();
                   if ("succeeded".equalsIgnoreCase(status)) {
                       System.out.println("----- task succeeded -----");
                       System.out.println(getResponse);
                       break;
                   } else if ("failed".equalsIgnoreCase(status)) {
                       System.out.println("----- task failed -----");
                       System.out.println("Error: " + getResponse.getStatus());
                       break;
                   } else {
                       System.out.printf("Current status: %s, Retrying in 10 seconds...", status);
                       TimeUnit.SECONDS.sleep(10);
                   }
               } catch (InterruptedException ie) {
                   Thread.currentThread().interrupt();
                   System.err.println("Polling interrupted");
                   break;
               }
           }
       }
   }
   \`\`\`



`}></RenderMd></Tabs.TabPane>
<Tabs.TabPane title="Go" key="wVT5Sv8bZD"><RenderMd content={`1. Using the \`content.draft_task.id\`  (obtained from the response in Step 1), initiate a video generation task and continue polling until the task status is retrieved.
2. Once the task status changes to \`succeeded\`, you can download the generated video file from the content.**video_url** field.
   \`\`\`Go
   package main
   
   import (
       "context"
       "fmt"
       "time"
       "os"
   
       "github.com/byteplus-sdk/byteplus-go-sdk-v2/service/arkruntime"
       "github.com/byteplus-sdk/byteplus-go-sdk-v2/service/arkruntime/model"
       "github.com/byteplus-sdk/byteplus-go-sdk-v2/byteplus"
   )
   
   func main() {
       // Make sure that you have stored the API Key in the environment variable ARK_API_KEY
       // Initialize the Ark client to read your API Key from an environment variable
       client := arkruntime.NewClientWithApiKey(
           // Get your API Key from the environment variable. This is the default mode and you can modify it as required
           os.Getenv("ARK_API_KEY"),
           // The base URL for model invocation
           arkruntime.WithBaseUrl("https://ark.ap-southeast.bytepluses.com/api/v3"),
       )
       ctx := context.Background()
       // Replace with Model ID
       modelEp := "seedance-1-5-pro-251215"
   
       // Generate a task
       fmt.Println("----- create request -----")
       createReq := model.CreateContentGenerationTaskRequest{
           Model: modelEp,
            Watermark:         byteplus.Bool(false),
            Resolution:        byteplus.String("720p"),
            ReturnLastFrame:   byteplus.Bool(true),
            ServiceTier:       byteplus.String("default"),
           Content: []*model.CreateContentGenerationContentItem{
               {
                   Type:      model.ContentGenerationContentItemTypeDraftTask,
                   DraftTask: &model.DraftTask{ID: "cgt-2026****-pzjqb"},
               },
           },
       }
   
       createResp, err := client.CreateContentGenerationTask(ctx, createReq)
       if err != nil {
           fmt.Printf("create content generation error: %v", err)
           return
       }
       taskID := createResp.ID
       fmt.Printf("Task Created with ID: %s", taskID)
   
       // Polling query section
       fmt.Println("----- polling task status -----")
       for {
           getReq := model.GetContentGenerationTaskRequest{ID: taskID}
           getResp, err := client.GetContentGenerationTask(ctx, getReq)
           if err != nil {
               fmt.Printf("get content generation task error: %v", err)
               return
           }
   
           status := getResp.Status
           if status == "succeeded" {
               fmt.Println("----- task succeeded -----")
               fmt.Printf("Task ID: %s \\n", getResp.ID)
               fmt.Printf("Model: %s \\n", getResp.Model)
               fmt.Printf("Video URL: %s \\n", getResp.Content.VideoURL)
               fmt.Printf("Completion Tokens: %d \\n", getResp.Usage.CompletionTokens)
               fmt.Printf("Created At: %d, Updated At: %d", getResp.CreatedAt, getResp.UpdatedAt)
               return
           } else if status == "failed" {
               fmt.Println("----- task failed -----")
               if getResp.Error != nil {
                   fmt.Printf("Error Code: %s, Message: %s", getResp.Error.Code, getResp.Error.Message)
               }
               return
           } else {
               fmt.Printf("Current status: %s, Retrying in 10 seconds... \\n", status)
               time.Sleep(10 * time.Second)
           }
       }
   }
   \`\`\`

`}></RenderMd></Tabs.TabPane></Tabs>);
 ```

<span id="141cf7fa"></span>
## Generate multiple consecutive videos
Use the last frame of the previously generated video as the first frame of the next video task, and repeat to generate multiple consecutive videos.
FFmpeg or other tools can be used to concatenate the generated short videos into a complete video.

| | | | \
|Output1 |Output2 |Output3 |
|---|---|---|
| | | | \
|<BytedReactXgplayer config={{ url: 'https://p9-arcosite.byteimg.com/tos-cn-i-goo7wpa0wc/09e5e4cf5aed462fb426ab9ce1eec485~tplv-goo7wpa0wc-image.image', poster: 'https://p9-arcosite.byteimg.com/tos-cn-i-goo7wpa0wc/09e5e4cf5aed462fb426ab9ce1eec485~tplv-goo7wpa0wc-video-poster.jpeg' }} ></BytedReactXgplayer> |\
|> A girl holding a fox, the girl opens her eyes, looks gently at the camera, the fox hugs affectionately, the camera slowly pulls out, the girl's hair is blown by the wind |<BytedReactXgplayer config={{ url: 'https://p9-arcosite.byteimg.com/tos-cn-i-goo7wpa0wc/d3f181984db040ac98d4d16f5c0115e6~tplv-goo7wpa0wc-image.image', poster: 'https://p9-arcosite.byteimg.com/tos-cn-i-goo7wpa0wc/d3f181984db040ac98d4d16f5c0115e6~tplv-goo7wpa0wc-video-poster.jpeg' }} ></BytedReactXgplayer> |\
| |> A girl and a fox running on the grass, sunny weather, the girl's smile is brilliant, the fox jumps happily |<BytedReactXgplayer config={{ url: 'https://p9-arcosite.byteimg.com/tos-cn-i-goo7wpa0wc/4bc452920fa94ffc838d871c2619323b~tplv-goo7wpa0wc-image.image', poster: 'https://p9-arcosite.byteimg.com/tos-cn-i-goo7wpa0wc/4bc452920fa94ffc838d871c2619323b~tplv-goo7wpa0wc-video-poster.jpeg' }} ></BytedReactXgplayer> |\
| | |> A girl and a fox resting under a tree, the girl gently strokes the fox's fur, the fox lies meekly on the girl's lap |

```Python
import os
import time  
# Install SDK:  pip install byteplus-python-sdk-v2 
from byteplussdkarkruntime import Ark 

# Make sure that you have stored the API Key in the environment variable ARK_API_KEY
# Initialize the Ark client to read your API Key from an environment variable
client = Ark(
    # This is the default path. You can configure it based on the service location
    base_url="https://ark.ap-southeast.bytepluses.com/api/v3",
    # Get API Key：https://console.byteplus.com/ark/region:ark+ap-southeast-1/apikey
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
        model="seedance-1-5-pro-251215", # Replace with Model ID
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

<span id="ff4331b6"></span>
## Use webhook notification
By specifying a callback notification address with the **callback_url** parameter, ModelArk will send a POST request to the specified address when the status of a video generation task changes, allowing you to receive timely updates on the latest task status. The request content structure is consistent with the response body of [Creating a video generation task](https://docs.byteplus.com/en/docs/ModelArk/1520757).
```Bash
{
  "id": "cgt-2025****",
  "model": "seedance-1-5-pro-251215",
  "status": "running", # Possible status values: queued, running, succeeded, failed, expired
  "created_at": 1765434920,
  "updated_at": 1765434920,
  "service_tier": "default",
  "execution_expires_after": 172800
}
```

You need to set up a publicly accessible web server yourself to receive webhook notifications. The following is a simple web server code example for your reference.
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
    """Automatically create task table on first run, aligning parameters with callback parameters"""
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

        # 2. Validate required parameters
        required_fields = ['id', 'model', 'status', 'created_at', 'updated_at', 'service_tier', 'execution_expires_after']
        for field in required_fields:
            if field not in callback_data:
                logging.error(f"Callback data missing required parameter: {field}, data: {callback_data}")
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
<span id="2760a484"></span>
## Retention period
Task data (such as task status, video URL, and so on) is retained for only 24 hours and will be automatically cleared after the timeout. Be sure to save the generated videos in a timely manner.
<span id="b25b1821"></span>
## Rate limits description
<span id="946ba4cf"></span>
### Model rate limits
**default (online inference)**

* **RPM Rate Limit:** For the same model (distinguished by model version) under an account, this is the maximum number of tasks allowed to be created per minute. If this limit is exceeded, an error is returned when creating a video generation task.
* **Concurrency Limit:** For the same model (distinguished by model version) under an account, this is the maximum number of tasks being processed at the same time. Tasks that exceed this limit will be queued for being processed.
* The limit values vary for different models. See [Video Generation](/docs/ModelArk/1330310#1dae4d0e) for details.

**flex (offline inference)**

* **TPD rate limit:** The maximum total number of tokens that can be called for the same model (distinguished by model version) under the same account in one day. Call requests exceeding this limit will be rejected. The TPD rate limit values differ for different models; see [Video generation](/docs/ModelArk/1330310#2705b333) for details.

<span id="f76aafc8"></span>
## Image cropping rules
**The image-to-video feature allows you to set the aspect ratio for the generated video.** If the selected video aspect ratio differs from that of your uploaded image, ModelArk will automatically crop your image using a center-based cropping method. The detailed rules are as follows:
:::tip
For better results, it is recommended that the specified video aspect ratio closely matches the original aspect ratio of your uploaded image.
:::

1. Input parameters:
   * Original image width: `W` (in pixels)
   * Original image height: `H` (in pixels)
   * Target aspect ratio: `A:B` (for example, 21:9), i.e., the cropped image's width-to-height ratio should be `A/B` (for example, 21/9 ≈ 2.333).
2. Compare the aspect ratios:
   * Calculate the original image's aspect ratio: `Ratio_original = W / H`.
   * Calculate the target aspect ratio value: `Ratio_target = A / B` (for example, the target ratio value for 21:9 = 21/9 ≈ 2.333).
   * Compare the ratios to determine the cropping base:
      * If `Ratio_original < Ratio_target` (i.e., the original image is "too tall" or "portrait-oriented"), the cropping will be width-based.
      * If `Ratio_original > Ratio_target` (i.e., the original image is "too wide" or "landscape-oriented"), the cropping will be height-based.
      * If the ratios are equal, no cropping is needed, and the full image is used.
3. Cropping dimension calculations:
   * Width-based cropping (applies to "tall" images):
      * Cropped Width, `Crop_W = W` (the full original width is used).
      * Cropped Height, `Crop_H = W * (B / A)` (the height is calculated proportionally based on the target ratio).
      * Starting coordinates of the crop area (centered):
         * X-coordinate (horizontal): 0 (aligned to the left edge, as the full width is used).
         * Y-coordinate (vertical): `(H - Crop_H) / 2` (calculated to center the crop area vertically).
   * Height-based cropping (applies to "wide" images):
      * Cropped Height, `Crop_H = H` (the full original height is used).
      * Cropped Width, `Crop_W = H * (A/B)` (the width is calculated proportionally based on the target ratio).
      * Starting coordinates of the crop area (centered):
         * X-coordinate (horizontal): `(W - Crop_W) / 2` (calculated to center the crop area horizontally).
         * Y-coordinate (vertical): 0 (aligned to the top edge, as the full height is used).
4. Result:
   * The final cropped image has dimensions of `Crop_W × Crop_H`, strictly conforming to the `A:B` aspect ratio. The cropped area is entirely within the original image, ensuring no black bars are introduced. 
   * The content is always centered relative to the original image.
5. Cropping example:

> The following examples demonstrate the cropping rules using the Seedance 1.0 Pro first-frame image-to-video feature.


| | | | \
|Input first-frame image |Specified aspect ratio |Generated video |
|---|---|---|
| | | | \
|16:9 |\
| |\
|![Image](https://p9-arcosite.byteimg.com/tos-cn-i-goo7wpa0wc/c66d7faff6104320a981b36149dc713f~tplv-goo7wpa0wc-image.image =1920x) |\
| |21:9 |\
| | |<BytedReactXgplayer config={{ url: 'https://p9-arcosite.byteimg.com/tos-cn-i-goo7wpa0wc/6e8590c07be9406d805209355b799a37~tplv-goo7wpa0wc-image.image', poster: 'https://p9-arcosite.byteimg.com/tos-cn-i-goo7wpa0wc/6e8590c07be9406d805209355b799a37~tplv-goo7wpa0wc-video-poster.jpeg' }} ></BytedReactXgplayer> |\
| | | |
|^^| | | \
| |16:9 |<BytedReactXgplayer config={{ url: 'https://p9-arcosite.byteimg.com/tos-cn-i-goo7wpa0wc/e0bef5f3806f439da5f0c9f5acc44c9b~tplv-goo7wpa0wc-image.image', poster: 'https://p9-arcosite.byteimg.com/tos-cn-i-goo7wpa0wc/e0bef5f3806f439da5f0c9f5acc44c9b~tplv-goo7wpa0wc-video-poster.jpeg' }} ></BytedReactXgplayer> |\
| | | |
|^^| | | \
| |4:3 |<BytedReactXgplayer config={{ url: 'https://p9-arcosite.byteimg.com/tos-cn-i-goo7wpa0wc/a8e3202b77744bec83e0c7baa247b84c~tplv-goo7wpa0wc-image.image', poster: 'https://p9-arcosite.byteimg.com/tos-cn-i-goo7wpa0wc/a8e3202b77744bec83e0c7baa247b84c~tplv-goo7wpa0wc-video-poster.jpeg' }} ></BytedReactXgplayer> |\
| | | |
|^^| | | \
| |1:1 |<BytedReactXgplayer config={{ url: 'https://p9-arcosite.byteimg.com/tos-cn-i-goo7wpa0wc/436df8f6dae74d6c86d08bf1e18bc9d0~tplv-goo7wpa0wc-image.image', poster: 'https://p9-arcosite.byteimg.com/tos-cn-i-goo7wpa0wc/436df8f6dae74d6c86d08bf1e18bc9d0~tplv-goo7wpa0wc-video-poster.jpeg' }} ></BytedReactXgplayer> |\
| | | |
|^^| | | \
| |3:4 |<BytedReactXgplayer config={{ url: 'https://p9-arcosite.byteimg.com/tos-cn-i-goo7wpa0wc/a3a94a577d754501889535a651d03a55~tplv-goo7wpa0wc-image.image', poster: 'https://p9-arcosite.byteimg.com/tos-cn-i-goo7wpa0wc/a3a94a577d754501889535a651d03a55~tplv-goo7wpa0wc-video-poster.jpeg' }} ></BytedReactXgplayer> |\
| | | |
|^^| | | \
| |9:16 |<BytedReactXgplayer config={{ url: 'https://p9-arcosite.byteimg.com/tos-cn-i-goo7wpa0wc/1423ee0fc9cf451398788dc57e9f55c4~tplv-goo7wpa0wc-image.image', poster: 'https://p9-arcosite.byteimg.com/tos-cn-i-goo7wpa0wc/1423ee0fc9cf451398788dc57e9f55c4~tplv-goo7wpa0wc-video-poster.jpeg' }} ></BytedReactXgplayer> |\
| | | |


