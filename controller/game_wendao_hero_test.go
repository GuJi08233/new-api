package controller

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/doufa"
	"github.com/QuantumNous/new-api/pkg/dungeon"
	"github.com/QuantumNous/new-api/pkg/wendao"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 创建主角时得到一个储物处(散修，还没开宗)；历练的奖励进储物处；开战后存档里有这一场，退走后结束；通关之前不能登问心阶；
// 问道关闭时不能操作。
func TestHeroJourneyThroughTheApi(t *testing.T) {
	require.NoError(t, i18n.Init())
	env := newGameTestEnv(t, sectSettings())
	player := env.player(0, gameTestIpA)
	path := "/api/game/wendao/hero"

	empty := player.ok(http.MethodGet, path, nil)
	assert.Nil(t, empty["hero"])
	assert.Nil(t, empty["res"], "no sect before the hero is created")
	player.fails(http.MethodPost, path+"/action", map[string]any{"action": "visit", "node": "c1_oath"})

	created := player.ok(http.MethodPost, path+"/action", map[string]any{"action": "create", "class": dungeon.ClassRanger})
	hero := created["hero"].(map[string]any)
	assert.Equal(t, dungeon.ClassRanger, hero["class"])
	assert.Equal(t, []any{"c1_oath"}, hero["open"])
	stone := created["res"].(map[string]any)["stone"].(float64)
	player.fails(http.MethodPost, path+"/action", map[string]any{"action": "create", "class": dungeon.ClassMage})

	assert.Equal(t, []any{}, hero["presets"])
	saved := player.ok(http.MethodPost, path+"/action", map[string]any{"action": "preset_save", "index": 0, "name": "首领"})
	preset := saved["hero"].(map[string]any)["presets"].([]any)[0].(map[string]any)
	assert.Equal(t, "首领", preset["name"])
	assert.Len(t, preset["deck"], len(hero["deck"].([]any)))

	visited := player.ok(http.MethodPost, path+"/action", map[string]any{"action": "visit", "node": "c1_oath"})
	assert.Equal(t, stone+50, visited["res"].(map[string]any)["stone"])
	assert.NotEmpty(t, visited["events"])

	fight := player.ok(http.MethodPost, path+"/action", map[string]any{"action": "fight", "node": "c1_backhill"})
	battle := fight["hero"].(map[string]any)["battle"].(map[string]any)
	assert.Equal(t, "c1_backhill", battle["node"])
	assert.NotContains(t, battle, "run")
	assert.NotContains(t, battle, "seed")
	assert.NotContains(t, battle, "setup")
	issued := fight["fight"].(map[string]any)
	assert.NotNil(t, issued["setup"])
	assert.NotNil(t, issued["seed"])
	read := player.ok(http.MethodGet, path, nil)
	assert.NotContains(t, read, "fight", "reading after a refresh cannot reissue the opening")
	player.fails(http.MethodPost, path+"/action", map[string]any{"action": "visit", "node": "c1_town"})

	fled := player.ok(http.MethodPost, path+"/action", map[string]any{"action": "flee", "battle_token": battle["token"]})
	assert.Nil(t, fled["hero"].(map[string]any)["battle"])
	assert.Equal(t, true, fled["ended"].(map[string]any)["fled"])
	assert.Equal(t, false, fled["hero"].(map[string]any)["steps_open"])
	player.fails(http.MethodPost, path+"/action", map[string]any{"action": "climb"})

	setting := sectSettings()
	setting.Wendao.Enabled = false
	operation_setting.SetGameSettingForTest(setting)
	player.fails(http.MethodPost, path+"/action", map[string]any{"action": "sync"})
}

// journeyReplay 为接口核对固定一场可验证的胜负：敌人安排与发出时间存在临时测试存档里，操作记录由同一规则走到结束。
func journeyReplay(t *testing.T, player *gamePlayer, win bool) (map[string]any, *doufa.Fight) {
	t.Helper()
	editWendao(t, player, func(save *wendao.Save) {
		active := save.Hero.Battle.Fight
		require.NotNil(t, active)
		active.Setup.Room, active.Setup.Waves = "glade", [][]doufa.Group{{{Foe: "slime", N: 1}}}
		active.Setup.Scale = doufa.FoeScale{HP: 100, Dmg: 100}
		active.Issued = common.GetTimestamp() - 3600
	})
	save, err := loadWendaoSave(player.user.Id)
	require.NoError(t, err)
	active := save.Hero.Battle.Fight
	fight, err := doufa.New(active.Seed, active.Setup)
	require.NoError(t, err)
	input := doufa.Pack(-1, -1, 0)
	if win {
		input = doufa.Pack(-1, -1, doufa.BtnAttack)
	}
	var inputs []int
	for !fight.Over {
		inputs = append(inputs, input)
		fight.Step(input)
	}
	require.Equal(t, win, fight.Won)
	return map[string]any{"inputs": doufa.EncodeInputs(inputs), "seed": active.Seed, "issued": active.Issued, "battle_token": save.Hero.Battle.Token}, fight
}

// 历练必须交服务端能重放到结束的操作，且不能提前交；结算只认重放的输赢，断网重交不再发奖励，也不改变后来新开的战斗。
func TestWendaoJourneyReplaySettlesOnce(t *testing.T) {
	require.NoError(t, i18n.Init())
	env := newGameTestEnv(t, sectSettings())
	player := env.player(19, gameTestIpA)
	path := "/api/game/wendao/hero"
	player.ok(http.MethodPost, path+"/action", map[string]any{"action": "create", "class": dungeon.ClassWarrior})
	player.ok(http.MethodPost, path+"/action", map[string]any{"action": "visit", "node": "c1_oath"})
	start := map[string]any{"action": "fight", "node": "c1_backhill", "request_id": "journey-start-1"}
	issued := player.ok(http.MethodPost, path+"/action", start)
	again := player.ok(http.MethodPost, path+"/action", start)
	assert.Equal(t, true, again["replayed"])
	assert.Equal(t, issued["fight"], again["fight"], "lost start answers recover the same opening")
	player.fails(http.MethodPost, path+"/action", map[string]any{"action": "battle", "move": "end"})
	player.fails(http.MethodPost, path+"/clear", map[string]any{"inputs": "AA"})
	request, result := journeyReplay(t, player, true)
	bad := map[string]any{"inputs": "%%%", "seed": request["seed"], "issued": request["issued"], "battle_token": request["battle_token"]}
	player.fails(http.MethodPost, path+"/clear", bad)
	bad["inputs"] = doufa.EncodeInputs([]int{doufa.Pack(-1, -1, doufa.BtnAttack)})
	player.fails(http.MethodPost, path+"/clear", bad)
	// 把服务端发出时间改回现在，即使操作记录确实能赢，也不能在相应时间之前交。
	now := common.GetTimestamp()
	editWendao(t, player, func(save *wendao.Save) { save.Hero.Battle.Fight.Issued = now })
	tooFast := map[string]any{"inputs": request["inputs"], "seed": request["seed"], "issued": now, "battle_token": request["battle_token"]}
	player.fails(http.MethodPost, path+"/clear", tooFast)
	editWendao(t, player, func(save *wendao.Save) { save.Hero.Battle.Fight.Issued = request["issued"].(int64) })

	before := player.ok(http.MethodGet, path, nil)
	request["request_id"] = "journey-clear-1"
	won := player.ok(http.MethodPost, path+"/clear", request)
	ended := won["ended"].(map[string]any)
	assert.Equal(t, true, ended["won"])
	assert.EqualValues(t, result.Tick, ended["ticks"])
	assert.EqualValues(t, result.Kills, ended["kills"])
	assert.Nil(t, won["hero"].(map[string]any)["battle"])
	assert.Greater(t, won["res"].(map[string]any)["stone"], before["res"].(map[string]any)["stone"])
	assert.Equal(t, 19.0, player.balance(), "journey fights never issue game coins")
	retried := player.ok(http.MethodPost, path+"/clear", request)
	assert.Equal(t, true, retried["replayed"])
	assert.Equal(t, won["res"], retried["res"])

	newFight := player.ok(http.MethodPost, path+"/action", map[string]any{"action": "fight", "node": "c1_backhill", "request_id": "journey-start-2"})
	oldStart := player.ok(http.MethodPost, path+"/action", start)
	assert.NotContains(t, oldStart, "fight", "an old start receipt cannot disclose a later opening")
	oldClear := player.ok(http.MethodPost, path+"/clear", request)
	assert.Equal(t, true, oldClear["replayed"])
	assert.Equal(t, newFight["hero"].(map[string]any)["battle"], oldClear["hero"].(map[string]any)["battle"])
	delete(request, "request_id")
	player.fails(http.MethodPost, path+"/clear", request)
	player.fails(http.MethodPost, path+"/action", map[string]any{"action": "flee", "battle_token": request["battle_token"]})
	player.ok(http.MethodPost, path+"/action", map[string]any{"action": "flee", "battle_token": newFight["hero"].(map[string]any)["battle"].(map[string]any)["token"]})
	player.ok(http.MethodPost, path+"/action", map[string]any{"action": "fight", "node": "c1_backhill"})
	lostReplay, _ := journeyReplay(t, player, false)
	lost := player.ok(http.MethodPost, path+"/clear", lostReplay)
	assert.Equal(t, false, lost["ended"].(map[string]any)["won"])
	assert.Equal(t, won["hero"].(map[string]any)["nodes"], lost["hero"].(map[string]any)["nodes"], "a defeat cannot clear a node")
	assert.Equal(t, 19.0, player.balance())
}

// 退回旧版本时遇到更新版本写的存档：读的接口给出明确提示，改写的接口拒绝，存档原样不动。
func TestNewerWendaoSaveIsNotOverwritten(t *testing.T) {
	require.NoError(t, i18n.Init())
	env := newGameTestEnv(t, sectSettings())
	player := env.player(0, gameTestIpA)
	newer := fmt.Sprintf(`{"schema":%d,"sect":{"res":{"stone":5}},"hero":{"class":"mage"},"future":{"x":1}}`, wendao.SaveSchema+1)
	_, err := model.UpdateGameSave(player.user.Id, wendao.SaveKey, func(string) (string, error) { return newer, nil })
	require.NoError(t, err)

	refused := i18n.Translate(i18n.DefaultLang, i18n.MsgGameWendaoNewerSave)
	assert.Equal(t, refused, player.fails(http.MethodGet, "/api/game/wendao", nil))
	assert.Equal(t, refused, player.fails(http.MethodGet, "/api/game/wendao/hero", nil))
	assert.Equal(t, refused, player.fails(http.MethodPost, "/api/game/wendao/hero/action", map[string]any{"action": "sync"}))
	assert.Equal(t, refused, player.fails(http.MethodPost, "/api/game/wendao/sect/action", map[string]any{"action": "sync"}))

	stored, err := model.GetGameSave(player.user.Id, wendao.SaveKey)
	require.NoError(t, err)
	assert.Equal(t, newer, stored)
}

// 问道页顶上的目标与待办：按存档算，只读不写；大比今天的奖励没领满时提醒，大比不发奖励时不提。
func TestWendaoAgendaThroughTheApi(t *testing.T) {
	require.NoError(t, i18n.Init())
	env := newGameTestEnv(t, sectSettings())
	player := env.player(0, gameTestIpA)
	path := "/api/game/wendao/agenda"
	todoKeys := func(agenda map[string]any) []string {
		keys := []string{}
		for _, todo := range agenda["todos"].([]any) {
			keys = append(keys, todo.(map[string]any)["key"].(string))
		}
		return keys
	}

	empty := player.ok(http.MethodGet, path, nil)
	assert.Equal(t, "create", empty["goal"].(map[string]any)["key"])
	assert.Empty(t, empty["todos"], "nothing to do before the sect exists")

	player.ok(http.MethodPost, "/api/game/wendao/hero/action", map[string]any{"action": "create", "class": dungeon.ClassWarrior})
	editSect(t, player, func(s *wendao.Sect) {
		s.Missions = []*wendao.Mission{{Id: 1, Key: "patrol", Start: 1, End: 2}}
	})
	before, err := model.GetGameSave(player.user.Id, wendao.SaveKey)
	require.NoError(t, err)
	agenda := player.ok(http.MethodGet, path, nil)
	goal := agenda["goal"].(map[string]any)
	assert.Equal(t, "node", goal["key"])
	assert.Equal(t, "c1_oath", goal["s"])
	assert.Equal(t, []string{"missions", "garden_empty", "tournament_reward"}, todoKeys(agenda))
	after, err := model.GetGameSave(player.user.Id, wendao.SaveKey)
	require.NoError(t, err)
	assert.Equal(t, before, after, "reading the agenda does not write the save")

	setting := sectSettings()
	setting.WendaoTournament.DailyRewards = 0
	operation_setting.SetGameSettingForTest(setting)
	assert.Equal(t, []string{"missions", "garden_empty"}, todoKeys(player.ok(http.MethodGet, path, nil)))

	setting.Wendao.Enabled = false
	operation_setting.SetGameSettingForTest(setting)
	player.fails(http.MethodGet, path, nil)
}

// 周天试炼通过接口：打败第一章首领之前没有；之后读主角能看到这一周的三关，开战记着是第几关，规则里有天象表。
func TestWendaoTrialThroughTheApi(t *testing.T) {
	require.NoError(t, i18n.Init())
	env := newGameTestEnv(t, sectSettings())
	player := env.player(0, gameTestIpA)
	path := "/api/game/wendao/hero"

	created := player.ok(http.MethodPost, path+"/action", map[string]any{"action": "create", "class": dungeon.ClassWarrior})
	assert.Nil(t, created["hero"].(map[string]any)["weekly"])
	player.fails(http.MethodPost, path+"/action", map[string]any{"action": "trial", "index": 1})

	_, err := model.UpdateGameSave(player.user.Id, wendao.SaveKey, func(data string) (string, error) {
		return wendao.UpdateSave(data, func(save *wendao.Save) error {
			for i := range wendao.JourneyNodes {
				if n := &wendao.JourneyNodes[i]; n.Chapter == 0 {
					save.Hero.Nodes[n.Id] = &wendao.NodeRecord{Clears: 1}
				}
			}
			return nil
		})
	})
	require.NoError(t, err)
	weekly := player.ok(http.MethodGet, path, nil)["hero"].(map[string]any)["weekly"].(map[string]any)
	assert.Len(t, weekly["trials"], wendao.WeeklyTrials)
	assert.Equal(t, float64(0), weekly["chapter"])
	assert.Greater(t, weekly["ends"], float64(0))

	fight := player.ok(http.MethodPost, path+"/action", map[string]any{"action": "trial", "index": 2})
	battle := fight["hero"].(map[string]any)["battle"].(map[string]any)
	assert.Equal(t, float64(2), battle["trial"])
	player.ok(http.MethodPost, path+"/action", map[string]any{"action": "flee", "battle_token": battle["token"]})

	journey := player.ok(http.MethodGet, "/api/game/wendao", nil)["journey"].(map[string]any)
	assert.Len(t, journey["weekly"].(map[string]any)["signs"], len(wendao.WeeklySigns))
}
