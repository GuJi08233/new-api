package controller

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/snake"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// snakeGoldenGame 是 pkg/snake/testdata/golden.json 里浏览器引擎打出的一整局。
type snakeGoldenGame struct {
	Name      string `json:"name"`
	Seed      uint32 `json:"seed"`
	Steps     int    `json:"steps"`
	Inputs    []int  `json:"inputs"`
	Score     int    `json:"score"`
	Length    int    `json:"length"`
	ElapsedMs int    `json:"elapsedMs"`
	Over      bool   `json:"over"`
}

func loadSnakeGolden(t *testing.T) map[string]snakeGoldenGame {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "pkg", "snake", "testdata", "golden.json"))
	require.NoError(t, err)
	var golden struct {
		Games []snakeGoldenGame `json:"games"`
	}
	require.NoError(t, common.Unmarshal(data, &golden))
	games := make(map[string]snakeGoldenGame, len(golden.Games))
	for _, game := range golden.Games {
		games[game.Name] = game
	}
	return games
}

// startSnakeSession 用 seed 开一局并把开局时间往前拨 startedAgo 秒，模拟玩家已经玩了这么久。
func startSnakeSession(t *testing.T, env *gameTestEnv, userId int, seed uint32, startedAgo int64) *model.GameSession {
	t.Helper()
	session, err := model.CreateGameSession(userId, gameSnake, seed, "")
	require.NoError(t, err)
	require.NoError(t, env.db.Model(session).Update("created_at", session.CreatedAt-startedAgo).Error)
	return session
}

func finishSnake(player *gamePlayer, sessionId int, steps int, inputs []int) map[string]any {
	return player.call(http.MethodPost, "/api/game/snake/finish", map[string]any{
		"session_id": sessionId,
		"steps":      steps,
		"inputs":     inputs,
	})
}

func snakeSetting(rule operation_setting.SkillRule) *operation_setting.GameSetting {
	return gameSetting(func(setting *operation_setting.GameSetting) { setting.Snake = rule })
}

// 开局给出的种子就是结算时重放用的种子：浏览器照着它走出的一局，服务端重放出同样的步数与游戏内时长。
func TestStartSnakeHandsOutTheReplaySeed(t *testing.T) {
	env := newGameTestEnv(t, snakeSetting(operation_setting.SkillRule{
		Enabled: true, DailyRewards: 3, Tiers: []operation_setting.RewardTier{{Score: 100, Coins: 1}},
	}))
	player := env.player(0, gameTestIpA)
	started := player.ok(http.MethodPost, "/api/game/snake/start", nil)
	sessionId := int(started["session_id"].(float64))

	// 刚开局只有 2 秒余量，走 10 步(1.5 秒)，中途向上转一次。
	local := snake.New(uint32(started["seed"].(float64)))
	var inputs []int
	for step := 0; step < 10; step++ {
		turn := snake.NoTurn
		if step == 4 {
			turn = snake.Up
		}
		if local.Step(turn) {
			inputs = append(inputs, step, int(turn))
		}
	}
	result := player.ok(http.MethodPost, "/api/game/snake/finish", map[string]any{"session_id": sessionId, "steps": 10, "inputs": inputs})
	assert.EqualValues(t, local.Score, result["score"])
	assert.Equal(t, map[string]any{"length": float64(local.Length()), "steps": float64(local.Steps), "over": local.Over}, result["detail"])
}

// 成绩与奖励只认服务端重放：吃到的食物数达到的最高一档，每个游戏币要求的时长按重放出的游戏内时长(所有步长之和)计算，
// 与开局以来过了多久无关。游戏内时长超过开局以来的真实时间(另留 2 秒余量)的记录拒绝；重复提交同一局返回第一次的结果。
func TestFinishSnakeSettlesReplayedScore(t *testing.T) {
	env := newGameTestEnv(t, snakeSetting(operation_setting.SkillRule{
		Enabled: true, DailyRewards: 3, Tiers: []operation_setting.RewardTier{{Score: 20, Coins: 1}, {Score: 40, Coins: 2}}, MinSecondsPerCoin: 45,
	}))
	player := env.player(0, gameTestIpA)
	game := loadSnakeGolden(t)["smart-then-crash"]
	playedSeconds := int64(game.ElapsedMs / 1000)

	rushed := finishSnake(player, startSnakeSession(t, env, player.user.Id, game.Seed, playedSeconds-5).Id, game.Steps, game.Inputs)
	assert.Equal(t, false, rushed["success"], "游戏内 63 秒的一局不可能 58 秒就玩完")
	malformed := finishSnake(player, startSnakeSession(t, env, player.user.Id, game.Seed, 600).Id, game.Steps, game.Inputs[1:])
	assert.Equal(t, false, malformed["success"], "转向记录要成对")

	session := startSnakeSession(t, env, player.user.Id, game.Seed, 600)
	settled := finishSnake(player, session.Id, game.Steps, game.Inputs)
	require.Equal(t, true, settled["success"], settled["message"])
	result := settled["data"].(map[string]any)
	assert.EqualValues(t, game.Score, result["score"])
	assert.Equal(t, map[string]any{"length": float64(game.Length), "steps": float64(game.Steps), "over": true}, result["detail"])
	// 45 个食物达到 2 个游戏币那一档，但游戏内只玩了 63 秒，每个游戏币要 45 秒，只够 1 个；开局以来的 600 秒不算数。
	assert.EqualValues(t, 1, result["reward"])
	assert.Equal(t, model.GameRewardLimitedByTime, result["limited_by"])
	assert.EqualValues(t, 1, result["balance"])
	assert.EqualValues(t, game.Score, result["best_score"])

	again := finishSnake(player, session.Id, 1, nil)
	require.Equal(t, true, again["success"], again["message"])
	assert.Equal(t, result, again["data"], "重复提交返回第一次结算的结果")
	assert.Equal(t, 1, player.balance())
}
