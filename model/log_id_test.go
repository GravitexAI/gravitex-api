package model

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// 单节点内雪花 id 必须严格递增且不重复（时间戳高位 + 毫秒内序列保证）
func TestGenerateLogIDMonotonicUnique(t *testing.T) {
	const n = 20000
	seen := make(map[int64]struct{}, n)
	prev := int64(0)
	for i := 0; i < n; i++ {
		id := generateLogID()
		require.Greater(t, id, prev, "ids must be strictly increasing")
		_, dup := seen[id]
		require.False(t, dup, "id %d duplicated", id)
		seen[id] = struct{}{}
		prev = id
	}
}

// 多协程并发下不重复
func TestGenerateLogIDConcurrentUnique(t *testing.T) {
	const workers = 8
	const perWorker = 5000
	var mu sync.Mutex
	seen := make(map[int64]struct{}, workers*perWorker)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				id := generateLogID()
				mu.Lock()
				seen[id] = struct{}{}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	require.Len(t, seen, workers*perWorker)
}

// 高 41 位必须是生成时刻的 Unix 毫秒：保证与 ByteHouse 存量 id（同布局、历史时间戳）
// 处于同一数值空间且必然更大，keyset 分页的时间有序性不变
func TestGenerateLogIDLayoutUnixMillis(t *testing.T) {
	before := time.Now().UnixMilli()
	id := generateLogID()
	after := time.Now().UnixMilli()
	ts := id >> logIDTimestampShift
	require.GreaterOrEqual(t, ts, before)
	require.LessOrEqual(t, ts, after)
	// 低 22 位不越过节点标识 + 序列的边界
	require.Equal(t, int64(0), id>>63)
	require.LessOrEqual(t, id&(1<<logIDTimestampShift-1), int64(1)<<logIDTimestampShift-1)
}
