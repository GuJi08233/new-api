package controller

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/wendao"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const lineagePath = "/api/game/wendao/sect/lineage"

// 收徒与候选、师承、花费和请求收据一起保存，旧候选摘要不能招到刷新后的其他人。
func TestWendaoLineageThroughTheApi(t *testing.T) {
	require.NoError(t, i18n.Init())
	env := newGameTestEnv(t, sectSettings())
	player := env.player(9, gameTestIpA)
	assert.Nil(t, player.ok(http.MethodGet, lineagePath, nil)["lineage"])
	teacher := prepareOutpostSect(t, player)
	editWendao(t, player, func(save *wendao.Save) { save.Sect.Disciples[0].Realm = 2 })
	view := player.ok(http.MethodGet, lineagePath, nil)
	candidate := view["sect"].(map[string]any)["recruits"].([]any)[0].(map[string]any)
	request := map[string]any{"action": "recruit", "index": 0, "teacher": teacher, "candidate_sum": candidate["sum"], "request_id": "lineage-recruit-1"}
	before, err := loadWendaoSave(player.user.Id)
	require.NoError(t, err)
	answer := player.ok(http.MethodPost, lineagePath, request)
	disciples := answer["sect"].(map[string]any)["disciples"].([]any)
	require.Len(t, disciples, len(before.Sect.Disciples)+1)
	student := disciples[len(disciples)-1].(map[string]any)
	assert.EqualValues(t, teacher, student["teacher_id"])
	assert.EqualValues(t, 2, student["generation"])
	assert.EqualValues(t, 15, student["teaching_pct"])
	assert.Equal(t, view["sect"].(map[string]any)["disciples"].([]any)[0].(map[string]any)["name"], student["teacher_name"])
	assert.Equal(t, candidate["name"], student["name"])
	after, err := loadWendaoSave(player.user.Id)
	require.NoError(t, err)
	assert.Equal(t, before.Sect.Res.Stone-wendao.RecruitCost(before.Sect.Recruits[0]).Stone, after.Sect.Res.Stone)
	again := player.ok(http.MethodPost, lineagePath, request)
	assert.Equal(t, true, again["replayed"])
	assert.Len(t, again["sect"].(map[string]any)["disciples"].([]any), len(disciples))
	assert.Equal(t, answer["sect"].(map[string]any)["res"], again["sect"].(map[string]any)["res"])
	player.fails(http.MethodPost, lineagePath, map[string]any{"action": "recruit", "index": 0, "teacher": teacher, "candidate_sum": candidate["sum"]})

	oldCandidate := again["sect"].(map[string]any)["recruits"].([]any)[1].(map[string]any)
	editWendao(t, player, func(save *wendao.Save) {
		save.Sect.Recruits[1].Name[0] = (save.Sect.Recruits[1].Name[0] + 1) % wendao.NameSurnames
	})
	player.fails(http.MethodPost, lineagePath, map[string]any{"action": "recruit", "index": 1, "teacher": teacher, "candidate_sum": oldCandidate["sum"]})
	stillSaved, err := loadWendaoSave(player.user.Id)
	require.NoError(t, err)
	assert.Len(t, stillSaved.Sect.Disciples, len(after.Sect.Disciples))
	assert.Equal(t, after.Sect.Res, stillSaved.Sect.Res)
	player.fails(http.MethodPost, lineagePath, map[string]any{"action": "recruit", "index": 2, "teacher": teacher, "candidate_sum": oldCandidate["sum"], "request_id": "lineage-recruit-1"})
	assert.Equal(t, 9.0, player.balance())
}

// 忙碌师长不能在另一页收徒；结算后恢复资格，被拒绝的请求不消耗候选、名额或资源。
func TestWendaoLineageWaitsForItsTeacher(t *testing.T) {
	require.NoError(t, i18n.Init())
	env := newGameTestEnv(t, sectSettings())
	player := env.player(0, gameTestIpA)
	teacher := prepareOutpostSect(t, player)
	editWendao(t, player, func(save *wendao.Save) { save.Sect.Disciples[0].Realm = 2 })
	round := player.ok(http.MethodPost, "/api/game/wendao/sect/tournament/start", map[string]any{"team": []int{teacher}})
	view := player.ok(http.MethodGet, lineagePath, nil)
	assert.Contains(t, view["lineage"].(map[string]any)["busy"].([]any), float64(teacher))
	candidate := view["sect"].(map[string]any)["recruits"].([]any)[0].(map[string]any)
	request := map[string]any{"action": "recruit", "index": 0, "teacher": teacher, "candidate_sum": candidate["sum"]}
	before, err := model.GetGameSave(player.user.Id, wendao.SaveKey)
	require.NoError(t, err)
	player.fails(http.MethodPost, lineagePath, request)
	after, err := model.GetGameSave(player.user.Id, wendao.SaveKey)
	require.NoError(t, err)
	assert.Equal(t, before, after)
	player.ok(http.MethodPost, "/api/game/wendao/sect/tournament/action", puzzleAction(round, map[string]any{"action": "abandon"}))
	player.ok(http.MethodPost, lineagePath, request)
	editWendao(t, player, func(save *wendao.Save) {
		save.Hero.Battle = &wendao.HeroBattle{Team: []int{teacher}, Fight: &wendao.Duel{Issued: common.GetTimestamp()}}
	})
	view = player.ok(http.MethodGet, lineagePath, nil)
	assert.Contains(t, view["lineage"].(map[string]any)["busy"].([]any), float64(teacher))
	second := view["sect"].(map[string]any)["recruits"].([]any)[1].(map[string]any)
	player.fails(http.MethodPost, lineagePath, map[string]any{"action": "recruit", "index": 1, "teacher": teacher, "candidate_sum": second["sum"]})
}

// 过期名单在只读副本刷新后的摘要可直接提交，拒绝旧摘要不能要求玩家先去做一个无关的写操作。
func TestWendaoLineageCanRecruitTheReadOnlyRefreshedCandidate(t *testing.T) {
	require.NoError(t, i18n.Init())
	env := newGameTestEnv(t, sectSettings())
	player := env.player(0, gameTestIpA)
	teacher := prepareOutpostSect(t, player)
	editWendao(t, player, func(save *wendao.Save) {
		save.Sect.Disciples[0].Realm = 2
		save.Sect.Last = common.GetTimestamp() - 60
		save.Sect.RecruitAt = common.GetTimestamp() - 1
		save.Sect.Daily.Day = "old-day"
	})
	old, err := loadWendaoSave(player.user.Id)
	require.NoError(t, err)
	oldSum, err := wendao.RequestSum(old.Sect.Recruits[0])
	require.NoError(t, err)
	view := player.ok(http.MethodGet, lineagePath, nil)
	candidate := view["sect"].(map[string]any)["recruits"].([]any)[0].(map[string]any)
	require.NotEqual(t, oldSum, candidate["sum"])
	player.fails(http.MethodPost, lineagePath, map[string]any{"action": "recruit", "index": 0, "teacher": teacher, "candidate_sum": oldSum})
	answer := player.ok(http.MethodPost, lineagePath, map[string]any{"action": "recruit", "index": 0, "teacher": teacher, "candidate_sum": candidate["sum"]})
	disciples := answer["sect"].(map[string]any)["disciples"].([]any)
	assert.Equal(t, candidate["name"], disciples[len(disciples)-1].(map[string]any)["name"])
}
