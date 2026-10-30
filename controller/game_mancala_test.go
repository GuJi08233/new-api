package controller

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/pkg/mancala"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 人机播棋走真实接口：玩家每走一步，AI 在同一个请求里走到又轮到玩家为止；空坑不能选。下到结束时子数守恒，按输赢结算。
func TestMancalaAiGameSettles(t *testing.T) {
	env := newGameTestEnv(t, gameSetting(func(s *operation_setting.GameSetting) {
		s.Mancala = operation_setting.PvpRule{Enabled: true, MaxStake: 100, RakePercent: 5, MoveSeconds: 30, Ai: boardAiRule()}
	}))
	fixBoardAiRandom(t, 5)
	player := env.player(0, gameTestIpA)

	session := player.ok(http.MethodPost, "/api/game/mancala/ai/start", map[string]any{"level": 1})["session"].(map[string]any)
	var result map[string]any
	rejectedEmpty := false
	for result == nil {
		state := session["state"].(map[string]any)
		board := state["board"].([]any)
		// 玩家坐座位 0：先走时是 0..5 号坑，后走时是 7..12 号坑。
		offset := 0
		if state["first"].(float64) != 0 {
			offset = mancala.Pits + 1
		}
		pit := -1
		for i := mancala.Pits - 1; i >= 0; i-- {
			if board[offset+i].(float64) > 0 {
				pit = i
				break
			}
		}
		require.GreaterOrEqual(t, pit, 0)
		for i := 0; i < mancala.Pits && !rejectedEmpty; i++ {
			if board[offset+i].(float64) == 0 {
				player.fails(http.MethodPost, "/api/game/mancala/ai/move", map[string]any{
					"session_id": session["session_id"], "version": session["version"], "move": map[string]any{"pit": i},
				})
				rejectedEmpty = true
			}
		}
		reply := player.ok(http.MethodPost, "/api/game/mancala/ai/move", map[string]any{
			"session_id": session["session_id"], "version": session["version"], "move": map[string]any{"pit": pit},
		})
		session = reply["session"].(map[string]any)
		result, _ = reply["result"].(map[string]any)
	}
	assert.True(t, rejectedEmpty)
	state := session["state"].(map[string]any)
	scores := state["scores"].([]any)
	assert.Equal(t, true, session["over"])
	assert.EqualValues(t, mancala.Total, scores[0].(float64)+scores[1].(float64))
	if session["winner"].(float64) == 0 {
		assert.Equal(t, 1.0, result["reward"])
	} else {
		assert.Equal(t, 0.0, result["reward"])
	}
}
