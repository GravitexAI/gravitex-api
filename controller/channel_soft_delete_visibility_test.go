package controller

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetSyncableChannelsExcludesDeletedChannels(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	activeURL := "https://active.example"
	deletedURL := "https://deleted.example"
	active := model.Channel{Name: "active", BaseURL: &activeURL, Status: common.ChannelStatusEnabled}
	deleted := model.Channel{Name: "deleted", BaseURL: &deletedURL, Status: common.ChannelStatusEnabled}
	require.NoError(t, db.Create(&active).Error)
	require.NoError(t, db.Create(&deleted).Error)
	require.NoError(t, deleted.Delete())

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/ratio_sync/channels", nil)
	GetSyncableChannels(ctx)

	var response struct {
		Data []dto.SyncableChannel `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	var returnedIDs []int
	for _, channel := range response.Data {
		returnedIDs = append(returnedIDs, channel.ID)
	}
	assert.Contains(t, returnedIDs, active.Id)
	assert.NotContains(t, returnedIDs, deleted.Id)
}

func TestFetchUpstreamRatiosRejectsDeletedChannelIDs(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	baseURL := "https://deleted.example"
	deleted := model.Channel{Name: "deleted", BaseURL: &baseURL, Status: common.ChannelStatusEnabled}
	require.NoError(t, db.Create(&deleted).Error)
	require.NoError(t, deleted.Delete())
	body, err := common.Marshal(dto.UpstreamRequest{ChannelIDs: []int64{int64(deleted.Id)}})
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/ratio_sync/fetch", bytes.NewReader(body))

	FetchUpstreamRatios(ctx)

	var response struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.False(t, response.Success)
	assert.Equal(t, "无有效上游渠道", response.Message)
}

func TestGetTagModelsIgnoresDeletedChannels(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	tag := "shared-tag"
	active := model.Channel{Name: "active", Tag: &tag, Models: "short-model", Status: common.ChannelStatusEnabled}
	deleted := model.Channel{Name: "deleted", Tag: &tag, Models: "deleted-model-a,deleted-model-b,deleted-model-c", Status: common.ChannelStatusEnabled}
	require.NoError(t, db.Create(&active).Error)
	require.NoError(t, db.Create(&deleted).Error)
	require.NoError(t, deleted.Delete())

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/channel/tag/models?tag=shared-tag", nil)
	GetTagModels(ctx)

	var response struct {
		Data string `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.Equal(t, active.Models, response.Data)
}
