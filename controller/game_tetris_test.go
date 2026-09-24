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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 成绩与奖励只认服务端重放：请求里只有帧数和操作，服务端从这一局的种子重放出与浏览器引擎一致的成绩
// (见 pkg/tetris/testdata/golden.json)，按每满 ScorePerCoin 分 1 个游戏币发放，并受每人每日上限约束。
// 游戏内时长超过开局以来经过的时间、重复结算、结算别人的对局都会被拒绝。
func TestFinishTetrisSettlesReplayedScore(t *testing.T) {
	db, router, user := accountSecurityFixture(t)
	require.NoError(t, db.AutoMigrate(&model.GameCoinAccount{}, &model.GameCoinLog{}, &model.GameSession{}, &model.GameRewardDay{}))
	previousSetting := operation_setting.GetGameSetting()
	operation_setting.SetGameSettingForTest(&operation_setting.GameSetting{
		Enabled: true,
		Tetris:  operation_setting.GameRule{Enabled: true, DailyRewardLimit: 3, ScorePerCoin: 5000},
	})
	t.Cleanup(func() { operation_setting.SetGameSettingForTest(previousSetting) })
	router.POST("/api/game/tetris/finish", middleware.UserAuth(), middleware.GameCenterEnabled(), FinishTetris)
	login, _ := accountRequest(t, router, http.MethodPost, "/primary", nil, nil, "")
	cookies := login.Result().Cookies()

	data, err := os.ReadFile(filepath.Join("..", "pkg", "tetris", "testdata", "golden.json"))
	require.NoError(t, err)
	var golden struct {
		Games []struct {
			Seed   uint32 `json:"seed"`
			Ticks  int    `json:"ticks"`
			Inputs []int  `json:"inputs"`
			Score  int    `json:"score"`
			Lines  int    `json:"lines"`
		} `json:"games"`
	}
	require.NoError(t, common.Unmarshal(data, &golden))
	game := golden.Games[0]
	playedSeconds := int64(game.Ticks / tetris.TicksPerSecond)

	startSession := func(userId int, startedAgo int64) *model.GameSession {
		session, err := model.CreateGameSession(userId, gameTetris, game.Seed)
		require.NoError(t, err)
		require.NoError(t, db.Model(session).Update("created_at", session.CreatedAt-startedAgo).Error)
		return session
	}
	finish := func(session *model.GameSession) map[string]any {
		_, response := accountRequest(t, router, http.MethodPost, "/api/game/tetris/finish", cookies, map[string]any{
			"session_id": session.Id,
			"ticks":      game.Ticks,
			"inputs":     game.Inputs,
		}, "")
		return response
	}

	rushed := finish(startSession(user.Id, playedSeconds/2))
	assert.Equal(t, false, rushed["success"], "开局才过了一半的时间，不可能已经玩完这一局")
	foreign := finish(startSession(user.Id+1, playedSeconds+10))
	assert.Equal(t, false, foreign["success"], "不能结算别人的对局")

	session := startSession(user.Id, playedSeconds+10)
	settled := finish(session)
	require.Equal(t, true, settled["success"], settled["message"])
	result := settled["data"].(map[string]any)
	assert.EqualValues(t, game.Score, result["score"])
	assert.EqualValues(t, game.Lines, result["lines"])
	assert.EqualValues(t, 3, result["reward"], "136087 分按每 5000 分 1 个是 27 个，受每日上限 3 个约束")
	assert.EqualValues(t, 3, result["earned_today"])
	assert.EqualValues(t, 3, result["balance"])

	again := finish(session)
	assert.Equal(t, false, again["success"], "同一局不能结算两次")
	next := finish(startSession(user.Id, playedSeconds+10))
	require.Equal(t, true, next["success"], next["message"])
	assert.EqualValues(t, 0, next["data"].(map[string]any)["reward"], "今天的上限已经领满")

	account, err := model.GetGameCoinAccount(user.Id)
	require.NoError(t, err)
	assert.Equal(t, 3, account.Balance)
}
