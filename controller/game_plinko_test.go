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

// 每颗球先扣这一档的花费，再按落点派彩，不限次数；挡位高于管理员设的最高一档时不开放，其他挡位也投不了。
// 派彩达到花费 10 倍的大奖记一条带 IP 的审计日志。
func TestDropPlinkoPaysEachOpenTier(t *testing.T) {
	env := newGameTestEnv(t, gameSetting(func(s *operation_setting.GameSetting) {
		s.Plinko = operation_setting.WagerRule{Enabled: true, MaxBet: 10}
	}))
	player := env.player(12, gameTestIpA)

	info := player.ok(http.MethodGet, "/api/game/plinko", nil)
	assert.Len(t, info["weights"], plinko.Slots)
	tiers := info["tiers"].([]any)
	require.Len(t, tiers, 3)
	assert.EqualValues(t, 2, tiers[0].(map[string]any)["cost"], "1 币档最多扣 2 个")
	assert.InDelta(t, 0.8, tiers[1].(map[string]any)["payback"], 1e-9)
	assert.Equal(t, true, tiers[1].(map[string]any)["open"])
	assert.Equal(t, false, tiers[2].(map[string]any)["open"])

	cases := []struct {
		stake       int
		rights      int
		wantErr     bool
		wantWin     int
		wantBalance int
	}{
		// 最左边一格 +10：扣 2 派彩 12。
		{stake: 1, rights: 0, wantWin: 12, wantBalance: 22},
		// 正中一格 -2：扣 2 派彩 0。
		{stake: 1, rights: 6, wantWin: 0, wantBalance: 20},
		{stake: 1, rights: 6, wantWin: 0, wantBalance: 18},
		// 最右边一格 13 倍：扣 10 派彩 130。
		{stake: 10, rights: 12, wantWin: 130, wantBalance: 138},
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
		assert.EqualValues(t, c.wantWin, result["win"], "case %d win", i)
		assert.EqualValues(t, c.wantBalance, result["balance"], "case %d balance", i)
	}
	account, err := model.GetGameCoinAccount(player.user.Id)
	require.NoError(t, err)
	assert.Equal(t, 16, account.TotalWagered)
	assert.Equal(t, 126, account.TotalWagerNet)
	var bigWinLogs int64
	require.NoError(t, env.db.Model(&model.Log{}).Where("user_id = ? AND type = ? AND content LIKE ?", player.user.Id, model.LogTypeSystem, "游戏大奖%").Count(&bigWinLogs).Error)
	assert.EqualValues(t, 1, bigWinLogs, "只有 13 倍那一颗是大奖")
}

// 1 币档一颗球最多扣 2 个：余额只有 1 个时拒绝，不扣币也不留下对局。
func TestDropPlinkoNeedsTheWholeCost(t *testing.T) {
	env := newGameTestEnv(t, gameSetting(func(s *operation_setting.GameSetting) {
		s.Plinko = operation_setting.WagerRule{Enabled: true, MaxBet: 100}
	}))
	player := env.player(1, gameTestIpA)
	dropPlinkoAlong(t, 0)
	player.fails(http.MethodPost, "/api/game/plinko/drop", map[string]any{"stake": 1})
	assert.Equal(t, 1, player.balance())
	var sessions int64
	require.NoError(t, env.db.Model(&model.GameSession{}).Where("user_id = ?", player.user.Id).Count(&sessions).Error)
	assert.Zero(t, sessions)
}
