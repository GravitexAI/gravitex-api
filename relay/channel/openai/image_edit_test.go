package openai

import (
	"bytes"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// parseMultipartFields 解析 multipart/form-data body 中的非文件字段为 map，
// 方便测试断言。文件字段直接跳过（filename != ""）。
func parseMultipartFields(t *testing.T, body []byte, boundary string) map[string]string {
	t.Helper()
	reader := multipart.NewReader(bytes.NewReader(body), boundary)
	fields := map[string]string{}
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		if part.FileName() != "" {
			_ = part.Close()
			continue
		}
		buf := new(bytes.Buffer)
		_, copyErr := io.Copy(buf, part)
		_ = part.Close()
		require.NoError(t, copyErr)
		fields[part.FormName()] = buf.String()
	}
	return fields
}

// TestConvertImageEditRequestMultipart verifies that ConvertImageRequest
// re-serializes multipart image edit requests with all fields (including
// stream) and the file intact, both when the form was already parsed and when
// it must be re-parsed from the reusable body.
func TestConvertImageEditRequestMultipart(t *testing.T) {
	gin.SetMode(gin.TestMode)

	newMultipartContext := func(t *testing.T, prompt string) *gin.Context {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		require.NoError(t, writer.WriteField("model", "gpt-image-1"))
		require.NoError(t, writer.WriteField("prompt", prompt))
		require.NoError(t, writer.WriteField("stream", "true"))
		require.NoError(t, writer.WriteField("partial_images", "3"))
		part, err := writer.CreateFormFile("image", "input.png")
		require.NoError(t, err)
		_, err = part.Write([]byte("fake image"))
		require.NoError(t, err)
		require.NoError(t, writer.Close())

		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", &body)
		c.Request.Header.Set("Content-Type", writer.FormDataContentType())
		return c
	}

	convertAndReplay := func(t *testing.T, c *gin.Context, prompt string) {
		info := &relaycommon.RelayInfo{
			RelayMode: relayconstant.RelayModeImagesEdits,
		}
		request := dto.ImageRequest{
			Model:  "gpt-image-1",
			Prompt: prompt,
			Stream: common.GetPointer(true),
		}

		converted, err := (&Adaptor{}).ConvertImageRequest(c, info, request)
		require.NoError(t, err)
		convertedBody, ok := converted.(*bytes.Buffer)
		require.True(t, ok)

		replayedRequest := httptest.NewRequest(http.MethodPost, "/v1/images/edits", bytes.NewReader(convertedBody.Bytes()))
		replayedRequest.Header.Set("Content-Type", c.Request.Header.Get("Content-Type"))
		require.NoError(t, replayedRequest.ParseMultipartForm(32<<20))

		require.Equal(t, "gpt-image-1", replayedRequest.PostForm.Get("model"))
		require.Equal(t, prompt, replayedRequest.PostForm.Get("prompt"))
		require.Equal(t, "true", replayedRequest.PostForm.Get("stream"))
		require.Equal(t, "3", replayedRequest.PostForm.Get("partial_images"))
		require.Len(t, replayedRequest.MultipartForm.File["image"], 1)

		file, err := replayedRequest.MultipartForm.File["image"][0].Open()
		require.NoError(t, err)
		defer file.Close()
		fileBytes, err := io.ReadAll(file)
		require.NoError(t, err)
		require.Equal(t, []byte("fake image"), fileBytes)
	}

	t.Run("with pre-parsed form", func(t *testing.T) {
		prompt := "edit this image"
		c := newMultipartContext(t, prompt)
		require.NoError(t, c.Request.ParseMultipartForm(32<<20))

		convertAndReplay(t, c, prompt)
	})

	t.Run("re-parses reusable body when form is missing", func(t *testing.T) {
		prompt := "edit without pre-parsed form"
		c := newMultipartContext(t, prompt)

		storage, err := common.GetBodyStorage(c)
		require.NoError(t, err)
		c.Request.Body = io.NopCloser(storage)
		c.Request.MultipartForm = nil
		c.Request.PostForm = nil

		convertAndReplay(t, c, prompt)
	})
}

// TestConvertImageEditRequestJSONForGPTImage 验证 gpt-image 系列即使客户端发 JSON，
// 也会被强制转成 multipart 提交给上游 —— Azure /v1/images/edits 的 JSON 模式既不接受
// 单数 `image` 字符串，也不接受 `images` 字符串数组，唯一稳定可用的是 multipart。
// 非 gpt-image 模型仍然走 JSON 透传 fast-path（保持上游官方行为）。
func TestConvertImageEditRequestJSONForGPTImage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	newJSONContext := func(t *testing.T, body string) *gin.Context {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", bytes.NewReader([]byte(body)))
		c.Request.Header.Set("Content-Type", "application/json")
		return c
	}
	info := &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeImagesEdits}
	// 一个 1x1 透明 PNG 的 base64，避免 multipart 转换里的 MIME 嗅探报错
	const tinyPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPj/HwAEAgH/+OuFAAAAAElFTkSuQmCC"

	t.Run("gpt-image with JSON content-type is converted to multipart with all fields", func(t *testing.T) {
		fidelity := "low"
		n := uint(2)
		req := dto.ImageRequest{
			Model:         "gpt-image-2",
			Prompt:        "换个背景颜色",
			Size:          "1024x1024",
			Quality:       "medium",
			N:             &n,
			InputFidelity: &fidelity,
			Image:         json.RawMessage(`"data:image/png;base64,` + tinyPNG + `"`),
		}
		c := newJSONContext(t, "{}")
		converted, err := (&Adaptor{}).ConvertImageRequest(c, info, req)
		require.NoError(t, err)
		// 强制走 multipart：返回 *bytes.Buffer 而不是原始 dto.ImageRequest
		buf, ok := converted.(*bytes.Buffer)
		require.True(t, ok, "gpt-image JSON 请求应被转换为 multipart body")

		// 解析 multipart body，验证 prompt/size/quality/n/input_fidelity 字段都在
		// （回归：b617e5c8 合并 main 时把这些 WriteField 调用一起删掉了，导致 Azure 报
		// "Missing required parameter: 'prompt'"）
		ct := c.Request.Header.Get("Content-Type")
		require.True(t, strings.HasPrefix(ct, "multipart/form-data"), "Content-Type should be multipart after conversion, got %q", ct)
		_, params, err := mime.ParseMediaType(ct)
		require.NoError(t, err)
		fields := parseMultipartFields(t, buf.Bytes(), params["boundary"])
		assert.Equal(t, "gpt-image-2", fields["model"])
		assert.Equal(t, "换个背景颜色", fields["prompt"])
		assert.Equal(t, "1024x1024", fields["size"])
		assert.Equal(t, "medium", fields["quality"])
		assert.Equal(t, "2", fields["n"])
		assert.Equal(t, "low", fields["input_fidelity"])
	})

	t.Run("non-gpt-image JSON model still passes through", func(t *testing.T) {
		req := dto.ImageRequest{
			Model: "dall-e-3",
			Image: json.RawMessage(`"data:image/png;base64,` + tinyPNG + `"`),
		}
		converted, err := (&Adaptor{}).ConvertImageRequest(newJSONContext(t, "{}"), info, req)
		require.NoError(t, err)
		got, ok := converted.(dto.ImageRequest)
		require.True(t, ok, "非 gpt-image 模型应原样透传 dto.ImageRequest")
		assert.Equal(t, "dall-e-3", got.Model)
	})
}
