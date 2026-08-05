package controller

import (
	"encoding/json"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

// GetAllModelsMeta 获取模型列表（分页）
func GetAllModelsMeta(c *gin.Context) {

	pageInfo := common.GetPageQuery(c)
	status := c.Query("status")
	syncOfficial := c.Query("sync_official")
	modelsMeta, total, err := model.SearchModels("", "", status, syncOfficial, pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	// 批量填充附加字段，提升列表接口性能
	enrichModels(modelsMeta)

	// 统计供应商计数（全部数据，不受分页影响）
	vendorCounts, _ := model.GetVendorModelCounts(status)

	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(modelsMeta)
	common.ApiSuccess(c, gin.H{
		"items":         modelsMeta,
		"total":         total,
		"page":          pageInfo.GetPage(),
		"page_size":     pageInfo.GetPageSize(),
		"vendor_counts": vendorCounts,
	})
}

// SearchModelsMeta 搜索模型列表
func SearchModelsMeta(c *gin.Context) {

	keyword := c.Query("keyword")
	vendor := c.Query("vendor")
	status := c.Query("status")
	syncOfficial := c.Query("sync_official")
	pageInfo := common.GetPageQuery(c)

	modelsMeta, total, err := model.SearchModels(keyword, vendor, status, syncOfficial, pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	// 批量填充附加字段，提升列表接口性能
	enrichModels(modelsMeta)
	vendorCounts, _ := model.GetVendorModelCounts(status)
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(modelsMeta)
	common.ApiSuccess(c, gin.H{
		"items":         modelsMeta,
		"total":         total,
		"page":          pageInfo.GetPage(),
		"page_size":     pageInfo.GetPageSize(),
		"vendor_counts": vendorCounts,
	})
}

// GetModelMeta 根据 ID 获取单条模型信息
func GetModelMeta(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	var m model.Model
	if err := model.DB.First(&m, id).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	enrichModels([]*model.Model{&m})
	common.ApiSuccess(c, &m)
}

func readModelMetaPayload(c *gin.Context) (model.Model, map[string]json.RawMessage, error) {
	var m model.Model
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return m, nil, err
	}
	payload := make(map[string]json.RawMessage)
	if err := common.Unmarshal(body, &payload); err != nil {
		return m, nil, err
	}
	if err := common.Unmarshal(body, &m); err != nil {
		return m, nil, err
	}
	return m, payload, nil
}

func buildModelMetaUpdates(m model.Model, payload map[string]json.RawMessage) map[string]any {
	updates := make(map[string]any, len(payload)+1)
	if _, ok := payload["model_name"]; ok {
		updates["model_name"] = m.ModelName
	}
	if _, ok := payload["description"]; ok {
		updates["description"] = m.Description
	}
	if _, ok := payload["description_en"]; ok {
		updates["description_en"] = m.DescriptionEn
	}
	if _, ok := payload["description_id"]; ok {
		updates["description_id"] = m.DescriptionId
	}
	if _, ok := payload["icon"]; ok {
		updates["icon"] = m.Icon
	}
	if _, ok := payload["icon_url"]; ok {
		updates["icon_url"] = m.IconURL
	}
	if _, ok := payload["tags"]; ok {
		updates["tags"] = m.Tags
	}
	if _, ok := payload["tags_en"]; ok {
		updates["tags_en"] = m.TagsEn
	}
	if _, ok := payload["tags_id"]; ok {
		updates["tags_id"] = m.TagsId
	}
	if _, ok := payload["show_tab"]; ok {
		updates["show_tab"] = m.ShowTab
	}
	if _, ok := payload["flag"]; ok {
		updates["flag"] = m.Flag
	}
	if _, ok := payload["sort_order"]; ok {
		updates["sort_order"] = m.SortOrder
	}
	if _, ok := payload["is_featured"]; ok {
		updates["is_featured"] = m.IsFeatured
	}
	if _, ok := payload["vendor_id"]; ok {
		updates["vendor_id"] = m.VendorID
	}
	if _, ok := payload["endpoints"]; ok {
		updates["endpoints"] = m.Endpoints
	}
	if _, ok := payload["model_limit"]; ok {
		updates["model_limit"] = m.ModelLimit
	}
	if _, ok := payload["status"]; ok {
		updates["status"] = m.Status
	}
	if _, ok := payload["sync_official"]; ok {
		updates["sync_official"] = m.SyncOfficial
	}
	if _, ok := payload["name_rule"]; ok {
		updates["name_rule"] = m.NameRule
	}
	if _, ok := payload["model_nick_name"]; ok {
		updates["model_nick_name"] = m.ModelNickName
	}
	if _, ok := payload["created_time"]; ok {
		updates["created_time"] = m.CreatedTime
	}
	return updates
}

// CreateModelMeta 新建模型
func CreateModelMeta(c *gin.Context) {
	m, payload, err := readModelMetaPayload(c)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if m.ModelName == "" {
		common.ApiErrorMsg(c, "模型名称不能为空")
		return
	}
	if _, ok := payload["status"]; !ok {
		m.Status = 1
	}
	if _, ok := payload["sync_official"]; !ok {
		m.SyncOfficial = 1
	}
	// 名称冲突检查
	if dup, err := model.IsModelNameDuplicated(0, m.ModelName); err != nil {
		common.ApiError(c, err)
		return
	} else if dup {
		common.ApiErrorMsg(c, "模型名称已存在")
		return
	}

	if err := m.Insert(); err != nil {
		common.ApiError(c, err)
		return
	}
	model.RefreshPricing()
	common.ApiSuccess(c, &m)
}

// UpdateModelMeta 更新模型
func UpdateModelMeta(c *gin.Context) {
	statusOnly := c.Query("status_only") == "true"

	m, payload, err := readModelMetaPayload(c)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if m.Id == 0 {
		common.ApiErrorMsg(c, "缺少模型 ID")
		return
	}

	if statusOnly {
		if _, ok := payload["status"]; !ok {
			common.ApiErrorMsg(c, "缺少状态字段")
			return
		}
		if err := model.UpdateModelFields(m.Id, map[string]any{
			"status":       m.Status,
			"updated_time": common.GetTimestamp(),
		}); err != nil {
			common.ApiError(c, err)
			return
		}
	} else {
		var current model.Model
		if err := model.DB.First(&current, m.Id).Error; err != nil {
			common.ApiError(c, err)
			return
		}

		modelName := current.ModelName
		if _, ok := payload["model_name"]; ok {
			modelName = m.ModelName
		}
		if modelName == "" {
			common.ApiErrorMsg(c, "模型名称不能为空")
			return
		}

		// 名称冲突检查
		if dup, err := model.IsModelNameDuplicated(m.Id, modelName); err != nil {
			common.ApiError(c, err)
			return
		} else if dup {
			common.ApiErrorMsg(c, "模型名称已存在")
			return
		}

		updates := buildModelMetaUpdates(m, payload)
		updates["updated_time"] = common.GetTimestamp()
		if err := model.UpdateModelFields(m.Id, updates); err != nil {
			common.ApiError(c, err)
			return
		}
	}

	if err := model.DB.First(&m, m.Id).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	model.RefreshPricing()
	common.ApiSuccess(c, &m)
}

// DeleteModelMeta 删除模型
func DeleteModelMeta(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if err := model.DB.Delete(&model.Model{}, id).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	model.RefreshPricing()
	common.ApiSuccess(c, nil)
}

// enrichModels 批量填充附加信息：端点、渠道、分组、计费类型，避免 N+1 查询
func enrichModels(models []*model.Model) {
	if len(models) == 0 {
		return
	}

	// 1) 拆分精确与规则匹配
	exactNames := make([]string, 0)
	exactIdx := make(map[string][]int) // modelName -> indices in models
	ruleIndices := make([]int, 0)
	for i, m := range models {
		if m == nil {
			continue
		}
		if m.NameRule == model.NameRuleExact {
			exactNames = append(exactNames, m.ModelName)
			exactIdx[m.ModelName] = append(exactIdx[m.ModelName], i)
		} else {
			ruleIndices = append(ruleIndices, i)
		}
	}

	// 2) 批量查询精确模型的绑定渠道
	channelsByModel, _ := model.GetBoundChannelsByModelsMap(exactNames)

	// 3) 精确模型：端点从缓存、渠道批量映射、分组/计费类型从缓存
	for name, indices := range exactIdx {
		chs := channelsByModel[name]
		for _, idx := range indices {
			mm := models[idx]
			if mm.Endpoints == "" {
				eps := model.GetModelSupportEndpointTypes(mm.ModelName)
				if b, err := common.Marshal(eps); err == nil {
					mm.Endpoints = string(b)
				}
			}
			mm.BoundChannels = chs
			mm.EnableGroups = model.GetModelEnableGroups(mm.ModelName)
			mm.QuotaTypes = model.GetModelQuotaTypes(mm.ModelName)
		}
	}

	if len(ruleIndices) == 0 {
		return
	}

	// 4) 一次性读取定价缓存，内存匹配所有规则模型
	pricings := model.GetPricing()

	// 为全部规则模型收集匹配名集合、端点并集、分组并集、配额集合
	matchedNamesByIdx := make(map[int][]string)
	endpointSetByIdx := make(map[int]map[constant.EndpointType]struct{})
	groupSetByIdx := make(map[int]map[string]struct{})
	quotaSetByIdx := make(map[int]map[int]struct{})

	for _, p := range pricings {
		for _, idx := range ruleIndices {
			mm := models[idx]
			var matched bool
			switch mm.NameRule {
			case model.NameRulePrefix:
				matched = strings.HasPrefix(p.ModelName, mm.ModelName)
			case model.NameRuleSuffix:
				matched = strings.HasSuffix(p.ModelName, mm.ModelName)
			case model.NameRuleContains:
				matched = strings.Contains(p.ModelName, mm.ModelName)
			}
			if !matched {
				continue
			}
			matchedNamesByIdx[idx] = append(matchedNamesByIdx[idx], p.ModelName)

			es := endpointSetByIdx[idx]
			if es == nil {
				es = make(map[constant.EndpointType]struct{})
				endpointSetByIdx[idx] = es
			}
			for _, et := range p.SupportedEndpointTypes {
				es[et] = struct{}{}
			}

			gs := groupSetByIdx[idx]
			if gs == nil {
				gs = make(map[string]struct{})
				groupSetByIdx[idx] = gs
			}
			for _, g := range p.EnableGroup {
				gs[g] = struct{}{}
			}

			qs := quotaSetByIdx[idx]
			if qs == nil {
				qs = make(map[int]struct{})
				quotaSetByIdx[idx] = qs
			}
			qs[p.QuotaType] = struct{}{}
		}
	}

	// 5) 汇总所有匹配到的模型名称，批量查询一次渠道
	allMatchedSet := make(map[string]struct{})
	for _, names := range matchedNamesByIdx {
		for _, n := range names {
			allMatchedSet[n] = struct{}{}
		}
	}
	allMatched := make([]string, 0, len(allMatchedSet))
	for n := range allMatchedSet {
		allMatched = append(allMatched, n)
	}
	matchedChannelsByModel, _ := model.GetBoundChannelsByModelsMap(allMatched)

	// 6) 回填每个规则模型的并集信息
	for _, idx := range ruleIndices {
		mm := models[idx]

		// 端点并集 -> 序列化
		if es, ok := endpointSetByIdx[idx]; ok && mm.Endpoints == "" {
			eps := make([]constant.EndpointType, 0, len(es))
			for et := range es {
				eps = append(eps, et)
			}
			if b, err := json.Marshal(eps); err == nil {
				mm.Endpoints = string(b)
			}
		}

		// 分组并集
		if gs, ok := groupSetByIdx[idx]; ok {
			groups := make([]string, 0, len(gs))
			for g := range gs {
				groups = append(groups, g)
			}
			mm.EnableGroups = groups
		}

		// 配额类型集合（保持去重并排序）
		if qs, ok := quotaSetByIdx[idx]; ok {
			arr := make([]int, 0, len(qs))
			for k := range qs {
				arr = append(arr, k)
			}
			sort.Ints(arr)
			mm.QuotaTypes = arr
		}

		// 渠道并集
		names := matchedNamesByIdx[idx]
		channelSet := make(map[string]model.BoundChannel)
		for _, n := range names {
			for _, ch := range matchedChannelsByModel[n] {
				key := ch.Name + "_" + strconv.Itoa(ch.Type)
				channelSet[key] = ch
			}
		}
		if len(channelSet) > 0 {
			chs := make([]model.BoundChannel, 0, len(channelSet))
			for _, ch := range channelSet {
				chs = append(chs, ch)
			}
			mm.BoundChannels = chs
		}

		// 匹配信息
		mm.MatchedModels = names
		mm.MatchedCount = len(names)
	}
}
