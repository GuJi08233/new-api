package controller

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestWheelRejectsPaidSpinFromPreviousAccountingDay(t *testing.T) {
	env := newGameTestEnv(t, gameSetting(func(s *operation_setting.GameSetting) {
		s.Wheel = operation_setting.WheelRule{Enabled: true, FreeSpins: 1, SpinCost: 1}
	}))
	player := env.player(5, gameTestIpA)
	info := player.ok(http.MethodGet, "/api/game/wheel", nil)
	assert.Equal(t, gameCenterDay(), info["day"])
	assert.Greater(t, info["day_ends_at"], info["server_time"])
	yesterday := model.GameCenterDay(time.Now().AddDate(0, 0, -1))
	player.fails(http.MethodPost, "/api/game/wheel/spin", map[string]any{"free": false, "day": yesterday})
	assert.Equal(t, 5.0, player.balance())
	stopWheelAt(t, 3)
	player.ok(http.MethodPost, "/api/game/wheel/spin", map[string]any{"free": true, "day": info["day"]})
	assert.Equal(t, 5.0, player.balance())
}

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
	assert.Equal(t, 0.0, info["free_used_today"])

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
		assert.Equal(t, float64(c.wantWin), result["win"], "case %d win", i)
		assert.Equal(t, float64(c.wantBalance), result["balance"], "case %d balance", i)
		assert.Equal(t, float64(c.wantNet), result["net_today"], "case %d net today", i)
	}
	account, err := model.GetGameCoinAccount(player.user.Id)
	require.NoError(t, err)
	assert.Equal(t, float64(3+6), coinsOf(t, account.TotalEarned), "种子币加上免费旋转的奖品")
	// 免费旋转没有下注，每次中奖都和技巧游戏的奖励一样记审计日志；付费旋转按净赢数量，净赢 98 个不记。
	var rewardLogs, bigWinLogs int64
	require.NoError(t, env.db.Model(&model.Log{}).Where("user_id = ? AND type = ? AND content LIKE ?", player.user.Id, model.LogTypeSystem, "游戏奖励%").Count(&rewardLogs).Error)
	require.NoError(t, env.db.Model(&model.Log{}).Where("user_id = ? AND type = ? AND content LIKE ?", player.user.Id, model.LogTypeSystem, "游戏大奖%").Count(&bigWinLogs).Error)
	assert.EqualValues(t, 1, rewardLogs)
	assert.Zero(t, bigWinLogs)
	assert.Equal(t, 8.0, coinsOf(t, account.TotalWagered))
	assert.Equal(t, 92.0, coinsOf(t, account.TotalWagerNet))
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
	three, err := model.WholeGameCoins(3)
	require.NoError(t, err)
	skill, err := model.FinishGameSession(session, model.GameSettlement{
		Score: 5000, Reward: three, DailyRewards: 1, FreeDailyLimit: three, Day: gameCenterDay(), Ip: gameTestIpA,
	})
	require.NoError(t, err)
	require.Equal(t, three, skill.Session.Reward)

	info := first.ok(http.MethodGet, "/api/game/wheel", nil)
	assert.EqualValues(t, 1, info["free_spins"])
	assert.NotContains(t, info, "free_daily_limit")
	stopWheelAt(t, 0) // 默认花费 1，头奖 50 倍。
	result := first.ok(http.MethodPost, "/api/game/wheel/spin", map[string]any{"free": true})
	assert.Equal(t, 50.0, result["win"])
	assert.Equal(t, 53.0, result["balance"])
	assert.Empty(t, result["limited_by"])
	assert.Equal(t, 1.0, result["free_used_today"])
	assert.Equal(t, 1.0, result["ip_free_used_today"])
	first.fails(http.MethodPost, "/api/game/wheel/spin", map[string]any{"free": true})
	second.fails(http.MethodPost, "/api/game/wheel/spin", map[string]any{"free": true})

	result = other.ok(http.MethodPost, "/api/game/wheel/spin", map[string]any{"free": true})
	assert.Equal(t, 50.0, result["win"], "别的 IP 有自己的免费次数")
	assert.Equal(t, 53.0, first.balance())
	assert.Zero(t, second.balance())
	assert.Equal(t, 50.0, other.balance())
}

// 结算已提交后统计查询不可用，仍应返回完整的中奖结果和累计统计，不能让玩家把成功的旋转当作失败重试。
func TestSpinWheelReturnsCommittedResultWhenStatsUnavailable(t *testing.T) {
	for _, test := range []struct {
		name        string
		free        bool
		wantBalance int
		wantNet     int
	}{
		{name: "free", free: true, wantBalance: 24, wantNet: 4},
		{name: "paid", wantBalance: 22, wantNet: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			env := newGameTestEnv(t, gameSetting(func(s *operation_setting.GameSetting) {
				s.Wheel = operation_setting.WheelRule{Enabled: true, FreeSpins: 1, SpinCost: 2}
			}))
			player := env.player(20, gameTestIpA)
			stopWheelAt(t, 3)
			player.ok(http.MethodPost, "/api/game/wheel/spin", map[string]any{"free": false})
			stopWheelAt(t, 2)

			sqlDB, err := env.db.DB()
			require.NoError(t, err)
			statsUnavailable := true
			statsError := errors.New("game statistics unavailable after settlement")
			const callback = "test:wheel_stats_unavailable"
			require.NoError(t, env.db.Callback().Row().Before("gorm:row").Register(callback, func(db *gorm.DB) {
				if statsUnavailable && db.Statement.Table == "game_reward_days" && db.Statement.ConnPool == sqlDB {
					db.AddError(statsError)
				}
			}))
			t.Cleanup(func() { require.NoError(t, env.db.Callback().Row().Remove(callback)) })
			_, _, err = model.GetGameDayStats(player.user.Id, gameWheel, gameCenterDay())
			require.ErrorIs(t, err, statsError)

			result := player.ok(http.MethodPost, "/api/game/wheel/spin", map[string]any{"free": test.free})
			assert.Equal(t, test.free, result["free"])
			assert.EqualValues(t, 2, result["segment"])
			assert.EqualValues(t, 3, result["multiplier"])
			assert.Equal(t, 6.0, result["win"])
			assert.Equal(t, float64(test.wantBalance), result["balance"])
			assert.Equal(t, float64(test.wantNet), result["net_today"])
			assert.EqualValues(t, 2, result["plays_today"])
			if test.free {
				assert.Equal(t, 1.0, result["free_used_today"])
				assert.Equal(t, 1.0, result["ip_free_used_today"])
			}
			assert.Equal(t, float64(test.wantBalance), player.balance())
			statsUnavailable = false
			net, plays, err := model.GetGameDayStats(player.user.Id, gameWheel, gameCenterDay())
			require.NoError(t, err)
			assert.Equal(t, float64(test.wantNet), coinsOf(t, net))
			assert.Equal(t, 2, plays)
		})
	}
}
