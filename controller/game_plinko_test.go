package controller

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/plinko"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// dropPlinkoAlong 让接下来的球都沿给定的路径落下：rights 排向右，其余向左，落进从左数第 rights 格。
func dropPlinkoAlong(t *testing.T, rights int) {
	t.Helper()
	var path [plinko.Rows]int
	for row := 0; row < rights; row++ {
		path[row] = 1
	}
	previous := dropPlinko
	dropPlinko = func() ([plinko.Rows]int, error) { return path, nil }
	t.Cleanup(func() { dropPlinko = previous })
}

// 每颗球先扣这一档的花费(就是挡位)，再按落点的倍率派彩，三档用同一张倍率表，派彩可以有小数，不限次数；挡位高于管理员设的
// 最高一档时不开放，其他挡位也投不了。净赢达到 100 个游戏币的大奖记一条带 IP 的审计日志。
func TestDropPlinkoPaysEachOpenTier(t *testing.T) {
	env := newGameTestEnv(t, gameSetting(func(s *operation_setting.GameSetting) {
		s.Plinko = operation_setting.WagerRule{Enabled: true, MaxBet: 10}
	}))
	player := env.player(12, gameTestIpA)

	info := player.ok(http.MethodGet, "/api/game/plinko", nil)
	assert.Len(t, info["weights"], plinko.Slots)
	tiers := info["tiers"].([]any)
	require.Len(t, tiers, 3)
	assert.EqualValues(t, 1, tiers[0].(map[string]any)["cost"])
	payouts := tiers[0].(map[string]any)["payouts"].([]any)
	assert.Equal(t, []any{13.0, 0.2}, []any{payouts[0], payouts[6]}, "1 币档两边 13 倍、正中 0.2 倍")
	assert.Equal(t, 130.0, tiers[1].(map[string]any)["payouts"].([]any)[12])
	assert.InDelta(t, 0.8, tiers[1].(map[string]any)["payback"], 1e-9)
	assert.Equal(t, true, tiers[1].(map[string]any)["open"])
	assert.Equal(t, false, tiers[2].(map[string]any)["open"])

	cases := []struct {
		stake       int
		rights      int
		wantErr     bool
		wantWin     float64
		wantBalance float64
	}{
		// 最左边一格 13 倍：扣 1 派彩 13。
		{stake: 1, rights: 0, wantWin: 13, wantBalance: 24},
		// 正中一格 0.2 倍：扣 1 派彩 0.2。
		{stake: 1, rights: 6, wantWin: 0.2, wantBalance: 23.2},
		{stake: 1, rights: 6, wantWin: 0.2, wantBalance: 22.4},
		// 最右边一格 13 倍：扣 10 派彩 130。
		{stake: 10, rights: 12, wantWin: 130, wantBalance: 142.4},
		{stake: 100, rights: 6, wantErr: true},
		{stake: 5, rights: 6, wantErr: true},
	}
	for i, c := range cases {
		dropPlinkoAlong(t, c.rights)
		if c.wantErr {
			player.fails(http.MethodPost, "/api/game/plinko/drop", map[string]any{"stake": c.stake})
			continue
		}
		result := player.ok(http.MethodPost, "/api/game/plinko/drop", map[string]any{"stake": c.stake})
		assert.EqualValues(t, c.rights, result["slot"], "case %d slot", i)
		assert.Len(t, result["path"], plinko.Rows, "case %d path", i)
		assert.Equal(t, c.wantWin, result["win"], "case %d win", i)
		assert.Equal(t, c.wantBalance, result["balance"], "case %d balance", i)
	}
	account, err := model.GetGameCoinAccount(player.user.Id)
	require.NoError(t, err)
	assert.Equal(t, 13.0, coinsOf(t, account.TotalWagered))
	assert.Equal(t, 130.4, coinsOf(t, account.TotalWagerNet))
	var bigWinLogs int64
	require.NoError(t, env.db.Model(&model.Log{}).Where("user_id = ? AND type = ? AND content LIKE ?", player.user.Id, model.LogTypeSystem, "游戏大奖%").Count(&bigWinLogs).Error)
	assert.EqualValues(t, 1, bigWinLogs, "只有净赢 120 的那一颗是大奖")
}

// 余额不够这一档的花费时拒绝，不扣币也不留下对局。
func TestDropPlinkoNeedsTheWholeCost(t *testing.T) {
	env := newGameTestEnv(t, gameSetting(func(s *operation_setting.GameSetting) {
		s.Plinko = operation_setting.WagerRule{Enabled: true, MaxBet: 100}
	}))
	player := env.player(9, gameTestIpA)
	dropPlinkoAlong(t, 0)
	player.fails(http.MethodPost, "/api/game/plinko/drop", map[string]any{"stake": 10})
	assert.Equal(t, 9.0, player.balance())
	var sessions int64
	require.NoError(t, env.db.Model(&model.GameSession{}).Where("user_id = ?", player.user.Id).Count(&sessions).Error)
	assert.Zero(t, sessions)
}
