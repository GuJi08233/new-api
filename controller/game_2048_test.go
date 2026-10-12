package controller

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/g2048"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// g2048GoldenGame 是 pkg/g2048/testdata/golden.json 里浏览器引擎打出的一整局。
type g2048GoldenGame struct {
	Name           string `json:"name"`
	Seed           uint32 `json:"seed"`
	Moves          string `json:"moves"`
	Score          int    `json:"score"`
	MaxTile        int    `json:"maxTile"`
	EffectiveMoves int    `json:"effectiveMoves"`
	Over           bool   `json:"over"`
}

func load2048Golden(t *testing.T) map[string]g2048GoldenGame {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "pkg", "g2048", "testdata", "golden.json"))
	require.NoError(t, err)
	var golden struct {
		Games []g2048GoldenGame `json:"games"`
	}
	require.NoError(t, common.Unmarshal(data, &golden))
	games := make(map[string]g2048GoldenGame, len(golden.Games))
	for _, game := range golden.Games {
		games[game.Name] = game
	}
	return games
}

// start2048Session 用 seed 开一局并把开局时间往前拨 startedAgo 秒，模拟玩家已经玩了这么久。
func start2048Session(t *testing.T, env *gameTestEnv, userId int, seed uint32, startedAgo int64) *model.GameSession {
	t.Helper()
	session, err := model.CreateGameSession(userId, game2048, seed, "")
	require.NoError(t, err)
	require.NoError(t, env.db.Model(session).Update("created_at", session.CreatedAt-startedAgo).Error)
	return session
}

func finish2048(player *gamePlayer, sessionId int, moves string) map[string]any {
	return player.call(http.MethodPost, "/api/game/2048/finish", map[string]any{
		"session_id": sessionId,
		"moves":      moves,
	})
}

func g2048Setting(rule operation_setting.SkillRule) *operation_setting.GameSetting {
	return gameSetting(func(setting *operation_setting.GameSetting) { setting.Game2048 = rule })
}

// 开局给出的种子就是结算时重放用的种子：浏览器照着它打出的移动，服务端重放出同样的得分。
func TestStart2048HandsOutTheReplaySeed(t *testing.T) {
	env := newGameTestEnv(t, g2048Setting(operation_setting.SkillRule{
		Enabled: true, DailyRewards: 3, Tiers: []operation_setting.RewardTier{{Score: 100000, Coins: 1}},
	}))
	player := env.player(0, gameTestIpA)
	started := player.ok(http.MethodPost, "/api/game/2048/start", nil)
	sessionId := int(started["session_id"].(float64))

	local := g2048.New(uint32(started["seed"].(float64)))
	moves := ""
	for _, direction := range []g2048.Direction{g2048.Left, g2048.Down, g2048.Right, g2048.Down, g2048.Left, g2048.Up, g2048.Down, g2048.Left} {
		if local.Move(direction) {
			moves += string(rune('0' + direction))
		}
	}
	result := player.ok(http.MethodPost, "/api/game/2048/finish", map[string]any{"session_id": sessionId, "moves": moves})
	assert.EqualValues(t, local.Score, result["score"])
	assert.EqualValues(t, local.Moves, result["detail"].(map[string]any)["moves"])
}

// 成绩与奖励只认服务端重放：得分达到的最高一档，再按开局以来的真实时间计算每个游戏币要求的时长(2048 没有游戏内时钟)。
// 有效移动快过人手(每秒 12 次，另留 2 秒余量)、记录里有 0-3 以外字符的一律拒绝；重复提交同一局返回第一次的结果。
func TestFinish2048SettlesReplayedScore(t *testing.T) {
	env := newGameTestEnv(t, g2048Setting(operation_setting.SkillRule{
		Enabled: true, DailyRewards: 3, Tiers: []operation_setting.RewardTier{{Score: 6000, Coins: 1}, {Score: 20000, Coins: 2}}, MinSecondsPerCoin: 60,
	}))
	player := env.player(0, gameTestIpA)
	game := load2048Golden(t)["smart-to-game-over"]
	require.Equal(t, len(game.Moves), game.EffectiveMoves, "浏览器只记生效的移动")

	// 1265 次有效移动至少要 1265/12 - 2 ≈ 103.4 秒。
	rushed := finish2048(player, start2048Session(t, env, player.user.Id, game.Seed, 60).Id, game.Moves)
	assert.Equal(t, false, rushed["success"], "60 秒按不出 1265 次移动")
	malformed := finish2048(player, start2048Session(t, env, player.user.Id, game.Seed, 200).Id, game.Moves+"4")
	assert.Equal(t, false, malformed["success"], "记录里有 0-3 以外的字符")

	// 110 秒只够 1 个游戏币，虽然 24484 分达到了 2 个游戏币那一档：先发 1 个，另一个记着。
	short := finish2048(player, start2048Session(t, env, player.user.Id, game.Seed, 110).Id, game.Moves)
	require.Equal(t, true, short["success"], short["message"])
	assert.Equal(t, 1.0, short["data"].(map[string]any)["reward"])
	assert.Equal(t, model.GameRewardLimitedByTime, short["data"].(map[string]any)["limited_by"])
	assert.Equal(t, 1.0, short["data"].(map[string]any)["progress"].(map[string]any)["pending_reward"])

	session := start2048Session(t, env, player.user.Id, game.Seed, 200)
	settled := finish2048(player, session.Id, game.Moves)
	require.Equal(t, true, settled["success"], settled["message"])
	result := settled["data"].(map[string]any)
	assert.EqualValues(t, game.Score, result["score"])
	assert.Equal(t, map[string]any{"max_tile": float64(game.MaxTile), "moves": float64(game.EffectiveMoves), "over": true}, result["detail"])
	assert.Equal(t, 3.0, result["reward"], "这一局的 2 个加上之前记着的 1 个，累计 310 秒够发")
	assert.Equal(t, "", result["limited_by"])
	assert.Equal(t, 4.0, result["balance"])
	assert.EqualValues(t, game.Score, result["best_score"])

	again := finish2048(player, session.Id, strings.Repeat("0", 10))
	require.Equal(t, true, again["success"], again["message"])
	assert.Equal(t, result, again["data"], "重复提交返回第一次结算的结果")
	assert.Equal(t, 4.0, player.balance())
}
