package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMigrateDBCreatesUpstreamSupportTables(t *testing.T) {
	originalDB := DB
	originalLogDB := LOG_DB
	originalMainType := common.MainDatabaseType()
	originalLogType := common.LogDatabaseType()
	t.Cleanup(func() {
		DB = originalDB
		LOG_DB = originalLogDB
		common.SetDatabaseTypes(originalMainType, originalLogType)
		initCol()
	})

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	DB = db
	LOG_DB = db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	initCol()

	require.NoError(t, migrateDB())
	for _, table := range []any{
		&PerfMetric{},
		&SystemInstance{},
		&SystemTask{},
		&SystemTaskLock{},
		&CasbinRule{},
		&AuthzRole{},
		&UserSubscriptionTierUsage{},
		&GameCoinAccount{},
		&GameCoinLog{},
		&GameCoinWithdrawal{},
		&GameCoinWithdrawDay{},
		&GameCoinConvertDay{},
		&GameSession{},
		&GameRewardDay{},
		&GameRewardIpDay{},
		&GameRoom{},
		&GameRoomSeat{},
		&GameJackpotRound{},
		&GameJackpotEntry{},
	} {
		require.True(t, DB.Migrator().HasTable(table))
	}
	require.True(t, DB.Migrator().HasColumn(&SubscriptionPlan{}, "quota_tiers"))
	require.True(t, DB.Migrator().HasColumn(&SubscriptionPlan{}, "disable_balance_deduction"))

	// A restart must preserve a plan's chosen quota mode. Adding equivalent
	// tiers would impose another independent cap on subscriptions and renewals.
	plan := SubscriptionPlan{Title: "Single quota mode", TotalAmount: 100, QuotaResetPeriod: SubscriptionResetDaily}
	require.NoError(t, DB.Omit("group_mode").Create(&plan).Error)
	for _, migrate := range []func() error{migrateDB, migrateDBFast} {
		require.NoError(t, migrate())
		var saved SubscriptionPlan
		require.NoError(t, DB.First(&saved, plan.Id).Error)
		require.EqualValues(t, 100, saved.TotalAmount)
		require.Equal(t, SubscriptionResetDaily, saved.QuotaResetPeriod)
		require.Empty(t, saved.QuotaTiers)
	}
}
