package controller

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/blackjack"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 关掉整个游戏中心之后不能再开新局，也不能兑换，但押着游戏币的一局还能回来下完：21 点照常停牌结算，对战房间里双方照常出拳，
// 不会眼看着后台按超时判负。游戏中心列表这时只列出这几个游戏，都结束以后整个游戏中心就打不开了。
func TestGameCenterClosedLetsPlayersFinishStakedRounds(t *testing.T) {
	require.NoError(t, i18n.Init())
	setting := gameSetting(func(s *operation_setting.GameSetting) {
		blackjackSetting(s)
		s.Rps = operation_setting.PvpRule{Enabled: true, MaxStake: 100, RakePercent: 5, MoveSeconds: 20}
	})
	env := newGameTestEnv(t, setting)
	player := env.player(100, gameTestIpA)
	rival := env.player(100, gameTestIpB)
	// 10、9 对庄家 6、10：停牌后庄家 16 点补 8 爆牌。
	stackBlackjackShoe(t, 10, 6, 9, 10, 8)
	player.ok(http.MethodPost, "/api/game/blackjack/deal", map[string]any{"bet": 10})
	created := player.ok(http.MethodPost, "/api/game/rooms", map[string]any{"game": gameRps, "stake": 10})
	roomPath := fmt.Sprintf("/api/game/rooms/%d", int(created["id"].(float64)))
	joined := rival.ok(http.MethodPost, roomPath+"/join", nil)

	setting.Enabled = false
	assert.Equal(t, gameMessage(i18n.MsgGamePlayDisabled), player.fails(http.MethodPost, "/api/game/blackjack/deal", map[string]any{"bet": 10}))
	assert.Equal(t, gameMessage(i18n.MsgGamePlayDisabled), rival.fails(http.MethodPost, "/api/game/rooms", map[string]any{"game": gameRps, "stake": 10}))
	assert.Equal(t, gameMessage(i18n.MsgGameCenterDisabled), player.fails(http.MethodPost, "/api/game/convert", map[string]any{"amount": 1}))

	hub := player.ok(http.MethodGet, "/api/game/self", nil)
	assert.Equal(t, true, hub["closed"])
	assert.Equal(t, false, hub["withdraw"].(map[string]any)["enabled"])
	var listed []string
	for _, game := range hub["games"].([]any) {
		listed = append(listed, game.(map[string]any)["key"].(string))
	}
	assert.ElementsMatch(t, []string{gameBlackjack, gameRps}, listed)
	assert.Equal(t, false, player.ok(http.MethodGet, "/api/game/blackjack", nil)["enabled"])

	moved := player.ok(http.MethodPost, roomPath+"/move", rpsMoveBody(joined["version"], 1, "rock"))
	rival.ok(http.MethodPost, roomPath+"/move", rpsMoveBody(moved["version"], 1, "scissors"))
	current := player.ok(http.MethodGet, "/api/game/blackjack/round", nil)
	stood := player.ok(http.MethodPost, "/api/game/blackjack/action", roundRequest(t, current, map[string]any{"action": blackjack.ActionStand}))
	assert.EqualValues(t, 20, roundOf(t, stood)["win"])
	player.ok(http.MethodPost, roomPath+"/resign", nil)

	assert.Equal(t, gameMessage(i18n.MsgGameCenterDisabled), player.fails(http.MethodGet, "/api/game/self", nil))
	assert.Equal(t, gameMessage(i18n.MsgGamePlayDisabled), player.fails(http.MethodGet, "/api/game/blackjack", nil))
}

// 累计时长要以服务端核实过的游戏过程为依据，不能只是开局以来过了多久：开了局挂着不动，最后交一份空记录、放弃或认输，
// 都攒不到时长(认输时只算最后一步到现在的一小段)。玩了多少步才算多久：2048 每一步最多 5 秒。
func TestIdleSessionsDoNotBankPlayTime(t *testing.T) {
	require.NoError(t, i18n.Init())
	env := newGameTestEnv(t, gameSetting(func(s *operation_setting.GameSetting) {
		s.Game2048.Enabled = true
		s.Sudoku.Enabled = true
		s.Gomoku = operation_setting.PvpRule{Enabled: true, MaxStake: 100, RakePercent: 5, MoveSeconds: 60, Ai: boardAiRule()}
	}))
	player := env.player(0, gameTestIpA)
	const idle = skillSessionTTL - 100
	secondsToday := func(result map[string]any) any {
		return result["progress"].(map[string]any)["seconds_today"]
	}

	empty := finish2048(player, start2048Session(t, env, player.user.Id, 1, idle).Id, "")
	require.Equal(t, true, empty["success"], empty["message"])
	assert.EqualValues(t, 0, secondsToday(empty["data"].(map[string]any)))
	few := finish2048(player, start2048Session(t, env, player.user.Id, 1, idle).Id, "0123")
	require.Equal(t, true, few["success"], few["message"])
	moved := few["data"].(map[string]any)["detail"].(map[string]any)["moves"].(float64)
	assert.EqualValues(t, moved*g2048SecondsPerMove, secondsToday(few["data"].(map[string]any)))

	sudokuSession, _ := startSudokuSession(t, env, player.user.Id, 1, idle)
	abandoned := player.ok(http.MethodPost, "/api/game/sudoku/abandon", map[string]any{"session_id": sudokuSession.Id})
	assert.EqualValues(t, 0, secondsToday(abandoned))

	started := player.ok(http.MethodPost, "/api/game/gomoku/ai/start", map[string]any{"level": 1})["session"].(map[string]any)
	longAgo := common.GetTimestamp() - idle
	require.NoError(t, env.db.Model(&model.GameSession{}).Where("id = ?", started["session_id"]).
		Updates(map[string]any{"created_at": longAgo, "played_at": longAgo}).Error)
	resigned := player.ok(http.MethodPost, "/api/game/gomoku/ai/resign", map[string]any{"session_id": started["session_id"]})
	assert.EqualValues(t, model.GameActionGapSeconds, secondsToday(resigned["result"].(map[string]any)))
}

// 游戏接口的请求体有上限：json.Decoder 会先把整个值读进内存，一个登录用户发一个巨大的请求就能耗尽内存。普通请求只有几个字段，
// 超过上限的请求直接拒绝，不扣币。
func TestGameRequestBodiesAreBounded(t *testing.T) {
	require.NoError(t, i18n.Init())
	env := newGameTestEnv(t, gameSetting(func(s *operation_setting.GameSetting) {
		s.Slots = operation_setting.WagerRule{Enabled: true, MaxBet: 5}
		s.Memory = operation_setting.SkillRule{Enabled: true, DailyRewards: 1, Tiers: []operation_setting.RewardTier{{Score: 1, Coins: 1}}}
	}))
	player := env.player(10, gameTestIpA)

	padding := strings.Repeat("a", gameRequestMaxBytes)
	player.fails(http.MethodPost, "/api/game/slots/spin", map[string]any{"bet": 1, "padding": padding})
	player.fails(http.MethodPost, "/api/game/memory/start", map[string]any{"difficulty": 1, "padding": padding})
	assert.Equal(t, 10, player.balance(), "被拒绝的下注没有扣币")
	player.ok(http.MethodPost, "/api/game/slots/spin", map[string]any{"bet": 1})
}

// 兑换限额按账号累计，与同 IP 的其他账号和全站提现池分别计算。
func TestGameCoinConvertDailyLimitOverHTTP(t *testing.T) {
	require.NoError(t, i18n.Init())
	env := newGameTestEnv(t, gameSetting(func(setting *operation_setting.GameSetting) {
		setting.WithdrawDailyLimit = 1000
	}))
	player := env.player(50, gameTestIpA)
	other := env.player(50, gameTestIpA)
	require.NoError(t, env.db.Create(&model.GameCoinWithdrawDay{Day: gameCenterDay(), Used: 100}).Error)

	info := player.ok(http.MethodGet, "/api/game/self", nil)
	assert.Equal(t, map[string]any{"daily_limit": float64(15), "daily_used": float64(0), "daily_remaining": float64(15)}, info["convert"])
	assert.EqualValues(t, 900, info["withdraw"].(map[string]any)["daily_remaining"])

	result := player.ok(http.MethodPost, "/api/game/convert", map[string]any{"amount": 10})
	assert.EqualValues(t, 10*common.QuotaPerUnit, result["quota"])
	assert.EqualValues(t, 40, result["balance"])
	info = player.ok(http.MethodGet, "/api/game/self", nil)
	assert.Equal(t, map[string]any{"daily_limit": float64(15), "daily_used": float64(10), "daily_remaining": float64(5)}, info["convert"])
	assert.EqualValues(t, 900, info["withdraw"].(map[string]any)["daily_remaining"])

	message := player.fails(http.MethodPost, "/api/game/convert", map[string]any{"amount": 6})
	assert.Equal(t, i18n.Translate(i18n.DefaultLang, i18n.MsgGameCoinConvertDailyLimit, map[string]any{"Limit": 15}), message)
	assert.Equal(t, 40, player.balance(), "超额请求不扣游戏币")
	player.ok(http.MethodPost, "/api/game/convert", map[string]any{"amount": 5})
	player.fails(http.MethodPost, "/api/game/convert", map[string]any{"amount": 1})
	info = player.ok(http.MethodGet, "/api/game/self", nil)
	assert.Equal(t, map[string]any{"daily_limit": float64(15), "daily_used": float64(15), "daily_remaining": float64(0)}, info["convert"])
	assert.Equal(t, 35, player.balance())
	quota, err := model.GetUserQuota(player.user.Id, true)
	require.NoError(t, err)
	assert.EqualValues(t, 15*common.QuotaPerUnit, quota)

	other.ok(http.MethodPost, "/api/game/convert", map[string]any{"amount": 15})
	assert.Equal(t, 35, other.balance(), "同 IP 的另一个账号仍有自己的兑换限额")
	used, err := model.GetGameCoinWithdrawDayUsed(gameCenterDay())
	require.NoError(t, err)
	assert.Equal(t, 100, used, "个人兑换不占用全站提现池")
}

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

	// 全站当日额度不够时直接拒绝，不必先做安全验证：验证之后才被拒绝的话，验证白做，重试还要再验证一次。
	require.NoError(t, db.Create(&model.GameCoinWithdrawDay{Day: gameCenterDay(), Used: 98}).Error)
	full, fullBody := accountRequest(t, router, http.MethodPost, "/api/game/withdraw", cookies, request, "")
	assert.Equal(t, http.StatusOK, full.Code, "没有要求安全验证")
	assert.Equal(t, false, fullBody["success"])
	require.NoError(t, db.Where("day = ?", gameCenterDay()).Delete(&model.GameCoinWithdrawDay{}).Error)

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

	// 别的节点刚把俄罗斯方块紧急关掉、把单次提现最多改成 20，库里已经是新值，本节点的快照还没同步。这边只改俄罗斯方块的
	// 每日领奖次数：以数据库里的配置为底合并，游戏不会被打开；跨字段校验也对着库里的单次最多做。
	_, opened := accountRequest(t, router, http.MethodPut, "/api/game/admin/setting", nil, map[string]any{"tetris": map[string]any{"enabled": true}}, "")
	require.Equal(t, true, opened["success"], opened["message"])
	closedTetris := operation_setting.GetGameSetting().Tetris
	closedTetris.Enabled = false
	stored, err := common.Marshal(closedTetris)
	require.NoError(t, err)
	require.NoError(t, db.Save(&model.Option{Key: operation_setting.GameSettingPrefix + "tetris", Value: string(stored)}).Error)
	require.NoError(t, db.Save(&model.Option{Key: operation_setting.GameSettingPrefix + "withdraw_max_amount", Value: "20"}).Error)
	require.True(t, operation_setting.GetGameSetting().Tetris.Enabled, "本节点的快照还没同步")
	_, stale := accountRequest(t, router, http.MethodPut, "/api/game/admin/setting", nil, map[string]any{"tetris": map[string]any{"daily_rewards": 5}}, "")
	require.Equal(t, true, stale["success"], stale["message"])
	assert.False(t, operation_setting.GetGameSetting().Tetris.Enabled, "别人关掉的游戏没有被这次保存打开")
	assert.Equal(t, 5, operation_setting.GetGameSetting().Tetris.DailyRewards)
	_, staleCross := accountRequest(t, router, http.MethodPut, "/api/game/admin/setting", nil, map[string]any{"withdraw_min_amount": 50}, "")
	assert.Equal(t, false, staleCross["success"], "单次最少大于库里的单次最多")
}
