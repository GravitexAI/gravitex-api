package service

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"

	"github.com/byteplus-sdk/byteplus-go-sdk-v2/byteplus"
	"github.com/byteplus-sdk/byteplus-go-sdk-v2/byteplus/credentials"
	"github.com/byteplus-sdk/byteplus-go-sdk-v2/byteplus/session"
	"github.com/byteplus-sdk/byteplus-go-sdk-v2/byteplus/universal"
)

// ByteplusAssetConfig holds credentials for BytePlus Asset API calls.
type ByteplusAssetConfig struct {
	AK          string
	SK          string
	Region      string // e.g. "ap-southeast-1"
	ProjectName string // e.g. "default"
}

func newByteplusSession(cfg ByteplusAssetConfig) (*session.Session, error) {
	config := byteplus.NewConfig().
		WithCredentials(credentials.NewStaticCredentials(cfg.AK, cfg.SK, "")).
		WithRegion(cfg.Region)
	return session.NewSession(config)
}

func byteplusCall(cfg ByteplusAssetConfig, action string, body map[string]interface{}) (*map[string]interface{}, error) {
	sess, err := newByteplusSession(cfg)
	if err != nil {
		return nil, fmt.Errorf("create byteplus session: %w", err)
	}
	resp, err := universal.New(sess).DoCall(
		universal.RequestUniversal{
			ServiceName: "ark",
			Action:      action,
			Version:     "2024-01-01",
			HttpMethod:  universal.POST,
			ContentType: universal.ApplicationJSON,
		},
		&body,
	)
	if err != nil {
		return nil, fmt.Errorf("byteplus %s: %w", action, err)
	}
	return resp, nil
}

// parseResponse converts the SDK response map to a typed struct via JSON round-trip.
// If the response contains a "Result" key (BytePlus SDK envelope), it unwraps that level first.
func parseResponse(resp *map[string]interface{}, target interface{}) error {
	if resp == nil {
		return fmt.Errorf("nil response")
	}
	m := *resp
	// Unwrap "Result" envelope if present
	if result, ok := m["Result"]; ok {
		if rm, ok := result.(map[string]interface{}); ok {
			m = rm
		}
	}
	data, err := common.Marshal(m)
	if err != nil {
		return fmt.Errorf("marshal response: %w", err)
	}
	return common.Unmarshal(data, target)
}

// ---------- Asset Group ----------

// ByteplusAssetGroupInfo represents a BytePlus asset group.
type ByteplusAssetGroupInfo struct {
	Id          string `json:"Id"`
	Name        string `json:"Name"`
	Title       string `json:"Title"`
	Description string `json:"Description"`
	GroupType   string `json:"GroupType"`
	ProjectName string `json:"ProjectName"`
	CreateTime  string `json:"CreateTime"`
	UpdateTime  string `json:"UpdateTime"`
}

// ByteplusCreateAssetGroup creates an asset group and returns the group ID.
func ByteplusCreateAssetGroup(cfg ByteplusAssetConfig, name, description string) (string, error) {
	body := map[string]interface{}{
		"Name":        name,
		"Description": description,
		"GroupType":   "AIGC",
		"ProjectName": cfg.ProjectName,
	}
	resp, err := byteplusCall(cfg, "CreateAssetGroup", body)
	if err != nil {
		return "", err
	}

	// Debug: log raw response to diagnose field mapping
	if resp != nil {
		rawBytes, _ := common.Marshal(*resp)
		fmt.Printf("[ByteplusCreateAssetGroup] raw response: %s\n", string(rawBytes))
	}

	// Try extracting Id from top level first, then from nested Result
	groupId := extractStringField(resp, "Id")
	if groupId == "" {
		return "", fmt.Errorf("CreateAssetGroup returned empty Id, raw: %v", resp)
	}
	return groupId, nil
}

// extractStringField tries to find a string field by key in the response map.
// It checks the top level first, then looks inside "Result" if present.
func extractStringField(resp *map[string]interface{}, key string) string {
	if resp == nil {
		return ""
	}
	m := *resp
	// Top level
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	// Nested inside "Result"
	if result, ok := m["Result"]; ok {
		if rm, ok := result.(map[string]interface{}); ok {
			if v, ok := rm[key]; ok {
				if s, ok := v.(string); ok && s != "" {
					return s
				}
			}
		}
	}
	return ""
}

// ByteplusListAssetGroups lists asset groups with pagination.
func ByteplusListAssetGroups(cfg ByteplusAssetConfig, groupIds []string, pageNum, pageSize int) ([]ByteplusAssetGroupInfo, int, error) {
	filter := map[string]interface{}{
		"GroupType": "AIGC",
	}
	if len(groupIds) > 0 {
		filter["GroupIds"] = groupIds
	}
	body := map[string]interface{}{
		"Filter":     filter,
		"PageNumber": pageNum,
		"PageSize":   pageSize,
	}
	resp, err := byteplusCall(cfg, "ListAssetGroups", body)
	if err != nil {
		return nil, 0, err
	}
	var result struct {
		Items      []ByteplusAssetGroupInfo `json:"Items"`
		TotalCount int                      `json:"TotalCount"`
	}
	if err := parseResponse(resp, &result); err != nil {
		return nil, 0, err
	}
	return result.Items, result.TotalCount, nil
}

// ByteplusDeleteAssetGroup deletes an asset group by ID.
func ByteplusDeleteAssetGroup(cfg ByteplusAssetConfig, groupId string) error {
	body := map[string]interface{}{
		"Id":          groupId,
		"ProjectName": cfg.ProjectName,
	}
	_, err := byteplusCall(cfg, "DeleteAssetGroup", body)
	return err
}

// ---------- Asset ----------

// ByteplusAssetInfo represents a single BytePlus asset.
type ByteplusAssetInfo struct {
	Id          string `json:"Id"`
	GroupId     string `json:"GroupId"`
	Name        string `json:"Name"`
	AssetType   string `json:"AssetType"` // Image, Video, Audio
	Status      string `json:"Status"`    // Processing, Active, Failed
	URL         string `json:"URL"`       // signed URL, valid 12 hrs
	ProjectName string `json:"ProjectName"`
	CreateTime  string `json:"CreateTime"`
	UpdateTime  string `json:"UpdateTime"`
}

// ByteplusCreateAsset creates an asset in the specified group and returns the asset ID.
func ByteplusCreateAsset(cfg ByteplusAssetConfig, groupId, imageURL, assetType, name string) (string, error) {
	if assetType == "" {
		assetType = "Image"
	}
	body := map[string]interface{}{
		"GroupId":     groupId,
		"URL":         imageURL,
		"AssetType":   assetType,
		"ProjectName": cfg.ProjectName,
	}
	if name != "" {
		body["Name"] = name
	}
	resp, err := byteplusCall(cfg, "CreateAsset", body)
	if err != nil {
		return "", err
	}
	assetId := extractStringField(resp, "Id")
	if assetId == "" {
		return "", fmt.Errorf("CreateAsset returned empty Id, raw: %v", resp)
	}
	return assetId, nil
}

// ByteplusGetAsset retrieves a single asset's info.
func ByteplusGetAsset(cfg ByteplusAssetConfig, assetId string) (*ByteplusAssetInfo, error) {
	body := map[string]interface{}{
		"Id":          assetId,
		"ProjectName": cfg.ProjectName,
	}
	resp, err := byteplusCall(cfg, "GetAsset", body)
	if err != nil {
		return nil, err
	}
	var result ByteplusAssetInfo
	if err := parseResponse(resp, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ByteplusListAssets lists assets with optional group and status filtering.
func ByteplusListAssets(cfg ByteplusAssetConfig, groupId string, statuses []string, pageNum, pageSize int) ([]ByteplusAssetInfo, int, error) {
	filter := map[string]interface{}{
		"GroupType": "AIGC",
	}
	if groupId != "" {
		filter["GroupIds"] = []string{groupId}
	}
	if len(statuses) > 0 {
		filter["Statuses"] = statuses
	}
	body := map[string]interface{}{
		"Filter":     filter,
		"PageNumber": pageNum,
		"PageSize":   pageSize,
		"SortBy":     "CreateTime",
		"SortOrder":  "Desc",
	}
	resp, err := byteplusCall(cfg, "ListAssets", body)
	if err != nil {
		return nil, 0, err
	}
	var result struct {
		Items      []ByteplusAssetInfo `json:"Items"`
		TotalCount int                 `json:"TotalCount"`
	}
	if err := parseResponse(resp, &result); err != nil {
		return nil, 0, err
	}
	return result.Items, result.TotalCount, nil
}

// ByteplusDeleteAsset deletes an asset by ID.
func ByteplusDeleteAsset(cfg ByteplusAssetConfig, assetId string) error {
	body := map[string]interface{}{
		"Id":          assetId,
		"ProjectName": cfg.ProjectName,
	}
	_, err := byteplusCall(cfg, "DeleteAsset", body)
	return err
}

// ByteplusStatusToInternal maps BytePlus asset statuses to the gateway internal statuses.
func ByteplusStatusToInternal(bpStatus string) string {
	switch bpStatus {
	case "Active":
		return "active"
	case "Processing":
		return "pending"
	case "Failed":
		return "failed"
	default:
		return "pending"
	}
}
