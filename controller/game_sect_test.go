package controller

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/sect"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sectSettings 打开问道宗门：大比连胜三场奖励 1 个游戏币，不限游戏时长。
func sectSettings() *operation_setting.GameSetting {
	return gameSetting(func(s *operation_setting.GameSetting) {
		s.Sect = operation_setting.SkillRule{Enabled: true, DailyRewards: 1, Tiers: []operation_setting.RewardTier{{Score: 3, Coins: 1}}}
	})
}

func sectView(t *testing.T, player *gamePlayer, body map[string]any) map[string]any {
	t.Helper()
	return player.ok(http.MethodPost, "/api/game/sect/action", body)["sect"].(map[string]any)
}

func editSect(t *testing.T, player *gamePlayer, edit func(s *sect.Sect)) {
	t.Helper()
	_, err := model.UpdateGameSave(player.user.Id, sectGame, func(data string) (string, error) {
		s, err := sect.ParseSect(data)
		if err != nil {
			return "", err
		}
		edit(s)
		return s.Encode()
	})
	require.NoError(t, err)
}

func editSectTournament(t *testing.T, env *gameTestEnv, id any, edit func(tour *sect.Tournament)) {
	t.Helper()
	var session model.GameSession
	require.NoError(t, env.db.First(&session, int(id.(float64))).Error)
	var state sectTournamentState
	require.NoError(t, common.UnmarshalJsonStr(session.State, &state))
	edit(state.Tournament)
	data, err := common.Marshal(state)
	require.NoError(t, err)
	require.NoError(t, env.db.Model(&session).Update("state", string(data)).Error)
}

// 第一次操作开宗立派；经营操作改写存档并保留下来，资源不够时什么都不改。
func TestSectActionsPersistInTheSave(t *testing.T) {
	require.NoError(t, i18n.Init())
	env := newGameTestEnv(t, sectSettings())
	player := env.player(0, gameTestIpA)
	view := sectView(t, player, map[string]any{"action": "sync"})
	disciples := view["disciples"].([]any)
	require.Len(t, disciples, 2)
	assert.Nil(t, view["rng"], "the random state never reaches the browser")

	message := player.fails(http.MethodPost, "/api/game/sect/action", map[string]any{"action": "upgrade", "building": sect.BuildingHall})
	assert.Equal(t, i18n.Translate(i18n.LangEn, i18n.MsgGameSectResources), message)
	player.fails(http.MethodPost, "/api/game/sect/action", map[string]any{"action": "fly"})

	first := disciples[0].(map[string]any)["id"]
	sectView(t, player, map[string]any{"action": "assign", "disciple": first, "job": sect.JobField})
	view = sectView(t, player, map[string]any{"action": "sync"})
	assert.Equal(t, sect.JobField, view["disciples"].([]any)[0].(map[string]any)["job"])
	assert.Equal(t, float64(200), view["res"].(map[string]any)["stone"])
}

// 大比结束时按连胜场数发游戏币，声望、灵石与没用完的回春丹记回宗门；重复提交拿到同一次结算，不会记两次。
func TestSectTournamentSettlesOnce(t *testing.T) {
	require.NoError(t, i18n.Init())
	env := newGameTestEnv(t, sectSettings())
	player := env.player(0, gameTestIpA)
	path := "/api/game/sect/tournament"
	view := sectView(t, player, map[string]any{"action": "sync"})
	first := view["disciples"].([]any)[0].(map[string]any)["id"]
	second := view["disciples"].([]any)[1].(map[string]any)["id"]

	editSect(t, player, func(s *sect.Sect) { s.Disciples[1].Injured = common.GetTimestamp() + 3600 })
	message := player.fails(http.MethodPost, path+"/start", map[string]any{"team": []any{first, second}})
	assert.Equal(t, i18n.Translate(i18n.LangEn, i18n.MsgGameSectUnavailable), message)

	round := player.ok(http.MethodPost, path+"/start", map[string]any{"team": []any{first}})
	player.fails(http.MethodPost, path+"/start", map[string]any{"team": []any{first}})
	assert.Equal(t, float64(2), round["tournament"].(map[string]any)["pills"])
	view = sectView(t, player, map[string]any{"action": "sync"})
	assert.Equal(t, float64(0), view["pills"].(map[string]any)[sect.PillHeal], "the pills travel with the team")

	ageSession(t, env, round["session_id"], 60)
	round = player.ok(http.MethodPost, path+"/action", puzzleAction(round, map[string]any{"action": "pick", "index": 0}))
	player.fails(http.MethodPost, path+"/action", puzzleAction(round, map[string]any{"action": sect.MoveSpell}))
	editSectTournament(t, env, round["session_id"], func(tour *sect.Tournament) {
		tour.Wins, tour.Round = 2, 2
		tour.Opponent.Hp, tour.Intent = 1, sect.MoveGuard
	})
	round = player.ok(http.MethodPost, path+"/action", puzzleAction(round, map[string]any{"action": sect.MoveStrike}))
	assert.Equal(t, sect.PhasePick, round["tournament"].(map[string]any)["phase"])

	abandon := puzzleAction(round, map[string]any{"action": "abandon"})
	finished := player.ok(http.MethodPost, path+"/action", abandon)
	assert.Equal(t, model.GameSessionStatusFinished, finished["status"])
	result := finished["result"].(map[string]any)
	assert.Equal(t, float64(3), result["score"])
	assert.Equal(t, 1.0, player.balance())
	detail := result["detail"].(map[string]any)
	assert.Equal(t, float64(3*8), detail["rep"])
	assert.Equal(t, float64(3*40), detail["stone"])

	view = sectView(t, player, map[string]any{"action": "sync"})
	assert.Equal(t, float64(2), view["pills"].(map[string]any)[sect.PillHeal])
	assert.Equal(t, float64(3*8), view["res"].(map[string]any)["rep"])

	again := player.ok(http.MethodPost, path+"/action", abandon)
	assert.Equal(t, result["session_id"], again["session_id"])
	view = sectView(t, player, map[string]any{"action": "sync"})
	assert.Equal(t, float64(3*8), view["res"].(map[string]any)["rep"], "a repeated settlement does not credit the sect again")
	assert.Equal(t, 1.0, player.balance())

	state := player.ok(http.MethodGet, path, nil)
	assert.Nil(t, state["session"])
	player.ok(http.MethodPost, path+"/start", map[string]any{"team": []any{first}})
}
