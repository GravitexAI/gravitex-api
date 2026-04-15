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

// assetSupportedChannelTypes lists channel types that support the asset API (/v1/assets).
var assetSupportedChannelTypes = []int{
	constant.ChannelTypeUptoken,    // 59
	constant.ChannelTypeVolcEngine, // 45
	constant.ChannelTypeDoubaoVideo, // 54
}

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

// assetListItem wraps UserAsset with a space_label for the list response.
// space_label groups assets by channel without exposing internal channel IDs.
type assetListItem struct {
	model.UserAsset
	SpaceLabel string `json:"space_label"`
}

// ---------- helpers ----------

// getAssetChannelById loads a channel by ID and verifies it is an asset-supported type.
func getAssetChannelById(channelId int) (*model.Channel, error) {
	ch, err := model.GetChannelById(channelId, true)
	if err != nil {
		return nil, fmt.Errorf("channel %d not found: %w", channelId, err)
	}
	for _, t := range assetSupportedChannelTypes {
		if ch.Type == t {
			return ch, nil
		}
	}
	return nil, fmt.Errorf("channel %d (type=%d) does not support asset API", channelId, ch.Type)
}

// getAssetSupportedChannels returns all enabled asset-supporting channels accessible by the token's group.
func getAssetSupportedChannels(group string) ([]model.Channel, error) {
	return model.GetAssetSupportedChannelsByGroup(group, assetSupportedChannelTypes)
}

// getAssetChannelBaseURL returns the base URL for the channel, falling back to the default for its type.
func getAssetChannelBaseURL(ch *model.Channel) string {
	baseURL := ch.GetBaseURL()
	if baseURL == "" {
		baseURL = constant.ChannelBaseURLs[ch.Type]
	}
	return baseURL
}

func assetHTTPClient(ch *model.Channel) (*http.Client, error) {
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

// UploadAsset proxies a multipart file upload to the upstream POST /v1/assets
// and records the user-asset mapping locally.
// The channel is auto-selected: if channel_id is provided, use it; otherwise pick the first
// asset-supporting channel accessible by the token's group.
func UploadAsset(c *gin.Context) {
	userId := c.GetInt("id")
	tokenId := c.GetInt("token_id")
	group := common.GetContextKeyString(c, constant.ContextKeyUsingGroup)

	// Determine which channel to use
	var ch *model.Channel
	var err error

	channelIdStr := c.Query("channel_id")
	if channelIdStr == "" {
		channelIdStr = c.PostForm("channel_id")
	}

	if channelIdStr != "" {
		// Explicit channel_id provided
		var channelId int
		if _, err := fmt.Sscanf(channelIdStr, "%d", &channelId); err != nil {
			assetErrorResponse(c, http.StatusBadRequest, "Invalid channel_id")
			return
		}
		ch, err = getAssetChannelById(channelId)
		if err != nil {
			logger.LogError(c.Request.Context(), "UploadAsset: "+err.Error())
			assetErrorResponse(c, http.StatusBadRequest, "Invalid or unsupported channel")
			return
		}
	} else {
		// Auto-select first available asset channel for this token's group
		channels, err := getAssetSupportedChannels(group)
		if err != nil || len(channels) == 0 {
			logger.LogError(c.Request.Context(), fmt.Sprintf("UploadAsset: no asset channel available for group=%s: %v", group, err))
			assetErrorResponse(c, http.StatusServiceUnavailable, "No asset channel available for this token")
			return
		}
		ch = &channels[0]
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

	baseURL := getAssetChannelBaseURL(ch)
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

	client, err := assetHTTPClient(ch)
	if err != nil {
		assetErrorResponse(c, http.StatusInternalServerError, "Failed to create HTTP client")
		return
	}

	logger.LogInfo(c.Request.Context(), fmt.Sprintf("UploadAsset: proxying to %s (channel=%d, type=%d), file=%s size=%d", url, ch.Id, ch.Type, header.Filename, header.Size))

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

	// Save mapping locally with channel_id
	userAsset := &model.UserAsset{
		UserId:      userId,
		TokenId:     tokenId,
		ChannelId:   ch.Id,
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

// ListAssets returns assets belonging to the current user across all asset-supporting channels
// accessible by the token's group. For pending assets, it refreshes status from upstream.
func ListAssets(c *gin.Context) {
	userId := c.GetInt("id")
	group := common.GetContextKeyString(c, constant.ContextKeyUsingGroup)

	// Find all asset-supporting channels accessible by this token
	channels, err := getAssetSupportedChannels(group)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("ListAssets: failed to get asset channels: %s", err.Error()))
		assetErrorResponse(c, http.StatusInternalServerError, "Failed to list assets")
		return
	}

	if len(channels) == 0 {
		// No channels available — return empty list
		c.JSON(http.StatusOK, gin.H{
			"assets": []model.UserAsset{},
			"total":  0,
		})
		return
	}

	// Build channel ID list and channel lookup map
	channelIds := make([]int, len(channels))
	channelMap := make(map[int]*model.Channel, len(channels))
	for i := range channels {
		channelIds[i] = channels[i].Id
		channelMap[channels[i].Id] = &channels[i]
	}

	assets, err := model.GetUserAssetsByUserIdAndChannelIds(userId, channelIds)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("ListAssets: DB query failed: %s", err.Error()))
		assetErrorResponse(c, http.StatusInternalServerError, "Failed to list assets")
		return
	}

	// Refresh status for pending/ready assets from upstream (per channel)
	for i := range assets {
		if assets[i].Status == "pending" || assets[i].Status == "ready" {
			ch, ok := channelMap[assets[i].ChannelId]
			if !ok {
				continue
			}
			client, _ := assetHTTPClient(ch)
			baseURL := getAssetChannelBaseURL(ch)
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

	// Assign space_label (A, B, C...) to each unique channel_id, ordered by channel id.
	// This abstracts away the internal channel concept into user-friendly "spaces".
	spaceLabelMap := make(map[int]string, len(channels))
	for i, ch := range channels {
		if i < 26 {
			spaceLabelMap[ch.Id] = string(rune('A' + i))
		} else {
			spaceLabelMap[ch.Id] = fmt.Sprintf("Z%d", i-25)
		}
	}

	items := make([]assetListItem, len(assets))
	for i := range assets {
		items[i] = assetListItem{
			UserAsset:  assets[i],
			SpaceLabel: spaceLabelMap[assets[i].ChannelId],
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"assets": items,
		"total":  len(items),
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

	ch, err := getAssetChannelById(asset.ChannelId)
	if err != nil {
		// Return local data if channel is unavailable
		logger.LogError(c.Request.Context(), fmt.Sprintf("GetAsset: channel lookup failed: %s", err.Error()))
		c.JSON(http.StatusOK, asset)
		return
	}

	client, _ := assetHTTPClient(ch)
	baseURL := getAssetChannelBaseURL(ch)

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

	asset, err := model.GetUserAssetByUserIdAndVirtualId(userId, virtualId)
	if err != nil {
		assetErrorResponse(c, http.StatusNotFound, "Asset not found")
		return
	}

	ch, err := getAssetChannelById(asset.ChannelId)
	if err != nil {
		logger.LogError(c.Request.Context(), "DeleteAsset: "+err.Error())
		assetErrorResponse(c, http.StatusServiceUnavailable, "Asset service unavailable")
		return
	}

	baseURL := getAssetChannelBaseURL(ch)
	url := fmt.Sprintf("%s/v1/assets/%s", baseURL, virtualId)

	ctx, cancel := context.WithTimeout(c.Request.Context(), assetFetchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		assetErrorResponse(c, http.StatusInternalServerError, "Failed to create upstream request")
		return
	}
	req.Header.Set("Authorization", "Bearer "+ch.Key)

	client, err := assetHTTPClient(ch)
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

// fetchUpstreamAsset fetches a single asset's details from upstream.
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
