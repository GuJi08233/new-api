package controller

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/wendao"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 仙盟的一整轮：建盟扣宗门资源、盟名唯一；入盟、捐献记盟资与贡献；盟主花盟资升级，成员产出随之加成；每人每天讨伐一次周 Boss，
// 击杀时本周出过力的成员都记一份击杀奖励，领奖记进宗门且只能领一次；盟主退盟时盟主之位交给贡献最高的成员，最后一人离开时解散。
func TestSectAllianceLifecycle(t *testing.T) {
	require.NoError(t, i18n.Init())
	env := newGameTestEnv(t, sectSettings())
	leader := env.player(0, gameTestIpA)
	member := env.player(0, gameTestIpB)
	path := "/api/game/wendao/sect/alliance"
	sectView(t, leader, map[string]any{"action": "sync"})
	sectView(t, member, map[string]any{"action": "sync"})

	message := leader.fails(http.MethodPost, path+"/create", map[string]any{"name": "青云盟"})
	assert.Equal(t, i18n.Translate(i18n.LangEn, i18n.MsgGameSectUnavailable), message, "the main hall must reach level 2")
	editSect(t, leader, func(s *wendao.Sect) {
		s.Buildings[wendao.BuildingHall] = 2
		s.Res = wendao.Cost{Stone: 5000, Herb: 500, Ore: 100, Rep: 100}
	})
	leader.fails(http.MethodPost, path+"/create", map[string]any{"name": "   "})
	created := leader.ok(http.MethodPost, path+"/create", map[string]any{"name": " 青云盟 "})
	alliance := created["alliance"].(map[string]any)
	assert.Equal(t, "青云盟", alliance["name"])
	assert.Equal(t, float64(4000), created["sect"].(map[string]any)["res"].(map[string]any)["stone"])
	id := alliance["id"]

	editSect(t, member, func(s *wendao.Sect) {
		s.Buildings[wendao.BuildingHall] = 2
		s.Res = wendao.Cost{Stone: 2000, Herb: 200, Ore: 50, Rep: 100}
	})
	message = member.fails(http.MethodPost, path+"/create", map[string]any{"name": "青云盟"})
	assert.Equal(t, i18n.Translate(i18n.LangEn, i18n.MsgGameAllianceName), message)
	listed := member.ok(http.MethodGet, path, nil)
	require.Len(t, listed["list"], 1)
	member.ok(http.MethodPost, path+"/join", map[string]any{"id": id})
	message = member.fails(http.MethodPost, path+"/join", map[string]any{"id": id})
	assert.Equal(t, i18n.Translate(i18n.LangEn, i18n.MsgGameAllianceJoined), message)

	member.fails(http.MethodPost, path+"/donate", map[string]any{"stone": 99999})
	member.fails(http.MethodPost, path+"/donate", map[string]any{"rep": 10})
	donated := member.ok(http.MethodPost, path+"/donate", map[string]any{"stone": 100, "ore": 10})
	assert.Equal(t, float64(130), donated["alliance"].(map[string]any)["funds"])
	assert.Equal(t, float64(130), donated["member"].(map[string]any)["contribution"])
	message = member.fails(http.MethodPost, path+"/upgrade", nil)
	assert.Equal(t, i18n.Translate(i18n.LangEn, i18n.MsgGameAllianceNotLeader), message)
	message = leader.fails(http.MethodPost, path+"/upgrade", nil)
	assert.Equal(t, i18n.Translate(i18n.LangEn, i18n.MsgGameAllianceFunds), message)
	require.NoError(t, env.db.Model(&model.SectAlliance{}).Where("id = ?", int(id.(float64))).Update("funds", wendao.AllianceUpgradeFunds(1)).Error)
	upgraded := leader.ok(http.MethodPost, path+"/upgrade", nil)
	assert.Equal(t, float64(2), upgraded["alliance"].(map[string]any)["level"])
	assert.Equal(t, float64(0), upgraded["alliance"].(map[string]any)["funds"])
	rates := sectView(t, member, map[string]any{"action": "sync"})["rates"].(map[string]any)
	assert.InDelta(t, 2*20*(1+2*wendao.AllianceBonusPerLevel)+30*(1+2*wendao.AllianceBonusPerLevel), rates["stone"], 1e-9)

	team := func(player *gamePlayer) []any {
		disciples := sectView(t, player, map[string]any{"action": "sync"})["disciples"].([]any)
		return []any{disciples[0].(map[string]any)["id"], disciples[1].(map[string]any)["id"]}
	}
	hit := member.ok(http.MethodPost, path+"/attack", map[string]any{"team": team(member)})
	damage := hit["attack"].(map[string]any)["damage"].(float64)
	assert.Greater(t, damage, float64(0))
	assert.Equal(t, false, hit["attack"].(map[string]any)["killed"])
	message = member.fails(http.MethodPost, path+"/attack", map[string]any{"team": team(member)})
	assert.Equal(t, i18n.Translate(i18n.LangEn, i18n.MsgGameAllianceAttacked), message, "one attack a day")

	require.NoError(t, env.db.Model(&model.SectAlliance{}).Where("id = ?", int(id.(float64))).Update("boss_hp", 1).Error)
	killed := leader.ok(http.MethodPost, path+"/attack", map[string]any{"team": team(leader)})
	assert.Equal(t, true, killed["attack"].(map[string]any)["killed"])
	assert.Equal(t, float64(1), killed["attack"].(map[string]any)["damage"])

	_, row, err := model.GetSectAllianceOf(member.user.Id)
	require.NoError(t, err)
	attackReward := wendao.AllianceAttackReward(int64(damage))
	killReward := wendao.AllianceKillReward(1)
	assert.Equal(t, attackReward.Stone+killReward.Stone, row.PendingStone)
	assert.Equal(t, attackReward.Rep+killReward.Rep, row.PendingRep)
	assert.Equal(t, wendao.AllianceKillPills, row.PendingPills)

	before := sectView(t, member, map[string]any{"action": "sync"})
	claimed := member.ok(http.MethodPost, path+"/claim", nil)
	after := claimed["sect"].(map[string]any)
	assert.Equal(t, before["res"].(map[string]any)["rep"].(float64)+float64(row.PendingRep), after["res"].(map[string]any)["rep"])
	assert.Equal(t, float64(wendao.AllianceKillPills), after["pills"].(map[string]any)[wendao.PillBreak])
	message = member.fails(http.MethodPost, path+"/claim", nil)
	assert.Equal(t, i18n.Translate(i18n.LangEn, i18n.MsgGameAllianceNothing), message)
	assert.Equal(t, 0.0, member.balance(), "alliances never pay game coins")

	message = member.fails(http.MethodPost, path+"/leave", map[string]any{"user_id": leader.user.Id})
	assert.Equal(t, i18n.Translate(i18n.LangEn, i18n.MsgGameAllianceNotLeader), message)
	leader.ok(http.MethodPost, path+"/leave", nil)
	view := member.ok(http.MethodGet, path, nil)
	assert.Equal(t, float64(member.user.Id), view["alliance"].(map[string]any)["leader_id"], "the top contributor takes over")
	assert.Equal(t, model.SectAllianceRoleLeader, view["member"].(map[string]any)["role"])
	member.ok(http.MethodPost, path+"/leave", nil)
	var left int64
	require.NoError(t, env.db.Model(&model.SectAlliance{}).Count(&left).Error)
	assert.Zero(t, left, "the last member leaving dissolves the alliance")
}
