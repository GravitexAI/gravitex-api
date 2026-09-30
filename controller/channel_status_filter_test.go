package controller

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelStatusMatchesFilterKeepsSQLAndInMemoryFiltersAligned(t *testing.T) {
	tests := []struct {
		name   string
		status int
		filter int
		want   bool
	}{
		{name: "all excludes deleted", status: common.ChannelStatusDeleted, filter: channelStatusFilterAll},
		{name: "all includes auto-disabled", status: common.ChannelStatusAutoDisabled, filter: channelStatusFilterAll, want: true},
		{name: "enabled excludes disabled", status: common.ChannelStatusManuallyDisabled, filter: common.ChannelStatusEnabled},
		{name: "auto-disabled exact match", status: common.ChannelStatusAutoDisabled, filter: common.ChannelStatusAutoDisabled, want: true},
		{name: "disabled includes auto-disabled", status: common.ChannelStatusAutoDisabled, filter: 0, want: true},
		{name: "disabled excludes deleted", status: common.ChannelStatusDeleted, filter: 0},
		{name: "deleted exact match", status: common.ChannelStatusDeleted, filter: common.ChannelStatusDeleted, want: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, channelStatusMatchesFilter(test.status, test.filter))
		})
	}
}

func TestGetAllChannelsExcludesDeletedByDefaultAndKeepsExplicitDeletedFilter(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	activeTag := "active-tag"
	deletedTag := "deleted-tag"
	require.NoError(t, db.Create(&model.Channel{Name: "active", Status: common.ChannelStatusEnabled, Tag: &activeTag, Group: "default"}).Error)
	require.NoError(t, db.Create(&model.Channel{Name: "deleted", Status: common.ChannelStatusDeleted, Tag: &deletedTag, Group: "default"}).Error)

	request := func(query string) struct {
		Data struct {
			Items      []model.Channel  `json:"items"`
			Total      int64            `json:"total"`
			TypeCounts map[string]int64 `json:"type_counts"`
		} `json:"data"`
	} {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest(http.MethodGet, "/api/channel/search?"+query, nil)
		GetAllChannels(ctx)
		var payload struct {
			Data struct {
				Items      []model.Channel  `json:"items"`
				Total      int64            `json:"total"`
				TypeCounts map[string]int64 `json:"type_counts"`
			} `json:"data"`
		}
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
		return payload
	}

	for _, tagMode := range []string{"false", "true"} {
		payload := request("status=&tag_mode=" + tagMode + "&p=1&page_size=10")
		require.EqualValues(t, 1, payload.Data.Total)
		require.Len(t, payload.Data.Items, 1)
		assert.Equal(t, "active", payload.Data.Items[0].Name)
		assert.EqualValues(t, 1, payload.Data.TypeCounts["0"])
	}

	deleted := request("status=-1&p=1&page_size=10")
	require.EqualValues(t, 1, deleted.Data.Total)
	require.Len(t, deleted.Data.Items, 1)
	assert.Equal(t, "deleted", deleted.Data.Items[0].Name)
}

func TestUpdateChannelRejectsDeletedChannel(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	channel := model.Channel{
		Name:   "deleted-channel-edit",
		Key:    "key",
		Models: "gpt-4o",
		Group:  "default",
		Status: common.ChannelStatusDeleted,
	}
	require.NoError(t, db.Create(&channel).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPut, "/api/channel", strings.NewReader(`{"id":`+strconv.Itoa(channel.Id)+`,"name":"renamed"}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	UpdateChannel(ctx)

	var response struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.False(t, response.Success)
	assert.Equal(t, "已删除的渠道不能修改", response.Message)
	var stored model.Channel
	require.NoError(t, db.First(&stored, channel.Id).Error)
	assert.Equal(t, common.ChannelStatusDeleted, stored.Status)
	assert.Equal(t, "deleted-channel-edit", stored.Name)
}
