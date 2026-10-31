package controller

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/sect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 论剑：设了守擂阵容的宗门出现在别人的对手列表里；打败全部守擂弟子就赢，按 Elo 改双方积分，资源记进挑战方的宗门，
// 不发游戏币；重复结算不会再改积分。
func TestSectArenaChallengeMovesRatingsOnce(t *testing.T) {
	require.NoError(t, i18n.Init())
	env := newGameTestEnv(t, sectSettings())
	defender := env.player(0, gameTestIpA)
	attacker := env.player(0, gameTestIpB)
	path := "/api/game/sect/arena"

	view := sectView(t, defender, map[string]any{"action": "sync"})
	guard := view["disciples"].([]any)[0].(map[string]any)["id"]
	defender.fails(http.MethodPost, path+"/defense", map[string]any{"team": []any{guard, guard}})
	defender.fails(http.MethodPost, path+"/defense", map[string]any{"team": []any{99}})
	set := defender.ok(http.MethodPost, path+"/defense", map[string]any{"team": []any{guard}})
	assert.Equal(t, []any{guard}, set["lineup"])
	assert.Equal(t, float64(model.SectArenaStartRating), set["row"].(map[string]any)["rating"])

	view = sectView(t, attacker, map[string]any{"action": "sync"})
	first := view["disciples"].([]any)[0].(map[string]any)["id"]
	overview := attacker.ok(http.MethodGet, path, nil)
	assert.Nil(t, overview["me"])
	opponents := overview["opponents"].([]any)
	require.Len(t, opponents, 1)
	assert.Len(t, opponents[0].(map[string]any)["defenders"], 1)

	attacker.fails(http.MethodPost, path+"/start", map[string]any{"opponent": attacker.user.Id, "team": []any{first}})
	round := attacker.ok(http.MethodPost, path+"/start", map[string]any{"opponent": defender.user.Id, "team": []any{first}})
	assert.Equal(t, float64(defender.user.Id), round["defender"])
	assert.Equal(t, float64(1), round["tournament"].(map[string]any)["rounds"])
	attacker.fails(http.MethodPost, path+"/start", map[string]any{"opponent": defender.user.Id, "team": []any{first}})

	ageSession(t, env, round["session_id"], 60)
	round = attacker.ok(http.MethodPost, path+"/action", puzzleAction(round, map[string]any{"action": "pick", "index": 0}))
	editSectTournament(t, env, round["session_id"], func(tour *sect.Tournament) {
		tour.Opponent.Hp, tour.Intent = 1, sect.MoveGuard
	})
	finished := attacker.ok(http.MethodPost, path+"/action", puzzleAction(round, map[string]any{"action": sect.MoveStrike}))
	assert.Equal(t, model.GameSessionStatusFinished, finished["status"])
	detail := finished["result"].(map[string]any)["detail"].(map[string]any)
	assert.Equal(t, true, detail["champion"])
	assert.Equal(t, float64(4+8), detail["rep"])
	battle := finished["battle"].(map[string]any)
	assert.Equal(t, float64(16), battle["attacker_delta"])
	assert.Equal(t, float64(-16), battle["defender_delta"])
	assert.Equal(t, 0.0, attacker.balance(), "the arena never pays game coins")

	again := attacker.ok(http.MethodPost, path+"/action", puzzleAction(round, map[string]any{"action": "abandon"}))
	assert.Equal(t, finished["session_id"], again["session_id"])
	defenderRow, err := model.GetSectArena(defender.user.Id)
	require.NoError(t, err)
	assert.Equal(t, model.SectArenaStartRating-16, defenderRow.Rating)
	assert.Equal(t, 1, defenderRow.DefendLosses)
	attackerRow, err := model.GetSectArena(attacker.user.Id)
	require.NoError(t, err)
	assert.Equal(t, model.SectArenaStartRating+16, attackerRow.Rating)
	assert.Equal(t, 1, attackerRow.Wins)
	assert.Empty(t, attackerRow.Lineup, "an attacker without a defense cannot be challenged")

	view = sectView(t, attacker, map[string]any{"action": "sync"})
	assert.Equal(t, float64(4+8), view["res"].(map[string]any)["rep"])
	arena := view["arena"].(map[string]any)
	assert.Equal(t, float64(1), arena["battles"])
	assert.Equal(t, float64(1), arena["wins"])

	overview = defender.ok(http.MethodGet, path, nil)
	assert.Empty(t, overview["opponents"], "attackers without a defense are not listed")
	assert.Len(t, overview["battles"], 1)
}
