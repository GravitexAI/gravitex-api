package model

import (
	"fmt"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// QuotaData 柱状图数据
type QuotaData struct {
	Id        int    `json:"id"`
	UserID    int    `json:"user_id" gorm:"index"`
	Username  string `json:"username" gorm:"index:idx_qdt_model_user_name,priority:2;size:64;default:''"`
	ModelName string `json:"model_name" gorm:"index:idx_qdt_model_user_name,priority:1;size:64;default:''"`
	CreatedAt int64  `json:"created_at" gorm:"bigint;index:idx_qdt_created_at,priority:2"`
	TokenUsed int    `json:"token_used" gorm:"default:0"`
	Count     int    `json:"count" gorm:"default:0"`
	Quota     int    `json:"quota" gorm:"default:0"`
}

func UpdateQuotaData() {
	for {
		if common.DataExportEnabled {
			common.SysLog("正在更新数据看板数据...")
			SaveQuotaDataCache()
		}
		time.Sleep(time.Duration(common.DataExportInterval) * time.Minute)
	}
}

var CacheQuotaData = make(map[string]*QuotaData)
var CacheQuotaDataLock = sync.Mutex{}
var quotaDataSyncLocks sync.Map

func getQuotaDataSyncLock(key string) *sync.Mutex {
	lock, _ := quotaDataSyncLocks.LoadOrStore(key, &sync.Mutex{})
	return lock.(*sync.Mutex)
}

func logQuotaDataCache(userId int, username string, modelName string, quota int, createdAt int64, tokenUsed int) {
	key := fmt.Sprintf("%d-%s-%s-%d", userId, username, modelName, createdAt)
	quotaData, ok := CacheQuotaData[key]
	if ok {
		quotaData.Count += 1
		quotaData.Quota += quota
		quotaData.TokenUsed += tokenUsed
	} else {
		quotaData = &QuotaData{
			UserID:    userId,
			Username:  username,
			ModelName: modelName,
			CreatedAt: createdAt,
			Count:     1,
			Quota:     quota,
			TokenUsed: tokenUsed,
		}
	}
	CacheQuotaData[key] = quotaData
}

func LogQuotaData(userId int, username string, modelName string, quota int, createdAt int64, tokenUsed int) {
	// 只精确到小时
	createdAt = createdAt - (createdAt % 3600)

	CacheQuotaDataLock.Lock()
	defer CacheQuotaDataLock.Unlock()
	logQuotaDataCache(userId, username, modelName, quota, createdAt, tokenUsed)
}

func SyncQuotaDataFromConsumeLogsByRequestId(requestId string) error {
	if requestId == "" {
		return nil
	}
	consumeLog := &Log{}
	err := LOG_DB.Where("request_id = ? AND type = ?", requestId, LogTypeConsume).
		Order("id desc").
		First(consumeLog).Error
	if err == gorm.ErrRecordNotFound {
		return nil
	}
	if err != nil {
		return err
	}

	createdAt := consumeLog.CreatedAt - (consumeLog.CreatedAt % 3600)
	lockKey := fmt.Sprintf("%d-%s-%s-%d", consumeLog.UserId, consumeLog.Username, consumeLog.ModelName, createdAt)
	lock := getQuotaDataSyncLock(lockKey)
	lock.Lock()
	defer lock.Unlock()

	type quotaDataAgg struct {
		Count     int
		Quota     int
		TokenUsed int
	}
	agg := quotaDataAgg{}
	err = LOG_DB.Model(&Log{}).
		Select("count(*) as count, COALESCE(sum(quota), 0) as quota, COALESCE(sum(prompt_tokens + completion_tokens), 0) as token_used").
		Where("type = ? AND user_id = ? AND username = ? AND model_name = ? AND created_at >= ? AND created_at < ?",
			LogTypeConsume, consumeLog.UserId, consumeLog.Username, consumeLog.ModelName, createdAt, createdAt+3600).
		Scan(&agg).Error
	if err != nil {
		return err
	}
	if agg.Count <= 0 {
		return nil
	}

	quotaDataDB := &QuotaData{}
	err = DB.Table("quota_data").Where("user_id = ? and username = ? and model_name = ? and created_at = ?",
		consumeLog.UserId, consumeLog.Username, consumeLog.ModelName, createdAt).First(quotaDataDB).Error
	if err == nil && quotaDataDB.Id > 0 {
		err = DB.Table("quota_data").Where("id = ?", quotaDataDB.Id).Updates(map[string]interface{}{
			"count":      agg.Count,
			"quota":      agg.Quota,
			"token_used": agg.TokenUsed,
		}).Error
		if err != nil {
			return err
		}
		common.SysLog(fmt.Sprintf("[QuotaData] synced user=%d username=%s model=%s created_at=%d count=%d quota=%d token_used=%d",
			consumeLog.UserId, consumeLog.Username, consumeLog.ModelName, createdAt, agg.Count, agg.Quota, agg.TokenUsed))
		return nil
	}
	if err != nil && err != gorm.ErrRecordNotFound {
		return err
	}

	quotaData := &QuotaData{
		UserID:    consumeLog.UserId,
		Username:  consumeLog.Username,
		ModelName: consumeLog.ModelName,
		CreatedAt: createdAt,
		TokenUsed: agg.TokenUsed,
		Count:     agg.Count,
		Quota:     agg.Quota,
	}
	if err := DB.Table("quota_data").Create(quotaData).Error; err != nil {
		return err
	}
	common.SysLog(fmt.Sprintf("[QuotaData] synced created user=%d username=%s model=%s created_at=%d count=%d quota=%d token_used=%d",
		consumeLog.UserId, consumeLog.Username, consumeLog.ModelName, createdAt, agg.Count, agg.Quota, agg.TokenUsed))
	return nil
}

func SaveQuotaDataCache() {
	CacheQuotaDataLock.Lock()
	defer CacheQuotaDataLock.Unlock()
	size := len(CacheQuotaData)
	// 如果缓存中有数据，就保存到数据库中
	// 1. 先查询数据库中是否有数据
	// 2. 如果有数据，就更新数据
	// 3. 如果没有数据，就插入数据
	for _, quotaData := range CacheQuotaData {
		quotaDataDB := &QuotaData{}
		DB.Table("quota_data").Where("user_id = ? and username = ? and model_name = ? and created_at = ?",
			quotaData.UserID, quotaData.Username, quotaData.ModelName, quotaData.CreatedAt).First(quotaDataDB)
		if quotaDataDB.Id > 0 {
			//quotaDataDB.Count += quotaData.Count
			//quotaDataDB.Quota += quotaData.Quota
			//DB.Table("quota_data").Save(quotaDataDB)
			increaseQuotaData(quotaData.UserID, quotaData.Username, quotaData.ModelName, quotaData.Count, quotaData.Quota, quotaData.CreatedAt, quotaData.TokenUsed)
		} else {
			DB.Table("quota_data").Create(quotaData)
		}
	}
	CacheQuotaData = make(map[string]*QuotaData)
	common.SysLog(fmt.Sprintf("保存数据看板数据成功，共保存%d条数据", size))
}

func increaseQuotaData(userId int, username string, modelName string, count int, quota int, createdAt int64, tokenUsed int) {
	err := DB.Table("quota_data").Where("user_id = ? and username = ? and model_name = ? and created_at = ?",
		userId, username, modelName, createdAt).Updates(map[string]interface{}{
		"count":      gorm.Expr("count + ?", count),
		"quota":      gorm.Expr("quota + ?", quota),
		"token_used": gorm.Expr("token_used + ?", tokenUsed),
	}).Error
	if err != nil {
		common.SysLog(fmt.Sprintf("increaseQuotaData error: %s", err))
	}
}

func GetQuotaDataByUsername(username string, startTime int64, endTime int64) (quotaData []*QuotaData, err error) {
	var quotaDatas []*QuotaData
	// 从quota_data表中查询数据
	err = DB.Table("quota_data").Where("username = ? and created_at >= ? and created_at <= ?", username, startTime, endTime).Find(&quotaDatas).Error
	return quotaDatas, err
}

func GetQuotaDataByUserId(userId int, startTime int64, endTime int64) (quotaData []*QuotaData, err error) {
	var quotaDatas []*QuotaData
	// 从quota_data表中查询数据
	err = DB.Table("quota_data").Where("user_id = ? and created_at >= ? and created_at <= ?", userId, startTime, endTime).Find(&quotaDatas).Error
	return quotaDatas, err
}

func GetQuotaDataGroupByUser(startTime int64, endTime int64) (quotaData []*QuotaData, err error) {
	var quotaDatas []*QuotaData
	err = DB.Table("quota_data").
		Select("username, created_at, sum(count) as count, sum(quota) as quota, sum(token_used) as token_used").
		Where("created_at >= ? and created_at <= ?", startTime, endTime).
		Group("username, created_at").
		Find(&quotaDatas).Error
	return quotaDatas, err
}

func GetAllQuotaDates(startTime int64, endTime int64, username string) (quotaData []*QuotaData, err error) {
	if username != "" {
		return GetQuotaDataByUsername(username, startTime, endTime)
	}
	var quotaDatas []*QuotaData
	// 从quota_data表中查询数据
	// only select model_name, sum(count) as count, sum(quota) as quota, model_name, created_at from quota_data group by model_name, created_at;
	//err = DB.Table("quota_data").Where("created_at >= ? and created_at <= ?", startTime, endTime).Find(&quotaDatas).Error
	err = DB.Table("quota_data").Select("model_name, sum(count) as count, sum(quota) as quota, sum(token_used) as token_used, created_at").Where("created_at >= ? and created_at <= ?", startTime, endTime).Group("model_name, created_at").Find(&quotaDatas).Error
	return quotaDatas, err
}
