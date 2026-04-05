package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
)

// OSS_BASE64_ENDPOINT — Java backend URL for base64 upload.
// Example: http://localhost:8080/resource/oss/uploadByBase64
const (
	envOSSBase64Endpoint  = "OSS_BASE64_ENDPOINT"
	defaultOSSHTTPTimeout = 300 * time.Second
)

var ossHTTPClient = &http.Client{
	Timeout: defaultOSSHTTPTimeout,
}

// OSSUploadRequest is the request body for base64 upload to the Java backend.
type OSSUploadRequest struct {
	Base64Content string `json:"base64Content"`
	FileName      string `json:"fileName,omitempty"`
	ExtensionType string `json:"extensionType,omitempty"`
}

// OSSUploadResponse wraps the Java backend OSS upload response.
type OSSUploadResponse struct {
	Code int              `json:"code"`
	Msg  string           `json:"msg"`
	Data *OSSUploadResult `json:"data"`
}

// OSSUploadResult holds the uploaded file info returned by the Java backend.
type OSSUploadResult struct {
	OssID    string `json:"ossId"`
	OssURL   string `json:"url"`
	FileName string `json:"fileName"`
}

// GetOSSBase64Endpoint returns the configured Java backend base64 upload URL.
func GetOSSBase64Endpoint() string {
	return common.GetEnvOrDefaultString(envOSSBase64Endpoint, "")
}

// IsVideoOSSEnabled returns true when the OSS base64 endpoint is configured.
func IsVideoOSSEnabled() bool {
	return GetOSSBase64Endpoint() != ""
}

// UploadVideoFromURL downloads the video from remoteURL (with optional extra headers) and
// uploads it to OSS via the Java backend base64 endpoint.
// The Go backend handles the download so it can inject channel-specific auth headers.
func UploadVideoFromURL(ctx context.Context, remoteURL string, headers map[string]string, ext string) (string, error) {
	if ext == "" {
		ext = "mp4"
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, remoteURL, nil)
	if err != nil {
		return "", fmt.Errorf("create download request: %w", err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	client := GetHttpClient()
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("download video from %s: %w", remoteURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download video returned status %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read video body: %w", err)
	}

	dataURI := "data:video/" + ext + ";base64," + base64.StdEncoding.EncodeToString(data)
	return UploadBase64ToOSS(ctx, dataURI, "", ext)
}

// UploadBase64ToOSS calls the Java backend to upload base64-encoded content to OSS.
// dataURI should be a full data URI, e.g. "data:video/mp4;base64,AAA..."
func UploadBase64ToOSS(ctx context.Context, dataURI string, fileName string, extensionType string) (string, error) {
	endpoint := GetOSSBase64Endpoint()
	if endpoint == "" {
		return "", fmt.Errorf("OSS_BASE64_ENDPOINT not configured")
	}

	payload := OSSUploadRequest{
		Base64Content: dataURI,
		FileName:      fileName,
		ExtensionType: extensionType,
	}
	body, err := common.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal OSS request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create OSS upload request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := ossHTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("call OSS endpoint: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("OSS endpoint returned %d: %s", resp.StatusCode, string(errBody))
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read OSS response: %w", err)
	}

	var ossResp OSSUploadResponse
	if err = common.Unmarshal(respBody, &ossResp); err != nil {
		return "", fmt.Errorf("parse OSS response: %w", err)
	}

	if ossResp.Code != 200 {
		return "", fmt.Errorf("OSS upload failed: code=%d msg=%s", ossResp.Code, ossResp.Msg)
	}
	if ossResp.Data == nil || ossResp.Data.OssURL == "" {
		return "", fmt.Errorf("OSS upload returned empty URL")
	}

	return ossResp.Data.OssURL, nil
}
