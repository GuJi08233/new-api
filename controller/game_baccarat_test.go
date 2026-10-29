package controller

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/pkg/baccarat"
	"github.com/QuantumNous/new-api/pkg/keno"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// dealBaccaratAs 让接下来的牌局都按 result 结算。
func dealBaccaratAs(t *testing.T, result baccarat.Result) {
	t.Helper()
	previous := playBaccarat
	playBaccarat = func() (baccarat.Result, error) { return result, nil }
	t.Cleanup(func() { playBaccarat = previous })
}

// 百家乐一次押多处：庄赢按 1.95 倍派彩，和局时押闲、押庄退回本金，和 9 倍，对子 12 倍。同一处押两次、合计超过上限、
// 不认识的位置都被拒绝，不扣游戏币。
func TestDealBaccaratPaysEverySpot(t *testing.T) {
	env := newGameTestEnv(t, gameSetting(func(s *operation_setting.GameSetting) {
		s.Baccarat = operation_setting.WagerRule{Enabled: true, MaxBet: 10}
	}))
	player := env.player(100, gameTestIpA)
	info := player.ok(http.MethodGet, "/api/game/baccarat", nil)
	assert.Len(t, info["spots"], len(baccarat.Spots))
	assert.InDelta(t, baccarat.Payback[baccarat.Banker], info["payback"].(map[string]any)[baccarat.Banker], 1e-9)

	for _, bets := range [][]map[string]any{
		{{"spot": baccarat.Player, "amount": 1}, {"spot": baccarat.Player, "amount": 1}},
		{{"spot": baccarat.Player, "amount": 6}, {"spot": baccarat.Banker, "amount": 5}},
		{{"spot": "dragon", "amount": 1}},
		{},
	} {
		player.fails(http.MethodPost, "/api/game/baccarat/deal", map[string]any{"bets": bets})
	}
	assert.Equal(t, 100.0, player.balance())

	dealBaccaratAs(t, baccarat.Result{Winner: baccarat.Banker, BankerPair: true})
	won := player.ok(http.MethodPost, "/api/game/baccarat/deal", map[string]any{"bets": []map[string]any{
		{"spot": baccarat.Banker, "amount": 2},
		{"spot": baccarat.Player, "amount": 3},
		{"spot": baccarat.BankerPair, "amount": 1},
	}})
	assert.Equal(t, 6.0, won["bet"])
	assert.Equal(t, 15.9, won["win"], "庄 2×1.95 + 庄对 1×12")
	assert.Equal(t, 109.9, player.balance())

	dealBaccaratAs(t, baccarat.Result{Winner: baccarat.Tie})
	tie := player.ok(http.MethodPost, "/api/game/baccarat/deal", map[string]any{"bets": []map[string]any{
		{"spot": baccarat.Player, "amount": 4},
		{"spot": baccarat.Tie, "amount": 1},
	}})
	assert.Equal(t, 13.0, tie["win"], "押闲退回 4，和 1×9")
	bets := tie["bets"].([]any)
	require.Len(t, bets, 2)
	assert.Equal(t, 4.0, bets[0].(map[string]any)["win"])
}

// drawKenoAs 让接下来的基诺都开出 drawn。
func drawKenoAs(t *testing.T, drawn []int) {
	t.Helper()
	previous := drawKeno
	drawKeno = func() ([]int, error) { return drawn, nil }
	t.Cleanup(func() { drawKeno = previous })
}

// 基诺按选号个数与中的个数查赔付表，派彩 = 下注 × 倍数；号码重复、越界、超过 10 个都被拒绝。
func TestPlayKenoPaysByTheTable(t *testing.T) {
	env := newGameTestEnv(t, gameSetting(func(s *operation_setting.GameSetting) {
		s.Keno = operation_setting.WagerRule{Enabled: true, MaxBet: 10}
	}))
	player := env.player(100, gameTestIpA)
	info := player.ok(http.MethodGet, "/api/game/keno", nil)
	assert.EqualValues(t, keno.Numbers, info["numbers"])
	assert.InDelta(t, keno.Payback(3), info["payback"].([]any)[3], 1e-9)

	for _, picks := range [][]int{{}, {1, 1}, {0}, {41}, {1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11}} {
		player.fails(http.MethodPost, "/api/game/keno/play", map[string]any{"bet": 1, "picks": picks})
	}
	assert.Equal(t, 100.0, player.balance())

	drawKenoAs(t, []int{1, 2, 3, 11, 12, 13, 14, 15, 16, 17})
	won := player.ok(http.MethodPost, "/api/game/keno/play", map[string]any{"bet": 2, "picks": []int{1, 2, 3}})
	assert.EqualValues(t, 3, won["hits"])
	assert.Equal(t, 84.0, won["win"], "选 3 中 3 是 42 倍")
	lost := player.ok(http.MethodPost, "/api/game/keno/play", map[string]any{"bet": 1, "picks": []int{38, 39, 40}})
	assert.EqualValues(t, 0, lost["hits"])
	assert.Equal(t, 0.0, lost["win"])
	assert.Equal(t, 181.0, player.balance())
}
