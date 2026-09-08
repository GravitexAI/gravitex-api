package volcengine

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// pngBytes 是一个最小合法 PNG，用来让 http.DetectContentType 识别出 image/png。
var pngBytes = []byte{
	0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A,
	0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52,
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
}

func newImageEditContext(t *testing.T, filenames []string, fieldName func(int) string) *gin.Context {
	t.Helper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("model", "doubao-seedream-4-0-250828"))
	require.NoError(t, writer.WriteField("prompt", "make it snow"))
	require.NoError(t, writer.WriteField("response_format", "b64_json"))
	require.NoError(t, writer.WriteField("seed", "42"))
	require.NoError(t, writer.WriteField("sequential_image_generation", "auto"))
	require.NoError(t, writer.WriteField("sequential_image_generation_options", `{"max_images":4}`))
	for i, filename := range filenames {
		part, err := writer.CreateFormFile(fieldName(i), filename)
		require.NoError(t, err)
		_, err = part.Write(append(pngBytes, byte(i)))
		require.NoError(t, err)
	}
	require.NoError(t, writer.Close())

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", &body)
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())
	return c
}

func expectedDataURI(index int) string {
	return fmt.Sprintf("data:image/png;base64,%s", base64.StdEncoding.EncodeToString(append(pngBytes, byte(index))))
}

// TestConvertImageRequestEditsMultipartFiles 锁定豆包图生图的契约：豆包只吃
// generations 接口的 JSON body，multipart 上传的图片必须转成 base64 data URI 的
// image 字段，多图时序列化为数组。
func TestConvertImageRequestEditsMultipartFiles(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name      string
		filenames []string
		fieldName func(int) string
		wantImage string
	}{
		{
			name:      "single image file becomes a string",
			filenames: []string{"a.png"},
			fieldName: func(int) string { return "image" },
			wantImage: fmt.Sprintf("%q", expectedDataURI(0)),
		},
		{
			name:      "two image[] files become an array",
			filenames: []string{"a.png", "b.png"},
			fieldName: func(int) string { return "image[]" },
			wantImage: fmt.Sprintf(`[%q,%q]`, expectedDataURI(0), expectedDataURI(1)),
		},
		{
			name:      "indexed image[N] files keep index order",
			filenames: []string{"a.png", "b.png"},
			fieldName: func(i int) string { return fmt.Sprintf("image[%d]", i) },
			wantImage: fmt.Sprintf(`[%q,%q]`, expectedDataURI(0), expectedDataURI(1)),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newImageEditContext(t, tt.filenames, tt.fieldName)

			request, err := helper.GetAndValidOpenAIImageRequest(c, constant.RelayModeImagesEdits)
			require.NoError(t, err)

			adaptor := &Adaptor{}
			converted, err := adaptor.ConvertImageRequest(c, &relaycommon.RelayInfo{RelayMode: constant.RelayModeImagesEdits}, *request)
			require.NoError(t, err)

			body, ok := converted.(map[string]json.RawMessage)
			require.True(t, ok, "edits 必须转成 JSON map，豆包不接受 multipart")
			require.JSONEq(t, tt.wantImage, string(body["image"]))
			// form-data 的渠道专有参数必须原样带到上游，且保留 JSON 原始类型。
			require.JSONEq(t, `"b64_json"`, string(body["response_format"]))
			require.JSONEq(t, `42`, string(body["seed"]))
			require.JSONEq(t, `"auto"`, string(body["sequential_image_generation"]))
			require.JSONEq(t, `{"max_images":4}`, string(body["sequential_image_generation_options"]))
			// model / prompt 不能被 Extra 覆盖成表单原值。
			require.JSONEq(t, `"doubao-seedream-4-0-250828"`, string(body["model"]))
			require.JSONEq(t, `"make it snow"`, string(body["prompt"]))
		})
	}
}

// TestConvertImageRequestEditsKeepsJSONImages 保证 JSON 图生图请求里的多图数组原样透传，
// 不会被 multipart 补图逻辑覆盖，且 Extra 里的豆包专有参数照旧转发到上游
// —— 图生图与文生图共用 generations 接口，两者必须一致。
func TestConvertImageRequestEditsKeepsJSONImages(t *testing.T) {
	gin.SetMode(gin.TestMode)

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", nil)

	request := dto.ImageRequest{
		Model:  "doubao-seedream-4-0-250828",
		Prompt: "make it snow",
		Image:  json.RawMessage(`["https://example.com/1.png","https://example.com/2.png"]`),
		Extra: map[string]json.RawMessage{
			"sequential_image_generation": json.RawMessage(`"auto"`),
		},
	}

	adaptor := &Adaptor{}
	converted, err := adaptor.ConvertImageRequest(c, &relaycommon.RelayInfo{RelayMode: constant.RelayModeImagesEdits}, request)
	require.NoError(t, err)

	body, ok := converted.(map[string]json.RawMessage)
	require.True(t, ok)
	require.JSONEq(t, `["https://example.com/1.png","https://example.com/2.png"]`, string(body["image"]))
	require.JSONEq(t, `"auto"`, string(body["sequential_image_generation"]))

	// 序列化一次，确认最终发往上游的 body 里 image 是数组。
	raw, err := common.Marshal(body)
	require.NoError(t, err)
	require.Contains(t, string(raw), `"image":["https://example.com/1.png","https://example.com/2.png"]`)
}
