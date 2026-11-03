package controller

import (
	"math"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/pkg/dungeon"
	"github.com/QuantumNous/new-api/pkg/wendao"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const meditationBase = "/api/game/wendao/meditation"

// 打坐的只读预览不写存档，离座结清同一段修为；重发和旧 token 都不能重复结清或离开新的灵脉。
func TestWendaoMeditationSettlesAtTheActivityBoundary(t *testing.T) {
	require.NoError(t, i18n.Init())
	env := newGameTestEnv(t, sectSettings())
	player := env.player(3, gameTestIpA)
	assert.Nil(t, player.ok(http.MethodGet, meditationBase, nil)["meditation"])
	player.ok(http.MethodPost, "/api/game/wendao/hero/action", map[string]any{"action": "create", "class": dungeon.ClassMage})
	player.fails(http.MethodPost, meditationBase+"/action", map[string]any{"action": "start"})
	player.fails(http.MethodPost, meditationBase+"/action", map[string]any{"action": "start", "region": 1})
	start := map[string]any{"action": "start", "region": 0, "request_id": "meditate-1"}
	started := player.ok(http.MethodPost, meditationBase+"/action", start)
	active := started["meditation"].(map[string]any)["active"].(map[string]any)
	token := active["token"]
	assert.NotEmpty(t, token)
	assert.Equal(t, active["rate"], started["hero"].(map[string]any)["rate"])
	assert.Equal(t, active["bonus_pct"], started["hero"].(map[string]any)["sheet"].(map[string]any)["meditation_pct"])
	assert.InDelta(t, active["base_rate"].(float64)*1.65, active["rate"], 1e-9)
	retried := player.ok(http.MethodPost, meditationBase+"/action", start)
	assert.Equal(t, true, retried["replayed"])
	assert.Equal(t, token, retried["meditation"].(map[string]any)["active"].(map[string]any)["token"])
	player.fails(http.MethodPost, travelBase+"/action", map[string]any{"action": "start", "region": 0, "request_id": "meditate-1"})

	editWendao(t, player, func(save *wendao.Save) {
		save.Hero.Layer, save.Hero.Exp = wendao.MaxLayer, 0
		save.Hero.Last = common.GetTimestamp() - 25*3600
		save.Hero.Meditation.Started = save.Hero.Last
	})
	before, err := loadWendaoSave(player.user.Id)
	require.NoError(t, err)
	read := player.ok(http.MethodGet, meditationBase, nil)["hero"].(map[string]any)
	expected := math.Min(before.Hero.IdleRate(before.Sect)*12, read["need"].(float64))
	assert.InDelta(t, expected, read["exp"], 1e-9)
	stillSaved, err := loadWendaoSave(player.user.Id)
	require.NoError(t, err)
	assert.Zero(t, stillSaved.Hero.Exp)
	leave := map[string]any{"action": "leave", "token": token, "request_id": "leave-1"}
	left := player.ok(http.MethodPost, meditationBase+"/action", leave)
	assert.InDelta(t, expected, left["hero"].(map[string]any)["exp"], 1e-9)
	assert.NotContains(t, left["meditation"].(map[string]any), "active")
	assert.Equal(t, 3.0, player.balance())
	newTrip := player.ok(http.MethodPost, meditationBase+"/action", map[string]any{"action": "start", "region": 0})
	newToken := newTrip["meditation"].(map[string]any)["active"].(map[string]any)["token"]
	assert.NotEqual(t, token, newToken)
	again := player.ok(http.MethodPost, meditationBase+"/action", leave)
	assert.Equal(t, true, again["replayed"])
	assert.Equal(t, newToken, again["meditation"].(map[string]any)["active"].(map[string]any)["token"])
	player.fails(http.MethodPost, meditationBase+"/action", map[string]any{"action": "leave", "token": token})
}

func TestWendaoMeditationExcludesTravelAndEveryDuel(t *testing.T) {
	require.NoError(t, i18n.Init())
	env := newGameTestEnv(t, sectSettings())
	player := env.player(0, gameTestIpA)
	hero := "/api/game/wendao/hero/action"
	player.ok(http.MethodPost, hero, map[string]any{"action": "create", "class": dungeon.ClassWarrior})
	player.ok(http.MethodPost, hero, map[string]any{"action": "visit", "node": "c1_oath"})
	started := player.ok(http.MethodPost, meditationBase+"/action", map[string]any{"action": "start", "region": 0})
	for _, action := range []string{"fight", "climb", "trial"} {
		player.fails(http.MethodPost, hero, map[string]any{"action": action, "node": "c1_backhill"})
	}
	player.fails(http.MethodPost, "/api/game/wendao/duel/start", map[string]any{"kind": "trial", "id": "tianjian"})
	player.fails(http.MethodPost, riftBase+"/start", map[string]any{"class": dungeon.ClassWarrior})
	player.fails(http.MethodPost, travelBase+"/action", map[string]any{"action": "start", "region": 0})
	assert.NotNil(t, player.ok(http.MethodGet, riftBase+"/state", nil)["meditation"].(map[string]any)["active"])
	token := started["meditation"].(map[string]any)["active"].(map[string]any)["token"]
	player.ok(http.MethodPost, meditationBase+"/action", map[string]any{"action": "leave", "token": token})
	trip := player.ok(http.MethodPost, travelBase+"/action", map[string]any{"action": "start", "region": 0})
	player.fails(http.MethodPost, meditationBase+"/action", map[string]any{"action": "start", "region": 0})
	player.ok(http.MethodPost, travelBase+"/action", map[string]any{"action": "finish", "token": trip["travel"].(map[string]any)["active"].(map[string]any)["token"]})
	rift := player.ok(http.MethodPost, riftBase+"/start", map[string]any{"class": dungeon.ClassWarrior})["session"].(map[string]any)
	player.fails(http.MethodPost, meditationBase+"/action", map[string]any{"action": "start", "region": 0})
	riftAct(player, rift, map[string]any{"action": "abandon"})
	player.ok(http.MethodPost, meditationBase+"/action", map[string]any{"action": "start", "region": 0})
}
