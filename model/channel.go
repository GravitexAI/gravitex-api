package model

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/samber/lo"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Channel struct {
	Id                 int     `json:"id"`
	Type               int     `json:"type" gorm:"default:0"`
	Key                string  `json:"key" gorm:"not null"`
	OpenAIOrganization *string `json:"openai_organization"`
	TestModel          *string `json:"test_model"`
	Status             int     `json:"status" gorm:"default:1"`
	Name               string  `json:"name" gorm:"index"`
	Weight             *uint   `json:"weight" gorm:"default:0"`
	CreatedTime        int64   `json:"created_time" gorm:"bigint"`
	TestTime           int64   `json:"test_time" gorm:"bigint"`
	ResponseTime       int     `json:"response_time"` // in milliseconds
	BaseURL            *string `json:"base_url" gorm:"column:base_url;default:''"`
	Other              string  `json:"other"`
	Balance            float64 `json:"balance"` // in USD
	BalanceUpdatedTime int64   `json:"balance_updated_time" gorm:"bigint"`
	Models             string  `json:"models"`
	Group              string  `json:"group" gorm:"type:varchar(256);default:'default'"`
	UsedQuota          int64   `json:"used_quota" gorm:"bigint;default:0"`
	ModelMapping       *string `json:"model_mapping" gorm:"type:text"`
	//MaxInputTokens     *int    `json:"max_input_tokens" gorm:"default:0"`
	StatusCodeMapping *string  `json:"status_code_mapping" gorm:"type:varchar(1024);default:''"`
	Priority          *int64   `json:"priority" gorm:"bigint;default:0"`
	AutoBan           *int     `json:"auto_ban" gorm:"default:1"`
	OtherInfo         string   `json:"other_info"`
	Tag               *string  `json:"tag" gorm:"index"`
	Setting           *string  `json:"setting" gorm:"type:text"` // 渠道额外设置
	ParamOverride     *string  `json:"param_override" gorm:"type:text"`
	HeaderOverride    *string  `json:"header_override" gorm:"type:text"`
	Remark            *string  `json:"remark" gorm:"type:varchar(255)" validate:"max=255"`
	CostDiscount      *float64 `json:"cost_discount" gorm:"type:decimal(4,3);default:null"`
	// add after v0.8.5
	ChannelInfo ChannelInfo `json:"channel_info" gorm:"type:json"`

	OtherSettings string `json:"settings" gorm:"column:settings"` // 其他设置，存储azure版本等不需要检索的信息，详见dto.ChannelOtherSettings

	// cache info
	Keys []string `json:"-" gorm:"-"`
	// BalanceDetail is the last balance query in channel responses; see
	// GetBalanceDetail.
	BalanceDetail *ChannelBalanceDetail `json:"balance_detail,omitempty" gorm:"-"`
}

const ChannelStatusReasonAllKeysDisabled = "All keys are disabled"

type ChannelInfo struct {
	IsMultiKey             bool                  `json:"is_multi_key"`                        // 是否多Key模式
	MultiKeySize           int                   `json:"multi_key_size"`                      // 多Key模式下的Key数量
	MultiKeyStatusList     map[int]int           `json:"multi_key_status_list"`               // key状态列表，key index -> status
	MultiKeyDisabledReason map[int]string        `json:"multi_key_disabled_reason,omitempty"` // key禁用原因列表，key index -> reason
	MultiKeyDisabledTime   map[int]int64         `json:"multi_key_disabled_time,omitempty"`   // key禁用时间列表，key index -> time
	MultiKeyPollingIndex   int                   `json:"multi_key_polling_index"`             // 多Key模式下轮询的key索引
	MultiKeyMode           constant.MultiKeyMode `json:"multi_key_mode"`
}

type ChannelSortOptions struct {
	SortBy    string
	SortOrder string
	IDSort    bool
}

var channelSortColumns = map[string]string{
	"id":            "id",
	"name":          "name",
	"priority":      "priority",
	"balance":       "balance",
	"response_time": "response_time",
	"test_time":     "test_time",
}

func NewChannelSortOptions(sortBy string, sortOrder string, idSort bool) ChannelSortOptions {
	normalizedSortBy := strings.ToLower(strings.TrimSpace(sortBy))
	normalizedSortOrder := strings.ToLower(strings.TrimSpace(sortOrder))
	if _, ok := channelSortColumns[normalizedSortBy]; !ok {
		normalizedSortBy = ""
		normalizedSortOrder = ""
	} else if normalizedSortOrder != "asc" {
		normalizedSortOrder = "desc"
	}

	return ChannelSortOptions{
		SortBy:    normalizedSortBy,
		SortOrder: normalizedSortOrder,
		IDSort:    idSort,
	}
}

func (options ChannelSortOptions) Apply(query *gorm.DB) *gorm.DB {
	if columnName, ok := channelSortColumns[options.SortBy]; ok {
		column := clause.Column{Name: columnName}
		if columnName == "priority" {
			// Routing treats a missing priority as zero on every database.
			column = clause.Column{Name: "COALESCE(priority, 0)", Raw: true}
		}
		return query.Order(clause.OrderByColumn{
			Column: column,
			Desc:   options.SortOrder != "asc",
		}).Order(clause.OrderByColumn{Column: clause.Column{Name: "id"}, Desc: true})
	}
	if options.IDSort {
		return query.Order(clause.OrderByColumn{
			Column: clause.Column{Name: "id"},
			Desc:   true,
		})
	}
	return query.Order(clause.OrderByColumn{
		Column: clause.Column{Name: "COALESCE(priority, 0)", Raw: true},
		Desc:   true,
	}).Order(clause.OrderByColumn{Column: clause.Column{Name: "id"}, Desc: true})
}

func resolveChannelSortOptions(idSort bool, sortOptions []ChannelSortOptions) ChannelSortOptions {
	if len(sortOptions) == 0 {
		return NewChannelSortOptions("", "", idSort)
	}
	options := sortOptions[0]
	options.IDSort = options.IDSort || idSort
	return options
}

func NormalizeChannelGroupFilter(group string) string {
	group = strings.TrimSpace(group)
	if group == "" || strings.EqualFold(group, "all") || strings.EqualFold(group, "null") {
		return ""
	}
	return group
}

func channelGroupFilterCondition() string {
	if common.UsingMainDatabase(common.DatabaseTypeMySQL) {
		return `CONCAT(',', ` + commonGroupCol + `, ',') LIKE ? ESCAPE '!'`
	}
	return `(',' || ` + commonGroupCol + ` || ',') LIKE ? ESCAPE '!'`
}

func channelGroupFilterPattern(group string) string {
	group = strings.NewReplacer(
		"!", "!!",
		"%", "!%",
		"_", "!_",
	).Replace(group)
	return "%," + group + ",%"
}

func ApplyChannelGroupFilter(query *gorm.DB, group string) *gorm.DB {
	group = NormalizeChannelGroupFilter(group)
	if group == "" {
		return query
	}
	return query.Where(channelGroupFilterCondition(), channelGroupFilterPattern(group))
}

// Value implements driver.Valuer interface
func (c ChannelInfo) Value() (driver.Value, error) {
	return common.Marshal(&c)
}

// Scan implements sql.Scanner interface
func (c *ChannelInfo) Scan(value interface{}) error {
	// 不同驱动/协议模式下 json 列可能以 []byte 或 string 返回，静默丢弃 string 会把字段清零。
	var bytesValue []byte
	switch v := value.(type) {
	case []byte:
		bytesValue = v
	case string:
		bytesValue = []byte(v)
	}
	return common.Unmarshal(bytesValue, c)
}

func (channel *Channel) GetKeys() []string {
	if channel.Key == "" {
		return []string{}
	}
	if len(channel.Keys) > 0 {
		return channel.Keys
	}
	trimmed := strings.TrimSpace(channel.Key)
	// If the key starts with '[', try to parse it as a JSON array (e.g., for Vertex AI scenarios)
	if strings.HasPrefix(trimmed, "[") {
		var arr []json.RawMessage
		if err := common.Unmarshal([]byte(trimmed), &arr); err == nil {
			res := make([]string, len(arr))
			for i, v := range arr {
				res[i] = string(v)
			}
			return res
		}
	}
	// Otherwise, fall back to splitting by newline
	keys := strings.Split(strings.Trim(channel.Key, "\n"), "\n")
	return keys
}

// FindKeyByProjectID 根据 project_id 从多 key 列表中找到匹配的 key。
// 用于 Vertex AI 视频任务轮询时，根据 taskID 中的 project 匹配正确的 service account。
// 如果找不到匹配的 key，返回第一个 key 作为兜底。
func (channel *Channel) FindKeyByProjectID(projectID string) string {
	keys := channel.GetKeys()
	if len(keys) == 0 {
		return channel.Key
	}
	if len(keys) == 1 || projectID == "" {
		return keys[0]
	}
	for _, key := range keys {
		var cred struct {
			ProjectID string `json:"project_id"`
		}
		if err := json.Unmarshal([]byte(key), &cred); err == nil && cred.ProjectID == projectID {
			return key
		}
	}
	return keys[0]
}

func (channel *Channel) GetNextEnabledKey() (string, int, *types.NewAPIError) {
	// If not in multi-key mode, return the original key string directly.
	if !channel.ChannelInfo.IsMultiKey {
		return channel.Key, 0, nil
	}

	// Obtain all keys (split by \n)
	keys := channel.GetKeys()
	if len(keys) == 0 {
		// No keys available, return error, should disable the channel
		return "", 0, types.NewError(errors.New("no keys available"), types.ErrorCodeChannelNoAvailableKey)
	}

	lock := GetChannelPollingLock(channel.Id)
	lock.Lock()
	defer lock.Unlock()

	statusList := channel.ChannelInfo.MultiKeyStatusList
	// helper to get key status, default to enabled when missing
	getStatus := func(idx int) int {
		if statusList == nil {
			return common.ChannelStatusEnabled
		}
		if status, ok := statusList[idx]; ok {
			return status
		}
		return common.ChannelStatusEnabled
	}

	// Collect indexes of enabled keys
	enabledIdx := make([]int, 0, len(keys))
	for i := range keys {
		if getStatus(i) == common.ChannelStatusEnabled {
			enabledIdx = append(enabledIdx, i)
		}
	}
	// If no specific status list or none enabled, return an explicit error so caller can
	// properly handle a channel with no available keys (e.g. mark channel disabled).
	// Returning the first key here caused requests to keep using an already-disabled key.
	if len(enabledIdx) == 0 {
		return "", 0, types.NewError(errors.New("no enabled keys"), types.ErrorCodeChannelNoAvailableKey)
	}

	switch channel.ChannelInfo.MultiKeyMode {
	case constant.MultiKeyModeRandom:
		// Randomly pick one enabled key
		selectedIdx := enabledIdx[rand.Intn(len(enabledIdx))]
		return keys[selectedIdx], selectedIdx, nil
	case constant.MultiKeyModePolling:
		// Use channel-specific lock to ensure thread-safe polling

		channelInfo, err := CacheGetChannelInfo(channel.Id)
		if err != nil {
			return "", 0, types.NewError(err, types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
		}
		defer func() {
			if common.DebugEnabled {
				logger.LogDebug(nil, "channel %d polling index: %d", channel.Id, channel.ChannelInfo.MultiKeyPollingIndex)
			}
			if !common.MemoryCacheEnabled {
				_ = channel.SaveChannelInfo()
			} else {
				// CacheUpdateChannel(channel)
			}
		}()
		// Start from the saved polling index and look for the next enabled key
		start := channelInfo.MultiKeyPollingIndex
		if start < 0 || start >= len(keys) {
			start = 0
		}
		for i := 0; i < len(keys); i++ {
			idx := (start + i) % len(keys)
			if getStatus(idx) == common.ChannelStatusEnabled {
				// update polling index for next call (point to the next position)
				channel.ChannelInfo.MultiKeyPollingIndex = (idx + 1) % len(keys)
				return keys[idx], idx, nil
			}
		}
		// Fallback – should not happen, but return first enabled key
		return keys[enabledIdx[0]], enabledIdx[0], nil
	default:
		// Unknown mode, default to first enabled key (or original key string)
		return keys[enabledIdx[0]], enabledIdx[0], nil
	}
}

func (channel *Channel) SaveChannelInfo() error {
	return DB.Model(channel).Update("channel_info", channel.ChannelInfo).Error
}

func (channel *Channel) GetModels() []string {
	if channel.Models == "" {
		return []string{}
	}
	return strings.Split(strings.Trim(channel.Models, ","), ",")
}

func (channel *Channel) GetGroups() []string {
	if channel.Group == "" {
		return []string{}
	}
	groups := strings.Split(strings.Trim(channel.Group, ","), ",")
	for i, group := range groups {
		groups[i] = strings.TrimSpace(group)
	}
	return groups
}

func (channel *Channel) GetOtherInfo() map[string]interface{} {
	otherInfo := make(map[string]interface{})
	if channel.OtherInfo != "" {
		err := common.Unmarshal([]byte(channel.OtherInfo), &otherInfo)
		if err != nil {
			common.SysLog(common.LogText("failed to unmarshal other info: channel_id=%d, tag=%s, name=%s, error=%v", channel.Id, channel.GetTag(), channel.Name, err))
		}
	}
	return otherInfo
}

func (channel *Channel) SetOtherInfo(otherInfo map[string]interface{}) {
	otherInfoBytes, err := json.Marshal(otherInfo)
	if err != nil {
		common.SysLog(common.LogText("failed to marshal other info: channel_id=%d, tag=%s, name=%s, error=%v", channel.Id, channel.GetTag(), channel.Name, err))
		return
	}
	channel.OtherInfo = string(otherInfoBytes)
}

func (channel *Channel) GetTag() string {
	if channel.Tag == nil {
		return ""
	}
	return *channel.Tag
}

func (channel *Channel) SetTag(tag string) {
	channel.Tag = &tag
}

func (channel *Channel) GetAutoBan() bool {
	if channel.AutoBan == nil {
		return false
	}
	return *channel.AutoBan == 1
}

// ErrChannelOtherInfoBusy reports a channel whose other_info other writers
// kept changing through every attempt of UpdateChannelOtherInfo.
var ErrChannelOtherInfoBusy = errors.New("channel other_info kept changing during the update")

// saveStatusState persists only the fields owned by the channel status flow.
// Keeping this allowlist here prevents a stale channel snapshot from
// overwriting credentials, accounting counters, or channel configuration.
func (channel *Channel) saveStatusState(tx *gorm.DB) error {
	if channel.Id == 0 {
		return errors.New("channel ID is 0")
	}
	if tx == nil {
		return errors.New("status update transaction is nil")
	}
	updates := map[string]any{
		"status":     channel.Status,
		"other_info": channel.OtherInfo,
	}
	if channel.ChannelInfo.IsMultiKey {
		updates["channel_info"] = channel.ChannelInfo
	}
	result := tx.Model(&Channel{}).Where("id = ? and status != ?", channel.Id, common.ChannelStatusDeleted).Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		var current Channel
		if err := tx.Select("status").First(&current, "id = ?", channel.Id).Error; err != nil {
			return err
		}
		if current.Status == common.ChannelStatusDeleted {
			return gorm.ErrRecordNotFound
		}
	}
	return nil
}

// ChannelTable returns the fully qualified channels table name based on region.
// Empty region uses the default "channels" table in the current database.
// Non-empty region (e.g. "gravitex_api2") returns "gravitex_api2.channels".
func ChannelTable(region string) string {
	if region != "" {
		return region + ".channels"
	}
	return "channels"
}

const channelOtherInfoAttempts = 5

// UpdateChannelOtherInfo runs update on the stored channel and writes the
// other_info it leaves, with the columns update returns, only while the
// stored other_info is still the one read. When another writer, on any node,
// changed it in between, the channel is read again and update runs on that,
// so neither change is lost: update may run more than once and must not wait
// on the network. It returns nil to write nothing, and written reports a
// write. Only the returned columns and other_info are written, so a stale
// read never puts back the key, counters or settings. On SQLite, which serves
// one node, the channel's key lock also keeps the locked updates of this
// process out.
func UpdateChannelOtherInfo(channelID int, update func(channel *Channel) (map[string]any, error)) (written bool, err error) {
	if common.UsingMainDatabase(common.DatabaseTypeSQLite) {
		lock := sqliteChannelKeyLock(channelID)
		lock.Lock()
		defer lock.Unlock()
	}
	for range channelOtherInfoAttempts {
		var channel Channel
		if err := DB.First(&channel, "id = ?", channelID).Error; err != nil {
			return false, err
		}
		read := channel.OtherInfo
		updates, err := update(&channel)
		if err != nil || updates == nil {
			return false, err
		}
		updates["other_info"] = channel.OtherInfo
		query := DB.Model(&Channel{}).Where("id = ?", channelID)
		if read == "" {
			query = query.Where("(other_info IS NULL OR other_info = ?)", "")
		} else {
			query = query.Where("other_info = ?", read)
		}
		result := query.Updates(updates)
		if result.Error != nil {
			return false, result.Error
		}
		if result.RowsAffected > 0 {
			return true, nil
		}
		// MySQL counts changed rows only: when other_info is still the one
		// read, the row matched and already held every value written.
		if common.UsingMainDatabase(common.DatabaseTypeMySQL) && channel.OtherInfo == read {
			var stored Channel
			if err := DB.Select("other_info").First(&stored, "id = ?", channelID).Error; err != nil {
				return false, err
			}
			if stored.OtherInfo == read {
				return true, nil
			}
		}
	}
	return false, ErrChannelOtherInfoBusy
}

func GetAllChannels(startIdx int, num int, selectAll bool, idSort bool, sortOptions ...ChannelSortOptions) ([]*Channel, error) {
	var channels []*Channel
	var err error
	order := resolveChannelSortOptions(idSort, sortOptions)
	if selectAll {
		err = order.Apply(DB).Find(&channels).Error
	} else {
		err = order.Apply(DB).Limit(num).Offset(startIdx).Omit("key").Find(&channels).Error
	}
	return channels, err
}

func GetChannelsByTag(tag string, idSort bool, selectAll bool, region string) ([]*Channel, error) {
	var channels []*Channel
	order := "priority desc"
	if idSort {
		order = "id desc"
	}
	query := DB.Table(ChannelTable(region)).Where("tag = ?", tag).Order(order)
	if !selectAll {
		query = query.Omit("key")
	}
	err := query.Find(&channels).Error
	return channels, err
}

func SearchChannels(keyword string, group string, model string, idSort bool, region string) ([]*Channel, error) {
	var channels []*Channel
	modelsCol := "`models`"

	// 如果是 PostgreSQL，使用双引号
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		modelsCol = `"models"`
	}

	baseURLCol := "`base_url`"
	// 如果是 PostgreSQL，使用双引号
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		baseURLCol = `"base_url"`
	}

	orderStr := "priority desc"
	if idSort {
		orderStr = "id desc"
	}

	// 构造基础查询
	baseQuery := DB.Table(ChannelTable(region)).Omit("key")

	// 构造WHERE子句
	whereClause := "(id = ? OR name LIKE ? OR " + commonKeyCol + " = ? OR " + baseURLCol + " LIKE ?) AND " + modelsCol + " LIKE ?"
	args := []any{common.String2Int(keyword), "%" + keyword + "%", keyword, "%" + keyword + "%", "%" + model + "%"}
	baseQuery = ApplyChannelGroupFilter(baseQuery.Where(whereClause, args...), group)

	// 执行查询
	err := baseQuery.Order(orderStr).Find(&channels).Error
	if err != nil {
		return nil, err
	}
	return channels, nil
}

func GetChannelById(id int, selectAll bool) (*Channel, error) {
	channel := &Channel{Id: id}
	var err error = nil
	if selectAll {
		err = DB.First(channel, "id = ?", id).Error
	} else {
		err = DB.Omit("key").First(channel, "id = ?", id).Error
	}
	if err != nil {
		return nil, err
	}
	return channel, nil
}

func BatchInsertChannels(channels []Channel) error {
	if len(channels) == 0 {
		return nil
	}
	unlockTagMutation := LockChannelTagMutation()
	defer unlockTagMutation()
	tx := DB.Begin()
	if tx.Error != nil {
		return tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
			panic(r)
		}
	}()

	// Keep each chunk backed by channels so generated IDs reach the caller.
	for chunk := range slices.Chunk(channels, 50) {
		if err := tx.Create(&chunk).Error; err != nil {
			tx.Rollback()
			return err
		}
		for _, channel_ := range chunk {
			if err := channel_.AddAbilities(tx); err != nil {
				tx.Rollback()
				return err
			}
		}
	}
	if err := tx.Commit().Error; err != nil {
		return err
	}
	BumpSyncTopic(SyncTopicChannels)
	return nil
}

func BatchDeleteChannels(ids []int) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	unlock := lockChannelPollingLocks(ids)
	defer unlock()
	// Keep channel and ability state changes atomic. Abilities are retained for
	// audit/configuration history but disabled so they cannot route traffic.
	tx := DB.Begin()
	if tx.Error != nil {
		return 0, tx.Error
	}
	var deletedCount int64
	for _, chunk := range lo.Chunk(ids, 200) {
		result := tx.Model(&Channel{}).Where("id in (?) and status != ?", chunk, common.ChannelStatusDeleted).
			Update("status", common.ChannelStatusDeleted)
		if result.Error != nil {
			tx.Rollback()
			return 0, result.Error
		}
		deletedCount += result.RowsAffected
		if err := tx.Model(&Ability{}).Where("channel_id in (?)", chunk).Update("enabled", false).Error; err != nil {
			tx.Rollback()
			return 0, err
		}
	}
	if err := tx.Commit().Error; err != nil {
		return 0, err
	}
	BumpSyncTopic(SyncTopicChannels)
	return deletedCount, nil
}

func (channel *Channel) GetPriority() int64 {
	if channel.Priority == nil {
		return 0
	}
	return *channel.Priority
}

func (channel *Channel) GetWeight() int {
	if channel.Weight == nil {
		return 0
	}
	return int(*channel.Weight)
}

func (channel *Channel) GetBaseURL() string {
	if channel.BaseURL == nil {
		return ""
	}
	url := *channel.BaseURL
	if url == "" {
		url = constant.ChannelBaseURLs[channel.Type]
	}
	return url
}

func (channel *Channel) GetModelMapping() string {
	if channel.ModelMapping == nil {
		return ""
	}
	return *channel.ModelMapping
}

func (channel *Channel) GetStatusCodeMapping() string {
	if channel.StatusCodeMapping == nil {
		return ""
	}
	return *channel.StatusCodeMapping
}

func (channel *Channel) Insert() error {
	unlockTagMutation := LockChannelTagMutation()
	defer unlockTagMutation()
	var err error
	err = DB.Create(channel).Error
	if err != nil {
		return err
	}
	defer BumpSyncTopic(SyncTopicChannels)
	err = channel.AddAbilities(nil)
	return err
}

func (channel *Channel) Update() error {
	// If this is a multi-key channel, recalculate MultiKeySize based on the current key list to avoid inconsistency after editing keys
	if channel.ChannelInfo.IsMultiKey {
		var keyStr string
		if channel.Key != "" {
			keyStr = channel.Key
		} else {
			// If key is not provided, read the existing key from the database
			if existing, err := GetChannelById(channel.Id, true); err == nil {
				keyStr = existing.Key
			}
		}
		// Parse the key list (supports newline separation or JSON array)
		keys := []string{}
		if keyStr != "" {
			trimmed := strings.TrimSpace(keyStr)
			if strings.HasPrefix(trimmed, "[") {
				var arr []json.RawMessage
				if err := common.Unmarshal([]byte(trimmed), &arr); err == nil {
					keys = make([]string, len(arr))
					for i, v := range arr {
						keys[i] = string(v)
					}
				}
			}
			if len(keys) == 0 { // fallback to newline split
				keys = strings.Split(strings.Trim(keyStr, "\n"), "\n")
			}
		}
		channel.ChannelInfo.MultiKeySize = len(keys)
		// Clean up status data that exceeds the new key count to prevent index out of range
		if channel.ChannelInfo.MultiKeyStatusList != nil {
			for idx := range channel.ChannelInfo.MultiKeyStatusList {
				if idx >= channel.ChannelInfo.MultiKeySize {
					delete(channel.ChannelInfo.MultiKeyStatusList, idx)
				}
			}
		}
	}
	tx := DB.Begin()
	if tx.Error != nil {
		return tx.Error
	}
	result := tx.Model(&Channel{}).Where("id = ? and status != ?", channel.Id, common.ChannelStatusDeleted).Updates(channel)
	if result.Error != nil {
		tx.Rollback()
		return result.Error
	}
	if result.RowsAffected == 0 {
		var current Channel
		if err := tx.Select("status").First(&current, "id = ?", channel.Id).Error; err != nil {
			tx.Rollback()
			return err
		}
		if current.Status == common.ChannelStatusDeleted {
			tx.Rollback()
			return gorm.ErrRecordNotFound
		}
	}
	if err := tx.First(channel, "id = ?", channel.Id).Error; err != nil {
		tx.Rollback()
		return err
	}
	if err := channel.UpdateAbilities(tx); err != nil {
		tx.Rollback()
		return err
	}
	if err := tx.Commit().Error; err != nil {
		return err
	}
	BumpSyncTopic(SyncTopicChannels)
	return nil
}

func (channel *Channel) UpdateResponseTime(responseTime int64) {
	err := DB.Model(channel).Select("response_time", "test_time").Updates(Channel{
		TestTime:     common.GetTimestamp(),
		ResponseTime: int(responseTime),
	}).Error
	if err != nil {
		common.SysLog(common.LogText("failed to update response time: channel_id=%d, error=%v", channel.Id, err))
	}
}

func (channel *Channel) UpdateBalance(balance float64) {
	err := DB.Model(channel).Select("balance_updated_time", "balance").Updates(Channel{
		BalanceUpdatedTime: common.GetTimestamp(),
		Balance:            balance,
	}).Error
	if err != nil {
		common.SysLog(common.LogText("failed to update balance: channel_id=%d, error=%v", channel.Id, err))
	}
}

func (channel *Channel) Delete() error {
	lock := GetChannelPollingLock(channel.Id)
	lock.Lock()
	defer lock.Unlock()

	tx := DB.Begin()
	if tx.Error != nil {
		return tx.Error
	}
	result := tx.Model(&Channel{}).Where("id = ? and status != ?", channel.Id, common.ChannelStatusDeleted).
		Update("status", common.ChannelStatusDeleted)
	if result.Error != nil {
		tx.Rollback()
		return result.Error
	}
	if err := tx.Model(&Ability{}).Where("channel_id = ?", channel.Id).Update("enabled", false).Error; err != nil {
		tx.Rollback()
		return err
	}
	if err := tx.Commit().Error; err != nil {
		return err
	}
	BumpSyncTopic(SyncTopicChannels)
	return nil
}

func lockChannelPollingLocks(channelIds []int) func() {
	ids := append([]int(nil), channelIds...)
	sort.Ints(ids)
	uniqueIds := ids[:0]
	for _, id := range ids {
		if len(uniqueIds) == 0 || uniqueIds[len(uniqueIds)-1] != id {
			uniqueIds = append(uniqueIds, id)
		}
	}
	locks := make([]*sync.Mutex, 0, len(uniqueIds))
	for _, id := range uniqueIds {
		lock := GetChannelPollingLock(id)
		lock.Lock()
		locks = append(locks, lock)
	}
	return func() {
		for i := len(locks) - 1; i >= 0; i-- {
			locks[i].Unlock()
		}
	}
}

// channelPollingLocks stores locks for each channel.id to ensure thread-safe polling
var channelPollingLocks sync.Map

// channelTagMutationLock keeps tag-based status updates consistent with
// channel insertions and edits that can change tag membership in this process.
var channelTagMutationLock sync.Mutex

func LockChannelTagMutation() func() {
	channelTagMutationLock.Lock()
	return channelTagMutationLock.Unlock
}

// GetChannelPollingLock returns or creates a mutex for the given channel ID
func GetChannelPollingLock(channelId int) *sync.Mutex {
	if lock, exists := channelPollingLocks.Load(channelId); exists {
		return lock.(*sync.Mutex)
	}
	// Create new lock for this channel
	newLock := &sync.Mutex{}
	actual, _ := channelPollingLocks.LoadOrStore(channelId, newLock)
	return actual.(*sync.Mutex)
}

// CleanupChannelPollingLocks removes locks for channels that no longer exist
// This is optional and can be called periodically to prevent memory leaks
func CleanupChannelPollingLocks() {
	var activeChannelIds []int
	DB.Model(&Channel{}).Pluck("id", &activeChannelIds)

	activeChannelSet := make(map[int]bool)
	for _, id := range activeChannelIds {
		activeChannelSet[id] = true
	}

	channelPollingLocks.Range(func(key, value interface{}) bool {
		channelId := key.(int)
		if !activeChannelSet[channelId] {
			channelPollingLocks.Delete(channelId)
		}
		return true
	})
}

// sqlitePluginRefreshLocks serialize the credential refreshes of a channel on
// SQLite, which serves one node: channel id -> chan struct{} of capacity one.
var sqlitePluginRefreshLocks sync.Map

// pluginRefreshLockSpace is the first key of the PostgreSQL advisory locks of
// credential refreshes; the channel id is the second.
const pluginRefreshLockSpace = 0x6e617069

// WithPluginCredentialRefreshLock runs refresh while no other node or
// goroutine refreshes a credential of the channel, so a refresh token goes out
// once even when several nodes need a fresh credential at the same time. MySQL
// holds a named lock and PostgreSQL a transaction advisory lock, each on a
// connection of its own, and SQLite, which serves one node, a process lock.
// None of them locks the channel row: no writer of the channel waits for the
// network call refresh makes. ctx bounds the wait for the lock only.
func WithPluginCredentialRefreshLock(ctx context.Context, channelID int, refresh func() error) error {
	switch {
	case common.UsingMainDatabase(common.DatabaseTypeMySQL):
		name := fmt.Sprintf("new-api:plugin-refresh:%d", channelID)
		wait := 0
		if deadline, ok := ctx.Deadline(); ok {
			wait = max(int(time.Until(deadline)/time.Second), 0)
		}
		return DB.WithContext(ctx).Connection(func(conn *gorm.DB) error {
			var acquired sql.NullInt64
			if err := conn.Raw("SELECT GET_LOCK(?, ?)", name, wait).Row().Scan(&acquired); err != nil {
				return err
			}
			if acquired.Int64 != 1 {
				return ErrPluginRefreshLockTimeout
			}
			// The lock belongs to this connection until released, whatever
			// happened to ctx meanwhile.
			defer conn.WithContext(context.Background()).Exec("DO RELEASE_LOCK(?)", name)
			return refresh()
		})
	case common.UsingMainDatabase(common.DatabaseTypePostgreSQL):
		// The commit releases the lock; it must not fail because ctx ended
		// while refresh ran.
		return DB.Transaction(func(tx *gorm.DB) error {
			if err := tx.WithContext(ctx).Exec("SELECT pg_advisory_xact_lock(?, ?)", pluginRefreshLockSpace, channelID).Error; err != nil {
				return err
			}
			return refresh()
		})
	default:
		value, _ := sqlitePluginRefreshLocks.LoadOrStore(channelID, make(chan struct{}, 1))
		lock := value.(chan struct{})
		select {
		case lock <- struct{}{}:
		case <-ctx.Done():
			return ErrPluginRefreshLockTimeout
		}
		defer func() { <-lock }()
		return refresh()
	}
}

// ErrPluginRefreshLockTimeout reports a credential refresh that waited for
// another refresh of the channel longer than it may.
var ErrPluginRefreshLockTimeout = errors.New("timed out waiting for another credential refresh of the channel")

func handlerMultiKeyUpdate(channel *Channel, usingKey string, status int, reason string) {
	keys := channel.GetKeys()
	if len(keys) == 0 {
		channel.Status = status
	} else {
		keyIndex := -1
		for i, key := range keys {
			if key == usingKey {
				keyIndex = i
				break
			}
		}
		if keyIndex < 0 {
			if usingKey != "" {
				common.SysLog(common.LogText("failed to update multi-key status: channel_id=%d, using key not found", channel.Id))
				return
			}
			channel.Status = status
			info := channel.GetOtherInfo()
			info["status_reason"] = reason
			info["status_time"] = common.GetTimestamp()
			channel.SetOtherInfo(info)
			return
		}
		if channel.ChannelInfo.MultiKeyStatusList == nil {
			channel.ChannelInfo.MultiKeyStatusList = make(map[int]int)
		}
		if status == common.ChannelStatusEnabled {
			delete(channel.ChannelInfo.MultiKeyStatusList, keyIndex)
		} else {
			channel.ChannelInfo.MultiKeyStatusList[keyIndex] = status
			if channel.ChannelInfo.MultiKeyDisabledReason == nil {
				channel.ChannelInfo.MultiKeyDisabledReason = make(map[int]string)
			}
			if channel.ChannelInfo.MultiKeyDisabledTime == nil {
				channel.ChannelInfo.MultiKeyDisabledTime = make(map[int]int64)
			}
			channel.ChannelInfo.MultiKeyDisabledReason[keyIndex] = reason
			channel.ChannelInfo.MultiKeyDisabledTime[keyIndex] = common.GetTimestamp()
		}
		if !hasEnabledMultiKey(keys, channel.ChannelInfo.MultiKeyStatusList) {
			channel.Status = common.ChannelStatusAutoDisabled
			info := channel.GetOtherInfo()
			info["status_reason"] = ChannelStatusReasonAllKeysDisabled
			info["status_time"] = common.GetTimestamp()
			channel.SetOtherInfo(info)
		} else if status == common.ChannelStatusEnabled {
			channel.Status = common.ChannelStatusEnabled
		}
	}
}

func hasEnabledMultiKey(keys []string, statusList map[int]int) bool {
	for i := range keys {
		if statusList == nil {
			return true
		}
		status, ok := statusList[i]
		if !ok || status == common.ChannelStatusEnabled {
			return true
		}
	}
	return false
}

func UpdateChannelStatus(channelId int, usingKey string, status int, reason string) bool {
	// ChannelInfo stores both multi-key status and the polling cursor. Hold the
	// same per-channel lock from the first read through persistence so neither
	// writer can save a stale JSON snapshot over the other.
	pollingLock := GetChannelPollingLock(channelId)
	pollingLock.Lock()
	defer pollingLock.Unlock()
	channel, err := GetChannelById(channelId, true)
	if err != nil || channel == nil || channel.Status == common.ChannelStatusDeleted {
		return false
	}

	if common.MemoryCacheEnabled {
		channelCache, _ := CacheGetChannel(channelId)
		if channelCache == nil {
			return false
		}
		// 官方 #392：请求失败不能覆盖管理员的手动禁用，否则之后可能被自动恢复重新启用
		if status == common.ChannelStatusAutoDisabled && channelCache.Status == common.ChannelStatusManuallyDisabled {
			return false
		}
	}

	shouldUpdateAbilities := false
	// 官方：多 key 渠道因所有 key 不可用被自动禁用后，管理员手动禁用需覆盖该原因（即使状态值相同），
	// 避免后续被当作 key 耗尽自动恢复
	overridesKeyExhaustion := channel.ChannelInfo.IsMultiKey && usingKey == "" &&
		status == common.ChannelStatusManuallyDisabled && reason != ChannelStatusReasonAllKeysDisabled &&
		channel.GetOtherInfo()["status_reason"] == ChannelStatusReasonAllKeysDisabled
	if channel.Status == status && !overridesKeyExhaustion {
		return false
	}
	// 官方 #392：本节点缓存可能过期，再按数据库行检查一次手动禁用
	if status == common.ChannelStatusAutoDisabled && channel.Status == common.ChannelStatusManuallyDisabled {
		return false
	}

	if channel.ChannelInfo.IsMultiKey {
		beforeStatus := channel.Status
		handlerMultiKeyUpdate(channel, usingKey, status, reason)
		if beforeStatus != channel.Status {
			shouldUpdateAbilities = true
		}
	} else {
		info := channel.GetOtherInfo()
		info["status_reason"] = reason
		info["status_time"] = common.GetTimestamp()
		channel.SetOtherInfo(info)
		channel.Status = status
		shouldUpdateAbilities = true
	}
	tx := DB.Begin()
	if tx.Error != nil {
		common.SysLog(fmt.Sprintf("failed to begin channel status update: channel_id=%d, error=%v", channel.Id, tx.Error))
		return false
	}
	err = channel.saveStatusState(tx)
	if err != nil {
		tx.Rollback()
		common.SysLog(fmt.Sprintf("failed to update channel status: channel_id=%d, status=%d, error=%v", channel.Id, status, err))
		return false
	}
	if shouldUpdateAbilities {
		if err := updateAbilityStatusForActiveChannels(
			tx.Model(&Ability{}).Where("channel_id = ?", channelId),
			tx.Model(&Channel{}).Where("id = ?", channelId),
			channel.Status == common.ChannelStatusEnabled,
		); err != nil {
			tx.Rollback()
			common.SysLog(fmt.Sprintf("failed to update ability status: channel_id=%d, error=%v", channelId, err))
			return false
		}
	}
	if err := tx.Commit().Error; err != nil {
		common.SysLog(fmt.Sprintf("failed to commit channel status update: channel_id=%d, error=%v", channelId, err))
		return false
	}
	if common.MemoryCacheEnabled {
		CacheUpdateChannelStatus(channelId, channel.Status)
		CacheUpdateChannel(channel)
	}
	// 官方节点同步：手动改状态立即通知其他节点重载渠道；自动禁用/恢复留给周期同步，避免频繁全量重载。
	// 从节点自动禁用了带 scheduled 钩子的插件渠道 key 时也要通知，主节点据此执行插件的定时钩子。
	pluginHook := status == common.ChannelStatusAutoDisabled && !common.IsMasterNode && channelPluginHasHook(channel, "scheduled")
	if (usingKey == "" && status != common.ChannelStatusAutoDisabled) || pluginHook {
		BumpSyncTopic(SyncTopicChannels)
	}
	return true
}

// channelPluginHasHook reports a task plugin channel whose plugin exports
// hook. It reads the setting without GetSetting, which clears an invalid
// stored setting.

func channelPluginHasHook(ch *Channel, hook string) bool {
	var setting dto.ChannelSettings
	if ch.Type != constant.ChannelTypeTaskPlugin || ch.Setting == nil || common.UnmarshalJsonStr(*ch.Setting, &setting) != nil {
		return false
	}
	plugin, ok := jsplugin.DefaultRegistry.Get(setting.TaskPluginKey)
	return ok && plugin.HasProviderHook(hook)
}

func EnableChannelByTag(tag string) error {
	return updateChannelStatusByTag(tag, common.ChannelStatusEnabled, true)
}

func DisableChannelByTag(tag string) error {
	// 官方：按标签禁用时，先把因 key 耗尽而自动禁用的多 key 渠道改为手动禁用，取消自动恢复
	var exhausted []Channel
	if err := DB.Where("tag = ? and status != ?", tag, common.ChannelStatusDeleted).Find(&exhausted).Error; err != nil {
		return err
	}
	for _, channel := range exhausted {
		if channel.ChannelInfo.IsMultiKey && channel.GetOtherInfo()["status_reason"] == ChannelStatusReasonAllKeysDisabled {
			if !UpdateChannelStatus(channel.Id, "", common.ChannelStatusManuallyDisabled, "manual tag operation") {
				return fmt.Errorf("failed to disable channel #%d by tag", channel.Id)
			}
		}
	}
	return updateChannelStatusByTag(tag, common.ChannelStatusManuallyDisabled, false)
}

func updateChannelStatusByTag(tag string, status int, abilityEnabled bool) error {
	unlockTagMutation := LockChannelTagMutation()
	defer unlockTagMutation()
	var channelIds []int
	if err := DB.Model(&Channel{}).Where("tag = ? and status != ?", tag, common.ChannelStatusDeleted).Pluck("id", &channelIds).Error; err != nil {
		return err
	}
	if len(channelIds) == 0 {
		return nil
	}

	unlock := lockChannelPollingLocks(channelIds)
	defer unlock()

	tx := DB.Begin()
	if tx.Error != nil {
		return tx.Error
	}
	channelIds = nil
	if err := tx.Model(&Channel{}).Where("tag = ? and status != ?", tag, common.ChannelStatusDeleted).Pluck("id", &channelIds).Error; err != nil {
		tx.Rollback()
		return err
	}
	if len(channelIds) == 0 {
		return tx.Commit().Error
	}
	if err := tx.Model(&Channel{}).Where("id in (?) and tag = ? and status != ?", channelIds, tag, common.ChannelStatusDeleted).Update("status", status).Error; err != nil {
		tx.Rollback()
		return err
	}
	if err := updateAbilityStatusForActiveChannels(
		tx.Model(&Ability{}).Where("channel_id in (?)", channelIds),
		tx.Model(&Channel{}).Where("id in (?) and tag = ?", channelIds, tag),
		abilityEnabled,
	); err != nil {
		tx.Rollback()
		return err
	}
	if err := tx.Commit().Error; err != nil {
		return err
	}
	BumpSyncTopic(SyncTopicChannels)
	return nil
}

func EditChannelByTag(tag string, newTag *string, modelMapping *string, models *string, group *string, priority *int64, weight *uint, paramOverride *string, headerOverride *string) error {
	if newTag != nil && *newTag != tag {
		unlockTagMutation := LockChannelTagMutation()
		defer unlockTagMutation()
	}
	updateData := Channel{}
	shouldReCreateAbilities := false
	updatedTag := tag
	// 如果 newTag 不为空且不等于 tag，则更新 tag
	if newTag != nil && *newTag != tag {
		updateData.Tag = newTag
		updatedTag = *newTag
	}
	if modelMapping != nil {
		updateData.ModelMapping = modelMapping
	}
	if models != nil && *models != "" {
		shouldReCreateAbilities = true
		updateData.Models = *models
	}
	if group != nil && *group != "" {
		shouldReCreateAbilities = true
		updateData.Group = *group
	}
	if priority != nil {
		updateData.Priority = priority
	}
	if weight != nil {
		updateData.Weight = weight
	}
	if paramOverride != nil {
		updateData.ParamOverride = paramOverride
	}
	if headerOverride != nil {
		updateData.HeaderOverride = headerOverride
	}

	err := DB.Model(&Channel{}).Where("tag = ? and status != ?", tag, common.ChannelStatusDeleted).Updates(updateData).Error
	if err != nil {
		return err
	}
	defer BumpSyncTopic(SyncTopicChannels)
	if shouldReCreateAbilities {
		var channels []*Channel
		if err := DB.Where("tag = ? and status != ?", updatedTag, common.ChannelStatusDeleted).Find(&channels).Error; err != nil {
			return err
		}
		for _, channel := range channels {
			if err := channel.UpdateAbilities(nil); err != nil {
				common.SysLog(fmt.Sprintf("failed to update abilities: channel_id=%d, tag=%s, error=%v", channel.Id, channel.GetTag(), err))
			}
		}
	} else {
		err := UpdateAbilityByTag(tag, newTag, priority, weight)
		if err != nil {
			return err
		}
	}
	return nil
}

func UpdateChannelUsedQuota(id int, quota int) {
	if common.BatchUpdateEnabled {
		addNewRecord(BatchUpdateTypeChannelUsedQuota, id, quota)
		return
	}
	updateChannelUsedQuota(id, quota)
}

func updateChannelUsedQuota(id int, quota int) {
	err := DB.Model(&Channel{}).Where("id = ?", id).Update("used_quota", gorm.Expr("used_quota + ?", quota)).Error
	if err != nil {
		common.SysLog(common.LogText("failed to update channel used quota: channel_id=%d, delta_quota=%d, error=%v", id, quota, err))
	}
}

func DeleteChannelByStatus(status int64) (int64, error) {
	var ids []int
	if err := DB.Model(&Channel{}).Where("status = ? and status != ?", status, common.ChannelStatusDeleted).Pluck("id", &ids).Error; err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}
	unlock := lockChannelPollingLocks(ids)
	defer unlock()
	if err := DB.Model(&Channel{}).Where("status = ? and status != ?", status, common.ChannelStatusDeleted).Pluck("id", &ids).Error; err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}
	tx := DB.Begin()
	if tx.Error != nil {
		return 0, tx.Error
	}
	result := tx.Model(&Channel{}).Where("id in (?) and status = ? and status != ?", ids, status, common.ChannelStatusDeleted).Update("status", common.ChannelStatusDeleted)
	if result.Error != nil {
		tx.Rollback()
		return 0, result.Error
	}
	var deletedIds []int
	if err := tx.Model(&Channel{}).Where("id in (?) and status = ?", ids, common.ChannelStatusDeleted).Pluck("id", &deletedIds).Error; err != nil {
		tx.Rollback()
		return 0, err
	}
	if len(deletedIds) == 0 {
		if err := tx.Commit().Error; err != nil {
			return 0, err
		}
		return 0, nil
	}
	if err := tx.Model(&Ability{}).Where("channel_id in (?)", deletedIds).Update("enabled", false).Error; err != nil {
		tx.Rollback()
		return 0, err
	}
	if err := tx.Commit().Error; err != nil {
		return int64(len(deletedIds)), err
	}
	return int64(len(deletedIds)), nil
}

func DeleteDisabledChannel() (int64, error) {
	query := DB.Model(&Channel{}).Where("(status = ? or status = ?) and status != ?", common.ChannelStatusAutoDisabled, common.ChannelStatusManuallyDisabled, common.ChannelStatusDeleted)
	var ids []int
	if err := query.Pluck("id", &ids).Error; err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}
	unlock := lockChannelPollingLocks(ids)
	defer unlock()
	if err := DB.Model(&Channel{}).Where("(status = ? or status = ?) and status != ?", common.ChannelStatusAutoDisabled, common.ChannelStatusManuallyDisabled, common.ChannelStatusDeleted).Pluck("id", &ids).Error; err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}
	tx := DB.Begin()
	if tx.Error != nil {
		return 0, tx.Error
	}
	result := tx.Model(&Channel{}).Where("id in (?) and (status = ? or status = ?) and status != ?", ids, common.ChannelStatusAutoDisabled, common.ChannelStatusManuallyDisabled, common.ChannelStatusDeleted).Update("status", common.ChannelStatusDeleted)
	if result.Error != nil {
		tx.Rollback()
		return 0, result.Error
	}
	var deletedIds []int
	if err := tx.Model(&Channel{}).Where("id in (?) and status = ?", ids, common.ChannelStatusDeleted).Pluck("id", &deletedIds).Error; err != nil {
		tx.Rollback()
		return 0, err
	}
	if len(deletedIds) == 0 {
		if err := tx.Commit().Error; err != nil {
			return 0, err
		}
		return 0, nil
	}
	if err := tx.Model(&Ability{}).Where("channel_id in (?)", deletedIds).Update("enabled", false).Error; err != nil {
		tx.Rollback()
		return 0, err
	}
	if err := tx.Commit().Error; err != nil {
		return int64(len(deletedIds)), err
	}
	BumpSyncTopic(SyncTopicChannels)
	return int64(len(deletedIds)), nil
}

func GetPaginatedTags(offset int, limit int, region string) ([]*string, error) {
	var tags []*string
	err := DB.Table(ChannelTable(region)).Select("DISTINCT tag").Where("tag != ''").Offset(offset).Limit(limit).Find(&tags).Error
	return tags, err
}

func GetPaginatedChannelTags(query *gorm.DB, offset int, limit int) ([]*string, error) {
	var tags []*string
	err := query.Session(&gorm.Session{}).
		Select("DISTINCT tag").
		Where("tag is not null AND tag != ''").
		Order(clause.OrderByColumn{Column: clause.Column{Name: "tag"}}).
		Offset(offset).
		Limit(limit).
		Find(&tags).Error
	return tags, err
}

func SearchTags(keyword string, group string, model string, idSort bool, region string) ([]*string, error) {
	var tags []*string
	modelsCol := "`models`"

	// 如果是 PostgreSQL，使用双引号
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		modelsCol = `"models"`
	}

	baseURLCol := "`base_url`"
	// 如果是 PostgreSQL，使用双引号
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		baseURLCol = `"base_url"`
	}

	order := "priority desc"
	if idSort {
		order = "id desc"
	}

	// 构造基础查询
	baseQuery := DB.Table(ChannelTable(region)).Omit("key")

	// 构造WHERE子句
	whereClause := "(id = ? OR name LIKE ? OR " + commonKeyCol + " = ? OR " + baseURLCol + " LIKE ?) AND " + modelsCol + " LIKE ?"
	args := []any{common.String2Int(keyword), "%" + keyword + "%", keyword, "%" + keyword + "%", "%" + model + "%"}
	baseQuery = ApplyChannelGroupFilter(baseQuery.Where(whereClause, args...), group)

	subQuery := baseQuery.
		Select("tag").
		Where("tag != ''").
		Order(order)

	err := DB.Table("(?) as sub", subQuery).
		Select("DISTINCT tag").
		Find(&tags).Error

	if err != nil {
		return nil, err
	}

	return tags, nil
}

func (channel *Channel) ValidateSettings() error {
	if channel.CostDiscount != nil &&
		(math.IsNaN(*channel.CostDiscount) || math.IsInf(*channel.CostDiscount, 0) || *channel.CostDiscount < 0 || *channel.CostDiscount > 1) {
		return fmt.Errorf("cost_discount must be between 0 and 1")
	}
	channelParams := &dto.ChannelSettings{}
	if channel.Setting != nil && *channel.Setting != "" {
		err := common.Unmarshal([]byte(*channel.Setting), channelParams)
		if err != nil {
			return err
		}
	}
	if _, err := common.ParseProxyURLStrict(channelParams.Proxy); err != nil {
		return fmt.Errorf("invalid channel proxy: %w", err)
	}
	if err := channelParams.ValidateHTTPTransport(); err != nil {
		return err
	}
	channelOtherSettings := &dto.ChannelOtherSettings{}
	if channel.OtherSettings != "" {
		err := common.UnmarshalJsonStr(channel.OtherSettings, channelOtherSettings)
		if err != nil {
			return err
		}
	}
	if err := channelOtherSettings.ValidateToolLossPolicy(); err != nil {
		return err
	}
	if err := channelOtherSettings.ValidateModelCostDiscount(); err != nil {
		return err
	}
	if preset := common.GetAdvancedCustomPreset(channel.Type); preset != nil {
		channelOtherSettings.AdvancedCustom = preset
	}
	if constant.IsAdvancedCustomChannel(channel.Type) {
		if channelOtherSettings.AdvancedCustom == nil {
			return fmt.Errorf("advanced_custom is required")
		}
	}
	if channelOtherSettings.AdvancedCustom != nil {
		if err := channelOtherSettings.AdvancedCustom.Validate(); err != nil {
			return err
		}
	}
	if channel.Type == constant.ChannelTypeMockAI {
		if channelOtherSettings.MockAI == nil {
			return fmt.Errorf("mock_ai is required")
		}
	}
	if channelOtherSettings.MockAI != nil {
		if err := channelOtherSettings.MockAI.Validate(); err != nil {
			return err
		}
	}
	if constant.IsAdvancedCustomChannel(channel.Type) && channelOtherSettings.UpstreamModelUpdateCheckEnabled {
		if _, ok := channelOtherSettings.AdvancedCustom.ModelListRoute(); !ok {
			return fmt.Errorf("advanced custom channels require a %s route when upstream model update checks are enabled", dto.AdvancedCustomModelListPath)
		}
	}
	return nil
}

func (channel *Channel) GetSetting() dto.ChannelSettings {
	setting := dto.ChannelSettings{}
	if channel.Setting != nil && *channel.Setting != "" {
		err := common.Unmarshal([]byte(*channel.Setting), &setting)
		if err != nil {
			common.SysLog(common.LogText("failed to unmarshal setting: channel_id=%d, error=%v", channel.Id, err))
			channel.Setting = nil // 清空设置以避免后续错误
			// Only the cleared column is written: the rest of this copy, its key
			// included, may be older than the stored channel.
			_ = DB.Model(&Channel{}).Where("id = ?", channel.Id).Update("setting", nil).Error
		}
	}
	return setting
}

func (channel *Channel) SetSetting(setting dto.ChannelSettings) {
	settingBytes, err := common.Marshal(setting)
	if err != nil {
		common.SysLog(common.LogText("failed to marshal setting: channel_id=%d, error=%v", channel.Id, err))
		return
	}
	channel.Setting = common.GetPointer[string](string(settingBytes))
}

// GetDialProxy 返回用于选择出站 HTTP 客户端的代理键：供应商渠道会带上拨号防护标记，
// 使 service.GetHttpClientWithProxy* 返回拨号阶段拒绝内网地址的客户端。
// 返回值仅用于选择客户端，不能写回渠道设置。
func (channel *Channel) GetDialProxy() string {
	proxy := channel.GetSetting().Proxy
	if channel.GetOtherSettings().IsSupplierChannel() {
		return common.WithSupplierDialGuard(proxy)
	}
	return proxy
}

func (channel *Channel) GetOtherSettings() dto.ChannelOtherSettings {
	setting := dto.ChannelOtherSettings{}
	if channel.OtherSettings != "" {
		err := common.UnmarshalJsonStr(channel.OtherSettings, &setting)
		if err != nil {
			common.SysLog(common.LogText("failed to unmarshal setting: channel_id=%d, error=%v", channel.Id, err))
			channel.OtherSettings = "{}" // 清空设置以避免后续错误
			// Only the cleared column is written, as in GetSetting.
			_ = DB.Model(&Channel{}).Where("id = ?", channel.Id).Update("settings", channel.OtherSettings).Error
		}
	}
	if preset := common.GetAdvancedCustomPreset(channel.Type); preset != nil {
		setting.AdvancedCustom = preset
	}
	return setting
}

func (channel *Channel) GetCostDiscountForModel(modelName string) (float64, bool) {
	// Resolve the effective upstream channel cost only after a channel is selected.
	// Keep the request model (before model_mapping) as the lookup key so aliases
	// can have their own cost without changing the upstream model name.
	settings := channel.GetOtherSettings()
	if discount, ok := settings.ModelCostDiscount[modelName]; ok &&
		!math.IsNaN(discount) && !math.IsInf(discount, 0) && discount >= 0 && discount <= 1 {
		return discount, true
	}
	if channel.CostDiscount != nil && *channel.CostDiscount >= 0 && *channel.CostDiscount <= 1 &&
		!math.IsNaN(*channel.CostDiscount) && !math.IsInf(*channel.CostDiscount, 0) {
		return *channel.CostDiscount, true
	}
	return 0, false
}

func (channel *Channel) SetOtherSettings(setting dto.ChannelOtherSettings) {
	settingBytes, err := common.Marshal(setting)
	if err != nil {
		common.SysLog(common.LogText("failed to marshal setting: channel_id=%d, error=%v", channel.Id, err))
		return
	}
	channel.OtherSettings = string(settingBytes)
}

func (channel *Channel) GetParamOverride() map[string]interface{} {
	paramOverride := make(map[string]interface{})
	if channel.ParamOverride != nil && *channel.ParamOverride != "" {
		err := common.Unmarshal([]byte(*channel.ParamOverride), &paramOverride)
		if err != nil {
			common.SysLog(common.LogText("failed to unmarshal param override: channel_id=%d, error=%v", channel.Id, err))
		}
	}
	return paramOverride
}

func (channel *Channel) GetHeaderOverride() map[string]interface{} {
	headerOverride := make(map[string]interface{})
	if channel.HeaderOverride != nil && *channel.HeaderOverride != "" {
		err := common.Unmarshal([]byte(*channel.HeaderOverride), &headerOverride)
		if err != nil {
			common.SysLog(common.LogText("failed to unmarshal header override: channel_id=%d, error=%v", channel.Id, err))
		}
	}
	return headerOverride
}

func GetChannelsByIds(ids []int) ([]*Channel, error) {
	var channels []*Channel
	err := DB.Where("id in (?) and status != ?", ids, common.ChannelStatusDeleted).Find(&channels).Error
	return channels, err
}

func BatchSetChannelTag(ids []int, tag *string) error {
	unlockTagMutation := LockChannelTagMutation()
	defer unlockTagMutation()

	// 开启事务
	tx := DB.Begin()
	if tx.Error != nil {
		return tx.Error
	}

	// 更新标签
	err := tx.Model(&Channel{}).Where("id in (?) and status != ?", ids, common.ChannelStatusDeleted).Update("tag", tag).Error
	if err != nil {
		tx.Rollback()
		return err
	}

	// update ability status
	var channels []*Channel
	if err := tx.Where("id in (?) and status != ?", ids, common.ChannelStatusDeleted).Find(&channels).Error; err != nil {
		tx.Rollback()
		return err
	}

	for _, channel := range channels {
		if err := channel.UpdateAbilities(tx); err != nil {
			tx.Rollback()
			return err
		}
	}

	// 提交事务
	if err = tx.Commit().Error; err != nil {
		return err
	}
	BumpSyncTopic(SyncTopicChannels)
	return nil
}

// CountAllChannels returns total channels in DB
func CountAllChannels() (int64, error) {
	var total int64
	err := DB.Model(&Channel{}).Count(&total).Error
	return total, err
}

// CountAllTags returns number of non-empty distinct tags
func CountAllTags(region string) (int64, error) {
	var total int64
	err := DB.Table(ChannelTable(region)).Where("tag is not null AND tag != ''").Distinct("tag").Count(&total).Error
	return total, err
}

func CountChannelTags(query *gorm.DB) (int64, error) {
	var total int64
	err := query.Session(&gorm.Session{}).Where("tag is not null AND tag != ''").Distinct("tag").Count(&total).Error
	return total, err
}

// Get channels of specified type with pagination
func GetChannelsByType(startIdx int, num int, idSort bool, channelType int) ([]*Channel, error) {
	var channels []*Channel
	order := "priority desc"
	if idSort {
		order = "id desc"
	}
	err := DB.Where("type = ?", channelType).Order(order).Limit(num).Offset(startIdx).Omit("key").Find(&channels).Error
	return channels, err
}

// Count channels of specific type
func CountChannelsByType(channelType int) (int64, error) {
	var count int64
	err := DB.Model(&Channel{}).Where("type = ?", channelType).Count(&count).Error
	return count, err
}

// Return map[type]count for all channels
func CountChannelsGroupByType() (map[int64]int64, error) {
	type result struct {
		Type  int64 `gorm:"column:type"`
		Count int64 `gorm:"column:count"`
	}
	var results []result
	err := DB.Model(&Channel{}).Select("type, count(*) as count").Group("type").Find(&results).Error
	if err != nil {
		return nil, err
	}
	counts := make(map[int64]int64)
	for _, r := range results {
		counts[r.Type] = r.Count
	}
	return counts, nil
}
