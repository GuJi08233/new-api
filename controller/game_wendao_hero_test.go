package controller

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/pkg/dungeon"
	"github.com/QuantumNous/new-api/pkg/wendao"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 创建主角时一起开宗立派；历练的奖励进宗门的库房；开战后存档里有这一场，逃跑后结束并把丹符放回行囊；问道关闭时不能操作。
func TestHeroJourneyThroughTheApi(t *testing.T) {
	require.NoError(t, i18n.Init())
	env := newGameTestEnv(t, sectSettings())
	player := env.player(0, gameTestIpA)
	path := "/api/game/wendao/hero"

	empty := player.ok(http.MethodGet, path, nil)
	assert.Nil(t, empty["hero"])
	assert.Nil(t, empty["res"], "no sect before the hero is created")
	player.fails(http.MethodPost, path+"/action", map[string]any{"action": "visit", "node": "c1_oath"})

	created := player.ok(http.MethodPost, path+"/action", map[string]any{"action": "create", "class": dungeon.ClassRanger})
	hero := created["hero"].(map[string]any)
	assert.Equal(t, dungeon.ClassRanger, hero["class"])
	assert.Equal(t, []any{"c1_oath"}, hero["open"])
	stone := created["res"].(map[string]any)["stone"].(float64)
	player.fails(http.MethodPost, path+"/action", map[string]any{"action": "create", "class": dungeon.ClassMage})

	visited := player.ok(http.MethodPost, path+"/action", map[string]any{"action": "visit", "node": "c1_oath"})
	assert.Equal(t, stone+50, visited["res"].(map[string]any)["stone"])
	assert.NotEmpty(t, visited["events"])

	fight := player.ok(http.MethodPost, path+"/action", map[string]any{"action": "fight", "node": "c1_backhill"})
	battle := fight["hero"].(map[string]any)["battle"].(map[string]any)
	assert.Equal(t, "c1_backhill", battle["node"])
	run := battle["run"].(map[string]any)
	assert.Equal(t, dungeon.PhaseCombat, run["phase"])
	enemy := run["combat"].(map[string]any)["enemies"].([]any)[0].(map[string]any)
	hp, _ := wendao.EnemyScale(wendao.JourneyNodeOf("c1_backhill"))
	assert.GreaterOrEqual(t, enemy["max_hp"], float64(28*hp/100), "journey enemies are scaled up")
	player.fails(http.MethodPost, path+"/action", map[string]any{"action": "visit", "node": "c1_town"})

	fled := player.ok(http.MethodPost, path+"/action", map[string]any{"action": "flee"})
	assert.Nil(t, fled["hero"].(map[string]any)["battle"])
	assert.Equal(t, dungeon.PhaseCombat, fled["ended"].(map[string]any)["phase"], "the page gets the last view of the fight")

	setting := sectSettings()
	setting.Wendao.Enabled = false
	operation_setting.SetGameSettingForTest(setting)
	player.fails(http.MethodPost, path+"/action", map[string]any{"action": "sync"})
}
