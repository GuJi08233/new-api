package controller

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/slots"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stopSlotsAt 让接下来的旋转都停在给定的停点。
func stopSlotsAt(t *testing.T, stops [3]int) {
	t.Helper()
	previous := spinSlots
	spinSlots = func() (slots.Result, error) { return slots.StopAt(stops), nil }
	t.Cleanup(func() { spinSlots = previous })
}

// 老虎机由服务端决定停点并按赔付表派彩，扣注与派彩在同一个事务里完成；下注游戏不设每日上限，三个 7 按 50 倍全额派彩，
// 之后照样能转。游戏页公示转轮、赔付表与返还率；派彩达到 10 倍的大奖记一条带 IP 的审计日志。
func TestSpinSlotsSettlesServerOutcome(t *testing.T) {
	env := newGameTestEnv(t, gameSetting(func(s *operation_setting.GameSetting) {
		s.Slots = operation_setting.WagerRule{Enabled: true, MaxBet: 5}
	}))
	player := env.player(10, gameTestIpA)

	info := player.ok(http.MethodGet, "/api/game/slots", nil)
	reels := info["reels"].([]any)
	assert.InDelta(t, 21720.0/27000.0, info["payback"], 1e-9)
	assert.EqualValues(t, 5, info["max_bet"])
	assert.Equal(t, 10.0, info["balance"])

	cases := []struct {
		stops       [3]int
		bet         int
		wantWin     int
		wantNet     int
		wantBalance int
	}{
		// 樱桃、樱桃、柠檬：前两个相同，派彩 3 倍。
		{stops: [3]int{0, 1, 2}, bet: 1, wantWin: 3, wantNet: 2, wantBalance: 12},
		// 柠檬打头：没有中奖。
		{stops: [3]int{1, 1, 1}, bet: 2, wantWin: 0, wantNet: 0, wantBalance: 10},
		// 三个 7：50 倍全额派彩。
		{stops: [3]int{10, 14, 17}, bet: 5, wantWin: 250, wantNet: 245, wantBalance: 255},
		{stops: [3]int{10, 14, 17}, bet: 5, wantWin: 250, wantNet: 490, wantBalance: 500},
	}
	for i, c := range cases {
		stopSlotsAt(t, c.stops)
		result := player.ok(http.MethodPost, "/api/game/slots/spin", map[string]any{"bet": c.bet})
		line := result["line"].([]any)
		for reel, stop := range c.stops {
			assert.Equal(t, reels[reel].([]any)[stop], line[reel], "case %d reel %d", i, reel)
		}
		assert.Equal(t, float64(c.wantWin), result["win"], "case %d win", i)
		assert.Equal(t, float64(c.wantNet), result["net_today"], "case %d net today", i)
		assert.EqualValues(t, i+1, result["plays_today"], "case %d plays today", i)
		assert.Equal(t, float64(c.wantBalance), result["balance"], "case %d balance", i)
	}
	account, err := model.GetGameCoinAccount(player.user.Id)
	require.NoError(t, err)
	assert.Equal(t, 13.0, coinsOf(t, account.TotalWagered))
	assert.Equal(t, 490.0, coinsOf(t, account.TotalWagerNet))
	var bigWinLogs int64
	require.NoError(t, env.db.Model(&model.Log{}).Where("user_id = ? AND type = ? AND content LIKE ?", player.user.Id, model.LogTypeSystem, "游戏大奖%").Count(&bigWinLogs).Error)
	assert.EqualValues(t, 2, bigWinLogs, "两局三个 7 是大奖")
}

// 下注数量超出规则、余额不够或老虎机没开放时拒绝旋转，不扣币也不留下对局。
func TestSpinSlotsRejectsBetsOutsideRule(t *testing.T) {
	setting := gameSetting(func(s *operation_setting.GameSetting) {
		s.Slots = operation_setting.WagerRule{Enabled: true, MaxBet: 5}
	})
	env := newGameTestEnv(t, setting)
	player := env.player(3, gameTestIpA)
	stopSlotsAt(t, [3]int{0, 1, 2})

	for _, body := range []map[string]any{{"bet": 0}, {"bet": 6}, {"bet": 4}} {
		player.fails(http.MethodPost, "/api/game/slots/spin", body)
	}
	setting.Slots.Enabled = false
	player.fails(http.MethodPost, "/api/game/slots/spin", map[string]any{"bet": 1})
	player.fails(http.MethodGet, "/api/game/slots", nil)

	assert.Equal(t, 3.0, player.balance())
	var sessions int64
	require.NoError(t, env.db.Model(&model.GameSession{}).Where("user_id = ?", player.user.Id).Count(&sessions).Error)
	assert.Zero(t, sessions)
}
