package controller

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stopWheelAt 让接下来的旋转都停在给定的格子上。
func stopWheelAt(t *testing.T, segment int) {
	t.Helper()
	previous := spinWheel
	spinWheel = func() (int, error) { return segment, nil }
	t.Cleanup(func() { spinWheel = previous })
}

// 每天先免费转一次，之后每次按每次花费扣币，奖品 = 每次花费 × 格子的倍数。含免费那次在内到了每日次数就不能再转；
// 免费旋转的奖品计入累计获得，付费旋转计入累计下注与下注输赢。派彩达到 10 倍的大奖记一条带 IP 的审计日志。
func TestSpinWheelFreeThenPaidUntilDailyLimit(t *testing.T) {
	rule := operation_setting.WheelRule{Enabled: true, FreeSpins: 1, DailySpins: 3, SpinCost: 2}
	db, router, user, cookies := spinGameFixture(t, &operation_setting.GameSetting{Enabled: true, Wheel: rule}, 3)

	wheelInfo := func() map[string]any {
		_, self := accountRequest(t, router, http.MethodGet, "/api/game/self", cookies, nil, "")
		require.Equal(t, true, self["success"], self["message"])
		for _, item := range self["data"].(map[string]any)["games"].([]any) {
			if item.(map[string]any)["key"] == gameWheel {
				return item.(map[string]any)
			}
		}
		return nil
	}
	game := wheelInfo()
	require.NotNil(t, game, "开放的大转盘出现在游戏列表里")
	assert.InDelta(t, 0.8, game["payback"], 1e-9)
	assert.Len(t, game["segments"], 8)
	assert.EqualValues(t, 0, game["free_used_today"])

	cases := []struct {
		free        bool
		segment     int
		wantErr     bool
		wantWin     int
		wantBalance int
		wantPlays   int
		wantEarned  int
	}{
		// 3 倍格：免费旋转不扣币，奖品 2 × 3。
		{free: true, segment: 2, wantWin: 6, wantBalance: 9, wantPlays: 1, wantEarned: 6},
		{free: true, segment: 2, wantErr: true},
		// 谢谢参与：扣掉 2 个。
		{segment: 3, wantWin: 0, wantBalance: 7, wantPlays: 2, wantEarned: 4},
		// 头奖 50 倍：扣 2 派 100。
		{segment: 0, wantWin: 100, wantBalance: 105, wantPlays: 3, wantEarned: 102},
		{segment: 3, wantErr: true},
	}
	for i, c := range cases {
		stopWheelAt(t, c.segment)
		_, response := accountRequest(t, router, http.MethodPost, "/api/game/wheel/spin", cookies, map[string]any{"free": c.free}, "")
		if c.wantErr {
			assert.Equal(t, false, response["success"], "case %d", i)
			continue
		}
		require.Equal(t, true, response["success"], "case %d: %v", i, response["message"])
		result := response["data"].(map[string]any)
		assert.EqualValues(t, c.segment, result["segment"], "case %d segment", i)
		assert.EqualValues(t, c.wantWin, result["win"], "case %d win", i)
		assert.EqualValues(t, c.wantBalance, result["balance"], "case %d balance", i)
		assert.EqualValues(t, c.wantPlays, result["plays_today"], "case %d plays", i)
		assert.EqualValues(t, 1, result["free_used_today"], "case %d free used", i)
		assert.EqualValues(t, c.wantEarned, result["earned_today"], "case %d earned", i)
	}

	game = wheelInfo()
	assert.EqualValues(t, 3, game["plays_today"])
	assert.EqualValues(t, 1, game["ip_free_used_today"])
	assert.EqualValues(t, 102, game["earned_today"])
	assert.EqualValues(t, 100, game["best_score"])
	account, err := model.GetGameCoinAccount(user.Id)
	require.NoError(t, err)
	assert.Equal(t, 105, account.Balance)
	assert.Equal(t, 9, account.TotalEarned, "初始的 3 个加上免费旋转中的 6 个")
	assert.Equal(t, 4, account.TotalWagered)
	assert.Equal(t, 96, account.TotalWagerNet)
	var sessions int64
	require.NoError(t, db.Model(&model.GameSession{}).Where("user_id = ? AND game = ?", user.Id, gameWheel).Count(&sessions).Error)
	assert.EqualValues(t, 3, sessions)
	var bigWinLogs int64
	require.NoError(t, db.Model(&model.Log{}).Where("user_id = ? AND type = ? AND content LIKE ?", user.Id, model.LogTypeSystem, "游戏大奖%").Count(&bigWinLogs).Error)
	assert.EqualValues(t, 1, bigWinLogs, "只有头奖那一次是大奖")
}

// 要求人机验证时，免费旋转没有带 Turnstile 令牌就拒绝，不会用掉免费次数；付费旋转不需要验证，余额不够时拒绝且不扣币。
// 大转盘没开放时不能转。
func TestSpinWheelChecksTurnstileBalanceAndSwitch(t *testing.T) {
	setting := &operation_setting.GameSetting{
		Enabled:              true,
		PlayTurnstileEnabled: true,
		Wheel:                operation_setting.WheelRule{Enabled: true, FreeSpins: 1, DailySpins: 10, SpinCost: 2},
	}
	db, router, user, cookies := spinGameFixture(t, setting, 2)
	previousTurnstile := common.TurnstileCheckEnabled
	common.TurnstileCheckEnabled = true
	t.Cleanup(func() { common.TurnstileCheckEnabled = previousTurnstile })
	stopWheelAt(t, 3)

	spin := func(free bool) map[string]any {
		_, response := accountRequest(t, router, http.MethodPost, "/api/game/wheel/spin", cookies, map[string]any{"free": free}, "")
		return response
	}
	assert.Equal(t, false, spin(true)["success"], "免费旋转要先过人机验证")
	paid := spin(false)
	require.Equal(t, true, paid["success"], paid["message"])
	assert.EqualValues(t, 0, paid["data"].(map[string]any)["balance"])
	assert.EqualValues(t, 0, paid["data"].(map[string]any)["free_used_today"], "被拒绝的免费旋转没有用掉免费次数")
	assert.Equal(t, false, spin(false)["success"], "余额不够")

	operation_setting.SetGameSettingForTest(&operation_setting.GameSetting{Enabled: true, Wheel: operation_setting.WheelRule{FreeSpins: 1, DailySpins: 10, SpinCost: 2}})
	assert.Equal(t, false, spin(true)["success"], "大转盘没开放")

	var sessions int64
	require.NoError(t, db.Model(&model.GameSession{}).Where("user_id = ?", user.Id).Count(&sessions).Error)
	assert.EqualValues(t, 1, sessions)
}
