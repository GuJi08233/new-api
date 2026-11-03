package controller

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/dungeon"
	"github.com/QuantumNous/new-api/pkg/wendao"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// editWendao 直接改玩家的问道存档(测试夹具)。
func editWendao(t *testing.T, player *gamePlayer, edit func(save *wendao.Save)) {
	t.Helper()
	_, err := model.UpdateGameSave(player.user.Id, wendao.SaveKey, func(data string) (string, error) {
		return wendao.UpdateSave(data, func(save *wendao.Save) error {
			edit(save)
			return nil
		})
	})
	require.NoError(t, err)
}

// 新主角是散修：储物处不能经营宗门、不能结仙盟，商店不卖符箓也不扣币；拜入正道宗门以后领俸禄、上交资源换贡献、用贡献换
// 宝库里的东西，重发同一次上交只记一次；退出宗门以后到了金丹、灵石够就能开宗立派，开宗以后宗门经营与符箓都能用。
func TestWendaoPathThroughTheApi(t *testing.T) {
	require.NoError(t, i18n.Init())
	env := newGameTestEnv(t, sectSettings())
	player := env.player(3, gameTestIpA)
	path := "/api/game/wendao/sect/path"
	action := "/api/game/wendao/sect/action"
	shop := "/api/game/wendao/sect/shop/buy"
	player.fails(http.MethodPost, duelBase+"/start", map[string]any{"kind": wendao.DuelTrial, "id": "jingang"})

	player.ok(http.MethodPost, "/api/game/wendao/hero/action", map[string]any{"action": "create", "class": dungeon.ClassWarrior})
	synced := player.ok(http.MethodPost, action, map[string]any{"action": "sync"})
	assert.Equal(t, "loose", synced["standing"].(map[string]any)["identity"])
	storehouse := synced["sect"].(map[string]any)
	assert.Equal(t, false, storehouse["founded"])
	assert.Empty(t, storehouse["disciples"])
	player.fails(http.MethodPost, action, map[string]any{"action": "upgrade", "building": wendao.BuildingHall})
	player.fails(http.MethodPost, "/api/game/wendao/sect/alliance/create", map[string]any{"name": "青云盟"})
	message := player.fails(http.MethodPost, shop, map[string]any{"item": wendao.ItemSwift, "count": 1})
	assert.Equal(t, i18n.Translate(i18n.LangEn, i18n.MsgGameShopItemLimit), message)
	assert.Equal(t, 3.0, player.balance(), "a refused talisman takes no coins")

	player.fails(http.MethodPost, path, map[string]any{"action": "join", "sect": "jingang"})
	joined := joinThroughTrial(t, player, "jingang")
	assert.Equal(t, "disciple", joined["standing"].(map[string]any)["identity"])
	assert.Equal(t, "jingang", joined["standing"].(map[string]any)["path"].(map[string]any)["sect"])
	assert.Contains(t, joined["events"], map[string]any{"k": "path_join", "s": "jingang"})
	assert.Equal(t, true, joined["standing"].(map[string]any)["stipend_ready"])
	paid := player.ok(http.MethodPost, path, map[string]any{"action": "stipend"})
	assert.Equal(t, false, paid["standing"].(map[string]any)["stipend_ready"])
	assert.Equal(t, "path_stipend", paid["events"].([]any)[0].(map[string]any)["k"])
	stone := paid["sect"].(map[string]any)["res"].(map[string]any)["stone"].(float64)
	assert.Equal(t, float64(200+wendao.RankStipend[wendao.RankOuter].Stone), stone)
	player.fails(http.MethodPost, path, map[string]any{"action": "stipend"})

	donate := map[string]any{"action": "donate", "stone": 100, "request_id": "donate-1"}
	donated := player.ok(http.MethodPost, path, donate)
	assert.Equal(t, 5.0, donated["standing"].(map[string]any)["path"].(map[string]any)["merit"])
	again := player.ok(http.MethodPost, path, donate)
	assert.Equal(t, true, again["replayed"])
	assert.Equal(t, 5.0, again["standing"].(map[string]any)["path"].(map[string]any)["merit"], "a retried donation counts once")
	assert.Equal(t, stone-100, again["sect"].(map[string]any)["res"].(map[string]any)["stone"])

	editWendao(t, player, func(save *wendao.Save) { save.Hero.Path.Merit = 25 })
	exchanged := player.ok(http.MethodPost, path, map[string]any{"action": "exchange", "goods": "pill_" + wendao.PillHeal})
	assert.Equal(t, 5.0, exchanged["standing"].(map[string]any)["path"].(map[string]any)["merit"])
	assert.Equal(t, 3.0, exchanged["sect"].(map[string]any)["pills"].(map[string]any)[wendao.PillHeal])
	player.fails(http.MethodPost, path, map[string]any{"action": "exchange", "goods": "pill_" + wendao.PillMarrow})

	player.fails(http.MethodPost, path, map[string]any{"action": "found"})
	left := player.ok(http.MethodPost, path, map[string]any{"action": "leave"})
	assert.Equal(t, "loose", left["standing"].(map[string]any)["identity"])
	player.fails(http.MethodPost, duelBase+"/start", map[string]any{"kind": wendao.DuelTrial, "id": "taixu"})
	player.fails(http.MethodPost, path, map[string]any{"action": "found"})
	editWendao(t, player, func(save *wendao.Save) {
		save.Hero.Realm = wendao.FoundRealm
		save.Sect.Res.Stone = wendao.FoundCost + 30
	})
	founded := player.ok(http.MethodPost, path, map[string]any{"action": "found"})
	assert.Equal(t, "master", founded["standing"].(map[string]any)["identity"])
	sect := founded["sect"].(map[string]any)
	assert.Equal(t, true, sect["founded"])
	assert.Equal(t, 30.0, sect["res"].(map[string]any)["stone"])
	require.Len(t, sect["disciples"], 2)
	player.fails(http.MethodPost, path, map[string]any{"action": "found"})

	first := sect["disciples"].([]any)[0].(map[string]any)["id"]
	player.ok(http.MethodPost, action, map[string]any{"action": "dispatch", "mission": wendao.Missions[0].Key, "team": []any{first}})
	player.ok(http.MethodPost, shop, map[string]any{"item": wendao.ItemSwift, "count": 1})
}
