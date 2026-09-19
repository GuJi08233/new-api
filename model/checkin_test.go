package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupCheckinTest(t *testing.T, minQuota, maxQuota int) {
	t.Helper()
	require.NoError(t, DB.AutoMigrate(&Checkin{}))
	truncateTables(t)
	t.Cleanup(func() { DB.Exec("DELETE FROM checkins") })

	checkin := operation_setting.GetCheckinSetting()
	origin := *checkin
	t.Cleanup(func() { *checkin = origin })
	checkin.Enabled, checkin.MinQuota, checkin.MaxQuota = true, minQuota, maxQuota
}

// 签到奖励是直接入账的钱包写入。事务路径（MySQL/PostgreSQL）曾经用裸的 quota + ? 更新，
// 绕开了 CAS 守卫，余额接近上限时会写出越界额度，而 SQLite 路径却会拒绝——同一份配置
// 在三种数据库上结果不一致。两条路径都必须拒绝并且不留下签到记录。
func TestUserCheckinRejectsCreditBeyondMaxQuota(t *testing.T) {
	for _, dbType := range []common.DatabaseType{common.DatabaseTypeSQLite, common.DatabaseTypeMySQL} {
		t.Run(string(dbType), func(t *testing.T) {
			setupCheckinTest(t, 1000, 1000)
			originMain, originLog := common.MainDatabaseType(), common.LogDatabaseType()
			common.SetDatabaseTypes(dbType, originLog)
			t.Cleanup(func() { common.SetDatabaseTypes(originMain, originLog) })

			ceiling := common.MaxQuota - 1
			require.NoError(t, DB.Create(&User{Id: 901, Username: "checkin-ceiling", Quota: ceiling}).Error)

			record, err := UserCheckin(901)
			require.Error(t, err)
			assert.Nil(t, record)

			quota, err := GetUserQuota(901, false)
			require.NoError(t, err)
			assert.Equal(t, ceiling, quota)

			var checkins int64
			require.NoError(t, DB.Model(&Checkin{}).Where("user_id = ?", 901).Count(&checkins).Error)
			assert.Zero(t, checkins)
		})
	}
}

// 奖励区间本身也是管理员可配置的入账金额，负数会把签到变成扣费：事务路径直接执行
// quota + (-1000)，余额反而被扣走。守卫在选择数据库路径之前，对三种数据库一致生效。
func TestUserCheckinRejectsOutOfRangeRewardSetting(t *testing.T) {
	setupCheckinTest(t, -1000, -1000)
	originMain, originLog := common.MainDatabaseType(), common.LogDatabaseType()
	common.SetDatabaseTypes(common.DatabaseTypeMySQL, originLog)
	t.Cleanup(func() { common.SetDatabaseTypes(originMain, originLog) })

	require.NoError(t, DB.Create(&User{Id: 902, Username: "checkin-negative", Quota: 500}).Error)

	record, err := UserCheckin(902)
	require.Error(t, err)
	assert.Nil(t, record)

	quota, err := GetUserQuota(902, false)
	require.NoError(t, err)
	assert.Equal(t, 500, quota)
}

func TestUserCheckinCreditsRewardOnBothDatabasePaths(t *testing.T) {
	for _, dbType := range []common.DatabaseType{common.DatabaseTypeSQLite, common.DatabaseTypeMySQL} {
		t.Run(string(dbType), func(t *testing.T) {
			setupCheckinTest(t, 1000, 1000)
			originMain, originLog := common.MainDatabaseType(), common.LogDatabaseType()
			common.SetDatabaseTypes(dbType, originLog)
			t.Cleanup(func() { common.SetDatabaseTypes(originMain, originLog) })

			require.NoError(t, DB.Create(&User{Id: 903, Username: "checkin-ok", Quota: 500}).Error)

			record, err := UserCheckin(903)
			require.NoError(t, err)
			require.NotNil(t, record)
			assert.Equal(t, 1000, record.QuotaAwarded)

			quota, err := GetUserQuota(903, false)
			require.NoError(t, err)
			assert.Equal(t, 1500, quota)
		})
	}
}
