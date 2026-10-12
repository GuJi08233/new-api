package controller

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/dice"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// rollDiceAt 让接下来的掷骰都掷出 roll。
func rollDiceAt(t *testing.T, roll int) {
	t.Helper()
	previous := rollDice
	rollDice = func() (int, error) { return roll, nil }
	t.Cleanup(func() { rollDice = previous })
}

// 骰子按配置的返还率算出获胜阈值(90% 返还率押 2 倍是 4500、押 100 倍是 90)，由服务端掷出点数：under 掷出小于阈值、
// over 掷出不小于 10000 − 阈值才赢，赢了按选的倍数全额派彩，不设每日上限。游戏页公示每一档倍数的阈值与概率；
// 下注可以有两位小数，派彩跟着有小数。净赢达到 100 个游戏币的大奖记一条带 IP 的审计日志。
func TestRollDicePaysChosenMultiplier(t *testing.T) {
	env := newGameTestEnv(t, gameSetting(func(s *operation_setting.GameSetting) {
		s.Dice = operation_setting.WagerRule{Enabled: true, MaxBet: 10, RtpPercent: 90}
	}))
	player := env.player(20, gameTestIpA)

	info := player.ok(http.MethodGet, "/api/game/dice", nil)
	assert.EqualValues(t, 90, info["rtp_percent"])
	assert.EqualValues(t, 10, info["max_bet"])
	multipliers := info["multipliers"].([]any)
	require.Len(t, multipliers, len(dice.Multipliers))
	lowest := multipliers[0].(map[string]any)
	assert.EqualValues(t, 2, lowest["multiplier"])
	assert.EqualValues(t, 4500, lowest["threshold"])
	assert.InDelta(t, 0.45, lowest["chance"], 1e-9)
	highest := multipliers[len(multipliers)-1].(map[string]any)
	assert.EqualValues(t, 100, highest["multiplier"])
	assert.EqualValues(t, 90, highest["threshold"])

	cases := []struct {
		roll          int
		bet           float64
		multiplier    int
		direction     string
		wantThreshold int
		wantWon       bool
		wantWin       float64
		wantBalance   float64
	}{
		{roll: 4499, bet: 2, multiplier: 2, direction: dice.Under, wantThreshold: 4500, wantWon: true, wantWin: 4, wantBalance: 22},
		{roll: 4500, bet: 2, multiplier: 2, direction: dice.Under, wantThreshold: 4500, wantWon: false, wantWin: 0, wantBalance: 20},
		{roll: 5499, bet: 2, multiplier: 2, direction: dice.Over, wantThreshold: 4500, wantWon: false, wantWin: 0, wantBalance: 18},
		{roll: 5500, bet: 2, multiplier: 2, direction: dice.Over, wantThreshold: 4500, wantWon: true, wantWin: 4, wantBalance: 20},
		// 押 100 倍 over：掷出 9910 及以上才赢，10 个游戏币派彩 1000，是大奖。
		{roll: 9909, bet: 1, multiplier: 100, direction: dice.Over, wantThreshold: 90, wantWon: false, wantWin: 0, wantBalance: 19},
		{roll: 9910, bet: 10, multiplier: 100, direction: dice.Over, wantThreshold: 90, wantWon: true, wantWin: 1000, wantBalance: 1009},
		{roll: 4499, bet: 0.25, multiplier: 2, direction: dice.Under, wantThreshold: 4500, wantWon: true, wantWin: 0.5, wantBalance: 1009.25},
		{roll: 9910, bet: 0.01, multiplier: 100, direction: dice.Over, wantThreshold: 90, wantWon: true, wantWin: 1, wantBalance: 1010.24},
	}
	for i, c := range cases {
		rollDiceAt(t, c.roll)
		result := player.ok(http.MethodPost, "/api/game/dice/roll", map[string]any{
			"bet": c.bet, "multiplier": c.multiplier, "direction": c.direction,
		})
		assert.EqualValues(t, c.roll, result["roll"], "case %d roll", i)
		assert.EqualValues(t, c.wantThreshold, result["threshold"], "case %d threshold", i)
		assert.EqualValues(t, c.multiplier, result["multiplier"], "case %d multiplier", i)
		assert.Equal(t, c.direction, result["direction"], "case %d direction", i)
		assert.Equal(t, c.wantWon, result["won"], "case %d won", i)
		assert.Equal(t, c.bet, result["bet"], "case %d bet", i)
		assert.Equal(t, c.wantWin, result["win"], "case %d win", i)
		assert.Equal(t, c.wantBalance, result["balance"], "case %d balance", i)
		assert.EqualValues(t, i+1, result["plays_today"], "case %d plays today", i)
	}
	account, err := model.GetGameCoinAccount(player.user.Id)
	require.NoError(t, err)
	assert.Equal(t, 19.26, coinsOf(t, account.TotalWagered))
	assert.Equal(t, 990.24, coinsOf(t, account.TotalWagerNet))
	var bigWinLogs int64
	require.NoError(t, env.db.Model(&model.Log{}).Where("user_id = ? AND type = ? AND content LIKE ?", player.user.Id, model.LogTypeSystem, "游戏大奖%").Count(&bigWinLogs).Error)
	assert.EqualValues(t, 1, bigWinLogs, "只有 100 倍那一局是大奖")
}

// 下注超出规则或余额、倍数不在公示的几档里、方向不是 under/over，或骰子没开放时拒绝，不扣币也不留下对局。
func TestRollDiceRejectsInvalidBets(t *testing.T) {
	setting := gameSetting(func(s *operation_setting.GameSetting) {
		s.Dice = operation_setting.WagerRule{Enabled: true, MaxBet: 5, RtpPercent: 96}
	})
	env := newGameTestEnv(t, setting)
	player := env.player(3, gameTestIpA)
	rollDiceAt(t, 0)

	for _, body := range []map[string]any{
		{"bet": 0, "multiplier": 2, "direction": dice.Under},
		{"bet": -1, "multiplier": 2, "direction": dice.Under},
		{"bet": 6, "multiplier": 2, "direction": dice.Under},
		{"bet": 4, "multiplier": 2, "direction": dice.Under},
		{"bet": 1, "multiplier": 1, "direction": dice.Under},
		{"bet": 1, "multiplier": 6, "direction": dice.Under},
		{"bet": 1, "multiplier": 1000, "direction": dice.Under},
		{"bet": 1, "multiplier": 2, "direction": "sideways"},
		{"bet": 1, "multiplier": 2},
		{"bet": 1.005, "multiplier": 2, "direction": dice.Under},
	} {
		player.fails(http.MethodPost, "/api/game/dice/roll", body)
	}
	setting.Dice.Enabled = false
	player.fails(http.MethodPost, "/api/game/dice/roll", map[string]any{"bet": 1, "multiplier": 2, "direction": dice.Under})
	player.fails(http.MethodGet, "/api/game/dice", nil)

	assert.Equal(t, 3.0, player.balance())
	var sessions int64
	require.NoError(t, env.db.Model(&model.GameSession{}).Where("user_id = ?", player.user.Id).Count(&sessions).Error)
	assert.Zero(t, sessions)
}
