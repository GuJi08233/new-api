package controller

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/hilo"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// dealHiloCards 让接下来翻开的牌依次是 cards，翻完之后再翻就让测试失败。
func dealHiloCards(t *testing.T, cards ...hilo.Card) {
	t.Helper()
	previous := drawHiloCard
	next := 0
	drawHiloCard = func() (hilo.Card, error) {
		require.Less(t, next, len(cards), "the test dealt more cards than it stacked")
		next++
		return cards[next-1], nil
	}
	t.Cleanup(func() { drawHiloCard = previous })
}

func hiloSetting(s *operation_setting.GameSetting) {
	s.Hilo = operation_setting.WagerRule{Enabled: true, MaxBet: 100, RtpPercent: 96}
}

// 高低牌一局分几步：开局扣注翻开第一张；猜对一步累计倍数乘 0.96 × 13/命中种数，兑现派彩 floor(下注 × 累计倍数)；
// 换牌不改变倍数；猜错输掉下注。进行中的响应给出两种猜法各自的命中种数、概率与猜对后的派彩。
func TestHiloCashOutAndLose(t *testing.T) {
	env, _ := newWagerRoundEnv(t, hiloSetting)
	player := env.player(100, gameTestIpA)
	info := player.ok(http.MethodGet, "/api/game/hilo", nil)
	assert.EqualValues(t, 96, info["rtp_percent"])
	assert.EqualValues(t, 5, info["max_skips"])

	// 7 上猜 higher 翻出 9(0.96 × 13/7 = 1.78)，9 上猜 lower 翻出 9(相同点数也算，再乘 0.96 × 13/9，累计 2.47)。
	dealHiloCards(t, hilo.Card{Rank: 7, Suit: 1}, hilo.Card{Rank: 9, Suit: 0}, hilo.Card{Rank: 9, Suit: 2})
	started := player.ok(http.MethodPost, "/api/game/hilo/start", map[string]any{"bet": 10})
	assert.EqualValues(t, 90, started["balance"])
	round := roundOf(t, started)
	assert.EqualValues(t, map[string]any{"rank": 7.0, "suit": 1.0}, round["card"])
	assert.Equal(t, "1.00", round["multiplier"])
	assert.EqualValues(t, 0, round["cashout"], "还没猜对过，不能兑现")
	assert.EqualValues(t, 5, round["skips_left"])
	options := round["options"].([]any)
	require.Len(t, options, 2)
	higher := options[0].(map[string]any)
	assert.Equal(t, hilo.Higher, higher["guess"])
	assert.EqualValues(t, 7, higher["hits"])
	assert.InDelta(t, 7.0/13, higher["chance"], 1e-12)
	assert.Equal(t, "1.78", higher["multiplier"])
	assert.EqualValues(t, 17, higher["payout"])
	assert.Equal(t, hilo.Lower, options[1].(map[string]any)["guess"])

	guessed := player.ok(http.MethodPost, "/api/game/hilo/guess", roundRequest(t, started, map[string]any{"guess": hilo.Higher}))
	round = roundOf(t, guessed)
	assert.EqualValues(t, 1, round["version"])
	assert.EqualValues(t, 9, round["card"].(map[string]any)["rank"])
	assert.Equal(t, "1.78", round["multiplier"])
	assert.EqualValues(t, 17, round["cashout"])
	assert.EqualValues(t, 1, round["wins"])
	assert.Equal(t, []any{map[string]any{"card": map[string]any{"rank": 7.0, "suit": 1.0}, "guess": hilo.Higher, "correct": true}}, round["history"])
	options = round["options"].([]any)
	assert.EqualValues(t, 5, options[0].(map[string]any)["hits"], "9 上 higher 命中 9 到 K 五种")
	assert.EqualValues(t, 44, options[0].(map[string]any)["payout"], "10 × 312/175 × 312/125 = 44.5")
	assert.EqualValues(t, 9, options[1].(map[string]any)["hits"])
	assert.EqualValues(t, 24, options[1].(map[string]any)["payout"], "10 × 312/175 × 104/75 = 24.7")

	guessed = player.ok(http.MethodPost, "/api/game/hilo/guess", roundRequest(t, guessed, map[string]any{"guess": hilo.Lower}))
	assert.Equal(t, "2.47", roundOf(t, guessed)["multiplier"])
	assert.EqualValues(t, 24, roundOf(t, guessed)["cashout"])
	cashed := player.ok(http.MethodPost, "/api/game/hilo/cashout", roundRequest(t, guessed, nil))
	round = roundOf(t, cashed)
	assert.Equal(t, model.GameSessionStatusFinished, round["status"])
	assert.EqualValues(t, 24, round["win"])
	assert.EqualValues(t, 14, round["net"])
	assert.EqualValues(t, 114, cashed["balance"])
	assert.NotContains(t, round, "options")

	// K 只能猜 lower 或 same；换一张 5，猜 higher 翻出 3，输掉。
	dealHiloCards(t, hilo.Card{Rank: 13, Suit: 3}, hilo.Card{Rank: 5, Suit: 1}, hilo.Card{Rank: 3, Suit: 0})
	started = player.ok(http.MethodPost, "/api/game/hilo/start", map[string]any{"bet": 10})
	options = roundOf(t, started)["options"].([]any)
	assert.Equal(t, hilo.Lower, options[0].(map[string]any)["guess"])
	assert.EqualValues(t, 12, options[0].(map[string]any)["hits"])
	assert.Equal(t, hilo.Same, options[1].(map[string]any)["guess"])
	assert.EqualValues(t, 1, options[1].(map[string]any)["hits"])
	assert.Equal(t, gameMessage(i18n.MsgGameMoveInvalid), player.fails(http.MethodPost, "/api/game/hilo/cashout", roundRequest(t, started, nil)), "还没猜对过")
	skipped := player.ok(http.MethodPost, "/api/game/hilo/guess", roundRequest(t, started, map[string]any{"guess": hilo.Skip}))
	assert.EqualValues(t, 4, roundOf(t, skipped)["skips_left"])
	assert.Equal(t, "1.00", roundOf(t, skipped)["multiplier"])
	lost := player.ok(http.MethodPost, "/api/game/hilo/guess", roundRequest(t, skipped, map[string]any{"guess": hilo.Higher}))
	round = roundOf(t, lost)
	assert.Equal(t, model.GameSessionStatusFinished, round["status"])
	assert.EqualValues(t, 0, round["win"])
	assert.EqualValues(t, map[string]any{"rank": 3.0, "suit": 0.0}, round["card"])
	history := round["history"].([]any)
	require.Len(t, history, 2)
	assert.Equal(t, hilo.Skip, history[0].(map[string]any)["guess"])
	assert.Equal(t, false, history[1].(map[string]any)["correct"])
	assert.EqualValues(t, 104, lost["balance"])
	assert.EqualValues(t, 4, lost["net_today"])
	assert.EqualValues(t, 2, lost["plays_today"])
}

// 累计派彩到了下注的 1000 倍自动兑现：A 上连猜三次 same，12.48 倍的三次方超过 1000 倍，按 1000 倍派彩，记大奖审计日志。
func TestHiloAutoCashoutAtCap(t *testing.T) {
	env, _ := newWagerRoundEnv(t, hiloSetting)
	player := env.player(100, gameTestIpA)
	ace := hilo.Card{Rank: 1, Suit: 0}
	dealHiloCards(t, ace, ace, ace, ace)
	data := player.ok(http.MethodPost, "/api/game/hilo/start", map[string]any{"bet": 1})
	for i := 0; i < 2; i++ {
		data = player.ok(http.MethodPost, "/api/game/hilo/guess", roundRequest(t, data, map[string]any{"guess": hilo.Same}))
	}
	assert.Equal(t, "155.75", roundOf(t, data)["multiplier"])
	assert.EqualValues(t, 1000, roundOf(t, data)["options"].([]any)[1].(map[string]any)["payout"], "再猜对一次就到顶")
	capped := player.ok(http.MethodPost, "/api/game/hilo/guess", roundRequest(t, data, map[string]any{"guess": hilo.Same}))
	assert.Equal(t, model.GameSessionStatusFinished, roundOf(t, capped)["status"])
	assert.EqualValues(t, 1000, roundOf(t, capped)["win"])
	assert.EqualValues(t, 1099, capped["balance"])

	var bigWinLogs int64
	require.NoError(t, env.db.Model(&model.Log{}).Where("user_id = ? AND type = ? AND content LIKE ?", player.user.Id, model.LogTypeSystem, "游戏大奖%").Count(&bigWinLogs).Error)
	assert.EqualValues(t, 1, bigWinLogs)
}

// 当前牌上不能猜的猜法、用完的换牌次数都拒绝，也不翻牌；过时的版本拒绝，不改动这一局；同一时间只能有一局。
func TestHiloRejectsInvalidRequests(t *testing.T) {
	env, _ := newWagerRoundEnv(t, hiloSetting)
	player := env.player(100, gameTestIpA)
	eight := hilo.Card{Rank: 8, Suit: 2}
	// 第二次开局先翻了第一张牌才发现还有一局没结束，那张牌没有用上。
	dealHiloCards(t, hilo.Card{Rank: 7}, hilo.Card{Rank: 2}, eight, eight, eight, eight, eight)
	started := player.ok(http.MethodPost, "/api/game/hilo/start", map[string]any{"bet": 10})
	assert.Equal(t, gameMessage(i18n.MsgGameRoundOpen), player.fails(http.MethodPost, "/api/game/hilo/start", map[string]any{"bet": 10}))
	assert.Equal(t, 90, player.balance())
	for _, guess := range []string{hilo.Same, "", "up"} {
		assert.Equal(t, gameMessage(i18n.MsgGameMoveInvalid), player.fails(http.MethodPost, "/api/game/hilo/guess", roundRequest(t, started, map[string]any{"guess": guess})), "7 上不能猜 %q", guess)
	}

	data := started
	for i := 0; i < hilo.MaxSkips; i++ {
		data = player.ok(http.MethodPost, "/api/game/hilo/guess", roundRequest(t, data, map[string]any{"guess": hilo.Skip}))
	}
	assert.EqualValues(t, 0, roundOf(t, data)["skips_left"])
	assert.Equal(t, gameMessage(i18n.MsgGameMoveInvalid), player.fails(http.MethodPost, "/api/game/hilo/guess", roundRequest(t, data, map[string]any{"guess": hilo.Skip})), "换牌次数用完")
	assert.Equal(t, gameMessage(i18n.MsgGameRoundChanged), player.fails(http.MethodPost, "/api/game/hilo/guess", roundRequest(t, started, map[string]any{"guess": hilo.Higher})), "版本过时")
	current := roundOf(t, player.ok(http.MethodGet, "/api/game/hilo/round", nil))
	assert.EqualValues(t, hilo.MaxSkips, current["version"], "被拒绝的请求没有改动这一局")
	assert.Len(t, current["history"], hilo.MaxSkips)
}

// 游戏关闭后不能开新局，但押着游戏币的一局还能回来玩完。
func TestHiloRoundResumableAfterDisabled(t *testing.T) {
	env, setting := newWagerRoundEnv(t, hiloSetting)
	player := env.player(100, gameTestIpA)
	dealHiloCards(t, hilo.Card{Rank: 7}, hilo.Card{Rank: 12})
	player.ok(http.MethodPost, "/api/game/hilo/start", map[string]any{"bet": 10})

	setting.Hilo.Enabled = false
	assert.Equal(t, gameMessage(i18n.MsgGamePlayDisabled), player.fails(http.MethodPost, "/api/game/hilo/start", map[string]any{"bet": 10}))
	assert.Equal(t, false, player.ok(http.MethodGet, "/api/game/hilo", nil)["enabled"])
	current := player.ok(http.MethodGet, "/api/game/hilo/round", nil)
	guessed := player.ok(http.MethodPost, "/api/game/hilo/guess", roundRequest(t, current, map[string]any{"guess": hilo.Higher}))
	cashed := player.ok(http.MethodPost, "/api/game/hilo/cashout", roundRequest(t, guessed, nil))
	assert.EqualValues(t, 107, cashed["balance"], "10 × 0.96 × 13/7 = 17.8")
	assert.Equal(t, gameMessage(i18n.MsgGamePlayDisabled), player.fails(http.MethodGet, "/api/game/hilo", nil))
}
