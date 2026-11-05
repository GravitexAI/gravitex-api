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

// GPT Image 2.6 parameters must reach the upstream exactly as the client sent
// them, including null and out-of-range values.
func TestImage26GenerationRoundTrip(t *testing.T) {
	for _, body := range []string{
		`{"model":"gpt-image-2.6-flare","prompt":"x"}`,
		`{"model":"gpt-image-2.6-flare","prompt":"x","complexity":null,"frame_count":null,"frame_duration_ms":null}`,
		`{"model":"gpt-image-2.6-flare","prompt":"x","complexity":"invalid","frame_count":"nine","frame_duration_ms":1.5,"stream":false}`,
		`{"model":"gpt-image-2.6-flare","prompt":"x","complexity":"low","frame_count":64,"frame_duration_ms":100,"output_format":"gif"}`,
		`{"model":"gpt-image-2.6-flare","prompt":"x","n":1,"output_format":"svg","svg":{"model":"author","reasoning":{"effort":"low"}},"safety_identifier":"user-hash","prompt_rewriter":{"enabled":false}}`,
	} {
		t.Run(body, func(t *testing.T) {
			var request dto.ImageRequest
			require.NoError(t, common.Unmarshal([]byte(body), &request))
			converted, err := (&Adaptor{}).ConvertImageRequest(nil, &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeImagesGenerations}, request)
			require.NoError(t, err)
			data, err := common.Marshal(converted)
			require.NoError(t, err)
			assert.JSONEq(t, body, string(data))
		})
	}
}

func image26PNG(t *testing.T) []byte {
	t.Helper()
	var out bytes.Buffer
	require.NoError(t, png.Encode(&out, image.NewNRGBA(image.Rect(0, 0, 2, 2))))
	return out.Bytes()
}

func readImage26Form(t *testing.T, c *gin.Context, converted any) *multipart.Form {
	t.Helper()
	_, params, err := mime.ParseMediaType(c.Request.Header.Get("Content-Type"))
	require.NoError(t, err)
	form, err := multipart.NewReader(bytes.NewReader(converted.(*bytes.Buffer).Bytes()), params["boundary"]).ReadForm(1 << 20)
	require.NoError(t, err)
	t.Cleanup(func() { _ = form.RemoveAll() })
	return form
}

func readImage26File(t *testing.T, header *multipart.FileHeader) []byte {
	t.Helper()
	file, err := header.Open()
	require.NoError(t, err)
	defer file.Close()
	data, err := io.ReadAll(file)
	require.NoError(t, err)
	return data
}

// JSON edits are converted to multipart for gpt-image models; every 2.6 field
// must survive that conversion, and replaying the conversion on retry must
// produce the same form.
func TestImage26JSONEditToMultipart(t *testing.T) {
	data := image26PNG(t)
	uri := "data:image/png;base64," + base64.StdEncoding.EncodeToString(data)
	body, err := common.Marshal(map[string]any{
		"model":             "gpt-image-2.6-flare",
		"prompt":            "x",
		"images":            []any{map[string]string{"image_url": uri}},
		"markup":            map[string]string{"image_url": uri},
		"complexity":        "low",
		"frame_count":       4,
		"frame_duration_ms": 100,
		"safety_identifier": "user-hash",
		"complexity_extra":  nil,
	})
	require.NoError(t, err)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	request, err := helper.GetAndValidOpenAIImageRequest(c, relayconstant.RelayModeImagesEdits)
	require.NoError(t, err)
	for i := 0; i < 2; i++ {
		converted, err := (&Adaptor{}).ConvertImageRequest(c, &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeImagesEdits}, *request)
		require.NoError(t, err)
		form := readImage26Form(t, c, converted)
		assert.Equal(t, []string{"low"}, form.Value["complexity"])
		assert.Equal(t, []string{"4"}, form.Value["frame_count"])
		assert.Equal(t, []string{"100"}, form.Value["frame_duration_ms"])
		assert.Equal(t, []string{"user-hash"}, form.Value["safety_identifier"])
		assert.Empty(t, form.Value["markup"])
		require.Len(t, form.File["markup"], 1)
		assert.Equal(t, "image/png", form.File["markup"][0].Header.Get("Content-Type"))
		assert.Equal(t, data, readImage26File(t, form.File["markup"][0]))
	}
}

func TestImage26MarkupReferences(t *testing.T) {
	for _, raw := range []string{`{}`, `null`, `"text"`, `{"image_url":""}`, `{"file_id":"file-x"}`, `{"image_url":7}`, `{"image_url":"http://example.com/x"}`, `{"image_url":"data:image/png,raw"}`} {
		t.Run(raw, func(t *testing.T) {
			_, err := readImageMarkup(context.Background(), json.RawMessage(raw))
			require.Error(t, err)
			var apiErr *types.NewAPIError
			require.ErrorAs(t, err, &apiErr)
			assert.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
			assert.True(t, types.IsSkipRetryError(apiErr))
		})
	}
	// Content is not validated here: a non-PNG payload is forwarded and the upstream decides.
	got, err := readImageMarkup(context.Background(), json.RawMessage(`{"image_url":"data:image/jpeg;base64,YmFk"}`))
	require.NoError(t, err)
	assert.Equal(t, []byte("bad"), got)
}

// Native multipart markup is forwarded untouched: every file part keeps its
// bytes and filename, text values pass through, and indexed images keep order.
func TestImage26MultipartMarkupPassthrough(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("model", "gpt-image-2.6-flare"))
	require.NoError(t, writer.WriteField("prompt", "x"))
	require.NoError(t, writer.WriteField("complexity", "medium"))
	require.NoError(t, writer.WriteField("markup", "text-markup"))
	for _, name := range []string{"image[10]", "image[2]"} {
		part, err := writer.CreateFormFile(name, "input.png")
		require.NoError(t, err)
		_, err = io.Copy(part, strings.NewReader(name))
		require.NoError(t, err)
	}
	for _, content := range []string{"overlay-a", "overlay-b"} {
		part, err := writer.CreateFormFile("markup", content+".png")
		require.NoError(t, err)
		_, err = part.Write([]byte(content))
		require.NoError(t, err)
	}
	require.NoError(t, writer.Close())
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", bytes.NewReader(body.Bytes()))
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())
	request, err := helper.GetAndValidOpenAIImageRequest(c, relayconstant.RelayModeImagesEdits)
	require.NoError(t, err)
	for i := 0; i < 2; i++ {
		converted, err := (&Adaptor{}).ConvertImageRequest(c, &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeImagesEdits}, *request)
		require.NoError(t, err)
		form := readImage26Form(t, c, converted)
		assert.Equal(t, []string{"medium"}, form.Value["complexity"])
		assert.Equal(t, []string{"text-markup"}, form.Value["markup"])
		require.Len(t, form.File["image[]"], 2)
		for j, expected := range []string{"image[2]", "image[10]"} {
			assert.Equal(t, expected, string(readImage26File(t, form.File["image[]"][j])))
		}
		require.Len(t, form.File["markup"], 2)
		for j, expected := range []string{"overlay-a", "overlay-b"} {
			assert.Equal(t, expected, string(readImage26File(t, form.File["markup"][j])))
			assert.Equal(t, expected+".png", form.File["markup"][j].Filename)
			assert.Equal(t, "image/png", form.File["markup"][j].Header.Get("Content-Type"))
		}
	}
}

func TestImage26MarkupHTTPS(t *testing.T) {
	data := image26PNG(t)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/downgrade":
			http.Redirect(w, r, "http://example.com/markup.png", http.StatusFound)
		case "/missing":
			w.WriteHeader(http.StatusNotFound)
		case "/other":
			_, _ = w.Write([]byte("not PNG"))
		default:
			_, _ = w.Write(data)
		}
	}))
	defer server.Close()
	service.InitHttpClient()
	settings := system_setting.GetFetchSetting()
	previous := *settings
	t.Cleanup(func() { *settings = previous })
	settings.EnableSSRFProtection = true
	settings.AllowPrivateIp = false
	settings.AllowedPorts = []string{"1-65535"}
	fetch := func(path string) ([]byte, error) {
		raw, err := common.Marshal(map[string]string{"image_url": server.URL + path})
		require.NoError(t, err)
		return readImageMarkup(context.Background(), raw)
	}
	_, err := fetch("")
	require.Error(t, err, "private targets must be blocked by the protected client")
	// Trust only this local TLS fixture for successful HTTP transfer tests.
	client := service.GetSSRFProtectedHTTPClient()
	previousTransport := client.Transport
	client.Transport = server.Client().Transport
	t.Cleanup(func() { client.Transport = previousTransport })
	got, err := fetch("")
	require.NoError(t, err)
	assert.Equal(t, data, got)
	got, err = fetch("/other")
	require.NoError(t, err)
	assert.Equal(t, []byte("not PNG"), got)
	for _, path := range []string{"/missing", "/downgrade"} {
		_, err := fetch(path)
		require.Error(t, err, path)
	}
}
