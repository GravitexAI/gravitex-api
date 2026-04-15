package middleware

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

// assetRequestBody is a minimal struct to extract content items with asset:// URLs from the request body.
type assetRequestBody struct {
	Content []map[string]interface{} `json:"content"`
}

// AssetResolveChannel is a middleware that runs before Distribute().
// It scans the request body for asset:// references, verifies ownership,
// checks they all belong to the same upstream channel, and forces routing
// to that channel via specific_channel_id.
func AssetResolveChannel() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Only process POST requests (video generation submissions)
		if c.Request.Method != http.MethodPost {
			c.Next()
			return
		}

		// Skip if specific_channel_id is already set (e.g. admin override)
		if _, exists := common.GetContextKey(c, constant.ContextKeyTokenSpecificChannelId); exists {
			c.Next()
			return
		}

		// Parse body to extract content items
		var body assetRequestBody
		if err := common.UnmarshalBodyReusable(c, &body); err != nil {
			// If we can't parse, let downstream handlers deal with it
			c.Next()
			return
		}

		// Extract asset:// virtual IDs from content items
		virtualIds := extractAssetVirtualIdsFromContent(body.Content)
		if len(virtualIds) == 0 {
			c.Next()
			return
		}

		// Verify ownership and get channel_ids
		userId := c.GetInt("id")
		channelMap, err := model.GetAssetChannelIdByVirtualIds(userId, virtualIds)
		if err != nil {
			abortWithOpenAiMessage(c, http.StatusInternalServerError, "素材引用校验失败 / Failed to validate asset references")
			return
		}

		// Check all referenced assets exist
		var missing []string
		for _, vid := range virtualIds {
			if _, ok := channelMap[vid]; !ok {
				missing = append(missing, vid)
			}
		}
		if len(missing) > 0 {
			abortWithOpenAiMessage(c, http.StatusBadRequest,
				fmt.Sprintf("素材未找到或无权访问 / Asset not found or access denied: %s", strings.Join(missing, ", ")))
			return
		}

		// Verify all assets belong to the same channel
		var targetChannelId int
		first := true
		for _, chId := range channelMap {
			if first {
				targetChannelId = chId
				first = false
			} else if chId != targetChannelId {
				abortWithOpenAiMessage(c, http.StatusBadRequest,
					"同一请求中的素材必须来自同一空间 / All assets in a single request must belong to the same space")
				return
			}
		}

		// Force routing to the asset's channel
		common.SetContextKey(c, constant.ContextKeyTokenSpecificChannelId, fmt.Sprintf("%d", targetChannelId))
		c.Next()
	}
}

// extractAssetVirtualIdsFromContent scans raw content items for asset:// URLs
// in image_url, video_url, and audio_url fields.
func extractAssetVirtualIdsFromContent(content []map[string]interface{}) []string {
	var ids []string
	for _, item := range content {
		if item == nil {
			continue
		}
		// Check image_url
		if m, ok := item["image_url"].(map[string]interface{}); ok && m != nil {
			if url, ok := m["url"].(string); ok && strings.HasPrefix(url, "asset://") {
				vid := strings.TrimPrefix(url, "asset://")
				if vid != "" {
					ids = append(ids, vid)
				}
			}
		}
		// Check video_url
		if m, ok := item["video_url"].(map[string]interface{}); ok && m != nil {
			if url, ok := m["url"].(string); ok && strings.HasPrefix(url, "asset://") {
				vid := strings.TrimPrefix(url, "asset://")
				if vid != "" {
					ids = append(ids, vid)
				}
			}
		}
		// Check audio_url
		if m, ok := item["audio_url"].(map[string]interface{}); ok && m != nil {
			if url, ok := m["url"].(string); ok && strings.HasPrefix(url, "asset://") {
				vid := strings.TrimPrefix(url, "asset://")
				if vid != "" {
					ids = append(ids, vid)
				}
			}
		}
	}
	return ids
}
