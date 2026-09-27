package controller

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 热钱包私钥只写不读：保存后立即生效并落库，但任何接口都只给出它对应的地址，不会把私钥带出去。
func TestGameAdminHotWalletKeyIsWriteOnly(t *testing.T) {
	const key = "4c0883a69102937d6231471b5dbb6204fe5129617082792ae468d01a3f362318"
	t.Setenv(service.GameHotWalletPrivateKeyEnv, "")
	db, router, _ := accountSecurityFixture(t)
	require.NoError(t, db.AutoMigrate(&model.Option{}))
	previousOptions, previousKey := common.OptionMap, operation_setting.GetGameHotWalletPrivateKey()
	common.OptionMap = map[string]string{}
	t.Cleanup(func() {
		common.OptionMap = previousOptions
		operation_setting.SetGameHotWalletPrivateKey(previousKey)
	})
	router.PUT("/api/game/admin/hot_wallet", UpdateGameAdminHotWallet)
	router.DELETE("/api/game/admin/hot_wallet", ClearGameAdminHotWallet)
	router.GET("/api/game/admin/setting", GetGameAdminSetting)
	router.GET("/api/option/", GetOptions)

	saved, response := accountRequest(t, router, http.MethodPut, "/api/game/admin/hot_wallet", nil, map[string]string{"private_key": "0x" + key}, "")
	require.Equal(t, true, response["success"], response["message"])
	wallet := response["data"].(map[string]any)
	assert.Equal(t, "0x2c7536E3605D9C16a7a3D7b1898e529396a65c23", wallet["address"])
	assert.Equal(t, service.GameHotWalletSourceSetting, wallet["source"])
	assert.NotContains(t, saved.Body.String(), key)
	for _, path := range []string{"/api/game/admin/setting", "/api/option/"} {
		read, _ := accountRequest(t, router, http.MethodGet, path, nil, nil, "")
		require.Equal(t, http.StatusOK, read.Code, path)
		assert.NotContains(t, read.Body.String(), key, path)
	}
	var option model.Option
	require.NoError(t, db.Where(&model.Option{Key: operation_setting.GameHotWalletPrivateKeyOption}).First(&option).Error)
	assert.Equal(t, key, option.Value, "私钥落库，重启和多节点同步后仍然生效")

	_, rejected := accountRequest(t, router, http.MethodPut, "/api/game/admin/hot_wallet", nil, map[string]string{"private_key": "0x1234"}, "")
	assert.Equal(t, false, rejected["success"])
	address, _, err := service.GameHotWalletAddress()
	require.NoError(t, err)
	assert.Equal(t, "0x2c7536E3605D9C16a7a3D7b1898e529396a65c23", address, "无效私钥不能覆盖已保存的钱包")

	_, cleared := accountRequest(t, router, http.MethodDelete, "/api/game/admin/hot_wallet", nil, nil, "")
	require.Equal(t, true, cleared["success"], cleared["message"])
	assert.Equal(t, false, cleared["data"].(map[string]any)["configured"], "没有环境变量兜底时清除即未配置")
}

// 游戏币离站之前必须通过安全验证，证明只对它绑定的收款地址和数量有效，且只能用一次。
func TestWithdrawGameCoinsRequiresProofBoundToAddressAndAmount(t *testing.T) {
	t.Setenv(service.GameHotWalletPrivateKeyEnv, "0x4c0883a69102937d6231471b5dbb6204fe5129617082792ae468d01a3f362318")
	db, router, user := accountSecurityFixture(t)
	require.NoError(t, db.AutoMigrate(&model.GameCoinAccount{}, &model.GameCoinLog{}, &model.GameCoinWithdrawal{}, &model.GameCoinWithdrawDay{}))
	previousSetting := operation_setting.GetGameSetting()
	operation_setting.SetGameSettingForTest(&operation_setting.GameSetting{
		Enabled:            true,
		WithdrawEnabled:    true,
		WithdrawDailyLimit: 100,
		WithdrawMinAmount:  1,
		WithdrawMaxAmount:  10,
		ChainId:            11155111,
		RpcUrl:             "http://127.0.0.1:1",
		TokenAddress:       "0x64bf89340562ab4fe829a481e6797b012664425b",
		TokenSymbol:        "✦",
		TokenDecimals:      6,
		Confirmations:      1,
	})
	t.Cleanup(func() { operation_setting.SetGameSettingForTest(previousSetting) })
	router.POST("/api/game/withdraw", middleware.UserAuth(), middleware.GameCenterEnabled(), WithdrawGameCoins)
	_, err := model.AdjustGameCoins(user.Id, 10, "seed")
	require.NoError(t, err)

	login, _ := accountRequest(t, router, http.MethodPost, "/primary", nil, nil, "")
	cookies := login.Result().Cookies()
	request := map[string]any{"address": "0x8ba1f109551bd432803012645ac136ddd64dba72", "amount": 3}

	denied, challenge := accountRequest(t, router, http.MethodPost, "/api/game/withdraw", cookies, request, "")
	require.Equal(t, http.StatusForbidden, denied.Code)
	assert.Equal(t, "game.withdraw", challenge["scope"])
	_, verified := accountRequest(t, router, http.MethodPost, "/api/verify", cookies, map[string]any{
		"method": "password", "password": "correct-password", "scope": challenge["scope"], "context_hash": challenge["context_hash"],
	}, "")
	require.Equal(t, true, verified["success"])
	proof := verified["data"].(map[string]any)["proof_token"].(string)

	larger, _ := accountRequest(t, router, http.MethodPost, "/api/game/withdraw", cookies, map[string]any{"address": request["address"], "amount": 4}, proof)
	assert.Equal(t, http.StatusForbidden, larger.Code, "证明不能拿去提走另一个数量")
	otherAddress, _ := accountRequest(t, router, http.MethodPost, "/api/game/withdraw", cookies, map[string]any{"address": "0x0000000000000000000000000000000000000001", "amount": 3}, proof)
	assert.Equal(t, http.StatusForbidden, otherAddress.Code, "证明不能拿去提到另一个地址")
	account, err := model.GetGameCoinAccount(user.Id)
	require.NoError(t, err)
	assert.Equal(t, 10, account.Balance)

	_, created := accountRequest(t, router, http.MethodPost, "/api/game/withdraw", cookies, request, proof)
	require.Equal(t, true, created["success"], created["message"])
	withdrawal := created["data"].(map[string]any)["withdrawal"].(map[string]any)
	assert.Equal(t, "0x8ba1f109551bD432803012645Ac136ddd64DBA72", withdrawal["to_address"], "收款地址按 EIP-55 格式保存")
	assert.Equal(t, model.GameCoinWithdrawalStatusPending, withdrawal["status"])
	// 申请时就记下要发放的代币，worker 只按它发。
	assert.EqualValues(t, 11155111, withdrawal["chain_id"])
	assert.Equal(t, "0x64bf89340562AB4fE829a481E6797B012664425B", withdrawal["token_address"])
	assert.EqualValues(t, 6, withdrawal["token_decimals"])

	reused, _ := accountRequest(t, router, http.MethodPost, "/api/game/withdraw", cookies, request, proof)
	assert.Equal(t, http.StatusForbidden, reused.Code)
	account, err = model.GetGameCoinAccount(user.Id)
	require.NoError(t, err)
	assert.Equal(t, 7, account.Balance)
}

// 管理页只提交改动过的字段：页面打开之后别人关掉了提现，这边只改俄罗斯方块的规则，保存不会把提现又打开；游戏规则里
// 也只带改动过的子字段。跨字段约束按合并到当前配置之后的整份配置校验，未知字段直接拒绝；审计日志记下改了哪些字段。
func TestUpdateGameAdminSettingWritesOnlySubmittedFields(t *testing.T) {
	db, router, _ := accountSecurityFixture(t)
	require.NoError(t, db.AutoMigrate(&model.Option{}))
	previousOptions, previousSetting := common.OptionMap, operation_setting.GetGameSetting()
	common.OptionMap = map[string]string{}
	t.Cleanup(func() {
		values, err := config.ConfigToMap(previousSetting)
		require.NoError(t, err)
		restore := make(map[string]string, len(values))
		for key, value := range values {
			restore[operation_setting.GameSettingPrefix+key] = value
		}
		require.NoError(t, model.UpdateOptionsBulk(restore))
		common.OptionMap = previousOptions
	})
	router.PUT("/api/game/admin/setting", UpdateGameAdminSetting)

	require.NoError(t, model.UpdateOptionsBulk(map[string]string{
		operation_setting.GameSettingPrefix + "withdraw_enabled":    "true",
		operation_setting.GameSettingPrefix + "withdraw_min_amount": "1",
		operation_setting.GameSettingPrefix + "withdraw_max_amount": "100",
	}))
	require.NoError(t, model.UpdateOption(operation_setting.GameSettingPrefix+"withdraw_enabled", "false"))

	tetris := operation_setting.GetGameSetting().Tetris
	tetris.DailyRewards = 7
	_, saved := accountRequest(t, router, http.MethodPut, "/api/game/admin/setting", nil, map[string]any{"tetris": tetris}, "")
	require.Equal(t, true, saved["success"], saved["message"])
	assert.Equal(t, 7, operation_setting.GetGameSetting().Tetris.DailyRewards)
	assert.False(t, operation_setting.GetGameSetting().WithdrawEnabled, "没有提交的字段保持别人刚改的值")
	var audit model.Log
	require.NoError(t, db.Where("content LIKE ?", "Updated game center settings%").Last(&audit).Error)
	assert.Equal(t, "Updated game center settings: tetris", audit.Content)

	// 游戏规则只带改动过的子字段时合并进当前规则：别人刚关掉了老虎机，这边只改单次最多下注，老虎机不会又被打开。
	require.NoError(t, model.UpdateOption(operation_setting.GameSettingPrefix+"slots", `{"enabled":false,"max_bet":5}`))
	_, partial := accountRequest(t, router, http.MethodPut, "/api/game/admin/setting", nil, map[string]any{"slots": map[string]any{"max_bet": 9}}, "")
	require.Equal(t, true, partial["success"], partial["message"])
	assert.Equal(t, operation_setting.WagerRule{Enabled: false, MaxBet: 9}, operation_setting.GetGameSetting().Slots)

	// 奖励档位整份替换，读出的快照不受之后的改动影响。
	before := operation_setting.GetGameSetting()
	_, tiers := accountRequest(t, router, http.MethodPut, "/api/game/admin/setting", nil, map[string]any{"2048": map[string]any{"tiers": []map[string]int{{"score": 100, "coins": 1}}}}, "")
	require.Equal(t, true, tiers["success"], tiers["message"])
	assert.Equal(t, []operation_setting.RewardTier{{Score: 100, Coins: 1}}, operation_setting.GetGameSetting().Game2048.Tiers)
	assert.Equal(t, 6000, before.Game2048.Tiers[0].Score, "保存前读出的快照不变")

	_, crossField := accountRequest(t, router, http.MethodPut, "/api/game/admin/setting", nil, map[string]any{"withdraw_min_amount": 500}, "")
	assert.Equal(t, false, crossField["success"], "单次最少大于当前的单次最多")
	assert.Equal(t, 1, operation_setting.GetGameSetting().WithdrawMinAmount)

	_, unknown := accountRequest(t, router, http.MethodPut, "/api/game/admin/setting", nil, map[string]any{"no_such_field": 1}, "")
	assert.Equal(t, false, unknown["success"])
}
