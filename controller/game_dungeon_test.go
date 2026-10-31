package controller

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/dungeon"
	"github.com/QuantumNous/new-api/pkg/wendao"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// dungeonSettings 打开问道：秘境打败一个首领奖励 1 个游戏币，不限游戏时长。
func dungeonSettings() *operation_setting.GameSetting {
	return gameSetting(func(s *operation_setting.GameSetting) {
		s.Wendao = puzzleRule()
	})
}

func dungeonMeta(t *testing.T, player *gamePlayer) map[string]any {
	t.Helper()
	return player.ok(http.MethodGet, "/api/game/wendao", nil)["abyss"].(map[string]any)["meta"].(map[string]any)
}

// editDungeonRun 直接改写进行中那一轮的状态，用来造出打败首领之类要玩很久才能到的局面。
func editDungeonRun(t *testing.T, env *gameTestEnv, id any, edit func(run *dungeon.Run)) {
	t.Helper()
	var session model.GameSession
	require.NoError(t, env.db.First(&session, int(id.(float64))).Error)
	var state dungeonState
	require.NoError(t, common.UnmarshalJsonStr(session.State, &state))
	edit(state.Run)
	data, err := common.Marshal(state)
	require.NoError(t, err)
	require.NoError(t, env.db.Model(&session).Update("state", string(data)).Error)
}

// 一轮远征结束时按成绩领游戏币、魂晶记进存档；重复提交拿到同一次结算，魂晶不会记两次。进行中的一轮没结束时不能开新的一轮。
func TestDungeonRunSettlesCoinsAndCrystalsOnce(t *testing.T) {
	env := newGameTestEnv(t, dungeonSettings())
	player := env.player(0, gameTestIpA)
	path := "/api/game/wendao/abyss"
	view := player.ok(http.MethodPost, path+"/start", map[string]any{"class": dungeon.ClassWarrior, "abyss": 0})
	player.fails(http.MethodPost, path+"/start", map[string]any{"class": dungeon.ClassWarrior, "abyss": 0})

	run := view["run"].(map[string]any)
	node := run["choices"].([]any)[0]
	ageSession(t, env, view["session_id"], 60)
	view = player.ok(http.MethodPost, path+"/action", puzzleAction(view, map[string]any{"action": "path", "node": node}))
	assert.Equal(t, dungeon.PhaseCombat, view["run"].(map[string]any)["phase"])
	player.fails(http.MethodPost, path+"/action", puzzleAction(view, map[string]any{"action": "play", "card": 99, "target": 0}))

	editDungeonRun(t, env, view["session_id"], func(run *dungeon.Run) { run.Bosses = 1 })
	abandon := puzzleAction(view, map[string]any{"action": "abandon"})
	finished := player.ok(http.MethodPost, path+"/action", abandon)
	assert.Equal(t, model.GameSessionStatusFinished, finished["status"])
	result := finished["result"].(map[string]any)
	assert.Equal(t, float64(1), result["score"])
	assert.Equal(t, 1.0, player.balance())
	crystals := float64(1 + 10) // 到达一层、打败一个首领
	assert.Equal(t, crystals, result["detail"].(map[string]any)["crystals"])
	assert.Equal(t, crystals, dungeonMeta(t, player)["crystals"])

	again := player.ok(http.MethodPost, path+"/action", abandon)
	assert.Equal(t, result["session_id"], again["session_id"])
	assert.Equal(t, crystals, dungeonMeta(t, player)["crystals"], "a repeated settlement does not credit crystals again")
	assert.Equal(t, 1.0, player.balance())

	state := player.ok(http.MethodGet, path+"/state", nil)
	assert.Nil(t, state["session"], "no run in progress after settling")
	player.ok(http.MethodPost, path+"/start", map[string]any{"class": dungeon.ClassWarrior, "abyss": 0})
}

// 魂晶解锁职业与天赋；没解锁的职业和深渊等级不能开局。
func TestDungeonUnlockSpendsCrystals(t *testing.T) {
	require.NoError(t, i18n.Init())
	message := func(key string) string { return i18n.Translate(i18n.LangEn, key) }
	env := newGameTestEnv(t, dungeonSettings())
	player := env.player(0, gameTestIpA)
	path := "/api/game/wendao/abyss"
	assert.Equal(t, message(i18n.MsgGameDungeonLocked), player.fails(http.MethodPost, path+"/start", map[string]any{"class": dungeon.ClassMage, "abyss": 0}))
	player.fails(http.MethodPost, path+"/start", map[string]any{"class": dungeon.ClassWarrior, "abyss": 1})
	assert.Equal(t, message(i18n.MsgGameDungeonCrystals), player.fails(http.MethodPost, path+"/unlock", map[string]any{"item": "class:" + dungeon.ClassMage}))

	_, err := model.UpdateGameSave(player.user.Id, wendao.SaveKey, func(data string) (string, error) {
		return updateAbyssSave(data, func(meta *dungeon.Meta) error {
			meta.Crystals = 60
			return nil
		})
	})
	require.NoError(t, err)
	meta := player.ok(http.MethodPost, path+"/unlock", map[string]any{"item": "class:" + dungeon.ClassMage})["meta"].(map[string]any)
	assert.Equal(t, float64(60-dungeon.ClassCosts[dungeon.ClassMage]), meta["crystals"])
	assert.Equal(t, message(i18n.MsgGameDungeonMaxed), player.fails(http.MethodPost, path+"/unlock", map[string]any{"item": "class:" + dungeon.ClassMage}))
	player.fails(http.MethodPost, path+"/unlock", map[string]any{"item": "talent:unknown"})

	view := player.ok(http.MethodPost, path+"/start", map[string]any{"class": dungeon.ClassMage, "abyss": 0})
	assert.Equal(t, dungeon.ClassMage, view["run"].(map[string]any)["class"])
}

// 试炼过关按星级发魂晶并解锁下一关；试炼与无尽远征是两局，开试炼不会作废进行中的远征。
func TestDungeonTrialAwardsStarsWithoutTouchingEndlessRun(t *testing.T) {
	env := newGameTestEnv(t, dungeonSettings())
	player := env.player(0, gameTestIpA)
	path := "/api/game/wendao/abyss"
	endless := player.ok(http.MethodPost, path+"/start", map[string]any{"class": dungeon.ClassWarrior})
	player.fails(http.MethodPost, path+"/start", map[string]any{"mode": dungeon.ModeTrial, "trial": dungeon.Trials[1].Id})

	trial := player.ok(http.MethodPost, path+"/start", map[string]any{"mode": dungeon.ModeTrial, "trial": dungeon.Trials[0].Id})
	editDungeonRun(t, env, trial["session_id"], func(run *dungeon.Run) {
		run.Combat.Hand = []dungeon.Card{{Id: "w_strike"}}
		run.Combat.Energy = 3
		for _, e := range run.Combat.Enemies {
			e.HP, e.Block = 1, 0
		}
	})
	ageSession(t, env, trial["session_id"], 60)
	body := puzzleAction(trial, map[string]any{"mode": dungeon.ModeTrial, "action": "play", "card": 0, "target": 0})
	finished := player.ok(http.MethodPost, path+"/action", body)
	assert.Equal(t, model.GameSessionStatusFinished, finished["status"])
	detail := finished["result"].(map[string]any)["detail"].(map[string]any)
	assert.Equal(t, float64(3), detail["stars"])
	crystals := float64(3 * dungeon.Trials[0].Crystals)
	assert.Equal(t, crystals, detail["crystals"])
	meta := dungeonMeta(t, player)
	assert.Equal(t, crystals, meta["crystals"])
	assert.Equal(t, float64(3), meta["trials"].(map[string]any)[dungeon.Trials[0].Id])
	assert.Equal(t, 0.0, player.balance(), "trials never pay game coins")

	player.ok(http.MethodPost, path+"/start", map[string]any{"mode": dungeon.ModeTrial, "trial": dungeon.Trials[1].Id})
	state := player.ok(http.MethodGet, path+"/state", nil)["session"].(map[string]any)
	assert.Equal(t, endless["session_id"], state["session_id"], "the endless run is still in progress")
}

// 练习可以随意配置、随时重来，不能放弃结算，也不影响存档。
func TestDungeonPracticeResetsAndNeverSettles(t *testing.T) {
	env := newGameTestEnv(t, dungeonSettings())
	player := env.player(0, gameTestIpA)
	path := "/api/game/wendao/abyss"
	setup := map[string]any{"class": dungeon.ClassRanger, "deck": []map[string]any{{"id": "r_shoot"}, {"id": "r_volley", "up": true}},
		"enemies": []string{dungeon.DummyEnemy}, "stage": 1, "infinite_energy": true}
	invalid := map[string]any{"class": dungeon.ClassRanger, "deck": []map[string]any{}, "enemies": []string{dungeon.DummyEnemy}, "stage": 1}
	player.fails(http.MethodPost, path+"/start", map[string]any{"mode": dungeon.ModePractice, "practice": invalid})

	view := player.ok(http.MethodPost, path+"/start", map[string]any{"mode": dungeon.ModePractice, "practice": setup})
	ageSession(t, env, view["session_id"], 60)
	view = player.ok(http.MethodPost, path+"/action", puzzleAction(view, map[string]any{"mode": dungeon.ModePractice, "action": "end"}))
	combat := view["run"].(map[string]any)["combat"].(map[string]any)
	assert.Equal(t, float64(2), combat["turn"])
	player.fails(http.MethodPost, path+"/action", puzzleAction(view, map[string]any{"mode": dungeon.ModePractice, "action": "abandon"}))

	view = player.ok(http.MethodPost, path+"/action", puzzleAction(view, map[string]any{"mode": dungeon.ModePractice, "action": "reset"}))
	combat = view["run"].(map[string]any)["combat"].(map[string]any)
	assert.Equal(t, float64(1), combat["turn"])
	state := player.ok(http.MethodGet, path+"/state?mode="+dungeon.ModePractice, nil)["session"].(map[string]any)
	assert.Equal(t, view["version"], state["version"])
	assert.Equal(t, float64(0), dungeonMeta(t, player)["runs"])
}
