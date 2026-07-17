package middleware

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

// SeedanceOfficialMirror rewrites the official ByteDance/BytePlus Ark
// video-generation paths onto the platform's internal /v1/video/generations
// dispatch so the existing Distribute() channel-selection logic runs
// unchanged, while flagging the request (common.KeySeedanceRawMirror) so
// downstream code forwards request/response bytes verbatim instead of the
// platform's normalized shapes.
//
// Unlike KlingRequestConvert/JimengRequestConvert, this middleware does NOT
// rewrite the request body — see
// docs/byteplus/seedance-2.0-official-api-mirror-design.md §3.2.
func SeedanceOfficialMirror() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set(common.KeySeedanceRawMirror, true)

		switch c.Request.Method {
		case http.MethodPost:
			c.Request.URL.Path = "/v1/video/generations"
		case http.MethodGet:
			taskId := c.Param("id")
			c.Set("task_id", taskId)
			c.Request.URL.Path = "/v1/video/generations/" + taskId
		case http.MethodDelete:
			// No path rewrite: DELETE is handled by a dedicated controller
			// (controller.RelayTaskCancel), not the shared
			// RelayTask/RelayTaskFetch pipeline.
		}
		c.Next()
	}
}
