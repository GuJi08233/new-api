package controller

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/pkg/dungeon"
	"github.com/QuantumNous/new-api/pkg/wendao"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 带请求编号的操作重发时不做第二遍：页面没收到回应、拿同一个编号再发一次，历练不会再给一次资源，宗门不会再派一次，
// 商店不会再扣一次币，回应里写明 replayed 并带上现在的样子；同一个编号换了内容要拒绝；不带编号照旧每次都做。
func TestWendaoRetriedRequestsAreDoneOnce(t *testing.T) {
	require.NoError(t, i18n.Init())
	env := newGameTestEnv(t, sectSettings())
	player := env.player(3, gameTestIpA)
	hero := "/api/game/wendao/hero/action"

	create := map[string]any{"action": "create", "class": dungeon.ClassRanger, "request_id": "create-1"}
	created := player.ok(http.MethodPost, hero, create)
	again := player.ok(http.MethodPost, hero, create)
	assert.Equal(t, true, again["replayed"])
	assert.Equal(t, []any{}, again["events"])
	assert.Equal(t, created["hero"].(map[string]any)["class"], again["hero"].(map[string]any)["class"])
	message := player.fails(http.MethodPost, hero, map[string]any{"action": "create", "class": dungeon.ClassMage, "request_id": "create-1"})
	assert.Equal(t, i18n.Translate(i18n.LangEn, i18n.MsgGameWendaoRequestReused), message)

	stone := func(view map[string]any) float64 { return view["res"].(map[string]any)["stone"].(float64) }
	visit := map[string]any{"action": "visit", "node": "c1_oath", "request_id": "visit-1"}
	visited := player.ok(http.MethodPost, hero, visit)
	assert.Equal(t, stone(created)+50, stone(visited))
	retried := player.ok(http.MethodPost, hero, visit)
	assert.Equal(t, true, retried["replayed"])
	assert.Equal(t, stone(visited), stone(retried), "the first visit reward is not paid twice")
	assert.Nil(t, visited["replayed"])

	sect := "/api/game/wendao/sect/action"
	first := sectView(t, player, map[string]any{"action": "sync"})["disciples"].([]any)[0].(map[string]any)["id"]
	dispatch := map[string]any{"action": "dispatch", "mission": wendao.Missions[0].Key, "team": []any{first}, "request_id": "dispatch-1"}
	sent := player.ok(http.MethodPost, sect, dispatch)
	resent := player.ok(http.MethodPost, sect, dispatch)
	assert.Equal(t, true, resent["replayed"])
	assert.Len(t, resent["sect"].(map[string]any)["missions"], len(sent["sect"].(map[string]any)["missions"].([]any)))

	shop := "/api/game/wendao/sect/shop/buy"
	buy := map[string]any{"item": "pill_" + wendao.PillBreak, "count": 1, "request_id": "buy-1"}
	bought := player.ok(http.MethodPost, shop, buy)
	rebought := player.ok(http.MethodPost, shop, buy)
	assert.Equal(t, true, rebought["replayed"])
	assert.Equal(t, bought["balance"], rebought["balance"], "the coins are taken once")
	pills := func(view map[string]any) any {
		return view["sect"].(map[string]any)["pills"].(map[string]any)[wendao.PillBreak]
	}
	assert.Equal(t, pills(bought), pills(rebought))
	message = player.fails(http.MethodPost, shop, map[string]any{"item": "pill_" + wendao.PillBreak, "count": 2, "request_id": "buy-1"})
	assert.Equal(t, i18n.Translate(i18n.LangEn, i18n.MsgGameWendaoRequestReused), message)

	plain := map[string]any{"item": "pill_" + wendao.PillBreak, "count": 1}
	once := player.ok(http.MethodPost, shop, plain)
	twice := player.ok(http.MethodPost, shop, plain)
	assert.Equal(t, pills(once).(float64)+1, pills(twice), "without a request id every purchase goes through")
}

// 万宝阁挂单、仙盟捐献与串门浇灌重发同一次请求时也只做一次：不会多押一份货多挂一单、多捐一份，也不会因为「这块地浇过了」
// 报错，而是交回现在的样子。
func TestWendaoRetriedTradesAndVisitsAreDoneOnce(t *testing.T) {
	require.NoError(t, i18n.Init())
	env := newGameTestEnv(t, sectSettings())
	seller := env.player(0, gameTestIpA)
	member := env.player(0, gameTestIpB)
	for _, p := range []*gamePlayer{seller, member} {
		sectView(t, p, map[string]any{"action": "sync"})
		editSect(t, p, func(s *wendao.Sect) {
			s.Buildings[wendao.BuildingHall] = 2
			s.Res = wendao.Cost{Stone: 5000, Herb: 500, Ore: 100, Rep: 100}
			s.Pills[wendao.PillHeal] = 5
		})
	}

	market := "/api/game/wendao/sect/market/list"
	list := map[string]any{"goods": "pill_heal", "count": 2, "price": 100, "request_id": "list-1"}
	listed := seller.ok(http.MethodPost, market, list)
	relisted := seller.ok(http.MethodPost, market, list)
	assert.Equal(t, true, relisted["replayed"])
	assert.Len(t, relisted["mine"], 1, "one listing, not two")
	assert.Equal(t, listed["sect"].(map[string]any)["pills"], relisted["sect"].(map[string]any)["pills"])

	alliance := "/api/game/wendao/sect/alliance"
	created := seller.ok(http.MethodPost, alliance+"/create", map[string]any{"name": "青云盟"})
	member.ok(http.MethodPost, alliance+"/join", map[string]any{"id": created["alliance"].(map[string]any)["id"]})
	donate := map[string]any{"stone": 100, "ore": 10, "request_id": "donate-1"}
	donated := member.ok(http.MethodPost, alliance+"/donate", donate)
	redonated := member.ok(http.MethodPost, alliance+"/donate", donate)
	assert.Equal(t, true, redonated["replayed"])
	funds := func(view map[string]any) any { return view["alliance"].(map[string]any)["funds"] }
	assert.Equal(t, funds(donated), funds(redonated), "the donation is counted once")
	message := member.fails(http.MethodPost, alliance+"/donate", map[string]any{"stone": 200, "request_id": "donate-1"})
	assert.Equal(t, i18n.Translate(i18n.LangEn, i18n.MsgGameWendaoRequestReused), message)

	sectView(t, seller, map[string]any{"action": "plant", "index": 0, "crop": "qingcao"})
	visit := map[string]any{"user_id": seller.user.Id, "index": 0, "kind": "water", "request_id": "water-1"}
	watered := member.ok(http.MethodPost, "/api/game/wendao/sect/garden/visit", visit)
	rewatered := member.ok(http.MethodPost, "/api/game/wendao/sect/garden/visit", visit)
	assert.Equal(t, true, rewatered["replayed"])
	assert.Equal(t, watered["water_left"], rewatered["water_left"])
	assert.Equal(t, watered["plots"], rewatered["plots"])
}
