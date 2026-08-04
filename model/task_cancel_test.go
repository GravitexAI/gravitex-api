package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeleteTaskByID(t *testing.T) {
	task := &Task{
		TaskID:    "task_delete_me",
		UserId:    9001,
		ChannelId: 1,
		Status:    TaskStatusSuccess,
	}
	require.NoError(t, DB.Create(task).Error)

	require.NoError(t, DeleteTaskByID(task.ID))

	_, exist, err := GetByTaskId(9001, "task_delete_me")
	require.NoError(t, err)
	assert.False(t, exist)
}

func TestTaskStatusCancelledIsDistinctValue(t *testing.T) {
	assert.Equal(t, TaskStatus("CANCELLED"), TaskStatusCancelled)
	assert.NotEqual(t, TaskStatusCancelled, TaskStatusFailure)
	assert.NotEqual(t, TaskStatusCancelled, TaskStatusSuccess)
}
