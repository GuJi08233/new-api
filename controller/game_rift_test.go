package controller

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/doufa"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// editRift 改写玩家进行中的那一轮(测试用：换成好打的房间、把发房时间往前挪)，返回改写后的对局版本。
func editRift(t *testing.T, userId int, edit func(run *doufa.Run)) int {
	t.Helper()
	session, err := model.GetPlayingGameSession(userId, riftGame)
	require.NoError(t, err)
	require.NotNil(t, session)
	state, err := parseRiftState(session)
	require.NoError(t, err)
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
	state, err := parseRiftState(session)
	require.NoError(t, err)
	return state.Run
}

// 秘境斗法一间房一间房地核对：服务端按房间的种子与开局重放操作记录，记录不对、提交得比帧数对应的时长快太多、或是
// 另一个页面已经走了这一步都拒绝；倒下以后按走到多深结算，记下最好成绩，可以再开一轮。
func TestWendaoRiftReplaysEachRoom(t *testing.T) {
	require.NoError(t, i18n.Init())
	env := newGameTestEnv(t, sectSettings())
	player := env.player(0, gameTestIpA)
	base := "/api/game/wendao/rift"
	message := func(key string) string { return i18n.Translate(i18n.LangEn, key) }

	state := player.ok(http.MethodGet, base+"/state", nil)
	assert.Nil(t, state["session"])
	assert.Equal(t, message(i18n.MsgInvalidParams), player.fails(http.MethodPost, base+"/start", map[string]any{"class": "monk"}))
	started := player.ok(http.MethodPost, base+"/start", map[string]any{"class": "warrior"})["session"].(map[string]any)
	assert.Equal(t, message(i18n.MsgGameRoundOpen), player.fails(http.MethodPost, base+"/start", map[string]any{"class": "mage"}))
	id := started["session_id"]
	assert.Equal(t, []any{"fight"}, started["run"].(map[string]any)["choices"])

	act := func(version int, body map[string]any) map[string]any {
		body["session_id"], body["version"] = id, version
		return player.ok(http.MethodPost, base+"/act", body)["session"].(map[string]any)
	}
	reject := func(version int, body map[string]any) string {
		body["session_id"], body["version"] = id, version
		return player.fails(http.MethodPost, base+"/act", body)
	}
	version := int(started["version"].(float64))
	entered := act(version, map[string]any{"action": "enter", "choice": 0})
	version = int(entered["version"].(float64))
	require.NotNil(t, entered["run"].(map[string]any)["room"])
	assert.Equal(t, message(i18n.MsgGameMoveInvalid), reject(version, map[string]any{"action": "enter", "choice": 0}))
	assert.Equal(t, message(i18n.MsgGameReplayInvalid), reject(version, map[string]any{"action": "clear", "inputs": "%%%"}))

	// 换成一只青泥怪的房间：站着攻击就能打赢。刚发出的房间马上交上几百帧的记录，比真打快太多，拒绝。
	version = editRift(t, player.user.Id, func(run *doufa.Run) {
		run.Room.Setup.Room = "glade"
		run.Room.Setup.Waves = [][]doufa.Group{{{Foe: "slime", N: 1}}}
		run.Room.Issued = common.GetTimestamp()
	})
	inputs := playRoom(t, riftRun(t, player.user.Id).Room, true)
	assert.Equal(t, message(i18n.MsgGameReplayInvalid), reject(version, map[string]any{"action": "clear", "inputs": inputs}))
	// 少一帧就没打完，也拒绝。
	short, err := doufa.DecodeInputs(inputs)
	require.NoError(t, err)
	version = editRift(t, player.user.Id, func(run *doufa.Run) { run.Room.Issued -= 600 })
	assert.Equal(t, message(i18n.MsgGameReplayInvalid), reject(version, map[string]any{"action": "clear", "inputs": doufa.EncodeInputs(short[:len(short)-1])}))

	cleared := act(version, map[string]any{"action": "clear", "inputs": inputs})
	run := cleared["run"].(map[string]any)
	assert.Equal(t, float64(1), run["cleared"])
	assert.Nil(t, run["room"])
	assert.Equal(t, message(i18n.MsgGameRoundChanged), reject(version, map[string]any{"action": "clear", "inputs": inputs}),
		"the same room cannot be cleared twice from a page that fell behind")
	version = int(cleared["version"].(float64))
	if run["offer"] != nil {
		version = int(act(version, map[string]any{"action": "pick", "choice": 0, "slot": 1})["version"].(float64))
	}

	// 下一间战斗房换成四只青泥怪，站着不动迟早倒下：这一轮结束并按走到的深度结算。
	for riftRun(t, player.user.Id).Room == nil {
		choices := riftRun(t, player.user.Id).Choices
		choice := 0
		for i, k := range choices {
			if k == "fight" || k == "elite" {
				choice = i
			}
		}
		next := act(version, map[string]any{"action": "enter", "choice": choice})
		version = int(next["version"].(float64))
		if next["run"].(map[string]any)["offer"] != nil {
			version = int(act(version, map[string]any{"action": "pick", "choice": -1})["version"].(float64))
		}
	}
	version = editRift(t, player.user.Id, func(run *doufa.Run) {
		run.Room.Setup.Room = "glade"
		run.Room.Setup.Waves = [][]doufa.Group{{{Foe: "slime", N: 4}}}
		run.Room.Issued -= 3600
	})
	score := riftRun(t, player.user.Id).Score()
	over := act(version, map[string]any{"action": "clear", "inputs": playRoom(t, riftRun(t, player.user.Id).Room, false)})
	assert.Equal(t, model.GameSessionStatusFinished, over["status"])
	assert.Equal(t, true, over["run"].(map[string]any)["over"])
	require.NotNil(t, over["result"])

	state = player.ok(http.MethodGet, base+"/state", nil)
	assert.Nil(t, state["session"])
	assert.Equal(t, float64(score), state["best"])
	player.ok(http.MethodPost, base+"/start", map[string]any{"class": "ranger"})
}

// 放弃也会结算这一轮，之后能马上再开。
func TestWendaoRiftAbandon(t *testing.T) {
	require.NoError(t, i18n.Init())
	env := newGameTestEnv(t, sectSettings())
	player := env.player(0, gameTestIpA)
	base := "/api/game/wendao/rift"
	started := player.ok(http.MethodPost, base+"/start", map[string]any{"class": "mage"})["session"].(map[string]any)
	over := player.ok(http.MethodPost, base+"/act", map[string]any{"session_id": started["session_id"], "version": started["version"],
		"action": "abandon"})["session"].(map[string]any)
	assert.Equal(t, model.GameSessionStatusFinished, over["status"])
	player.ok(http.MethodPost, base+"/start", map[string]any{"class": "mage"})
}
