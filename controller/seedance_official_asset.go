package controller

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
)

// seedanceAssetRawAction is a package-level seam over service.ByteplusRawAction
// so tests can stub the upstream call without hitting the network. Production
// code never reassigns this.
var seedanceAssetRawAction = service.ByteplusRawAction

// seedanceOfficialAssetActions is the set of Action values this endpoint
// accepts — mirrors docs/byteplus/seedance-2.0-official-api-mirror-design.md §4.1.
var seedanceOfficialAssetActions = map[string]bool{
	"CreateAssetGroup": true, "CreateAsset": true,
	"ListAssetGroups": true, "ListAssets": true,
	"GetAsset": true, "GetAssetGroup": true,
	"UpdateAsset": true, "UpdateAssetGroup": true,
	"DeleteAsset": true, "DeleteAssetGroup": true,
	"CreateVisualValidateSession": true, "GetVisualValidateResult": true,
}

func seedanceAssetErrorEnvelope(action string, code, message string) gin.H {
	return gin.H{
		"ResponseMetadata": gin.H{
			"Action":  action,
			"Version": "2024-01-01",
			"Service": "ark",
			"Error":   gin.H{"Code": code, "Message": message},
		},
	}
}

// checkSeedanceGroupQuota enforces the per-user asset-group limit before an
// upstream call that would create a new group (CreateAssetGroup, or
// GetVisualValidateResult — the moment a liveness-verified group is
// materialized locally). Writes its own error response and returns false
// when the caller must abort; returns true when the call may proceed.
func checkSeedanceGroupQuota(c *gin.Context, action string, userId, channelId int, groupType string) bool {
	limit := system_setting.GetByteplusAssetGroupLimit()
	count, err := model.CountUserAssetGroupsByChannel(userId, channelId, groupType)
	if err != nil {
		c.JSON(http.StatusInternalServerError, seedanceAssetErrorEnvelope(action, "QuotaCheckFailed", "failed to check asset group quota"))
		return false
	}
	if int(count) >= limit {
		c.JSON(http.StatusForbidden, seedanceAssetErrorEnvelope(action, "QuotaExceeded", fmt.Sprintf("asset group quota exceeded: limit %d", limit)))
		return false
	}
	return true
}

// SeedanceOfficialAssetDispatch handles POST
// /api/v3/seedance?Action=X&Version=2024-01-01 — the BytePlus asset-library
// official-shape mirror. See
// docs/byteplus/seedance-2.0-official-api-mirror-design.md §4.
func SeedanceOfficialAssetDispatch(c *gin.Context) {
	action := c.Query("Action")
	if !seedanceOfficialAssetActions[action] {
		c.JSON(http.StatusBadRequest, seedanceAssetErrorEnvelope(action, "InvalidAction", "unsupported Action: "+action))
		return
	}

	var body map[string]interface{}
	if err := common.UnmarshalBodyReusable(c, &body); err != nil {
		c.JSON(http.StatusBadRequest, seedanceAssetErrorEnvelope(action, "InvalidParameter", "invalid request body"))
		return
	}
	if body == nil {
		body = map[string]interface{}{}
	}

	userId := c.GetInt("id")
	usingGroup := common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
	channels, err := getByteplusEnabledChannels(c, usingGroup)
	if err != nil || len(channels) == 0 {
		c.JSON(http.StatusServiceUnavailable, seedanceAssetErrorEnvelope(action, "NoChannel", "no BytePlus asset channel available"))
		return
	}
	ch := channels[0]
	cfg := getByteplusAssetConfig(ch)

	if action == "CreateAssetGroup" {
		// GroupType is always forced to aigc — liveness_face can only be
		// created via CreateVisualValidateSession/GetVisualValidateResult
		// (mirrored below) or the existing H5 flow. See design doc §4.2.
		if !checkSeedanceGroupQuota(c, action, userId, ch.Id, model.GroupTypeAIGC) {
			return
		}
		body["GroupType"] = service.ByteplusGroupTypeAIGC
	}
	if action == "CreateVisualValidateSession" {
		// Quota is checked here, not in GetVisualValidateResult: BytePlus creates
		// the liveness-face asset group upstream the moment H5 verification
		// succeeds, before we ever call GetVisualValidateResult. Gating on the
		// later call would let a user finish H5 verification against a full
		// quota and then get locked out of a GroupId we can never sync. See
		// design doc §4.2.
		if !checkSeedanceGroupQuota(c, action, userId, ch.Id, model.GroupTypeLivenessFace) {
			return
		}
		// CallbackURL is always forced to the platform's own callback page —
		// the mirror's GetVisualValidateResult is authenticated and doesn't
		// depend on anything carried through the redirect. See design doc §4.2.
		body["CallbackURL"] = strings.TrimRight(system_setting.ServerAddress, "/") + VisualValidateCallbackPath
	}
	if action == "GetVisualValidateResult" {
		// Second gate, not redundant: quota could have filled up between
		// CreateVisualValidateSession and the user finishing H5 verification.
		// Doesn't fully close the race (BytePlus already created the group
		// upstream by this point) but stops us from syncing past the limit.
		if !checkSeedanceGroupQuota(c, action, userId, ch.Id, model.GroupTypeLivenessFace) {
			return
		}
	}

	resp, err := seedanceAssetRawAction(cfg, action, body)
	if err != nil {
		c.JSON(http.StatusBadGateway, seedanceAssetErrorEnvelope(action, "UpstreamError", err.Error()))
		return
	}

	if syncErr := syncSeedanceAssetLocalState(userId, ch.Id, action, body, resp); syncErr != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("[SeedanceAssetMirror] local sync failed action=%s err=%v", action, syncErr))
	}

	c.JSON(http.StatusOK, resp)
}

// seedanceExtractStringField pulls a named string field out of a raw
// BytePlus response map, checking the top level first, then inside
// "Result" — mirroring the existing extractStringField's behavior in
// byteplus_asset.go, since different upstream Actions place fields at
// different nesting levels (and the two visual-validate Actions return a
// flat response with no "Result" envelope at all).
func seedanceExtractStringField(resp map[string]interface{}, field string) string {
	if v, ok := resp[field].(string); ok && v != "" {
		return v
	}
	if result, ok := resp["Result"].(map[string]interface{}); ok {
		if v, ok := result[field].(string); ok {
			return v
		}
	}
	return ""
}

// syncSeedanceAssetLocalState replicates the local bookkeeping that
// controller/asset.go and controller/asset_group.go perform after their own
// ByteplusXxx calls, so assets/groups created via this mirror endpoint stay
// visible to model.CheckUserOwnsAssets and the existing /v1/assets,
// /v1/asset-groups endpoints. This is mandatory, not optional — see design
// doc §4.2.
func syncSeedanceAssetLocalState(userId, channelId int, action string, reqBody map[string]interface{}, resp map[string]interface{}) error {
	switch action {
	case "CreateAssetGroup":
		id := seedanceExtractStringField(resp, "Id")
		if id == "" {
			return fmt.Errorf("CreateAssetGroup: empty Id in response")
		}
		name, _ := reqBody["Name"].(string)
		desc, _ := reqBody["Description"].(string)
		return model.InsertUserAssetGroup(&model.UserAssetGroup{
			UserId: userId, ChannelId: channelId, GroupId: id,
			GroupType: model.GroupTypeAIGC, Name: name, Description: desc,
		})
	case "CreateAsset":
		id := seedanceExtractStringField(resp, "Id")
		if id == "" {
			return fmt.Errorf("CreateAsset: empty Id in response")
		}
		groupId, _ := reqBody["GroupId"].(string)
		url, _ := reqBody["URL"].(string)
		assetType, _ := reqBody["AssetType"].(string)
		if assetType == "" {
			assetType = "Image"
		}
		name, _ := reqBody["Name"].(string)
		return model.InsertUserAsset(&model.UserAsset{
			UserId: userId, ChannelId: channelId, GroupId: groupId,
			VirtualId: id, AssetUrl: "asset://" + id, Url: url,
			Filename: name, AssetType: assetType, Status: "pending",
		})
	case "UpdateAsset":
		id, _ := reqBody["Id"].(string)
		if id == "" {
			return fmt.Errorf("UpdateAsset: missing Id in request")
		}
		updates := map[string]interface{}{}
		if name, ok := reqBody["Name"].(string); ok {
			updates["filename"] = name
		}
		if len(updates) == 0 {
			return nil
		}
		return model.UpdateUserAssetFields(id, updates)
	case "UpdateAssetGroup":
		id, _ := reqBody["Id"].(string)
		if id == "" {
			return fmt.Errorf("UpdateAssetGroup: missing Id in request")
		}
		name, _ := reqBody["Name"].(string)
		desc, _ := reqBody["Description"].(string)
		return model.UpdateUserAssetGroupName(id, name, desc)
	case "DeleteAsset":
		id, _ := reqBody["Id"].(string)
		if id == "" {
			return fmt.Errorf("DeleteAsset: missing Id in request")
		}
		return model.DeleteUserAssetByVirtualId(id)
	case "DeleteAssetGroup":
		id, _ := reqBody["Id"].(string)
		if id == "" {
			return fmt.Errorf("DeleteAssetGroup: missing Id in request")
		}
		return model.DeleteUserAssetGroupByGroupId(id)
	case "GetVisualValidateResult":
		groupId := seedanceExtractStringField(resp, "GroupId")
		if groupId == "" {
			return fmt.Errorf("GetVisualValidateResult: empty GroupId in response")
		}
		return model.InsertUserAssetGroup(&model.UserAssetGroup{
			UserId: userId, ChannelId: channelId, GroupId: groupId,
			GroupType: model.GroupTypeLivenessFace,
		})
	default:
		// Read-only actions (ListAssets/ListAssetGroups/GetAsset/GetAssetGroup)
		// and CreateVisualValidateSession (no local state until verification
		// completes) have no local state to sync.
		return nil
	}
}
