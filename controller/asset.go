package controller

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
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

// assetListItem wraps UserAsset with a space_label for the list response.
// space_label groups assets by channel without exposing internal channel IDs.
type assetListItem struct {
	model.UserAsset
	SpaceLabel string `json:"space_label"`
}

// ---------- helpers ----------

// getAssetChannelById loads a channel by ID and verifies it supports the asset API
// (i.e., has seedance-2-0 models configured and is enabled in channel cache).
func getAssetChannelById(channelId int) (*model.Channel, error) {
	ch, err := model.CacheGetChannel(channelId)
	if err != nil {
		return nil, fmt.Errorf("channel %d not found: %w", channelId, err)
	}
	if !model.IsAssetSupportedChannel(channelId) {
		return nil, fmt.Errorf("channel %d does not have seedance-2-0 models configured", channelId)
	}
	return ch, nil
}

// getAssetSupportedChannels returns all enabled channels with seedance-2-0 models accessible by the token's group.
// Uses the same channel cache and priority/weight ordering as Distribute().
func getAssetSupportedChannels(group string) ([]*model.Channel, error) {
	return model.GetAssetSupportedChannelsByGroup(group)
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

// ---------- BytePlus mode: create asset request ----------

// AssetType values understood by BytePlus's CreateAsset API. The gateway accepts
// the lower-case form (`image|video|audio`) for ergonomics and normalises to
// the upstream's title-case ("Image"/"Video"/"Audio") before calling BytePlus.
const (
	AssetTypeImage = "Image"
	AssetTypeVideo = "Video"
	AssetTypeAudio = "Audio"
)

// Format whitelists per BytePlus virtual portrait library docs (2026-05).
// Source: docs/seedance2.0/Private virtual portrait library.md
//   - Image: jpeg / jpg / png / webp / bmp / tiff / gif / heic / heif (≤ 30 MB)
//   - Video: mp4 / mov
//   - Audio: mp3 / wav
var (
	imageContentTypes = map[string]struct{}{
		"image/jpeg": {}, "image/jpg": {}, "image/png": {}, "image/webp": {},
		"image/bmp": {}, "image/tiff": {}, "image/gif": {},
		"image/heic": {}, "image/heif": {},
	}
	imageExts = map[string]struct{}{
		".jpg": {}, ".jpeg": {}, ".png": {}, ".webp": {}, ".bmp": {},
		".tif": {}, ".tiff": {}, ".gif": {}, ".heic": {}, ".heif": {},
	}
	videoContentTypes = map[string]struct{}{
		"video/mp4": {}, "video/quicktime": {}, "video/x-quicktime": {}, "video/mov": {},
	}
	videoExts = map[string]struct{}{
		".mp4": {}, ".mov": {},
	}
	audioContentTypes = map[string]struct{}{
		"audio/mpeg": {}, "audio/mp3": {}, "audio/wav": {}, "audio/wave": {}, "audio/x-wav": {},
	}
	audioExts = map[string]struct{}{
		".mp3": {}, ".wav": {},
	}
)

type createAssetRequest struct {
	URL       string `json:"url" binding:"required"`
	GroupId   string `json:"group_id" binding:"required"`
	AssetType string `json:"asset_type"` // "Image" | "Video" | "Audio"; case-insensitive; defaults to "Image"
	Name      string `json:"name"`
}

// normalizeAssetType maps user-supplied / inferred asset type to one of the
// BytePlus title-case values. Empty string or unknown values fall back to "Image".
// Returns ("", false) if the value is non-empty but unknown — caller should reject.
func normalizeAssetType(s string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "image":
		return AssetTypeImage, true
	case "video":
		return AssetTypeVideo, true
	case "audio":
		return AssetTypeAudio, true
	default:
		return "", false
	}
}

// isPublicHTTPURL reports whether rawURL is an http(s) URL pointing at a
// public host. Used to decide whether req.URL is safe to persist as
// gravitex_url — non-public URLs (data:, file://, loopback, RFC1918 private
// nets, link-local, unspecified, localhost/*.local) are skipped to avoid
// storing unusable references that would later 404 or leak internal targets.
func isPublicHTTPURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	host := u.Hostname()
	if host == "" {
		return false
	}
	if ip := net.ParseIP(host); ip != nil {
		return !ip.IsLoopback() && !ip.IsPrivate() &&
			!ip.IsLinkLocalUnicast() && !ip.IsUnspecified()
	}
	lower := strings.ToLower(host)
	if lower == "localhost" || strings.HasSuffix(lower, ".local") {
		return false
	}
	return true
}

// validateAssetUrlForType performs lightweight format whitelisting based on the
// URL's path extension. We deliberately do NOT HEAD the remote URL — BytePlus
// itself does the authoritative content sniffing during async preprocessing,
// and adding an upstream HEAD here would slow the API materially.
//
// Returns nil on success or a 400-eligible error describing the mismatch.
func validateAssetUrlForType(rawURL, assetType string) error {
	lower := strings.ToLower(rawURL)
	// strip query string for extension matching
	if idx := strings.IndexByte(lower, '?'); idx >= 0 {
		lower = lower[:idx]
	}
	ext := ""
	if idx := strings.LastIndexByte(lower, '.'); idx >= 0 {
		ext = lower[idx:]
	}
	if ext == "" {
		// No extension we can match — let BytePlus arbitrate. Don't reject here.
		return nil
	}
	switch assetType {
	case AssetTypeImage:
		if _, ok := imageExts[ext]; !ok {
			return fmt.Errorf("file extension %q not supported for image asset (allowed: jpg/jpeg/png/webp/bmp/tiff/gif/heic/heif)", ext)
		}
	case AssetTypeVideo:
		if _, ok := videoExts[ext]; !ok {
			return fmt.Errorf("file extension %q not supported for video asset (allowed: mp4/mov)", ext)
		}
	case AssetTypeAudio:
		if _, ok := audioExts[ext]; !ok {
			return fmt.Errorf("file extension %q not supported for audio asset (allowed: mp3/wav)", ext)
		}
	}
	return nil
}

// inferAssetTypeFromContentType maps a MIME type (e.g. from multipart upload or
// a HEAD request) to the BytePlus asset type. Returns empty string if not a
// known media type.
func inferAssetTypeFromContentType(ct string) string {
	ct = strings.ToLower(strings.TrimSpace(ct))
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		ct = strings.TrimSpace(ct[:i])
	}
	if _, ok := imageContentTypes[ct]; ok {
		return AssetTypeImage
	}
	if _, ok := videoContentTypes[ct]; ok {
		return AssetTypeVideo
	}
	if _, ok := audioContentTypes[ct]; ok {
		return AssetTypeAudio
	}
	return ""
}

// CreateAsset handles asset creation in dual mode:
//   - Uptoken channels: multipart file upload (Content-Type: multipart/form-data)
//   - BytePlus channels: JSON body with URL + group_id (Content-Type: application/json)
func CreateAsset(c *gin.Context) {
	ct := c.ContentType()
	if strings.HasPrefix(ct, "multipart/form-data") {
		uploadAssetUptoken(c)
	} else {
		createAssetByteplus(c)
	}
}

// createAssetByteplus creates an asset via BytePlus SDK (URL-based, no file upload).
func createAssetByteplus(c *gin.Context) {
	userId := c.GetInt("id")
	tokenId := c.GetInt("token_id")

	var req createAssetRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		assetErrorResponse(c, http.StatusBadRequest, "Invalid request: "+err.Error())
		return
	}

	// Verify group ownership
	assetGroup, err := model.GetUserAssetGroupByUserIdAndGroupId(userId, req.GroupId)
	if err != nil {
		assetErrorResponse(c, http.StatusBadRequest, "Asset group not found or access denied")
		return
	}

	ch, err := getAssetChannelById(assetGroup.ChannelId)
	if err != nil {
		assetErrorResponse(c, http.StatusServiceUnavailable, "Asset channel unavailable")
		return
	}

	if !isByteplusAssetChannel(ch) {
		assetErrorResponse(c, http.StatusBadRequest, "Channel does not have BytePlus asset configuration")
		return
	}

	cfg := getByteplusAssetConfig(ch)

	assetType, ok := normalizeAssetType(req.AssetType)
	if !ok {
		assetErrorResponse(c, http.StatusBadRequest, "Invalid asset_type, expected image|video|audio")
		return
	}

	if err := validateAssetUrlForType(req.URL, assetType); err != nil {
		assetErrorResponse(c, http.StatusBadRequest, err.Error())
		return
	}

	logger.LogInfo(c.Request.Context(), fmt.Sprintf("CreateAsset(byteplus): channel=%d group=%s url=%s type=%s", ch.Id, req.GroupId, req.URL, assetType))

	assetId, err := service.ByteplusCreateAsset(cfg, req.GroupId, req.URL, assetType, req.Name)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("CreateAsset(byteplus): %s", err.Error()))
		username, _ := model.GetUsernameById(userId, false)
		errLog := &model.Log{
			UserId:    userId,
			Username:  username,
			CreatedAt: common.GetTimestamp(),
			Type:      model.LogTypeError,
			Content:   fmt.Sprintf("Failed to upload %s asset: %s", assetType, err.Error()),
			ChannelId: ch.Id,
			ModelName: "BytePlusAsset",
			TokenName: c.GetString("token_name"),
			TokenId:   tokenId,
			Group:     common.GetContextKeyString(c, constant.ContextKeyUsingGroup),
			RequestId: c.GetString(common.RequestIdKey),
		}
		if logErr := model.CreateLog(errLog); logErr != nil {
			logger.LogError(c.Request.Context(), fmt.Sprintf("CreateAsset(byteplus): failed to record error log: %s", logErr.Error()))
		}
		assetErrorResponse(c, http.StatusBadGateway, "Failed to create asset on upstream: "+err.Error())
		return
	}

	//tokenKey := c.Request.Header.Get("Authorization")
	//gravitexUrl, err := service.UploadByImageURL(req.URL, tokenKey)
	//if err != nil {
	//	logger.LogError(c.Request.Context(), fmt.Sprintf("CreateAsset(byteplus): upload by image URL failed: %s", err.Error()))
	//	// Don't fail the whole request if the Gravitex upload fails — the asset is still created on BytePlus and can be retried later.
	//	assetErrorResponse(c, http.StatusAccepted, "Asset created but failed to upload to Gravitex: "+err.Error())
	//}
	gravitexUrl := ""
	if isPublicHTTPURL(req.URL) {
		gravitexUrl = req.URL
	} else {
		logger.LogInfo(c.Request.Context(), fmt.Sprintf("CreateAsset(byteplus): skip gravitex_url, non-public url=%s", req.URL))
	}
	// Save locally
	userAsset := &model.UserAsset{
		UserId:      userId,
		TokenId:     tokenId,
		ChannelId:   ch.Id,
		GroupId:     req.GroupId,
		VirtualId:   assetId,
		AssetUrl:    "asset://" + assetId,
		Filename:    req.Name,
		AssetType:   assetType,
		Status:      "pending", // BytePlus "Processing" → internal "pending"
		GravitexUrl: gravitexUrl,
	}
	if err := model.InsertUserAsset(userAsset); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("CreateAsset(byteplus): save failed: %s", err.Error()))
	}

	c.JSON(http.StatusOK, gin.H{
		"virtual_id": assetId,
		"asset_url":  "asset://" + assetId,
		"group_id":   req.GroupId,
		"asset_type": assetType,
		"status":     "pending",
	})
}

// uploadAssetUptoken is the legacy uptoken multipart upload flow.
func uploadAssetUptoken(c *gin.Context) {
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
		// BytePlus channels don't support multipart file upload — must use JSON body with URL
		if isByteplusAssetChannel(ch) {
			assetErrorResponse(c, http.StatusBadRequest, "This channel requires JSON body with image URL, not file upload. Use POST /v1/assets with Content-Type: application/json")
			return
		}
	} else {
		channels, err := getAssetSupportedChannels(group)
		if err != nil || len(channels) == 0 {
			logger.LogError(c.Request.Context(), fmt.Sprintf("UploadAsset: no asset channel available for group=%s: %v", group, err))
			assetErrorResponse(c, http.StatusServiceUnavailable, "No asset channel available for this token")
			return
		}
		// Filter out BytePlus channels — they don't support multipart file upload (REST proxy)
		var uptokenChannels []*model.Channel
		for _, c := range channels {
			if !isByteplusAssetChannel(c) {
				uptokenChannels = append(uptokenChannels, c)
			}
		}
		if len(uptokenChannels) == 0 {
			assetErrorResponse(c, http.StatusBadRequest, "No file-upload channel available. Use JSON body with image URL for BytePlus channels")
			return
		}
		ch = uptokenChannels[0]
	}

	// Read uploaded file
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("UploadAsset: FormFile error: %s", err.Error()))
		assetErrorResponse(c, http.StatusBadRequest, "Missing file field")
		return
	}
	defer file.Close()

	ct := header.Header.Get("Content-Type")
	if ct == "" || ct == "application/octet-stream" {
		ct = inferImageContentType(header.Filename)
	}
	if !isAllowedImageType(ct) {
		assetErrorResponse(c, http.StatusBadRequest, fmt.Sprintf("Unsupported file type: %s", ct))
		return
	}

	if header.Size > 30*1024*1024 {
		assetErrorResponse(c, http.StatusBadRequest, "File too large, max 30MB")
		return
	}

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

	var assetResp uptokenAssetResponse
	if err := common.Unmarshal(body, &assetResp); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("UploadAsset: failed to parse upstream response: %s", err.Error()))
		assetErrorResponse(c, http.StatusInternalServerError, "Failed to parse upstream response")
		return
	}

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

// ListAssets returns assets belonging to the current user across all BytePlus-enabled channels
// accessible by the token's group. For pending assets, it refreshes status from upstream.
// Note: only BytePlus channels are listed; uptoken assets are excluded from the asset library.
//
// Supports optional ?group_type=aigc|liveness_face|all to filter by the owning group's type.
// Defaults to no group_type filter when only ?group_id is provided (the group itself already
// disambiguates the type). Defaults to no filter at all when neither is provided.
func ListAssets(c *gin.Context) {
	userId := c.GetInt("id")
	group := common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
	groupIdFilter := c.Query("group_id") // optional: filter by BytePlus asset group

	groupType, ok := normalizeAssetGroupType(c.Query("group_type"))
	if !ok {
		assetErrorResponse(c, http.StatusBadRequest, "Invalid group_type")
		return
	}

	// Only list assets from BytePlus-enabled channels (skip uptoken channels)
	channels, err := getByteplusEnabledChannels(group)
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
		channelMap[channels[i].Id] = channels[i]
	}

	var assets []model.UserAsset
	var err2 error
	switch {
	case groupIdFilter != "":
		assets, err2 = model.GetUserAssetsByGroupId(userId, groupIdFilter)
	case groupType != "" && groupType != "all":
		assets, err2 = model.GetUserAssetsByUserIdChannelIdsAndGroupType(userId, channelIds, groupType)
	default:
		assets, err2 = model.GetUserAssetsByUserIdAndChannelIds(userId, channelIds)
	}
	if err2 != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("ListAssets: DB query failed: %s", err2.Error()))
		assetErrorResponse(c, http.StatusInternalServerError, "Failed to list assets")
		return
	}

	// Refresh status / url / asset_type for pending/ready or type-less assets from upstream.
	// Type backfill covers rows persisted before the asset_type column existed (default "Image").
	for i := range assets {
		needRefresh := assets[i].Status == "pending" || assets[i].Status == "ready" || assets[i].AssetType == ""
		if !needRefresh {
			continue
		}
		ch, ok := channelMap[assets[i].ChannelId]
		if !ok {
			continue
		}
		cfg := getByteplusAssetConfig(ch)
		info, err := service.ByteplusGetAsset(cfg, assets[i].VirtualId)
		if err != nil || info == nil {
			continue
		}
		updates := map[string]interface{}{}
		newStatus := service.ByteplusStatusToInternal(info.Status)
		if newStatus != assets[i].Status {
			updates["status"] = newStatus
			assets[i].Status = newStatus
		}
		if info.URL != "" && info.URL != assets[i].Url {
			updates["url"] = info.URL
			assets[i].Url = info.URL
		}
		if info.AssetType != "" && info.AssetType != assets[i].AssetType {
			updates["asset_type"] = info.AssetType
			assets[i].AssetType = info.AssetType
		}
		if newStatus == "failed" {
			errMsg := ""
			if len(info.Error) > 0 && string(info.Error) != "null" {
				errMsg = string(info.Error)
			}
			if errMsg != "" && errMsg != assets[i].ErrorMsg {
				updates["error_msg"] = errMsg
				assets[i].ErrorMsg = errMsg
			}
		}
		if len(updates) > 0 {
			_ = model.UpdateUserAssetFields(assets[i].VirtualId, updates)
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

	if isByteplusAssetChannel(ch) {
		// BytePlus path: use SDK
		cfg := getByteplusAssetConfig(ch)
		info, err := service.ByteplusGetAsset(cfg, virtualId)
		if err != nil {
			logger.LogError(c.Request.Context(), fmt.Sprintf("GetAsset(byteplus): %s", err.Error()))
			c.JSON(http.StatusOK, asset)
			return
		}
		newStatus := service.ByteplusStatusToInternal(info.Status)
		updates := map[string]interface{}{}
		if newStatus != asset.Status {
			updates["status"] = newStatus
			asset.Status = newStatus
		}
		if info.URL != asset.Url {
			updates["url"] = info.URL
			asset.Url = info.URL
		}
		if newStatus == "failed" {
			errMsg := ""
			if len(info.Error) > 0 && string(info.Error) != "null" {
				errMsg = string(info.Error)
			}
			if errMsg != "" && errMsg != asset.ErrorMsg {
				updates["error_msg"] = errMsg
				asset.ErrorMsg = errMsg
			}
		}
		if len(updates) > 0 {
			_ = model.UpdateUserAssetFields(virtualId, updates)
		}
		// Return merged response
		c.JSON(http.StatusOK, gin.H{
			"virtual_id":      asset.VirtualId,
			"asset_url":       asset.AssetUrl,
			"url":             info.URL,
			"filename":        asset.Filename,
			"status":          newStatus,
			"error_msg":       asset.ErrorMsg,
			"group_id":        asset.GroupId,
			"byteplus_status": info.Status,
			"asset_type":      info.AssetType,
			"created_at":      asset.CreatedAt,
		})
	} else {
		// Uptoken path: use REST API
		client, _ := assetHTTPClient(ch)
		baseURL := getAssetChannelBaseURL(ch)

		refreshed, err := fetchUpstreamAsset(c, client, baseURL, ch.Key, virtualId)
		if err != nil {
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

	if isByteplusAssetChannel(ch) {
		// BytePlus path: use SDK to delete upstream
		cfg := getByteplusAssetConfig(ch)
		if err := service.ByteplusDeleteAsset(cfg, virtualId); err != nil {
			logger.LogError(c.Request.Context(), fmt.Sprintf("DeleteAsset(byteplus): %s", err.Error()))
			// Continue to delete locally even if upstream fails
		}
	} else {
		// Uptoken path: use REST API to delete upstream
		baseURL := getAssetChannelBaseURL(ch)
		delURL := fmt.Sprintf("%s/v1/assets/%s", baseURL, virtualId)

		ctx, cancel := context.WithTimeout(c.Request.Context(), assetFetchTimeout)
		defer cancel()

		req, reqErr := http.NewRequestWithContext(ctx, http.MethodDelete, delURL, nil)
		if reqErr == nil {
			req.Header.Set("Authorization", "Bearer "+ch.Key)
			client, clientErr := assetHTTPClient(ch)
			if clientErr == nil {
				resp, doErr := client.Do(req)
				if doErr != nil {
					logger.LogError(c.Request.Context(), fmt.Sprintf("DeleteAsset: upstream request failed: %s", doErr.Error()))
				} else {
					body, _ := io.ReadAll(resp.Body)
					resp.Body.Close()
					if resp.StatusCode != http.StatusOK {
						logger.LogError(c.Request.Context(), fmt.Sprintf("DeleteAsset: upstream returned %d: %s", resp.StatusCode, string(body)))
					}
				}
			} else {
				logger.LogError(c.Request.Context(), fmt.Sprintf("DeleteAsset: failed to create HTTP client: %s", clientErr.Error()))
			}
		} else {
			logger.LogError(c.Request.Context(), fmt.Sprintf("DeleteAsset: failed to create upstream request: %s", reqErr.Error()))
		}
	}

	// Delete from local DB regardless of upstream result (asset may already be gone upstream)
	if err := model.DeleteUserAssetByVirtualId(virtualId); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("DeleteAsset: failed to delete local record: %s", err.Error()))
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
