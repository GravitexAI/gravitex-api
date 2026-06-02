package controller

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"

	"github.com/gin-gonic/gin"
)

// 默认用来模拟"生产环境返回数据量"的图片地址
const defaultTestImageURL = "https://gravitexgrayoss.tos-s3-ap-southeast-1.bytepluses.com/2026/05/27/2c4af9a81bcd4b7b8848a5359a3c6e59.png"

type testImagePayload struct {
	URL          string
	Base64       string
	ContentType  string
	SizeBytes    int
	Base64Length int
	FetchedAt    time.Time
	FetchCostMs  float64
}

var (
	testImageCache   *testImagePayload
	testImageCacheMu sync.RWMutex
)

func loadTestImageBase64(ctx context.Context, url string, refresh bool) (*testImagePayload, error) {
	if !refresh {
		testImageCacheMu.RLock()
		cached := testImageCache
		testImageCacheMu.RUnlock()
		if cached != nil && cached.URL == url {
			return cached, nil
		}
	}

	fetchStart := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch image: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch image: upstream status=%d", resp.StatusCode)
	}

	// 限制单图最大 50MB，防止把内存撑爆
	body, err := io.ReadAll(io.LimitReader(resp.Body, 50<<20))
	if err != nil {
		return nil, fmt.Errorf("read image body: %w", err)
	}

	encoded := base64.StdEncoding.EncodeToString(body)
	payload := &testImagePayload{
		URL:          url,
		Base64:       encoded,
		ContentType:  resp.Header.Get("Content-Type"),
		SizeBytes:    len(body),
		Base64Length: len(encoded),
		FetchedAt:    fetchStart,
		FetchCostMs:  time.Since(fetchStart).Seconds() * 1000,
	}

	testImageCacheMu.Lock()
	testImageCache = payload
	testImageCacheMu.Unlock()

	return payload, nil
}

// TestTimeout 是用来排查 nginx / CDN / 中间网关空闲超时的探针端点。
//
//   - GET/POST  /v1/test/timeout?time=240
//     普通模式：sleep 指定秒数后一次性返回 JSON，期间不发任何字节，
//     用于测试"上游 idle/read timeout"是不是基于"两次读之间的间隔"。
//
//   - GET/POST  /v1/test/timeout?time=240&stream=1&interval=5
//     流式模式：sleep 期间每隔 interval 秒发送一行心跳，
//     用于测试"中间网关是否要求持续有字节流动"。
//
// 端点不走 TokenAuth / Distribute / RateLimit，方便外部直接 curl。
// 客户端中途断开时通过 c.Request.Context() 感知并提前退出。
func TestTimeout(c *gin.Context) {
	timeParam := c.Query("time")
	if timeParam == "" {
		timeParam = c.DefaultQuery("seconds", "5")
	}

	sleepSeconds, err := strconv.ParseFloat(timeParam, 64)
	if err != nil || sleepSeconds < 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "invalid time param, must be a non-negative number of seconds, e.g. ?time=240",
		})
		return
	}

	// 防御性上限：12 小时
	const maxSeconds = 43200.0
	if sleepSeconds > maxSeconds {
		sleepSeconds = maxSeconds
	}

	streamFlag := c.Query("stream")
	streaming := streamFlag == "1" || streamFlag == "true"

	intervalSeconds, _ := strconv.ParseFloat(c.DefaultQuery("interval", "1"), 64)
	if intervalSeconds <= 0 {
		intervalSeconds = 1
	}

	startedAt := time.Now()
	ctx := c.Request.Context()
	hostname, _ := os.Hostname()

	common.SysLog(fmt.Sprintf(
		"[test/timeout] start  remote=%s  ua=%s  time=%.3fs  stream=%v  interval=%.3fs",
		c.ClientIP(), c.Request.UserAgent(), sleepSeconds, streaming, intervalSeconds,
	))

	if streaming {
		runStreaming(c, ctx, startedAt, sleepSeconds, intervalSeconds, hostname)
		return
	}

	runBlocking(c, ctx, startedAt, sleepSeconds, hostname)
}

func runBlocking(c *gin.Context, ctx context.Context, startedAt time.Time, sleepSeconds float64, hostname string) {
	timer := time.NewTimer(time.Duration(sleepSeconds * float64(time.Second)))
	defer timer.Stop()

	select {
	case <-timer.C:
	case <-ctx.Done():
		elapsed := time.Since(startedAt).Seconds()
		common.SysLog(fmt.Sprintf(
			"[test/timeout] client_canceled  remote=%s  requested=%.3fs  elapsed=%.3fs  err=%v",
			c.ClientIP(), sleepSeconds, elapsed, ctx.Err(),
		))
		return
	}

	elapsed := time.Since(startedAt).Seconds()
	common.SysLog(fmt.Sprintf(
		"[test/timeout] done  remote=%s  requested=%.3fs  elapsed=%.3fs",
		c.ClientIP(), sleepSeconds, elapsed,
	))

	c.JSON(http.StatusOK, gin.H{
		"success":           true,
		"mode":              "blocking",
		"requested_seconds": sleepSeconds,
		"elapsed_seconds":   elapsed,
		"started_at":        startedAt.Format(time.RFC3339Nano),
		"finished_at":       time.Now().Format(time.RFC3339Nano),
		"client_ip":         c.ClientIP(),
		"server_hostname":   hostname,
	})
}

func runStreaming(c *gin.Context, ctx context.Context, startedAt time.Time, sleepSeconds, intervalSeconds float64, hostname string) {
	c.Writer.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
	c.Writer.WriteHeader(http.StatusOK)
	c.Writer.Flush()

	deadline := startedAt.Add(time.Duration(sleepSeconds * float64(time.Second)))
	interval := time.Duration(intervalSeconds * float64(time.Second))
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	seq := 0
	writeBeat := func(final bool) bool {
		seq++
		elapsed := time.Since(startedAt).Seconds()
		line := fmt.Sprintf(
			"data: {\"seq\":%d,\"elapsed_seconds\":%.3f,\"requested_seconds\":%.3f,\"server_hostname\":%q,\"final\":%t}\n\n",
			seq, elapsed, sleepSeconds, hostname, final,
		)
		if _, err := c.Writer.WriteString(line); err != nil {
			common.SysLog(fmt.Sprintf(
				"[test/timeout] write_failed  remote=%s  elapsed=%.3fs  err=%v",
				c.ClientIP(), elapsed, err,
			))
			return false
		}
		c.Writer.Flush()
		return true
	}

	if !writeBeat(false) {
		return
	}

	for {
		if !time.Now().Before(deadline) {
			break
		}
		select {
		case <-ticker.C:
			if !writeBeat(false) {
				return
			}
		case <-ctx.Done():
			elapsed := time.Since(startedAt).Seconds()
			common.SysLog(fmt.Sprintf(
				"[test/timeout] stream_client_canceled  remote=%s  requested=%.3fs  elapsed=%.3fs  err=%v",
				c.ClientIP(), sleepSeconds, elapsed, ctx.Err(),
			))
			return
		}
	}

	writeBeat(true)
	elapsed := time.Since(startedAt).Seconds()
	common.SysLog(fmt.Sprintf(
		"[test/timeout] stream_done  remote=%s  requested=%.3fs  elapsed=%.3fs  beats=%d",
		c.ClientIP(), sleepSeconds, elapsed, seq,
	))
}

// TestTimeoutWithImage 与 TestTimeout 行为一致（阻塞模式，sleep 指定秒数后一次性返回），
// 区别在于响应体里会带上一张 OSS 图片的 base64，用来模拟生产 /v1/images/generations 那种
// 1~2MB 量级的响应数据，方便验证"大响应 + 长 sleep" 组合下是否会被中间网关截断。
//
//   - GET/POST  /v1/test/timeout-image?time=240
//     使用进程内缓存（首次启动后第一次请求会去 OSS 拉一次，之后所有请求都直接复用）
//
//   - GET/POST  /v1/test/timeout-image?time=240&refresh=1
//     强制刷新缓存，重新去 OSS 拉
//
//   - GET/POST  /v1/test/timeout-image?time=240&url=https://xxx
//     指定其他图片 URL（不写则用默认那张）
func TestTimeoutWithImage(c *gin.Context) {
	timeParam := c.Query("time")
	if timeParam == "" {
		timeParam = c.DefaultQuery("seconds", "5")
	}

	sleepSeconds, err := strconv.ParseFloat(timeParam, 64)
	if err != nil || sleepSeconds < 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "invalid time param, must be a non-negative number of seconds, e.g. ?time=240",
		})
		return
	}

	const maxSeconds = 43200.0
	if sleepSeconds > maxSeconds {
		sleepSeconds = maxSeconds
	}

	imageURL := c.DefaultQuery("url", defaultTestImageURL)
	refreshFlag := c.Query("refresh")
	refresh := refreshFlag == "1" || refreshFlag == "true"

	startedAt := time.Now()
	ctx := c.Request.Context()
	hostname, _ := os.Hostname()

	common.SysLog(fmt.Sprintf(
		"[test/timeout-image] start  remote=%s  ua=%s  time=%.3fs  refresh=%v  url=%s",
		c.ClientIP(), c.Request.UserAgent(), sleepSeconds, refresh, imageURL,
	))

	// 预加载图片（如有缓存则立即返回，否则走 HTTP 拉取）。
	// 放在 sleep 之前是为了让 sleep 时长尽量精确等于参数 time。
	img, err := loadTestImageBase64(ctx, imageURL, refresh)
	if err != nil {
		common.SysLog(fmt.Sprintf(
			"[test/timeout-image] image_fetch_failed  remote=%s  url=%s  err=%v",
			c.ClientIP(), imageURL, err,
		))
		c.JSON(http.StatusBadGateway, gin.H{
			"success": false,
			"message": fmt.Sprintf("fetch image failed: %v", err),
			"url":     imageURL,
		})
		return
	}

	timer := time.NewTimer(time.Duration(sleepSeconds * float64(time.Second)))
	defer timer.Stop()

	select {
	case <-timer.C:
	case <-ctx.Done():
		elapsed := time.Since(startedAt).Seconds()
		common.SysLog(fmt.Sprintf(
			"[test/timeout-image] client_canceled  remote=%s  requested=%.3fs  elapsed=%.3fs  err=%v",
			c.ClientIP(), sleepSeconds, elapsed, ctx.Err(),
		))
		return
	}

	elapsed := time.Since(startedAt).Seconds()
	common.SysLog(fmt.Sprintf(
		"[test/timeout-image] done  remote=%s  requested=%.3fs  elapsed=%.3fs  image_bytes=%d  base64_len=%d",
		c.ClientIP(), sleepSeconds, elapsed, img.SizeBytes, img.Base64Length,
	))

	c.JSON(http.StatusOK, gin.H{
		"success":           true,
		"mode":              "blocking_with_image",
		"requested_seconds": sleepSeconds,
		"elapsed_seconds":   elapsed,
		"started_at":        startedAt.Format(time.RFC3339Nano),
		"finished_at":       time.Now().Format(time.RFC3339Nano),
		"client_ip":         c.ClientIP(),
		"server_hostname":   hostname,
		"image": gin.H{
			"url":               img.URL,
			"content_type":      img.ContentType,
			"size_bytes":        img.SizeBytes,
			"base64_length":     img.Base64Length,
			"fetched_at":        img.FetchedAt.Format(time.RFC3339Nano),
			"fetch_cost_ms":     img.FetchCostMs,
			"served_from_cache": !refresh && img.FetchedAt.Before(startedAt),
			"base64":            img.Base64,
		},
	})
}
