package controller

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/doufa"
	"github.com/QuantumNous/new-api/pkg/wendao"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// wendaoEconomyRound 是结算边界夹具：真实 HTTP 开局后直接布置达标进度，不冒充完整实打首领或大比。
type wendaoEconomyRound struct {
	game     string
	path     string
	request  map[string]any
	loot     wendao.Cost
	crystals int
	score    int
}

func startWendaoEconomyRound(t *testing.T, env *gameTestEnv, player *gamePlayer, mode string) wendaoEconomyRound {
	t.Helper()
	if mode == "rift" {
		started := player.ok(http.MethodPost, riftBase+"/start", map[string]any{"class": "mage"})["session"].(map[string]any)
		started["version"] = editRift(t, player.user.Id, func(run *doufa.Run) {
			run.Floor, run.Step, run.Cleared = 2, 3, 8
		})
		return wendaoEconomyRound{game: riftGame, path: riftBase + "/act", score: 1, crystals: 28,
			loot:    wendao.Cost{Stone: 246, Herb: 90, Ore: 84},
			request: map[string]any{"action": "abandon", "session_id": started["session_id"], "version": started["version"]}}
	}
	path := "/api/game/wendao/sect/tournament"
	save, err := loadWendaoSave(player.user.Id)
	require.NoError(t, err)
	started := player.ok(http.MethodPost, path+"/start", map[string]any{"team": []int{save.Sect.Disciples[0].Id}})
	editSectTournament(t, env, started["session_id"], func(tour *wendao.Tournament) {
		tour.Wins, tour.Round = 3, 3
	})
	return wendaoEconomyRound{game: sectTournamentGame, path: path + "/action", score: 3,
		loot: wendao.Cost{Stone: 120, Rep: 24}, request: puzzleAction(started, map[string]any{"action": "abandon"})}
}

func (round wendaoEconomyRound) settle(player *gamePlayer) map[string]any {
	data := player.ok(http.MethodPost, round.path, round.request)
	if data["session"] != nil {
		data = data["session"].(map[string]any)
	}
	if result, ok := data["result"].(map[string]any); ok {
		return result
	}
	return data // 已结算场次重发返回统一结算视图。
}

// 初始关闭和开局后关闭都以结算时配置为准：不新增币资格，养成资源照常到账，恢复配置也不会补发旧场次。
func TestWendaoEconomyZeroDailyRewardsPreservesGameplayAndIdempotency(t *testing.T) {
	for _, mode := range []string{"rift", "tournament"} {
		for _, disableDuringRun := range []bool{false, true} {
			name := mode + "/initially_zero"
			if disableDuringRun {
				name = mode + "/disabled_during_run"
			}
			t.Run(name, func(t *testing.T) {
				setting := gameSetting(func(s *operation_setting.GameSetting) {
					s.Wendao, s.WendaoTournament = puzzleRule(), puzzleRule()
					s.WendaoTournament.Tiers = []operation_setting.RewardTier{{Score: 3, Coins: 1}}
					if !disableDuringRun {
						s.Wendao.DailyRewards, s.WendaoTournament.DailyRewards = 0, 0
					}
				})
				env := newGameTestEnv(t, setting)
				player := env.player(0, gameTestIpA)
				foundTestSect(t, player)
				round := startWendaoEconomyRound(t, env, player, mode)
				before, err := loadWendaoSave(player.user.Id)
				require.NoError(t, err)
				if disableDuringRun {
					paused := *setting
					paused.Wendao.DailyRewards, paused.WendaoTournament.DailyRewards = 0, 0
					operation_setting.SetGameSettingForTest(&paused)
				}
				result := round.settle(player)
				assert.EqualValues(t, round.score, result["score"])
				assert.EqualValues(t, 0, result["reward"])
				assert.Equal(t, model.GameRewardLimitedByDaily, result["limited_by"])
				assert.Equal(t, 0.0, player.balance())
				progress, err := model.GetGameRewardProgress(player.user.Id, gameRewardIpKey(player.ip), round.game, gameCenterDay())
				require.NoError(t, err)
				assert.Equal(t, 1, progress.Plays)
				assert.Zero(t, progress.Rewards)
				assert.Zero(t, progress.PendingReward)
				after, err := loadWendaoSave(player.user.Id)
				require.NoError(t, err)
				assert.Equal(t, before.Sect.Res.Stone+round.loot.Stone, after.Sect.Res.Stone)
				assert.Equal(t, before.Sect.Res.Herb+round.loot.Herb, after.Sect.Res.Herb)
				assert.Equal(t, before.Sect.Res.Ore+round.loot.Ore, after.Sect.Res.Ore)
				assert.Equal(t, before.Sect.Res.Rep+round.loot.Rep, after.Sect.Res.Rep)
				assert.Equal(t, before.Rift.Crystals+round.crystals, after.Rift.Crystals)
				if mode == "rift" {
					assert.Equal(t, before.Rift.Runs+1, after.Rift.Runs)
					assert.Equal(t, 1, after.AbyssLoot.Runs)
				} else {
					assert.Equal(t, before.Sect.Stats.Tournaments+1, after.Sect.Stats.Tournaments)
					assert.Equal(t, before.Sect.Pills[wendao.PillHeal]+2, after.Sect.Pills[wendao.PillHeal], "unused carried pills return without a coin reward")
				}
				persisted, err := model.GetGameSave(player.user.Id, wendao.SaveKey)
				require.NoError(t, err)
				reenabled := *setting
				reenabled.Wendao.DailyRewards, reenabled.WendaoTournament.DailyRewards = 1, 1
				operation_setting.SetGameSettingForTest(&reenabled)
				again := round.settle(player)
				assert.Equal(t, result["session_id"], again["session_id"])
				assert.EqualValues(t, 0, again["reward"], "restoring the rule cannot reprice a finished session")
				assert.Equal(t, model.GameRewardLimitedByDaily, again["limited_by"])
				assert.Equal(t, 0.0, player.balance())
				repeated, err := model.GetGameSave(player.user.Id, wendao.SaveKey)
				require.NoError(t, err)
				assert.Equal(t, persisted, repeated, "the final request cannot credit resources or crystals a second time")
				var logs int64
				require.NoError(t, env.db.Model(&model.GameCoinLog{}).Where("user_id = ? AND type = ?", player.user.Id, model.GameCoinLogTypeGameReward).Count(&logs).Error)
				assert.Zero(t, logs)
			})
		}
	}
}

// DailyRewards 是新领奖次数上限，已有待发权益仍按累计有效时长发出；它不是冻结已取得权益的总开关。
func TestWendaoEconomyZeroDailyRewardsStillSettlesPreviouslyPendingReward(t *testing.T) {
	for _, mode := range []string{"rift", "tournament"} {
		t.Run(mode, func(t *testing.T) {
			setting := gameSetting(func(s *operation_setting.GameSetting) {
				s.Wendao, s.WendaoTournament = puzzleRule(), puzzleRule()
				s.WendaoTournament.Tiers = []operation_setting.RewardTier{{Score: 3, Coins: 1}}
				s.Wendao.MinSecondsPerCoin, s.WendaoTournament.MinSecondsPerCoin = 120, 120
			})
			env := newGameTestEnv(t, setting)
			player := env.player(0, gameTestIpA)
			foundTestSect(t, player)
			first := startWendaoEconomyRound(t, env, player, mode)
			// 初始零步/零 ticks 的夹具至多算 60 秒，确定达不到 120 秒要求，无需等待或比较运行速度。
			pending := first.settle(player)
			assert.EqualValues(t, 0, pending["reward"])
			assert.Equal(t, model.GameRewardLimitedByTime, pending["limited_by"])
			progress, err := model.GetGameRewardProgress(player.user.Id, gameRewardIpKey(player.ip), first.game, gameCenterDay())
			require.NoError(t, err)
			assert.Equal(t, 1, progress.Rewards)
			unit, err := model.WholeGameCoins(1)
			require.NoError(t, err)
			assert.Equal(t, unit, progress.PendingReward)
			paused := *setting
			paused.Wendao.DailyRewards, paused.WendaoTournament.DailyRewards = 0, 0
			operation_setting.SetGameSettingForTest(&paused)
			second := startWendaoEconomyRound(t, env, player, mode)
			// 直接布置此前已核实操作的累计时长，专测统一结算，不把fixture编辑当真实游玩时长验证。
			require.NoError(t, env.db.Model(&model.GameSession{}).Where("id = ?", second.request["session_id"]).Updates(map[string]any{"play_seconds": 120, "played_at": common.GetTimestamp()}).Error)
			if mode == "rift" {
				second.request["version"] = editRift(t, player.user.Id, func(run *doufa.Run) { run.Ticks = 120 * doufa.FPS })
			} else {
				session, err := model.GetUserGameSession(player.user.Id, second.game, int(second.request["session_id"].(float64)))
				require.NoError(t, err)
				state, err := parseSectTournament(session)
				require.NoError(t, err)
				state.Steps = 8
				encoded, err := common.Marshal(state)
				require.NoError(t, err)
				require.NoError(t, env.db.Model(session).Update("state", string(encoded)).Error)
			}
			settled := second.settle(player)
			assert.EqualValues(t, 1, settled["reward"], "the HTTP amount is expressed in game coins")
			assert.Equal(t, 1.0, player.balance())
			progress, err = model.GetGameRewardProgress(player.user.Id, gameRewardIpKey(player.ip), second.game, gameCenterDay())
			require.NoError(t, err)
			assert.Equal(t, 1, progress.Rewards, "zero limit creates no new claim")
			assert.Zero(t, progress.PendingReward)
			again := second.settle(player)
			assert.Equal(t, settled["reward"], again["reward"])
			assert.Equal(t, 1.0, player.balance())
		})
	}
}
