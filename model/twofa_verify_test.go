package model

import (
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/pquerna/otp/totp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const twoFactorTestSecret = "JBSWY3DPEHPK3PXP"

func newTwoFactorUser(t *testing.T) User {
	t.Helper()
	truncateTables(t)
	user := User{Username: "twofa-verify-user", Password: "password"}
	require.NoError(t, DB.Create(&user).Error)
	require.NoError(t, DB.Create(&TwoFA{UserId: user.Id, Secret: twoFactorTestSecret, IsEnabled: true}).Error)
	return user
}

// 同一验证码只能成功一次：并发提交同一验证码时，条件更新只让一个请求通过，重放不计入失败次数。
func TestVerifyTwoFactorCodeAcceptsEachCodeOnce(t *testing.T) {
	user := newTwoFactorUser(t)
	code, err := totp.GenerateCode(twoFactorTestSecret, time.Now())
	require.NoError(t, err)

	const attempts = 3
	results := make(chan bool, attempts)
	var wg sync.WaitGroup
	for range attempts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			verified, err := VerifyTwoFactorCode(user.Id, code, true)
			assert.NoError(t, err)
			results <- verified
		}()
	}
	wg.Wait()
	close(results)
	wins := 0
	for verified := range results {
		if verified {
			wins++
		}
	}
	assert.Equal(t, 1, wins)

	verified, err := VerifyTwoFactorCode(user.Id, code, true)
	require.NoError(t, err)
	assert.False(t, verified, "the same code cannot be replayed")
	var factor TwoFA
	require.NoError(t, DB.First(&factor, "user_id = ?", user.Id).Error)
	assert.NotZero(t, factor.LastUsedStep)
	assert.NotNil(t, factor.LastUsedAt)
	assert.Zero(t, factor.FailedAttempts)
}

// 错误验证码累计失败次数并在达到上限后锁定；解锁后正确验证码通过并清零计数。
func TestVerifyTwoFactorCodeLocksAfterRepeatedFailures(t *testing.T) {
	user := newTwoFactorUser(t)
	code, err := totp.GenerateCode(twoFactorTestSecret, time.Now())
	require.NoError(t, err)
	wrong := "000000"
	if wrong == code {
		wrong = "111111"
	}
	for i := 0; i < common.MaxFailAttempts; i++ {
		verified, err := VerifyTwoFactorCode(user.Id, wrong, true)
		require.NoError(t, err)
		assert.False(t, verified)
	}
	var factor TwoFA
	require.NoError(t, DB.First(&factor, "user_id = ?", user.Id).Error)
	assert.Equal(t, common.MaxFailAttempts, factor.FailedAttempts)
	require.NotNil(t, factor.LockedUntil)
	_, err = VerifyTwoFactorCode(user.Id, code, true)
	assert.Error(t, err, "a locked factor rejects even a correct code")

	require.NoError(t, DB.Model(&TwoFA{}).Where("id = ?", factor.Id).Update("locked_until", nil).Error)
	verified, err := VerifyTwoFactorCode(user.Id, code, true)
	require.NoError(t, err)
	assert.True(t, verified)
	var unlocked TwoFA
	require.NoError(t, DB.First(&unlocked, "user_id = ?", user.Id).Error)
	assert.Zero(t, unlocked.FailedAttempts)
	assert.Nil(t, unlocked.LockedUntil)
}

// 备用码在验证事务内一次性核销，并发使用只有一个成功；只允许认证器验证码的场景拒绝备用码。
func TestVerifyTwoFactorCodeConsumesBackupCodeOnce(t *testing.T) {
	user := newTwoFactorUser(t)
	const code = "ABCD-1234"
	hash, err := common.HashBackupCode(code)
	require.NoError(t, err)
	require.NoError(t, DB.Create(&TwoFABackupCode{UserId: user.Id, CodeHash: hash}).Error)

	const attempts = 2
	results := make(chan bool, attempts)
	var wg sync.WaitGroup
	for range attempts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			verified, err := VerifyTwoFactorCode(user.Id, "abcd1234", true)
			assert.NoError(t, err)
			results <- verified
		}()
	}
	wg.Wait()
	close(results)
	wins := 0
	for verified := range results {
		if verified {
			wins++
		}
	}
	assert.Equal(t, 1, wins)
	remaining, err := GetUnusedBackupCodeCount(user.Id)
	require.NoError(t, err)
	assert.Zero(t, remaining)

	require.NoError(t, DB.Model(&TwoFABackupCode{}).Where("user_id = ?", user.Id).Updates(map[string]any{"is_used": false, "used_at": nil}).Error)
	verified, err := VerifyTwoFactorCode(user.Id, code, false)
	require.NoError(t, err)
	assert.False(t, verified)
	remaining, err = GetUnusedBackupCodeCount(user.Id)
	require.NoError(t, err)
	assert.Equal(t, 1, remaining)
}

func TestVerifyTwoFactorCodeRequiresEnabledFactor(t *testing.T) {
	truncateTables(t)
	_, err := VerifyTwoFactorCode(4242, "123456", true)
	assert.ErrorIs(t, err, ErrTwoFANotEnabled)
}
