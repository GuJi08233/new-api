package controller

import (
	"net/http"
	"testing"

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

// 每天先免费转，免费次数用完之后每次按每次花费扣币、不限次数，奖品 = 每次花费 × 格子的倍数。免费旋转的奖品计入累计获得，
// 付费旋转计入累计下注与下注输赢；请求写明要免费转但免费次数已经用完时拒绝，不会改成扣币。
func TestSpinWheelFreeThenPaidWithoutDailyLimit(t *testing.T) {
	env := newGameTestEnv(t, gameSetting(func(s *operation_setting.GameSetting) {
		s.Wheel = operation_setting.WheelRule{Enabled: true, FreeSpins: 1, SpinCost: 2}
	}))
	player := env.player(3, gameTestIpA)

	info := player.ok(http.MethodGet, "/api/game/wheel", nil)
	assert.InDelta(t, 0.8, info["payback"], 1e-9)
	assert.Len(t, info["segments"], 8)
	assert.EqualValues(t, 0, info["free_used_today"])

	cases := []struct {
		free        bool
		segment     int
		wantErr     bool
		wantWin     int
		wantBalance int
		wantNet     int
	}{
		// 3 倍格：免费旋转不扣币，奖品 2 × 3。
		{free: true, segment: 2, wantWin: 6, wantBalance: 9, wantNet: 6},
		{free: true, segment: 2, wantErr: true},
		// 谢谢参与：扣掉 2 个。
		{segment: 3, wantWin: 0, wantBalance: 7, wantNet: 4},
		{segment: 3, wantWin: 0, wantBalance: 5, wantNet: 2},
		// 头奖 50 倍：扣 2 派 100。
		{segment: 0, wantWin: 100, wantBalance: 103, wantNet: 100},
		{segment: 3, wantWin: 0, wantBalance: 101, wantNet: 98},
	}
	for i, c := range cases {
		stopWheelAt(t, c.segment)
		if c.wantErr {
			player.fails(http.MethodPost, "/api/game/wheel/spin", map[string]any{"free": c.free})
			continue
		}
		result := player.ok(http.MethodPost, "/api/game/wheel/spin", map[string]any{"free": c.free})
		assert.EqualValues(t, c.wantWin, result["win"], "case %d win", i)
		assert.EqualValues(t, c.wantBalance, result["balance"], "case %d balance", i)
		assert.EqualValues(t, c.wantNet, result["net_today"], "case %d net today", i)
	}
	account, err := model.GetGameCoinAccount(player.user.Id)
	require.NoError(t, err)
	assert.Equal(t, 3+6, account.TotalEarned, "种子币加上免费旋转的奖品")
	// 免费旋转没有下注，每次中奖都和技巧游戏的奖励一样记审计日志；付费旋转按净赢数量，净赢 98 个不记。
	var rewardLogs, bigWinLogs int64
	require.NoError(t, env.db.Model(&model.Log{}).Where("user_id = ? AND type = ? AND content LIKE ?", player.user.Id, model.LogTypeSystem, "游戏奖励%").Count(&rewardLogs).Error)
	require.NoError(t, env.db.Model(&model.Log{}).Where("user_id = ? AND type = ? AND content LIKE ?", player.user.Id, model.LogTypeSystem, "游戏大奖%").Count(&bigWinLogs).Error)
	assert.EqualValues(t, 1, rewardLogs)
	assert.Zero(t, bigWinLogs)
	assert.Equal(t, 8, account.TotalWagered)
	assert.Equal(t, 92, account.TotalWagerNet)
}

// 技巧游戏已经领满当天奖励后，默认的一次免费旋转仍能领到完整的 50 币头奖；账号与同一 IP 的免费次数限制继续生效。
func TestSpinWheelPaysFullFreePrizeAfterSkillRewardLimit(t *testing.T) {
	env := newGameTestEnv(t, gameSetting(func(s *operation_setting.GameSetting) {
		s.Wheel.Enabled = true
		s.FreeDailyLimit = 3
	}))
	first := env.player(0, gameTestIpA)
	second := env.player(0, gameTestIpA)
	other := env.player(0, gameTestIpB)
	session, err := model.CreateGameSession(first.user.Id, "tetris", 7, "")
	require.NoError(t, err)
	skill, err := model.FinishGameSession(session, model.GameSettlement{
		Score: 5000, Reward: 3, DailyRewards: 1, FreeDailyLimit: 3, Day: gameCenterDay(), Ip: gameTestIpA,
	})
	require.NoError(t, err)
	require.Equal(t, 3, skill.Session.Reward)

	info := first.ok(http.MethodGet, "/api/game/wheel", nil)
	assert.EqualValues(t, 1, info["free_spins"])
	assert.NotContains(t, info, "free_daily_limit")
	stopWheelAt(t, 0) // 默认花费 1，头奖 50 倍。
	result := first.ok(http.MethodPost, "/api/game/wheel/spin", map[string]any{"free": true})
	assert.EqualValues(t, 50, result["win"])
	assert.EqualValues(t, 53, result["balance"])
	assert.Empty(t, result["limited_by"])
	assert.EqualValues(t, 1, result["free_used_today"])
	assert.EqualValues(t, 1, result["ip_free_used_today"])
	first.fails(http.MethodPost, "/api/game/wheel/spin", map[string]any{"free": true})
	second.fails(http.MethodPost, "/api/game/wheel/spin", map[string]any{"free": true})

	result = other.ok(http.MethodPost, "/api/game/wheel/spin", map[string]any{"free": true})
	assert.EqualValues(t, 50, result["win"], "别的 IP 有自己的免费次数")
	assert.Equal(t, 53, first.balance())
	assert.Zero(t, second.balance())
	assert.Equal(t, 50, other.balance())
}
