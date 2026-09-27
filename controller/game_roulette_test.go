package controller

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/roulette"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// spinRouletteAt 让接下来的旋转都开出 number。
func spinRouletteAt(t *testing.T, number int) {
	t.Helper()
	previous := spinRoulette
	spinRoulette = func() (int, error) { return number, nil }
	t.Cleanup(func() { spinRoulette = previous })
}

// 一次旋转下多注，各注金额之和是这一局的下注；开出的号码按每一注的赔率派彩，没中的注输掉。开出 0 时外围注全输，
// 只有押 0 的单号中。游戏页公示赔率、红色号码与返还率 36/37；派彩达到下注 10 倍的大奖记一条带 IP 的审计日志。
func TestSpinRoulettePaysEveryWinningBet(t *testing.T) {
	env := newGameTestEnv(t, gameSetting(func(s *operation_setting.GameSetting) {
		s.Roulette = operation_setting.WagerRule{Enabled: true, MaxBet: 30}
	}))
	player := env.player(100, gameTestIpA)

	info := player.ok(http.MethodGet, "/api/game/roulette", nil)
	payouts := info["payouts"].(map[string]any)
	assert.EqualValues(t, 35, payouts[roulette.Straight])
	assert.EqualValues(t, 1, payouts[roulette.Red])
	assert.EqualValues(t, 2, payouts[roulette.Column])
	assert.Len(t, info["red_numbers"], 18)
	assert.EqualValues(t, roulette.MaxBets, info["max_bets"])
	assert.InDelta(t, 36.0/37.0, info["payback"], 1e-9)

	// 17 是黑色、单数、小、第二打(13–24)、第二列。
	spread := []map[string]any{
		{"type": roulette.Straight, "value": 17, "amount": 2},
		{"type": roulette.Black, "amount": 3},
		{"type": roulette.Odd, "amount": 4},
		{"type": roulette.Dozen, "value": 2, "amount": 5},
		{"type": roulette.Column, "value": 3, "amount": 6},
		{"type": roulette.High, "amount": 1},
	}
	cases := []struct {
		number      int
		bets        []map[string]any
		wantColor   string
		wantBet     int
		wantWins    []int
		wantWin     int
		wantBalance int
	}{
		// 单号 2×36 + 黑 3×2 + 单 4×2 + 第二打 5×3 = 101，第三列与大输掉。
		{number: 17, bets: spread, wantColor: roulette.Black, wantBet: 21, wantWins: []int{72, 6, 8, 15, 0, 0}, wantWin: 101, wantBalance: 180},
		{number: 0, bets: spread, wantColor: roulette.Green, wantBet: 21, wantWins: []int{0, 0, 0, 0, 0, 0}, wantWin: 0, wantBalance: 159},
		// 押 0 的单号 5×36 = 180，扣掉合计下注 7 净赢 173，是大奖。
		{number: 0, bets: []map[string]any{
			{"type": roulette.Straight, "value": 0, "amount": 5},
			{"type": roulette.Red, "amount": 1},
			{"type": roulette.Even, "amount": 1},
		}, wantColor: roulette.Green, wantBet: 7, wantWins: []int{180, 0, 0}, wantWin: 180, wantBalance: 332},
	}
	for i, c := range cases {
		spinRouletteAt(t, c.number)
		result := player.ok(http.MethodPost, "/api/game/roulette/spin", map[string]any{"bets": c.bets})
		assert.EqualValues(t, c.number, result["number"], "case %d number", i)
		assert.Equal(t, c.wantColor, result["color"], "case %d color", i)
		assert.EqualValues(t, c.wantBet, result["bet"], "case %d bet", i)
		assert.EqualValues(t, c.wantWin, result["win"], "case %d win", i)
		assert.EqualValues(t, c.wantBalance, result["balance"], "case %d balance", i)
		bets := result["bets"].([]any)
		require.Len(t, bets, len(c.bets), "case %d bets", i)
		for j, bet := range bets {
			view := bet.(map[string]any)
			assert.Equal(t, c.bets[j]["type"], view["type"], "case %d bet %d type", i, j)
			assert.EqualValues(t, c.bets[j]["amount"], view["amount"], "case %d bet %d amount", i, j)
			assert.EqualValues(t, c.wantWins[j], view["win"], "case %d bet %d win", i, j)
		}
	}
	account, err := model.GetGameCoinAccount(player.user.Id)
	require.NoError(t, err)
	assert.Equal(t, 49, account.TotalWagered)
	assert.Equal(t, 232, account.TotalWagerNet)
	var bigWinLogs int64
	require.NoError(t, env.db.Model(&model.Log{}).Where("user_id = ? AND type = ? AND content LIKE ?", player.user.Id, model.LogTypeSystem, "游戏大奖%").Count(&bigWinLogs).Error)
	assert.EqualValues(t, 1, bigWinLogs, "只有押中 0 的那一局净赢超过 100 个")
}

// 注型或号码不合规则、一注都没下或超过 50 注、某一注金额不是正整数或超过单次下注上限、合计超过上限、余额不够，
// 或轮盘没开放时拒绝，不扣币也不留下对局。
func TestSpinRouletteRejectsInvalidBets(t *testing.T) {
	setting := gameSetting(func(s *operation_setting.GameSetting) {
		s.Roulette = operation_setting.WagerRule{Enabled: true, MaxBet: 100}
	})
	env := newGameTestEnv(t, setting)
	player := env.player(60, gameTestIpA)
	spinRouletteAt(t, 7)

	tooMany := make([]map[string]any, roulette.MaxBets+1)
	for i := range tooMany {
		tooMany[i] = map[string]any{"type": roulette.Red, "amount": 1}
	}
	for _, bets := range [][]map[string]any{
		nil,
		{},
		tooMany,
		{{"type": "split", "value": 1, "amount": 1}},
		{{"type": roulette.Straight, "value": 37, "amount": 1}},
		{{"type": roulette.Straight, "value": -1, "amount": 1}},
		{{"type": roulette.Dozen, "value": 0, "amount": 1}},
		{{"type": roulette.Column, "value": 4, "amount": 1}},
		{{"type": roulette.Red, "value": 7, "amount": 1}},
		{{"type": roulette.Red, "amount": 1}, {"type": roulette.Black, "amount": 0}},
		{{"type": roulette.Red, "amount": -5}},
		{{"type": roulette.Red, "amount": 1.5}},
		{{"type": roulette.Red, "amount": 101}},
		{{"type": roulette.Red, "amount": 60}, {"type": roulette.Black, "amount": 41}},
		{{"type": roulette.Red, "amount": 61}},
	} {
		player.fails(http.MethodPost, "/api/game/roulette/spin", map[string]any{"bets": bets})
	}
	setting.Roulette.Enabled = false
	player.fails(http.MethodPost, "/api/game/roulette/spin", map[string]any{"bets": []map[string]any{{"type": roulette.Red, "amount": 1}}})
	player.fails(http.MethodGet, "/api/game/roulette", nil)

	assert.Equal(t, 60, player.balance())
	var sessions int64
	require.NoError(t, env.db.Model(&model.GameSession{}).Where("user_id = ?", player.user.Id).Count(&sessions).Error)
	assert.Zero(t, sessions)
}
