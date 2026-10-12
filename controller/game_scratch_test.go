package controller

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/scratch"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sellScratchTicket 让接下来买到的都是这张票。
func sellScratchTicket(t *testing.T, ticket scratch.Ticket) {
	t.Helper()
	previous := buyScratch
	buyScratch = func() (scratch.Ticket, error) { return ticket, nil }
	t.Cleanup(func() { buyScratch = previous })
}

// 每张票按票价扣币，刮出 3 格相同的金额就派彩这个金额，票面金额是票价乘以格子上的倍数；不设每日上限。票价只有 1、5、20
// 三档，高于管理员开放的最高一档或不是其中一档时买不了。派彩达到票价 10 倍的大奖记一条带 IP 的审计日志。
func TestBuyScratchPaysTheTicketPrize(t *testing.T) {
	env := newGameTestEnv(t, gameSetting(func(s *operation_setting.GameSetting) {
		s.Scratch = operation_setting.WagerRule{Enabled: true, MaxBet: 5}
	}))
	player := env.player(10, gameTestIpA)

	info := player.ok(http.MethodGet, "/api/game/scratch", nil)
	prices := info["prices"].([]any)
	require.Len(t, prices, 3)
	for i, want := range []struct {
		price int
		open  bool
	}{{1, true}, {5, true}, {20, false}} {
		assert.EqualValues(t, want.price, prices[i].(map[string]any)["price"])
		assert.Equal(t, want.open, prices[i].(map[string]any)["open"])
	}
	prizes := info["prizes"].([]any)
	require.Len(t, prizes, len(scratch.Prizes))
	assert.EqualValues(t, 1, prizes[0].(map[string]any)["multiple"])
	assert.InDelta(t, 0.15, prizes[0].(map[string]any)["chance"], 1e-9)
	assert.EqualValues(t, 100, prizes[len(prizes)-1].(map[string]any)["multiple"])
	assert.InDelta(t, 0.8, info["payback"], 1e-9)

	cases := []struct {
		price       int
		ticket      scratch.Ticket
		wantErr     bool
		wantCells   [scratch.CellCount]int
		wantWin     int
		wantBalance int
	}{
		// 刮出 3 个 2 倍：5 个游戏币的票派彩 10。
		{price: 5, ticket: scratch.Ticket{Multiple: 2, Cells: [scratch.CellCount]int{2, 1, 2, 5, 100, 2, 1, 5, 3}},
			wantCells: [scratch.CellCount]int{10, 5, 10, 25, 500, 10, 5, 25, 15}, wantWin: 10, wantBalance: 15},
		// 没有 3 格相同：不中奖。
		{price: 1, ticket: scratch.Ticket{Cells: [scratch.CellCount]int{1, 2, 3, 5, 10, 20, 50, 100, 1}},
			wantCells: [scratch.CellCount]int{1, 2, 3, 5, 10, 20, 50, 100, 1}, wantWin: 0, wantBalance: 14},
		// 刮出 3 个 100 倍：5 个游戏币的票派彩 500。
		{price: 5, ticket: scratch.Ticket{Multiple: 100, Cells: [scratch.CellCount]int{100, 50, 100, 20, 100, 50, 1, 2, 3}},
			wantCells: [scratch.CellCount]int{500, 250, 500, 100, 500, 250, 5, 10, 15}, wantWin: 500, wantBalance: 509},
		{price: 20, ticket: scratch.Ticket{Multiple: 1, Cells: [scratch.CellCount]int{1, 1, 1, 2, 2, 3, 3, 5, 5}}, wantErr: true},
		{price: 2, ticket: scratch.Ticket{Multiple: 1, Cells: [scratch.CellCount]int{1, 1, 1, 2, 2, 3, 3, 5, 5}}, wantErr: true},
	}
	for i, c := range cases {
		sellScratchTicket(t, c.ticket)
		if c.wantErr {
			player.fails(http.MethodPost, "/api/game/scratch/buy", map[string]any{"price": c.price})
			continue
		}
		result := player.ok(http.MethodPost, "/api/game/scratch/buy", map[string]any{"price": c.price})
		assert.EqualValues(t, c.price, result["price"], "case %d price", i)
		assert.Equal(t, float64(c.price), result["bet"], "case %d bet", i)
		cells := result["cells"].([]any)
		require.Len(t, cells, scratch.CellCount, "case %d cells", i)
		for cell, want := range c.wantCells {
			assert.EqualValues(t, want, cells[cell], "case %d cell %d", i, cell)
		}
		assert.Equal(t, float64(c.wantWin), result["prize"], "case %d prize", i)
		assert.Equal(t, float64(c.wantWin), result["win"], "case %d win", i)
		assert.Equal(t, float64(c.wantBalance), result["balance"], "case %d balance", i)
	}
	account, err := model.GetGameCoinAccount(player.user.Id)
	require.NoError(t, err)
	assert.Equal(t, 11.0, coinsOf(t, account.TotalWagered))
	assert.Equal(t, 499.0, coinsOf(t, account.TotalWagerNet))
	var bigWinLogs int64
	require.NoError(t, env.db.Model(&model.Log{}).Where("user_id = ? AND type = ? AND content LIKE ?", player.user.Id, model.LogTypeSystem, "游戏大奖%").Count(&bigWinLogs).Error)
	assert.EqualValues(t, 1, bigWinLogs, "只有 100 倍那一张是大奖")
}

// 余额不够票价、票价不是正整数或刮刮乐没开放时拒绝，不扣币也不留下对局。
func TestBuyScratchRejectsWithoutCharging(t *testing.T) {
	setting := gameSetting(func(s *operation_setting.GameSetting) {
		s.Scratch = operation_setting.WagerRule{Enabled: true, MaxBet: 20}
	})
	env := newGameTestEnv(t, setting)
	player := env.player(3, gameTestIpA)
	sellScratchTicket(t, scratch.Ticket{Multiple: 1, Cells: [scratch.CellCount]int{1, 1, 1, 2, 2, 3, 3, 5, 5}})

	for _, body := range []map[string]any{{"price": 5}, {"price": 0}, {"price": -1}, {"price": 1.5}, {}} {
		player.fails(http.MethodPost, "/api/game/scratch/buy", body)
	}
	setting.Scratch.Enabled = false
	player.fails(http.MethodPost, "/api/game/scratch/buy", map[string]any{"price": 1})
	player.fails(http.MethodGet, "/api/game/scratch", nil)

	assert.Equal(t, 3.0, player.balance())
	var sessions int64
	require.NoError(t, env.db.Model(&model.GameSession{}).Where("user_id = ?", player.user.Id).Count(&sessions).Error)
	assert.Zero(t, sessions)
}
