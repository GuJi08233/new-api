package controller

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/dungeon"
	"github.com/QuantumNous/new-api/pkg/wendao"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const outpostPath = "/api/game/wendao/sect/outpost"

func prepareOutpostSect(t *testing.T, player *gamePlayer) int {
	t.Helper()
	player.ok(http.MethodPost, "/api/game/wendao/hero/action", map[string]any{"action": "create", "class": dungeon.ClassWarrior})
	foundTestSect(t, player)
	var disciple int
	editWendao(t, player, func(save *wendao.Save) {
		save.Sect.Buildings[wendao.BuildingHall] = 3
		save.Sect.Res = wendao.Cost{Stone: 10000, Herb: 5000, Ore: 5000, Rep: 1000}
		save.Hero.Realm = 1
		save.Hero.Nodes["c2_arrive"] = &wendao.NodeRecord{Clears: 1}
		save.Sect.Disciples[0].Realm = 1
		disciple = save.Sect.Disciples[0].Id
	})
	return disciple
}

// 建院、派驻、召回与收据一起保存；只读预览不发资源，旧驻守token不能结束同一地域后来的一次派驻。
func TestWendaoOutpostThroughTheApi(t *testing.T) {
	require.NoError(t, i18n.Init())
	env := newGameTestEnv(t, sectSettings())
	player := env.player(4, gameTestIpA)
	assert.Nil(t, player.ok(http.MethodGet, outpostPath, nil)["outposts"])
	id := prepareOutpostSect(t, player)
	before, err := loadWendaoSave(player.user.Id)
	require.NoError(t, err)
	build := map[string]any{"action": "build", "region": 1, "request_id": "build-outpost-1"}
	built := player.ok(http.MethodPost, outpostPath, build)
	assert.Equal(t, true, built["outposts"].(map[string]any)["regions"].([]any)[0].(map[string]any)["built"])
	assert.Equal(t, before.Sect.Res.Stone-wendao.OutpostCost(1).Stone, int(built["sect"].(map[string]any)["res"].(map[string]any)["stone"].(float64)))
	retried := player.ok(http.MethodPost, outpostPath, build)
	assert.Equal(t, true, retried["replayed"])
	assert.Equal(t, built["sect"].(map[string]any)["res"], retried["sect"].(map[string]any)["res"])
	player.fails(http.MethodPost, outpostPath, map[string]any{"action": "build", "region": 0})
	player.fails(http.MethodPost, outpostPath, map[string]any{"action": "build", "region": 2})
	station := map[string]any{"action": "station", "region": 1, "disciple": id, "request_id": "station-outpost-1"}
	stationed := player.ok(http.MethodPost, outpostPath, station)
	disciple := stationed["sect"].(map[string]any)["disciples"].([]any)[0].(map[string]any)
	token := disciple["outpost_token"]
	assert.NotEmpty(t, token)
	assert.EqualValues(t, 1, disciple["outpost"])
	assert.Positive(t, stationed["outposts"].(map[string]any)["total"].(map[string]any)["stone"])
	player.fails(http.MethodPost, "/api/game/wendao/sect/action", map[string]any{"action": "dispatch", "mission": "patrol", "team": []int{id}})
	player.fails(http.MethodPost, "/api/game/wendao/sect/tournament/start", map[string]any{"team": []int{id}})
	view := player.ok(http.MethodGet, "/api/game/wendao/hero", nil)["hero"].(map[string]any)
	assert.Equal(t, false, view["companions"].([]any)[0].(map[string]any)["ready"])
	data, err := model.GetGameSave(player.user.Id, wendao.SaveKey)
	require.NoError(t, err)
	player.ok(http.MethodGet, outpostPath, nil)
	afterRead, err := model.GetGameSave(player.user.Id, wendao.SaveKey)
	require.NoError(t, err)
	assert.Equal(t, data, afterRead)
	recall := map[string]any{"action": "recall", "region": 1, "disciple": id, "token": token, "request_id": "recall-outpost-1"}
	player.ok(http.MethodPost, outpostPath, recall)
	newStation := player.ok(http.MethodPost, outpostPath, map[string]any{"action": "station", "region": 1, "disciple": id})
	newToken := newStation["sect"].(map[string]any)["disciples"].([]any)[0].(map[string]any)["outpost_token"]
	assert.NotEqual(t, token, newToken)
	again := player.ok(http.MethodPost, outpostPath, recall)
	assert.Equal(t, true, again["replayed"])
	assert.Equal(t, newToken, again["sect"].(map[string]any)["disciples"].([]any)[0].(map[string]any)["outpost_token"])
	player.fails(http.MethodPost, outpostPath, map[string]any{"action": "recall", "region": 1, "disciple": id, "token": token})
	assert.Equal(t, 4.0, player.balance())
}

// 大比先开局时派驻被拒绝，结算后才可派驻；有效期限以外的会话不占用弟子。
func TestWendaoOutpostWaitsForActiveDisciples(t *testing.T) {
	require.NoError(t, i18n.Init())
	env := newGameTestEnv(t, sectSettings())
	player := env.player(0, gameTestIpA)
	id := prepareOutpostSect(t, player)
	player.ok(http.MethodPost, outpostPath, map[string]any{"action": "build", "region": 1})
	round := player.ok(http.MethodPost, "/api/game/wendao/sect/tournament/start", map[string]any{"team": []int{id}})
	busy := player.ok(http.MethodGet, outpostPath, nil)["outposts"].(map[string]any)["busy"].([]any)
	assert.Contains(t, busy, float64(id))
	player.fails(http.MethodPost, outpostPath, map[string]any{"action": "station", "region": 1, "disciple": id})
	player.ok(http.MethodPost, "/api/game/wendao/sect/tournament/action", puzzleAction(round, map[string]any{"action": "abandon"}))
	stationed := player.ok(http.MethodPost, outpostPath, map[string]any{"action": "station", "region": 1, "disciple": id})
	token := stationed["sect"].(map[string]any)["disciples"].([]any)[0].(map[string]any)["outpost_token"]
	player.ok(http.MethodPost, outpostPath, map[string]any{"action": "recall", "region": 1, "disciple": id, "token": token})
	round = player.ok(http.MethodPost, "/api/game/wendao/sect/tournament/start", map[string]any{"team": []int{id}})
	require.NoError(t, env.db.Model(&model.GameSession{}).Where("id = ?", round["session_id"]).Update("created_at", common.GetTimestamp()-skillSessionTTL-1).Error)
	player.ok(http.MethodPost, outpostPath, map[string]any{"action": "station", "region": 1, "disciple": id})
}
