package controller

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 奖池页刷新时才处理到点的一轮：没到开奖时间时奖池原样开着，到点之后的下一次刷新开奖派彩，最近一轮带着每位玩家的投入；
// 只有一位玩家的一轮没放够时间也原样开着，放太久之后的下一次刷新原样退回。
func TestJackpotRefreshSettlesOnlyDueRounds(t *testing.T) {
	env := newGameTestEnv(t, gameSetting(func(s *operation_setting.GameSetting) {
		s.Jackpot = operation_setting.JackpotRule{Enabled: true, MaxBet: 100, RoundSeconds: 60, RakePercent: 10}
	}))
	previous := pickJackpotTicket
	pickJackpotTicket = func(tickets int) (int, error) { return 0, nil }
	t.Cleanup(func() { pickJackpotTicket = previous })
	first := env.player(50, gameTestIpA)
	second := env.player(50, gameTestIpB)
	first.ok(http.MethodPost, "/api/game/jackpot/enter", map[string]any{"amount": 30})
	second.ok(http.MethodPost, "/api/game/jackpot/enter", map[string]any{"amount": 10})

	info := first.ok(http.MethodGet, "/api/game/jackpot", nil)
	round := info["round"].(map[string]any)
	assert.Equal(t, 40.0, round["pot"], "没到开奖时间，奖池还开着")
	assert.Empty(t, info["recent"])

	require.NoError(t, env.db.Model(&model.GameJackpotRound{}).Where("id = ?", round["id"]).Update("deadline", common.GetTimestamp()-1).Error)
	info = second.ok(http.MethodGet, "/api/game/jackpot", nil)
	assert.Nil(t, info["round"], "到点之后的刷新开奖")
	recent := info["recent"].([]any)
	require.Len(t, recent, 1)
	drawn := recent[0].(map[string]any)
	assert.Equal(t, 36.0, drawn["payout"], "奖池 40，抽成 10%")
	assert.Len(t, drawn["stakes"], 2)
	assert.Equal(t, 56.0, first.balance(), "第一张奖券属于先投入的玩家")
	assert.Equal(t, 40.0, second.balance())

	first.ok(http.MethodPost, "/api/game/jackpot/enter", map[string]any{"amount": 6})
	info = first.ok(http.MethodGet, "/api/game/jackpot", nil)
	round = info["round"].(map[string]any)
	assert.Equal(t, 6.0, round["pot"], "只有一位玩家、还没放够时间，奖池还开着")
	assert.Equal(t, 50.0, first.balance())

	require.NoError(t, env.db.Model(&model.GameJackpotRound{}).Where("id = ?", round["id"]).
		Update("created_at", common.GetTimestamp()-jackpotStaleSeconds-1).Error)
	info = second.ok(http.MethodGet, "/api/game/jackpot", nil)
	assert.Nil(t, info["round"], "放太久之后的刷新退回投入")
	assert.Equal(t, 56.0, first.balance())
}
