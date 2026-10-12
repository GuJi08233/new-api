package controller

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/landlord"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// aiFirstPvpEngine 固定由 AI 先走，验证开局时电脑的动作不能算作玩家的游戏时长。
type aiFirstPvpEngine struct{ pvpEngine }

func (engine aiFirstPvpEngine) New(int) (pvpState, error) {
	return engine.pvpEngine.New(1)
}

func TestPvpAiResignCreditsOnlyVerifiedPlayTime(t *testing.T) {
	require.NoError(t, i18n.Init())
	previousShuffle := landlord.Shuffle
	landlord.Shuffle = func([]int) error { return nil }
	t.Cleanup(func() { landlord.Shuffle = previousShuffle })
	for _, key := range []string{gameGomoku, gameConnect4, gameReversi, gameXiangqi, gameLandlord} {
		for _, playMove := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/played=%t", key, playMove), func(t *testing.T) {
				rule := boardAiRule()
				rule.MinSecondsPerCoin = model.GameActionGapSeconds
				env := newGameTestEnv(t, gameSetting(func(setting *operation_setting.GameSetting) {
					pvp := operation_setting.PvpRule{Enabled: true, MaxStake: 100, MoveSeconds: 60, Ai: rule}
					switch key {
					case gameGomoku:
						setting.Gomoku = pvp
					case gameConnect4:
						setting.Connect4 = pvp
					case gameReversi:
						setting.Reversi = pvp
					case gameXiangqi:
						setting.Xiangqi = pvp
					case gameLandlord:
						setting.Landlord = pvp
					}
				}))
				game := pvpGames[key]
				previousEngine := game.engine
				game.engine = aiFirstPvpEngine{previousEngine}
				t.Cleanup(func() { game.engine = previousEngine })
				fixBoardAiRandom(t, 7)
				player := env.player(0, gameTestIpA)
				coin, err := model.WholeGameCoins(1)
				require.NoError(t, err)
				// 模拟此前已经达标但时长不足的一局，由真实结算生成待发奖励。
				prior, err := model.CreateGameSession(player.user.Id, pvpAiGame(key), 0, "")
				require.NoError(t, err)
				pending, err := model.FinishGameSession(prior, model.GameSettlement{
					Score: 1, Reward: coin, SecondsPerCoin: rule.MinSecondsPerCoin, DailyRewards: rule.DailyRewards,
					Day: gameCenterDay(), Ip: gameRewardIpKey(gameTestIpA),
				})
				require.NoError(t, err)
				require.Equal(t, coin, pending.PendingReward)
				require.Zero(t, player.balance())
				path := "/api/game/" + key + "/ai"
				started := player.ok(http.MethodPost, path+"/start", map[string]any{"level": 1})["session"].(map[string]any)
				require.Greater(t, jsonInt(t, started["moves"]), 0, "AI 确实已经先走")
				require.EqualValues(t, 0, started["version"], "玩家尚未提交合法操作")
				id := jsonInt(t, started["session_id"])
				longAgo := common.GetTimestamp() - 10*model.GameActionGapSeconds
				require.NoError(t, env.db.Model(&model.GameSession{}).Where("id = ?", id).
					Updates(map[string]any{"created_at": longAgo, "played_at": longAgo}).Error)
				player.fails(http.MethodPost, path+"/move", map[string]any{
					"session_id": id, "version": started["version"], "move": map[string]any{},
				})
				stored, err := model.GetUserGameSession(player.user.Id, pvpAiGame(key), id)
				require.NoError(t, err)
				require.Zero(t, stored.PlaySeconds, "AI 先手和非法操作均不计玩家时长")
				wantSeconds, wantReward, wantPending := 0, 0, 1
				if playMove {
					var state pvpAiState
					require.NoError(t, common.UnmarshalJsonStr(stored.State, &state))
					move, err := game.engine.AiMove(state.Board, 0, 1)
					require.NoError(t, err)
					moved := player.ok(http.MethodPost, path+"/move", map[string]any{
						"session_id": id, "version": stored.Version, "move": move,
					})["session"].(map[string]any)
					require.Equal(t, model.GameSessionStatusPlaying, moved["status"])
					stored, err = model.GetUserGameSession(player.user.Id, pvpAiGame(key), id)
					require.NoError(t, err)
					require.Equal(t, model.GameActionGapSeconds, stored.PlaySeconds)
					// 合法操作之后再次挂机，认输不能再追加这段间隔。
					require.NoError(t, env.db.Model(&model.GameSession{}).Where("id = ?", id).Update("played_at", longAgo).Error)
					wantSeconds, wantReward, wantPending = model.GameActionGapSeconds, 1, 0
				}
				request := map[string]any{"session_id": id}
				finished := player.ok(http.MethodPost, path+"/resign", request)
				result := finished["result"].(map[string]any)
				assert.EqualValues(t, 0, result["score"])
				assert.EqualValues(t, wantReward, result["reward"])
				progress := result["progress"].(map[string]any)
				assert.EqualValues(t, wantSeconds, progress["seconds_today"])
				assert.EqualValues(t, wantPending, progress["pending_reward"])
				assert.EqualValues(t, wantReward, player.balance())
				repeated := player.ok(http.MethodPost, path+"/resign", request)
				assert.Equal(t, result, repeated["result"], "重复认输不累计时长或重复派彩")
				assert.EqualValues(t, wantReward, player.balance())
			})
		}
	}
}

func TestPvpAiWinningMoveIncludesFinalThinkingTime(t *testing.T) {
	rule := boardAiRule()
	rule.MinSecondsPerCoin = model.GameActionGapSeconds
	env := newGameTestEnv(t, gameSetting(func(setting *operation_setting.GameSetting) {
		setting.Gomoku = operation_setting.PvpRule{Enabled: true, Ai: rule}
	}))
	player := env.player(0, gameTestIpA)
	// 玩家执黑已有横向四子，下一手连五结束；该步仍须计入真实思考时间。
	board, err := common.Marshal(gomokuState{Black: 0, Moves: []int{0, 15, 1, 16, 2, 17, 3, 18}})
	require.NoError(t, err)
	state, err := common.Marshal(pvpAiState{Level: 1, Board: board, ToMove: 0, Winner: -1, Moves: 8})
	require.NoError(t, err)
	session, err := model.CreateGameSession(player.user.Id, pvpAiGame(gameGomoku), 0, string(state))
	require.NoError(t, err)
	longAgo := common.GetTimestamp() - 2*model.GameActionGapSeconds
	require.NoError(t, env.db.Model(&model.GameSession{}).Where("id = ?", session.Id).
		Updates(map[string]any{"created_at": longAgo, "played_at": longAgo}).Error)
	request := map[string]any{"session_id": session.Id, "version": session.Version, "move": map[string]any{"x": 4, "y": 0}}
	finished := player.ok(http.MethodPost, "/api/game/gomoku/ai/move", request)
	result := finished["result"].(map[string]any)
	assert.EqualValues(t, 1, result["score"])
	assert.EqualValues(t, 1, result["reward"])
	assert.EqualValues(t, model.GameActionGapSeconds, result["progress"].(map[string]any)["seconds_today"])
	assert.Equal(t, 1.0, player.balance())
	repeated := player.ok(http.MethodPost, "/api/game/gomoku/ai/move", request)
	assert.Equal(t, result, repeated["result"])
	assert.Equal(t, 1.0, player.balance())
}
