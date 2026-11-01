package controller

import (
	"net/http"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/wendao"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 串门：道友出现在列表里并标出能浇灌的地；浇灌改道友的灵田(早熟、记来访)，谢礼记进自己的宗门，同一块地不能浇两次；熟了很久
// 没收的地能摘走一成；不能串自己的门，对方没有宗门时不能串。
func TestGardenVisitThroughTheApi(t *testing.T) {
	require.NoError(t, i18n.Init())
	env := newGameTestEnv(t, sectSettings())
	owner := env.player(0, gameTestIpA)
	visitor := env.player(0, gameTestIpB)
	lonely := env.player(0, gameTestIpB)
	sectView(t, owner, map[string]any{"action": "sync"})
	sectView(t, visitor, map[string]any{"action": "sync"})
	planted := sectView(t, owner, map[string]any{"action": "plant", "index": 0, "crop": "qingcao"})
	plot := planted["garden"].(map[string]any)["plots"].([]any)[0].(map[string]any)
	ripe := plot["ripe"].(float64)

	neighbors := visitor.ok(http.MethodGet, "/api/game/wendao/sect/garden/neighbors", nil)
	list := neighbors["neighbors"].([]any)
	require.Len(t, list, 1, "players without a sect are not listed")
	assert.Equal(t, float64(owner.user.Id), list[0].(map[string]any)["user_id"])
	assert.Equal(t, float64(1), list[0].(map[string]any)["water"])

	stone := sectView(t, visitor, map[string]any{"action": "sync"})["res"].(map[string]any)["stone"].(float64)
	body := map[string]any{"user_id": owner.user.Id, "index": 0, "kind": "water"}
	watered := visitor.ok(http.MethodPost, "/api/game/wendao/sect/garden/visit", body)
	assert.Equal(t, float64(wendao.GardenWaterDaily-1), watered["water_left"])
	after := watered["plots"].([]any)[0].(map[string]any)
	assert.Equal(t, ripe-360, after["ripe"])
	assert.Nil(t, after["can_water"], "watered once already")
	visitor.fails(http.MethodPost, "/api/game/wendao/sect/garden/visit", body)
	view := sectView(t, visitor, map[string]any{"action": "sync"})
	assert.GreaterOrEqual(t, view["res"].(map[string]any)["stone"].(float64), stone+float64(wendao.WaterReward.Stone))
	log := sectView(t, owner, map[string]any{"action": "sync"})["garden"].(map[string]any)["log"].([]any)
	require.Len(t, log, 1)
	assert.Equal(t, "water", log[0].(map[string]any)["kind"])

	editSect(t, owner, func(s *wendao.Sect) {
		s.Garden.Plots[0].Ripe = common.GetTimestamp() - wendao.PickGrace - 1
	})
	herb := sectView(t, visitor, map[string]any{"action": "sync"})["res"].(map[string]any)["herb"].(float64)
	picked := visitor.ok(http.MethodPost, "/api/game/wendao/sect/garden/visit", map[string]any{"user_id": owner.user.Id, "index": 0, "kind": "pick"})
	assert.Equal(t, float64(1), picked["plots"].([]any)[0].(map[string]any)["picks"])
	data, err := model.GetGameSave(visitor.user.Id, wendao.SaveKey)
	require.NoError(t, err)
	s, err := wendao.ParseSect(data)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, float64(s.Res.Herb), herb+6)

	owner.fails(http.MethodPost, "/api/game/wendao/sect/garden/visit", map[string]any{"user_id": owner.user.Id, "index": 0, "kind": "water"})
	owner.fails(http.MethodGet, "/api/game/wendao/sect/garden/visit?user_id="+strconv.Itoa(owner.user.Id), nil)
	lonely.fails(http.MethodPost, "/api/game/wendao/sect/garden/visit", body)
	visitor.fails(http.MethodGet, "/api/game/wendao/sect/garden/visit?user_id="+strconv.Itoa(lonely.user.Id), nil)
}
