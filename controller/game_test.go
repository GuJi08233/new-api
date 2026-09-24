package controller

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
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
		TokenAddress:       "0x64bf89340562AB4fE829a481E6797B012664425B",
		TokenSymbol:        "✦",
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

	reused, _ := accountRequest(t, router, http.MethodPost, "/api/game/withdraw", cookies, request, proof)
	assert.Equal(t, http.StatusForbidden, reused.Code)
	account, err = model.GetGameCoinAccount(user.Id)
	require.NoError(t, err)
	assert.Equal(t, 7, account.Balance)
}
