package controller

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/tetris"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// tetrisGoldenGame 是 pkg/tetris/testdata/golden.json 里浏览器引擎打出的一整局。
type tetrisGoldenGame struct {
	Name   string `json:"name"`
	Seed   uint32 `json:"seed"`
	Ticks  int    `json:"ticks"`
	Inputs []int  `json:"inputs"`
	Score  int    `json:"score"`
	Lines  int    `json:"lines"`
}

func loadTetrisGolden(t *testing.T) map[string]tetrisGoldenGame {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "pkg", "tetris", "testdata", "golden.json"))
	require.NoError(t, err)
	var golden struct {
		Games []tetrisGoldenGame `json:"games"`
	}
	require.NoError(t, common.Unmarshal(data, &golden))
	games := make(map[string]tetrisGoldenGame, len(golden.Games))
	for _, game := range golden.Games {
		games[game.Name] = game
	}
	return games
}

// tetrisFixture 准备一个已登录的用户、俄罗斯方块的开局与结算接口，并发布给定的游戏中心配置。
func tetrisFixture(t *testing.T, setting operation_setting.GameSetting) (*gorm.DB, *gin.Engine, model.User, []*http.Cookie) {
	t.Helper()
	db, router, user := accountSecurityFixture(t)
	require.NoError(t, db.AutoMigrate(&model.GameCoinAccount{}, &model.GameCoinLog{}, &model.GameSession{}, &model.GameRewardDay{}, &model.GameRewardIpDay{}, &model.RiskEvent{}))
	previousSetting := operation_setting.GetGameSetting()
	operation_setting.SetGameSettingForTest(&setting)
	t.Cleanup(func() { operation_setting.SetGameSettingForTest(previousSetting) })
	router.POST("/api/game/tetris/start", middleware.UserAuth(), middleware.GameCenterEnabled(), StartTetris)
	router.POST("/api/game/tetris/finish", middleware.UserAuth(), middleware.GameCenterEnabled(), FinishTetris)
	login, _ := accountRequest(t, router, http.MethodPost, "/primary", nil, nil, "")
	return db, router, user, login.Result().Cookies()
}

// startTetrisSession 开一局并把开局时间往前拨 startedAgo 秒，模拟玩家已经玩了这么久。
func startTetrisSession(t *testing.T, db *gorm.DB, userId int, game tetrisGoldenGame, startedAgo int64) *model.GameSession {
	t.Helper()
	session, err := model.CreateGameSession(userId, gameTetris, game.Seed)
	require.NoError(t, err)
	require.NoError(t, db.Model(session).Update("created_at", session.CreatedAt-startedAgo).Error)
	return session
}

func finishTetrisSession(t *testing.T, router *gin.Engine, cookies []*http.Cookie, session *model.GameSession, game tetrisGoldenGame) map[string]any {
	t.Helper()
	_, response := accountRequest(t, router, http.MethodPost, "/api/game/tetris/finish", cookies, map[string]any{
		"session_id": session.Id,
		"ticks":      game.Ticks,
		"inputs":     game.Inputs,
	}, "")
	return response
}

func tetrisRuleSetting(rule operation_setting.GameRule) operation_setting.GameSetting {
	return operation_setting.GameSetting{Enabled: true, Tetris: rule}
}

// 成绩与奖励只认服务端重放：请求里只有帧数和操作，服务端从这一局的种子重放出与浏览器引擎一致的成绩
// (见 pkg/tetris/testdata/golden.json)，发奖受每人每日上限约束。游戏内时长超过开局以来经过的时间、
// 结算别人的对局都会被拒绝；重复提交同一局返回第一次的结果，不会再发一次。
func TestFinishTetrisSettlesReplayedScore(t *testing.T) {
	db, router, user, cookies := tetrisFixture(t, tetrisRuleSetting(operation_setting.GameRule{
		Enabled: true, DailyRewardLimit: 3, ScorePerCoin: 5000, MinSecondsPerCoin: 60,
	}))
	game := loadTetrisGolden(t)["smart-then-top-out"]
	playedSeconds := int64(game.Ticks / tetris.TicksPerSecond)

	rushed := finishTetrisSession(t, router, cookies, startTetrisSession(t, db, user.Id, game, playedSeconds/2), game)
	assert.Equal(t, false, rushed["success"], "开局才过了一半的时间，不可能已经玩完这一局")
	foreign := finishTetrisSession(t, router, cookies, startTetrisSession(t, db, user.Id+1, game, playedSeconds+10), game)
	assert.Equal(t, false, foreign["success"], "不能结算别人的对局")

	session := startTetrisSession(t, db, user.Id, game, playedSeconds+10)
	settled := finishTetrisSession(t, router, cookies, session, game)
	require.Equal(t, true, settled["success"], settled["message"])
	result := settled["data"].(map[string]any)
	assert.EqualValues(t, game.Score, result["score"])
	assert.EqualValues(t, game.Lines, result["lines"])
	assert.EqualValues(t, 3, result["reward"], "136087 分按每 5000 分 1 个是 27 个，按时长是 4 个，再受每日上限 3 个约束")
	assert.Equal(t, model.GameRewardLimitedByDaily, result["limited_by"])
	assert.EqualValues(t, 3, result["earned_today"])
	assert.EqualValues(t, 3, result["balance"])
	assert.EqualValues(t, game.Score, result["best_score"])

	again := finishTetrisSession(t, router, cookies, session, game)
	require.Equal(t, true, again["success"], again["message"])
	assert.Equal(t, result, again["data"], "重复提交返回第一次结算的结果")

	next := finishTetrisSession(t, router, cookies, startTetrisSession(t, db, user.Id, game, playedSeconds+10), game)
	require.Equal(t, true, next["success"], next["message"])
	assert.EqualValues(t, 0, next["data"].(map[string]any)["reward"], "今天的上限已经领满")

	account, err := model.GetGameCoinAccount(user.Id)
	require.NoError(t, err)
	assert.Equal(t, 3, account.Balance)
	var rewardLogs int64
	require.NoError(t, db.Model(&model.Log{}).Where("user_id = ? AND type = ? AND content LIKE ?", user.Id, model.LogTypeSystem, "游戏奖励%").Count(&rewardLogs).Error)
	assert.EqualValues(t, 1, rewardLogs, "发了奖励的那一局记一条带 IP 的审计日志")
}

// 分数够了还要玩够时间：每个游戏币至少要玩 MinSecondsPerCoin 秒。操作快过人类极限的对局判定为脚本，
// 分数再高也不发奖励。少发的原因随结果返回，浏览器据此提示玩家。
func TestFinishTetrisAppliesAntiBotRules(t *testing.T) {
	golden := loadTetrisGolden(t)
	cases := map[string]struct {
		game              string
		minSecondsPerCoin int
		wantReward        int
		wantLimitedBy     string
	}{
		// 每个游戏币要玩够 120 秒，271 秒的一局只够 2 个。
		"play time limits the reward":                      {game: "smart-then-top-out", minSecondsPerCoin: 120, wantReward: 2, wantLimitedBy: model.GameRewardLimitedByTime},
		"without a time rule only the daily limit applies": {game: "smart-then-top-out", minSecondsPerCoin: 0, wantReward: 3, wantLimitedBy: model.GameRewardLimitedByDaily},
		"a script placing a piece per frame earns nothing": {game: "fast-bot", minSecondsPerCoin: 0, wantReward: 0, wantLimitedBy: model.GameRewardLimitedBySpeed},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			db, router, user, cookies := tetrisFixture(t, tetrisRuleSetting(operation_setting.GameRule{
				Enabled: true, DailyRewardLimit: 3, ScorePerCoin: 5000, MinSecondsPerCoin: c.minSecondsPerCoin,
			}))
			game := golden[c.game]
			session := startTetrisSession(t, db, user.Id, game, int64(game.Ticks/tetris.TicksPerSecond)+10)
			response := finishTetrisSession(t, router, cookies, session, game)
			require.Equal(t, true, response["success"], response["message"])
			result := response["data"].(map[string]any)
			assert.EqualValues(t, game.Score, result["score"])
			assert.EqualValues(t, c.wantReward, result["reward"])
			assert.Equal(t, c.wantLimitedBy, result["limited_by"])
		})
	}
}

// 要求人机验证时，没有带 Turnstile 令牌的开局请求直接拒绝，也不会留下对局。
func TestStartTetrisRequiresTurnstileToken(t *testing.T) {
	setting := tetrisRuleSetting(operation_setting.GameRule{Enabled: true, DailyRewardLimit: 3, ScorePerCoin: 5000})
	setting.PlayTurnstileEnabled = true
	db, router, user, cookies := tetrisFixture(t, setting)
	previousTurnstile := common.TurnstileCheckEnabled
	common.TurnstileCheckEnabled = true
	t.Cleanup(func() { common.TurnstileCheckEnabled = previousTurnstile })

	_, response := accountRequest(t, router, http.MethodPost, "/api/game/tetris/start", cookies, nil, "")
	assert.Equal(t, false, response["success"])
	var sessions int64
	require.NoError(t, db.Model(&model.GameSession{}).Where("user_id = ?", user.Id).Count(&sessions).Error)
	assert.EqualValues(t, 0, sessions)

	common.TurnstileCheckEnabled = false
	_, response = accountRequest(t, router, http.MethodPost, "/api/game/tetris/start", cookies, nil, "")
	assert.Equal(t, true, response["success"], "站点没有启用 Turnstile 时不要求验证")
}
