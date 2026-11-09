package xai

import (
	"bytes"
	"encoding/base64"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func convertXaiImageRequest(t *testing.T, relayMode int, path string, body string) (string, *relaycommon.RelayInfo) {
	t.Helper()
	var req dto.ImageRequest
	require.NoError(t, common.UnmarshalJsonStr(body, &req))
	info := &relaycommon.RelayInfo{RelayMode: relayMode, RequestURLPath: path}

	converted, err := (&Adaptor{}).ConvertImageRequest(nil, info, req)
	require.NoError(t, err)
	out, err := common.Marshal(converted)
	require.NoError(t, err)
	return string(out), info
}

func TestXaiImageRequestPassesGrokImagineParams(t *testing.T) {
	out, info := convertXaiImageRequest(t, relayconstant.RelayModeImagesGenerations, "/v1/images/generations", `{
		"model":"grok-imagine-image-2.0","prompt":"cat","n":2,
		"aspect_ratio":"9:16","resolution":"2k","quality":"medium",
		"response_format":"b64_json","size":"1024x1024"
	}`)

	assert.JSONEq(t, `{
		"model":"grok-imagine-image-2.0","prompt":"cat","n":2,
		"aspect_ratio":"9:16","resolution":"2k","quality":"medium",
		"response_format":"b64_json"
	}`, out)
	assert.Equal(t, "/v1/images/generations", info.RequestURLPath)
}

func TestXaiImageGenerationsWithReferenceImageRoutesToEdits(t *testing.T) {
	out, info := convertXaiImageRequest(t, relayconstant.RelayModeImagesGenerations, "/v1/images/generations", `{
		"model":"grok-imagine-image-2.0","prompt":"sketch",
		"image":{"url":"https://example.com/a.png","type":"image_url"}
	}`)

	assert.JSONEq(t, `{
		"model":"grok-imagine-image-2.0","prompt":"sketch",
		"image":{"url":"https://example.com/a.png","type":"image_url"}
	}`, out)
	assert.Equal(t, "/v1/images/edits", info.RequestURLPath)
}

func TestXaiImageEditsKeepsMultipleImages(t *testing.T) {
	out, info := convertXaiImageRequest(t, relayconstant.RelayModeImagesEdits, "/v1/images/edits", `{
		"model":"grok-imagine-image-2.0","prompt":"merge","aspect_ratio":"1:1",
		"images":[{"url":"https://example.com/a.png","type":"image_url"},{"url":"data:image/png;base64,AAAA","type":"image_url"}]
	}`)

	assert.JSONEq(t, `{
		"model":"grok-imagine-image-2.0","prompt":"merge","aspect_ratio":"1:1",
		"images":[{"url":"https://example.com/a.png","type":"image_url"},{"url":"data:image/png;base64,AAAA","type":"image_url"}]
	}`, out)
	assert.Equal(t, "/v1/images/edits", info.RequestURLPath)
}

// OpenAI 习惯的字符串参考图要转成 xAI 的引用对象，否则 xAI 按 schema 拒绝。
func TestXaiImageEditsConvertsStringRefsToObjects(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "single string image",
			body: `{"model":"grok-imagine-image-2.0","prompt":"p","image":"https://example.com/a.png"}`,
			want: `{"model":"grok-imagine-image-2.0","prompt":"p","image":{"url":"https://example.com/a.png"}}`,
		},
		{
			name: "string array image becomes images",
			body: `{"model":"grok-imagine-image-2.0","prompt":"p","image":["https://example.com/a.png","data:image/png;base64,AAAA"]}`,
			want: `{"model":"grok-imagine-image-2.0","prompt":"p","images":[{"url":"https://example.com/a.png"},{"url":"data:image/png;base64,AAAA"}]}`,
		},
		{
			name: "string images keep images key",
			body: `{"model":"grok-imagine-image-2.0","prompt":"p","images":["https://example.com/a.png"]}`,
			want: `{"model":"grok-imagine-image-2.0","prompt":"p","images":[{"url":"https://example.com/a.png"}]}`,
		},
		{
			name: "file_id object kept as is",
			body: `{"model":"grok-imagine-image-2.0","prompt":"p","image":{"file_id":"file_1"}}`,
			want: `{"model":"grok-imagine-image-2.0","prompt":"p","image":{"file_id":"file_1"}}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, _ := convertXaiImageRequest(t, relayconstant.RelayModeImagesEdits, "/v1/images/edits", tt.body)
			assert.JSONEq(t, tt.want, out)
		})
	}
}

// OpenAI SDK 的 images.edit() 走 multipart；xAI 只收 JSON，上传的文件要转成 data URI，
// 表单里的 xAI 专有参数要还原成 JSON 类型，Content-Type 也必须改成 application/json。
func TestXaiImageEditsMultipartConvertedToJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	png := []byte("\x89PNG\r\n\x1a\n0000")

	var form bytes.Buffer
	writer := multipart.NewWriter(&form)
	require.NoError(t, writer.WriteField("model", "grok-imagine-image-2.0"))
	require.NoError(t, writer.WriteField("prompt", "sketch"))
	require.NoError(t, writer.WriteField("n", "2"))
	require.NoError(t, writer.WriteField("size", "1024x1024"))
	require.NoError(t, writer.WriteField("aspect_ratio", "16:9"))
	require.NoError(t, writer.WriteField("resolution", "2k"))
	require.NoError(t, writer.WriteField("response_format", "b64_json"))
	for _, name := range []string{"a.png", "b.png"} {
		part, err := writer.CreateFormFile("image[]", name)
		require.NoError(t, err)
		_, err = part.Write(png)
		require.NoError(t, err)
	}
	require.NoError(t, writer.Close())

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", &form)
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())

	req, err := helper.GetAndValidOpenAIImageRequest(c, relayconstant.RelayModeImagesEdits)
	require.NoError(t, err)
	info := &relaycommon.RelayInfo{
		RelayMode:      relayconstant.RelayModeImagesEdits,
		RequestURLPath: "/v1/images/edits",
		ChannelMeta:    &relaycommon.ChannelMeta{},
	}
	converted, err := (&Adaptor{}).ConvertImageRequest(c, info, *req)
	require.NoError(t, err)
	out, err := common.Marshal(converted)
	require.NoError(t, err)

	dataURI := "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)
	assert.JSONEq(t, `{
		"model":"grok-imagine-image-2.0","prompt":"sketch","n":2,
		"aspect_ratio":"16:9","resolution":"2k","response_format":"b64_json",
		"images":[{"url":"`+dataURI+`"},{"url":"`+dataURI+`"}]
	}`, string(out))

	header := http.Header{}
	require.NoError(t, (&Adaptor{}).SetupRequestHeader(c, &header, info))
	assert.Equal(t, "application/json", header.Get("Content-Type"))
}

// OpenAI SDK 只传 size，要换算成最接近的 xAI 画幅；显式传了 aspect_ratio 时以它为准。
func TestXaiImageSizeMapsToAspectRatio(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{"square", `{"model":"m","prompt":"p","size":"1024x1024"}`, `{"model":"m","prompt":"p","aspect_ratio":"1:1"}`},
		{"landscape 3:2", `{"model":"m","prompt":"p","size":"1536x1024"}`, `{"model":"m","prompt":"p","aspect_ratio":"3:2"}`},
		{"portrait 2:3", `{"model":"m","prompt":"p","size":"1024x1536"}`, `{"model":"m","prompt":"p","aspect_ratio":"2:3"}`},
		{"dall-e wide nearest 16:9", `{"model":"m","prompt":"p","size":"1792x1024"}`, `{"model":"m","prompt":"p","aspect_ratio":"16:9"}`},
		{"auto", `{"model":"m","prompt":"p","size":"auto"}`, `{"model":"m","prompt":"p","aspect_ratio":"auto"}`},
		{"unparseable dropped", `{"model":"m","prompt":"p","size":"large"}`, `{"model":"m","prompt":"p"}`},
		{"explicit aspect_ratio wins", `{"model":"m","prompt":"p","size":"1024x1024","aspect_ratio":"9:16"}`, `{"model":"m","prompt":"p","aspect_ratio":"9:16"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, _ := convertXaiImageRequest(t, relayconstant.RelayModeImagesGenerations, "/v1/images/generations", tt.body)
			assert.JSONEq(t, tt.want, out)
		})
	}
}

// 网关没有 GET /v1/images/{request_id} 轮询接口，deferred 放行会扣费却拿不到图，必须 400 拒绝。
func TestXaiImageDeferredRejected(t *testing.T) {
	var req dto.ImageRequest
	require.NoError(t, common.UnmarshalJsonStr(`{"model":"m","prompt":"p","deferred":true}`, &req))
	info := &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeImagesGenerations, RequestURLPath: "/v1/images/generations"}

	_, err := (&Adaptor{}).ConvertImageRequest(nil, info, req)
	require.Error(t, err)
	var apiErr *types.NewAPIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, http.StatusBadRequest, apiErr.StatusCode)

	require.NoError(t, common.UnmarshalJsonStr(`{"model":"m","prompt":"p","deferred":false}`, &req))
	out, err := (&Adaptor{}).ConvertImageRequest(nil, info, req)
	require.NoError(t, err)
	encoded, err := common.Marshal(out)
	require.NoError(t, err)
	assert.JSONEq(t, `{"model":"m","prompt":"p","deferred":false}`, string(encoded))
}
