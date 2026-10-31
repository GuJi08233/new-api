package controller

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/dungeon"
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
func TestDungeonShopChargesCoinsIntoTheBag(t *testing.T) {
	require.NoError(t, i18n.Init())
	env := newGameTestEnv(t, gameSetting(func(s *operation_setting.GameSetting) {
		s.Wendao = puzzleRule()
		s.ShopPricePercent = 50
	}))
	player := env.player(2, gameTestIpA)
	path := "/api/game/wendao/abyss/shop/buy"

	info := player.ok(http.MethodGet, "/api/game/wendao", nil)
	shop := info["abyss"].(map[string]any)["shop"].(map[string]any)
	assert.Equal(t, true, shop["enabled"])
	assert.Equal(t, map[string]any{"id": dungeon.ItemWard, "price": 0.5}, shop["items"].([]any)[4], "half of the 1-coin list price")

	bought := player.ok(http.MethodPost, path, map[string]any{"item": dungeon.ItemElixir, "count": 3})
	assert.Equal(t, 1.7, bought["balance"])
	assert.Equal(t, map[string]any{dungeon.ItemElixir: float64(3)}, bought["meta"].(map[string]any)["items"])
	logs := shopLogs(t, env, player.user.Id)
	require.Len(t, logs, 1)
	assert.Equal(t, "wendao_abyss:elixir×3", logs[0].Remark)
	assert.Equal(t, -0.3, coinsOf(t, logs[0].Amount))

	message := player.fails(http.MethodPost, path, map[string]any{"item": dungeon.ItemWard, "count": 4})
	assert.Equal(t, i18n.Translate(i18n.LangEn, i18n.MsgGameCoinInsufficient), message)
	message = player.fails(http.MethodPost, path, map[string]any{"item": dungeon.ItemPotion, "count": dungeon.MaxItemStack})
	assert.Equal(t, i18n.Translate(i18n.LangEn, i18n.MsgInvalidParams), message, "at most 20 per purchase")
	player.fails(http.MethodPost, path, map[string]any{"item": "crown", "count": 1})
	player.fails(http.MethodPost, path, map[string]any{"item": dungeon.ItemElixir, "count": 0})
	assert.Equal(t, 1.7, player.balance())
	assert.Len(t, shopLogs(t, env, player.user.Id), 1)

	setting := operation_setting.GetGameSetting().Clone()
	setting.ShopEnabled = false
	operation_setting.SetGameSettingForTest(setting)
	message = player.fails(http.MethodPost, path, map[string]any{"item": dungeon.ItemElixir, "count": 1})
	assert.Equal(t, i18n.Translate(i18n.LangEn, i18n.MsgGameShopDisabled), message)
	assert.Equal(t, 1.7, player.balance())
}

// 背包里的道具在一轮里用掉一件少一件，道具与新局面一起保存；背包里没有、这一轮已经用满或用了没有效果时不扣道具。
func TestDungeonItemsAreConsumedInsideARun(t *testing.T) {
	require.NoError(t, i18n.Init())
	env := newGameTestEnv(t, dungeonSettings())
	player := env.player(5, gameTestIpA)
	player.ok(http.MethodPost, "/api/game/wendao/abyss/shop/buy", map[string]any{"item": dungeon.ItemGold, "count": 6})
	player.ok(http.MethodPost, "/api/game/wendao/abyss/shop/buy", map[string]any{"item": dungeon.ItemElixir, "count": 1})

	path := "/api/game/wendao/abyss"
	view := player.ok(http.MethodPost, path+"/start", map[string]any{"class": dungeon.ClassWarrior, "abyss": 0})
	ageSession(t, env, view["session_id"], 60)
	message := player.fails(http.MethodPost, path+"/action", puzzleAction(view, map[string]any{"action": "item", "item": dungeon.ItemWard}))
	assert.Equal(t, i18n.Translate(i18n.LangEn, i18n.MsgGameShopNoItem), message)
	message = player.fails(http.MethodPost, path+"/action", puzzleAction(view, map[string]any{"action": "item", "item": dungeon.ItemElixir}))
	assert.Equal(t, i18n.Translate(i18n.LangEn, i18n.MsgGameShopItemLimit), message, "an elixir at full health does nothing")

	gold := view["run"].(map[string]any)["gold"].(float64)
	for i := 1; i <= dungeon.ItemsPerRun; i++ {
		view = player.ok(http.MethodPost, path+"/action", puzzleAction(view, map[string]any{"action": "item", "item": dungeon.ItemGold}))
		run := view["run"].(map[string]any)
		assert.Equal(t, gold+float64(100*i), run["gold"])
		assert.Equal(t, float64(i), run["items_used"])
	}
	assert.Equal(t, map[string]any{dungeon.ItemGold: float64(1), dungeon.ItemElixir: float64(1)}, view["meta"].(map[string]any)["items"])
	message = player.fails(http.MethodPost, path+"/action", puzzleAction(view, map[string]any{"action": "item", "item": dungeon.ItemGold}))
	assert.Equal(t, i18n.Translate(i18n.LangEn, i18n.MsgGameShopItemLimit), message)
	assert.Equal(t, map[string]any{dungeon.ItemGold: float64(1), dungeon.ItemElixir: float64(1)}, dungeonMeta(t, player)["items"])
}

// 宗门商店：丹药直接进丹房，符箓进背包；神行符让在外的历练马上回来，没有在外的队伍时不扣符。
func TestSectShopPillsAndTalismans(t *testing.T) {
	require.NoError(t, i18n.Init())
	env := newGameTestEnv(t, sectSettings())
	player := env.player(3, gameTestIpA)
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
