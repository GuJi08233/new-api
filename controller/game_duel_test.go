package controller

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/pkg/doufa"
	"github.com/QuantumNous/new-api/pkg/dungeon"
	"github.com/QuantumNous/new-api/pkg/wendao"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const duelBase = "/api/game/wendao/duel"

// duelHero 给玩家建一个主角(散修)，并让榜单排到今天。
func duelHero(t *testing.T, player *gamePlayer) {
	t.Helper()
	player.ok(http.MethodPost, "/api/game/wendao/hero/action", map[string]any{"action": "create", "class": dungeon.ClassWarrior})
	player.ok(http.MethodPost, "/api/game/wendao/sect/action", map[string]any{"action": "sync"})
	player.ok(http.MethodPost, duelBase+"/giveup", map[string]any{}) // 排好今天的榜单，没有进行中的一场也不报错
}

// rigDuel 把进行中的一场换成好打或必输的房间(站着攻击能打赢一只青泥怪，站着不动被四只青泥怪打倒)，并把发出时间往前挪，
// 免得提交得比真打快。返回对应的操作记录。
func rigDuel(t *testing.T, player *gamePlayer, win bool) string {
	t.Helper()
	editWendao(t, player, func(save *wendao.Save) {
		active := save.Duels.Active
		require.NotNil(t, active)
		active.Setup.Room = "glade"
		active.Setup.Waves = [][]doufa.Group{{{Foe: "slime", N: 1}}}
		if !win {
			active.Setup.Waves = [][]doufa.Group{{{Foe: "slime", N: 4}}}
		}
		active.Setup.Scale = doufa.FoeScale{HP: 100, Dmg: 100}
		active.Issued -= 3600
	})
	save, err := loadWendaoSave(player.user.Id)
	require.NoError(t, err)
	fight, err := doufa.New(save.Duels.Active.Seed, save.Duels.Active.Setup)
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
	return doufa.EncodeInputs(inputs)
}

// joinThroughTrial 拜入 sect：接下入门试炼，换成好打的房间，交上记录。返回提交的回应。
func joinThroughTrial(t *testing.T, player *gamePlayer, sect string) map[string]any {
	t.Helper()
	player.ok(http.MethodPost, duelBase+"/start", map[string]any{"kind": wendao.DuelTrial, "id": sect})
	return player.ok(http.MethodPost, duelBase+"/clear", map[string]any{"inputs": rigDuel(t, player, true)})
}

// bounty 返回今天榜上第 i 张悬赏，并把它挪到第一个地域(新主角只够得着那里)。
func bountyAt(t *testing.T, player *gamePlayer, i int) string {
	t.Helper()
	var id string
	editWendao(t, player, func(save *wendao.Save) {
		save.Duels.Bounties[i].Region = 0
		id = save.Duels.Bounties[i].Id
	})
	return id
}

// 入门试炼：接下的回应里有种子与开局，读取状态拿不到；打赢才拜入宗门；记录对不上或交得太快拒绝；没打完的一场只能放弃，
// 放弃不退次数；每个宗门每天五次。
func TestWendaoDuelEntryTrialDecidesJoining(t *testing.T) {
	require.NoError(t, i18n.Init())
	env := newGameTestEnv(t, sectSettings())
	player := env.player(0, gameTestIpA)
	message := func(key string) string { return i18n.Translate(i18n.LangEn, key) }
	player.fails(http.MethodPost, duelBase+"/start", map[string]any{"kind": wendao.DuelTrial, "id": "jingang"})
	duelHero(t, player)

	started := player.ok(http.MethodPost, duelBase+"/start", map[string]any{"kind": wendao.DuelTrial, "id": "jingang"})
	duel := started["duel"].(map[string]any)
	assert.NotZero(t, duel["seed"], "the answer to starting carries the fight")
	assert.NotNil(t, duel["setup"])
	assert.Equal(t, map[string]any{"kind": "trial", "id": "jingang"}, started["book"].(map[string]any)["active"])
	assert.Equal(t, float64(1), started["book"].(map[string]any)["trials"].(map[string]any)["jingang"])

	// 读取状态只知道有这么一场，没有种子；另一个页面不能再开一场。
	state := player.ok(http.MethodGet, duelBase+"/state", nil)
	assert.Equal(t, map[string]any{"kind": "trial", "id": "jingang"}, state["book"].(map[string]any)["active"])
	assert.NotContains(t, state["book"].(map[string]any)["active"], "seed")
	player.fails(http.MethodPost, duelBase+"/start", map[string]any{"kind": wendao.DuelTrial, "id": "taixu"})

	// 刚发出就交几百帧记录，比真打快太多；没打完的记录也不行。
	save, err := loadWendaoSave(player.user.Id)
	require.NoError(t, err)
	editWendao(t, player, func(save *wendao.Save) {
		save.Duels.Active.Setup.Room, save.Duels.Active.Setup.Waves = "glade", [][]doufa.Group{{{Foe: "slime", N: 1}}}
		save.Duels.Active.Issued = common.GetTimestamp()
	})
	save, err = loadWendaoSave(player.user.Id)
	require.NoError(t, err)
	fight, err := doufa.New(save.Duels.Active.Seed, save.Duels.Active.Setup)
	require.NoError(t, err)
	var inputs []int
	for !fight.Over {
		inputs = append(inputs, doufa.Pack(-1, -1, doufa.BtnAttack))
		fight.Step(inputs[len(inputs)-1])
	}
	assert.Equal(t, message(i18n.MsgGameReplayInvalid), player.fails(http.MethodPost, duelBase+"/clear", map[string]any{"inputs": doufa.EncodeInputs(inputs)}))
	assert.Equal(t, message(i18n.MsgGameReplayInvalid), player.fails(http.MethodPost, duelBase+"/clear", map[string]any{"inputs": doufa.EncodeInputs(inputs[:10])}))
	assert.Equal(t, message(i18n.MsgGameReplayInvalid), player.fails(http.MethodPost, duelBase+"/clear", map[string]any{"inputs": "%%%"}))

	// 打输了不拜入，这一场结束、次数用掉。
	lost := player.ok(http.MethodPost, duelBase+"/clear", map[string]any{"inputs": rigDuel(t, player, false)})
	assert.Equal(t, false, lost["won"])
	assert.Equal(t, "loose", lost["standing"].(map[string]any)["identity"])
	assert.Nil(t, lost["book"].(map[string]any)["active"])
	assert.Equal(t, float64(1), lost["book"].(map[string]any)["trials"].(map[string]any)["jingang"])
	assert.Equal(t, message(i18n.MsgGameSectUnavailable), player.fails(http.MethodPost, duelBase+"/clear", map[string]any{"inputs": "AA"}), "nothing in progress")

	// 放弃：一次也不退。
	player.ok(http.MethodPost, duelBase+"/start", map[string]any{"kind": wendao.DuelTrial, "id": "jingang"})
	gave := player.ok(http.MethodPost, duelBase+"/giveup", map[string]any{})
	assert.Nil(t, gave["book"].(map[string]any)["active"])
	assert.Equal(t, float64(2), gave["book"].(map[string]any)["trials"].(map[string]any)["jingang"])

	won := joinThroughTrial(t, player, "jingang")
	assert.Equal(t, true, won["won"])
	assert.Equal(t, "disciple", won["standing"].(map[string]any)["identity"])
	assert.Equal(t, "jingang", won["standing"].(map[string]any)["path"].(map[string]any)["sect"])
	player.fails(http.MethodPost, duelBase+"/start", map[string]any{"kind": wendao.DuelTrial, "id": "taixu"})
}

// 悬赏：打赢给资源、这张算完成；输了或放弃都用掉一次挑战，每张两次；清算的请求带着编号重发不会发第二遍奖励。
func TestWendaoDuelBountyPaysOnce(t *testing.T) {
	env := newGameTestEnv(t, sectSettings())
	player := env.player(0, gameTestIpA)
	duelHero(t, player)
	id := bountyAt(t, player, 0)
	book := player.ok(http.MethodGet, duelBase+"/state", nil)["book"].(map[string]any)
	reward := book["bounties"].([]any)[0].(map[string]any)["reward"].(map[string]any)["stone"].(float64)
	assert.Equal(t, []any{true, false, false, false, false}, book["open"])
	stone := player.ok(http.MethodPost, "/api/game/wendao/sect/action", map[string]any{"action": "sync"})["sect"].(map[string]any)["res"].(map[string]any)["stone"].(float64)

	start := map[string]any{"kind": wendao.DuelBounty, "id": id, "request_id": "start-1"}
	player.ok(http.MethodPost, duelBase+"/start", start)
	again := player.ok(http.MethodPost, duelBase+"/start", start)
	assert.Equal(t, true, again["replayed"])
	assert.NotNil(t, again["duel"], "a start whose answer was lost gets the fight again, without spending another try")
	assert.Equal(t, float64(wendao.ContractTries-1), again["book"].(map[string]any)["bounties"].([]any)[0].(map[string]any)["tries"])

	lost := player.ok(http.MethodPost, duelBase+"/clear", map[string]any{"inputs": rigDuel(t, player, false)})
	assert.Equal(t, false, lost["won"])
	player.ok(http.MethodPost, duelBase+"/start", map[string]any{"kind": wendao.DuelBounty, "id": id})
	clear := map[string]any{"inputs": rigDuel(t, player, true), "request_id": "clear-1"}
	won := player.ok(http.MethodPost, duelBase+"/clear", clear)
	assert.Equal(t, true, won["won"])
	assert.Equal(t, stone+reward, won["sect"].(map[string]any)["res"].(map[string]any)["stone"])
	assert.Equal(t, true, won["book"].(map[string]any)["bounties"].([]any)[0].(map[string]any)["done"])

	retried := player.ok(http.MethodPost, duelBase+"/clear", clear)
	assert.Equal(t, true, retried["replayed"])
	assert.Equal(t, stone+reward, retried["sect"].(map[string]any)["res"].(map[string]any)["stone"], "a retried submission pays once")
	player.fails(http.MethodPost, duelBase+"/start", map[string]any{"kind": wendao.DuelBounty, "id": id})
	player.fails(http.MethodPost, duelBase+"/start", map[string]any{"kind": wendao.DuelBounty, "id": "b9"})
	player.fails(http.MethodPost, duelBase+"/start", map[string]any{"kind": wendao.DuelTask, "id": "t1"})
}

// 宗门任务：宗门弟子才有，打赢记贡献并给一点灵石；贡献够了自动晋升。
func TestWendaoDuelTaskGivesMerit(t *testing.T) {
	env := newGameTestEnv(t, sectSettings())
	player := env.player(0, gameTestIpA)
	duelHero(t, player)
	assert.Empty(t, player.ok(http.MethodGet, duelBase+"/state", nil)["book"].(map[string]any)["tasks"], "a rogue cultivator has no sect tasks")
	joinThroughTrial(t, player, "tianjian")

	var merit int
	editWendao(t, player, func(save *wendao.Save) {
		save.Duels.Tasks[0].Region = 0
		merit = save.Duels.Tasks[0].Merit
	})
	book := player.ok(http.MethodGet, duelBase+"/state", nil)["book"].(map[string]any)
	require.Len(t, book["tasks"], wendao.TaskSlots)
	player.ok(http.MethodPost, duelBase+"/start", map[string]any{"kind": wendao.DuelTask, "id": "t1"})
	won := player.ok(http.MethodPost, duelBase+"/clear", map[string]any{"inputs": rigDuel(t, player, true)})
	assert.Equal(t, float64(merit), won["standing"].(map[string]any)["path"].(map[string]any)["merit"])
	assert.Contains(t, won["events"], map[string]any{"k": "path_merit", "n": float64(merit)})
}
