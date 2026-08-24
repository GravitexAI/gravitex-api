package router

import (
	"bufio"
	"log"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/relay"
	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/gin-gonic/gin"
)

// delayedWriter wraps a gin.ResponseWriter, buffering all output until flush().
// Used by the /chat/completions/:wait_time test endpoint to enforce minimum response time.
// IMPORTANT: gin.ResponseWriter is a NAMED field (not embedded) to prevent io.Copy
// from detecting io.ReaderFrom on the underlying writer and bypassing our buffer.
type delayedWriter struct {
	w          gin.ResponseWriter
	buf        []byte
	delay      time.Duration
	once       sync.Once
	statusCode int
}

func (w *delayedWriter) Header() http.Header {
	return w.w.Header()
}

func (w *delayedWriter) Write(b []byte) (int, error) {
	w.buf = append(w.buf, b...)
	return len(b), nil
}

func (w *delayedWriter) WriteString(s string) (int, error) {
	return w.Write([]byte(s))
}

func (w *delayedWriter) WriteHeader(code int) {
	if w.statusCode == 0 {
		w.statusCode = code
	}
}

// Required by gin.ResponseWriter interface — must be no-op to prevent early flush
func (w *delayedWriter) WriteHeaderNow() {}

// Required by gin.ResponseWriter interface — must be no-op to prevent early flush
func (w *delayedWriter) Flush() {}

func (w *delayedWriter) Status() int   { return w.statusCode }
func (w *delayedWriter) Size() int     { return len(w.buf) }
func (w *delayedWriter) Written() bool { return len(w.buf) > 0 || w.statusCode > 0 }
func (w *delayedWriter) Pusher() http.Pusher {
	if pusher, ok := w.w.(http.Pusher); ok {
		return pusher
	}
	return nil
}

// Required by gin.ResponseWriter (http.Hijacker)
func (w *delayedWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return w.w.Hijack()
}

// Required by gin.ResponseWriter (http.CloseNotifier)
func (w *delayedWriter) CloseNotify() <-chan bool {
	if notifier, ok := w.w.(http.CloseNotifier); ok {
		return notifier.CloseNotify()
	}
	return make(chan bool)
}

func (w *delayedWriter) flush() {
	w.once.Do(func() {
		time.Sleep(w.delay)
		if w.statusCode > 0 {
			w.w.WriteHeader(w.statusCode)
		}
		if len(w.buf) > 0 {
			_, _ = w.w.Write(w.buf)
		}
	})
}

func SetRelayRouter(router *gin.Engine) {
	router.Use(middleware.CORS())
	router.Use(middleware.DecompressRequestMiddleware())
	router.Use(middleware.BodyStorageCleanup()) // 清理请求体存储
	router.Use(middleware.StatsMiddleware())
	// https://platform.openai.com/docs/api-reference/introduction
	modelsRouter := router.Group("/v1/models")
	modelsRouter.Use(middleware.RouteTag("relay"))
	modelsRouter.Use(middleware.TokenAuth())
	{
		modelsRouter.GET("", func(c *gin.Context) {
			switch {
			case c.GetHeader("x-api-key") != "" && c.GetHeader("anthropic-version") != "":
				controller.ListModels(c, constant.ChannelTypeAnthropic)
			case c.GetHeader("x-goog-api-key") != "" || c.Query("key") != "": // 单独的适配
				controller.ListModels(c, constant.ChannelTypeGemini)
			default:
				controller.ListModels(c, constant.ChannelTypeOpenAI)
			}
		})

		modelsRouter.GET("/:model", func(c *gin.Context) {
			switch {
			case c.GetHeader("x-api-key") != "" && c.GetHeader("anthropic-version") != "":
				controller.RetrieveModel(c, constant.ChannelTypeAnthropic)
			default:
				controller.RetrieveModel(c, constant.ChannelTypeOpenAI)
			}
		})
	}

	geminiRouter := router.Group("/v1beta/models")
	geminiRouter.Use(middleware.RouteTag("relay"))
	geminiRouter.Use(middleware.TokenAuth())
	{
		geminiRouter.GET("", func(c *gin.Context) {
			controller.ListModels(c, constant.ChannelTypeGemini)
		})
	}

	geminiCompatibleRouter := router.Group("/v1beta/openai/models")
	geminiCompatibleRouter.Use(middleware.RouteTag("relay"))
	geminiCompatibleRouter.Use(middleware.TokenAuth())
	{
		geminiCompatibleRouter.GET("", func(c *gin.Context) {
			controller.ListModels(c, constant.ChannelTypeOpenAI)
		})
	}

	playgroundRouter := router.Group("/pg")
	playgroundRouter.Use(middleware.RouteTag("relay"))
	playgroundRouter.Use(middleware.SystemPerformanceCheck())
	playgroundRouter.Use(middleware.UserAuth(), middleware.Distribute())
	{
		playgroundRouter.POST("/chat/completions", controller.Playground)
	}

	// 超时探针端点：不挂任何认证/分发/限流中间件，方便从外部 curl 排查 nginx/CDN/网关 idle timeout
	// GET/POST /v1/test/timeout?time=240
	// GET/POST /v1/test/timeout?time=240&stream=1&interval=5
	testTimeoutRouter := router.Group("/v1/test")
	testTimeoutRouter.Use(middleware.RouteTag("relay"))
	{
		testTimeoutRouter.GET("/timeout", controller.TestTimeout)
		testTimeoutRouter.POST("/timeout", controller.TestTimeout)
		testTimeoutRouter.GET("/timeout-image", controller.TestTimeoutWithImage)
		testTimeoutRouter.POST("/timeout-image", controller.TestTimeoutWithImage)
		testTimeoutRouter.GET("/metrics", controller.GetTestMetrics)
	}

	relayV1Router := router.Group("/v1")
	relayV1Router.Use(middleware.RouteTag("relay"))
	relayV1Router.Use(middleware.SystemPerformanceCheck())
	relayV1Router.Use(middleware.TokenAuth())
	relayV1Router.Use(middleware.ModelRequestRateLimit())
	{
		// WebSocket 路由（统一到 Relay）
		wsRouter := relayV1Router.Group("")
		wsRouter.Use(middleware.Distribute())
		wsRouter.GET("/realtime", func(c *gin.Context) {
			controller.Relay(c, types.RelayFormatOpenAIRealtime)
		})
	}
	{
		//http router
		httpRouter := relayV1Router.Group("")
		httpRouter.Use(middleware.Distribute())

		// claude related routes
		httpRouter.POST("/messages", func(c *gin.Context) {
			controller.Relay(c, types.RelayFormatClaude)
		})

		// chat related routes
		httpRouter.POST("/completions", func(c *gin.Context) {
			controller.Relay(c, types.RelayFormatOpenAI)
		})
		httpRouter.POST("/chat/completions", func(c *gin.Context) {
			controller.Relay(c, types.RelayFormatOpenAI)
		})
		// Test endpoint: force minimum response time (wait_time in milliseconds)
		httpRouter.POST("/chat/completions/:wait_time", func(c *gin.Context) {
			wt, err := strconv.Atoi(c.Param("wait_time"))
			if err != nil || wt <= 0 {
				controller.Relay(c, types.RelayFormatOpenAI)
				return
			}
			delay := time.Duration(wt) * time.Millisecond
			// Strip /:wait_time from path so upstream sees /v1/chat/completions
			c.Request.URL.Path = "/v1/chat/completions"
			c.Request.URL.RawPath = ""
			dw := &delayedWriter{w: c.Writer, delay: delay}
			c.Writer = dw
			controller.Relay(c, types.RelayFormatOpenAI)
			dw.flush()
			log.Printf("[Relay] wait_time enforced: %v", delay)
		})

		// response related routes
		httpRouter.POST("/responses", func(c *gin.Context) {
			controller.Relay(c, types.RelayFormatOpenAIResponses)
		})
		httpRouter.POST("/responses/compact", func(c *gin.Context) {
			controller.Relay(c, types.RelayFormatOpenAIResponsesCompaction)
		})

		// alpha search related routes (Codex standalone web search)
		httpRouter.POST("/alpha/search", func(c *gin.Context) {
			controller.Relay(c, types.RelayFormatOpenAIAlphaSearch)
		})

		// image related routes
		httpRouter.POST("/edits", func(c *gin.Context) {
			controller.Relay(c, types.RelayFormatOpenAIImage)
		})
		httpRouter.POST("/images/generations", func(c *gin.Context) {
			controller.Relay(c, types.RelayFormatOpenAIImage)
		})
		httpRouter.POST("/images/edits", func(c *gin.Context) {
			controller.Relay(c, types.RelayFormatOpenAIImage)
		})

		// embedding related routes
		httpRouter.POST("/embeddings", func(c *gin.Context) {
			controller.Relay(c, types.RelayFormatEmbedding)
		})

		// audio related routes
		httpRouter.POST("/audio/transcriptions", func(c *gin.Context) {
			controller.Relay(c, types.RelayFormatOpenAIAudio)
		})
		httpRouter.POST("/audio/translations", func(c *gin.Context) {
			controller.Relay(c, types.RelayFormatOpenAIAudio)
		})
		httpRouter.POST("/audio/speech", func(c *gin.Context) {
			controller.Relay(c, types.RelayFormatOpenAIAudio)
		})

		// rerank related routes
		httpRouter.POST("/rerank", func(c *gin.Context) {
			controller.Relay(c, types.RelayFormatRerank)
		})

		// gemini relay routes
		httpRouter.POST("/engines/:model/embeddings", func(c *gin.Context) {
			controller.Relay(c, types.RelayFormatGemini)
		})
		httpRouter.POST("/models/*path", func(c *gin.Context) {
			controller.Relay(c, types.RelayFormatGemini)
		})

		// other relay routes
		httpRouter.POST("/moderations", func(c *gin.Context) {
			controller.Relay(c, types.RelayFormatOpenAI)
		})

		// not implemented
		httpRouter.POST("/images/variations", controller.RelayNotImplemented)
		httpRouter.GET("/files", controller.RelayNotImplemented)
		httpRouter.POST("/files", controller.RelayNotImplemented)
		httpRouter.DELETE("/files/:id", controller.RelayNotImplemented)
		httpRouter.GET("/files/:id", controller.RelayNotImplemented)
		httpRouter.GET("/files/:id/content", controller.RelayNotImplemented)
		httpRouter.POST("/fine-tunes", controller.RelayNotImplemented)
		httpRouter.GET("/fine-tunes", controller.RelayNotImplemented)
		httpRouter.GET("/fine-tunes/:id", controller.RelayNotImplemented)
		httpRouter.POST("/fine-tunes/:id/cancel", controller.RelayNotImplemented)
		httpRouter.GET("/fine-tunes/:id/events", controller.RelayNotImplemented)
		httpRouter.DELETE("/models/:model", controller.RelayNotImplemented)
	}

	relayMjRouter := router.Group("/mj")
	relayMjRouter.Use(middleware.RouteTag("relay"))
	relayMjRouter.Use(middleware.SystemPerformanceCheck())
	registerMjRouterGroup(relayMjRouter)

	relayMjModeRouter := router.Group("/:mode/mj")
	relayMjModeRouter.Use(middleware.RouteTag("relay"))
	relayMjModeRouter.Use(middleware.SystemPerformanceCheck())
	registerMjRouterGroup(relayMjModeRouter)
	//relayMjRouter.Use()

	relaySunoRouter := router.Group("/suno")
	relaySunoRouter.Use(middleware.RouteTag("relay"))
	relaySunoRouter.Use(middleware.SystemPerformanceCheck())
	relaySunoRouter.Use(middleware.TokenAuth(), middleware.Distribute())
	{
		relaySunoRouter.POST("/submit/:action", controller.RelayTask)
		relaySunoRouter.POST("/fetch", controller.RelayTaskFetch)
		relaySunoRouter.GET("/fetch/:id", controller.RelayTaskFetch)
	}

	// Gemini API 路径格式: /v1beta/models/{model_name}:{action}
	// v1beta 是 Gemini Developer API 的 preview 版本名，v1beta1 是 Vertex AI 对同一
	// preview 面的叫法。两种写法都接受，上游用哪个由渠道 adaptor 按自己后端的方言决定。
	for _, prefix := range []string{"/v1beta", "/v1beta1"} {
		relayGeminiRouter := router.Group(prefix)
		relayGeminiRouter.Use(middleware.RouteTag("relay"))
		relayGeminiRouter.Use(middleware.SystemPerformanceCheck())
		relayGeminiRouter.Use(middleware.TokenAuth())
		relayGeminiRouter.Use(middleware.ModelRequestRateLimit())
		relayGeminiRouter.Use(middleware.Distribute())
		relayGeminiRouter.POST("/models/*path", func(c *gin.Context) {
			controller.Relay(c, types.RelayFormatGemini)
		})
	}
}

func registerMjRouterGroup(relayMjRouter *gin.RouterGroup) {
	relayMjRouter.GET("/image/:id", relay.RelayMidjourneyImage)
	relayMjRouter.Use(middleware.TokenAuth(), middleware.Distribute())
	{
		relayMjRouter.POST("/submit/action", controller.RelayMidjourney)
		relayMjRouter.POST("/submit/shorten", controller.RelayMidjourney)
		relayMjRouter.POST("/submit/modal", controller.RelayMidjourney)
		relayMjRouter.POST("/submit/imagine", controller.RelayMidjourney)
		relayMjRouter.POST("/submit/change", controller.RelayMidjourney)
		relayMjRouter.POST("/submit/simple-change", controller.RelayMidjourney)
		relayMjRouter.POST("/submit/describe", controller.RelayMidjourney)
		relayMjRouter.POST("/submit/blend", controller.RelayMidjourney)
		relayMjRouter.POST("/submit/edits", controller.RelayMidjourney)
		relayMjRouter.POST("/submit/video", controller.RelayMidjourney)
		//relayMjRouter.POST("/notify", controller.RelayMidjourney)
		relayMjRouter.GET("/task/:id/fetch", controller.RelayMidjourney)
		relayMjRouter.GET("/task/:id/image-seed", controller.RelayMidjourney)
		relayMjRouter.POST("/task/list-by-condition", controller.RelayMidjourney)
		relayMjRouter.POST("/insight-face/swap", controller.RelayMidjourney)
		relayMjRouter.POST("/submit/upload-discord-images", controller.RelayMidjourney)
	}
}
