package controller

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/blackjack"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stackBlackjackShoe 让接下来开的每一局都按这些点数发牌(花色都是黑桃)：玩家、庄家明牌、玩家、庄家暗牌，之后按需要依次发出。
func stackBlackjackShoe(t *testing.T, ranks ...int) {
	t.Helper()
	previous := shuffleBlackjack
	shuffleBlackjack = func() ([]int, error) {
		shoe := make([]int, len(ranks))
		for i, rank := range ranks {
			shoe[i] = rank - 1
		}
		return shoe, nil
	}
	t.Cleanup(func() { shuffleBlackjack = previous })
}

func blackjackSetting(s *operation_setting.GameSetting) {
	s.Blackjack = operation_setting.WagerRule{Enabled: true, MaxBet: 100}
}

// handOf 取出这一局的第 i 手。
func handOf(t *testing.T, round map[string]any, i int) map[string]any {
	t.Helper()
	hands := round["hands"].([]any)
	require.Greater(t, len(hands), i)
	return hands[i].(map[string]any)
}

// 21 点一局：发牌时庄家只亮明牌，要牌、停牌之后庄家翻开暗牌补到 17 点，赢了派彩 2 倍。有黑杰克时发完牌就结算：
// 玩家黑杰克派彩 2.5 倍，庄家明牌是 A 时偷看出黑杰克，玩家直接输。
func TestBlackjackHitStandAndNaturals(t *testing.T) {
	env, _ := newWagerRoundEnv(t, blackjackSetting)
	player := env.player(100, gameTestIpA)
	info := player.ok(http.MethodGet, "/api/game/blackjack", nil)
	assert.EqualValues(t, 2, info["min_bet"])
	assert.EqualValues(t, 6, info["decks"])
	assert.EqualValues(t, 2.5, info["payouts"].(map[string]any)["blackjack"])

	// 10、5 对庄家 6、10：要到 4 共 19 点停牌，庄家 16 点补 8 爆牌。
	stackBlackjackShoe(t, 10, 6, 5, 10, 4, 8)
	dealt := player.ok(http.MethodPost, "/api/game/blackjack/deal", map[string]any{"bet": 10})
	assert.EqualValues(t, 90, dealt["balance"])
	round := roundOf(t, dealt)
	assert.Equal(t, model.GameSessionStatusPlaying, round["status"])
	hand := handOf(t, round, 0)
	assert.EqualValues(t, 15, hand["total"])
	assert.Equal(t, blackjack.HandPlaying, hand["status"])
	dealer := round["dealer"].(map[string]any)
	assert.Equal(t, []any{map[string]any{"rank": 6.0, "suit": 0.0}}, dealer["cards"], "暗牌不给")
	assert.EqualValues(t, 6, dealer["total"])
	assert.Equal(t, true, dealer["hidden"])
	assert.Equal(t, false, round["peeked"])
	assert.Equal(t, []any{blackjack.ActionHit, blackjack.ActionStand, blackjack.ActionDouble}, round["actions"])
	assert.NotContains(t, player.send(http.MethodGet, "/api/game/blackjack/round", nil).Body.String(), "shoe", "牌靴不出现在响应里")

	hit := player.ok(http.MethodPost, "/api/game/blackjack/action", roundRequest(t, dealt, map[string]any{"action": blackjack.ActionHit}))
	round = roundOf(t, hit)
	assert.EqualValues(t, 1, round["version"])
	assert.EqualValues(t, 19, handOf(t, round, 0)["total"])
	assert.Equal(t, []any{blackjack.ActionHit, blackjack.ActionStand}, round["actions"])
	assert.Len(t, round["dealer"].(map[string]any)["cards"], 1)

	stood := player.ok(http.MethodPost, "/api/game/blackjack/action", roundRequest(t, hit, map[string]any{"action": blackjack.ActionStand}))
	round = roundOf(t, stood)
	assert.Equal(t, model.GameSessionStatusFinished, round["status"])
	assert.EqualValues(t, 20, round["win"])
	assert.EqualValues(t, 10, round["net"])
	dealer = round["dealer"].(map[string]any)
	assert.Len(t, dealer["cards"], 3)
	assert.EqualValues(t, 24, dealer["total"])
	assert.Equal(t, false, dealer["hidden"])
	assert.Equal(t, blackjack.OutcomeWin, handOf(t, round, 0)["result"])
	assert.EqualValues(t, 20, handOf(t, round, 0)["payout"])
	assert.EqualValues(t, -1, round["current"])
	assert.Empty(t, round["actions"])
	assert.EqualValues(t, 110, stood["balance"])

	// 玩家黑杰克：发完牌就结算，派彩 2.5 倍。
	stackBlackjackShoe(t, 1, 9, 13, 7)
	natural := player.ok(http.MethodPost, "/api/game/blackjack/deal", map[string]any{"bet": 10})
	round = roundOf(t, natural)
	assert.Equal(t, model.GameSessionStatusFinished, round["status"])
	assert.EqualValues(t, 25, round["win"])
	assert.Equal(t, blackjack.HandBlackjack, handOf(t, round, 0)["status"])
	assert.Equal(t, blackjack.OutcomeBlackjack, handOf(t, round, 0)["result"])
	assert.EqualValues(t, 125, natural["balance"])

	// 庄家明牌 A、暗牌 K：偷看出黑杰克，玩家直接输。
	stackBlackjackShoe(t, 10, 1, 9, 13)
	dealerNatural := player.ok(http.MethodPost, "/api/game/blackjack/deal", map[string]any{"bet": 10})
	round = roundOf(t, dealerNatural)
	assert.Equal(t, model.GameSessionStatusFinished, round["status"])
	assert.EqualValues(t, 0, round["win"])
	assert.Equal(t, true, round["peeked"])
	assert.Len(t, round["dealer"].(map[string]any)["cards"], 2)
	assert.EqualValues(t, 115, dealerNatural["balance"])
	assert.EqualValues(t, 15, dealerNatural["net_today"])
	assert.EqualValues(t, 3, dealerNatural["plays_today"])
}

// 加倍、分牌追加与这一手相同的下注，在同一个请求里扣掉；分牌后两手依次操作，结算时按手相加。余额不够追加时拒绝，
// 这一局不变，玩家照样可以停牌。
func TestBlackjackDoubleAndSplitChargeTheExtraBet(t *testing.T) {
	env, _ := newWagerRoundEnv(t, blackjackSetting)
	player := env.player(100, gameTestIpA)

	// 5、6 对庄家 10、7：加倍补到 10 共 21 点，庄家 17 点停牌，这一手下注 20、派彩 40。
	stackBlackjackShoe(t, 5, 10, 6, 7, 10)
	dealt := player.ok(http.MethodPost, "/api/game/blackjack/deal", map[string]any{"bet": 10})
	doubled := player.ok(http.MethodPost, "/api/game/blackjack/action", roundRequest(t, dealt, map[string]any{"action": blackjack.ActionDouble}))
	round := roundOf(t, doubled)
	assert.Equal(t, model.GameSessionStatusFinished, round["status"])
	assert.EqualValues(t, 20, round["bet"])
	assert.EqualValues(t, 40, round["win"])
	assert.EqualValues(t, 20, round["net"])
	assert.Equal(t, true, handOf(t, round, 0)["doubled"])
	assert.EqualValues(t, 120, doubled["balance"])

	// 8、8 对庄家 6、10：分成 8、3 与 8、10；第一手加倍补到 9 共 20 点，第二手 18 点停牌；庄家 16 点补 7 爆牌，两手都赢。
	stackBlackjackShoe(t, 8, 6, 8, 10, 3, 10, 9, 7)
	dealt = player.ok(http.MethodPost, "/api/game/blackjack/deal", map[string]any{"bet": 10})
	assert.Contains(t, roundOf(t, dealt)["actions"], blackjack.ActionSplit)
	split := player.ok(http.MethodPost, "/api/game/blackjack/action", roundRequest(t, dealt, map[string]any{"action": blackjack.ActionSplit}))
	assert.EqualValues(t, 100, split["balance"], "分牌追加 10 个")
	round = roundOf(t, split)
	assert.EqualValues(t, 20, round["bet"])
	assert.Len(t, round["hands"], 2)
	assert.EqualValues(t, 0, round["current"])
	assert.Equal(t, blackjack.HandWaiting, handOf(t, round, 1)["status"])
	doubled = player.ok(http.MethodPost, "/api/game/blackjack/action", roundRequest(t, split, map[string]any{"action": blackjack.ActionDouble}))
	assert.EqualValues(t, 90, doubled["balance"], "第一手加倍追加 10 个")
	round = roundOf(t, doubled)
	assert.EqualValues(t, 1, round["current"])
	assert.EqualValues(t, 30, round["bet"])
	stood := player.ok(http.MethodPost, "/api/game/blackjack/action", roundRequest(t, doubled, map[string]any{"action": blackjack.ActionStand}))
	round = roundOf(t, stood)
	assert.Equal(t, model.GameSessionStatusFinished, round["status"])
	assert.EqualValues(t, 60, round["win"])
	assert.EqualValues(t, 30, round["net"])
	assert.EqualValues(t, 40, handOf(t, round, 0)["payout"])
	assert.EqualValues(t, 20, handOf(t, round, 1)["payout"])
	assert.EqualValues(t, 150, stood["balance"])

	account, err := model.GetGameCoinAccount(player.user.Id)
	require.NoError(t, err)
	assert.Equal(t, 50, account.TotalWagered)
	assert.Equal(t, 50, account.TotalWagerNet)

	// 余额正好够下注：加倍拒绝，什么都不变，玩家照样停牌。
	poor := env.player(10, gameTestIpB)
	stackBlackjackShoe(t, 5, 10, 6, 7, 10)
	dealt = poor.ok(http.MethodPost, "/api/game/blackjack/deal", map[string]any{"bet": 10})
	assert.Equal(t, gameMessage(i18n.MsgGameCoinInsufficient), poor.fails(http.MethodPost, "/api/game/blackjack/action", roundRequest(t, dealt, map[string]any{"action": blackjack.ActionDouble})))
	current := roundOf(t, poor.ok(http.MethodGet, "/api/game/blackjack/round", nil))
	assert.EqualValues(t, 0, current["version"])
	assert.EqualValues(t, 10, current["bet"])
	assert.Len(t, handOf(t, current, 0)["cards"], 2)
	stood = poor.ok(http.MethodPost, "/api/game/blackjack/action", roundRequest(t, dealt, map[string]any{"action": blackjack.ActionStand}))
	assert.EqualValues(t, 0, roundOf(t, stood)["win"], "11 点对 17 点输了")
	assert.EqualValues(t, 0, stood["balance"])
}

// 下注必须是偶数；同一时间只能有一局；不合规则的操作与过时的版本都拒绝，不改动这一局。
func TestBlackjackRejectsInvalidRequests(t *testing.T) {
	env, _ := newWagerRoundEnv(t, blackjackSetting)
	player := env.player(100, gameTestIpA)
	assert.Equal(t, gameMessage(i18n.MsgGameBetEven), player.fails(http.MethodPost, "/api/game/blackjack/deal", map[string]any{"bet": 3}))
	for _, bet := range []int{0, 1, 102} {
		player.fails(http.MethodPost, "/api/game/blackjack/deal", map[string]any{"bet": bet})
	}
	assert.Equal(t, 100, player.balance())

	// 5、4 对庄家 6、10：不是对子不能分牌；要到 2 共 11 点，还没结束。
	stackBlackjackShoe(t, 5, 6, 4, 10, 2)
	dealt := player.ok(http.MethodPost, "/api/game/blackjack/deal", map[string]any{"bet": 10})
	assert.Equal(t, gameMessage(i18n.MsgGameRoundOpen), player.fails(http.MethodPost, "/api/game/blackjack/deal", map[string]any{"bet": 10}))
	for _, action := range []string{blackjack.ActionSplit, "surrender", ""} {
		assert.Equal(t, gameMessage(i18n.MsgGameMoveInvalid), player.fails(http.MethodPost, "/api/game/blackjack/action", roundRequest(t, dealt, map[string]any{"action": action})), "action %q", action)
	}
	hit := player.ok(http.MethodPost, "/api/game/blackjack/action", roundRequest(t, dealt, map[string]any{"action": blackjack.ActionHit}))
	assert.Equal(t, gameMessage(i18n.MsgGameRoundChanged), player.fails(http.MethodPost, "/api/game/blackjack/action", roundRequest(t, dealt, map[string]any{"action": blackjack.ActionStand})), "版本过时")
	current := roundOf(t, player.ok(http.MethodGet, "/api/game/blackjack/round", nil))
	assert.EqualValues(t, roundOf(t, hit)["version"], current["version"])
	assert.Equal(t, model.GameSessionStatusPlaying, current["status"])
	assert.Equal(t, 90, player.balance())
}

// 加倍、分牌之后结束的一局要记两次账：追加下注之后派彩没写进去时，这一局已经结束却还在进行中，查看对局时补上结算。
// 发牌即是黑杰克的一局同理。
func TestBlackjackSettlesFinishedButUnpaidRound(t *testing.T) {
	env, _ := newWagerRoundEnv(t, blackjackSetting)
	player := env.player(100, gameTestIpA)
	game, err := blackjack.Deal([]int{0, 8, 12, 6}, 10) // 玩家 A、K 对庄家 9、7：黑杰克
	require.NoError(t, err)
	require.True(t, game.Over)
	state, err := common.Marshal(game)
	require.NoError(t, err)
	_, _, err = model.OpenGameRound(player.user.Id, gameBlackjack, model.GameRound{Bet: 10, State: string(state), Ip: gameTestIpA})
	require.NoError(t, err)
	assert.Equal(t, 90, player.balance())

	repaired := player.ok(http.MethodGet, "/api/game/blackjack/round", nil)
	assert.Equal(t, model.GameSessionStatusFinished, roundOf(t, repaired)["status"])
	assert.EqualValues(t, 25, roundOf(t, repaired)["win"])
	assert.EqualValues(t, 115, repaired["balance"])
	assert.Nil(t, player.ok(http.MethodGet, "/api/game/blackjack/round", nil)["round"])
}

// 游戏关闭后不能开新局，但押着游戏币的一局还能回来打完。
func TestBlackjackRoundResumableAfterDisabled(t *testing.T) {
	env, setting := newWagerRoundEnv(t, blackjackSetting)
	player := env.player(100, gameTestIpA)
	// 10、9 对庄家 6、10：停牌后庄家 16 点补 8 爆牌。
	stackBlackjackShoe(t, 10, 6, 9, 10, 8)
	player.ok(http.MethodPost, "/api/game/blackjack/deal", map[string]any{"bet": 10})

	setting.Blackjack.Enabled = false
	assert.Equal(t, gameMessage(i18n.MsgGamePlayDisabled), player.fails(http.MethodPost, "/api/game/blackjack/deal", map[string]any{"bet": 10}))
	assert.Equal(t, false, player.ok(http.MethodGet, "/api/game/blackjack", nil)["enabled"])
	current := player.ok(http.MethodGet, "/api/game/blackjack/round", nil)
	stood := player.ok(http.MethodPost, "/api/game/blackjack/action", roundRequest(t, current, map[string]any{"action": blackjack.ActionStand}))
	assert.EqualValues(t, 20, roundOf(t, stood)["win"])
	assert.EqualValues(t, 110, stood["balance"])
	assert.Equal(t, gameMessage(i18n.MsgGamePlayDisabled), player.fails(http.MethodGet, "/api/game/blackjack", nil))
}
