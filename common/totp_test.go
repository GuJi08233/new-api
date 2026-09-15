package common

import (
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 防重放栅栏依赖 MatchTOTPStep 返回验证码本身所属的时间步，而不是当前时间步。
func TestMatchTOTPStepReturnsStepOfTheCode(t *testing.T) {
	const secret = "JBSWY3DPEHPK3PXP"
	issued := time.Unix(1_800_000_000, 0)
	code, err := totp.GenerateCode(secret, issued)
	require.NoError(t, err)
	expected := issued.Unix() / TOTPPeriod

	for _, offset := range []time.Duration{0, 29 * time.Second, 30 * time.Second, -30 * time.Second, 59 * time.Second} {
		step, matched := MatchTOTPStep(secret, code, issued.Add(offset))
		require.True(t, matched, "offset %v", offset)
		assert.Equal(t, expected, step, "offset %v", offset)
	}
	_, matched := MatchTOTPStep(secret, code, issued.Add(90*time.Second))
	assert.False(t, matched)
	_, matched = MatchTOTPStep(secret, "000000", issued)
	assert.False(t, matched)
	step, matched := MatchTOTPStep(secret, code[:3]+" "+code[3:], issued)
	require.True(t, matched, "spaces inside the code are ignored")
	assert.Equal(t, expected, step)
}
