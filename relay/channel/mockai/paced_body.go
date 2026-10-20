package mockai

import (
	"context"
	"io"
	"time"
)

// newPacedBody 把一组预先合成好的上游字节片按时间节奏吐出，用作 *http.Response 的 Body。
//
// 节奏约定：第 i 片在 firstDelay + (totalDelay-firstDelay)*i/(n-1) 时刻出现，
// 即首片等到 firstDelay、末片等到 totalDelay、中间均匀铺开；只有一片时（非流式）
// 直接等到 totalDelay，不做除零。
//
// sleep 是等待的注入点，传 nil 使用生产实现（响应 ctx 取消）。所有等待都必须能被
// ctx 取消或 Close 打断：StreamScannerHandler 在清理时会 Close 掉 Body 并
// wg.Wait() 等待 scanner goroutine 退出，若 Read 卡在一个不可打断的等待里会死锁。
func newPacedBody(ctx context.Context, frames [][]byte, firstDelay, totalDelay time.Duration, sleep func(context.Context, time.Duration) error) io.ReadCloser {
	if ctx == nil {
		ctx = context.Background()
	}
	if sleep == nil {
		sleep = waitOrCancel
	}
	ctx, cancel := context.WithCancel(ctx)
	return &pacedBody{
		ctx:        ctx,
		cancel:     cancel,
		frames:     frames,
		firstDelay: firstDelay,
		totalDelay: totalDelay,
		sleep:      sleep,
	}
}

// waitOrCancel 是生产等待实现。
func waitOrCancel(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

type pacedBody struct {
	ctx        context.Context
	cancel     context.CancelFunc
	frames     [][]byte
	firstDelay time.Duration
	totalDelay time.Duration
	sleep      func(context.Context, time.Duration) error

	next    int           // 下一片待吐出的 frame 下标
	elapsed time.Duration // 已经等过的虚拟时长
	cur     []byte        // 当前 frame 中尚未被读走的部分
	err     error
}

func (b *pacedBody) Read(p []byte) (int, error) {
	if b.err != nil {
		return 0, b.err
	}
	for len(b.cur) == 0 {
		if b.next >= len(b.frames) {
			b.err = io.EOF
			b.cancel()
			return 0, b.err
		}
		target := b.frameTime(b.next)
		if wait := target - b.elapsed; wait > 0 {
			if err := b.sleep(b.ctx, wait); err != nil {
				b.err = err
				return 0, err
			}
		}
		b.elapsed = target
		b.cur = b.frames[b.next]
		b.next++
	}
	n := copy(p, b.cur)
	b.cur = b.cur[n:]
	return n, nil
}

// frameTime 返回第 i 片应当出现的时刻（相对于响应开始）。
func (b *pacedBody) frameTime(i int) time.Duration {
	n := len(b.frames)
	if n <= 1 || b.totalDelay <= b.firstDelay {
		return b.totalDelay
	}
	return b.firstDelay + (b.totalDelay-b.firstDelay)*time.Duration(i)/time.Duration(n-1)
}

func (b *pacedBody) Close() error {
	b.cancel()
	return nil
}
