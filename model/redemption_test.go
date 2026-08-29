package model

import (
	"fmt"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestSearchRedemptionsFiltersAndPaginates(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&Redemption{}))
	require.NoError(t, DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(&Redemption{}).Error)
	t.Cleanup(func() {
		require.NoError(t, DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(&Redemption{}).Error)
	})

	now := common.GetTimestamp()
	redemptions := []Redemption{
		{Id: 1, Name: "alpha-active", Key: "00000000000000000000000000000001", Status: common.RedemptionCodeStatusEnabled, ExpiredTime: 0},
		{Id: 2, Name: "alpha-future", Key: "00000000000000000000000000000002", Status: common.RedemptionCodeStatusEnabled, ExpiredTime: now + 3600},
		{Id: 3, Name: "alpha-expired", Key: "00000000000000000000000000000003", Status: common.RedemptionCodeStatusEnabled, ExpiredTime: now - 10},
		{Id: 4, Name: "beta-disabled", Key: "00000000000000000000000000000004", Status: common.RedemptionCodeStatusDisabled, ExpiredTime: 0},
		{Id: 5, Name: "beta-used", Key: "00000000000000000000000000000005", Status: common.RedemptionCodeStatusUsed, ExpiredTime: 0},
	}
	require.NoError(t, DB.Create(&redemptions).Error)

	tests := []struct {
		name      string
		keyword   string
		status    string
		startIdx  int
		num       int
		wantTotal int64
		wantIds   []int
	}{
		{
			name:      "no filters returns all rows",
			num:       10,
			wantTotal: 5,
			wantIds:   []int{5, 4, 3, 2, 1},
		},
		{
			name:      "keyword filters by name prefix",
			keyword:   "alpha",
			num:       10,
			wantTotal: 3,
			wantIds:   []int{3, 2, 1},
		},
		{
			name:      "enabled status excludes expired rows",
			status:    "1",
			num:       10,
			wantTotal: 2,
			wantIds:   []int{2, 1},
		},
		{
			name:      "expired status returns enabled expired rows",
			status:    "expired",
			num:       10,
			wantTotal: 1,
			wantIds:   []int{3},
		},
		{
			name:      "disabled status",
			status:    "2",
			num:       10,
			wantTotal: 1,
			wantIds:   []int{4},
		},
		{
			name:      "used status",
			status:    "3",
			num:       10,
			wantTotal: 1,
			wantIds:   []int{5},
		},
		{
			name:      "pagination keeps unpaged total",
			startIdx:  1,
			num:       2,
			wantTotal: 5,
			wantIds:   []int{4, 3},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows, total, err := SearchRedemptions(tt.keyword, tt.status, tt.startIdx, tt.num)
			require.NoError(t, err)
			assert.Equal(t, tt.wantTotal, total)
			gotIds := make([]int, 0, len(rows))
			for _, row := range rows {
				gotIds = append(gotIds, row.Id)
			}
			assert.Equal(t, tt.wantIds, gotIds)
		})
	}
}

func setupRedeemFixture(t *testing.T, quota int) (userId int, key string) {
	t.Helper()
	require.NoError(t, DB.AutoMigrate(&Redemption{}))
	require.NoError(t, DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(&Redemption{}).Error)
	t.Cleanup(func() {
		require.NoError(t, DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(&Redemption{}).Error)
		DB.Exec("DELETE FROM users")
		DB.Exec("DELETE FROM logs")
	})

	user := &User{Username: "redeem-user", Password: "password", Status: common.UserStatusEnabled, Quota: 0}
	require.NoError(t, DB.Create(user).Error)

	key = "10000000000000000000000000000001"
	redemption := &Redemption{
		Name:        "redeem-test",
		Key:         key,
		Status:      common.RedemptionCodeStatusEnabled,
		Quota:       quota,
		CreatedTime: common.GetTimestamp(),
	}
	require.NoError(t, DB.Create(redemption).Error)
	return user.Id, key
}

func TestRedeemCreditsQuotaExactlyOnce(t *testing.T) {
	userId, key := setupRedeemFixture(t, 500)

	quota, err := Redeem(key, userId)
	require.NoError(t, err)
	assert.Equal(t, 500, quota)

	var user User
	require.NoError(t, DB.First(&user, "id = ?", userId).Error)
	assert.Equal(t, 500, user.Quota)

	var redemption Redemption
	require.NoError(t, DB.First(&redemption, "name = ?", "redeem-test").Error)
	assert.Equal(t, common.RedemptionCodeStatusUsed, redemption.Status)
	assert.Equal(t, userId, redemption.UsedUserId)

	// Redeeming the same code again must fail and must not credit quota.
	_, err = Redeem(key, userId)
	require.Error(t, err)
	require.NoError(t, DB.First(&user, "id = ?", userId).Error)
	assert.Equal(t, 500, user.Quota)
}

// Exactly one of several concurrent redeems of the same code may win, and
// quota must be credited exactly once.
func TestRedeemConcurrentSingleSuccess(t *testing.T) {
	userId, key := setupRedeemFixture(t, 300)

	const goroutines = 5
	successes := make([]bool, goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			if _, err := Redeem(key, userId); err == nil {
				successes[idx] = true
			}
		}(i)
	}
	wg.Wait()

	successCount := 0
	for _, ok := range successes {
		if ok {
			successCount++
		}
	}
	assert.Equal(t, 1, successCount, "exactly one concurrent redeem should succeed")

	var user User
	require.NoError(t, DB.First(&user, "id = ?", userId).Error)
	assert.Equal(t, 300, user.Quota, "quota must be credited exactly once")
}

// setupMultiUseRedeemFixture 创建一个可用 maxUses 次的兑换码与若干用户。
func setupMultiUseRedeemFixture(t *testing.T, quota int, maxUses int, userCount int) (userIds []int, key string) {
	t.Helper()
	require.NoError(t, DB.AutoMigrate(&Redemption{}, &CodeUse{}))
	require.NoError(t, DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(&Redemption{}).Error)
	DB.Exec("DELETE FROM code_uses")
	t.Cleanup(func() {
		require.NoError(t, DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(&Redemption{}).Error)
		DB.Exec("DELETE FROM code_uses")
		DB.Exec("DELETE FROM users")
		DB.Exec("DELETE FROM logs")
	})

	for i := 0; i < userCount; i++ {
		user := &User{
			Username: fmt.Sprintf("multi-use-%d", i),
			Password: "password",
			Status:   common.UserStatusEnabled,
			Quota:    0,
			AffCode:  fmt.Sprintf("aff-multi-use-%d", i), // 唯一索引要求各用户不同
		}
		require.NoError(t, DB.Create(user).Error)
		userIds = append(userIds, user.Id)
	}

	key = "20000000000000000000000000000002"
	require.NoError(t, DB.Create(&Redemption{
		Name:        "multi-use-test",
		Key:         key,
		Status:      common.RedemptionCodeStatusEnabled,
		Quota:       quota,
		MaxUses:     maxUses,
		CreatedTime: common.GetTimestamp(),
	}).Error)
	return userIds, key
}

func TestRedeemMultiUseCreditsEachUserOnce(t *testing.T) {
	userIds, key := setupMultiUseRedeemFixture(t, 100, 3, 4)

	// 前 3 个用户各成功一次,每人得到全额 quota
	for i := 0; i < 3; i++ {
		quota, err := Redeem(key, userIds[i])
		require.NoError(t, err, "user %d should redeem successfully", i)
		assert.Equal(t, 100, quota)
	}

	// 同一用户重复兑换被拒,额度不变
	_, err := Redeem(key, userIds[0])
	require.Error(t, err, "same user must not redeem a multi-use code twice")

	// 次数用满后第 4 个用户失败
	_, err = Redeem(key, userIds[3])
	require.Error(t, err, "code should be exhausted after max uses")

	var redemption Redemption
	require.NoError(t, DB.First(&redemption, "name = ?", "multi-use-test").Error)
	assert.Equal(t, 3, redemption.UsedCount)
	assert.Equal(t, common.RedemptionCodeStatusUsed, redemption.Status, "用满后自动关闭")

	for i := 0; i < 3; i++ {
		var user User
		require.NoError(t, DB.First(&user, "id = ?", userIds[i]).Error)
		assert.Equal(t, 100, user.Quota, "user %d quota credited exactly once", i)
	}
	var lastUser User
	require.NoError(t, DB.First(&lastUser, "id = ?", userIds[3]).Error)
	assert.Equal(t, 0, lastUser.Quota, "exhausted code must not credit the 4th user")
}

// 并发下多用码的核销次数不得超过上限,总发放额度 = quota × maxUses。
func TestRedeemMultiUseConcurrentRespectsCap(t *testing.T) {
	const maxUses = 3
	const goroutines = 8
	userIds, key := setupMultiUseRedeemFixture(t, 50, maxUses, goroutines)

	successes := make([]bool, goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			if _, err := Redeem(key, userIds[idx]); err == nil {
				successes[idx] = true
			}
		}(i)
	}
	wg.Wait()

	successCount := 0
	for _, ok := range successes {
		if ok {
			successCount++
		}
	}
	assert.Equal(t, maxUses, successCount, "concurrent redeems must not exceed max uses")

	var redemption Redemption
	require.NoError(t, DB.First(&redemption, "name = ?", "multi-use-test").Error)
	assert.Equal(t, maxUses, redemption.UsedCount)

	totalCredited := 0
	for _, id := range userIds {
		var user User
		require.NoError(t, DB.First(&user, "id = ?", id).Error)
		totalCredited += user.Quota
	}
	assert.Equal(t, 50*maxUses, totalCredited, "total credited quota must equal quota × max uses")
}

// 旧数据 max_uses=0 必须仍按单次码处理。
func TestRedeemLegacyZeroMaxUsesBehavesAsSingleUse(t *testing.T) {
	userIds, key := setupMultiUseRedeemFixture(t, 80, 0, 2)

	quota, err := Redeem(key, userIds[0])
	require.NoError(t, err)
	assert.Equal(t, 80, quota)

	_, err = Redeem(key, userIds[1])
	require.Error(t, err, "legacy code with max_uses=0 must be single use")

	var redemption Redemption
	require.NoError(t, DB.First(&redemption, "name = ?", "multi-use-test").Error)
	assert.Equal(t, common.RedemptionCodeStatusUsed, redemption.Status)
}
