package model

import (
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuditLogRecordQueryAndCleanup(t *testing.T) {
	// 重复迁移必须幂等（启动多次不能报错或重复建表）。
	require.NoError(t, DB.AutoMigrate(&AuditLog{}))
	require.NoError(t, DB.AutoMigrate(&AuditLog{}))
	require.NoError(t, DB.Where("1 = 1").Delete(&AuditLog{}).Error)
	t.Cleanup(func() { DB.Where("1 = 1").Delete(&AuditLog{}) })

	now := common.GetTimestamp()
	adminOp := map[string]any{"admin_id": 7, "admin_username": "ops", "admin_role": common.RoleAdminUser, "auth_method": "session"}
	request := AuditLog{ActorRole: common.RoleAdminUser, Method: "PUT", Route: "/api/channel/", RequestId: "req-admin"}
	RecordOperationAuditLog(7, "Updated channel c1 (ID: 3)", "10.0.0.1", "channel.update",
		map[string]any{"id": 3, "big": json.Number("9007199254740993")}, adminOp, nil, &request)

	RecordAuditLog(AuditLog{UserId: 8, Username: "alice", ActorRole: common.RoleCommonUser, CreatedAt: now - 10,
		Category: AuditCategorySecurity, Action: "token.key_view", Status: 200, Success: true, RequestId: "req-user",
		Other: AuditOther{Op: &AuditOperation{Action: "token.key_view", Params: AuditFields{"id": 5}},
			AdminInfo: &AuditAdminInfo{AdminID: 8}}})
	RecordAuditLog(AuditLog{UserId: 1, Username: "root", ActorRole: common.RoleRootUser, CreatedAt: now - 20,
		Category: AuditCategoryOperation, Action: "option.update", Success: false, RequestId: "req-root"})
	RecordAuditLog(AuditLog{UserId: 9, Username: "old", ActorRole: common.RoleAdminUser, CreatedAt: now - 400*24*3600,
		Category: AuditCategoryOperation, Action: "channel.delete", Success: true})

	// 管理操作双写：logs 表 type=3 照写，audit_logs 多一条结构化记录。
	var manageLogs int64
	require.NoError(t, LOG_DB.Model(&Log{}).Where("type = ? AND user_id = ?", LogTypeManage, 7).Count(&manageLogs).Error)
	assert.Equal(t, int64(1), manageLogs)

	logs, total, err := GetAuditLogs(AuditLogFilter{RequestId: "req-admin"}, 0, 10, common.RoleAdminUser)
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	got := logs[0]
	assert.Equal(t, AuditCategoryOperation, got.Category)
	assert.Equal(t, "channel.update", got.Action)
	assert.Equal(t, "10.0.0.1", got.Ip)
	assert.True(t, got.Success)
	assert.NotEmpty(t, got.EventId)
	require.NotNil(t, got.Other.AdminInfo)
	assert.Equal(t, "ops", got.Other.AdminInfo.AdminUsername)
	require.NotNil(t, got.Other.Op)
	assert.Equal(t, json.RawMessage("9007199254740993"), got.Other.Op.Params["big"])

	// 非 root 看不到 root 的记录；root 能看到全部。
	_, adminTotal, err := GetAuditLogs(AuditLogFilter{}, 0, 10, common.RoleAdminUser)
	require.NoError(t, err)
	assert.Equal(t, int64(3), adminTotal)
	_, rootTotal, err := GetAuditLogs(AuditLogFilter{}, 0, 10, common.RoleRootUser)
	require.NoError(t, err)
	assert.Equal(t, int64(4), rootTotal)

	// 过滤条件：分类、action、成功与否、时间范围。
	failed := false
	_, n, err := GetAuditLogs(AuditLogFilter{Success: &failed}, 0, 10, common.RoleRootUser)
	require.NoError(t, err)
	assert.Equal(t, int64(1), n)
	_, n, err = GetAuditLogs(AuditLogFilter{Category: AuditCategorySecurity, Action: "token.key_view"}, 0, 10, common.RoleAdminUser)
	require.NoError(t, err)
	assert.Equal(t, int64(1), n)
	_, n, err = GetAuditLogs(AuditLogFilter{StartTimestamp: now - 3600}, 0, 10, common.RoleRootUser)
	require.NoError(t, err)
	assert.Equal(t, int64(3), n)

	// 用户看自己的记录时裁剪管理员明细。
	self, n, err := GetAuditLogs(AuditLogFilter{UserId: 8, SelfView: true}, 0, 10, common.RoleCommonUser)
	require.NoError(t, err)
	require.Equal(t, int64(1), n)
	assert.Nil(t, self[0].Other.AdminInfo)
	require.NotNil(t, self[0].Other.Op)

	// 按保留期分批清理，只删过期记录。
	deleted, err := DeleteOldAuditLogBatch(now-180*24*3600, 100)
	require.NoError(t, err)
	assert.Equal(t, int64(1), deleted)
	_, rootTotal, err = GetAuditLogs(AuditLogFilter{}, 0, 10, common.RoleRootUser)
	require.NoError(t, err)
	assert.Equal(t, int64(3), rootTotal)
}
