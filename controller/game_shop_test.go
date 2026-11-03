package controller

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/doufa"
	"github.com/QuantumNous/new-api/pkg/wendao"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func shopLogs(t *testing.T, env *gameTestEnv, userId int) []model.GameCoinLog {
	t.Helper()
	var logs []model.GameCoinLog
	require.NoError(t, env.db.Where("user_id = ? AND type = ?", userId, model.GameCoinLogTypeGameShop).Order("id").Find(&logs).Error)
	return logs
}

// 买道具按单价乘件数扣币(单价乘上管理员设的价格比例)，道具进背包，流水记下买了什么；余额不够、商店关闭或背包放不下时
// 什么都不改。
func TestRiftShopChargesCoinsIntoTheBag(t *testing.T) {
	require.NoError(t, i18n.Init())
	env := newGameTestEnv(t, gameSetting(func(s *operation_setting.GameSetting) {
		s.Wendao = puzzleRule()
		s.ShopPricePercent = 50
	}))
	player := env.player(2, gameTestIpA)
	path := "/api/game/wendao/rift/shop/buy"

	info := player.ok(http.MethodGet, "/api/game/wendao", nil)
	shop := info["abyss"].(map[string]any)["shop"].(map[string]any)
	assert.Equal(t, true, shop["enabled"])
	assert.Equal(t, map[string]any{"id": doufa.ItemWard, "price": 0.5}, shop["items"].([]any)[2], "half of the 1-coin list price")

	bought := player.ok(http.MethodPost, path, map[string]any{"item": doufa.ItemElixir, "count": 3})
	assert.Equal(t, 1.7, bought["balance"])
	assert.Equal(t, map[string]any{doufa.ItemElixir: float64(3)}, bought["meta"].(map[string]any)["items"])
	logs := shopLogs(t, env, player.user.Id)
	require.Len(t, logs, 1)
	assert.Equal(t, "wendao_abyss:elixir×3", logs[0].Remark)
	assert.Equal(t, -0.3, coinsOf(t, logs[0].Amount))

	message := player.fails(http.MethodPost, path, map[string]any{"item": doufa.ItemWard, "count": 4})
	assert.Equal(t, i18n.Translate(i18n.LangEn, i18n.MsgGameCoinInsufficient), message)
	message = player.fails(http.MethodPost, path, map[string]any{"item": doufa.ItemFortune, "count": doufa.MaxItemStack})
	assert.Equal(t, i18n.Translate(i18n.LangEn, i18n.MsgInvalidParams), message, "at most 20 per purchase")
	player.fails(http.MethodPost, path, map[string]any{"item": "crown", "count": 1})
	player.fails(http.MethodPost, path, map[string]any{"item": "gold_pouch", "count": 1})
	player.fails(http.MethodPost, path, map[string]any{"item": doufa.ItemElixir, "count": 0})
	assert.Equal(t, 1.7, player.balance())
	assert.Len(t, shopLogs(t, env, player.user.Id), 1)

	setting := operation_setting.GetGameSetting().Clone()
	setting.ShopEnabled = false
	operation_setting.SetGameSettingForTest(setting)
	message = player.fails(http.MethodPost, path, map[string]any{"item": doufa.ItemElixir, "count": 1})
	assert.Equal(t, i18n.Translate(i18n.LangEn, i18n.MsgGameShopDisabled), message)
	assert.Equal(t, 1.7, player.balance())
}

// 护命符挂上以后，一轮里第一次倒下不结束这一轮：以半条命复活，同一间房换个种子重新打；每轮最多用三件道具，用满了不扣道具。
func TestRiftWardRevivesAfterTheFirstFall(t *testing.T) {
	require.NoError(t, i18n.Init())
	env := newGameTestEnv(t, riftSettings())
	player := env.player(5, gameTestIpA)
	path := "/api/game/wendao/rift"
	player.ok(http.MethodPost, path+"/shop/buy", map[string]any{"item": doufa.ItemWard, "count": 2})
	player.ok(http.MethodPost, path+"/shop/buy", map[string]any{"item": doufa.ItemElixir, "count": 1})

	started := player.ok(http.MethodPost, path+"/start", map[string]any{"class": "warrior"})["session"].(map[string]any)
	armed := riftAct(player, started, map[string]any{"action": "item", "item": doufa.ItemWard})
	assert.Equal(t, true, armed["run"].(map[string]any)["ward"])
	message := riftReject(player, armed, map[string]any{"action": "item", "item": doufa.ItemWard})
	assert.Equal(t, i18n.Translate(i18n.LangEn, i18n.MsgGameShopItemLimit), message, "one ward at a time")
	assert.Equal(t, map[string]any{doufa.ItemWard: float64(1), doufa.ItemElixir: float64(1)}, riftMeta(t, player)["items"])

	entered := riftAct(player, armed, map[string]any{"action": "enter", "choice": 0})
	room := entered["run"].(map[string]any)["room"].(map[string]any)
	entered["version"] = editRift(t, player.user.Id, func(run *doufa.Run) {
		run.Room.Setup.Room = "glade"
		run.Room.Setup.Waves = [][]doufa.Group{{{Foe: "slime", N: 4}}}
		run.Room.Issued -= 3600
	})
	revived := riftAct(player, entered, map[string]any{"action": "clear", "inputs": playRoom(t, riftRun(t, player.user.Id).Room, false)})
	assert.Equal(t, model.GameSessionStatusPlaying, revived["status"], "the ward catches the fall")
	run := revived["run"].(map[string]any)
	assert.NotContains(t, run, "ward", "the ward is spent")
	assert.Equal(t, revived["max_hp"].(float64)*50/100, run["hp"])
	assert.NotEqual(t, room["seed"], run["room"].(map[string]any)["seed"], "the revived fight comes with a new seed in the answer")
	assert.Equal(t, run["hp"], run["room"].(map[string]any)["setup"].(map[string]any)["hp"])

	revived["version"] = editRift(t, player.user.Id, func(run *doufa.Run) { run.Room.Issued -= 3600 })
	over := riftAct(player, revived, map[string]any{"action": "clear", "inputs": playRoom(t, riftRun(t, player.user.Id).Room, false)})
	assert.Equal(t, model.GameSessionStatusFinished, over["status"], "the second fall ends the run")
}

// 宗门商店：丹药直接进丹房，符箓进背包；神行符让在外的历练马上回来，没有在外的队伍时不扣符。
func TestSectShopPillsAndTalismans(t *testing.T) {
	require.NoError(t, i18n.Init())
	env := newGameTestEnv(t, sectSettings())
	player := env.player(3, gameTestIpA)
	foundTestSect(t, player)
	path := "/api/game/wendao/sect/shop/buy"

	bought := player.ok(http.MethodPost, path, map[string]any{"item": "pill_" + wendao.PillBreak, "count": 2})
	assert.Equal(t, 2.0, bought["balance"])
	assert.Equal(t, float64(2), bought["sect"].(map[string]any)["pills"].(map[string]any)[wendao.PillBreak])
	bought = player.ok(http.MethodPost, path, map[string]any{"item": wendao.ItemSwift, "count": 1})
	assert.Equal(t, map[string]any{wendao.ItemSwift: float64(1)}, bought["sect"].(map[string]any)["items"])

	message := player.fails(http.MethodPost, "/api/game/wendao/sect/action", map[string]any{"action": "item", "item": wendao.ItemSwift})
	assert.Equal(t, i18n.Translate(i18n.LangEn, i18n.MsgGameShopItemLimit), message)
	message = player.fails(http.MethodPost, "/api/game/wendao/sect/action", map[string]any{"action": "item", "item": wendao.ItemSpirit})
	assert.Equal(t, i18n.Translate(i18n.LangEn, i18n.MsgGameShopNoItem), message)

	view := sectView(t, player, map[string]any{"action": "sync"})
	first := view["disciples"].([]any)[0].(map[string]any)["id"]
	view = sectView(t, player, map[string]any{"action": "dispatch", "mission": wendao.Missions[0].Key, "team": []any{first}})
	mission := view["missions"].([]any)[0].(map[string]any)
	player.fails(http.MethodPost, "/api/game/wendao/sect/action", map[string]any{"action": "claim", "id": mission["id"]})
	view = sectView(t, player, map[string]any{"action": "item", "item": wendao.ItemSwift})
	assert.Equal(t, map[string]any{}, view["items"])
	sectView(t, player, map[string]any{"action": "claim", "id": mission["id"]})
}
