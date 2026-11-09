package channel

import (
	"encoding/base64"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
)

// ImageDataURIsFromForm 把 multipart 上传的图片转成 data URI，覆盖 OpenAI 允许的
// image、image[]、image[N] 三种命名，顺序与用户提交顺序保持一致。
func ImageDataURIsFromForm(c *gin.Context) ([]string, error) {
	form := c.Request.MultipartForm
	if form == nil || len(form.File) == 0 {
		return nil, nil
	}

	headers := make([]*multipart.FileHeader, 0, len(form.File))
	headers = append(headers, form.File["image"]...)
	headers = append(headers, form.File["image[]"]...)

	indexedKeys := make([]string, 0, len(form.File))
	for key := range form.File {
		if key != "image[]" && strings.HasPrefix(key, "image[") {
			indexedKeys = append(indexedKeys, key)
		}
	}
	sort.Strings(indexedKeys)
	for _, key := range indexedKeys {
		headers = append(headers, form.File[key]...)
	}

	images := make([]string, 0, len(headers))
	for i, header := range headers {
		file, err := header.Open()
		if err != nil {
			return nil, fmt.Errorf("failed to open image file %d: %w", i, err)
		}
		data, err := io.ReadAll(file)
		_ = file.Close()
		if err != nil {
			return nil, fmt.Errorf("failed to read image file %d: %w", i, err)
		}
		mimeType := http.DetectContentType(data)
		if !strings.HasPrefix(mimeType, "image/") {
			mimeType = detectImageMimeType(header.Filename)
		}
		images = append(images, fmt.Sprintf("data:%s;base64,%s", mimeType, base64.StdEncoding.EncodeToString(data)))
	}
	return images, nil
}

func detectImageMimeType(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	default:
		if strings.HasPrefix(ext, ".jp") {
			return "image/jpeg"
		}
		return "image/png"
	}
}
