package model

import (
	"hash/fnv"
	"os"
	"strconv"
	"sync"
	"time"
)

// ADB（AnalyticDB for MySQL）日志库没有 ByteHouse 的 DEFAULT generateSnowflakeID()，
// 也没有自增列，logs.id 必须在应用侧生成后随行写入。
//
// 数值布局与 ByteHouse 生成的存量 id 同一空间：高 41 位是 Unix 毫秒时间戳
// （时间有序，keyset 分页按 id 递减扫描的行为不变），低 22 位是节点标识 + 毫秒内序列。
// 新 id 的时间戳必然大于全部存量数据的时间戳，不会与已同步的历史 id 冲突。
//
// 布局：1 符号位(恒 0) | 41 位 Unix 毫秒 | 12 位节点标识 | 10 位毫秒内序列
// （41+22=63 位，符号位恒 0 保证恒为正数，与 ByteHouse 的数值范围一致）
const (
	logIDMachineBits    = 12
	logIDSequenceBits   = 10
	logIDMaxSequence    = 1<<logIDSequenceBits - 1
	logIDMaxMachine     = 1<<logIDMachineBits - 1
	logIDTimestampShift = logIDMachineBits + logIDSequenceBits
)

var (
	logIDMu        sync.Mutex
	logIDMachineID int64
	logIDLastMS    int64
	logIDSequence  int64
)

func init() {
	// 节点标识默认取 主机名+进程PID 的哈希（各转发节点主机名不同即可错开）；
	// 极小概率的哈希撞车可用 LOG_ID_WORKER_ID 显式指定（0-4095，每台节点配不同值）。
	if v, err := strconv.Atoi(os.Getenv("LOG_ID_WORKER_ID")); err == nil && v >= 0 && v <= logIDMaxMachine {
		logIDMachineID = int64(v)
		return
	}
	host, err := os.Hostname()
	if err != nil {
		host = "unknown-host"
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(host + ":" + strconv.Itoa(os.Getpid())))
	logIDMachineID = int64(h.Sum32() & logIDMaxMachine)
}

// generateLogID 生成 logs 表的应用侧雪花 id（ADB 日志库模式使用）
func generateLogID() int64 {
	logIDMu.Lock()
	defer logIDMu.Unlock()
	now := time.Now().UnixMilli()
	if now < logIDLastMS {
		// 时钟回拨：沿用上次的时间戳继续消耗序列号，优先保证 id 不重复
		now = logIDLastMS
	}
	if now == logIDLastMS {
		logIDSequence++
		if logIDSequence > logIDMaxSequence {
			// 当前毫秒序列耗尽：自旋等到下一毫秒，避免 id 回绕重复
			for now <= logIDLastMS {
				time.Sleep(50 * time.Microsecond)
				now = time.Now().UnixMilli()
			}
			logIDSequence = 0
		}
	} else {
		logIDSequence = 0
	}
	logIDLastMS = now
	return now<<logIDTimestampShift | logIDMachineID<<logIDSequenceBits | logIDSequence
}
