package model

import (
	"errors"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"

	"github.com/bytedance/gopkg/util/gopool"
	"gorm.io/gorm"
)

const (
	BatchUpdateTypeUserQuota = iota
	BatchUpdateTypeTokenQuota
	BatchUpdateTypeUsedQuota
	BatchUpdateTypeChannelUsedQuota
	BatchUpdateTypeRequestCount
	BatchUpdateTypeCount // if you add a new type, you need to add a new map and a new lock
)

var batchUpdateStores []map[int]int
var batchUpdateLocks []sync.Mutex

func init() {
	for i := 0; i < BatchUpdateTypeCount; i++ {
		batchUpdateStores = append(batchUpdateStores, make(map[int]int))
		batchUpdateLocks = append(batchUpdateLocks, sync.Mutex{})
	}
}

func InitBatchUpdater() {
	gopool.Go(func() {
		for {
			time.Sleep(time.Duration(common.BatchUpdateInterval) * time.Second)
			batchUpdate()
		}
	})
}

func addNewRecord(type_ int, id int, value int) {
	batchUpdateLocks[type_].Lock()
	defer batchUpdateLocks[type_].Unlock()
	if _, ok := batchUpdateStores[type_][id]; !ok {
		batchUpdateStores[type_][id] = value
	} else {
		batchUpdateStores[type_][id] += value
	}
}

func batchUpdate() {
	// check if there's any data to update
	hasData := false
	for i := 0; i < BatchUpdateTypeCount; i++ {
		batchUpdateLocks[i].Lock()
		if len(batchUpdateStores[i]) > 0 {
			hasData = true
			batchUpdateLocks[i].Unlock()
			break
		}
		batchUpdateLocks[i].Unlock()
	}

	if !hasData {
		return
	}

	common.SysLog("batch update started")

	// Atomically swap out all stores
	stores := make([]map[int]int, BatchUpdateTypeCount)
	for i := 0; i < BatchUpdateTypeCount; i++ {
		batchUpdateLocks[i].Lock()
		stores[i] = batchUpdateStores[i]
		batchUpdateStores[i] = make(map[int]int)
		batchUpdateLocks[i].Unlock()
	}

	// Merge user-related types (UserQuota, UsedQuota, RequestCount) into a single UPDATE per userId.
	// This reduces 3 separate UPDATEs per user to 1, cutting MySQL row lock contention by ~2/3.
	type userDelta struct {
		quotaDelta        int
		usedQuotaDelta    int
		requestCountDelta int
	}
	userDeltas := make(map[int]*userDelta)

	collectUserDelta := func(store map[int]int, field string) {
		for userId, value := range store {
			d, ok := userDeltas[userId]
			if !ok {
				d = &userDelta{}
				userDeltas[userId] = d
			}
			switch field {
			case "quota":
				d.quotaDelta += value
			case "used_quota":
				d.usedQuotaDelta += value
			case "request_count":
				d.requestCountDelta += value
			}
		}
	}

	collectUserDelta(stores[BatchUpdateTypeUserQuota], "quota")
	collectUserDelta(stores[BatchUpdateTypeUsedQuota], "used_quota")
	collectUserDelta(stores[BatchUpdateTypeRequestCount], "request_count")

	// Flush merged user updates: one UPDATE per userId
	for userId, d := range userDeltas {
		updates := map[string]interface{}{}
		if d.quotaDelta != 0 {
			updates["quota"] = gorm.Expr("quota + ?", d.quotaDelta)
		}
		if d.usedQuotaDelta != 0 {
			updates["used_quota"] = gorm.Expr("used_quota + ?", d.usedQuotaDelta)
		}
		if d.requestCountDelta != 0 {
			updates["request_count"] = gorm.Expr("request_count + ?", d.requestCountDelta)
		}
		if len(updates) == 0 {
			continue
		}
		if err := DB.Model(&User{}).Where("id = ?", userId).Updates(updates).Error; err != nil {
			common.SysLog("failed to batch update user: " + err.Error())
		}
	}

	// Flush non-user types (token quota, channel used quota) individually
	for key, value := range stores[BatchUpdateTypeTokenQuota] {
		if err := increaseTokenQuota(key, value); err != nil {
			common.SysLog("failed to batch update token quota: " + err.Error())
		}
	}
	for key, value := range stores[BatchUpdateTypeChannelUsedQuota] {
		updateChannelUsedQuota(key, value)
	}

	common.SysLog("batch update finished")
}

func RecordExist(err error) (bool, error) {
	if err == nil {
		return true, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	return false, err
}

func shouldUpdateRedis(fromDB bool, err error) bool {
	return common.RedisEnabled && fromDB && err == nil
}
