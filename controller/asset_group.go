package controller

import (
	"fmt"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

// ---------- request / response types ----------

// MaxAssetGroupsPerUserPerChannel limits how many asset groups a single user
// can create on one upstream channel (one AK/SK account).
// BytePlus does not document an explicit limit, but upstream resources are shared
// across all gateway users routed to the same channel, so we cap it defensively.
const MaxAssetGroupsPerUserPerChannel = 5

type createAssetGroupRequest struct {
	Name        string `json:"name" binding:"required"`
	Description string `json:"description"`
	ChannelId   int    `json:"channel_id"` // optional, auto-select if 0
}

type assetGroupListItem struct {
	model.UserAssetGroup
	SpaceLabel string `json:"space_label"`
	AssetCount int64  `json:"asset_count"`
}

// ---------- helpers ----------

// getByteplusAssetConfig extracts BytePlus config from a channel's other settings.
func getByteplusAssetConfig(ch *model.Channel) service.ByteplusAssetConfig {
	s := ch.GetOtherSettings()
	return service.ByteplusAssetConfig{
		AK:          s.ByteplusAssetAK,
		SK:          s.ByteplusAssetSK,
		Region:      s.GetByteplusAssetRegion(),
		ProjectName: s.GetByteplusAssetProjectName(),
	}
}

// isByteplusAssetChannel checks if the channel has BytePlus AK/SK configured.
func isByteplusAssetChannel(ch *model.Channel) bool {
	return ch.GetOtherSettings().HasByteplusAssetConfig()
}

// getByteplusEnabledChannels returns asset-supporting channels that have BytePlus AK/SK configured.
func getByteplusEnabledChannels(group string) ([]*model.Channel, error) {
	channels, err := getAssetSupportedChannels(group)
	if err != nil {
		return nil, err
	}
	var result []*model.Channel
	for _, ch := range channels {
		if isByteplusAssetChannel(ch) {
			result = append(result, ch)
		}
	}
	return result, nil
}

// ---------- handlers ----------

// CreateAssetGroup creates a new asset group on BytePlus and records it locally.
func CreateAssetGroup(c *gin.Context) {
	userId := c.GetInt("id")
	group := common.GetContextKeyString(c, constant.ContextKeyUsingGroup)

	var req createAssetGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		assetErrorResponse(c, http.StatusBadRequest, "Invalid request: "+err.Error())
		return
	}

	// Select channel
	var ch *model.Channel
	var err error

	if req.ChannelId > 0 {
		ch, err = getAssetChannelById(req.ChannelId)
		if err != nil {
			assetErrorResponse(c, http.StatusBadRequest, "Invalid or unsupported channel")
			return
		}
		if !isByteplusAssetChannel(ch) {
			assetErrorResponse(c, http.StatusBadRequest, "Channel does not have BytePlus asset configuration")
			return
		}
	} else {
		channels, err := getByteplusEnabledChannels(group)
		if err != nil || len(channels) == 0 {
			logger.LogError(c.Request.Context(), fmt.Sprintf("CreateAssetGroup: no byteplus channel for group=%s: %v", group, err))
			assetErrorResponse(c, http.StatusServiceUnavailable, "No BytePlus asset channel available")
			return
		}
		ch = channels[0]
	}

	cfg := getByteplusAssetConfig(ch)

	// Check per-user group limit on this channel
	count, err := model.CountUserAssetGroupsByChannel(userId, ch.Id)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("CreateAssetGroup: count check failed: %s", err.Error()))
		assetErrorResponse(c, http.StatusInternalServerError, "Failed to check group limit")
		return
	}
	if count >= MaxAssetGroupsPerUserPerChannel {
		assetErrorResponse(c, http.StatusForbidden, fmt.Sprintf("Asset group limit reached (%d per channel)", MaxAssetGroupsPerUserPerChannel))
		return
	}

	// Call BytePlus API
	groupId, err := service.ByteplusCreateAssetGroup(cfg, req.Name, req.Description)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("CreateAssetGroup: byteplus error: %s", err.Error()))
		assetErrorResponse(c, http.StatusBadGateway, "Failed to create asset group on upstream: "+err.Error())
		return
	}

	// Save locally
	assetGroup := &model.UserAssetGroup{
		UserId:      userId,
		ChannelId:   ch.Id,
		GroupId:     groupId,
		Name:        req.Name,
		Description: req.Description,
		ProjectName: cfg.ProjectName,
	}
	if err := model.InsertUserAssetGroup(assetGroup); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("CreateAssetGroup: failed to save locally: %s", err.Error()))
		// Group was created upstream — still return success
	}

	c.JSON(http.StatusOK, gin.H{
		"group_id":    groupId,
		"name":        req.Name,
		"description": req.Description,
		"channel_id":  ch.Id,
	})
}

// ListAssetGroups lists the user's asset groups across all BytePlus-enabled channels.
func ListAssetGroups(c *gin.Context) {
	userId := c.GetInt("id")
	group := common.GetContextKeyString(c, constant.ContextKeyUsingGroup)

	channels, err := getByteplusEnabledChannels(group)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("ListAssetGroups: failed to get channels: %s", err.Error()))
		assetErrorResponse(c, http.StatusInternalServerError, "Failed to list asset groups")
		return
	}

	if len(channels) == 0 {
		c.JSON(http.StatusOK, gin.H{
			"groups":                []interface{}{},
			"total":                 0,
			"has_byteplus_channels": false,
		})
		return
	}

	channelIds := make([]int, len(channels))
	for i, ch := range channels {
		channelIds[i] = ch.Id
	}

	groups, err := model.GetUserAssetGroupsByUserIdAndChannelIds(userId, channelIds)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("ListAssetGroups: DB query failed: %s", err.Error()))
		assetErrorResponse(c, http.StatusInternalServerError, "Failed to list asset groups")
		return
	}

	// Build space label map (same logic as ListAssets)
	spaceLabelMap := make(map[int]string, len(channels))
	for i, ch := range channels {
		if i < 26 {
			spaceLabelMap[ch.Id] = string(rune('A' + i))
		} else {
			spaceLabelMap[ch.Id] = fmt.Sprintf("Z%d", i-25)
		}
	}

	items := make([]assetGroupListItem, len(groups))
	for i := range groups {
		count, _ := model.CountAssetsByGroupId(groups[i].GroupId)
		items[i] = assetGroupListItem{
			UserAssetGroup: groups[i],
			SpaceLabel:     spaceLabelMap[groups[i].ChannelId],
			AssetCount:     count,
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"groups":                items,
		"total":                 len(items),
		"has_byteplus_channels": true,
	})
}

// DeleteAssetGroup deletes an asset group from both BytePlus and local DB.
func DeleteAssetGroup(c *gin.Context) {
	userId := c.GetInt("id")
	groupId := c.Param("group_id")

	if groupId == "" {
		assetErrorResponse(c, http.StatusBadRequest, "group_id is required")
		return
	}

	// Verify ownership
	assetGroup, err := model.GetUserAssetGroupByUserIdAndGroupId(userId, groupId)
	if err != nil {
		assetErrorResponse(c, http.StatusNotFound, "Asset group not found")
		return
	}

	ch, err := getAssetChannelById(assetGroup.ChannelId)
	if err != nil {
		logger.LogError(c.Request.Context(), "DeleteAssetGroup: "+err.Error())
		assetErrorResponse(c, http.StatusServiceUnavailable, "Asset service unavailable")
		return
	}

	if !isByteplusAssetChannel(ch) {
		assetErrorResponse(c, http.StatusBadRequest, "Channel does not support BytePlus asset groups")
		return
	}

	cfg := getByteplusAssetConfig(ch)

	// Delete from BytePlus
	if err := service.ByteplusDeleteAssetGroup(cfg, groupId); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("DeleteAssetGroup: byteplus error: %s", err.Error()))
		// Continue to delete locally even if upstream fails
	}

	// Delete local group and its assets
	if err := model.DeleteUserAssetGroupByGroupId(groupId); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("DeleteAssetGroup: failed to delete group: %s", err.Error()))
	}

	c.JSON(http.StatusOK, gin.H{
		"deleted":  true,
		"group_id": groupId,
	})
}
