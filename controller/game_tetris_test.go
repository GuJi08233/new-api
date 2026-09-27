package controller

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/tetris"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

// startTetrisSession 开一局并把开局时间往前拨 startedAgo 秒，模拟玩家已经玩了这么久。
func startTetrisSession(t *testing.T, env *gameTestEnv, userId int, game tetrisGoldenGame, startedAgo int64) *model.GameSession {
	t.Helper()
	session, err := model.CreateGameSession(userId, gameTetris, game.Seed, "")
	require.NoError(t, err)
	require.NoError(t, env.db.Model(session).Update("created_at", session.CreatedAt-startedAgo).Error)
	return session
}

func finishTetris(player *gamePlayer, session *model.GameSession, game tetrisGoldenGame) map[string]any {
	return player.call(http.MethodPost, "/api/game/tetris/finish", map[string]any{
		"session_id": session.Id,
		"ticks":      game.Ticks,
		"inputs":     game.Inputs,
	})
}

func tetrisSetting(rule operation_setting.SkillRule) *operation_setting.GameSetting {
	return gameSetting(func(setting *operation_setting.GameSetting) { setting.Tetris = rule })
}

// 成绩与奖励只认服务端重放：请求里只有帧数和操作，服务端从这一局的种子重放出与浏览器引擎一致的成绩
// (见 pkg/tetris/testdata/golden.json)，奖励取达到的最高一档，每天只能领 DailyRewards 次。游戏内时长超过开局以来经过的时间、
// 结算别人的对局都会被拒绝；重复提交同一局返回第一次的结果，不会再发一次。
func TestFinishTetrisSettlesReplayedScore(t *testing.T) {
	env := newGameTestEnv(t, tetrisSetting(operation_setting.SkillRule{
		Enabled: true, DailyRewards: 1, Tiers: []operation_setting.RewardTier{{Score: 5000, Coins: 1}, {Score: 100000, Coins: 3}}, MinSecondsPerCoin: 60,
	}))
	player := env.player(0, gameTestIpA)
	game := loadTetrisGolden(t)["smart-then-top-out"]
	playedSeconds := int64(game.Ticks / tetris.TicksPerSecond)

	rushed := finishTetris(player, startTetrisSession(t, env, player.user.Id, game, playedSeconds/2), game)
	assert.Equal(t, false, rushed["success"], "开局才过了一半的时间，不可能已经玩完这一局")
	foreign := finishTetris(player, startTetrisSession(t, env, player.user.Id+100000, game, playedSeconds+10), game)
	assert.Equal(t, false, foreign["success"], "不能结算别人的对局")

	session := startTetrisSession(t, env, player.user.Id, game, playedSeconds+10)
	settled := finishTetris(player, session, game)
	require.Equal(t, true, settled["success"], settled["message"])
	result := settled["data"].(map[string]any)
	assert.EqualValues(t, game.Score, result["score"])
	assert.EqualValues(t, game.Lines, result["detail"].(map[string]any)["lines"])
	assert.EqualValues(t, 3, result["reward"], "136087 分达到 100000 分那一档，271 秒也够 3 个游戏币")
	assert.Equal(t, "", result["limited_by"])
	assert.EqualValues(t, 1, result["progress"].(map[string]any)["rewards_today"])
	assert.EqualValues(t, 3, result["balance"])
	assert.EqualValues(t, game.Score, result["best_score"])

	again := finishTetris(player, session, game)
	require.Equal(t, true, again["success"], again["message"])
	assert.Equal(t, result, again["data"], "重复提交返回第一次结算的结果")

	next := finishTetris(player, startTetrisSession(t, env, player.user.Id, game, playedSeconds+10), game)
	require.Equal(t, true, next["success"], next["message"])
	assert.EqualValues(t, 0, next["data"].(map[string]any)["reward"], "今天的领奖次数已经用完")
	assert.Equal(t, model.GameRewardLimitedByDaily, next["data"].(map[string]any)["limited_by"])

	assert.Equal(t, 3, player.balance())
	var rewardLogs int64
	require.NoError(t, env.db.Model(&model.Log{}).Where("user_id = ? AND type = ? AND content LIKE ?", player.user.Id, model.LogTypeSystem, "游戏奖励%").Count(&rewardLogs).Error)
	assert.EqualValues(t, 1, rewardLogs, "发了奖励的那一局记一条带 IP 的审计日志")
}

// 成绩够了还要玩够时间：每个游戏币至少要玩 MinSecondsPerCoin 秒。操作快过人类极限的对局判定为脚本，
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
		"without a time rule the top tier is paid":         {game: "smart-then-top-out", minSecondsPerCoin: 0, wantReward: 3},
		"a script placing a piece per frame earns nothing": {game: "fast-bot", minSecondsPerCoin: 0, wantReward: 0, wantLimitedBy: model.GameRewardLimitedBySpeed},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			env := newGameTestEnv(t, tetrisSetting(operation_setting.SkillRule{
				Enabled: true, DailyRewards: 3, Tiers: []operation_setting.RewardTier{{Score: 1, Coins: 3}}, MinSecondsPerCoin: c.minSecondsPerCoin,
			}))
			player := env.player(0, gameTestIpA)
			game := golden[c.game]
			session := startTetrisSession(t, env, player.user.Id, game, int64(game.Ticks/tetris.TicksPerSecond)+10)
			response := finishTetris(player, session, game)
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
	setting := tetrisSetting(operation_setting.SkillRule{Enabled: true, DailyRewards: 3, Tiers: []operation_setting.RewardTier{{Score: 5000, Coins: 1}}})
	setting.PlayTurnstileEnabled = true
	env := newGameTestEnv(t, setting)
	player := env.player(0, gameTestIpA)
	previousTurnstile := common.TurnstileCheckEnabled
	common.TurnstileCheckEnabled = true
	t.Cleanup(func() { common.TurnstileCheckEnabled = previousTurnstile })

	player.fails(http.MethodPost, "/api/game/tetris/start", nil)
	var sessions int64
	require.NoError(t, env.db.Model(&model.GameSession{}).Where("user_id = ?", player.user.Id).Count(&sessions).Error)
	assert.EqualValues(t, 0, sessions)

	common.TurnstileCheckEnabled = false
	started := player.ok(http.MethodPost, "/api/game/tetris/start", nil)
	assert.NotZero(t, started["session_id"], "站点没有启用 Turnstile 时不要求验证")
}
