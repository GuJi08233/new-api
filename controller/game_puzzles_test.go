package controller

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/bulls"
	"github.com/QuantumNous/new-api/pkg/nonogram"
	"github.com/QuantumNous/new-api/pkg/twentyfour"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// puzzleRule 是测试里解谜游戏的领奖规则：赢下简单难度就奖励 1 个游戏币，不限游戏时长。
func puzzleRule() operation_setting.SkillRule {
	return operation_setting.SkillRule{Enabled: true, DailyRewards: 1, Tiers: []operation_setting.RewardTier{{Score: 1, Coins: 1}}}
}

func puzzleSettings() *operation_setting.GameSetting {
	return gameSetting(func(s *operation_setting.GameSetting) {
		s.Twentyfour, s.Bulls, s.Nonogram = puzzleRule(), puzzleRule(), puzzleRule()
	})
}

// sessionSeed 读出对局的种子，测试据此算出服务端出的题。
func sessionSeed(t *testing.T, env *gameTestEnv, id any) uint64 {
	t.Helper()
	var session model.GameSession
	require.NoError(t, env.db.First(&session, int(id.(float64))).Error)
	return uint64(session.Seed)
}

func twentyfourHand(t *testing.T, view map[string]any) [4]int {
	t.Helper()
	var hand [4]int
	for i, n := range view["hand"].([]any) {
		hand[i] = int(n.(float64))
	}
	return hand
}

// 24 点走真实接口：算错的算式不记录、版本不变；跳过的题公布一种解法，跳过次数用完就不能再跳；不能撤销重置；
// 解出全部题目过关领奖。
func TestTwentyfourRoundSettlesAfterAllHands(t *testing.T) {
	env := newGameTestEnv(t, puzzleSettings())
	player := env.player(0, gameTestIpA)
	path := "/api/game/twentyfour"
	view := player.ok(http.MethodPost, path+"/start", map[string]any{"difficulty": 1})
	action := func(v map[string]any, fields map[string]any) map[string]any {
		body := map[string]any{"session_id": v["session_id"], "version": v["version"]}
		for key, value := range fields {
			body[key] = value
		}
		return body
	}
	player.fails(http.MethodPost, path+"/action", action(view, map[string]any{"action": "answer", "answer": "1+1"}))
	player.fails(http.MethodPost, path+"/action", action(view, map[string]any{"action": "undo"}))
	player.fails(http.MethodPost, path+"/action", action(view, map[string]any{"action": "reset"}))
	again := player.ok(http.MethodGet, path+"/state", nil)["session"].(map[string]any)
	assert.Equal(t, view["version"], again["version"], "算错不改动对局")

	for i := 0; i < twentyfour.MaxSkips; i++ {
		skipped := twentyfourHand(t, view)
		view = player.ok(http.MethodPost, path+"/action", action(view, map[string]any{"action": "skip"}))
		history := view["history"].([]any)
		last := history[len(history)-1].(map[string]any)
		assert.Equal(t, true, last["skipped"])
		require.NoError(t, twentyfour.Check(last["answer"].(string), skipped), "公布的解法是对的")
	}
	player.fails(http.MethodPost, path+"/action", action(view, map[string]any{"action": "skip"}))

	for view["status"] != model.GameSessionStatusFinished {
		answer := twentyfour.Solve(twentyfourHand(t, view))
		view = player.ok(http.MethodPost, path+"/action", action(view, map[string]any{"action": "answer", "answer": answer}))
	}
	assert.Equal(t, true, view["won"])
	assert.EqualValues(t, twentyfour.HandsPerRound, view["solved"])
	assert.Equal(t, 1.0, view["result"].(map[string]any)["reward"])
	assert.Equal(t, 1.0, player.balance())
}

// 猜数字走真实接口：答案在结束前不公开；格式不对的猜测不占次数；次数用完自动结算、成绩为 0 并公开答案；
// 下一局猜中过关领奖。
func TestBullsRevealsTheSecretOnlyAtTheEnd(t *testing.T) {
	env := newGameTestEnv(t, puzzleSettings())
	player := env.player(0, gameTestIpA)
	path := "/api/game/bulls"
	view := player.ok(http.MethodPost, path+"/start", map[string]any{"difficulty": 1})
	assert.NotContains(t, view, "secret")
	assert.EqualValues(t, 720, view["candidates"], "3 位各不相同的数有 10×9×8 个")
	secret := ""
	if game, err := bulls.New(1, sessionSeed(t, env, view["session_id"])); assert.NoError(t, err) {
		secret = game.Secret
	}
	guess := func(v map[string]any, answer string) map[string]any {
		return map[string]any{"session_id": v["session_id"], "version": v["version"], "action": "answer", "answer": answer}
	}
	player.fails(http.MethodPost, path+"/action", guess(view, "112"))
	player.fails(http.MethodPost, path+"/action", guess(view, "1234"))
	wrong := "012"
	if wrong == secret {
		wrong = "345"
	}
	for i := 0; i < bulls.Levels[1].Guesses; i++ {
		assert.NotContains(t, view, "secret", "第 %d 次之前", i+1)
		view = player.ok(http.MethodPost, path+"/action", guess(view, wrong))
	}
	assert.Equal(t, model.GameSessionStatusFinished, view["status"])
	assert.Equal(t, secret, view["secret"])
	assert.Equal(t, 0.0, view["result"].(map[string]any)["score"])

	view = player.ok(http.MethodPost, path+"/start", map[string]any{"difficulty": 1})
	game, err := bulls.New(1, sessionSeed(t, env, view["session_id"]))
	require.NoError(t, err)
	view = player.ok(http.MethodPost, path+"/action", guess(view, game.Secret))
	assert.Equal(t, true, view["won"])
	assert.Equal(t, 1.0, view["result"].(map[string]any)["reward"])
	restored := player.ok(http.MethodGet, fmt.Sprintf("%s/state?session_id=%v", path, view["session_id"]), nil)["session"].(map[string]any)
	assert.Equal(t, game.Secret, restored["secret"])
}

// 数织走真实接口：提示公开、答案不公开；照着推得出的答案涂完就过关，可以撤销。
func TestNonogramSolvesByClues(t *testing.T) {
	env := newGameTestEnv(t, puzzleSettings())
	player := env.player(0, gameTestIpA)
	path := "/api/game/nonogram"
	view := player.ok(http.MethodPost, path+"/start", map[string]any{"difficulty": 1})
	assert.EqualValues(t, nonogram.Sizes[1], view["size"])
	assert.Len(t, view["rows"], nonogram.Sizes[1])
	game, err := nonogram.New(1, sessionSeed(t, env, view["session_id"]))
	require.NoError(t, err)
	move := func(v map[string]any, fields map[string]any) map[string]any {
		body := map[string]any{"session_id": v["session_id"], "version": v["version"]}
		for key, value := range fields {
			body[key] = value
		}
		return body
	}
	view = player.ok(http.MethodPost, path+"/action", move(view, map[string]any{"action": "move", "cell": 0}))
	view = player.ok(http.MethodPost, path+"/action", move(view, map[string]any{"action": "undo"}))
	assert.EqualValues(t, 0, view["moves"])

	solution := solveNonogramForTest(t, game)
	for cell, on := range solution {
		if !on || view["status"] == model.GameSessionStatusFinished {
			continue
		}
		view = player.ok(http.MethodPost, path+"/action", move(view, map[string]any{"action": "move", "cell": cell}))
	}
	assert.Equal(t, true, view["won"])
	assert.Equal(t, 1.0, view["result"].(map[string]any)["reward"])
}

// solveNonogramForTest 按提示穷举每一行的涂法、逐列核对，找出一个解(5×5 很快)。
func solveNonogramForTest(t *testing.T, game *nonogram.Game) []bool {
	t.Helper()
	size := game.Size
	grid := make([]bool, size*size)
	var try func(r int) bool
	try = func(r int) bool {
		if r == size {
			check := &nonogram.Game{Puzzle: game.Puzzle, Filled: grid}
			return check.Solved()
		}
		for mask := 0; mask < 1<<size; mask++ {
			for c := 0; c < size; c++ {
				grid[r*size+c] = mask&(1<<c) != 0
			}
			line := grid[r*size : (r+1)*size]
			if fmt.Sprint(runsForTest(line)) != fmt.Sprint(game.Rows[r]) {
				continue
			}
			if try(r + 1) {
				return true
			}
		}
		return false
	}
	require.True(t, try(0))
	return grid
}

func runsForTest(line []bool) []int {
	result := []int{}
	count := 0
	for _, on := range line {
		if on {
			count++
		} else if count > 0 {
			result = append(result, count)
			count = 0
		}
	}
	if count > 0 {
		result = append(result, count)
	}
	return result
}
