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
	opened := first.ok(http.MethodPost, "/api/game/jackpot/enter", map[string]any{"amount": 30, "round_id": 0})
	second.ok(http.MethodPost, "/api/game/jackpot/enter", map[string]any{"amount": 10, "round_id": opened["round"].(map[string]any)["id"]})

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

	first.ok(http.MethodPost, "/api/game/jackpot/enter", map[string]any{"amount": 6, "round_id": 0})
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

func TestJackpotLateEntryNeverChangesTargetRound(t *testing.T) {
	env := newGameTestEnv(t, gameSetting(func(s *operation_setting.GameSetting) {
		s.Jackpot = operation_setting.JackpotRule{Enabled: true, MaxBet: 100, RoundSeconds: 60, RakePercent: 5}
	}))
	first, second, late := env.player(10, gameTestIpA), env.player(10, gameTestIpB), env.player(10, "192.0.2.19")
	opened := first.ok(http.MethodPost, "/api/game/jackpot/enter", map[string]any{"amount": 1, "round_id": 0})
	id := opened["round"].(map[string]any)["id"]
	late.fails(http.MethodPost, "/api/game/jackpot/enter", map[string]any{"amount": 1})
	late.fails(http.MethodPost, "/api/game/jackpot/enter", map[string]any{"amount": 1, "round_id": 0})
	assert.Equal(t, 10.0, late.balance())
	second.ok(http.MethodPost, "/api/game/jackpot/enter", map[string]any{"amount": 1, "round_id": id})
	require.NoError(t, env.db.Model(&model.GameJackpotRound{}).Where("id = ?", id).Update("deadline", common.GetTimestamp()-1).Error)
	late.fails(http.MethodPost, "/api/game/jackpot/enter", map[string]any{"amount": 1, "round_id": id})
	assert.Equal(t, 10.0, late.balance(), "轮次过期不能把钱扣入下一轮")
	var count int64
	require.NoError(t, env.db.Model(&model.GameJackpotEntry{}).Where("user_id = ?", late.user.Id).Count(&count).Error)
	assert.Zero(t, count)
	info := late.ok(http.MethodGet, "/api/game/jackpot", nil)
	assert.Nil(t, info["round"])
	created := late.ok(http.MethodPost, "/api/game/jackpot/enter", map[string]any{"amount": 1, "round_id": 0})
	assert.NotEqual(t, id, created["round"].(map[string]any)["id"])
	assert.Equal(t, 9.0, late.balance())
}
