package controller

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

const (
	assetUploadTimeout = 120 * time.Second // upstream upload timeout
	assetFetchTimeout  = 30 * time.Second  // upstream GET/DELETE timeout
)

// ---------- uptoken asset response types ----------

type uptokenAssetResponse struct {
	VirtualId      string `json:"virtual_id"`
	AssetUrl       string `json:"asset_url"`
	Url            string `json:"url"`
	Filename       string `json:"filename"`
	ContentType    string `json:"content_type"`
	SizeBytes      int64  `json:"size_bytes"`
	Status         string `json:"status"`
	ByteplusStatus string `json:"byteplus_status"`
	CreatedAt      string `json:"created_at"`
}

// ---------- helpers ----------

// getUptokenChannel finds an active uptoken channel to use for proxying asset requests.
func getUptokenChannel() (*model.Channel, error) {
	var channel model.Channel
	err := model.DB.Where("type = ? AND status = ?", constant.ChannelTypeUptoken, common.ChannelStatusEnabled).First(&channel).Error
	if err != nil {
		return nil, fmt.Errorf("no active uptoken channel found: %w", err)
	}
	return &channel, nil
}

func uptokenHTTPClient(ch *model.Channel) (*http.Client, error) {
	proxy := ch.GetSetting().Proxy
	return service.GetHttpClientWithProxy(proxy)
}

func assetErrorResponse(c *gin.Context, status int, message string) {
	c.JSON(status, gin.H{
		"error": gin.H{
			"message": message,
			"type":    "invalid_request_error",
		},
	})
}

// ---------- handlers ----------

// isAllowedImageType checks whether the content type is an allowed image format.
// Also accepts common browser-reported variants (e.g. "image/jpg").
func isAllowedImageType(ct string) bool {
	ct = strings.ToLower(strings.TrimSpace(ct))
	switch ct {
	case "image/jpeg", "image/jpg", "image/png", "image/webp", "image/gif", "image/heic", "image/heif":
		return true
	}
	return false
}

// UploadAsset proxies a multipart file upload to uptoken POST /v1/assets
// and records the user-asset mapping locally.
func UploadAsset(c *gin.Context) {
	userId := c.GetInt("id")
	tokenId := c.GetInt("token_id")

	ch, err := getUptokenChannel()
	if err != nil {
		logger.LogError(c.Request.Context(), "UploadAsset: "+err.Error())
		assetErrorResponse(c, http.StatusServiceUnavailable, "Asset service unavailable")
		return
	}

	// Read uploaded file
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("UploadAsset: FormFile error: %s", err.Error()))
		assetErrorResponse(c, http.StatusBadRequest, "Missing file field")
		return
	}
	defer file.Close()

	// Validate file type — accept common image MIME types; be lenient since browsers vary
	ct := header.Header.Get("Content-Type")
	if ct == "" || ct == "application/octet-stream" {
		// Fallback: infer from filename extension
		ct = inferImageContentType(header.Filename)
	}
	if !isAllowedImageType(ct) {
		assetErrorResponse(c, http.StatusBadRequest, fmt.Sprintf("Unsupported file type: %s", ct))
		return
	}

	// Validate size (30MB)
	if header.Size > 30*1024*1024 {
		assetErrorResponse(c, http.StatusBadRequest, "File too large, max 30MB")
		return
	}

	// Build multipart body for upstream
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile("file", header.Filename)
	if err != nil {
		assetErrorResponse(c, http.StatusInternalServerError, "Failed to build upstream request")
		return
	}
	if _, err := io.Copy(part, file); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("UploadAsset: io.Copy error: %s", err.Error()))
		assetErrorResponse(c, http.StatusInternalServerError, "Failed to read file")
		return
	}
	writer.Close()

	baseURL := ch.GetBaseURL()
	if baseURL == "" {
		baseURL = constant.ChannelBaseURLs[constant.ChannelTypeUptoken]
	}
	url := fmt.Sprintf("%s/v1/assets", baseURL)

	// Use a dedicated timeout context for the upstream upload
	ctx, cancel := context.WithTimeout(c.Request.Context(), assetUploadTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, &buf)
	if err != nil {
		assetErrorResponse(c, http.StatusInternalServerError, "Failed to create upstream request")
		return
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+ch.Key)

	client, err := uptokenHTTPClient(ch)
	if err != nil {
		assetErrorResponse(c, http.StatusInternalServerError, "Failed to create HTTP client")
		return
	}

	logger.LogInfo(c.Request.Context(), fmt.Sprintf("UploadAsset: proxying to %s, file=%s size=%d", url, header.Filename, header.Size))

	resp, err := client.Do(req)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("UploadAsset: upstream request failed: %s", err.Error()))
		assetErrorResponse(c, http.StatusBadGateway, "Failed to upload asset to upstream")
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		assetErrorResponse(c, http.StatusInternalServerError, "Failed to read upstream response")
		return
	}

	logger.LogInfo(c.Request.Context(), fmt.Sprintf("UploadAsset: upstream returned %d, body length=%d", resp.StatusCode, len(body)))

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		logger.LogError(c.Request.Context(), fmt.Sprintf("UploadAsset: upstream returned %d: %s", resp.StatusCode, string(body)))
		c.Data(resp.StatusCode, "application/json", body)
		return
	}

	// Parse upstream response
	var assetResp uptokenAssetResponse
	if err := common.Unmarshal(body, &assetResp); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("UploadAsset: failed to parse upstream response: %s", err.Error()))
		assetErrorResponse(c, http.StatusInternalServerError, "Failed to parse upstream response")
		return
	}

	// Save mapping locally
	userAsset := &model.UserAsset{
		UserId:      userId,
		TokenId:     tokenId,
		VirtualId:   assetResp.VirtualId,
		AssetUrl:    assetResp.AssetUrl,
		Url:         assetResp.Url,
		Filename:    assetResp.Filename,
		ContentType: assetResp.ContentType,
		SizeBytes:   assetResp.SizeBytes,
		Status:      assetResp.Status,
	}
	if err := model.InsertUserAsset(userAsset); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("UploadAsset: failed to save asset mapping: %s", err.Error()))
		// Still return the upstream response to user — asset was created upstream
	}

	c.Data(http.StatusOK, "application/json", body)
}

// inferImageContentType infers MIME type from filename extension.
func inferImageContentType(filename string) string {
	lower := strings.ToLower(filename)
	switch {
	case strings.HasSuffix(lower, ".jpg"), strings.HasSuffix(lower, ".jpeg"):
		return "image/jpeg"
	case strings.HasSuffix(lower, ".png"):
		return "image/png"
	case strings.HasSuffix(lower, ".webp"):
		return "image/webp"
	case strings.HasSuffix(lower, ".gif"):
		return "image/gif"
	case strings.HasSuffix(lower, ".heic"):
		return "image/heic"
	case strings.HasSuffix(lower, ".heif"):
		return "image/heif"
	default:
		return "application/octet-stream"
	}
}

// ListAssets returns assets belonging to the current user.
// For pending assets, it refreshes status from upstream.
func ListAssets(c *gin.Context) {
	userId := c.GetInt("id")

	assets, err := model.GetUserAssetsByUserId(userId)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("ListAssets: DB query failed: %s", err.Error()))
		assetErrorResponse(c, http.StatusInternalServerError, "Failed to list assets")
		return
	}

	ch, err := getUptokenChannel()
	if err != nil {
		// Can't refresh statuses, return local data as-is
		logger.LogError(c.Request.Context(), "ListAssets: "+err.Error())
		c.JSON(http.StatusOK, gin.H{
			"assets": assets,
			"total":  len(assets),
		})
		return
	}

	// Refresh status for pending/ready assets from upstream
	client, _ := uptokenHTTPClient(ch)
	baseURL := ch.GetBaseURL()
	if baseURL == "" {
		baseURL = constant.ChannelBaseURLs[constant.ChannelTypeUptoken]
	}

	for i := range assets {
		if assets[i].Status == "pending" || assets[i].Status == "ready" {
			refreshed, err := fetchUpstreamAsset(c, client, baseURL, ch.Key, assets[i].VirtualId)
			if err == nil && refreshed != nil {
				if refreshed.Status != assets[i].Status {
					_ = model.UpdateUserAssetFields(assets[i].VirtualId, map[string]interface{}{
						"status": refreshed.Status,
						"url":    refreshed.Url,
					})
					assets[i].Status = refreshed.Status
					assets[i].Url = refreshed.Url
				}
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"assets": assets,
		"total":  len(assets),
	})
}

// GetAsset returns a single asset's details, refreshed from upstream.
func GetAsset(c *gin.Context) {
	userId := c.GetInt("id")
	virtualId := c.Param("virtual_id")

	if virtualId == "" {
		assetErrorResponse(c, http.StatusBadRequest, "virtual_id is required")
		return
	}

	asset, err := model.GetUserAssetByUserIdAndVirtualId(userId, virtualId)
	if err != nil {
		assetErrorResponse(c, http.StatusNotFound, "Asset not found")
		return
	}

	ch, err := getUptokenChannel()
	if err != nil {
		// Return local data
		c.JSON(http.StatusOK, asset)
		return
	}

	client, _ := uptokenHTTPClient(ch)
	baseURL := ch.GetBaseURL()
	if baseURL == "" {
		baseURL = constant.ChannelBaseURLs[constant.ChannelTypeUptoken]
	}

	refreshed, err := fetchUpstreamAsset(c, client, baseURL, ch.Key, virtualId)
	if err != nil {
		// Return local data on upstream error
		logger.LogError(c.Request.Context(), fmt.Sprintf("GetAsset: upstream fetch failed: %s", err.Error()))
		c.JSON(http.StatusOK, asset)
		return
	}

	// Update local if status changed
	if refreshed.Status != asset.Status || refreshed.Url != asset.Url {
		_ = model.UpdateUserAssetFields(virtualId, map[string]interface{}{
			"status": refreshed.Status,
			"url":    refreshed.Url,
		})
	}

	// Return upstream data (more complete)
	c.JSON(http.StatusOK, refreshed)
}

// DeleteAsset removes an asset, both locally and from upstream.
func DeleteAsset(c *gin.Context) {
	userId := c.GetInt("id")
	virtualId := c.Param("virtual_id")

	if virtualId == "" {
		assetErrorResponse(c, http.StatusBadRequest, "virtual_id is required")
		return
	}

	_, err := model.GetUserAssetByUserIdAndVirtualId(userId, virtualId)
	if err != nil {
		assetErrorResponse(c, http.StatusNotFound, "Asset not found")
		return
	}

	ch, err := getUptokenChannel()
	if err != nil {
		logger.LogError(c.Request.Context(), "DeleteAsset: "+err.Error())
		assetErrorResponse(c, http.StatusServiceUnavailable, "Asset service unavailable")
		return
	}

	baseURL := ch.GetBaseURL()
	if baseURL == "" {
		baseURL = constant.ChannelBaseURLs[constant.ChannelTypeUptoken]
	}
	url := fmt.Sprintf("%s/v1/assets/%s", baseURL, virtualId)

	ctx, cancel := context.WithTimeout(c.Request.Context(), assetFetchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		assetErrorResponse(c, http.StatusInternalServerError, "Failed to create upstream request")
		return
	}
	req.Header.Set("Authorization", "Bearer "+ch.Key)

	client, err := uptokenHTTPClient(ch)
	if err != nil {
		assetErrorResponse(c, http.StatusInternalServerError, "Failed to create HTTP client")
		return
	}

	resp, err := client.Do(req)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("DeleteAsset: upstream request failed: %s", err.Error()))
		assetErrorResponse(c, http.StatusBadGateway, "Failed to delete asset from upstream")
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	// Delete from local DB regardless of upstream result (asset may already be gone upstream)
	if err := model.DeleteUserAssetByVirtualId(virtualId); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("DeleteAsset: failed to delete local record: %s", err.Error()))
	}

	if resp.StatusCode != http.StatusOK {
		logger.LogError(c.Request.Context(), fmt.Sprintf("DeleteAsset: upstream returned %d: %s", resp.StatusCode, string(body)))
	}

	c.JSON(http.StatusOK, gin.H{
		"deleted":    true,
		"virtual_id": virtualId,
	})
}

// fetchUpstreamAsset fetches a single asset's details from uptoken.
func fetchUpstreamAsset(c *gin.Context, client *http.Client, baseURL, apiKey, virtualId string) (*uptokenAssetResponse, error) {
	if client == nil {
		client = service.GetHttpClient()
	}
	url := fmt.Sprintf("%s/v1/assets/%s", baseURL, virtualId)

	ctx, cancel := context.WithTimeout(c.Request.Context(), assetFetchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")

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
		return nil, fmt.Errorf("upstream returned %d: %s", resp.StatusCode, string(body))
	}

	var assetResp uptokenAssetResponse
	if err := common.Unmarshal(body, &assetResp); err != nil {
		return nil, err
	}
	return &assetResp, nil
}
