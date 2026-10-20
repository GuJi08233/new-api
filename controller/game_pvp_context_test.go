package controller

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type cancelPvpSearchEngine struct {
	pvpEngine
	cancel context.CancelFunc
	called bool
}

func (e *cancelPvpSearchEngine) AiMoveContext(ctx context.Context, _ []byte, _, _ int) (json.RawMessage, error) {
	e.called = true
	e.cancel()
	return json.RawMessage(`{"from":[0,0],"to":[0,1]}`), nil
}

func TestPvpAiSearchDiscardsCancelledMove(t *testing.T) {
	for _, beforeSearch := range []bool{true, false} {
		name := "during search"
		if beforeSearch {
			name = "before search"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			engine := &cancelPvpSearchEngine{cancel: cancel}
			if beforeSearch {
				cancel()
			}
			move, err := pvpAiSearchMove(ctx, engine, nil, 0, 2)
			require.ErrorIs(t, err, context.Canceled)
			assert.Nil(t, move, "取消后不能把已算出的着法继续落盘")
			assert.Equal(t, !beforeSearch, engine.called)
		})
	}
}

type cancelXiangqiHintEngine struct {
	xiangqiEngine
	cancel context.CancelFunc
}

func (e cancelXiangqiHintEngine) HintContext(_ context.Context, data []byte, seat int) (pvpHint, error) {
	answer, err := e.xiangqiEngine.Hint(data, seat)
	e.cancel()
	return answer, err
}

func TestCancelledXiangqiHintDoesNotChargeOrUpdateSession(t *testing.T) {
	env := newGameTestEnv(t, xiangqiSetting())
	player := env.player(2, gameTestIpA)
	started := player.ok(http.MethodPost, "/api/game/xiangqi/ai/start", map[string]any{"level": 1})
	session := started["session"].(map[string]any)
	var stored model.GameSession
	require.NoError(t, env.db.First(&stored, session["session_id"]).Error)
	var state pvpAiState
	require.NoError(t, common.UnmarshalJsonStr(stored.State, &state))
	board, err := loadXiangqiState(state.Board)
	require.NoError(t, err)
	board.Hints = gameHintFreePlays
	state.Board, err = common.Marshal(board)
	require.NoError(t, err)
	data, err := common.Marshal(state)
	require.NoError(t, err)
	stored.State = string(data)
	require.NoError(t, env.db.Model(&stored).Update("state", stored.State).Error)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	game := *pvpGames[gameXiangqi]
	game.engine = cancelXiangqiHintEngine{cancel: cancel}
	env.router.POST("/cancel-hint", middleware.UserAuth(), func(c *gin.Context) {
		c.Request = c.Request.WithContext(ctx)
		hintPvpAi(c, &game)
	})
	player.send(http.MethodPost, "/cancel-hint", map[string]any{
		"session_id": stored.Id, "version": stored.Version,
	})
	require.ErrorIs(t, ctx.Err(), context.Canceled, "请求应在分析完成、收费之前取消")
	var after model.GameSession
	require.NoError(t, env.db.First(&after, stored.Id).Error)
	assert.Equal(t, stored.State, after.State, "取消的提示不能消耗次数或改动局面")
	assert.Equal(t, stored.Version, after.Version)
	assert.Equal(t, 2.0, player.balance())
	var fees int64
	require.NoError(t, env.db.Model(&model.GameCoinLog{}).Where("user_id = ? AND remark = ?", player.user.Id, "xiangqi_ai:hint").Count(&fees).Error)
	assert.Zero(t, fees)
}
