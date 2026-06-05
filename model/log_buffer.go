package model

import (
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
)

// GlobalLogBuffer 全局日志批量写入缓冲区，仅在日志库为 ClickHouse/ByteHouse 时初始化。
// 计费日志高频写入，列存数据库不适合单条 INSERT，故改为内存缓冲 + 定时/满量批量 flush。
var GlobalLogBuffer *LogBuffer

// LogBuffer 日志批量写入缓冲区
type LogBuffer struct {
	mu       sync.Mutex
	buffer   []*Log
	maxSize  int           // 批量阈值，达到即触发 flush
	interval time.Duration // 定时 flush 间隔
	ticker   *time.Ticker
	done     chan struct{}
	wg       sync.WaitGroup
}

// NewLogBuffer 创建缓冲区并启动后台 flush 协程
func NewLogBuffer(maxSize int, interval time.Duration) *LogBuffer {
	if maxSize <= 0 {
		maxSize = 5000
	}
	if interval <= 0 {
		interval = 5 * time.Second
	}
	lb := &LogBuffer{
		buffer:   make([]*Log, 0, maxSize),
		maxSize:  maxSize,
		interval: interval,
		ticker:   time.NewTicker(interval),
		done:     make(chan struct{}),
	}
	lb.wg.Add(1)
	go lb.flushLoop()
	return lb
}

// Add 追加一条日志到缓冲区；达到阈值立即 flush
func (lb *LogBuffer) Add(log *Log) {
	lb.mu.Lock()
	lb.buffer = append(lb.buffer, log)
	n := len(lb.buffer)
	lb.mu.Unlock()
	if n >= lb.maxSize {
		lb.Flush()
	}
}

func (lb *LogBuffer) flushLoop() {
	defer lb.wg.Done()
	for {
		select {
		case <-lb.ticker.C:
			lb.Flush()
		case <-lb.done:
			lb.Flush() // 退出前最后一次 flush
			return
		}
	}
}

// Flush 将缓冲区数据批量写入日志库
func (lb *LogBuffer) Flush() {
	lb.mu.Lock()
	if len(lb.buffer) == 0 {
		lb.mu.Unlock()
		return
	}
	batch := lb.buffer
	lb.buffer = make([]*Log, 0, lb.maxSize)
	lb.mu.Unlock()

	// Omit("id")：忽略 id 列，让 ByteHouse 列 DEFAULT generateSnowflakeID() 服务端生成雪花 ID。
	// 若显式带上 id（即便为 0），ClickHouse 会用该值而不触发 DEFAULT。
	if err := LOG_DB.Omit("id").CreateInBatches(batch, len(batch)).Error; err != nil {
		common.SysError(fmt.Sprintf("[LogBuffer] batch insert failed: %v, %d logs in this batch", err, len(batch)))
		// 计费数据兜底：写入失败时落本地文件，避免彻底丢失（可后续人工补写）
		dumpFailedLogs(batch)
	}
}

// Close 优雅停机：停定时器、触发最后一次 flush 并等待完成
func (lb *LogBuffer) Close() {
	lb.ticker.Stop()
	close(lb.done)
	lb.wg.Wait()
}

// dumpFailedLogs 批量写入失败时，把日志追加落盘到本地文件兜底
func dumpFailedLogs(batch []*Log) {
	defer func() { _ = recover() }()
	f, err := os.OpenFile("failed_logs.jsonl", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	for _, lg := range batch {
		b, e := common.Marshal(lg)
		if e != nil {
			continue
		}
		_, _ = f.Write(append(b, '\n'))
	}
}
