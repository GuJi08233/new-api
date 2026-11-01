package controller

import (
	"net/http"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/wendao"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 每日功课跟着各处的操作走：宗门页同步时换到今天的三件；收获灵田、去道友那里浇灌、在万宝阁成交都记进功课；做满了才能领奖，
// 三件都领了才能开宝箱。
func TestWendaoDailyTasksThroughTheApi(t *testing.T) {
	require.NoError(t, i18n.Init())
	env := newGameTestEnv(t, sectSettings())
	player := env.player(0, gameTestIpA)
	friend := env.player(0, gameTestIpB)
	synced := sectView(t, player, map[string]any{"action": "sync"})
	daily := synced["daily"].(map[string]any)
	assert.Equal(t, model.GameCenterDay(time.Now()), daily["day"])
	assert.Len(t, daily["tasks"], wendao.DailyTaskCount)
	sectView(t, friend, map[string]any{"action": "sync"})

	for _, p := range []*gamePlayer{player, friend} {
		editSect(t, p, func(s *wendao.Sect) {
			s.Buildings[wendao.BuildingHall] = 2
			s.Res.Stone = 2000
			s.Pills[wendao.PillHeal] = 3
			s.Daily.Tasks = []wendao.DailyTask{{Key: wendao.TaskHarvest, Need: 1}, {Key: wendao.TaskWater, Need: 1},
				{Key: wendao.TaskTrade, Need: 1}}
		})
	}
	sectView(t, friend, map[string]any{"action": "plant", "index": 0, "crop": "qingcao"})
	player.ok(http.MethodPost, "/api/game/wendao/sect/garden/visit", map[string]any{"user_id": friend.user.Id, "index": 0, "kind": "water"})
	listed := friend.ok(http.MethodPost, "/api/game/wendao/sect/market/list", map[string]any{"goods": "pill_heal", "count": 1, "price": 10})
	player.ok(http.MethodPost, "/api/game/wendao/sect/market/buy", map[string]any{"id": listed["mine"].([]any)[0].(map[string]any)["id"]})

	path := "/api/game/wendao/sect/action"
	player.fails(http.MethodPost, path, map[string]any{"action": "daily_claim", "index": 0})
	editSect(t, player, func(s *wendao.Sect) {
		s.Garden.Plots[0] = &wendao.Plot{Crop: "qingcao", Planted: 1, Ripe: 2}
	})
	sectView(t, player, map[string]any{"action": "harvest", "index": 0})
	player.fails(http.MethodPost, path, map[string]any{"action": "daily_chest"})
	for i := range 3 {
		sectView(t, player, map[string]any{"action": "daily_claim", "index": i})
	}
	opened := sectView(t, player, map[string]any{"action": "daily_chest"})
	assert.Equal(t, true, opened["daily"].(map[string]any)["chest"])
	assert.Equal(t, float64(1), opened["daily"].(map[string]any)["streak"])

	tasks := sectOf(t, friend).Daily.Tasks
	assert.Equal(t, 1, tasks[2].Have, "the seller's trade counts too")
	assert.Equal(t, 0, tasks[1].Have, "being watered is not watering")
}
