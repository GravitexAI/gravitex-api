package openai

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

// TestConvertImageEditRequestJSONForGPTImage 验证 JSON 透传分支下 gpt-image 系列
// 把单数 image 字段规范化为 images 数组（Azure/OpenAI 新版 edits 要求）。
func TestConvertImageEditRequestJSONForGPTImage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	newJSONContext := func(t *testing.T) *gin.Context {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", nil)
		c.Request.Header.Set("Content-Type", "application/json")
		return c
	}
	info := &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeImagesEdits}

	t.Run("singular image string is normalized to images array", func(t *testing.T) {
		req := dto.ImageRequest{
			Model:  "gpt-image-2",
			Prompt: "edit",
			Image:  json.RawMessage(`"data:image/png;base64,abc"`),
		}
		converted, err := (&Adaptor{}).ConvertImageRequest(newJSONContext(t), info, req)
		require.NoError(t, err)
		got, ok := converted.(dto.ImageRequest)
		require.True(t, ok)
		assert.Equal(t, json.RawMessage(nil), got.Image, "singular image should be cleared")
		var images []string
		require.NoError(t, json.Unmarshal(got.Images, &images))
		assert.Equal(t, []string{"data:image/png;base64,abc"}, images)
	})

	t.Run("array image input is preserved", func(t *testing.T) {
		req := dto.ImageRequest{
			Model: "gpt-image-2",
			Image: json.RawMessage(`["a","b"]`),
		}
		converted, err := (&Adaptor{}).ConvertImageRequest(newJSONContext(t), info, req)
		require.NoError(t, err)
		got := converted.(dto.ImageRequest)
		var images []string
		require.NoError(t, json.Unmarshal(got.Images, &images))
		assert.Equal(t, []string{"a", "b"}, images)
	})

	t.Run("non-gpt-image model is untouched", func(t *testing.T) {
		req := dto.ImageRequest{
			Model: "dall-e-3",
			Image: json.RawMessage(`"data:image/png;base64,abc"`),
		}
		converted, err := (&Adaptor{}).ConvertImageRequest(newJSONContext(t), info, req)
		require.NoError(t, err)
		got := converted.(dto.ImageRequest)
		assert.Equal(t, json.RawMessage(`"data:image/png;base64,abc"`), got.Image)
		assert.Empty(t, got.Images)
	})

	t.Run("missing image returns clear error", func(t *testing.T) {
		req := dto.ImageRequest{Model: "gpt-image-2", Prompt: "no images here"}
		_, err := (&Adaptor{}).ConvertImageRequest(newJSONContext(t), info, req)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "image or images")
	})
}
