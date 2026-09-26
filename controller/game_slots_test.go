package controller

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/slots"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// slotsFixture 准备一个已登录、有 coins 个游戏币的用户，挂上游戏中心信息与老虎机旋转接口，并发布给定的老虎机规则。
func slotsFixture(t *testing.T, rule operation_setting.SlotsRule, coins int) (*gorm.DB, *gin.Engine, model.User, []*http.Cookie) {
	t.Helper()
	return spinGameFixture(t, &operation_setting.GameSetting{Enabled: true, Slots: rule}, coins)
}

// spinGameFixture 准备一个已登录、有 coins 个游戏币的用户，挂上游戏中心信息与当场出结果的游戏(老虎机、大转盘)的接口，
// 并发布给定的游戏中心配置。
func spinGameFixture(t *testing.T, setting *operation_setting.GameSetting, coins int) (*gorm.DB, *gin.Engine, model.User, []*http.Cookie) {
	t.Helper()
	db, router, user := accountSecurityFixture(t)
	require.NoError(t, db.AutoMigrate(&model.GameCoinAccount{}, &model.GameCoinLog{}, &model.GameCoinWithdrawDay{}, &model.GameSession{}, &model.GameRewardDay{}, &model.GameRewardIpDay{}))
	previousSetting := operation_setting.GetGameSetting()
	operation_setting.SetGameSettingForTest(setting)
	t.Cleanup(func() { operation_setting.SetGameSettingForTest(previousSetting) })
	router.GET("/api/game/self", middleware.UserAuth(), middleware.GameCenterEnabled(), GetGameCenterSelf)
	router.POST("/api/game/slots/spin", middleware.UserAuth(), middleware.GameCenterEnabled(), SpinSlots)
	router.POST("/api/game/wheel/spin", middleware.UserAuth(), middleware.GameCenterEnabled(), SpinWheel)
	if coins > 0 {
		_, err := model.AdjustGameCoins(user.Id, coins, "seed")
		require.NoError(t, err)
	}
	login, _ := accountRequest(t, router, http.MethodPost, "/primary", nil, nil, "")
	return db, router, user, login.Result().Cookies()
}

// stopSlotsAt 让接下来的旋转都停在给定的停点。
func stopSlotsAt(t *testing.T, stops [3]int) {
	t.Helper()
	previous := spinSlots
	spinSlots = func(stage slots.Stage) (slots.Result, error) { return slots.StopAt(stops, stage), nil }
	t.Cleanup(func() { spinSlots = previous })
}

// 结果只由服务端决定：浏览器按游戏信息里的转轮画出停点处的符号，与接口返回的中线一致。扣注与派彩同时入账，
// 设了每日上限时，派彩让今天的净赢越过上限就只派到上限并告诉浏览器原因，到了上限就不能再转。
// 派彩达到下注 10 倍的大奖记一条带 IP 的审计日志。
func TestSpinSlotsSettlesServerOutcome(t *testing.T) {
	db, router, user, cookies := slotsFixture(t, operation_setting.SlotsRule{Enabled: true, DailyRewardLimit: 5, MaxBet: 5}, 10)

	_, self := accountRequest(t, router, http.MethodGet, "/api/game/self", cookies, nil, "")
	require.Equal(t, true, self["success"], self["message"])
	var game map[string]any
	for _, item := range self["data"].(map[string]any)["games"].([]any) {
		if item.(map[string]any)["key"] == gameSlots {
			game = item.(map[string]any)
		}
	}
	require.NotNil(t, game, "开放的老虎机出现在游戏列表里")
	reels := game["reels"].([]any)
	paybacks := game["paybacks"].(map[string]any)
	assert.InDelta(t, 1.0, paybacks["even"], 1e-9)
	assert.InDelta(t, 21720.0/27000.0, paybacks["house"], 1e-9)

	spin := func(bet int) map[string]any {
		_, response := accountRequest(t, router, http.MethodPost, "/api/game/slots/spin", cookies, map[string]any{"bet": bet}, "")
		return response
	}
	cases := []struct {
		stops         [3]int
		bet           int
		wantWin       int
		wantPayout    int
		wantLimitedBy string
		wantEarned    int
		wantBalance   int
	}{
		// 樱桃、樱桃、柠檬：前两个相同，派彩 3 倍。
		{stops: [3]int{0, 1, 2}, bet: 1, wantWin: 3, wantPayout: 3, wantEarned: 2, wantBalance: 12},
		// 柠檬打头：没有中奖。
		{stops: [3]int{1, 1, 1}, bet: 1, wantWin: 0, wantPayout: 0, wantEarned: 1, wantBalance: 11},
		// 三个 7 应得 50 倍，今天只剩 4 个净赢额度，派彩截断为下注加 4。
		{stops: [3]int{10, 14, 17}, bet: 1, wantWin: 50, wantPayout: 5, wantLimitedBy: model.GameRewardLimitedByDaily, wantEarned: 5, wantBalance: 15},
	}
	for i, c := range cases {
		stopSlotsAt(t, c.stops)
		response := spin(c.bet)
		require.Equal(t, true, response["success"], "case %d: %v", i, response["message"])
		result := response["data"].(map[string]any)
		line := result["line"].([]any)
		for reel, stop := range c.stops {
			assert.Equal(t, reels[reel].([]any)[stop], line[reel], "case %d reel %d", i, reel)
		}
		assert.EqualValues(t, c.wantWin, result["win"], "case %d win", i)
		assert.EqualValues(t, c.wantPayout, result["payout"], "case %d payout", i)
		assert.Equal(t, c.wantLimitedBy, result["limited_by"], "case %d limited by", i)
		assert.EqualValues(t, c.wantEarned, result["earned_today"], "case %d earned", i)
		assert.EqualValues(t, c.wantEarned, result["ip_earned_today"], "case %d ip earned", i)
		assert.EqualValues(t, c.wantBalance, result["balance"], "case %d balance", i)
	}

	refused := spin(1)
	assert.Equal(t, false, refused["success"], "今天的净赢已到上限")
	account, err := model.GetGameCoinAccount(user.Id)
	require.NoError(t, err)
	assert.Equal(t, 15, account.Balance)
	var sessions int64
	require.NoError(t, db.Model(&model.GameSession{}).Where("user_id = ? AND game = ?", user.Id, gameSlots).Count(&sessions).Error)
	assert.EqualValues(t, 3, sessions)
	var bigWinLogs int64
	require.NoError(t, db.Model(&model.Log{}).Where("user_id = ? AND type = ? AND content LIKE ?", user.Id, model.LogTypeSystem, "游戏大奖%").Count(&bigWinLogs).Error)
	assert.EqualValues(t, 1, bigWinLogs, "只有三个 7 那一局是大奖")
}

// 玩家累计下注满均衡阶段下注额之前按均衡赔付表，单个樱桃原数退回；满了之后按常规赔付表，单个樱桃不再退回。
// 阶段按这一局之前的累计下注决定，记在接口返回与对局明细里。
func TestSpinSlotsSwitchesToHouseStageAfterEvenWager(t *testing.T) {
	db, router, user, cookies := slotsFixture(t, operation_setting.SlotsRule{Enabled: true, EvenStageWager: 6, MaxBet: 5}, 20)
	stopSlotsAt(t, [3]int{0, 0, 0}) // 樱桃、柠檬、橙子：只有第一个是樱桃

	cases := []struct {
		wantStage   string
		wantPayout  int
		wantWagered int
		wantBalance int
	}{
		{wantStage: "even", wantPayout: 5, wantWagered: 5, wantBalance: 20},
		{wantStage: "even", wantPayout: 5, wantWagered: 10, wantBalance: 20},
		{wantStage: "house", wantPayout: 0, wantWagered: 15, wantBalance: 15},
	}
	for i, c := range cases {
		_, response := accountRequest(t, router, http.MethodPost, "/api/game/slots/spin", cookies, map[string]any{"bet": 5}, "")
		require.Equal(t, true, response["success"], "spin %d: %v", i, response["message"])
		result := response["data"].(map[string]any)
		assert.Equal(t, c.wantStage, result["stage"], "spin %d stage", i)
		assert.EqualValues(t, c.wantPayout, result["payout"], "spin %d payout", i)
		assert.EqualValues(t, c.wantWagered, result["wagered"], "spin %d wagered", i)
		assert.EqualValues(t, c.wantBalance, result["balance"], "spin %d balance", i)
	}
	var details []string
	require.NoError(t, db.Model(&model.GameSession{}).Where("user_id = ?", user.Id).Order("id asc").Pluck("detail", &details).Error)
	require.Len(t, details, 3)
	assert.Contains(t, details[2], `"stage":"house"`)
	account, err := model.GetGameCoinAccount(user.Id)
	require.NoError(t, err)
	assert.Equal(t, 15, account.TotalWagered)
}

// 每日净赢上限为 0 时不限制：三个 7 按 50 倍全额派彩，之后照样能转。
func TestSpinSlotsWithoutDailyLimitPaysInFull(t *testing.T) {
	_, router, user, cookies := slotsFixture(t, operation_setting.SlotsRule{Enabled: true, DailyRewardLimit: 0, MaxBet: 5}, 10)
	stopSlotsAt(t, [3]int{10, 14, 17})

	for i, wantBalance := range []int{255, 500} {
		_, response := accountRequest(t, router, http.MethodPost, "/api/game/slots/spin", cookies, map[string]any{"bet": 5}, "")
		require.Equal(t, true, response["success"], "spin %d: %v", i, response["message"])
		result := response["data"].(map[string]any)
		assert.EqualValues(t, 250, result["payout"], "spin %d payout", i)
		assert.Equal(t, "", result["limited_by"], "spin %d limited by", i)
		assert.EqualValues(t, wantBalance, result["balance"], "spin %d balance", i)
	}
	account, err := model.GetGameCoinAccount(user.Id)
	require.NoError(t, err)
	assert.Equal(t, 490, account.TotalWagerNet)
}

// 下注数量超出规则、余额不够或老虎机没开放时拒绝旋转，不扣币也不留下对局。
func TestSpinSlotsRejectsBetsOutsideRule(t *testing.T) {
	db, router, user, cookies := slotsFixture(t, operation_setting.SlotsRule{Enabled: true, DailyRewardLimit: 50, MaxBet: 5}, 3)
	stopSlotsAt(t, [3]int{0, 1, 2})

	for _, body := range []map[string]any{{"bet": 0}, {"bet": 6}, {"bet": 4}} {
		_, response := accountRequest(t, router, http.MethodPost, "/api/game/slots/spin", cookies, body, "")
		assert.Equal(t, false, response["success"], body)
	}
	operation_setting.SetGameSettingForTest(&operation_setting.GameSetting{Enabled: true, Slots: operation_setting.SlotsRule{DailyRewardLimit: 50, MaxBet: 5}})
	_, response := accountRequest(t, router, http.MethodPost, "/api/game/slots/spin", cookies, map[string]any{"bet": 1}, "")
	assert.Equal(t, false, response["success"], "老虎机没开放")

	account, err := model.GetGameCoinAccount(user.Id)
	require.NoError(t, err)
	assert.Equal(t, 3, account.Balance)
	var sessions int64
	require.NoError(t, db.Model(&model.GameSession{}).Where("user_id = ?", user.Id).Count(&sessions).Error)
	assert.Zero(t, sessions)
}
