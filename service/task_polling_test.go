package service

import (
	"context"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

type testTaskPollingAdaptor struct {
	adjustCalled bool
}

func (a *testTaskPollingAdaptor) Init(info *relaycommon.RelayInfo) {}

func (a *testTaskPollingAdaptor) FetchTask(baseURL string, key string, body map[string]any, proxy string) (*http.Response, error) {
	return nil, nil
}

func (a *testTaskPollingAdaptor) ParseTaskResult(body []byte) (*relaycommon.TaskInfo, error) {
	return nil, nil
}

func (a *testTaskPollingAdaptor) AdjustBillingOnComplete(task *model.Task, taskResult *relaycommon.TaskInfo) int {
	a.adjustCalled = true
	return 123
}

func TestSettleTaskBillingOnCompleteUsesInjectedVideoBilling(t *testing.T) {
	previous := SettleVideoTaskBillingOnSuccessFunc
	defer func() {
		SettleVideoTaskBillingOnSuccessFunc = previous
	}()

	called := false
	SettleVideoTaskBillingOnSuccessFunc = func(ctx context.Context, task *model.Task, taskResult *relaycommon.TaskInfo) (bool, error) {
		called = true
		if task.TaskID != "task_video" {
			t.Fatalf("unexpected task id: %s", task.TaskID)
		}
		if taskResult.TotalTokens != 42 {
			t.Fatalf("unexpected total tokens: %d", taskResult.TotalTokens)
		}
		return true, nil
	}

	adaptor := &testTaskPollingAdaptor{}
	settleTaskBillingOnComplete(context.Background(), adaptor, &model.Task{TaskID: "task_video"}, &relaycommon.TaskInfo{TotalTokens: 42})

	if !called {
		t.Fatal("expected injected video billing hook to be called")
	}
	if adaptor.adjustCalled {
		t.Fatal("generic billing should not run after video billing hook handled the task")
	}
}
