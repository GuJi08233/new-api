package controller

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/puzzle15"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func puzzle15Setting(rule operation_setting.SkillRule) *operation_setting.GameSetting {
	return gameSetting(func(setting *operation_setting.GameSetting) { setting.Puzzle15 = rule })
}

// startPuzzle15Session 直接建一局 5×5 的数字华容道：从排好的盘面让空格先上后左各走一步，再走 "RD" 就能排好。
// 开局时间往前拨 startedAgo 秒。
func startPuzzle15Session(t *testing.T, env *gameTestEnv, userId int, startedAgo int64) *model.GameSession {
	t.Helper()
	solved := make([]int, 25)
	for index := range solved[:24] {
		solved[index] = index + 1
	}
	board, err := (&puzzle15.Game{Size: 5, Board: solved}).Replay("UL")
	require.NoError(t, err)
	state, err := common.Marshal(&puzzle15.Game{Difficulty: 3, Size: 5, Board: board})
	require.NoError(t, err)
	session, err := model.CreateGameSession(userId, gamePuzzle15, 0, string(state))
	require.NoError(t, err)
	require.NoError(t, env.db.Model(session).Update("created_at", session.CreatedAt-startedAgo).Error)
	return session
}

// 开局给出一个可解、还没排好的盘面，与服务端保存的开局盘面一致；刷新页面后拿到的也是它。
func TestPuzzle15StartDealsASolvableBoard(t *testing.T) {
	env := newGameTestEnv(t, puzzle15Setting(operation_setting.SkillRule{
		Enabled: true, DailyRewards: 3, Tiers: []operation_setting.RewardTier{{Score: 1, Coins: 1}},
	}))
	player := env.player(0, gameTestIpA)

	info := player.ok(http.MethodGet, "/api/game/puzzle15", nil)
	assert.Len(t, info["levels"], 3)
	assert.EqualValues(t, puzzle15.MaxMoves, info["max_moves"])
	assert.EqualValues(t, puzzle15.MaxMovesPerSecond, info["max_moves_per_second"])
	player.fails(http.MethodPost, "/api/game/puzzle15/start", map[string]any{"difficulty": 4})

	started := player.ok(http.MethodPost, "/api/game/puzzle15/start", map[string]any{"difficulty": 2})
	assert.EqualValues(t, 2, started["difficulty"])
	assert.EqualValues(t, 4, started["size"])
	board := make([]int, 0, 16)
	for _, tile := range started["board"].([]any) {
		board = append(board, int(tile.(float64)))
	}
	sessionId := int(started["session_id"].(float64))
	session, err := model.GetUserGameSession(player.user.Id, gamePuzzle15, sessionId)
	require.NoError(t, err)
	var stored puzzle15.Game
	require.NoError(t, common.UnmarshalJsonStr(session.State, &stored))
	assert.Equal(t, stored.Board, board)
	assert.Equal(t, []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}, slices.Sorted(slices.Values(board)))
	assert.True(t, puzzle15.Solvable(4, board))
	assert.False(t, puzzle15.Solved(board))

	restored := player.ok(http.MethodGet, "/api/game/puzzle15/state", nil)["session"].(map[string]any)
	assert.EqualValues(t, sessionId, restored["session_id"])
	assert.Equal(t, started["board"], restored["board"])
}

// 结算时从开局盘面重放上传的步骤：有一步走出盘面、有不认识的字符、走完没排好都不结算；步数不能快过人手，开局 10 秒(请求时
// 可能已经过了 11 秒)时最多 (10+2)×15 = 180 到 (11+2)×15 = 195 步。排好了按难度领这一档的奖励，重复提交返回第一次结算的结果；
// 同样的步数在开局更久之后就不算快。
func TestPuzzle15FinishReplaysTheMoves(t *testing.T) {
	require.NoError(t, i18n.Init())
	env := newGameTestEnv(t, puzzle15Setting(operation_setting.SkillRule{
		Enabled: true, DailyRewards: 3, Tiers: []operation_setting.RewardTier{{Score: 2, Coins: 1}, {Score: 3, Coins: 2}},
	}))
	player := env.player(0, gameTestIpA)
	invalid := i18n.Translate(i18n.DefaultLang, i18n.MsgGameReplayInvalid)
	// 排好之后空格左右来回走，走完仍是排好的。
	solvedIn := func(moves int) string { return "RD" + strings.Repeat("LR", (moves-2)/2) }

	session := startPuzzle15Session(t, env, player.user.Id, 10)
	for name, moves := range map[string]string{
		"a move off the board":         "RDD",
		"an unknown character":         "RDX",
		"not solved at the end":        "R",
		"no moves at all":              "",
		"faster than a human can move": solvedIn(198),
	} {
		message := player.fails(http.MethodPost, "/api/game/puzzle15/finish", map[string]any{"session_id": session.Id, "moves": moves})
		assert.Equal(t, invalid, message, name)
	}
	assert.NotNil(t, player.ok(http.MethodGet, "/api/game/puzzle15/state", nil)["session"], "没排好不结算，这局还在")

	result := player.ok(http.MethodPost, "/api/game/puzzle15/finish", map[string]any{"session_id": session.Id, "moves": solvedIn(180)})
	assert.EqualValues(t, 3, result["score"], "成绩是排好的难度")
	assert.EqualValues(t, 2, result["reward"])
	assert.Equal(t, map[string]any{"difficulty": 3.0, "moves": 180.0}, result["detail"])
	again := player.ok(http.MethodPost, "/api/game/puzzle15/finish", map[string]any{"session_id": session.Id, "moves": "RD"})
	assert.Equal(t, result, again, "重复提交返回第一次结算的结果")
	assert.Nil(t, player.ok(http.MethodGet, "/api/game/puzzle15/state", nil)["session"])

	older := startPuzzle15Session(t, env, player.user.Id, 20)
	result = player.ok(http.MethodPost, "/api/game/puzzle15/finish", map[string]any{"session_id": older.Id, "moves": solvedIn(198)})
	assert.EqualValues(t, 3, result["score"], "开局 20 秒时 198 步不算快")
	assert.EqualValues(t, 2, result["progress"].(map[string]any)["rewards_today"])
	assert.Equal(t, 4, player.balance())
}
