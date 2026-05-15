package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
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
