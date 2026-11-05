package openai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
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
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestImage26GenerationRoundTrip(t *testing.T) {
	for _, body := range []string{
		`{"model":"gpt-image-2.6-flare","prompt":"x"}`,
		`{"model":"gpt-image-2.6-flare","prompt":"x","complexity":null,"frame_count":null,"frame_duration_ms":null}`,
		`{"model":"gpt-image-2.6-flare","prompt":"x","complexity":"invalid","frame_count":0,"frame_duration_ms":0,"stream":false,"svg":{"model":"author","reasoning":{"effort":"low"}},"unknown":"unchanged"}`,
		`{"model":"gpt-image-2.6-flare","prompt":"x","complexity":"low","frame_count":4,"frame_duration_ms":100,"output_format":"gif"}`,
	} {
		t.Run(body, func(t *testing.T) {
			var request dto.ImageRequest
			require.NoError(t, common.Unmarshal([]byte(body), &request))
			copied, err := common.DeepCopy(&request)
			require.NoError(t, err)
			converted, err := (&Adaptor{}).ConvertImageRequest(nil, &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeImagesGenerations}, *copied)
			require.NoError(t, err)
			data, err := common.Marshal(converted)
			require.NoError(t, err)
			var expected, actual map[string]any
			require.NoError(t, common.Unmarshal([]byte(body), &expected))
			require.NoError(t, common.Unmarshal(data, &actual))
			delete(expected, "unknown")
			for _, key := range []string{"complexity", "frame_count", "frame_duration_ms"} {
				if expected[key] == nil {
					delete(expected, key)
				}
			}
			assert.Equal(t, expected, actual)
		})
	}
}

func image26PNG(t *testing.T) []byte {
	t.Helper()
	var out bytes.Buffer
	require.NoError(t, png.Encode(&out, image.NewNRGBA(image.Rect(0, 0, 2, 2))))
	return out.Bytes()
}

func TestImage26JSONMarkupFile(t *testing.T) {
	data := image26PNG(t)
	uri := "data:image/png;base64," + base64.StdEncoding.EncodeToString(data)
	body, err := common.Marshal(map[string]any{"model": "gpt-image-2.6-flare", "prompt": "x", "images": []any{map[string]string{"image_url": uri}}, "markup": map[string]string{"image_url": uri}, "complexity": "low"})
	require.NoError(t, err)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	request, err := helper.GetAndValidOpenAIImageRequest(c, relayconstant.RelayModeImagesEdits)
	require.NoError(t, err)
	for i := 0; i < 2; i++ {
		copied, err := common.DeepCopy(request)
		require.NoError(t, err)
		converted, err := (&Adaptor{}).ConvertImageRequest(c, &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeImagesEdits}, *copied)
		require.NoError(t, err)
		_, params, err := mime.ParseMediaType(c.Request.Header.Get("Content-Type"))
		require.NoError(t, err)
		form, err := multipart.NewReader(bytes.NewReader(converted.(*bytes.Buffer).Bytes()), params["boundary"]).ReadForm(1 << 20)
		require.NoError(t, err)
		defer form.RemoveAll()
		assert.Equal(t, []string{"low"}, form.Value["complexity"])
		assert.Empty(t, form.Value["markup"])
		require.Len(t, form.File["markup"], 1)
		assert.Equal(t, "image/png", form.File["markup"][0].Header.Get("Content-Type"))
		file, err := form.File["markup"][0].Open()
		require.NoError(t, err)
		got, err := io.ReadAll(file)
		require.NoError(t, err)
		require.NoError(t, file.Close())
		assert.Equal(t, data, got)
	}
}

func TestImage26MarkupInvalidReferences(t *testing.T) {
	for _, raw := range []string{`{}`, `null`, `"text"`, `{"image_url":""}`, `{"file_id":"file-x"}`, `{"image_url":"https://example.com/x","file_id":"file-x"}`, `{"image_url":7}`, `{"image_url":"http://example.com/x"}`, `{"image_url":"data:image/png;base64,YmFk"}`} {
		t.Run(raw, func(t *testing.T) {
			_, err := readImageMarkup(context.Background(), json.RawMessage(raw))
			require.Error(t, err)
			var apiErr *types.NewAPIError
			require.ErrorAs(t, err, &apiErr)
			assert.Equal(t, 400, apiErr.StatusCode)
			assert.True(t, types.IsSkipRetryError(apiErr))
		})
	}
	assert.Error(t, validateImageMarkup(make([]byte, maxImageSize+1)))
}

func TestImage26MultipartMarkupOrderAndReplay(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("model", "gpt-image-2.6-flare"))
	require.NoError(t, writer.WriteField("prompt", "x"))
	require.NoError(t, writer.WriteField("complexity", "medium"))
	for _, name := range []string{"image[10]", "image[2]"} {
		part, err := writer.CreateFormFile(name, "input.png")
		require.NoError(t, err)
		_, err = io.Copy(part, strings.NewReader(name))
		require.NoError(t, err)
	}
	data := image26PNG(t)
	part, err := writer.CreateFormFile("markup", "overlay.png")
	require.NoError(t, err)
	_, err = part.Write(data)
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", bytes.NewReader(body.Bytes()))
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())
	request, err := helper.GetAndValidOpenAIImageRequest(c, relayconstant.RelayModeImagesEdits)
	require.NoError(t, err)
	for i := 0; i < 2; i++ {
		converted, err := (&Adaptor{}).ConvertImageRequest(c, &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeImagesEdits}, *request)
		require.NoError(t, err)
		_, params, err := mime.ParseMediaType(c.Request.Header.Get("Content-Type"))
		require.NoError(t, err)
		form, err := multipart.NewReader(bytes.NewReader(converted.(*bytes.Buffer).Bytes()), params["boundary"]).ReadForm(1 << 20)
		require.NoError(t, err)
		defer form.RemoveAll()
		assert.Equal(t, []string{"medium"}, form.Value["complexity"])
		require.Len(t, form.File["markup"], 1)
		require.Len(t, form.File["image[]"], 2)
		for j, expected := range []string{"image[2]", "image[10]"} {
			file, err := form.File["image[]"][j].Open()
			require.NoError(t, err)
			got, err := io.ReadAll(file)
			require.NoError(t, err)
			require.NoError(t, file.Close())
			assert.Equal(t, expected, string(got))
		}
		file, err := form.File["markup"][0].Open()
		require.NoError(t, err)
		got, err := io.ReadAll(file)
		require.NoError(t, err)
		require.NoError(t, file.Close())
		assert.Equal(t, data, got)
		assert.Equal(t, "overlay.png", form.File["markup"][0].Filename)
	}
}

func TestImage26MarkupHTTPS(t *testing.T) {
	data := image26PNG(t)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/downgrade" {
			http.Redirect(w, r, "http://example.com/markup.png", http.StatusFound)
			return
		}
		if r.URL.Path == "/missing" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.URL.Path == "/invalid" {
			_, _ = w.Write([]byte("not PNG"))
			return
		}
		_, _ = w.Write(data)
	}))
	defer server.Close()
	service.InitHttpClient()
	settings := system_setting.GetFetchSetting()
	previous := *settings
	t.Cleanup(func() { *settings = previous })
	settings.EnableSSRFProtection = true
	settings.AllowPrivateIp = false
	settings.AllowedPorts = []string{"1-65535"}
	raw, err := common.Marshal(map[string]string{"image_url": server.URL})
	require.NoError(t, err)
	_, err = readImageMarkup(context.Background(), raw)
	require.Error(t, err, "private targets must be blocked by the protected client")
	// Trust only this local TLS fixture for successful HTTP transfer tests.
	client := service.GetSSRFProtectedHTTPClient()
	previousTransport := client.Transport
	client.Transport = server.Client().Transport
	t.Cleanup(func() { client.Transport = previousTransport })
	got, err := readImageMarkup(context.Background(), raw)
	require.NoError(t, err)
	assert.Equal(t, data, got)
	for _, path := range []string{"/missing", "/invalid", "/downgrade"} {
		raw, err := common.Marshal(map[string]string{"image_url": server.URL + path})
		require.NoError(t, err)
		_, err = readImageMarkup(context.Background(), raw)
		require.Error(t, err)
	}
}

func TestImage26MultipartRejectsTextAndDuplicateMarkup(t *testing.T) {
	for _, fileCount := range []int{0, 2} {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		require.NoError(t, writer.WriteField("model", "gpt-image-2.6-flare"))
		if fileCount == 0 {
			require.NoError(t, writer.WriteField("markup", "text"))
		}
		imagePart, err := writer.CreateFormFile("image", "input.png")
		require.NoError(t, err)
		_, err = imagePart.Write(image26PNG(t))
		require.NoError(t, err)
		for i := 0; i < fileCount; i++ {
			part, err := writer.CreateFormFile("markup", "overlay.png")
			require.NoError(t, err)
			_, err = part.Write(image26PNG(t))
			require.NoError(t, err)
		}
		require.NoError(t, writer.Close())
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", bytes.NewReader(body.Bytes()))
		c.Request.Header.Set("Content-Type", writer.FormDataContentType())
		request, err := helper.GetAndValidOpenAIImageRequest(c, relayconstant.RelayModeImagesEdits)
		require.NoError(t, err)
		_, err = (&Adaptor{}).ConvertImageRequest(c, &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeImagesEdits}, *request)
		require.Error(t, err)
		var apiErr *types.NewAPIError
		require.ErrorAs(t, err, &apiErr)
		assert.Equal(t, 400, apiErr.StatusCode)
		assert.True(t, types.IsSkipRetryError(apiErr))
	}
}
