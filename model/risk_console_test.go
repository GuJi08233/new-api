package model

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 活动趋势按 UTC 整点分桶、按事件类型分组;拦截事件的命中次数取聚合后的累计值。
func TestGetRiskActivityBucketsGroupsByHourAndType(t *testing.T) {
	truncateTables(t)

	hour := (time.Now().Unix()/RiskActivityBucketSeconds - 2) * RiskActivityBucketSeconds
	seed := []*RiskEvent{
		{EventType: RiskEventBlockIp, Ip: "192.0.2.1", Count: 5, CreatedAt: hour + 10},
		{EventType: RiskEventBlockIp, Ip: "192.0.2.2", Count: 3, CreatedAt: hour + 3599},
		{EventType: RiskEventAlert, Ip: "192.0.2.1", CreatedAt: hour + 20},
		{EventType: RiskEventBlockIp, Ip: "192.0.2.1", Count: 2, CreatedAt: hour + RiskActivityBucketSeconds},
		{EventType: RiskEventBanIp, Ip: "192.0.2.1", CreatedAt: hour - 7200}, // 窗口外
	}
	for _, event := range seed {
		require.NoError(t, InsertRiskEvent(event))
	}

	buckets, err := GetRiskActivityBuckets(hour)
	require.NoError(t, err)
	assert.ElementsMatch(t, []RiskActivityBucket{
		{Bucket: hour, EventType: RiskEventAlert, Records: 1, Hits: 1},
		{Bucket: hour, EventType: RiskEventBlockIp, Records: 2, Hits: 8},
		{Bucket: hour + RiskActivityBucketSeconds, EventType: RiskEventBlockIp, Records: 1, Hits: 2},
	}, buckets)
}

// 拦截来源按累计命中排行;规则排行把逐目标的动态封禁拦截合并成一行,不含管理员的手动处置。
func TestRiskTopBlockedIpsAndRules(t *testing.T) {
	truncateTables(t)

	now := time.Now().Unix()
	seed := []*RiskEvent{
		{EventType: RiskEventBlockIp, Ip: "192.0.2.1", Rule: "ban:192.0.2.1", Count: 4, CreatedAt: now - 60},
		{EventType: RiskEventBlockIp, Ip: "192.0.2.2", Rule: "ban:192.0.2.0/24", Count: 3, CreatedAt: now - 30},
		{EventType: RiskEventBlockIp, Ip: "192.0.2.3", Rule: "203.0.113.0/24", Count: 1, CreatedAt: now - 30},
		{EventType: RiskEventBlockClientUa, Ip: "192.0.2.2", Rule: "cli-only", Count: 6, CreatedAt: now - 20},
		{EventType: RiskEventBanIp, Ip: "192.0.2.9", Rule: RiskBanSourceManual, CreatedAt: now - 10},
		{EventType: RiskEventBanIp, Ip: "192.0.2.8", Rule: RiskBanSourceProbeGuard, CreatedAt: now - 10},
		{EventType: RiskEventAlert, Rule: "ip_multi_user", RuleId: "rule-a", CreatedAt: now - 10},
		{EventType: RiskEventBlockIp, Ip: "192.0.2.1", Rule: "ban:192.0.2.1", Count: 50, CreatedAt: now - 7200}, // 窗口外
	}
	for _, event := range seed {
		require.NoError(t, InsertRiskEvent(event))
	}
	since := now - 3600

	ips, err := GetRiskTopBlockedIps(since, 2)
	require.NoError(t, err)
	require.Len(t, ips, 2)
	assert.Equal(t, "192.0.2.2", ips[0].Ip)
	assert.EqualValues(t, 9, ips[0].Hits)
	assert.Equal(t, "192.0.2.1", ips[1].Ip)
	assert.EqualValues(t, 4, ips[1].Hits)

	rules, err := GetRiskTopRules(since, 10)
	require.NoError(t, err)
	summary := map[string]int64{}
	for _, rule := range rules {
		summary[rule.EventType+"|"+rule.Rule+"|"+rule.RuleId] = rule.Hits
	}
	assert.Equal(t, map[string]int64{
		RiskEventBlockIp + "|" + RiskDynamicBanRule + "|":    7,
		RiskEventBlockIp + "|203.0.113.0/24|":                1,
		RiskEventBlockClientUa + "|cli-only|":                6,
		RiskEventBanIp + "|" + RiskBanSourceProbeGuard + "|": 1,
		RiskEventAlert + "|ip_multi_user|rule-a":             1,
	}, summary)
}

// 禁用账号列表按期限筛选、按用户名或 ID 搜索,并带出最近一次禁用事件。
func TestGetRiskDisabledUsers(t *testing.T) {
	truncateTables(t)

	now := common.GetTimestamp()
	users := []User{
		{Id: 11, Username: "temp-a", Status: common.UserStatusDisabled, DisableExpiresAt: now + 600, AffCode: "a11"},
		{Id: 12, Username: "perm-b", DisplayName: "Bob", Status: common.UserStatusDisabled, AffCode: "b12"},
		{Id: 13, Username: "temp-c", Status: common.UserStatusDisabled, DisableExpiresAt: now + 60, AffCode: "c13"},
		{Id: 14, Username: "active-d", Status: common.UserStatusEnabled, AffCode: "d14"},
	}
	require.NoError(t, DB.Create(&users).Error)
	require.NoError(t, InsertRiskEvent(&RiskEvent{EventType: RiskEventBanAuto, UserId: 11, Rule: RiskBanSourceErrorGuard, CreatedAt: now - 100}))
	require.NoError(t, InsertRiskEvent(&RiskEvent{EventType: RiskEventBanAuto, UserId: 12, Rule: RiskBanSourceAutoRule, CreatedAt: now - 300}))
	require.NoError(t, InsertRiskEvent(&RiskEvent{EventType: RiskEventBanManual, UserId: 12, OperatorId: 1, CreatedAt: now - 200}))

	all, total, err := GetRiskDisabledUsers(RiskDisabledUserQuery{Limit: 10})
	require.NoError(t, err)
	assert.EqualValues(t, 3, total)
	require.Len(t, all, 3)
	assert.Equal(t, []int{13, 12, 11}, []int{all[0].Id, all[1].Id, all[2].Id})
	assert.Equal(t, RiskEventBanManual, all[1].LastBanType, "取最近一次禁用事件")
	assert.Equal(t, 1, all[1].LastBanOperator)
	assert.Equal(t, RiskBanSourceErrorGuard, all[2].LastBanRule)
	assert.Zero(t, all[0].LastBanAt, "没有禁用事件的账号不带来源")

	temporary, total, err := GetRiskDisabledUsers(RiskDisabledUserQuery{Kind: RiskDisabledKindTemporary, Limit: 10})
	require.NoError(t, err)
	assert.EqualValues(t, 2, total)
	assert.Equal(t, []int{13, 11}, []int{temporary[0].Id, temporary[1].Id}, "临时禁用按到期先后排列")

	permanent, _, err := GetRiskDisabledUsers(RiskDisabledUserQuery{Kind: RiskDisabledKindPermanent, Limit: 10})
	require.NoError(t, err)
	require.Len(t, permanent, 1)
	assert.Equal(t, 12, permanent[0].Id)

	for _, keyword := range []string{"bob", "perm", "12"} {
		found, total, err := GetRiskDisabledUsers(RiskDisabledUserQuery{Keyword: keyword, Limit: 10})
		require.NoError(t, err)
		assert.EqualValues(t, 1, total, keyword)
		assert.Equal(t, 12, found[0].Id, keyword)
	}
}

// 批量处理逐条检查版本:被别人改过的那条失败,其余照常提交;省略备注与负责人时保留原值。
func TestReviewRiskEventsBatchChecksEachVersion(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Create(&User{Id: 1, Username: "admin", Role: common.RoleAdminUser, Status: common.UserStatusEnabled, AffCode: "adm"}).Error)

	first := &RiskEvent{EventType: RiskEventAlert, Reason: "a"}
	second := &RiskEvent{EventType: RiskEventAlert, Reason: "b"}
	require.NoError(t, InsertRiskEvent(first))
	require.NoError(t, InsertRiskEvent(second))
	_, err := ReviewRiskEvent(second.Id, 0, RiskReviewPending, "keep this note", 1, 1)
	require.NoError(t, err)

	results, err := ReviewRiskEventsBatch([]RiskEventReviewTarget{{Id: first.Id, Version: 0}, {Id: second.Id, Version: 0}, {Id: 999999, Version: 0}},
		RiskReviewConfirmed, nil, nil, 1)
	require.NoError(t, err)
	require.Len(t, results, 3)
	assert.True(t, results[0].Success)
	assert.False(t, results[1].Success)
	assert.Equal(t, ErrRiskReviewChanged.Error(), results[1].Message)
	assert.False(t, results[2].Success)
	assert.Equal(t, "事件不存在", results[2].Message)

	results, err = ReviewRiskEventsBatch([]RiskEventReviewTarget{{Id: second.Id, Version: 1}}, RiskReviewFalsePositive, nil, nil, 1)
	require.NoError(t, err)
	require.True(t, results[0].Success)
	assert.Equal(t, "keep this note", results[0].Event.ReviewNote)
	assert.Equal(t, 1, results[0].Event.AssigneeId)
	reviews, err := GetRiskEventReviews(second.Id)
	require.NoError(t, err)
	require.Len(t, reviews, 2)
	assert.Equal(t, RiskReviewFalsePositive, reviews[0].Status)
	assert.Equal(t, "keep this note", reviews[0].Note, "历史记录写入处理后的实际值")

	// 只改负责人:状态与备注保持原样
	assignee := 1
	results, err = ReviewRiskEventsBatch([]RiskEventReviewTarget{{Id: first.Id, Version: 1}}, "", nil, &assignee, 1)
	require.NoError(t, err)
	require.True(t, results[0].Success)
	assert.Equal(t, RiskReviewConfirmed, results[0].Event.ReviewStatus)
	assert.Equal(t, 1, results[0].Event.AssigneeId)

	_, err = ReviewRiskEventsBatch(nil, RiskReviewConfirmed, nil, nil, 1)
	assert.Error(t, err)
	_, err = ReviewRiskEventsBatch([]RiskEventReviewTarget{{Id: first.Id, Version: 2}}, "", nil, nil, 1)
	assert.Error(t, err, "什么都不改的请求无效")
}

// 事件列表按大类、关键词与未分配负责人筛选。
func TestQueryRiskEventsByCategoryKeywordAndAssignee(t *testing.T) {
	truncateTables(t)

	seed := []*RiskEvent{
		{EventType: RiskEventBlockUa, Ua: "python-requests/2.31", Rule: "python"},
		{EventType: RiskEventBlockClientUa, Ua: "curl/8.0", Rule: "cli-only"},
		{EventType: RiskEventBanAuto, UserId: 5, Username: "abuser", Reason: "微量请求超阈值"},
		{EventType: RiskEventAlert, Reason: "IP 关联 4 个用户", AssigneeId: 3},
	}
	for _, event := range seed {
		require.NoError(t, InsertRiskEvent(event))
	}

	tests := []struct {
		name   string
		filter RiskEventQuery
		want   []string
	}{
		{name: "block category", filter: RiskEventQuery{EventTypes: RiskEventCategoryTypes(RiskEventCategoryBlock)}, want: []string{RiskEventBlockClientUa, RiskEventBlockUa}},
		{name: "keyword matches ua", filter: RiskEventQuery{Keyword: "PYTHON-req"}, want: []string{RiskEventBlockUa}},
		{name: "keyword matches username", filter: RiskEventQuery{Keyword: "abus"}, want: []string{RiskEventBanAuto}},
		{name: "keyword matches reason", filter: RiskEventQuery{Keyword: "关联"}, want: []string{RiskEventAlert}},
		{name: "keyword escapes wildcards", filter: RiskEventQuery{Keyword: "%"}, want: []string{}},
		{name: "unassigned", filter: RiskEventQuery{Unassigned: true, EventTypes: []string{RiskEventAlert, RiskEventBanAuto}}, want: []string{RiskEventBanAuto}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			events, total, err := QueryRiskEvents(tt.filter, 0, 20)
			require.NoError(t, err)
			types := []string{}
			for _, event := range events {
				types = append(types, event.EventType)
			}
			assert.ElementsMatch(t, tt.want, types)
			assert.EqualValues(t, len(tt.want), total)
		})
	}
}

// 封禁列表按状态与来源筛选;拦截次数按封禁目标统计动态封禁产生的拦截。
func TestGetIpBansFiltersAndBanHits(t *testing.T) {
	truncateTables(t)
	resetIpBanCache(t)

	now := common.GetTimestamp()
	seed := []*IpBan{
		{Target: "192.0.2.1", ExpiresAt: 0, Source: RiskBanSourceManual},
		{Target: "192.0.2.2", ExpiresAt: now + 600, Source: RiskBanSourceProbeGuard},
		{Target: "192.0.2.3", ExpiresAt: now - 10, Source: RiskBanSourceProbeGuard},
	}
	for _, ban := range seed {
		ban.CreatedAt, ban.UpdatedAt = now, now
		require.NoError(t, DB.Create(ban).Error)
	}
	targetsOf := func(bans []*IpBan) []string {
		targets := []string{}
		for _, ban := range bans {
			targets = append(targets, ban.Target)
		}
		return targets
	}
	tests := []struct {
		filter IpBanQuery
		want   []string
	}{
		{filter: IpBanQuery{Status: IpBanStatusActive}, want: []string{"192.0.2.1", "192.0.2.2"}},
		{filter: IpBanQuery{Status: IpBanStatusExpired}, want: []string{"192.0.2.3"}},
		{filter: IpBanQuery{Status: IpBanStatusPermanent}, want: []string{"192.0.2.1"}},
		{filter: IpBanQuery{Source: RiskBanSourceProbeGuard, Status: IpBanStatusActive}, want: []string{"192.0.2.2"}},
	}
	for _, tt := range tests {
		bans, total, err := GetIpBans(tt.filter, 0, 20)
		require.NoError(t, err)
		assert.ElementsMatch(t, tt.want, targetsOf(bans), "%+v", tt.filter)
		assert.EqualValues(t, len(tt.want), total)
	}

	require.NoError(t, InsertRiskEvent(&RiskEvent{EventType: RiskEventBlockIp, Ip: "192.0.2.2", Rule: "ban:192.0.2.2", Count: 7}))
	require.NoError(t, InsertRiskEvent(&RiskEvent{EventType: RiskEventBlockIp, Ip: "192.0.2.2", Rule: "ban:192.0.2.2", Count: 2, CreatedAt: now - 7200}))
	require.NoError(t, InsertRiskEvent(&RiskEvent{EventType: RiskEventBlockIp, Ip: "192.0.2.1", Rule: "192.0.2.0/24", Count: 9}))
	hits, err := GetRecentBanBlockHits([]string{"192.0.2.1", "192.0.2.2"}, now-3600)
	require.NoError(t, err)
	assert.Equal(t, map[string]int64{"192.0.2.2": 7}, hits, "只统计动态封禁本身产生的拦截")
}
