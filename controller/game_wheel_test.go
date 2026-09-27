package controller

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stopWheelAt 让接下来的旋转都停在给定的格子上。
func stopWheelAt(t *testing.T, segment int) {
	t.Helper()
	previous := spinWheel
	spinWheel = func() (int, error) { return segment, nil }
	t.Cleanup(func() { spinWheel = previous })
}

// 每天先免费转，免费次数用完之后每次按每次花费扣币、不限次数，奖品 = 每次花费 × 格子的倍数。免费旋转的奖品计入累计获得，
// 付费旋转计入累计下注与下注输赢；请求写明要免费转但免费次数已经用完时拒绝，不会改成扣币。
func TestSpinWheelFreeThenPaidWithoutDailyLimit(t *testing.T) {
	env := newGameTestEnv(t, gameSetting(func(s *operation_setting.GameSetting) {
		s.Wheel = operation_setting.WheelRule{Enabled: true, FreeSpins: 1, SpinCost: 2}
	}))
	player := env.player(3, gameTestIpA)

	info := player.ok(http.MethodGet, "/api/game/wheel", nil)
	assert.InDelta(t, 0.8, info["payback"], 1e-9)
	assert.Len(t, info["segments"], 8)
	assert.EqualValues(t, 0, info["free_used_today"])

	cases := []struct {
		free        bool
		segment     int
		wantErr     bool
		wantWin     int
		wantBalance int
		wantNet     int
	}{
		// 3 倍格：免费旋转不扣币，奖品 2 × 3。
		{free: true, segment: 2, wantWin: 6, wantBalance: 9, wantNet: 6},
		{free: true, segment: 2, wantErr: true},
		// 谢谢参与：扣掉 2 个。
		{segment: 3, wantWin: 0, wantBalance: 7, wantNet: 4},
		{segment: 3, wantWin: 0, wantBalance: 5, wantNet: 2},
		// 头奖 50 倍：扣 2 派 100。
		{segment: 0, wantWin: 100, wantBalance: 103, wantNet: 100},
		{segment: 3, wantWin: 0, wantBalance: 101, wantNet: 98},
	}
	for i, c := range cases {
		stopWheelAt(t, c.segment)
		if c.wantErr {
			player.fails(http.MethodPost, "/api/game/wheel/spin", map[string]any{"free": c.free})
			continue
		}
		result := player.ok(http.MethodPost, "/api/game/wheel/spin", map[string]any{"free": c.free})
		assert.EqualValues(t, c.wantWin, result["win"], "case %d win", i)
		assert.EqualValues(t, c.wantBalance, result["balance"], "case %d balance", i)
		assert.EqualValues(t, c.wantNet, result["net_today"], "case %d net today", i)
	}
	account, err := model.GetGameCoinAccount(player.user.Id)
	require.NoError(t, err)
	assert.Equal(t, 3+6, account.TotalEarned, "种子币加上免费旋转的奖品")
	assert.Equal(t, 8, account.TotalWagered)
	assert.Equal(t, 92, account.TotalWagerNet)
}

// 免费旋转是免费奖励：同一 IP 下所有账号合计也只有 FreeSpins 次；奖品计入所有免费奖励的合计上限，
// 合计上限用完之后不能再免费转。
func TestSpinWheelFreeSpinsAreSharedAndCapped(t *testing.T) {
	env := newGameTestEnv(t, gameSetting(func(s *operation_setting.GameSetting) {
		s.Wheel = operation_setting.WheelRule{Enabled: true, FreeSpins: 2, SpinCost: 1}
		s.FreeDailyLimit = 3
	}))
	first := env.player(0, gameTestIpA)
	second := env.player(0, gameTestIpA)
	other := env.player(0, gameTestIpB)

	stopWheelAt(t, 5) // 2 倍格：奖品 2 个
	first.ok(http.MethodPost, "/api/game/wheel/spin", map[string]any{"free": true})
	result := second.ok(http.MethodPost, "/api/game/wheel/spin", map[string]any{"free": true})
	assert.EqualValues(t, 1, result["win"], "同一 IP 的合计上限只剩 1 个")
	assert.Equal(t, model.GameRewardLimitedByTotal, result["limited_by"])
	second.fails(http.MethodPost, "/api/game/wheel/spin", map[string]any{"free": true})

	result = other.ok(http.MethodPost, "/api/game/wheel/spin", map[string]any{"free": true})
	assert.EqualValues(t, 2, result["win"], "别的 IP 不受影响")
	assert.Equal(t, 2, first.balance())
	assert.Equal(t, 1, second.balance())
}
