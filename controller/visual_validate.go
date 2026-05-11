package controller

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/gin-gonic/gin"
)

// VisualValidateCallbackPath is the gateway-hosted static page that receives the
// BytePlus liveness verification callback. It is registered in router/video-router.go
// and served from web/static/asset-validate-callback.html.
const VisualValidateCallbackPath = "/asset-validate-callback.html"

type createVisualValidateSessionRequest struct {
	ChannelId   int    `json:"channel_id"`
	Name        string `json:"name" binding:"required"`
	Description string `json:"description"`
}

type submitVisualValidateResultRequest struct {
	State      string `json:"state" binding:"required"`
	BytedToken string `json:"byted_token" binding:"required"`
}

// CreateVisualValidateSession launches a real-person liveness verification session.
//
// Flow:
//  1. Pick a BytePlus-enabled channel for the user (explicit channel_id or auto).
//  2. Enforce the per-user-per-channel asset group quota (AIGC + LivenessFace combined).
//  3. Sign a state token (binds user/channel/group_name + ttl).
//  4. Build CallbackURL = {ServerAddress}{CallbackPath}?state={signed}.
//  5. Call BytePlus CreateVisualValidateSession; persist the BytedToken into the state.
//  6. Return {h5_link, state, expires_in, channel_id} to the client; the client opens
//     h5_link in a popup and listens for the callback page's postMessage.
func CreateVisualValidateSession(c *gin.Context) {
	userId := c.GetInt("id")
	group := common.GetContextKeyString(c, constant.ContextKeyUsingGroup)

	var req createVisualValidateSessionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		assetErrorResponse(c, http.StatusBadRequest, "Invalid request: "+err.Error())
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		assetErrorResponse(c, http.StatusBadRequest, "name is required")
		return
	}

	ch, err := pickByteplusChannel(c, group, req.ChannelId)
	if err != nil {
		return
	}

	// Fall back to the user's username when the client omits a description, so
	// the BytePlus console always shows a useful label for who owns each group.
	// We deliberately don't override an explicit description from the client.
	req.Description = strings.TrimSpace(req.Description)
	if req.Description == "" {
		if username, uerr := model.GetUsernameById(userId, false); uerr == nil && username != "" {
			req.Description = username
		}
	}

	limit := system_setting.GetByteplusAssetGroupLimit()
	count, err := model.CountUserAssetGroupsByChannel(userId, ch.Id, "")
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("CreateVisualValidateSession: count check failed: %s", err.Error()))
		assetErrorResponse(c, http.StatusInternalServerError, "Failed to check group limit")
		return
	}
	if int(count) >= limit {
		assetErrorResponse(c, http.StatusForbidden, fmt.Sprintf("Asset group limit reached (%d)", limit))
		return
	}

	cfg := getByteplusAssetConfig(ch)

	// Pre-sign a state we can attach to the CallbackURL. We sign twice:
	//   1) the first signature carries everything except BytedToken so we can include it
	//      in the URL the BytePlus call needs.
	//   2) after BytePlus returns BytedToken, we re-sign with BytedToken bound, and
	//      return that value to the client. The client will surface the FULL state when
	//      the callback page POSTs back to us.
	preState := &service.VisualValidateState{
		UserId:    userId,
		ChannelId: ch.Id,
		GroupName: req.Name,
		GroupDesc: req.Description,
	}
	preToken, err := service.SignVisualValidateState(preState)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("CreateVisualValidateSession: sign pre-state failed: %s", err.Error()))
		assetErrorResponse(c, http.StatusInternalServerError, "Failed to sign state")
		return
	}

	callbackURL := buildVisualValidateCallbackURL(preToken)

	h5Link, bytedToken, err := service.ByteplusCreateVisualValidateSession(cfg, callbackURL)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("CreateVisualValidateSession: byteplus error: %s", err.Error()))
		assetErrorResponse(c, http.StatusBadGateway, "Failed to create visual validate session: "+err.Error())
		return
	}

	finalState := &service.VisualValidateState{
		UserId:     userId,
		ChannelId:  ch.Id,
		GroupName:  req.Name,
		GroupDesc:  req.Description,
		BytedToken: bytedToken,
		IssuedAt:   preState.IssuedAt,
		ExpireAt:   preState.ExpireAt,
	}
	finalStateToken, err := service.SignVisualValidateState(finalState)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("CreateVisualValidateSession: sign final state failed: %s", err.Error()))
		assetErrorResponse(c, http.StatusInternalServerError, "Failed to sign state")
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"h5_link":     h5Link,
		"state":       finalStateToken,
		"channel_id":  ch.Id,
		"byted_token": bytedToken,
		"expires_in":  int(service.VisualValidateStateTTL.Seconds()),
	})
}

// SubmitVisualValidateResult is invoked by the gateway-hosted callback page (no auth)
// once BytePlus redirects the end user back. It validates the signed state, exchanges
// the BytedToken for the new GroupId, and persists the asset group locally with
// group_type="liveness_face".
func SubmitVisualValidateResult(c *gin.Context) {
	var req submitVisualValidateResultRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		assetErrorResponse(c, http.StatusBadRequest, "Invalid request: "+err.Error())
		return
	}

	state, err := service.VerifyVisualValidateState(req.State)
	if err != nil {
		assetErrorResponse(c, http.StatusBadRequest, "Invalid or expired state: "+err.Error())
		return
	}
	if state.BytedToken != "" && state.BytedToken != req.BytedToken {
		assetErrorResponse(c, http.StatusBadRequest, "BytedToken mismatch with signed state")
		return
	}

	ch, err := getAssetChannelById(state.ChannelId)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("SubmitVisualValidateResult: channel lookup failed: %s", err.Error()))
		assetErrorResponse(c, http.StatusServiceUnavailable, "Asset channel unavailable")
		return
	}
	if !isByteplusAssetChannel(ch) {
		assetErrorResponse(c, http.StatusBadRequest, "Channel does not have BytePlus asset configuration")
		return
	}

	cfg := getByteplusAssetConfig(ch)

	groupId, err := service.ByteplusGetVisualValidateResult(cfg, req.BytedToken)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("SubmitVisualValidateResult: byteplus error: %s", err.Error()))
		assetErrorResponse(c, http.StatusBadGateway, "Failed to fetch validate result: "+err.Error())
		return
	}

	// CreateVisualValidateSession does not accept Name/Description, so the group
	// BytePlus auto-creates is initially nameless. Backfill it now so the user
	// sees the same label they entered, both in our DB and the BytePlus console.
	// Failure here is non-fatal — the local row will still hold the canonical name.
	if updateErr := service.ByteplusUpdateAssetGroup(cfg, groupId, state.GroupName, state.GroupDesc); updateErr != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("SubmitVisualValidateResult: backfill name on byteplus failed (group=%s): %s", groupId, updateErr.Error()))
	}

	// Idempotency: if BytePlus reuses a GroupId for the same end user (e.g. retry),
	// the unique index on group_id will reject the second insert. Prefer surfacing
	// the existing group rather than failing.
	if existing, lookupErr := model.GetUserAssetGroupByGroupId(groupId); lookupErr == nil && existing != nil {
		if existing.UserId != state.UserId {
			assetErrorResponse(c, http.StatusConflict, "GroupId already bound to another user")
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"group_id":   existing.GroupId,
			"name":       existing.Name,
			"channel_id": existing.ChannelId,
			"group_type": existing.GroupType,
			"reused":     true,
		})
		return
	}

	assetGroup := &model.UserAssetGroup{
		UserId:      state.UserId,
		ChannelId:   ch.Id,
		GroupId:     groupId,
		GroupType:   model.GroupTypeLivenessFace,
		Name:        state.GroupName,
		Description: state.GroupDesc,
		ProjectName: cfg.ProjectName,
	}
	if err := model.InsertUserAssetGroup(assetGroup); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("SubmitVisualValidateResult: failed to save locally: %s", err.Error()))
		// Group exists upstream — still return success so the client doesn't loop.
	}

	c.JSON(http.StatusOK, gin.H{
		"group_id":   groupId,
		"name":       state.GroupName,
		"channel_id": ch.Id,
		"group_type": model.GroupTypeLivenessFace,
	})
}

// pickByteplusChannel selects an upstream BytePlus channel for the user, either explicit
// (by channel_id) or auto (first enabled). It writes the appropriate error response and
// returns nil if no channel is usable.
func pickByteplusChannel(c *gin.Context, group string, explicitId int) (*model.Channel, error) {
	if explicitId > 0 {
		ch, err := getAssetChannelById(explicitId)
		if err != nil {
			assetErrorResponse(c, http.StatusBadRequest, "Invalid or unsupported channel")
			return nil, err
		}
		if !isByteplusAssetChannel(ch) {
			assetErrorResponse(c, http.StatusBadRequest, "Channel does not have BytePlus asset configuration")
			return nil, fmt.Errorf("channel %d not byteplus", explicitId)
		}
		return ch, nil
	}
	channels, err := getByteplusEnabledChannels(group)
	if err != nil || len(channels) == 0 {
		logger.LogError(c.Request.Context(), fmt.Sprintf("pickByteplusChannel: no byteplus channel for group=%s: %v", group, err))
		assetErrorResponse(c, http.StatusServiceUnavailable, "No BytePlus asset channel available")
		if err == nil {
			err = fmt.Errorf("no channels")
		}
		return nil, err
	}
	return channels[0], nil
}

// buildVisualValidateCallbackURL composes the absolute CallbackURL passed to BytePlus.
// BytePlus appends `?bytedToken=...&resultCode=...` (or `&...` if `?` already present)
// before redirecting the end user, so we use a single `state=` query param up front.
func buildVisualValidateCallbackURL(stateToken string) string {
	base := strings.TrimRight(system_setting.ServerAddress, "/")
	if base == "" {
		base = "http://localhost:3000"
	}
	q := url.Values{}
	q.Set("state", stateToken)
	return base + VisualValidateCallbackPath + "?" + q.Encode()
}
