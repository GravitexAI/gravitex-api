package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
)

type UploadByUrlBo struct {
	URL           string `json:"url"`
	ExtensionType string `json:"extensionType"`
	IsPermanent   int    `json:"isPermanent"`
}

type SysOssUploadVo struct {
	URL      string `json:"url"`
	FileName string `json:"fileName"`
	OssId    string `json:"ossId"`
}

type R struct {
	Code int            `json:"code"`
	Msg  string         `json:"msg"`
	Data SysOssUploadVo `json:"data"`
}

func UploadByImageURL(imageURL, token string) (*string, error) {

	api := os.Getenv("GRAVITEX_API_END")
	if api == "" {
		return nil, fmt.Errorf("GRAVITEX_API_END not set")
	}

	reqBody := UploadByUrlBo{
		URL:           imageURL,
		ExtensionType: "png",
		IsPermanent:   1,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}
	url := api + "/resource/oss/uploadByImageUrl"

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http error: %d, body: %s", resp.StatusCode, string(body))
	}

	var result R
	err = json.Unmarshal(body, &result)
	if err != nil {
		return nil, err
	}

	if result.Code != 200 {
		return nil, fmt.Errorf("upload failed: %s", result.Msg)
	}

	return &result.Data.URL, nil
}

// assetOSSUploadTimeout is the hard deadline for staging an asset URL into OSS.
// Intentionally short: if the Java backend cannot download + re-upload within
// 30 s the caller falls back to the original URL rather than blocking the
// Seedance request for too long.
const assetOSSUploadTimeout = 30 * time.Second

// IsAssetOSSStagingEnabled reports whether OSS staging is turned on.
// Controlled by env var ASSET_OSS_STAGING_ENABLED (default: "false").
// Only "true" or "1" (case-insensitive, trimmed) enables staging.
func IsAssetOSSStagingEnabled() bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv("ASSET_OSS_STAGING_ENABLED")))
	return v == "true" || v == "1"
}

// UploadAssetURLToOSS stages a remote asset URL into our own OSS via the Java
// backend and returns the resulting public OSS URL. The Java backend performs
// the actual download + re-upload, so the Go gateway never buffers large media
// in memory.
//
// Endpoint is chosen by assetType (BytePlus title-case: "Image"/"Video"/"Audio"):
//   - "Image"         -> /resource/oss/uploadByImageUrl
//   - "Video"/"Audio" -> /resource/oss/uploadByVideoUrl
//
// Both endpoints are @SaIgnore on the Java side — no auth header required.
// ext is the file extension hint (without dot); empty falls back to a
// type-appropriate default. Fails fast after assetOSSUploadTimeout (30 s).
func UploadAssetURLToOSS(rawURL, assetType, ext string) (ossURL string, err error) {
	api := os.Getenv("GRAVITEX_API_END")
	if api == "" {
		return "", fmt.Errorf("GRAVITEX_API_END not set")
	}
	api = strings.TrimRight(api, "/")

	path := "/resource/oss/uploadByImageUrl"
	if assetType == "Video" || assetType == "Audio" {
		path = "/resource/oss/uploadByVideoUrl"
	}
	if ext == "" {
		switch assetType {
		case "Video":
			ext = "mp4"
		case "Audio":
			ext = "mp3"
		default:
			ext = "png"
		}
	}

	reqBody := UploadByUrlBo{
		URL:           rawURL,
		ExtensionType: ext,
		IsPermanent:   1,
	}
	jsonData, err := common.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal OSS request: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, api+path, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("build OSS request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: assetOSSUploadTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("call OSS endpoint %s: %w", api+path, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read OSS response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("OSS http %d: %s", resp.StatusCode, string(body))
	}

	var result R
	if err := common.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("parse OSS response: %w", err)
	}
	if result.Code != 200 {
		return "", fmt.Errorf("OSS upload failed: code=%d msg=%s", result.Code, result.Msg)
	}
	if result.Data.URL == "" {
		return "", fmt.Errorf("OSS upload returned empty url")
	}
	return result.Data.URL, nil
}
