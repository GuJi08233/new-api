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

const agendaBase = "/api/game/wendao/agenda"

// 进入心跳持久化一次真实结算与固定的报告；只读预览不能重复入账、刷新报告时间，旧确认也不能隐藏新报告。
func TestWendaoAgendaPersistsReturnSettlementAndAcknowledgment(t *testing.T) {
	require.NoError(t, i18n.Init())
	env := newGameTestEnv(t, sectSettings())
	player := env.player(11, gameTestIpA)
	player.ok(http.MethodPost, "/api/game/wendao/hero/action", map[string]any{"action": "create", "class": dungeon.ClassWarrior})
	foundTestSect(t, player)
	editWendao(t, player, func(save *wendao.Save) {
		save.Seen = common.GetTimestamp() - 25*3600
		save.Sect.Last, save.Hero.Last = save.Seen, save.Seen
	})
	before, err := loadWendaoSave(player.user.Id)
	require.NoError(t, err)
	data, err := model.GetGameSave(player.user.Id, wendao.SaveKey)
	require.NoError(t, err)
	assert.NotContains(t, player.ok(http.MethodGet, agendaBase, nil), "back", "only the explicit heartbeat creates a report")
	readData, err := model.GetGameSave(player.user.Id, wendao.SaveKey)
	require.NoError(t, err)
	assert.Equal(t, data, readData)

	first := player.ok(http.MethodPost, agendaBase+"/sync", map[string]any{})
	back := first["back"].(map[string]any)
	id := back["id"]
	assert.NotEmpty(t, id)
	assert.EqualValues(t, wendao.OfflineCap, back["cap_seconds"])
	assert.Equal(t, "cave", back["cultivation"].(map[string]any)["kind"])
	assert.Positive(t, back["cultivation"].(map[string]any)["exp"])
	sect := back["sect"].(map[string]any)
	assert.Equal(t, true, sect["capped"])
	assert.NotContains(t, back, "travel")
	after, err := loadWendaoSave(player.user.Id)
	require.NoError(t, err)
	stone := sect["res"].(map[string]any)["stone"].(float64)
	assert.Equal(t, before.Sect.Res.Stone+int(stone), after.Sect.Res.Stone)
	assert.Greater(t, after.Hero.Layer, before.Hero.Layer)
	assert.Equal(t, back, player.ok(http.MethodPost, agendaBase+"/sync", map[string]any{})["back"])
	assert.Equal(t, back, player.ok(http.MethodGet, agendaBase, nil)["back"])
	assert.Equal(t, back, player.ok(http.MethodPost, agendaBase+"/ack", map[string]any{"id": "stale-report"})["back"])
	assert.NotContains(t, player.ok(http.MethodPost, agendaBase+"/ack", map[string]any{"id": id}), "back")
	assert.NotContains(t, player.ok(http.MethodGet, agendaBase, nil), "back", "acknowledgment also applies to another browser")
	assert.NotContains(t, player.ok(http.MethodPost, agendaBase+"/sync", map[string]any{}), "back")

	editWendao(t, player, func(save *wendao.Save) {
		save.Seen = common.GetTimestamp() - 2*3600
		save.Sect.Last, save.Hero.Last = save.Seen, save.Seen
	})
	newReport := player.ok(http.MethodPost, agendaBase+"/sync", map[string]any{})["back"].(map[string]any)
	assert.NotEqual(t, id, newReport["id"])
	assert.Equal(t, newReport, player.ok(http.MethodPost, agendaBase+"/ack", map[string]any{"id": id})["back"])
	editWendao(t, player, func(save *wendao.Save) { save.Back.At = common.GetTimestamp() - 31*60 })
	assert.NotContains(t, player.ok(http.MethodGet, agendaBase, nil), "back")
	assert.NotContains(t, player.ok(http.MethodGet, agendaBase, nil), "back", "reading does not refresh the report lifetime")
	assert.Equal(t, 11.0, player.balance())
	setting := sectSettings()
	setting.Wendao.Enabled = false
	operation_setting.SetGameSettingForTest(setting)
	player.fails(http.MethodPost, agendaBase+"/sync", map[string]any{})
	player.fails(http.MethodPost, agendaBase+"/ack", map[string]any{"id": id})
}

// 游历回流写明整次行程累计待领取，心跳只记报告和活动目标，不自动领取材料或恢复普通闭关。
func TestWendaoAgendaReportsTravelWithoutPayingItsPendingLoot(t *testing.T) {
	require.NoError(t, i18n.Init())
	env := newGameTestEnv(t, sectSettings())
	player := env.player(0, gameTestIpA)
	player.ok(http.MethodPost, "/api/game/wendao/hero/action", map[string]any{"action": "create", "class": dungeon.ClassRanger})
	player.ok(http.MethodPost, travelBase+"/action", map[string]any{"action": "start", "region": 0})
	editWendao(t, player, func(save *wendao.Save) {
		save.Seen = common.GetTimestamp() - 25*3600
		save.Hero.Travel.Started = save.Seen
		save.Hero.Last, save.Sect.Last = save.Seen, save.Seen
	})
	before, err := loadWendaoSave(player.user.Id)
	require.NoError(t, err)
	answer := player.ok(http.MethodPost, agendaBase+"/sync", map[string]any{})
	back := answer["back"].(map[string]any)
	assert.NotContains(t, back, "cultivation")
	travel := back["travel"].(map[string]any)
	assert.Equal(t, true, travel["capped"])
	assert.EqualValues(t, wendao.OfflineCap, travel["elapsed"])
	assert.Positive(t, travel["exp"])
	assert.Equal(t, "travel", answer["goal"].(map[string]any)["key"])
	assert.Equal(t, "world", answer["goal"].(map[string]any)["part"])
	var keys []string
	for _, raw := range answer["todos"].([]any) {
		keys = append(keys, raw.(map[string]any)["key"].(string))
	}
	assert.Contains(t, keys, "travel_ready")
	assert.NotContains(t, keys, "bounties")
	after, err := loadWendaoSave(player.user.Id)
	require.NoError(t, err)
	assert.Equal(t, before.Sect.Res, after.Sect.Res)
	assert.Equal(t, before.Hero.Exp, after.Hero.Exp)
	require.NotNil(t, after.Hero.Travel)
	assert.Equal(t, before.Hero.Travel.Token, after.Hero.Travel.Token)
}

// 恢复可见时两处心跳谁先到都保存同一报告，不能让宗门同步先吃掉回流明细，或在第二次同步再发一遍。
func TestWendaoReturnSurvivesEitherHeartbeatOrder(t *testing.T) {
	for _, sectFirst := range []bool{false, true} {
		t.Run(map[bool]string{false: "agenda-first", true: "sect-first"}[sectFirst], func(t *testing.T) {
			require.NoError(t, i18n.Init())
			env := newGameTestEnv(t, sectSettings())
			player := env.player(0, gameTestIpA)
			player.ok(http.MethodPost, "/api/game/wendao/hero/action", map[string]any{"action": "create", "class": dungeon.ClassMage})
			foundTestSect(t, player)
			editWendao(t, player, func(save *wendao.Save) {
				save.Seen = common.GetTimestamp() - 25*3600
				save.Hero.Last, save.Sect.Last = save.Seen, save.Seen
			})
			before, err := loadWendaoSave(player.user.Id)
			require.NoError(t, err)
			if sectFirst {
				player.ok(http.MethodPost, "/api/game/wendao/sect/action", map[string]any{"action": "sync"})
			} else {
				player.ok(http.MethodPost, agendaBase+"/sync", map[string]any{})
			}
			first, err := loadWendaoSave(player.user.Id)
			require.NoError(t, err)
			require.NotNil(t, first.Back)
			require.NotNil(t, first.Back.Cultivation)
			require.NotNil(t, first.Back.Sect)
			assert.Positive(t, first.Back.Cultivation.Exp)
			assert.Equal(t, first.Sect.Res.Stone-before.Sect.Res.Stone, first.Back.Sect.Res.Stone)
			assert.Positive(t, first.Back.Sect.Res.Stone)
			if sectFirst {
				player.ok(http.MethodPost, agendaBase+"/sync", map[string]any{})
			} else {
				player.ok(http.MethodPost, "/api/game/wendao/sect/action", map[string]any{"action": "sync"})
			}
			second, err := loadWendaoSave(player.user.Id)
			require.NoError(t, err)
			assert.Equal(t, first.Back, second.Back)
			assert.Less(t, second.Sect.Res.Stone-first.Sect.Res.Stone, first.Back.Sect.Res.Stone, "a second heartbeat only settles new elapsed time")
		})
	}
}
