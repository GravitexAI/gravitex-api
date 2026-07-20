package router

import (
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"

	"github.com/gin-gonic/gin"
)

func SetVideoRouter(router *gin.Engine) {
	// Video proxy: accepts either session auth (dashboard) or token auth (API clients)
	videoProxyRouter := router.Group("/v1")
	videoProxyRouter.Use(middleware.RouteTag("relay"))
	videoProxyRouter.Use(middleware.TokenOrUserAuth())
	{
		videoProxyRouter.GET("/videos/:task_id/content", controller.VideoProxy)
	}

	// Asset library routes — TokenAuth only, no Distribute (multi-channel: uptoken/volcengine)
	assetV1Router := router.Group("/v1")
	assetV1Router.Use(middleware.TokenAuth())
	{
		assetV1Router.POST("/assets", controller.CreateAsset)
		assetV1Router.GET("/assets", controller.ListAssets)
		assetV1Router.GET("/assets/:virtual_id", controller.GetAsset)
		assetV1Router.DELETE("/assets/:virtual_id", controller.DeleteAsset)

		// Asset groups (BytePlus mode)
		assetV1Router.POST("/asset-groups", controller.CreateAssetGroup)
		assetV1Router.GET("/asset-groups", controller.ListAssetGroups)
		assetV1Router.DELETE("/asset-groups/:group_id", controller.DeleteAssetGroup)

		// Real-human portrait library: H5 liveness verification session lifecycle.
		// /session needs TokenAuth (binds the verification to the calling user/channel).
		assetV1Router.POST("/visual-validate/session", controller.CreateVisualValidateSession)

		// Seedance 2.0 moderation block-reason query (opt-in per channel, BytePlus whitelist required).
		// Lives on the TokenAuth-only group because the handler resolves the channel from the
		// task record itself — Distribute() would re-route to a possibly different channel.
		// Path uses `/moderation-result/:task_id` (segment-before-id) so it doesn't shadow the
		// existing `/video/generations/:task_id` GET handler when Gin's router resolves params.
		assetV1Router.GET("/video/generations/moderation-result/:task_id", controller.QueryModerationResult)
	}

	// /visual-validate/result is invoked anonymously by the gateway-hosted callback page;
	// authority comes from the HMAC-signed `state` token rather than a user session.
	router.POST("/v1/visual-validate/result", middleware.RouteTag("relay"), controller.SubmitVisualValidateResult)

	// Gateway-hosted callback landing page that BytePlus redirects to after H5 verification.
	// The HTML asset is embedded into the binary (see controller/visual_validate_callback.go).
	router.GET(controller.VisualValidateCallbackPath, controller.ServeVisualValidateCallbackPage)

	videoV1Router := router.Group("/v1")
	videoV1Router.Use(middleware.RouteTag("relay"))
	videoV1Router.Use(middleware.TokenAuth(), middleware.AssetResolveChannel(), middleware.Distribute())
	{
		videoV1Router.POST("/video/generations", controller.RelayTask)
		videoV1Router.GET("/video/generations/:task_id", controller.RelayTaskFetch)
		videoV1Router.POST("/videos/:video_id/remix", controller.RelayTask)
	}
	// openai compatible API video routes
	// docs: https://platform.openai.com/docs/api-reference/videos/create
	{
		videoV1Router.POST("/videos", controller.RelayTask)
		videoV1Router.GET("/videos/:task_id", controller.RelayTaskFetch)
	}

	// Seedance 2.0 official-mirror routes — byte-identical request/response to
	// BytePlus Ark's own API, only the base URL differs. See
	// docs/byteplus/seedance-2.0-official-api-mirror-design.md.
	seedanceOfficialRouter := router.Group("/api/v3/contents/generations")
	seedanceOfficialRouter.Use(middleware.RouteTag("relay"))
	seedanceOfficialRouter.Use(middleware.SeedanceOfficialMirror(), middleware.TokenAuth(), middleware.AssetResolveChannel(), middleware.Distribute())
	{
		seedanceOfficialRouter.POST("/tasks", controller.RelayTask)
		seedanceOfficialRouter.GET("/tasks/:id", controller.RelayTaskFetch)
	}

	seedanceOfficialCancelRouter := router.Group("/api/v3/contents/generations")
	seedanceOfficialCancelRouter.Use(middleware.RouteTag("relay"))
	seedanceOfficialCancelRouter.Use(middleware.SeedanceOfficialMirror(), middleware.TokenAuth())
	{
		seedanceOfficialCancelRouter.DELETE("/tasks/:id", controller.RelayTaskCancel)
	}

	// Seedance asset-library official-mirror route — single endpoint, Action
	// query param dispatch, same shape as the existing jimeng channel's
	// convention. See docs/byteplus/seedance-2.0-official-api-mirror-design.md §4.
	assetOfficialRouter := router.Group("/ark/seedance/v3")
	assetOfficialRouter.Use(middleware.RouteTag("relay"))
	assetOfficialRouter.Use(middleware.TokenAuth())
	{
		assetOfficialRouter.POST("", controller.SeedanceOfficialAssetDispatch)
	}

	klingV1Router := router.Group("/kling/v1")
	klingV1Router.Use(middleware.RouteTag("relay"))
	klingV1Router.Use(middleware.KlingRequestConvert(), middleware.TokenAuth(), middleware.Distribute())
	{
		klingV1Router.POST("/videos/text2video", controller.RelayTask)
		klingV1Router.POST("/videos/image2video", controller.RelayTask)
		klingV1Router.GET("/videos/text2video/:task_id", controller.RelayTaskFetch)
		klingV1Router.GET("/videos/image2video/:task_id", controller.RelayTaskFetch)
	}

	// Jimeng official API routes - direct mapping to official API format
	jimengOfficialGroup := router.Group("jimeng")
	jimengOfficialGroup.Use(middleware.RouteTag("relay"))
	jimengOfficialGroup.Use(middleware.JimengRequestConvert(), middleware.TokenAuth(), middleware.Distribute())
	{
		// Maps to: /?Action=CVSync2AsyncSubmitTask&Version=2022-08-31 and /?Action=CVSync2AsyncGetResult&Version=2022-08-31
		jimengOfficialGroup.POST("/", controller.RelayTask)
	}
}
