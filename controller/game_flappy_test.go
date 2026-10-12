package controller

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/flappy"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// flappyGoldenGame 是 pkg/flappy/testdata/golden.json 里浏览器引擎打出的一整局。
type flappyGoldenGame struct {
	Name      string `json:"name"`
	Seed      uint32 `json:"seed"`
	Ticks     int    `json:"ticks"`
	Flaps     []int  `json:"flaps"`
	Score     int    `json:"score"`
	FlapCount int    `json:"flapCount"`
	Over      bool   `json:"over"`
}

func loadFlappyGolden(t *testing.T) map[string]flappyGoldenGame {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "pkg", "flappy", "testdata", "golden.json"))
	require.NoError(t, err)
	var golden struct {
		Games []flappyGoldenGame `json:"games"`
	}
	require.NoError(t, common.Unmarshal(data, &golden))
	games := make(map[string]flappyGoldenGame, len(golden.Games))
	for _, game := range golden.Games {
		games[game.Name] = game
	}
	return games
}

// startFlappySession 用 seed 开一局并把开局时间往前拨 startedAgo 秒，模拟玩家已经玩了这么久。
func startFlappySession(t *testing.T, env *gameTestEnv, userId int, seed uint32, startedAgo int64) *model.GameSession {
	t.Helper()
	session, err := model.CreateGameSession(userId, gameFlappy, seed, "")
	require.NoError(t, err)
	require.NoError(t, env.db.Model(session).Update("created_at", session.CreatedAt-startedAgo).Error)
	return session
}

func finishFlappy(player *gamePlayer, sessionId int, ticks int, flaps []int) map[string]any {
	return player.call(http.MethodPost, "/api/game/flappy/finish", map[string]any{
		"session_id": sessionId,
		"ticks":      ticks,
		"flaps":      flaps,
	})
}

func flappySetting(rule operation_setting.SkillRule) *operation_setting.GameSetting {
	return gameSetting(func(setting *operation_setting.GameSetting) { setting.Flappy = rule })
}

// 开局给出的种子就是结算时重放用的种子：浏览器照着它飞出的一局，服务端重放出同样的帧数与鸟的位置。
func TestStartFlappyHandsOutTheReplaySeed(t *testing.T) {
	env := newGameTestEnv(t, flappySetting(operation_setting.SkillRule{
		Enabled: true, DailyRewards: 3, Tiers: []operation_setting.RewardTier{{Score: 100, Coins: 1}},
	}))
	player := env.player(0, gameTestIpA)
	started := player.ok(http.MethodPost, "/api/game/flappy/start", nil)
	sessionId := int(started["session_id"].(float64))

	// 刚开局只有 2 秒余量：飞 100 帧，每 30 帧拍一次。
	local := flappy.New(uint32(started["seed"].(float64)))
	var flaps []int
	last := 0
	for tick := 0; tick < 100; tick++ {
		flap := tick%30 == 0
		if flap {
			flaps = append(flaps, tick-last)
			last = tick
		}
		local.Tick(flap)
	}
	result := player.ok(http.MethodPost, "/api/game/flappy/finish", map[string]any{"session_id": sessionId, "ticks": 100, "flaps": flaps})
	assert.EqualValues(t, local.Score, result["score"])
	assert.Equal(t, map[string]any{"ticks": float64(local.Ticks), "flaps": float64(local.Flaps), "over": local.Over, "peak_flaps": float64(2)}, result["detail"])
}

// 成绩与奖励只认服务端重放：穿过的管道数达到的最高一档，每个游戏币要求的时长按重放出的游戏内帧数计算。
// 游戏内帧数超过开局以来的真实时间(另留 2 秒余量)、第 0 帧没拍翅膀的记录拒绝；重复提交同一局返回第一次的结果。
func TestFinishFlappySettlesReplayedScore(t *testing.T) {
	env := newGameTestEnv(t, flappySetting(operation_setting.SkillRule{
		Enabled: true, DailyRewards: 3, Tiers: []operation_setting.RewardTier{{Score: 15, Coins: 1}, {Score: 40, Coins: 2}}, MinSecondsPerCoin: 30,
	}))
	player := env.player(0, gameTestIpA)
	game := loadFlappyGolden(t)["human-then-fall"]
	playedSeconds := int64(game.Ticks / flappy.TicksPerSecond)

	rushed := finishFlappy(player, startFlappySession(t, env, player.user.Id, game.Seed, playedSeconds-5).Id, game.Ticks, game.Flaps)
	assert.Equal(t, false, rushed["success"], "游戏内 62 秒的一局不可能 57 秒就玩完")
	shifted := append([]int{1}, game.Flaps[1:]...)
	malformed := finishFlappy(player, startFlappySession(t, env, player.user.Id, game.Seed, 600).Id, game.Ticks, shifted)
	assert.Equal(t, false, malformed["success"], "计时从第一次拍翅膀开始，第 0 帧一定拍了翅膀")

	session := startFlappySession(t, env, player.user.Id, game.Seed, playedSeconds+10)
	settled := finishFlappy(player, session.Id, game.Ticks, game.Flaps)
	require.Equal(t, true, settled["success"], settled["message"])
	result := settled["data"].(map[string]any)
	assert.EqualValues(t, game.Score, result["score"])
	detail := result["detail"].(map[string]any)
	assert.EqualValues(t, game.Ticks, detail["ticks"])
	assert.EqualValues(t, game.FlapCount, detail["flaps"])
	assert.Equal(t, true, detail["over"])
	assert.LessOrEqual(t, detail["peak_flaps"], float64(flappy.HumanFlapLimit))
	assert.Equal(t, 2.0, result["reward"], "40 根管道达到 2 个游戏币那一档，62 秒也够 2 个")
	assert.Equal(t, "", result["limited_by"])
	assert.Equal(t, 2.0, result["balance"])

	again := finishFlappy(player, session.Id, 1, []int{0})
	require.Equal(t, true, again["success"], again["message"])
	assert.Equal(t, result, again["data"], "重复提交返回第一次结算的结果")
	assert.Equal(t, 2.0, player.balance())
}

// 任意 1 秒内拍翅膀超过 12 次只可能是脚本：不发奖励，成绩不算数(记为 0，不会成为最高分)，并给管理员留一条风控告警，
// 告警里写着重放出的成绩。
func TestFinishFlappyRefusesScriptedFlapping(t *testing.T) {
	env := newGameTestEnv(t, flappySetting(operation_setting.SkillRule{
		Enabled: true, DailyRewards: 3, Tiers: []operation_setting.RewardTier{{Score: 1, Coins: 3}},
	}))
	player := env.player(0, gameTestIpA)
	game := loadFlappyGolden(t)["script"]

	session := startFlappySession(t, env, player.user.Id, game.Seed, int64(game.Ticks/flappy.TicksPerSecond)+10)
	response := finishFlappy(player, session.Id, game.Ticks, game.Flaps)
	require.Equal(t, true, response["success"], response["message"])
	result := response["data"].(map[string]any)
	require.Greater(t, game.Score, 0)
	assert.EqualValues(t, 0, result["score"])
	assert.EqualValues(t, 0, result["best_score"])
	assert.Greater(t, result["detail"].(map[string]any)["peak_flaps"], float64(flappy.HumanFlapLimit))
	assert.Equal(t, 0.0, result["reward"])
	assert.Equal(t, model.GameRewardLimitedBySpeed, result["limited_by"])
	assert.Equal(t, 0.0, player.balance())

	var alerts []model.RiskEvent
	require.NoError(t, env.db.Where("user_id = ? AND rule = ?", player.user.Id, "game:"+gameFlappy).Find(&alerts).Error)
	require.Len(t, alerts, 1)
	assert.Contains(t, alerts[0].Reason, fmt.Sprintf("重放成绩 %d", game.Score))
}
