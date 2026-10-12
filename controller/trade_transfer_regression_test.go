package controller

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 展示可以保留预测成本，实际划转必须拒绝未决的过期估值，并把已判负持仓按零兑付计算。
func TestTradeTransferDoesNotReturnCoinsUsingPredictionDisplayCost(t *testing.T) {
	require.NoError(t, i18n.Init())
	previousUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 500_000
	t.Cleanup(func() { common.QuotaPerUnit = previousUnit })

	for _, test := range []struct {
		name    string
		outcome string
	}{
		{name: "expired unresolved prediction"},
		{name: "confirmed losing prediction", outcome: model.TradePredictionDown},
	} {
		t.Run(test.name, func(t *testing.T) {
			env := newGameTestEnv(t, gameSetting(nil))
			require.NoError(t, env.db.AutoMigrate(&model.TradeAccount{}, &model.TradePosition{}, &model.TradeLedger{},
				&model.TradeFuturesPosition{}, &model.TradeFuturesOrder{}, &model.TradeSpotMargin{}, &model.TradeSpotAssetLoan{},
				&model.TradePredictionRound{}, &model.TradePredictionPosition{}, &model.TradeProfitOutDay{}))
			env.router.POST("/api/trade/transfer", middleware.UserAuth(), TransferTrade)
			player := env.player(0, gameTestIpA)
			tenCoins, err := model.WholeGameCoins(10)
			require.NoError(t, err)
			account := model.TradeAccount{
				UserId: player.user.Id, Cash: int(tenCoins), QuotaPrincipal: int(2 * tenCoins),
				CoinPrincipal: tenCoins, TotalIn: int(2 * tenCoins), LossOrder: model.TradeLossCoinFirst,
			}
			require.NoError(t, env.db.Create(&account).Error)
			round := model.TradePredictionRound{WindowStart: 300, EndTime: 600, Outcome: test.outcome}
			require.NoError(t, env.db.Create(&round).Error)
			position := model.TradePredictionPosition{
				UserId: player.user.Id, WindowStart: round.WindowStart, Side: model.TradePredictionUp,
				Qty: 20 * 100_000_000, InitialQty: 20 * 100_000_000, Cost: int(tenCoins),
				TotalCost: int(tenCoins), Status: model.TradePredictionActive,
			}
			require.NoError(t, env.db.Create(&position).Error)

			_, display, err := service.ValueTradeUser(player.user.Id)
			require.NoError(t, err)
			require.Equal(t, int(2*tenCoins), display.Equity, "展示仍允许按历史成本回退")
			response := player.call(http.MethodPost, "/api/trade/transfer", map[string]any{
				"direction": "out", "wallet": "game_coin", "amount": "10",
			})
			assert.Equal(t, false, response["success"])
			assert.Zero(t, player.balance(), "估值不可用或游戏币本金已亏掉时不能入账")
			stored, err := model.GetTradeAccount(player.user.Id)
			require.NoError(t, err)
			assert.Equal(t, account.Cash, stored.Cash)
			assert.Equal(t, account.QuotaPrincipal, stored.QuotaPrincipal)
			assert.Equal(t, account.CoinPrincipal, stored.CoinPrincipal, "被拒绝的划转应整笔回滚")
			var entries int64
			require.NoError(t, env.db.Model(&model.TradeLedger{}).Where("user_id = ?", player.user.Id).Count(&entries).Error)
			assert.Zero(t, entries)
		})
	}
}
