package controller

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/doufa"
	"github.com/QuantumNous/new-api/pkg/wendao"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const riftBase = "/api/game/wendao/rift"

// riftSettings 打开问道：九幽秘境打通一重奖励 1 个游戏币，不限游戏时长。
func riftSettings() *operation_setting.GameSetting {
	return gameSetting(func(s *operation_setting.GameSetting) { s.Wendao = puzzleRule() })
}

// editRift 改写玩家进行中的那一轮(测试用：换成好打的房间、把发房时间往前挪、造出要打很久才到的局面)，返回改写后的对局版本。
func editRift(t *testing.T, userId int, edit func(run *doufa.Run)) int {
	t.Helper()
	session, err := model.GetPlayingGameSession(userId, riftGame)
	require.NoError(t, err)
	require.NotNil(t, session)
	state := parseRiftState(session)
	edit(state.Run)
	data, err := common.Marshal(state)
	require.NoError(t, err)
	require.NoError(t, model.UpdateGameSessionState(session, string(data), ""))
	return session.Version
}

// playRoom 在服务端规则上把这间房打到结束：attack 为真时站着自动瞄准一直攻击，否则站着不动。返回操作记录。
func playRoom(t *testing.T, room *doufa.RunRoom, attack bool) string {
	t.Helper()
	fight, err := doufa.New(room.Seed, room.Setup)
	require.NoError(t, err)
	input := doufa.Pack(-1, -1, 0)
	if attack {
		input = doufa.Pack(-1, -1, doufa.BtnAttack)
	}
	var inputs []int
	for !fight.Over {
		inputs = append(inputs, input)
		fight.Step(input)
	}
	return doufa.EncodeInputs(inputs)
}

func riftRun(t *testing.T, userId int) *doufa.Run {
	t.Helper()
	session, err := model.GetPlayingGameSession(userId, riftGame)
	require.NoError(t, err)
	require.NotNil(t, session)
	return parseRiftState(session).Run
}

// riftMeta 读出玩家问道存档里秘境斗法的局外成长。
func riftMeta(t *testing.T, player *gamePlayer) map[string]any {
	t.Helper()
	return player.ok(http.MethodGet, "/api/game/wendao", nil)["abyss"].(map[string]any)["meta"].(map[string]any)
}

// editRiftMeta 直接改写玩家存档里的局外成长(测试用：攒好道韵)。
func editRiftMeta(t *testing.T, player *gamePlayer, edit func(meta *doufa.Meta)) {
	t.Helper()
	_, err := model.UpdateGameSave(player.user.Id, wendao.SaveKey, func(data string) (string, error) {
		return wendao.UpdateSave(data, func(save *wendao.Save) error {
			edit(save.Rift)
			return nil
		})
	})
	require.NoError(t, err)
}

// riftAct 对进行中的一轮走一步，返回回应里的局面。
func riftAct(player *gamePlayer, session map[string]any, body map[string]any) map[string]any {
	body["session_id"], body["version"] = session["session_id"], session["version"]
	return player.ok(http.MethodPost, riftBase+"/act", body)["session"].(map[string]any)
}

// riftReject 走一步，要求被拒绝，返回提示。
func riftReject(player *gamePlayer, session map[string]any, body map[string]any) string {
	body["session_id"], body["version"] = session["session_id"], session["version"]
	return player.fails(http.MethodPost, riftBase+"/act", body)
}

// 秘境斗法一间房一间房地核对：服务端按房间的种子与开局重放操作记录，记录不对、提交得比帧数对应的时长快太多、或是
// 另一个页面已经走了这一步都拒绝；倒下以后按打通的重数结算，记下最好成绩，可以再开一轮。
func TestWendaoRiftReplaysEachRoom(t *testing.T) {
	require.NoError(t, i18n.Init())
	env := newGameTestEnv(t, sectSettings())
	player := env.player(0, gameTestIpA)
	message := func(key string) string { return i18n.Translate(i18n.LangEn, key) }

	state := player.ok(http.MethodGet, riftBase+"/state", nil)
	assert.Nil(t, state["session"])
	assert.Equal(t, message(i18n.MsgInvalidParams), player.fails(http.MethodPost, riftBase+"/start", map[string]any{"class": "monk"}))
	started := player.ok(http.MethodPost, riftBase+"/start", map[string]any{"class": "warrior"})["session"].(map[string]any)
	assert.Equal(t, message(i18n.MsgGameRoundOpen), player.fails(http.MethodPost, riftBase+"/start", map[string]any{"class": "mage"}))
	assert.Equal(t, []any{"fight"}, started["run"].(map[string]any)["choices"])

	entered := riftAct(player, started, map[string]any{"action": "enter", "choice": 0})
	room := entered["run"].(map[string]any)["room"].(map[string]any)
	assert.NotZero(t, room["seed"], "entering a room delivers its seed and setup")
	assert.NotNil(t, room["setup"])
	assert.Equal(t, message(i18n.MsgGameMoveInvalid), riftReject(player, entered, map[string]any{"action": "enter", "choice": 0}))
	assert.Equal(t, message(i18n.MsgGameReplayInvalid), riftReject(player, entered, map[string]any{"action": "clear", "inputs": "%%%"}))

	// 读状态拿不到已经发出的房间的种子与开局，只知道有这么一间房。
	hidden := player.ok(http.MethodGet, riftBase+"/state", nil)["session"].(map[string]any)["run"].(map[string]any)["room"].(map[string]any)
	assert.Equal(t, true, hidden["hidden"])
	assert.Equal(t, "fight", hidden["kind"])
	assert.Zero(t, hidden["seed"])

	// 换成一只青泥怪的房间：站着攻击就能打赢。刚发出的房间马上交上几百帧的记录，比真打快太多，拒绝。
	version := editRift(t, player.user.Id, func(run *doufa.Run) {
		run.Room.Setup.Room = "glade"
		run.Room.Setup.Waves = [][]doufa.Group{{{Foe: "slime", N: 1}}}
		run.Room.Issued = common.GetTimestamp()
	})
	entered["version"] = version
	inputs := playRoom(t, riftRun(t, player.user.Id).Room, true)
	assert.Equal(t, message(i18n.MsgGameReplayInvalid), riftReject(player, entered, map[string]any{"action": "clear", "inputs": inputs}))
	// 少一帧就没打完，也拒绝。
	short, err := doufa.DecodeInputs(inputs)
	require.NoError(t, err)
	entered["version"] = editRift(t, player.user.Id, func(run *doufa.Run) { run.Room.Issued -= 600 })
	assert.Equal(t, message(i18n.MsgGameReplayInvalid), riftReject(player, entered, map[string]any{"action": "clear", "inputs": doufa.EncodeInputs(short[:len(short)-1])}))

	cleared := riftAct(player, entered, map[string]any{"action": "clear", "inputs": inputs})
	run := cleared["run"].(map[string]any)
	assert.Equal(t, float64(1), run["cleared"])
	assert.Nil(t, run["room"])
	assert.Equal(t, message(i18n.MsgGameRoundChanged), riftReject(player, entered, map[string]any{"action": "clear", "inputs": inputs}),
		"the same room cannot be cleared twice from a page that fell behind")
	if run["offer"] != nil {
		cleared = riftAct(player, cleared, map[string]any{"action": "pick", "choice": 0, "slot": 1})
	}

	// 下一间战斗房换成四只青泥怪，站着不动迟早倒下：这一轮结束并按打通的重数结算。
	next := cleared
	for riftRun(t, player.user.Id).Room == nil {
		choice := 0
		for i, k := range riftRun(t, player.user.Id).Choices {
			if k == "fight" || k == "elite" {
				choice = i
			}
		}
		next = riftAct(player, next, map[string]any{"action": "enter", "choice": choice})
		if next["run"].(map[string]any)["offer"] != nil {
			next = riftAct(player, next, map[string]any{"action": "pick", "choice": -1})
		}
	}
	next["version"] = editRift(t, player.user.Id, func(run *doufa.Run) {
		run.Room.Setup.Room = "glade"
		run.Room.Setup.Waves = [][]doufa.Group{{{Foe: "slime", N: 4}}}
		run.Room.Issued -= 3600
	})
	over := riftAct(player, next, map[string]any{"action": "clear", "inputs": playRoom(t, riftRun(t, player.user.Id).Room, false)})
	assert.Equal(t, model.GameSessionStatusFinished, over["status"])
	assert.Equal(t, true, over["run"].(map[string]any)["over"])
	require.NotNil(t, over["result"])

	state = player.ok(http.MethodGet, riftBase+"/state", nil)
	assert.Nil(t, state["session"])
	assert.Equal(t, float64(0), state["best"], "no boss beaten, no floor cleared")
	player.ok(http.MethodPost, riftBase+"/start", map[string]any{"class": "ranger"})
}

// 一轮结束按打通的重数领游戏币，道韵、战绩和带回宗门的材料在同一次结算里记进存档；最后一步的请求重发拿到同一次结算，
// 游戏币与道韵都不会记两遍。一间房也没打通就放弃，什么都不记。
func TestWendaoRiftSettlesCoinsAndCrystalsOnce(t *testing.T) {
	env := newGameTestEnv(t, riftSettings())
	player := env.player(0, gameTestIpA)
	foundTestSect(t, player)
	stone := sectView(t, player, map[string]any{"action": "sync"})["res"].(map[string]any)["stone"].(float64)

	empty := player.ok(http.MethodPost, riftBase+"/start", map[string]any{"class": "warrior"})["session"].(map[string]any)
	over := riftAct(player, empty, map[string]any{"action": "abandon"})
	assert.Equal(t, model.GameSessionStatusFinished, over["status"])
	assert.Equal(t, 0.0, player.balance())
	assert.Equal(t, float64(0), riftMeta(t, player)["runs"], "a run that cleared no room is not a finished run")

	started := player.ok(http.MethodPost, riftBase+"/start", map[string]any{"class": "mage"})["session"].(map[string]any)
	started["version"] = editRift(t, player.user.Id, func(run *doufa.Run) {
		run.Floor, run.Step, run.Cleared = 2, 3, 8 // 打通了第一重
	})
	abandon := map[string]any{"action": "abandon", "session_id": started["session_id"], "version": started["version"]}
	finished := player.ok(http.MethodPost, riftBase+"/act", abandon)["session"].(map[string]any)
	assert.Equal(t, model.GameSessionStatusFinished, finished["status"])
	result := finished["result"].(map[string]any)
	assert.Equal(t, float64(1), result["score"], "the score is the number of floors cleared")
	assert.Equal(t, 1.0, player.balance())
	detail := result["detail"].(map[string]any)
	crystals := float64(2*8 + 12)
	assert.Equal(t, crystals, detail["crystals"])
	loot := detail["loot"].(map[string]any)
	assert.Equal(t, float64(12*8+150), loot["stone"])
	meta := riftMeta(t, player)
	assert.Equal(t, crystals, meta["crystals"])
	assert.Equal(t, float64(1), meta["runs"])
	assert.Equal(t, float64(1*doufa.FloorSteps+3), meta["records"].(map[string]any)["mage"].(map[string]any)["best"])
	after := sectView(t, player, map[string]any{"action": "sync"})["res"].(map[string]any)["stone"].(float64)
	assert.GreaterOrEqual(t, after, stone+12*8+150, "the loot lands in the sect storehouse")

	again := player.ok(http.MethodPost, riftBase+"/act", abandon)["session"].(map[string]any)
	assert.Equal(t, model.GameSessionStatusFinished, again["status"])
	assert.Equal(t, result["session_id"], again["result"].(map[string]any)["session_id"])
	assert.Equal(t, crystals, riftMeta(t, player)["crystals"], "a repeated settlement does not credit crystals again")
	assert.Equal(t, 1.0, player.balance())
	assert.Nil(t, player.ok(http.MethodGet, riftBase+"/state", nil)["session"], "no run in progress after settling")

	// 今天的奖励领过了：再打通一重照样记道韵，不再发游戏币。
	second := player.ok(http.MethodPost, riftBase+"/start", map[string]any{"class": "warrior"})["session"].(map[string]any)
	second["version"] = editRift(t, player.user.Id, func(run *doufa.Run) { run.Floor, run.Cleared = 3, 5 })
	done := riftAct(player, second, map[string]any{"action": "abandon"})["result"].(map[string]any)
	assert.Equal(t, float64(2), done["score"])
	assert.Equal(t, 1.0, player.balance(), "one coin reward a day")
	assert.Equal(t, crystals+float64(2*5+12*2), riftMeta(t, player)["crystals"])
}

// 退路：从发出的房间里退出来要用一次，同一间房换个种子、回应里带着新的种子；退路用完再退出这一轮就结束。
// 没有房间可退，或者拿旧版本号来退，都拒绝。
func TestWendaoRiftRetreatIsHowARoomIsReissued(t *testing.T) {
	require.NoError(t, i18n.Init())
	env := newGameTestEnv(t, riftSettings())
	player := env.player(0, gameTestIpA)
	message := func(key string) string { return i18n.Translate(i18n.LangEn, key) }
	started := player.ok(http.MethodPost, riftBase+"/start", map[string]any{"class": "warrior"})["session"].(map[string]any)
	assert.Equal(t, float64(doufa.BaseRetreats), started["run"].(map[string]any)["retreats"])
	assert.Equal(t, message(i18n.MsgGameMoveInvalid), riftReject(player, started, map[string]any{"action": "retreat"}), "no room to leave")

	entered := riftAct(player, started, map[string]any{"action": "enter", "choice": 0})
	room := entered["run"].(map[string]any)["room"].(map[string]any)
	retreated := riftAct(player, entered, map[string]any{"action": "retreat"})
	assert.Equal(t, message(i18n.MsgGameRoundChanged), riftReject(player, entered, map[string]any{"action": "retreat"}), "a stale page cannot spend another retreat")
	again := retreated["run"].(map[string]any)
	assert.Equal(t, float64(doufa.BaseRetreats-1), again["retreats"])
	assert.NotEqual(t, room["seed"], again["room"].(map[string]any)["seed"])
	assert.Equal(t, room["setup"], again["room"].(map[string]any)["setup"], "same enemies, same health")

	for range doufa.BaseRetreats - 1 {
		retreated = riftAct(player, retreated, map[string]any{"action": "retreat"})
	}
	assert.Equal(t, float64(0), retreated["run"].(map[string]any)["retreats"])
	over := riftAct(player, retreated, map[string]any{"action": "retreat"})
	assert.Equal(t, model.GameSessionStatusFinished, over["status"], "leaving with no retreat left is falling in the room")
	assert.Equal(t, true, over["run"].(map[string]any)["over"])
	player.ok(http.MethodPost, riftBase+"/start", map[string]any{"class": "mage"})
}

// 道韵买天赋、天赋在下一轮开局生效；游戏币买商店道具，道具在两间房之间用，用掉一件背包少一件，与新局面一起保存。
func TestWendaoRiftTalentsAndItems(t *testing.T) {
	require.NoError(t, i18n.Init())
	message := func(key string) string { return i18n.Translate(i18n.LangEn, key) }
	env := newGameTestEnv(t, riftSettings())
	player := env.player(5, gameTestIpA)
	assert.Equal(t, message(i18n.MsgGameDungeonCrystals), player.fails(http.MethodPost, riftBase+"/talent", map[string]any{"id": "anchor"}))
	assert.Equal(t, message(i18n.MsgInvalidParams), player.fails(http.MethodPost, riftBase+"/talent", map[string]any{"id": "nonsense"}))
	editRiftMeta(t, player, func(meta *doufa.Meta) { meta.Crystals = 100 })
	meta := player.ok(http.MethodPost, riftBase+"/talent", map[string]any{"id": "anchor"})["meta"].(map[string]any)
	assert.Equal(t, float64(60), meta["crystals"])
	assert.Equal(t, map[string]any{"anchor": float64(1)}, meta["talents"])
	assert.Equal(t, message(i18n.MsgGameDungeonMaxed), player.fails(http.MethodPost, riftBase+"/talent", map[string]any{"id": "anchor"}))

	started := player.ok(http.MethodPost, riftBase+"/start", map[string]any{"class": "ranger"})["session"].(map[string]any)
	run := started["run"].(map[string]any)
	assert.Equal(t, float64(doufa.BaseRetreats+1), run["retreats"], "the talent comes along from the start")
	assert.Equal(t, map[string]any{"anchor": float64(1)}, run["tal"])

	// 背包里没有的道具用不了；买来以后生命是满的时候续命丹不能用，也不扣。
	assert.Equal(t, message(i18n.MsgGameShopNoItem), riftReject(player, started, map[string]any{"action": "item", "item": doufa.ItemElixir}))
	bought := player.ok(http.MethodPost, riftBase+"/shop/buy", map[string]any{"item": doufa.ItemElixir, "count": 2})
	assert.Equal(t, map[string]any{doufa.ItemElixir: float64(2)}, bought["meta"].(map[string]any)["items"])
	assert.Equal(t, message(i18n.MsgGameShopItemLimit), riftReject(player, started, map[string]any{"action": "item", "item": doufa.ItemElixir}))
	assert.Equal(t, map[string]any{doufa.ItemElixir: float64(2)}, riftMeta(t, player)["items"])

	started["version"] = editRift(t, player.user.Id, func(run *doufa.Run) { run.HP = 10 })
	used := riftAct(player, started, map[string]any{"action": "item", "item": doufa.ItemElixir})
	healed := used["run"].(map[string]any)["hp"].(float64)
	assert.Greater(t, healed, float64(10))
	assert.Equal(t, float64(1), used["run"].(map[string]any)["used"])
	assert.Equal(t, map[string]any{doufa.ItemElixir: float64(1)}, used["meta"].(map[string]any)["items"])
	assert.Equal(t, used["version"], player.ok(http.MethodGet, riftBase+"/state", nil)["session"].(map[string]any)["version"], "the item and the new state were saved together")
}

// 旧版卡牌秘境留在对局里的一局(或坏掉的状态)读不成斗法的一轮：当作按旧版规则开始的，只能放弃，放弃以后能开新的一轮。
func TestWendaoRiftOldSessionCanOnlyBeAbandoned(t *testing.T) {
	require.NoError(t, i18n.Init())
	env := newGameTestEnv(t, riftSettings())
	player := env.player(0, gameTestIpA)
	old, err := model.CreateGameSession(player.user.Id, riftGame, 7, `{"steps":3,"run":{"phase":"map","choices":[{"id":4}],"hp":"many"}}`)
	require.NoError(t, err)

	state := player.ok(http.MethodGet, riftBase+"/state", nil)["session"].(map[string]any)
	assert.Equal(t, true, state["outdated"])
	assert.Equal(t, float64(old.Id), state["session_id"])
	assert.Equal(t, i18n.Translate(i18n.LangEn, i18n.MsgGameRoundOpen), player.fails(http.MethodPost, riftBase+"/start", map[string]any{"class": "warrior"}))
	assert.Equal(t, i18n.Translate(i18n.LangEn, i18n.MsgGameDungeonOutdated), riftReject(player, state, map[string]any{"action": "enter", "choice": 0}))

	over := riftAct(player, state, map[string]any{"action": "abandon"})
	assert.Equal(t, model.GameSessionStatusFinished, over["status"])
	assert.Equal(t, float64(0), over["result"].(map[string]any)["score"])
	player.ok(http.MethodPost, riftBase+"/start", map[string]any{"class": "warrior"})
}
