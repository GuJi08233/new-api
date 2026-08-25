package service

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupGroupSettings(t *testing.T, groupRatio, userUsableGroups, autoGroups string) {
	t.Helper()
	savedGroupRatio := ratio_setting.GroupRatio2JSONString()
	savedUsableGroups := setting.UserUsableGroups2JSONString()
	savedAutoGroups := setting.AutoGroups2JsonString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(savedGroupRatio))
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(savedUsableGroups))
		require.NoError(t, setting.UpdateAutoGroupsByJsonString(savedAutoGroups))
	})
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(groupRatio))
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(userUsableGroups))
	require.NoError(t, setting.UpdateAutoGroupsByJsonString(autoGroups))
}

func seedAttachSubscription(t *testing.T, userId int, group string, endTime int64) {
	t.Helper()
	sub := &model.UserSubscription{
		UserId:       userId,
		Status:       model.SubscriptionStatusActive,
		EndTime:      endTime,
		GroupMode:    model.SubscriptionGroupModeAttach,
		UpgradeGroup: group,
	}
	require.NoError(t, model.DB.Create(sub).Error)
}

func TestSplitTokenGroups(t *testing.T) {
	assert.Equal(t, []string{"vip", "default"}, SplitTokenGroups(" vip , default ,vip,, "))
	assert.Equal(t, []string{"default"}, SplitTokenGroups("default"))
	assert.Empty(t, SplitTokenGroups(""))
}

func TestIsMultiCandidateGroup(t *testing.T) {
	assert.True(t, IsMultiCandidateGroup("auto"))
	assert.True(t, IsMultiCandidateGroup("vip,default"))
	assert.False(t, IsMultiCandidateGroup("vip"))
	assert.False(t, IsMultiCandidateGroup(""))
}

func TestFilterUsableTokenGroupsKeepsOnlyLiveGroups(t *testing.T) {
	truncate(t)
	setupGroupSettings(t,
		`{"default":1,"vip":0.8,"embedding":0.2}`,
		`{"default":"默认分组","vip":"VIP","auto":"智能"}`,
		`[]`)
	seedUser(t, 4001, 1000)

	// 多分组保序过滤：embedding 未授权被跳过，其余保留
	effective, reason := FilterUsableTokenGroups(4001, "default", "vip,embedding,default")
	assert.Equal(t, "vip,default", effective)
	assert.Equal(t, "无权访问 embedding 分组", reason)

	// 订阅追加分组在有效期内可用
	seedAttachSubscription(t, 4001, "embedding", time.Now().Add(time.Hour).Unix())
	effective, _ = FilterUsableTokenGroups(4001, "default", "embedding,default")
	assert.Equal(t, "embedding,default", effective)

	// 订阅过期后附加分组自动跳过，令牌无感回退到剩余分组
	require.NoError(t, model.DB.Model(&model.UserSubscription{}).
		Where("user_id = ?", 4001).
		Update("end_time", time.Now().Add(-time.Hour).Unix()).Error)
	effective, _ = FilterUsableTokenGroups(4001, "default", "embedding,default")
	assert.Equal(t, "default", effective)

	// 全部失效时返回空串和最后一条拒绝原因
	effective, reason = FilterUsableTokenGroups(4001, "default", "embedding")
	assert.Empty(t, effective)
	assert.Equal(t, "无权访问 embedding 分组", reason)

	// auto 与具体分组混选时按纯 auto 处理，且 auto 豁免弃用检查
	effective, _ = FilterUsableTokenGroups(4001, "default", "default,auto")
	assert.Equal(t, "auto", effective)

	// 在可选分组中但已从分组倍率删除 → 弃用
	setupGroupSettings(t,
		`{"default":1}`,
		`{"default":"默认分组","vip":"VIP"}`,
		`[]`)
	effective, reason = FilterUsableTokenGroups(4001, "default", "vip,default")
	assert.Equal(t, "default", effective)
	assert.Equal(t, "分组 vip 已被弃用", reason)
}

func TestGetUserAutoGroupIncludesLiveAttachedGroups(t *testing.T) {
	truncate(t)
	setupGroupSettings(t,
		`{"default":1,"vip":0.8,"embedding":0.2}`,
		`{"default":"默认分组","vip":"VIP"}`,
		`["embedding","vip","default"]`)
	seedUser(t, 4002, 1000)

	// 无订阅：embedding 不在用户可用分组中，被过滤
	assert.Equal(t, []string{"vip", "default"}, GetUserAutoGroup(4002, "default"))

	// 有效订阅：附加分组按全局优先级出现在序列中
	seedAttachSubscription(t, 4002, "embedding", time.Now().Add(time.Hour).Unix())
	assert.Equal(t, []string{"embedding", "vip", "default"}, GetUserAutoGroup(4002, "default"))

	// 订阅过期：附加分组自动从序列消失
	require.NoError(t, model.DB.Model(&model.UserSubscription{}).
		Where("user_id = ?", 4002).
		Update("end_time", time.Now().Add(-time.Hour).Unix()).Error)
	assert.Equal(t, []string{"vip", "default"}, GetUserAutoGroup(4002, "default"))
}
