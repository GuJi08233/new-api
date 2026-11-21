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
	personal1, _ := wendao.AlliancePersonalReward(1, 1<<0)
	stage1, _ := wendao.AllianceStageReward(1, 1<<0)
	stage2, _ := wendao.AllianceStageReward(1, 1<<1)
	killReward := wendao.AllianceKillReward(1)
	assert.Equal(t, attackReward.Stone+personal1.Stone+stage1.Stone+stage2.Stone+killReward.Stone, row.PendingStone)
	assert.Equal(t, attackReward.Rep+personal1.Rep+stage1.Rep+stage2.Rep+killReward.Rep, row.PendingRep)
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

// 仙盟周首领扩展契约：轮换机制展示、全盟协作与个人周目标、讨伐与领奖收据幂等、跨盟防重与待办提示。
func TestSectAllianceBossIdempotencyCrossAllianceAndAgenda(t *testing.T) {
	require.NoError(t, i18n.Init())
	env := newGameTestEnv(t, sectSettings())
	alice := env.player(0, gameTestIpA)
	bob := env.player(0, gameTestIpB)
	const path = "/api/game/wendao/sect/alliance"

	for _, p := range []*gamePlayer{alice, bob} {
		sectView(t, p, map[string]any{"action": "sync"})
		editSect(t, p, func(s *wendao.Sect) {
			s.Buildings[wendao.BuildingHall] = 2
			s.Res = wendao.Cost{Stone: 5000, Herb: 500, Ore: 100, Rep: 100}
		})
	}
	createdA := alice.ok(http.MethodPost, path+"/create", map[string]any{"name": "青岚盟"})
	bossView := createdA["boss"].(map[string]any)
	assert.Contains(t, wendao.AllianceBossKinds, bossView["kind"])
	assert.Len(t, bossView["stages"].([]any), 3)
	assert.Len(t, bossView["personal"].([]any), 3)

	// 入盟且未讨伐时，顶部待办提示讨伐仙盟妖王。
	todoKeys := func(p *gamePlayer) []string {
		var keys []string
		for _, raw := range p.ok(http.MethodGet, agendaBase, nil)["todos"].([]any) {
			keys = append(keys, raw.(map[string]any)["key"].(string))
		}
		return keys
	}
	assert.Contains(t, todoKeys(alice), "alliance_boss")
	assert.NotContains(t, todoKeys(alice), "alliance_reward")

	// 强行设为玄甲妖王并给满编三名弟子，核对 +20% 破甲加成与请求收据幂等。
	allianceAId := int(createdA["alliance"].(map[string]any)["id"].(float64))
	require.NoError(t, env.db.Model(&model.SectAlliance{}).Where("id = ?", allianceAId).
		Updates(map[string]any{"boss_kind": wendao.AllianceBossIronShell, "boss_hp": 10000, "boss_max_hp": 10000}).Error)
	editSect(t, alice, func(s *wendao.Sect) {
		c := s.Recruits[0]
		s.Disciples = append(s.Disciples, &wendao.Disciple{
			Id: s.NextId, Name: c.Name, Element: 0, Talent: 3, Trait: c.Trait, Realm: 0, Layer: 1, Job: wendao.JobCultivate, Generation: 1,
		})
		s.NextId++
	})
	disciples := sectView(t, alice, map[string]any{"action": "sync"})["disciples"].([]any)
	team3 := []any{disciples[0].(map[string]any)["id"], disciples[1].(map[string]any)["id"], disciples[2].(map[string]any)["id"]}
	hit := alice.ok(http.MethodPost, path+"/attack", map[string]any{"team": team3, "request_id": "atk-req-1"})
	atk := hit["attack"].(map[string]any)
	assert.Equal(t, float64(20), atk["bonus_pct"])
	assert.Equal(t, []any{float64(1)}, atk["personal_gained"])

	// 相同 request_id 重发讨伐返回 replayed 且不重复扣血/发奖；不同阵容复用同一 request_id 被拒绝。
	replay := alice.ok(http.MethodPost, path+"/attack", map[string]any{"team": team3, "request_id": "atk-req-1"})
	assert.Equal(t, true, replay["replayed"])
	alice.fails(http.MethodPost, path+"/attack", map[string]any{"team": team3[:1], "request_id": "atk-req-1"})

	// 讨伐后待办切换为待领奖励，不再提示今天讨伐。
	keys := todoKeys(alice)
	assert.Contains(t, keys, "alliance_reward")
	assert.NotContains(t, keys, "alliance_boss")

	// 领奖带 request_id，断网重发返回 replayed 且不二次入账。
	beforeClaim := sectView(t, alice, map[string]any{"action": "sync"})["res"].(map[string]any)["stone"].(float64)
	claimed := alice.ok(http.MethodPost, path+"/claim", map[string]any{"request_id": "claim-req-1"})
	afterClaim := claimed["sect"].(map[string]any)["res"].(map[string]any)["stone"].(float64)
	assert.Greater(t, afterClaim, beforeClaim)
	claimReplay := alice.ok(http.MethodPost, path+"/claim", map[string]any{"request_id": "claim-req-1"})
	assert.Equal(t, true, claimReplay["replayed"])
	assert.Equal(t, afterClaim, claimReplay["sect"].(map[string]any)["res"].(map[string]any)["stone"].(float64))
	assert.NotContains(t, todoKeys(alice), "alliance_reward")

	// 同一天退盟转投 Bob 的仙盟 B，当天不能再讨伐；即使跨日到明天(仍同周)讨伐，也不重复发个人 1 次目标奖。
	createdB := bob.ok(http.MethodPost, path+"/create", map[string]any{"name": "赤霞盟"})
	allianceBId := createdB["alliance"].(map[string]any)["id"]
	alice.ok(http.MethodPost, path+"/leave", nil)
	alice.ok(http.MethodPost, path+"/join", map[string]any{"id": allianceBId})
	message := alice.fails(http.MethodPost, path+"/attack", map[string]any{"team": team3, "request_id": "atk-req-2"})
	assert.Equal(t, i18n.Translate(i18n.LangEn, i18n.MsgGameAllianceAttacked), message, "same-day alliance switch cannot attack twice")

	// 模拟次日（仍在本周）讨伐仙盟 B，确认个人 1 次目标已记入位图不再重复发放。
	require.NoError(t, env.db.Model(&model.SectAllianceMember{}).Where("user_id = ?", alice.user.Id).Update("attack_day", "2000-01-01").Error)
	editSect(t, alice, func(s *wendao.Sect) {
		if s.AllianceBoss != nil {
			s.AllianceBoss.AttackDay = "2000-01-01"
		}
	})
	hitB := alice.ok(http.MethodPost, path+"/attack", map[string]any{"team": team3, "request_id": "atk-req-3"})
	assert.NotContains(t, hitB["attack"].(map[string]any), "personal_gained", "personal goal 1 was already awarded in alliance A this week")
	assert.Equal(t, 0.0, alice.balance(), "alliances never pay game coins")
}
