package mockai

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPacedBodyCancellation 保证客户端断开时读取立刻结束，而不是把 goroutine
// 挂满整个 latency 窗口。
func TestPacedBodyCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	body := newPacedBody(ctx, [][]byte{[]byte("first"), []byte("second")},
		10*time.Second, 30*time.Second, nil)
	defer body.Close()

	start := time.Now()
	_, err := io.ReadAll(body)
	elapsed := time.Since(start)

	require.ErrorIs(t, err, context.Canceled)
	assert.Less(t, elapsed, time.Second)
}

// TestPacedBodyCloseUnblocksPendingRead 守护一条被定性为「不能退化」的契约：
// Close() 必须能打断一个正在等待中的 Read。StreamScannerHandler 的 cleanup()
// 先 resp.Body.Close() 再 wg.Wait()，若 Read 卡在不可打断的等待里，scanner
// goroutine 永远不退出，整个 handler 死锁。
// 这条和 TestPacedBodyCancellation 不同：那条验的是父 ctx 传播。
func TestPacedBodyCloseUnblocksPendingRead(t *testing.T) {
	body := newPacedBody(context.Background(), [][]byte{[]byte("first"), []byte("second")},
		10*time.Second, 30*time.Second, nil)

	type readResult struct {
		err error
	}
	done := make(chan readResult, 1)
	go func() {
		_, err := io.ReadAll(body)
		done <- readResult{err: err}
	}()

	// 让 Read 先进入等待，再关闭。
	time.Sleep(20 * time.Millisecond)
	require.NoError(t, body.Close())

	select {
	case got := <-done:
		require.ErrorIs(t, got.err, context.Canceled)
	case <-time.After(2 * time.Second):
		t.Fatal("Close() did not unblock the pending Read")
	}
}

// TestPacedBodyEmitsAllFrames 锁住两条契约：读出的字节恰好是所有 frame 的拼接，
// 且首片等到 firstDelay、末片等到 totalDelay，中间均匀铺开。
func TestPacedBodyEmitsAllFrames(t *testing.T) {
	frames := [][]byte{
		[]byte("data: one\n\n"),
		[]byte("data: two\n\n"),
		[]byte("data: three\n\n"),
		[]byte("data: [DONE]\n\n"),
	}

	var waited []time.Duration
	sleep := func(_ context.Context, d time.Duration) error {
		waited = append(waited, d)
		return nil
	}

	body := newPacedBody(context.Background(), frames, 300*time.Millisecond, 900*time.Millisecond, sleep)
	defer body.Close()

	got, err := io.ReadAll(body)
	require.NoError(t, err)
	assert.Equal(t, "data: one\n\ndata: two\n\ndata: three\n\ndata: [DONE]\n\n", string(got))

	// 首片等到 300ms，其余三片把剩下的 600ms 均分 —— 逐项断言，
	// 否则任何「首 300、总 900」的单调排程都能蒙混过关，「均匀铺开」零保护。
	assert.Equal(t, []time.Duration{
		300 * time.Millisecond,
		200 * time.Millisecond,
		200 * time.Millisecond,
		200 * time.Millisecond,
	}, waited)
}

// TestPacedBodySingleFrame 覆盖非流式形态：只有一片时不得除零 panic，
// 且总等待时长等于 totalDelay。
func TestPacedBodySingleFrame(t *testing.T) {
	var waited []time.Duration
	sleep := func(_ context.Context, d time.Duration) error {
		waited = append(waited, d)
		return nil
	}

	body := newPacedBody(context.Background(), [][]byte{[]byte(`{"ok":true}`)},
		200*time.Millisecond, 900*time.Millisecond, sleep)
	defer body.Close()

	got, err := io.ReadAll(body)
	require.NoError(t, err)
	assert.Equal(t, `{"ok":true}`, string(got))

	var total time.Duration
	for _, d := range waited {
		total += d
	}
	assert.Equal(t, 900*time.Millisecond, total)
}
