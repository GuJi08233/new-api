package controller

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/horserace"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHorseraceStoresFullSlipWithinDatabaseDetailLimit(t *testing.T) {
	env := newGameTestEnv(t, gameSetting(func(s *operation_setting.GameSetting) {
		s.Horserace = operation_setting.WagerRule{Enabled: true, MaxBet: 10}
	}))
	player := env.player(10, gameTestIpA)
	order := []int{4, 1, 0, 2, 3, 5}
	raceHorsesAs(t, order)
	bets := make([]map[string]any, 0, 2*horserace.Horses)
	for horse := range horserace.Horses {
		for _, kind := range []string{horserace.BetWin, horserace.BetPlace} {
			bets = append(bets, map[string]any{"kind": kind, "horse": horse, "amount": 0.01})
		}
	}
	result := player.ok(http.MethodPost, "/api/game/horserace/race", map[string]any{"bets": bets})
	assert.Equal(t, 0.12, result["bet"])
	var session model.GameSession
	require.NoError(t, env.db.Where("user_id = ? AND game = ?", player.user.Id, gameHorserace).First(&session).Error)
	assert.LessOrEqual(t, len(session.Detail), 255, "MySQL and PostgreSQL enforce the detail column capacity")
	var detail horseraceDetail
	require.NoError(t, common.UnmarshalJsonStr(session.Detail, &detail))
	assert.Equal(t, order, detail.Order)
	assert.Equal(t, len(bets), detail.Bets)
	var slip []horseraceBetRequest
	require.NoError(t, common.UnmarshalJsonStr(session.State, &slip))
	require.Len(t, slip, len(bets))
	win := model.GameCoins(0)
	for i, bet := range slip {
		assert.Equal(t, bets[i]["kind"], bet.Kind)
		assert.Equal(t, bets[i]["horse"], bet.Horse)
		assert.Equal(t, 0.01, coinsOf(t, bet.Amount))
		win += bet.Amount * model.GameCoins(horserace.Multiplier(bet.Kind, bet.Horse, detail.Order)) / 100
	}
	assert.Equal(t, session.Reward, win, "stored slip and order reproduce the payout")
	assert.Equal(t, coinsOf(t, win), result["win"])
	assert.Equal(t, 10.0-0.12+coinsOf(t, win), player.balance())
}
