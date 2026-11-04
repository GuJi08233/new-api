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
	player.ok(http.MethodGet, duelBase+"/state", nil)
}

func duelClearBody(t *testing.T, player *gamePlayer, inputs string) map[string]any {
	t.Helper()
	save, err := loadWendaoSave(player.user.Id)
	require.NoError(t, err)
	require.NotNil(t, save.Duels.Active)
	return map[string]any{"inputs": inputs, "battle_token": save.Duels.Active.Token}
}

func giveUpCurrentDuel(t *testing.T, player *gamePlayer) map[string]any {
	t.Helper()
	active := player.ok(http.MethodGet, duelBase+"/state", nil)["book"].(map[string]any)["active"].(map[string]any)
	return player.ok(http.MethodPost, duelBase+"/giveup", map[string]any{"battle_token": active["token"]})
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
	return player.ok(http.MethodPost, duelBase+"/clear", duelClearBody(t, player, rigDuel(t, player, true)))
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
	require.NotEmpty(t, duel["token"])
	expectedActive := map[string]any{"kind": "trial", "id": "jingang", "token": duel["token"]}
	assert.Equal(t, expectedActive, started["book"].(map[string]any)["active"])
	assert.Equal(t, float64(1), started["book"].(map[string]any)["trials"].(map[string]any)["jingang"])

	// 读取状态只知道有这么一场，没有种子；另一个页面不能再开一场。
	state := player.ok(http.MethodGet, duelBase+"/state", nil)
	assert.Equal(t, expectedActive, state["book"].(map[string]any)["active"])
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
	assert.Equal(t, message(i18n.MsgGameReplayInvalid), player.fails(http.MethodPost, duelBase+"/clear", duelClearBody(t, player, doufa.EncodeInputs(inputs))))
	assert.Equal(t, message(i18n.MsgGameReplayInvalid), player.fails(http.MethodPost, duelBase+"/clear", duelClearBody(t, player, doufa.EncodeInputs(inputs[:10]))))
	assert.Equal(t, message(i18n.MsgGameReplayInvalid), player.fails(http.MethodPost, duelBase+"/clear", duelClearBody(t, player, "%%%")))

	// 打输了不拜入，这一场结束、次数用掉。
	lost := player.ok(http.MethodPost, duelBase+"/clear", duelClearBody(t, player, rigDuel(t, player, false)))
	assert.Equal(t, false, lost["won"])
	assert.Equal(t, "loose", lost["standing"].(map[string]any)["identity"])
	assert.Nil(t, lost["book"].(map[string]any)["active"])
	assert.Equal(t, float64(1), lost["book"].(map[string]any)["trials"].(map[string]any)["jingang"])
	assert.Equal(t, message(i18n.MsgGameSectUnavailable), player.fails(http.MethodPost, duelBase+"/clear", map[string]any{"inputs": "AA", "battle_token": duel["token"]}), "nothing in progress")

	// 放弃：一次也不退。
	player.ok(http.MethodPost, duelBase+"/start", map[string]any{"kind": wendao.DuelTrial, "id": "jingang"})
	gave := giveUpCurrentDuel(t, player)
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

	lost := player.ok(http.MethodPost, duelBase+"/clear", duelClearBody(t, player, rigDuel(t, player, false)))
	assert.Equal(t, false, lost["won"])
	player.ok(http.MethodPost, duelBase+"/start", map[string]any{"kind": wendao.DuelBounty, "id": id})
	clear := duelClearBody(t, player, rigDuel(t, player, true))
	clear["request_id"] = "clear-1"
	won := player.ok(http.MethodPost, duelBase+"/clear", clear)
	assert.Equal(t, true, won["won"])
	assert.Equal(t, stone+reward, won["sect"].(map[string]any)["res"].(map[string]any)["stone"])
	assert.Equal(t, true, won["book"].(map[string]any)["bounties"].([]any)[0].(map[string]any)["done"])

	retried := player.ok(http.MethodPost, duelBase+"/clear", clear)
	assert.Equal(t, true, retried["replayed"])
	for _, key := range []string{"won", "kills", "ticks", "events"} {
		assert.Equal(t, won[key], retried[key], "retry preserves %s", key)
	}
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
	won := player.ok(http.MethodPost, duelBase+"/clear", duelClearBody(t, player, rigDuel(t, player, true)))
	assert.Equal(t, float64(merit), won["standing"].(map[string]any)["path"].(map[string]any)["merit"])
	assert.Contains(t, won["events"], map[string]any{"k": "path_merit", "n": float64(merit)})
}

// 藏经阁换来的法宝与法术：用贡献在宝库里换(身份不够、已经有的不行)，配置出战要手上有的；配置以后契约开局用它，法术写进开局。
func TestWendaoDuelUsesTheLoadoutBoughtInTheLibrary(t *testing.T) {
	require.NoError(t, i18n.Init())
	env := newGameTestEnv(t, sectSettings())
	player := env.player(0, gameTestIpA)
	duelHero(t, player)
	player.fails(http.MethodPost, duelBase+"/arms", map[string]any{"weapons": []string{"sword"}, "skill": "nova"})
	joinThroughTrial(t, player, "taixu")
	editWendao(t, player, func(save *wendao.Save) {
		save.Hero.Path.Merit, save.Hero.Path.Earned = 1000, 1000
		save.Hero.Path.Rank = wendao.RankHeir
	})

	path := "/api/game/wendao/sect/path"
	player.fails(http.MethodPost, path, map[string]any{"action": "exchange", "goods": "arm_spear"}) // 太虚观没有
	bought := player.ok(http.MethodPost, path, map[string]any{"action": "exchange", "goods": "skill_nova"})
	assert.Equal(t, 520.0, bought["standing"].(map[string]any)["path"].(map[string]any)["merit"])
	player.ok(http.MethodPost, path, map[string]any{"action": "exchange", "goods": "arm_thunder"})
	player.fails(http.MethodPost, path, map[string]any{"action": "exchange", "goods": "arm_thunder"})

	arms := player.ok(http.MethodGet, duelBase+"/state", nil)["book"].(map[string]any)["arms"].(map[string]any)
	assert.Equal(t, []any{"sword", "talisman", "thunder"}, arms["weapons"])
	assert.Equal(t, []any{"quake", "nova"}, arms["skills"])
	player.fails(http.MethodPost, duelBase+"/arms", map[string]any{"weapons": []string{"spear"}, "skill": "nova"})
	equipped := player.ok(http.MethodPost, duelBase+"/arms", map[string]any{"weapons": []string{"thunder", "sword"}, "skill": "nova"})
	assert.Equal(t, map[string]any{"weapons": []any{"thunder", "sword"}, "skill": "nova"}, equipped["book"].(map[string]any)["arms"].(map[string]any)["loadout"])

	var id string
	editWendao(t, player, func(save *wendao.Save) {
		save.Duels.Bounties[0].Region = 0
		id = save.Duels.Bounties[0].Id
	})
	started := player.ok(http.MethodPost, duelBase+"/start", map[string]any{"kind": wendao.DuelBounty, "id": id})
	setup := started["duel"].(map[string]any)["setup"].(map[string]any)
	assert.Equal(t, []any{"thunder", "sword"}, setup["weapons"])
	assert.Equal(t, "nova", setup["skill"])
	won := player.ok(http.MethodPost, duelBase+"/clear", duelClearBody(t, player, rigDuel(t, player, true)))
	assert.Equal(t, true, won["won"])
}

func TestWendaoDuelStaleRequestsKeepLaterAttempt(t *testing.T) {
	require.NoError(t, i18n.Init())
	env := newGameTestEnv(t, sectSettings())
	player := env.player(0, gameTestIpA)
	duelHero(t, player)
	start := map[string]any{"kind": wendao.DuelTrial, "id": "tianjian", "request_id": "first-start"}
	first := player.ok(http.MethodPost, duelBase+"/start", start)["duel"].(map[string]any)
	giveUp := map[string]any{"battle_token": first["token"], "request_id": "first-giveup"}
	player.ok(http.MethodPost, duelBase+"/giveup", giveUp)
	second := player.ok(http.MethodPost, duelBase+"/start", map[string]any{"kind": wendao.DuelTrial, "id": "tianjian", "request_id": "second-start"})
	active := second["book"].(map[string]any)["active"]
	assert.NotEqual(t, first["token"], second["duel"].(map[string]any)["token"])

	// 重发第一次开局不能拿到同一契约的第二次种子；第一次放弃的收据也不能再放弃第二次。
	oldStart := player.ok(http.MethodPost, duelBase+"/start", start)
	assert.NotContains(t, oldStart, "duel")
	oldGiveUp := player.ok(http.MethodPost, duelBase+"/giveup", giveUp)
	assert.Equal(t, true, oldGiveUp["replayed"])
	assert.Equal(t, active, oldGiveUp["book"].(map[string]any)["active"])
	player.fails(http.MethodPost, duelBase+"/giveup", map[string]any{"battle_token": first["token"], "request_id": "stale-page-giveup"})
	player.fails(http.MethodPost, duelBase+"/giveup", map[string]any{"request_id": "unbound-giveup"})
	player.fails(http.MethodPost, duelBase+"/clear", map[string]any{"battle_token": first["token"], "inputs": "AA", "request_id": "stale-page-clear"})
	state := player.ok(http.MethodGet, duelBase+"/state", nil)["book"].(map[string]any)
	assert.Equal(t, active, state["active"])
	assert.Equal(t, float64(2), state["trials"].(map[string]any)["tianjian"])
}

func TestWendaoDuelClearRetryKeepsResultAfterNextFight(t *testing.T) {
	for _, win := range []bool{false, true} {
		t.Run(map[bool]string{false: "defeat", true: "victory"}[win], func(t *testing.T) {
			env := newGameTestEnv(t, sectSettings())
			player := env.player(0, gameTestIpA)
			duelHero(t, player)
			firstId, nextId := bountyAt(t, player, 0), bountyAt(t, player, 1)
			player.ok(http.MethodPost, duelBase+"/start", map[string]any{"kind": wendao.DuelBounty, "id": firstId})
			request := duelClearBody(t, player, rigDuel(t, player, win))
			request["request_id"] = "lost-clear-answer"
			settled := player.ok(http.MethodPost, duelBase+"/clear", request)
			require.Equal(t, win, settled["won"])
			require.NotZero(t, settled["ticks"])

			next := player.ok(http.MethodPost, duelBase+"/start", map[string]any{"kind": wendao.DuelBounty, "id": nextId})
			retry := player.ok(http.MethodPost, duelBase+"/clear", request)
			assert.Equal(t, true, retry["replayed"])
			for _, key := range []string{"won", "ticks", "kills", "events"} {
				assert.Equal(t, settled[key], retry[key], "the original %s survives a new fight", key)
			}
			assert.Equal(t, settled["sect"].(map[string]any)["res"], retry["sect"].(map[string]any)["res"], "retry cannot issue resources twice")
			assert.Equal(t, next["book"].(map[string]any)["active"], retry["book"].(map[string]any)["active"])
			request["battle_token"] = next["duel"].(map[string]any)["token"]
			player.fails(http.MethodPost, duelBase+"/clear", request)
		})
	}
}

func TestWendaoLegacyDuelCanBeAbandonedAfterRefresh(t *testing.T) {
	env := newGameTestEnv(t, sectSettings())
	player := env.player(0, gameTestIpA)
	duelHero(t, player)
	player.ok(http.MethodPost, duelBase+"/start", map[string]any{"kind": wendao.DuelTrial, "id": "tianjian"})
	editWendao(t, player, func(save *wendao.Save) { save.Duels.Active.Token = "" })
	first := player.ok(http.MethodGet, duelBase+"/state", nil)["book"].(map[string]any)["active"]
	second := player.ok(http.MethodGet, duelBase+"/state", nil)["book"].(map[string]any)["active"]
	assert.Equal(t, first, second, "an old save has a stable battle identity before any write")
	require.NotEmpty(t, first.(map[string]any)["token"])
	gaveUp := giveUpCurrentDuel(t, player)
	assert.Nil(t, gaveUp["book"].(map[string]any)["active"])
	assert.Equal(t, float64(1), gaveUp["book"].(map[string]any)["trials"].(map[string]any)["tianjian"])
}
