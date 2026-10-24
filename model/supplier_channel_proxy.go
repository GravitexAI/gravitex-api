package model

import (
	"context"
	"fmt"
	"hash/fnv"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"gorm.io/gorm"
)

// SupplierChannelProxy is a read-only view of the proxy list managed by the Java
// admin backend. The table is owned by Java, so it is intentionally not part of
// AutoMigrate; Go only reads it, and only on MySQL deployments.
type SupplierChannelProxy struct {
	Id          int64          `json:"id"`
	Proxy       string         `json:"proxy"`
	Region      string         `json:"region"`
	PublicFlag  int            `json:"publicFlag"`
	Remark      string         `json:"remark"`
	CreatedTime int64          `json:"-"`
	UpdatedTime int64          `json:"-"`
	DeletedAt   gorm.DeletedAt `json:"-"`
}

func (SupplierChannelProxy) TableName() string {
	return "supplier_channel_proxy"
}

const (
	supplierProxyPoolTTL           = 60 * time.Second
	supplierProxyPoolQueryTimeout  = 3 * time.Second
	defaultProxyRandomRotateSecond = 300
)

type supplierProxyPoolSnapshot struct {
	items    []SupplierChannelProxy // all non-deleted rows, sorted by id
	err      error                  // last load error; items keep the previous good copy
	loadedAt time.Time
}

var (
	supplierProxyPool        atomic.Pointer[supplierProxyPoolSnapshot]
	supplierProxyPoolLoadMu  sync.Mutex
	supplierProxyWarnedSlice sync.Map // warning key -> last slice index already warned
	proxyRandomRotateSeconds = loadProxyRandomRotateSeconds()
	proxyRotationNow         = time.Now
)

func loadProxyRandomRotateSeconds() int64 {
	seconds := common.GetEnvOrDefault("PROXY_RANDOM_ROTATE_SECONDS", defaultProxyRandomRotateSecond)
	if seconds < 60 || seconds > 86400 {
		return defaultProxyRandomRotateSecond
	}
	return int64(seconds)
}

// GetSupplierChannelProxies returns every non-deleted proxy row (public and internal)
// from the 60s in-memory cache. The error is non-nil when the latest load failed;
// in that case the previous successful rows (possibly none) are still returned.
func GetSupplierChannelProxies() ([]SupplierChannelProxy, error) {
	snapshot := supplierProxyPool.Load()
	if snapshot != nil && time.Since(snapshot.loadedAt) < supplierProxyPoolTTL {
		return snapshot.items, snapshot.err
	}
	if snapshot == nil {
		// First load: every caller waits for it.
		supplierProxyPoolLoadMu.Lock()
	} else if !supplierProxyPoolLoadMu.TryLock() {
		// Another goroutine is refreshing; serve the stale copy instead of blocking relay.
		return snapshot.items, snapshot.err
	}
	defer supplierProxyPoolLoadMu.Unlock()

	if current := supplierProxyPool.Load(); current != nil && time.Since(current.loadedAt) < supplierProxyPoolTTL {
		return current.items, current.err
	}
	next := loadSupplierProxyPool(snapshot)
	supplierProxyPool.Store(next)
	return next.items, next.err
}

func loadSupplierProxyPool(previous *supplierProxyPoolSnapshot) *supplierProxyPoolSnapshot {
	if !common.UsingMainDatabase(common.DatabaseTypeMySQL) {
		return &supplierProxyPoolSnapshot{
			err:      fmt.Errorf("supplier proxy pool is only available on MySQL deployments"),
			loadedAt: time.Now(),
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), supplierProxyPoolQueryTimeout)
	defer cancel()
	var rows []SupplierChannelProxy
	err := DB.WithContext(ctx).Order("id asc").Find(&rows).Error
	if err != nil {
		common.SysError("failed to load supplier_channel_proxy pool: " + err.Error())
		next := &supplierProxyPoolSnapshot{err: fmt.Errorf("failed to load proxy pool: %w", err), loadedAt: time.Now()}
		if previous != nil {
			next.items = previous.items
		}
		return next
	}
	for i := range rows {
		rows[i].Proxy = strings.TrimSpace(rows[i].Proxy)
		rows[i].Region = strings.TrimSpace(rows[i].Region)
	}
	return &supplierProxyPoolSnapshot{items: rows, loadedAt: time.Now()}
}

// IsProxyInSupplierPool reports whether proxyURL matches any non-deleted row of
// the proxy table (public or internal). A pool load failure is returned as an error
// so that saving fails loudly instead of silently passing.
func IsProxyInSupplierPool(proxyURL string) (bool, error) {
	items, err := GetSupplierChannelProxies()
	if err != nil {
		return false, err
	}
	target := strings.TrimSpace(proxyURL)
	for _, item := range items {
		if item.Proxy == target {
			return true, nil
		}
	}
	return false, nil
}

// ResolveChannelProxy expands the "random" sentinel into the concrete proxy URL for
// the current time slice; fixed and empty proxies are returned unchanged. An empty
// random pool falls back to a direct connection with a SysLog warning.
func ResolveChannelProxy(channelId int, setting dto.ChannelSettings) string {
	proxy := strings.TrimSpace(setting.Proxy)
	if proxy != dto.ProxyRandom {
		return setting.Proxy
	}

	sliceIndex := proxyRotationNow().Unix() / proxyRandomRotateSeconds
	items, err := GetSupplierChannelProxies()
	if err != nil && len(items) == 0 {
		warnSupplierProxyOncePerSlice("load_error", sliceIndex,
			fmt.Sprintf("random proxy pool unavailable, falling back to direct connection: channel_id=%d, error=%v", channelId, err))
		return ""
	}

	allScope := setting.ProxyRandomScope == dto.ProxyRandomScopeAll
	regions := make(map[string]struct{}, len(setting.ProxyRandomRegions))
	for _, region := range setting.ProxyRandomRegions {
		regions[strings.TrimSpace(region)] = struct{}{}
	}
	candidates := make([]string, 0, len(items))
	for _, item := range items {
		if !allScope && item.PublicFlag != 1 {
			continue
		}
		if len(regions) > 0 {
			if _, ok := regions[item.Region]; !ok {
				continue
			}
		}
		if _, err := common.ParseProxyURLStrict(item.Proxy); err != nil || item.Proxy == "" {
			continue
		}
		candidates = append(candidates, item.Proxy)
	}
	if len(candidates) == 0 {
		regionList := append([]string(nil), setting.ProxyRandomRegions...)
		sort.Strings(regionList)
		warnSupplierProxyOncePerSlice(
			"empty:"+setting.ProxyRandomScope+":"+strings.Join(regionList, ","),
			sliceIndex,
			fmt.Sprintf("random proxy pool is empty after filtering, falling back to direct connection: channel_id=%d, scope=%q, regions=%v",
				channelId, setting.ProxyRandomScope, regionList),
		)
		return ""
	}

	// Offset by a per-channel hash so channels spread across the pool, then step
	// once per slice so the proxy is stable within a slice and changes between slices.
	hasher := fnv.New64a()
	_, _ = hasher.Write([]byte(strconv.Itoa(channelId)))
	index := (hasher.Sum64() + uint64(sliceIndex)) % uint64(len(candidates))
	return candidates[index]
}

func warnSupplierProxyOncePerSlice(key string, sliceIndex int64, message string) {
	if previous, loaded := supplierProxyWarnedSlice.Swap(key, sliceIndex); loaded && previous.(int64) == sliceIndex {
		return
	}
	common.SysLog(message)
}

// GetEffectiveProxy returns the proxy URL this channel should use right now, with
// the random sentinel expanded. Use GetSetting().Proxy only when the raw stored
// value is needed (cache invalidation, change detection).
func (channel *Channel) GetEffectiveProxy() string {
	return ResolveChannelProxy(channel.Id, channel.GetSetting())
}
