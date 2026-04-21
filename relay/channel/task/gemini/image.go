package gemini

import (
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
)

const maxVeoImageSize = 20 * 1024 * 1024 // 20 MB

// ExtractMultipartImage reads the first `input_reference` file from a multipart
// form upload and returns a VeoImageInput. Returns nil if no file is present.
func ExtractMultipartImage(c *gin.Context, info *relaycommon.RelayInfo) *VeoImageInput {
	mf, err := c.MultipartForm()
	if err != nil {
		return nil
	}
	files, exists := mf.File["input_reference"]
	if !exists || len(files) == 0 {
		return nil
	}
	fh := files[0]
	if fh.Size > maxVeoImageSize {
		return nil
	}
	file, err := fh.Open()
	if err != nil {
		return nil
	}
	defer file.Close()

	fileBytes, err := io.ReadAll(file)
	if err != nil {
		return nil
	}

	mimeType := fh.Header.Get("Content-Type")
	if mimeType == "" || mimeType == "application/octet-stream" {
		mimeType = http.DetectContentType(fileBytes)
	}

	info.Action = constant.TaskActionGenerate
	return &VeoImageInput{
		BytesBase64Encoded: base64.StdEncoding.EncodeToString(fileBytes),
		MimeType:           mimeType,
	}
}

// ParseImageInput parses an image string (data URI, HTTP URL, or raw base64) into a
// VeoImageInput. Returns (nil, nil) if the input is empty; returns (nil, error) on download failure.
func ParseImageInput(imageStr string) (*VeoImageInput, error) {
	imageStr = strings.TrimSpace(imageStr)
	if imageStr == "" {
		return nil, nil
	}

	if strings.HasPrefix(imageStr, "data:") {
		return parseDataURI(imageStr), nil
	}

	// HTTP/HTTPS URL — download and convert to base64
	if strings.HasPrefix(imageStr, "http://") || strings.HasPrefix(imageStr, "https://") {
		result, err := downloadImageToBase64(imageStr)
		if err != nil {
			return nil, err
		}
		return result, nil
	}

	raw, err := base64.StdEncoding.DecodeString(imageStr)
	if err != nil {
		return nil, nil
	}
	return &VeoImageInput{
		BytesBase64Encoded: imageStr,
		MimeType:           http.DetectContentType(raw),
	}, nil
}

// downloadImageToBase64 downloads an image from a URL and returns a VeoImageInput with base64 data.
func downloadImageToBase64(url string) (*VeoImageInput, error) {
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to download image from URL: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to download image from URL: HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read image data: %w", err)
	}
	mimeType := http.DetectContentType(data)
	return &VeoImageInput{
		BytesBase64Encoded: base64.StdEncoding.EncodeToString(data),
		MimeType:           mimeType,
	}, nil
}

func parseDataURI(uri string) *VeoImageInput {
	// data:image/png;base64,iVBOR...
	rest := uri[len("data:"):]
	idx := strings.Index(rest, ",")
	if idx < 0 {
		return nil
	}
	meta := rest[:idx]
	b64 := rest[idx+1:]
	if b64 == "" {
		return nil
	}

	mimeType := "application/octet-stream"
	parts := strings.SplitN(meta, ";", 2)
	if len(parts) >= 1 && parts[0] != "" {
		mimeType = parts[0]
	}

	return &VeoImageInput{
		BytesBase64Encoded: b64,
		MimeType:           mimeType,
	}
}
