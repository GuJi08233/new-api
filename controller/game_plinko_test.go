package controller

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/plinko"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// dropPlinkoAlong 让接下来的球都沿给定的路径落下：rights 排向右，其余向左，落进从左数第 rights 格。
func dropPlinkoAlong(t *testing.T, rights int) {
	t.Helper()
	var path [plinko.Rows]int
	for row := 0; row < rights; row++ {
		path[row] = 1
	}
	previous := dropPlinko
	dropPlinko = func() ([plinko.Rows]int, error) { return path, nil }
	t.Cleanup(func() { dropPlinko = previous })
}

// 三档各自计次：到了一档的每日次数只挡住这一档，次数为 0 的一档不开放。每颗球先扣这一档的花费，再按落点派彩，
// 对局与流水记在 plinko 下，计数按档分开；派彩达到花费 10 倍的大奖记一条带 IP 的审计日志。
func TestDropPlinkoCountsEachTierSeparately(t *testing.T) {
	rule := operation_setting.PlinkoRule{Enabled: true, DailyDrops1: 2, DailyDrops10: 1, DailyDrops100: 0}
	db, router, user, cookies := spinGameFixture(t, &operation_setting.GameSetting{Enabled: true, Plinko: rule}, 12)

	plinkoInfo := func() map[string]any {
		_, self := accountRequest(t, router, http.MethodGet, "/api/game/self", cookies, nil, "")
		require.Equal(t, true, self["success"], self["message"])
		for _, item := range self["data"].(map[string]any)["games"].([]any) {
			if item.(map[string]any)["key"] == gamePlinko {
				return item.(map[string]any)
			}
		}
		return nil
	}
	game := plinkoInfo()
	require.NotNil(t, game, "开放的 Plinko 出现在游戏列表里")
	assert.Len(t, game["weights"], plinko.Slots)
	tiers := game["tiers"].([]any)
	require.Len(t, tiers, 3)
	assert.EqualValues(t, 2, tiers[0].(map[string]any)["cost"], "1 币档最多扣 2 个")
	assert.InDelta(t, 0.8, tiers[1].(map[string]any)["payback"], 1e-9)
	assert.EqualValues(t, 0, tiers[2].(map[string]any)["daily_drops"])

	cases := []struct {
		stake       int
		rights      int
		wantErr     bool
		wantPayout  int
		wantBalance int
		wantPlays   int
		wantEarned  int
	}{
		// 最左边一格 +10：扣 2 派彩 12。
		{stake: 1, rights: 0, wantPayout: 12, wantBalance: 22, wantPlays: 1, wantEarned: 10},
		// 正中一格 -2：扣 2 派彩 0。
		{stake: 1, rights: 6, wantPayout: 0, wantBalance: 20, wantPlays: 2, wantEarned: 8},
		{stake: 1, rights: 6, wantErr: true},
		// 10 币档另算次数，最右边一格 13 倍：扣 10 派彩 130。
		{stake: 10, rights: 12, wantPayout: 130, wantBalance: 140, wantPlays: 1, wantEarned: 120},
		{stake: 10, rights: 6, wantErr: true},
		{stake: 100, rights: 6, wantErr: true},
		{stake: 5, rights: 6, wantErr: true},
	}
	for i, c := range cases {
		dropPlinkoAlong(t, c.rights)
		_, response := accountRequest(t, router, http.MethodPost, "/api/game/plinko/drop", cookies, map[string]any{"stake": c.stake}, "")
		if c.wantErr {
			assert.Equal(t, false, response["success"], "case %d", i)
			continue
		}
		require.Equal(t, true, response["success"], "case %d: %v", i, response["message"])
		result := response["data"].(map[string]any)
		assert.EqualValues(t, c.rights, result["slot"], "case %d slot", i)
		assert.Len(t, result["path"], plinko.Rows, "case %d path", i)
		assert.EqualValues(t, c.wantPayout, result["payout"], "case %d payout", i)
		assert.EqualValues(t, c.wantBalance, result["balance"], "case %d balance", i)
		assert.EqualValues(t, c.wantPlays, result["plays_today"], "case %d plays", i)
		assert.EqualValues(t, c.wantEarned, result["earned_today"], "case %d earned", i)
	}

	game = plinkoInfo()
	tiers = game["tiers"].([]any)
	assert.EqualValues(t, 2, tiers[0].(map[string]any)["plays_today"])
	assert.EqualValues(t, 1, tiers[1].(map[string]any)["plays_today"])
	assert.EqualValues(t, 128, game["earned_today"], "三档合计")
	account, err := model.GetGameCoinAccount(user.Id)
	require.NoError(t, err)
	assert.Equal(t, 140, account.Balance)
	assert.Equal(t, 14, account.TotalWagered)
	assert.Equal(t, 128, account.TotalWagerNet)
	var sessions int64
	require.NoError(t, db.Model(&model.GameSession{}).Where("user_id = ? AND game = ?", user.Id, gamePlinko).Count(&sessions).Error)
	assert.EqualValues(t, 3, sessions)
	var wagerLogs int64
	require.NoError(t, db.Model(&model.GameCoinLog{}).Where("user_id = ? AND type = ? AND remark = ?", user.Id, model.GameCoinLogTypeGameWager, gamePlinko).Count(&wagerLogs).Error)
	assert.EqualValues(t, 3, wagerLogs)
	var bigWinLogs int64
	require.NoError(t, db.Model(&model.Log{}).Where("user_id = ? AND type = ? AND content LIKE ?", user.Id, model.LogTypeSystem, "游戏大奖%").Count(&bigWinLogs).Error)
	assert.EqualValues(t, 1, bigWinLogs, "只有 13 倍那一颗是大奖")
}

// 1 币档一颗球最多扣 2 个：余额只有 1 个时拒绝，不扣币也不占次数。Plinko 没开放时不能投。
func TestDropPlinkoChecksBalanceAndSwitch(t *testing.T) {
	rule := operation_setting.PlinkoRule{Enabled: true, DailyDrops1: 10, DailyDrops10: 10, DailyDrops100: 10}
	db, router, user, cookies := spinGameFixture(t, &operation_setting.GameSetting{Enabled: true, Plinko: rule}, 1)
	dropPlinkoAlong(t, 0)

	drop := func() map[string]any {
		_, response := accountRequest(t, router, http.MethodPost, "/api/game/plinko/drop", cookies, map[string]any{"stake": 1}, "")
		return response
	}
	assert.Equal(t, false, drop()["success"], "余额不够扣 2 个")
	account, err := model.GetGameCoinAccount(user.Id)
	require.NoError(t, err)
	assert.Equal(t, 1, account.Balance)
	count, err := model.GetGameDailyPlayCount(user.Id, "", plinkoCounter(1), gameCenterDay())
	require.NoError(t, err)
	assert.Equal(t, 0, count.Plays)

	rule.Enabled = false
	operation_setting.SetGameSettingForTest(&operation_setting.GameSetting{Enabled: true, Plinko: rule})
	_, err = model.AdjustGameCoins(user.Id, 10, "seed")
	require.NoError(t, err)
	assert.Equal(t, false, drop()["success"], "Plinko 没开放")

	var sessions int64
	require.NoError(t, db.Model(&model.GameSession{}).Where("user_id = ?", user.Id).Count(&sessions).Error)
	assert.EqualValues(t, 0, sessions)
}
