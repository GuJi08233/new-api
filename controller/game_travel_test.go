package controller

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/dungeon"
	"github.com/QuantumNous/new-api/pkg/wendao"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const travelBase = "/api/game/wendao/travel"

// 只读状态、每日刷新和跨板块操作不会领取或续长游历；结束才发放，旧行程和重复请求不能影响下一次。
func TestWendaoTravelPaysOnceAfterAnOfflineTrip(t *testing.T) {
	require.NoError(t, i18n.Init())
	env := newGameTestEnv(t, sectSettings())
	player := env.player(7, gameTestIpA)
	assert.Nil(t, player.ok(http.MethodGet, travelBase, nil)["travel"])
	player.fails(http.MethodPost, travelBase+"/action", map[string]any{"action": "start", "region": 0})
	player.ok(http.MethodPost, "/api/game/wendao/hero/action", map[string]any{"action": "create", "class": dungeon.ClassRanger})
	player.fails(http.MethodPost, travelBase+"/action", map[string]any{"action": "start"})
	player.fails(http.MethodPost, travelBase+"/action", map[string]any{"action": "start", "region": -1})
	player.fails(http.MethodPost, travelBase+"/action", map[string]any{"action": "start", "region": 1})

	start := map[string]any{"action": "start", "region": 0, "request_id": "travel-start-1"}
	started := player.ok(http.MethodPost, travelBase+"/action", start)
	active := started["travel"].(map[string]any)["active"].(map[string]any)
	token := active["token"]
	assert.NotEmpty(t, token)
	assert.Equal(t, 0.0, started["hero"].(map[string]any)["rate"], "ordinary cultivation pauses while travelling")
	again := player.ok(http.MethodPost, travelBase+"/action", start)
	assert.Equal(t, true, again["replayed"])
	assert.Equal(t, token, again["travel"].(map[string]any)["active"].(map[string]any)["token"])
	player.fails(http.MethodPost, travelBase+"/action", map[string]any{"action": "start", "region": 0})

	editWendao(t, player, func(save *wendao.Save) {
		save.Hero.Exp = 0
		save.Hero.Travel.Started = common.GetTimestamp() - 25*3600
		save.Hero.Last = save.Hero.Travel.Started
	})
	before, err := loadWendaoSave(player.user.Id)
	require.NoError(t, err)
	preview := player.ok(http.MethodGet, travelBase, nil)
	progress := preview["travel"].(map[string]any)["active"].(map[string]any)
	assert.EqualValues(t, wendao.OfflineCap, progress["elapsed"])
	assert.Equal(t, true, progress["capped"])
	player.ok(http.MethodGet, "/api/game/wendao/agenda", nil)
	player.ok(http.MethodPost, "/api/game/wendao/hero/action", map[string]any{"action": "sync"})
	afterRead, err := loadWendaoSave(player.user.Id)
	require.NoError(t, err)
	assert.Equal(t, before.Sect.Res, afterRead.Sect.Res)
	assert.Equal(t, before.Hero.Exp, afterRead.Hero.Exp)
	assert.Equal(t, before.Hero.Travel.Started, afterRead.Hero.Travel.Started)

	finish := map[string]any{"action": "finish", "token": token, "request_id": "travel-finish-1"}
	finished := player.ok(http.MethodPost, travelBase+"/action", finish)
	loot := finished["loot"].(map[string]any)
	assert.Equal(t, progress["res"], loot["res"])
	assert.Equal(t, progress["exp"], loot["exp"])
	assert.Equal(t, true, loot["capped"])
	assert.NotContains(t, finished["travel"].(map[string]any), "active")
	after, err := loadWendaoSave(player.user.Id)
	require.NoError(t, err)
	assert.Equal(t, before.Sect.Res.Stone+int(loot["res"].(map[string]any)["stone"].(float64)), after.Sect.Res.Stone)
	assert.Equal(t, 7.0, player.balance(), "travel never pays game coins")
	retried := player.ok(http.MethodPost, travelBase+"/action", finish)
	assert.Equal(t, true, retried["replayed"])
	assert.NotContains(t, retried, "loot")
	assert.Equal(t, finished["res"], retried["res"])

	second := player.ok(http.MethodPost, travelBase+"/action", map[string]any{"action": "start", "region": 0})
	newToken := second["travel"].(map[string]any)["active"].(map[string]any)["token"]
	assert.NotEqual(t, token, newToken)
	retriedOld := player.ok(http.MethodPost, travelBase+"/action", finish)
	assert.Equal(t, newToken, retriedOld["travel"].(map[string]any)["active"].(map[string]any)["token"])
	player.fails(http.MethodPost, travelBase+"/action", map[string]any{"action": "finish", "token": token})
	player.fails(http.MethodPost, travelBase+"/action", map[string]any{"action": "start", "region": 1, "request_id": "travel-start-1"})
	setting := sectSettings()
	setting.Wendao.Enabled = false
	operation_setting.SetGameSettingForTest(setting)
	player.fails(http.MethodGet, travelBase, nil)
	player.fails(http.MethodPost, travelBase+"/action", map[string]any{"action": "finish", "token": newToken})
}

// 原子开局入口继续遵守九幽的人机验证策略，验证失败时不留对局；按游戏豁免后才允许创建。
func TestWendaoRiftActivityStartPreservesTurnstile(t *testing.T) {
	setting := sectSettings()
	setting.PlayTurnstileEnabled = true
	env := newGameTestEnv(t, setting)
	player := env.player(0, gameTestIpA)
	previous := common.TurnstileCheckEnabled
	common.TurnstileCheckEnabled = true
	t.Cleanup(func() { common.TurnstileCheckEnabled = previous })
	info := player.ok(http.MethodGet, "/api/game/wendao", nil)
	assert.Equal(t, true, info["abyss"].(map[string]any)["play_turnstile"].(map[string]any)["required"])
	player.fails(http.MethodPost, riftBase+"/start", map[string]any{"class": dungeon.ClassWarrior})
	playing, err := model.GetPlayingGameSession(player.user.Id, riftGame)
	require.NoError(t, err)
	assert.Nil(t, playing)
	exempt := setting.Clone()
	exempt.PlayTurnstileExemptGames = []string{wendaoGame}
	operation_setting.SetGameSettingForTest(exempt)
	player.ok(http.MethodPost, riftBase+"/start", map[string]any{"class": dungeon.ClassWarrior})
}

// 所有发出斗法房间的入口都与游历互斥；结束后可恢复，秘境整轮未完时也不能出发。
func TestWendaoTravelExcludesJourneyContractsAndRift(t *testing.T) {
	require.NoError(t, i18n.Init())
	env := newGameTestEnv(t, sectSettings())
	player := env.player(0, gameTestIpA)
	hero := "/api/game/wendao/hero/action"
	player.ok(http.MethodPost, hero, map[string]any{"action": "create", "class": dungeon.ClassWarrior})
	player.ok(http.MethodPost, hero, map[string]any{"action": "visit", "node": "c1_oath"})
	fight := player.ok(http.MethodPost, hero, map[string]any{"action": "fight", "node": "c1_backhill"})
	player.fails(http.MethodPost, travelBase+"/action", map[string]any{"action": "start", "region": 0})
	player.ok(http.MethodPost, hero, map[string]any{"action": "flee", "battle_token": fight["fight"].(map[string]any)["token"]})
	player.ok(http.MethodPost, "/api/game/wendao/duel/start", map[string]any{"kind": "trial", "id": "tianjian"})
	player.fails(http.MethodPost, travelBase+"/action", map[string]any{"action": "start", "region": 0})
	player.ok(http.MethodPost, "/api/game/wendao/duel/giveup", map[string]any{})

	start := map[string]any{"action": "start", "region": 0, "request_id": "trip-before-rift"}
	trip := player.ok(http.MethodPost, travelBase+"/action", start)
	for _, action := range []string{"fight", "climb", "trial"} {
		player.fails(http.MethodPost, hero, map[string]any{"action": action, "node": "c1_backhill"})
	}
	player.fails(http.MethodPost, "/api/game/wendao/duel/start", map[string]any{"kind": "trial", "id": "tianjian"})
	player.fails(http.MethodPost, riftBase+"/start", map[string]any{"class": dungeon.ClassWarrior})
	playing, err := model.GetPlayingGameSession(player.user.Id, riftGame)
	require.NoError(t, err)
	assert.Nil(t, playing, "rejecting rift entry does not leave a playing session")
	assert.NotNil(t, player.ok(http.MethodGet, riftBase+"/state", nil)["travel"].(map[string]any)["active"])
	assert.NotNil(t, player.ok(http.MethodGet, "/api/game/wendao/duel/state", nil)["travel"].(map[string]any)["active"])
	token := trip["travel"].(map[string]any)["active"].(map[string]any)["token"]
	player.ok(http.MethodPost, travelBase+"/action", map[string]any{"action": "finish", "token": token})

	rift := player.ok(http.MethodPost, riftBase+"/start", map[string]any{"class": dungeon.ClassWarrior})["session"].(map[string]any)
	assert.Equal(t, true, player.ok(http.MethodGet, travelBase, nil)["rift_active"])
	player.fails(http.MethodPost, travelBase+"/action", map[string]any{"action": "start", "region": 0})
	oldStart := player.ok(http.MethodPost, travelBase+"/action", start)
	assert.Equal(t, true, oldStart["replayed"], "old successful starts are recognized before checking a newer rift")
	assert.NotContains(t, oldStart["travel"].(map[string]any), "active")
	riftAct(player, rift, map[string]any{"action": "abandon"})
	player.ok(http.MethodPost, travelBase+"/action", map[string]any{"action": "start", "region": 0})
}
