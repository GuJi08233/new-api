package controller

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 上一节点的结果经由下一任务的 payload 交接；本轮链配置暂不可用时也要保留上次的观察时间，
// 恢复后由服务判断实际停顿长度，而不能把失败的一轮当成成功的链上观察。
func TestGameCoinWithdrawHandlerCarriesNonceWatchAcrossTasks(t *testing.T) {
	env := newGameTestEnv(t, &operation_setting.GameSetting{})
	require.NoError(t, env.db.AutoMigrate(&model.SystemTask{}, &model.SystemTaskLock{}))
	watch := service.GameCoinNonceWatch{ChainId: 11155111, RpcURLHash: "rpc-address-digest", StartedAt: 1000, CheckedAt: 1600}
	result, err := common.Marshal(service.GameCoinWithdrawRunSummary{NonceWatch: &watch})
	require.NoError(t, err)
	require.NoError(t, env.db.Create(&model.SystemTask{
		TaskID: "previous-withdraw-run", Type: model.SystemTaskTypeGameCoinWithdraw,
		Status: model.SystemTaskStatusSucceeded, Result: string(result),
	}).Error)
	handler := gameCoinWithdrawHandler{}
	task, err := model.CreateSystemTask(handler.Type(), handler.NewPayload(), nil)
	require.NoError(t, err)
	var payload service.GameCoinNonceWatch
	require.NoError(t, task.DecodePayload(&payload))
	assert.Equal(t, watch, payload)
	claimed, ok, err := model.ClaimSystemTask(task.ID, handler.Type(), "next-node", common.GetTimestamp()+60)
	require.NoError(t, err)
	require.True(t, ok)
	handler.Run(context.Background(), claimed, "next-node")
	finished, err := model.GetSystemTaskByTaskID(task.TaskID)
	require.NoError(t, err)
	assert.Equal(t, model.SystemTaskStatusFailed, finished.Status)
	var summary service.GameCoinWithdrawRunSummary
	require.NoError(t, common.UnmarshalJsonStr(finished.Result, &summary))
	require.NotNil(t, summary.NonceWatch)
	assert.Equal(t, watch, *summary.NonceWatch)
	assert.Equal(t, "chain is not configured", summary.Skipped)
}

func TestGameCoinWithdrawHandlerStartsFreshWithoutValidCheckpoint(t *testing.T) {
	for _, result := range []string{"", "{}", "{invalid"} {
		t.Run(result, func(t *testing.T) {
			env := newGameTestEnv(t, &operation_setting.GameSetting{})
			require.NoError(t, env.db.AutoMigrate(&model.SystemTask{}))
			require.NoError(t, env.db.Create(&model.SystemTask{
				TaskID: "previous-withdraw-run", Type: model.SystemTaskTypeGameCoinWithdraw,
				Status: model.SystemTaskStatusFailed, Result: result,
			}).Error)
			handler := gameCoinWithdrawHandler{}
			payload := handler.NewPayload()
			watch, ok := payload.(*service.GameCoinNonceWatch)
			require.True(t, ok)
			require.NotNil(t, watch)
			assert.Equal(t, service.GameCoinNonceWatch{}, *watch)
		})
	}
}
