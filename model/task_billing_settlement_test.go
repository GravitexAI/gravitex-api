package model

import (
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupTaskBillingSettlementDB(t *testing.T) {
	t.Helper()
	previousDB, previousLogDB := DB, LOG_DB
	previousMainType, previousLogType := common.MainDatabaseType(), common.LogDatabaseType()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	DB, LOG_DB = db, db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	require.NoError(t, db.AutoMigrate(&TaskBillingSettlement{}, &TaskBillingLogOutbox{}, &Task{}, &User{}, &Channel{}, &Token{}, &UserSubscription{}))
	t.Cleanup(func() {
		DB, LOG_DB = previousDB, previousLogDB
		common.SetDatabaseTypes(previousMainType, previousLogType)
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
}

func TestInteractionsTieredTaskInsertCreatesPendingSettlementAtomically(t *testing.T) {
	setupTaskBillingSettlementDB(t)
	task := Task{
		TaskID: "interaction-task-insert", UserId: 905, Status: TaskStatusQueued,
		PrivateData: TaskPrivateData{BillingContext: &TaskBillingContext{
			NativeInteractions: true,
			TieredSnapshot:     &billingexpr.BillingSnapshot{ExprString: `tier("base", p)`},
		}},
	}
	require.NoError(t, task.Insert())
	var stored TaskBillingSettlement
	require.NoError(t, DB.First(&stored, "task_key = ?", taskBillingKey(task.TaskID)).Error)
	assert.Equal(t, TaskBillingSettlementPending, stored.Status)
	var payload TaskBillingContext
	require.NoError(t, common.Unmarshal([]byte(stored.Payload), &payload))
	assert.True(t, payload.NativeInteractions)
	assert.Equal(t, `tier("base", p)`, payload.TieredSnapshot.ExprString)

	duplicate := Task{
		TaskID: task.TaskID, UserId: task.UserId, Status: TaskStatusQueued,
		PrivateData: task.PrivateData,
	}
	assert.Error(t, duplicate.Insert())
	var taskCount int64
	require.NoError(t, DB.Model(&Task{}).Where("task_id = ?", task.TaskID).Count(&taskCount).Error)
	assert.EqualValues(t, 1, taskCount)
}

func TestTaskPrivateDataValuePreservesBillingOwnershipFields(t *testing.T) {
	privateData := TaskPrivateData{BillingSource: "subscription", SubscriptionId: 73, TokenId: 74, NodeName: "origin-node"}
	value, err := privateData.Value()
	require.NoError(t, err)
	assert.NotNil(t, value)
	var decoded TaskPrivateData
	require.NoError(t, common.Unmarshal(value.([]byte), &decoded))
	assert.Equal(t, privateData, decoded)
}

func TestTaskBillingSettlementAndOutboxUseStableUniqueKeys(t *testing.T) {
	setupTaskBillingSettlementDB(t)
	payload, err := EncodeTaskBillingPayload(map[string]any{"frozen_expr": `tier("base", p)`})
	require.NoError(t, err)
	settlement, err := CreateTaskBillingSettlement("interaction-task-unique", payload)
	require.NoError(t, err)

	_, err = CreateTaskBillingSettlement(settlement.TaskID, payload)
	assert.Error(t, err)

	eventPayload, err := common.Marshal(map[string]any{"task_id": settlement.TaskID, "quota": 125})
	require.NoError(t, err)
	operationID := "task-billing-settlement:interaction-task-unique"
	outbox, err := CreateTaskBillingLogOutbox(operationID, settlement.TaskID, string(eventPayload))
	require.NoError(t, err)
	_, err = CreateTaskBillingLogOutbox(operationID, settlement.TaskID, string(eventPayload))
	assert.Error(t, err)

	var loaded TaskBillingLogOutbox
	require.NoError(t, DB.First(&loaded, "operation_key = ?", taskBillingKey(outbox.OperationID)).Error)
	var decoded map[string]any
	require.NoError(t, common.Unmarshal([]byte(loaded.Payload), &decoded))
	assert.Equal(t, float64(125), decoded["quota"])
}

func TestFinalizeTaskBillingSettlementIsTerminalAndCanResolveMissingUsage(t *testing.T) {
	setupTaskBillingSettlementDB(t)
	_, err := CreateTaskBillingSettlement("interaction-task-finalize", `{"expr":"tier(\"base\", p)"}`)
	require.NoError(t, err)

	var finalized bool
	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		var transitionErr error
		finalized, transitionErr = FinalizeTaskBillingSettlement(tx, "interaction-task-finalize", TaskBillingSettlementUsageUnavailable, "usage unavailable; preconsume retained", 500, "missing input_tokens", "input_tokens")
		return transitionErr
	}))
	assert.True(t, finalized)

	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		var transitionErr error
		finalized, transitionErr = FinalizeTaskBillingSettlement(tx, "interaction-task-finalize", TaskBillingSettlementSettled, "usage resolved", 125, "", "")
		return transitionErr
	}))
	assert.True(t, finalized)

	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		var transitionErr error
		finalized, transitionErr = FinalizeTaskBillingSettlement(tx, "interaction-task-finalize", TaskBillingSettlementSettled, "duplicate", 900, "", "")
		return transitionErr
	}))
	assert.False(t, finalized)

	var settlement TaskBillingSettlement
	require.NoError(t, DB.First(&settlement, "task_key = ?", taskBillingKey("interaction-task-finalize")).Error)
	assert.Equal(t, TaskBillingSettlementSettled, settlement.Status)
	assert.Equal(t, 125, settlement.ActualQuota)
	assert.Empty(t, settlement.UsageMissingReason)
}

func TestCommitTaskBillingSettlementAppliesFinancialDeltaExactlyOnce(t *testing.T) {
	setupTaskBillingSettlementDB(t)
	user := User{Id: 901, Quota: 10_000, UsedQuota: 100}
	channel := Channel{Id: 902, UsedQuota: 100}
	token := Token{Id: 903, UserId: user.Id, Key: "test-key", RemainQuota: 10_000, UsedQuota: 100}
	task := Task{TaskID: "interaction-atomic-settle", UserId: user.Id, ChannelId: channel.Id, TokenId: token.Id, Quota: 100, Status: TaskStatusSuccess}
	require.NoError(t, DB.Create(&user).Error)
	require.NoError(t, DB.Create(&channel).Error)
	require.NoError(t, DB.Create(&token).Error)
	require.NoError(t, DB.Create(&task).Error)
	_, err := CreateTaskBillingSettlement(task.TaskID, `{}`)
	require.NoError(t, err)

	input := TaskBillingSettlementCommit{
		TaskID:      task.TaskID,
		ActualQuota: 125, Status: TaskBillingSettlementSettled, Outcome: "settled",
		OutboxOperationID: "interactions-settlement:interaction-atomic-settle",
		OutboxPayload:     `{"quota":25}`,
	}
	committed, err := CommitTaskBillingSettlement(input)
	require.NoError(t, err)
	assert.True(t, committed)
	committed, err = CommitTaskBillingSettlement(input)
	require.NoError(t, err)
	assert.False(t, committed)

	var gotUser User
	require.NoError(t, DB.First(&gotUser, user.Id).Error)
	assert.Equal(t, 9_975, gotUser.Quota)
	assert.Equal(t, 125, gotUser.UsedQuota)
	var gotChannel Channel
	require.NoError(t, DB.First(&gotChannel, channel.Id).Error)
	assert.EqualValues(t, 125, gotChannel.UsedQuota)
	var gotToken Token
	require.NoError(t, DB.First(&gotToken, token.Id).Error)
	assert.Equal(t, 9_975, gotToken.RemainQuota)
	assert.Equal(t, 125, gotToken.UsedQuota)
	var gotTask Task
	require.NoError(t, DB.First(&gotTask, task.ID).Error)
	assert.Equal(t, 125, gotTask.Quota)
	var outboxCount int64
	require.NoError(t, DB.Model(&TaskBillingLogOutbox{}).Where("operation_key = ?", taskBillingKey(input.OutboxOperationID)).Count(&outboxCount).Error)
	assert.EqualValues(t, 1, outboxCount)
}

func TestCommitTaskBillingSettlementRollsBackAllFinancialWritesOnSubscriptionLimit(t *testing.T) {
	setupTaskBillingSettlementDB(t)
	user := User{Id: 911, Quota: 10_000, UsedQuota: 100}
	channel := Channel{Id: 912, UsedQuota: 100}
	token := Token{Id: 913, UserId: user.Id, Key: "subscription-token", RemainQuota: 10_000, UsedQuota: 100}
	subscription := UserSubscription{Id: 914, UserId: user.Id, AmountTotal: 500, AmountUsed: 400, Status: "active"}
	task := Task{TaskID: "interaction-subscription-rollback", UserId: user.Id, ChannelId: channel.Id, TokenId: token.Id, Quota: 100, Status: TaskStatusSuccess,
		PrivateData: TaskPrivateData{BillingSource: "subscription", SubscriptionId: subscription.Id, TokenId: token.Id}}
	for _, value := range []any{&user, &channel, &token, &subscription, &task} {
		require.NoError(t, DB.Create(value).Error)
	}
	_, err := CreateTaskBillingSettlement(task.TaskID, `{}`)
	require.NoError(t, err)
	var persistedTask Task
	require.NoError(t, DB.Where("task_id = ?", task.TaskID).First(&persistedTask).Error)
	assert.Equal(t, "subscription", persistedTask.PrivateData.BillingSource)
	assert.Equal(t, subscription.Id, persistedTask.PrivateData.SubscriptionId)
	input := TaskBillingSettlementCommit{
		TaskID: task.TaskID, ActualQuota: 600, Status: TaskBillingSettlementSettled,
		Outcome: "settled", OutboxOperationID: "subscription-settle:" + task.TaskID, OutboxPayload: `{}`,
	}
	committed, err := CommitTaskBillingSettlement(input)
	assert.Error(t, err)
	assert.False(t, committed)
	var gotUser User
	require.NoError(t, DB.First(&gotUser, user.Id).Error)
	assert.Equal(t, user.Quota, gotUser.Quota)
	assert.Equal(t, user.UsedQuota, gotUser.UsedQuota)
	var gotToken Token
	require.NoError(t, DB.First(&gotToken, token.Id).Error)
	assert.Equal(t, token.RemainQuota, gotToken.RemainQuota)
	var gotTask Task
	require.NoError(t, DB.First(&gotTask, task.ID).Error)
	assert.Equal(t, task.Quota, gotTask.Quota)
	var settlement TaskBillingSettlement
	require.NoError(t, DB.First(&settlement, "task_key = ?", taskBillingKey(task.TaskID)).Error)
	assert.Equal(t, TaskBillingSettlementPending, settlement.Status)
	var outboxCount int64
	require.NoError(t, DB.Model(&TaskBillingLogOutbox{}).Count(&outboxCount).Error)
	assert.Zero(t, outboxCount)
	input.ActualQuota = 200
	committed, err = CommitTaskBillingSettlement(input)
	require.NoError(t, err)
	assert.True(t, committed)
	var gotSubscription UserSubscription
	require.NoError(t, DB.First(&gotSubscription, subscription.Id).Error)
	assert.EqualValues(t, 500, gotSubscription.AmountUsed)
	require.NoError(t, DB.First(&gotUser, user.Id).Error)
	assert.Equal(t, user.Quota, gotUser.Quota)
	assert.Equal(t, 200, gotUser.UsedQuota)
	require.NoError(t, DB.First(&gotToken, token.Id).Error)
	assert.Equal(t, 9_900, gotToken.RemainQuota)
	require.NoError(t, DB.First(&gotTask, task.ID).Error)
	assert.Equal(t, 200, gotTask.Quota)
}

func TestTaskBillingLogOutboxClaimIsExclusiveAndRetryable(t *testing.T) {
	setupTaskBillingSettlementDB(t)
	outbox, err := CreateTaskBillingLogOutbox("claim-operation", "claim-task", `{}`)
	require.NoError(t, err)
	claimed, err := ClaimTaskBillingLogOutbox(outbox.ID)
	require.NoError(t, err)
	assert.True(t, claimed)
	claimed, err = ClaimTaskBillingLogOutbox(outbox.ID)
	require.NoError(t, err)
	assert.False(t, claimed)
	require.NoError(t, MarkTaskBillingLogOutboxFailed(outbox.ID, errors.New("temporary log failure")))
	claimed, err = ClaimTaskBillingLogOutbox(outbox.ID)
	require.NoError(t, err)
	assert.True(t, claimed)
	require.NoError(t, MarkTaskBillingLogOutboxDeliveredByID(outbox.ID))
	claimed, err = ClaimTaskBillingLogOutbox(outbox.ID)
	require.NoError(t, err)
	assert.False(t, claimed)
}

func TestPendingTaskBillingSettlementsExcludeUnfinishedTasks(t *testing.T) {
	setupTaskBillingSettlementDB(t)
	unfinished := Task{TaskID: "interaction-unfinished", Status: TaskStatusInProgress}
	finished := Task{TaskID: "interaction-finished", Status: TaskStatusSuccess}
	require.NoError(t, DB.Create(&unfinished).Error)
	require.NoError(t, DB.Create(&finished).Error)
	_, err := CreateTaskBillingSettlement(unfinished.TaskID, `{}`)
	require.NoError(t, err)
	_, err = CreateTaskBillingSettlement(finished.TaskID, `{}`)
	require.NoError(t, err)

	rows, err := GetPendingTaskBillingSettlements(1)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, finished.TaskID, rows[0].TaskID)
}
