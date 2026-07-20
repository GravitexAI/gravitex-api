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

// SeedanceOfficialAssetDispatch handles POST
// /ark/seedance/v3?Action=X&Version=2024-01-01 — the BytePlus asset-library
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
		// created via the existing H5 flow (/v1/visual-validate/session),
		// unchanged by this mirror. See design doc §4.2.
		body["GroupType"] = service.ByteplusGroupTypeAIGC
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

// seedanceExtractResultId pulls the "Id" field out of a raw BytePlus response
// map, checking the top level first, then inside "Result" — mirroring the
// existing extractStringField's behavior in byteplus_asset.go, since
// different upstream Actions place Id at different nesting levels.
func seedanceExtractResultId(resp map[string]interface{}) string {
	if v, ok := resp["Id"].(string); ok && v != "" {
		return v
	}
	if result, ok := resp["Result"].(map[string]interface{}); ok {
		if v, ok := result["Id"].(string); ok {
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
		id := seedanceExtractResultId(resp)
		if id == "" {
			return fmt.Errorf("CreateAssetGroup: empty Id in response")
		}
		name, _ := reqBody["Name"].(string)
		desc, _ := reqBody["Description"].(string)
		return model.InsertUserAssetGroup(&model.UserAssetGroup{
			UserId: userId, ChannelId: channelId, GroupId: id,
			GroupType: "aigc", Name: name, Description: desc,
		})
	case "CreateAsset":
		id := seedanceExtractResultId(resp)
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
	default:
		// Read-only actions (ListAssets/ListAssetGroups/GetAsset/GetAssetGroup)
		// have no local state to sync.
		return nil
	}
}
