package controller

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestVideoTokenRatioBillingLogInsertFailureRollsBackCharge(t *testing.T) {
	ratio_setting.InitRatioSettings()
	t.Cleanup(ratio_setting.InitRatioSettings)
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousRedisEnabled := common.RedisEnabled
	previousMainDatabaseType := common.MainDatabaseType()
	previousLogDatabaseType := common.LogDatabaseType()
	previousBatchUpdateEnabled := common.BatchUpdateEnabled
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Task{}, &model.Log{}, &model.Channel{}))
	model.DB, model.LOG_DB = db, db
	common.RedisEnabled = false
	common.BatchUpdateEnabled = false
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.RedisEnabled = previousRedisEnabled
		common.BatchUpdateEnabled = previousBatchUpdateEnabled
		common.SetDatabaseTypes(previousMainDatabaseType, previousLogDatabaseType)
		require.NoError(t, sqlDB.Close())
	})
	require.NoError(t, db.Create(&model.User{Id: 7, Username: "video-billing", Quota: 10000, Group: "default"}).Error)
	task := &model.Task{
		TaskID: "interaction-log-fail",
		UserId: 7,
		Group:  "default",
		Quota:  0,
		Properties: model.Properties{
			OriginModelName: relaycommon.GeminiOmni11FlashPreview,
		},
	}
	task.SetData(map[string]any{"billing_effective_group_ratio": 1.0})
	require.NoError(t, db.Create(task).Error)
	require.NoError(t, db.Exec("CREATE TRIGGER reject_video_consume_log BEFORE INSERT ON logs BEGIN SELECT RAISE(FAIL, 'forced log insert failure'); END").Error)

	err = CompleteVideoTaskOnUpstreamSuccess(context.Background(), task, nil, &relaycommon.TaskInfo{
		Status:            model.TaskStatusSuccess,
		VideoOutputTokens: 1000,
	}, []byte(`{"status":"completed"}`))

	require.Error(t, err)
	var user model.User
	require.NoError(t, db.First(&user, "id = ?", 7).Error)
	require.Equal(t, 10000, user.Quota, "failed log insertion must not leave the user charged")
	var storedTask model.Task
	require.NoError(t, db.First(&storedTask, "id = ?", task.ID).Error)
	require.Equal(t, model.TaskStatus("SUCCESS"), storedTask.Status, "log failure must not downgrade an upstream-completed task")
	require.Zero(t, storedTask.Quota, "failed log insertion must release the retryable billing claim")
	var storedData map[string]any
	require.NoError(t, common.Unmarshal(storedTask.Data, &storedData))
	require.NotEqual(t, true, storedData["billing_processed"], "failed log insertion must remain retryable")
	var logCount int64
	require.NoError(t, db.Model(&model.Log{}).Count(&logCount).Error)
	require.Zero(t, logCount)
	require.NoError(t, db.Exec("DROP TRIGGER reject_video_consume_log").Error)

	err = CompleteVideoTaskOnUpstreamSuccess(context.Background(), &storedTask, nil, &relaycommon.TaskInfo{
		Status:            model.TaskStatusSuccess,
		VideoOutputTokens: 1000,
	}, []byte(`{"status":"completed"}`))
	require.NoError(t, err, "a later GET should retry billing and log insertion")
	require.NoError(t, db.First(&user, "id = ?", 7).Error)
	require.Equal(t, 1250, user.Quota, "retry should apply the charge exactly once")
	require.NoError(t, db.First(&storedTask, "id = ?", task.ID).Error)
	require.Equal(t, 8750, storedTask.Quota)
	require.NoError(t, common.Unmarshal(storedTask.Data, &storedData))
	require.Equal(t, true, storedData["billing_processed"])
	require.NoError(t, db.Model(&model.Log{}).Count(&logCount).Error)
	require.EqualValues(t, 1, logCount)
}

func TestGeminiOmniInputTokensFallsBackToAggregateUsage(t *testing.T) {
	aggregate, textInput := geminiOmniInputTokens(&relaycommon.TaskInfo{InputTokens: 23})
	require.Equal(t, 23, aggregate)
	require.Equal(t, 23, textInput)
}

func TestGeminiOmniInputTokensKeepsImageAndVideoInputSeparate(t *testing.T) {
	aggregate, textInput := geminiOmniInputTokens(&relaycommon.TaskInfo{
		InputTokens:      1200,
		ImageInputTokens: 700,
		VideoInputTokens: 400,
	})
	require.Equal(t, 1200, aggregate)
	require.Equal(t, 100, textInput)
}

func TestVideoTaskUseTimeSecondsFallsBackToSubmitTimeForOmni(t *testing.T) {
	task := &model.Task{
		SubmitTime: 100,
		FinishTime: 163,
		StartTime:  0,
		CreatedAt:  90,
	}

	require.Equal(t, 63, videoTaskUseTimeSeconds(task, "gemini-omni-flash-preview"))
	require.Equal(t, 63, videoTaskUseTimeSeconds(task, "gemini-omni-1.1-flash-preview"))
	require.Zero(t, videoTaskUseTimeSeconds(task, "another-video-model"))
}

func TestGeminiOmniFallbackVideoTokensPerSecond(t *testing.T) {
	for _, tc := range []struct {
		model      string
		resolution string
		want       int
	}{
		{"gemini-omni-flash-preview", "1080p", 5792},
		{"gemini-omni-1.1-flash-preview", "360p", 1931},
		{"gemini-omni-1.1-flash-preview", "720p", 5792},
		{"gemini-omni-1.1-flash-preview", "1080p", 8688},
		{"gemini-omni-1.1-flash-preview", "4k", 17376},
	} {
		require.Equal(t, tc.want, geminiOmniFallbackVideoTokensPerSecond(tc.model, tc.resolution))
	}
}
