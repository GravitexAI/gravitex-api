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

	require.Len(t, waited, len(frames))
	assert.Equal(t, 300*time.Millisecond, waited[0])
	var total time.Duration
	for _, d := range waited {
		total += d
	}
	assert.Equal(t, 900*time.Millisecond, total)
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
