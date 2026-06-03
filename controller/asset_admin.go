package controller

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

// ---------- helpers ----------

func adminAssetOK(c *gin.Context, data interface{}) {
	c.JSON(http.StatusOK, gin.H{"success": true, "data": data})
}

func adminAssetErr(c *gin.Context, status int, msg string) {
	c.JSON(status, gin.H{"success": false, "message": msg})
}

// requireByteplusChannel loads and validates a BytePlus-capable channel by ID
// from the query/body. Returns nil and writes the error response on failure.
func requireByteplusChannel(c *gin.Context, channelId int) (*model.Channel, service.ByteplusAssetConfig) {
	ch, err := model.CacheGetChannel(channelId)
	if err != nil || ch == nil {
		adminAssetErr(c, http.StatusBadRequest, fmt.Sprintf("channel %d not found", channelId))
		return nil, service.ByteplusAssetConfig{}
	}
	if !isByteplusAssetChannel(ch) {
		adminAssetErr(c, http.StatusBadRequest, fmt.Sprintf("channel %d has no BytePlus asset AK/SK configured", channelId))
		return nil, service.ByteplusAssetConfig{}
	}
	return ch, getByteplusAssetConfig(ch)
}

func parseAdminChannelId(c *gin.Context) (int, bool) {
	raw := c.Query("channel_id")
	if raw == "" {
		adminAssetErr(c, http.StatusBadRequest, "channel_id is required")
		return 0, false
	}
	id, err := strconv.Atoi(raw)
	if err != nil || id <= 0 {
		adminAssetErr(c, http.StatusBadRequest, "invalid channel_id")
		return 0, false
	}
	return id, true
}

func parseAdminPage(c *gin.Context) (int, int) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	return page, pageSize
}

// ============================================================
// A. 本地表管理 — /api/asset-admin/groups + /api/asset-admin/assets
// ============================================================

// AdminListLocalGroups lists t_user_asset_groups by channel, with pagination.
// GET /api/asset-admin/groups?channel_id=&group_type=&page=&page_size=
func AdminListLocalGroups(c *gin.Context) {
	channelId, ok := parseAdminChannelId(c)
	if !ok {
		return
	}
	page, pageSize := parseAdminPage(c)
	groupType := c.Query("group_type")

	groups, total, err := model.GetUserAssetGroupsByChannelIdPaged(model.AdminGroupQuery{
		ChannelId: channelId,
		GroupType: groupType,
		Page:      page,
		PageSize:  pageSize,
	})
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("AdminListLocalGroups: %s", err.Error()))
		adminAssetErr(c, http.StatusInternalServerError, "query failed: "+err.Error())
		return
	}
	adminAssetOK(c, gin.H{"items": groups, "total": total, "page": page, "page_size": pageSize})
}

// AdminUpdateLocalGroup updates name/description of a local group by group_id (route param).
// PUT /api/asset-admin/groups/:group_id
func AdminUpdateLocalGroup(c *gin.Context) {
	groupId := c.Param("group_id")
	if groupId == "" {
		adminAssetErr(c, http.StatusBadRequest, "group_id is required")
		return
	}
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		adminAssetErr(c, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}
	if err := model.UpdateUserAssetGroupName(groupId, req.Name, req.Description); err != nil {
		adminAssetErr(c, http.StatusInternalServerError, "update failed: "+err.Error())
		return
	}
	adminAssetOK(c, gin.H{"updated": true, "group_id": groupId})
}

// AdminDeleteLocalGroup deletes a local group and its assets (cascade).
// DELETE /api/asset-admin/groups/:group_id
func AdminDeleteLocalGroup(c *gin.Context) {
	groupId := c.Param("group_id")
	if groupId == "" {
		adminAssetErr(c, http.StatusBadRequest, "group_id is required")
		return
	}
	if err := model.DeleteUserAssetGroupByGroupId(groupId); err != nil {
		adminAssetErr(c, http.StatusInternalServerError, "delete failed: "+err.Error())
		return
	}
	adminAssetOK(c, gin.H{"deleted": true, "group_id": groupId})
}

// AdminListLocalAssets lists t_user_assets by channel with optional group/status filter.
// GET /api/asset-admin/assets?channel_id=&group_id=&status=&page=&page_size=
func AdminListLocalAssets(c *gin.Context) {
	channelId, ok := parseAdminChannelId(c)
	if !ok {
		return
	}
	page, pageSize := parseAdminPage(c)

	assets, total, err := model.GetUserAssetsByChannelIdPaged(model.AdminAssetQuery{
		ChannelId: channelId,
		GroupId:   c.Query("group_id"),
		Status:    c.Query("status"),
		Page:      page,
		PageSize:  pageSize,
	})
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("AdminListLocalAssets: %s", err.Error()))
		adminAssetErr(c, http.StatusInternalServerError, "query failed: "+err.Error())
		return
	}
	adminAssetOK(c, gin.H{"items": assets, "total": total, "page": page, "page_size": pageSize})
}

// AdminUpdateLocalAsset updates the filename (display name) of a local asset.
// PUT /api/asset-admin/assets/:virtual_id
func AdminUpdateLocalAsset(c *gin.Context) {
	virtualId := c.Param("virtual_id")
	if virtualId == "" {
		adminAssetErr(c, http.StatusBadRequest, "virtual_id is required")
		return
	}
	var req struct {
		Filename string `json:"filename" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		adminAssetErr(c, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}
	if err := model.UpdateUserAssetFilename(virtualId, req.Filename); err != nil {
		adminAssetErr(c, http.StatusInternalServerError, "update failed: "+err.Error())
		return
	}
	adminAssetOK(c, gin.H{"updated": true, "virtual_id": virtualId})
}

// AdminDeleteLocalAsset deletes a local asset record (does NOT call BytePlus upstream).
// DELETE /api/asset-admin/assets/:virtual_id
func AdminDeleteLocalAsset(c *gin.Context) {
	virtualId := c.Param("virtual_id")
	if virtualId == "" {
		adminAssetErr(c, http.StatusBadRequest, "virtual_id is required")
		return
	}
	if err := model.DeleteUserAssetByVirtualId(virtualId); err != nil {
		adminAssetErr(c, http.StatusInternalServerError, "delete failed: "+err.Error())
		return
	}
	adminAssetOK(c, gin.H{"deleted": true, "virtual_id": virtualId})
}

// ============================================================
// B. 火山素材库直连 — /api/asset-admin/byteplus/*
// ============================================================

// AdminByteplusListGroups queries BytePlus upstream for asset groups.
// GET /api/asset-admin/byteplus/groups?channel_id=&group_type=&page=&page_size=
func AdminByteplusListGroups(c *gin.Context) {
	channelId, ok := parseAdminChannelId(c)
	if !ok {
		return
	}
	ch, cfg := requireByteplusChannel(c, channelId)
	if ch == nil {
		return
	}
	page, pageSize := parseAdminPage(c)
	groupType := c.DefaultQuery("group_type", "AIGC")

	items, total, err := service.ByteplusListAssetGroups(cfg, groupType, nil, page, pageSize)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("AdminByteplusListGroups channel=%d: %s", channelId, err.Error()))
		adminAssetErr(c, http.StatusBadGateway, "BytePlus error: "+err.Error())
		return
	}
	adminAssetOK(c, gin.H{"items": items, "total": total, "page": page, "page_size": pageSize})
}

// AdminByteplusCreateGroup creates an asset group on BytePlus.
// POST /api/asset-admin/byteplus/groups
func AdminByteplusCreateGroup(c *gin.Context) {
	var req struct {
		ChannelId   int    `json:"channel_id" binding:"required"`
		Name        string `json:"name" binding:"required"`
		Description string `json:"description"`
		GroupType   string `json:"group_type"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		adminAssetErr(c, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}
	ch, cfg := requireByteplusChannel(c, req.ChannelId)
	if ch == nil {
		return
	}
	groupType := req.GroupType
	if groupType == "" {
		groupType = service.ByteplusGroupTypeAIGC
	}
	groupId, err := service.ByteplusCreateAssetGroup(cfg, req.Name, req.Description, groupType)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("AdminByteplusCreateGroup channel=%d: %s", req.ChannelId, err.Error()))
		adminAssetErr(c, http.StatusBadGateway, "BytePlus error: "+err.Error())
		return
	}
	adminAssetOK(c, gin.H{"group_id": groupId, "name": req.Name})
}

// AdminByteplusUpdateGroup updates an asset group name/description on BytePlus.
// PUT /api/asset-admin/byteplus/groups/:group_id
func AdminByteplusUpdateGroup(c *gin.Context) {
	groupId := c.Param("group_id")
	if groupId == "" {
		adminAssetErr(c, http.StatusBadRequest, "group_id is required")
		return
	}
	var req struct {
		ChannelId   int    `json:"channel_id" binding:"required"`
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		adminAssetErr(c, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}
	ch, cfg := requireByteplusChannel(c, req.ChannelId)
	if ch == nil {
		return
	}
	if err := service.ByteplusUpdateAssetGroup(cfg, groupId, req.Name, req.Description); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("AdminByteplusUpdateGroup channel=%d group=%s: %s", req.ChannelId, groupId, err.Error()))
		adminAssetErr(c, http.StatusBadGateway, "BytePlus error: "+err.Error())
		return
	}
	adminAssetOK(c, gin.H{"updated": true, "group_id": groupId})
}

// AdminByteplusDeleteGroup deletes an asset group on BytePlus.
// DELETE /api/asset-admin/byteplus/groups/:group_id?channel_id=
func AdminByteplusDeleteGroup(c *gin.Context) {
	groupId := c.Param("group_id")
	if groupId == "" {
		adminAssetErr(c, http.StatusBadRequest, "group_id is required")
		return
	}
	channelId, ok := parseAdminChannelId(c)
	if !ok {
		return
	}
	ch, cfg := requireByteplusChannel(c, channelId)
	if ch == nil {
		return
	}
	if err := service.ByteplusDeleteAssetGroup(cfg, groupId); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("AdminByteplusDeleteGroup channel=%d group=%s: %s", channelId, groupId, err.Error()))
		adminAssetErr(c, http.StatusBadGateway, "BytePlus error: "+err.Error())
		return
	}
	adminAssetOK(c, gin.H{"deleted": true, "group_id": groupId})
}

// AdminByteplusListAssets queries BytePlus upstream for assets.
// GET /api/asset-admin/byteplus/assets?channel_id=&group_id=&statuses=&group_type=&page=&page_size=
func AdminByteplusListAssets(c *gin.Context) {
	channelId, ok := parseAdminChannelId(c)
	if !ok {
		return
	}
	ch, cfg := requireByteplusChannel(c, channelId)
	if ch == nil {
		return
	}
	page, pageSize := parseAdminPage(c)
	groupType := c.DefaultQuery("group_type", "AIGC")
	groupId := c.Query("group_id")
	var statuses []string
	if raw := c.Query("statuses"); raw != "" {
		for _, s := range strings.Split(raw, ",") {
			if t := strings.TrimSpace(s); t != "" {
				statuses = append(statuses, t)
			}
		}
	}

	items, total, err := service.ByteplusListAssets(cfg, groupType, groupId, statuses, page, pageSize)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("AdminByteplusListAssets channel=%d: %s", channelId, err.Error()))
		adminAssetErr(c, http.StatusBadGateway, "BytePlus error: "+err.Error())
		return
	}
	adminAssetOK(c, gin.H{"items": items, "total": total, "page": page, "page_size": pageSize})
}

// AdminByteplusGetAsset fetches a single asset from BytePlus (includes 12h signed URL).
// GET /api/asset-admin/byteplus/assets/:asset_id?channel_id=
func AdminByteplusGetAsset(c *gin.Context) {
	assetId := c.Param("asset_id")
	if assetId == "" {
		adminAssetErr(c, http.StatusBadRequest, "asset_id is required")
		return
	}
	channelId, ok := parseAdminChannelId(c)
	if !ok {
		return
	}
	ch, cfg := requireByteplusChannel(c, channelId)
	if ch == nil {
		return
	}
	info, err := service.ByteplusGetAsset(cfg, assetId)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("AdminByteplusGetAsset channel=%d asset=%s: %s", channelId, assetId, err.Error()))
		adminAssetErr(c, http.StatusBadGateway, "BytePlus error: "+err.Error())
		return
	}
	adminAssetOK(c, info)
}

// AdminByteplusCreateAsset creates an asset on BytePlus (supports Moderation.Skip).
// POST /api/asset-admin/byteplus/assets
func AdminByteplusCreateAsset(c *gin.Context) {
	var req struct {
		ChannelId          int    `json:"channel_id" binding:"required"`
		GroupId            string `json:"group_id" binding:"required"`
		URL                string `json:"url" binding:"required"`
		AssetType          string `json:"asset_type"`
		Name               string `json:"name"`
		ModerationStrategy string `json:"moderation_strategy"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		adminAssetErr(c, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}
	ch, cfg := requireByteplusChannel(c, req.ChannelId)
	if ch == nil {
		return
	}
	assetType := req.AssetType
	if assetType == "" {
		assetType = "Image"
	}
	assetId, err := service.ByteplusCreateAsset(
		context.Background(), cfg, req.GroupId, req.URL, assetType, req.Name, req.ModerationStrategy,
	)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("AdminByteplusCreateAsset channel=%d group=%s: %s", req.ChannelId, req.GroupId, err.Error()))
		adminAssetErr(c, http.StatusBadGateway, "BytePlus error: "+err.Error())
		return
	}
	adminAssetOK(c, gin.H{"asset_id": assetId, "status": "pending"})
}

// AdminByteplusUpdateAsset updates the name of an asset on BytePlus.
// PUT /api/asset-admin/byteplus/assets/:asset_id
func AdminByteplusUpdateAsset(c *gin.Context) {
	assetId := c.Param("asset_id")
	if assetId == "" {
		adminAssetErr(c, http.StatusBadRequest, "asset_id is required")
		return
	}
	var req struct {
		ChannelId int    `json:"channel_id" binding:"required"`
		Name      string `json:"name" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		adminAssetErr(c, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}
	ch, cfg := requireByteplusChannel(c, req.ChannelId)
	if ch == nil {
		return
	}
	if err := service.ByteplusUpdateAsset(cfg, assetId, req.Name); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("AdminByteplusUpdateAsset channel=%d asset=%s: %s", req.ChannelId, assetId, err.Error()))
		adminAssetErr(c, http.StatusBadGateway, "BytePlus error: "+err.Error())
		return
	}
	adminAssetOK(c, gin.H{"updated": true, "asset_id": assetId})
}

// AdminByteplusDeleteAsset deletes an asset on BytePlus.
// DELETE /api/asset-admin/byteplus/assets/:asset_id?channel_id=
func AdminByteplusDeleteAsset(c *gin.Context) {
	assetId := c.Param("asset_id")
	if assetId == "" {
		adminAssetErr(c, http.StatusBadRequest, "asset_id is required")
		return
	}
	channelId, ok := parseAdminChannelId(c)
	if !ok {
		return
	}
	ch, cfg := requireByteplusChannel(c, channelId)
	if ch == nil {
		return
	}
	if err := service.ByteplusDeleteAsset(cfg, assetId); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("AdminByteplusDeleteAsset channel=%d asset=%s: %s", channelId, assetId, err.Error()))
		adminAssetErr(c, http.StatusBadGateway, "BytePlus error: "+err.Error())
		return
	}
	adminAssetOK(c, gin.H{"deleted": true, "asset_id": assetId})
}
