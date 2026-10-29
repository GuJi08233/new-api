package controller

import (
	"math/rand/v2"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/pkg/dots"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 人机点格棋走真实接口：玩家每连一条边，AI 在同一个请求里连到又轮到玩家为止，局面和本地按双方着法重放的一致；
// 连过的边不能再连。玩家用困难难度对简单难度的 AI，赢下后领奖。
func TestDotsAiGameSettles(t *testing.T) {
	env := newGameTestEnv(t, gameSetting(func(s *operation_setting.GameSetting) {
		s.Dots = operation_setting.PvpRule{Enabled: true, MaxStake: 100, RakePercent: 5, MoveSeconds: 30, Ai: boardAiRule()}
	}))
	fixBoardAiRandom(t, 11)
	player := env.player(0, gameTestIpA)

	session := player.ok(http.MethodPost, "/api/game/dots/ai/start", map[string]any{"level": 1})["session"].(map[string]any)
	rng := rand.New(rand.NewPCG(2, 2))
	var result map[string]any
	for result == nil {
		state := session["state"].(map[string]any)
		game := replayDotsView(t, state)
		edge, err := dots.BestMove(game, 3, rng)
		require.NoError(t, err)
		if state["moves"].(float64) > 0 && state["last"].(float64) >= 0 {
			player.fails(http.MethodPost, "/api/game/dots/ai/move", map[string]any{
				"session_id": session["session_id"], "version": session["version"], "move": map[string]any{"edge": state["last"]},
			})
		}
		reply := player.ok(http.MethodPost, "/api/game/dots/ai/move", map[string]any{
			"session_id": session["session_id"], "version": session["version"], "move": map[string]any{"edge": edge},
		})
		session = reply["session"].(map[string]any)
		result, _ = reply["result"].(map[string]any)
	}
	state := session["state"].(map[string]any)
	scores := state["scores"].([]any)
	assert.Equal(t, true, session["over"])
	assert.EqualValues(t, dots.Boxes, scores[0].(float64)+scores[1].(float64))
	assert.EqualValues(t, 0, session["winner"], "困难难度赢下简单难度")
	assert.Equal(t, 1.0, result["reward"])
}

// replayDotsView 按视图里连上的边还原一局，供本地 AI 选下一步。边的先后不影响之后的选择(局面只由连过的边决定)，
// 轮到谁由视图给出。
func replayDotsView(t *testing.T, state map[string]any) *dots.Game {
	t.Helper()
	edges := state["edges"].(string)
	g := dots.New()
	for e := 0; e < dots.Edges; e++ {
		if edges[e] == '1' {
			require.NoError(t, g.Play(e))
		}
	}
	return g
}
