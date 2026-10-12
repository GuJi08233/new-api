package controller

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/sudoku"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sudokuSetting(rule operation_setting.SkillRule) *operation_setting.GameSetting {
	return gameSetting(func(setting *operation_setting.GameSetting) { setting.Sudoku = rule })
}

// startSudokuSession 直接建一局用固定种子出题的数独，并把开局时间往前拨 startedAgo 秒；返回的题目带着谜底。
func startSudokuSession(t *testing.T, env *gameTestEnv, userId int, difficulty int, startedAgo int64) (*model.GameSession, *sudoku.Puzzle) {
	t.Helper()
	puzzle, err := sudoku.Generate(difficulty, 1)
	require.NoError(t, err)
	state, err := common.Marshal(puzzle)
	require.NoError(t, err)
	session, err := model.CreateGameSession(userId, gameSudoku, 1, string(state))
	require.NoError(t, err)
	require.NoError(t, env.db.Model(session).Update("created_at", session.CreatedAt-startedAgo).Error)
	return session, puzzle
}

// 开局给出一道唯一解的题，谜底只存在服务端：开局与刷新页面的响应里都只有题目。
func TestSudokuKeepsTheSolutionOnTheServer(t *testing.T) {
	env := newGameTestEnv(t, sudokuSetting(operation_setting.SkillRule{
		Enabled: true, DailyRewards: 3, Tiers: []operation_setting.RewardTier{{Score: 1, Coins: 1}},
	}))
	player := env.player(0, gameTestIpA)

	info := player.ok(http.MethodGet, "/api/game/sudoku", nil)
	levels := info["levels"].([]any)
	require.Len(t, levels, 3)
	assert.Equal(t, map[string]any{"difficulty": 3.0, "min_givens": 24.0, "max_givens": 28.0}, levels[2])
	player.fails(http.MethodPost, "/api/game/sudoku/start", map[string]any{"difficulty": 4})

	recorder := player.send(http.MethodPost, "/api/game/sudoku/start", map[string]any{"difficulty": 3})
	var response map[string]any
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, true, response["success"], response["message"])
	started := response["data"].(map[string]any)
	assert.EqualValues(t, 3, started["difficulty"])
	sessionId := int(started["session_id"].(float64))
	session, err := model.GetUserGameSession(player.user.Id, gameSudoku, sessionId)
	require.NoError(t, err)
	var stored sudoku.Puzzle
	require.NoError(t, common.UnmarshalJsonStr(session.State, &stored))
	assert.Equal(t, stored.Puzzle, started["puzzle"])
	assert.True(t, stored.Givens() >= 24 && stored.Givens() <= 28, "困难难度给出 24-28 个数")
	assert.True(t, sudoku.Check(stored.Puzzle, stored.Solution))
	assert.NotContains(t, recorder.Body.String(), stored.Solution)
	assert.NotContains(t, recorder.Body.String(), "solution")

	recorder = player.send(http.MethodGet, "/api/game/sudoku/state", nil)
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	restored := response["data"].(map[string]any)["session"].(map[string]any)
	assert.EqualValues(t, sessionId, restored["session_id"])
	assert.Equal(t, stored.Puzzle, restored["puzzle"])
	assert.EqualValues(t, 3, restored["difficulty"])
	assert.NotContains(t, recorder.Body.String(), stored.Solution)
}

// 交卷时答卷不对(没填完、有一行一列重复、改了给出的数字)只提示记录无效、不结算，玩家接着改了还能再交；交对了按解出的难度
// 领这一档的奖励，时长是开局以来的秒数。重复交卷返回第一次结算的结果，别人的对局交不了。
func TestSudokuFinishChecksTheAnswer(t *testing.T) {
	require.NoError(t, i18n.Init())
	env := newGameTestEnv(t, sudokuSetting(operation_setting.SkillRule{
		Enabled: true, DailyRewards: 3, Tiers: []operation_setting.RewardTier{{Score: 1, Coins: 1}, {Score: 3, Coins: 3}}, MinSecondsPerCoin: 90,
	}))
	player := env.player(0, gameTestIpA)
	other := env.player(0, gameTestIpB)
	session, puzzle := startSudokuSession(t, env, player.user.Id, 3, 200)

	// 找两个题目里是空格、谜底不同的格子对调：填满了，但行列里有重复。
	blank := []int{}
	for index := range puzzle.Puzzle {
		if puzzle.Puzzle[index] == '0' {
			blank = append(blank, index)
		}
	}
	first, second := blank[0], blank[1]
	for _, index := range blank[1:] {
		if puzzle.Solution[index] != puzzle.Solution[first] {
			second = index
			break
		}
	}
	swapped := []byte(puzzle.Solution)
	swapped[first], swapped[second] = swapped[second], swapped[first]
	given := 0
	for puzzle.Puzzle[given] == '0' {
		given++
	}
	changedGiven := []byte(puzzle.Solution)
	changedGiven[given] = '1' + (changedGiven[given]-'1'+1)%9

	for name, grid := range map[string]string{
		"the puzzle without answers": puzzle.Puzzle,
		"two answers swapped":        string(swapped),
		"a given changed":            string(changedGiven),
		"too short":                  puzzle.Solution[:80],
	} {
		message := player.fails(http.MethodPost, "/api/game/sudoku/finish", map[string]any{"session_id": session.Id, "grid": grid})
		assert.Equal(t, i18n.Translate(i18n.DefaultLang, i18n.MsgGameReplayInvalid), message, name)
	}
	message := other.fails(http.MethodPost, "/api/game/sudoku/finish", map[string]any{"session_id": session.Id, "grid": puzzle.Solution})
	assert.Equal(t, i18n.Translate(i18n.DefaultLang, i18n.MsgGameSessionNotFound), message)
	assert.NotNil(t, player.ok(http.MethodGet, "/api/game/sudoku/state", nil)["session"], "答卷不对不结算，这局还在")

	result := player.ok(http.MethodPost, "/api/game/sudoku/finish", map[string]any{"session_id": session.Id, "grid": puzzle.Solution})
	assert.EqualValues(t, 3, result["score"], "成绩是解出的难度")
	assert.EqualValues(t, 2, result["reward"], "200 秒只够 2 个游戏币")
	assert.Equal(t, model.GameRewardLimitedByTime, result["limited_by"])
	assert.EqualValues(t, 2, result["balance"])
	assert.Equal(t, map[string]any{"difficulty": 3.0, "givens": float64(puzzle.Givens()), "solved": true}, result["detail"])

	again := player.ok(http.MethodPost, "/api/game/sudoku/finish", map[string]any{"session_id": session.Id, "grid": puzzle.Solution})
	assert.Equal(t, result, again, "重复交卷返回第一次结算的结果")
	assert.Nil(t, player.ok(http.MethodGet, "/api/game/sudoku/state", nil)["session"])
	assert.Equal(t, 2, player.balance())
}

// 放弃按成绩 0 结算、什么都不发；放弃之后再交对的答卷，拿到的仍是放弃时的结果。
func TestSudokuAbandonSettlesNothing(t *testing.T) {
	env := newGameTestEnv(t, sudokuSetting(operation_setting.SkillRule{
		Enabled: true, DailyRewards: 3, Tiers: []operation_setting.RewardTier{{Score: 1, Coins: 1}},
	}))
	player := env.player(0, gameTestIpA)
	session, puzzle := startSudokuSession(t, env, player.user.Id, 1, 200)

	abandoned := player.ok(http.MethodPost, "/api/game/sudoku/abandon", map[string]any{"session_id": session.Id})
	assert.EqualValues(t, 0, abandoned["score"])
	assert.EqualValues(t, 0, abandoned["reward"])
	assert.Equal(t, map[string]any{"difficulty": 1.0, "givens": float64(puzzle.Givens()), "solved": false}, abandoned["detail"])
	assert.EqualValues(t, 1, abandoned["progress"].(map[string]any)["plays_today"])

	late := player.ok(http.MethodPost, "/api/game/sudoku/finish", map[string]any{"session_id": session.Id, "grid": puzzle.Solution})
	assert.Equal(t, abandoned, late)
	assert.Equal(t, 0, player.balance())
}
