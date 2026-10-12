package model

import (
	"strconv"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// tradeCashEquity 像转账接口那样在转账之前给账户估值：只有现金的账户总资产就是现金。
func tradeCashEquity(t *testing.T, userId int) TradeEquity {
	t.Helper()
	account := tradeTestAccount(t, userId)
	return TradeEquity{Value: account.Cash, Known: true, Account: account}
}

func TestTradeCoinAndQuotaPrincipalShareOneCashPool(t *testing.T) {
	truncateTables(t)
	seedTradeUser(t, 6100, 10)
	require.NoError(t, DB.Create(&GameCoinAccount{UserId: 6100, Balance: coin(10)}).Error)
	_, err := TransferQuotaToTrade(6100, tradeUsd(4), tradeCashEquity(t, 6100))
	require.NoError(t, err)
	account, err := TransferGameCoinsToTrade(6100, 6, tradeTestDay, tradeCashEquity(t, 6100))
	require.NoError(t, err)
	assert.Equal(t, tradeUsd(10), account.Cash)
	assert.Equal(t, tradeUsd(10), account.QuotaPrincipal)
	assert.Equal(t, coin(6), account.CoinPrincipal)
	assert.Equal(t, tradeUsd(10), account.TotalIn)

	_, _, err = TransferGameCoinsFromTrade(6100, coinText("6.01"), tradeTestDay, tradeUsd(100), nil, tradeCashEquity(t, 6100))
	require.ErrorIs(t, err, ErrTradeCoinPrincipal, "quota principal must not come back as game coins")
	_, profit, err := TransferGameCoinsFromTrade(6100, coinText("2.5"), tradeTestDay, tradeUsd(100), nil, tradeCashEquity(t, 6100))
	require.NoError(t, err)
	assert.Zero(t, profit, "both deposit currencies contribute to the same principal")
	_, _, err = TransferGameCoinsFromTrade(6100, coinText("3.5"), tradeTestDay, tradeUsd(100), nil, tradeCashEquity(t, 6100))
	require.NoError(t, err, "coins can come back in fractional parts")
	account, profit, err = TransferQuotaFromTrade(6100, tradeUsd(4), tradeTestDay, tradeUsd(100), nil, tradeCashEquity(t, 6100))
	require.NoError(t, err)
	assert.Zero(t, profit)
	assert.Zero(t, account.Cash)
	assert.Zero(t, account.QuotaPrincipal)
	assert.Zero(t, account.CoinPrincipal)
	assert.Equal(t, tradeUsd(10), account.TotalOut)
	quota, err := GetUserQuota(6100, true)
	require.NoError(t, err)
	assert.Equal(t, tradeUsd(10), quota)
	assert.Equal(t, coin(10), gameBalance(t, 6100), "each wallet gets back exactly the principal it put in")

	var logs []GameCoinLog
	require.NoError(t, DB.Where("user_id = ?", 6100).Order("id ASC").Find(&logs).Error)
	require.Len(t, logs, 3)
	assert.Equal(t, []string{GameCoinLogTypeTradeOut, GameCoinLogTypeTradeIn, GameCoinLogTypeTradeIn}, []string{logs[0].Type, logs[1].Type, logs[2].Type})
	assert.Equal(t, []GameCoins{-coin(6), coinText("2.5"), coinText("3.5")}, []GameCoins{logs[0].Amount, logs[1].Amount, logs[2].Amount})
	assert.Equal(t, []int{tradeUsd(6), tradeUsd(2.5), tradeUsd(3.5)}, []int{logs[0].Quota, logs[1].Quota, logs[2].Quota})
	for _, log := range logs {
		ledgerId, err := strconv.Atoi(log.RefId)
		require.NoError(t, err)
		var ledger TradeLedger
		require.NoError(t, DB.First(&ledger, ledgerId).Error)
		assert.Equal(t, log.UserId, ledger.UserId)
		assert.Equal(t, -int(log.Amount), ledger.Amount, "game coins and the simulator book the same units")
	}
}

// 盈利只能转出成额度：转回游戏币以转入的币数为限，从不算盈利，也不占用每日盈利转出上限。
func TestTradeProfitCanOnlyLeaveAsQuota(t *testing.T) {
	truncateTables(t)
	seedTradeUser(t, 6101, 0)
	require.NoError(t, DB.Create(&GameCoinAccount{UserId: 6101, Balance: coin(10)}).Error)
	_, err := TransferGameCoinsToTrade(6101, 10, tradeTestDay, tradeCashEquity(t, 6101))
	require.NoError(t, err)
	tradeRoundTrip(t, 6101, 10, 115)
	_, _, err = TransferGameCoinsFromTrade(6101, coin(11), tradeTestDay, tradeUsd(100), nil, tradeCashEquity(t, 6101))
	require.ErrorIs(t, err, ErrTradeCoinPrincipal)
	_, profit, err := TransferGameCoinsFromTrade(6101, coin(10), tradeTestDay, tradeUsd(100), nil, tradeCashEquity(t, 6101))
	require.NoError(t, err)
	assert.Zero(t, profit)
	used, err := GetTradeProfitOutUsed(6101, tradeTestDay)
	require.NoError(t, err)
	assert.Zero(t, used)
	_, profit, err = TransferQuotaFromTrade(6101, tradeUsd(100), tradeTestDay, tradeUsd(100), nil, tradeCashEquity(t, 6101))
	require.NoError(t, err)
	assert.Equal(t, tradeUsd(100), profit)
	_, _, err = TransferQuotaFromTrade(6101, tradeUsd(1), tradeTestDay, tradeUsd(100), nil, tradeCashEquity(t, 6101))
	require.ErrorIs(t, err, ErrTradeProfitOutLimit)
	account := tradeTestAccount(t, 6101)
	assert.Equal(t, tradeUsd(5), account.Cash)
	assert.Zero(t, account.CoinPrincipal)
	assert.Equal(t, coin(10), gameBalance(t, 6101))
}

// 本金转出成额度后，剩下的本金不够折合的币也不能再转回游戏币：先转出本金、再转入额度，不能把额度换成游戏币。
func TestTradeCoinPrincipalShrinksWithPrincipal(t *testing.T) {
	truncateTables(t)
	seedTradeUser(t, 6106, 4)
	require.NoError(t, DB.Create(&GameCoinAccount{UserId: 6106, Balance: coin(6)}).Error)
	_, err := TransferGameCoinsToTrade(6106, 6, tradeTestDay, tradeCashEquity(t, 6106))
	require.NoError(t, err)
	account, _, err := TransferQuotaFromTrade(6106, tradeUsd(4), tradeTestDay, tradeUsd(100), nil, tradeCashEquity(t, 6106))
	require.NoError(t, err)
	assert.Equal(t, tradeUsd(2), account.QuotaPrincipal)
	assert.Equal(t, coin(2), account.CoinPrincipal)
	account, err = TransferQuotaToTrade(6106, tradeUsd(8), tradeCashEquity(t, 6106))
	require.NoError(t, err)
	assert.Equal(t, tradeUsd(10), account.QuotaPrincipal)
	assert.Equal(t, coin(2), account.CoinPrincipal, "quota deposits never add coin principal")
	_, _, err = TransferGameCoinsFromTrade(6106, coin(3), tradeTestDay, tradeUsd(100), nil, tradeCashEquity(t, 6106))
	require.ErrorIs(t, err, ErrTradeCoinPrincipal)
	account, _, err = TransferGameCoinsFromTrade(6106, coin(2), tradeTestDay, tradeUsd(100), nil, tradeCashEquity(t, 6106))
	require.NoError(t, err)
	assert.Zero(t, account.CoinPrincipal)
	assert.Equal(t, tradeUsd(8), account.Cash)
	assert.Equal(t, coin(2), gameBalance(t, 6106))
}

// 转入模拟盘的币可以再转出成额度，和兑换额度共用每人每天的上限；超出时什么都不改，换一天重新计算。
func TestTradeCoinDepositSharesDailyConvertLimit(t *testing.T) {
	truncateTables(t)
	seedTradeUser(t, 6107, 0)
	require.NoError(t, DB.Create(&GameCoinAccount{UserId: 6107, Balance: coin(40)}).Error)
	today := GameCenterDay(time.Now())
	account, err := TransferGameCoinsToTrade(6107, 5, today, tradeCashEquity(t, 6107))
	require.NoError(t, err)
	assert.Equal(t, coin(5), account.CoinPrincipal)
	used, err := GetGameCoinConvertDayUsed(6107, today)
	require.NoError(t, err)
	assert.Equal(t, coin(5), used, "the deposit's own ledger entry must not be counted twice")
	before := tradeTestAccount(t, 6107)
	_, err = TransferGameCoinsToTrade(6107, 11, today, tradeCashEquity(t, 6107))
	require.ErrorIs(t, err, ErrGameCoinConvertDailyLimit)
	assert.Equal(t, coin(35), gameBalance(t, 6107))
	assert.Equal(t, before, tradeTestAccount(t, 6107), "a rejected deposit must not fund the account")
	assert.Equal(t, []string{GameCoinLogTypeTradeOut}, gameCoinLogTypes(t, 6107))

	_, _, err = ConvertGameCoinsToQuota(6107, coin(10))
	require.NoError(t, err)
	used, err = GetGameCoinConvertDayUsed(6107, today)
	require.NoError(t, err)
	assert.Equal(t, coin(GameCoinConvertDailyLimit), used)
	_, _, err = ConvertGameCoinsToQuota(6107, coin(1))
	require.ErrorIs(t, err, ErrGameCoinConvertDailyLimit, "simulator deposits use up the conversion allowance too")

	_, err = TransferGameCoinsToTrade(6107, GameCoinConvertDailyLimit+1, "2026-10-04", tradeCashEquity(t, 6107))
	require.ErrorIs(t, err, ErrGameCoinConvertDailyLimit)
	_, err = TransferGameCoinsToTrade(6107, GameCoinConvertDailyLimit, "2026-10-04", tradeCashEquity(t, 6107))
	require.NoError(t, err)
	assert.Equal(t, coin(10), gameBalance(t, 6107))
}

func TestTradeCoinWalletFailureRollsBackBothLedgers(t *testing.T) {
	truncateTables(t)
	seedTradeUser(t, 6102, 0)
	require.NoError(t, DB.Create(&GameCoinAccount{UserId: 6102, Balance: coin(12)}).Error)
	_, err := TransferGameCoinsToTrade(6102, 12, tradeTestDay, tradeCashEquity(t, 6102))
	require.NoError(t, err)
	// 钱包余额已到上限，转回的币入不了账。
	require.NoError(t, DB.Model(&GameCoinAccount{}).Where("user_id = ?", 6102).Update("balance", common.MaxQuota-1).Error)
	before := tradeTestAccount(t, 6102)
	_, ledgersBefore, err := GetTradeLedgers(6102, "", 0, 10)
	require.NoError(t, err)
	_, _, err = TransferGameCoinsFromTrade(6102, coin(12), tradeTestDay, tradeUsd(100), nil, tradeCashEquity(t, 6102))
	require.ErrorIs(t, err, ErrGameCoinAmountInvalid)
	assert.Equal(t, before, tradeTestAccount(t, 6102), "cash and coin principal must roll back together")
	assert.Equal(t, GameCoins(common.MaxQuota-1), gameBalance(t, 6102))
	_, ledgersAfter, err := GetTradeLedgers(6102, "", 0, 10)
	require.NoError(t, err)
	assert.Equal(t, ledgersBefore, ledgersAfter)
	assert.Equal(t, []string{GameCoinLogTypeTradeOut}, gameCoinLogTypes(t, 6102))
	_, profit, err := TransferQuotaFromTrade(6102, tradeUsd(12), tradeTestDay, tradeUsd(100), nil, tradeCashEquity(t, 6102))
	require.NoError(t, err)
	assert.Zero(t, profit)
}

func TestTradeCoinTransferRejectsUnfundedOrInvalidDeposits(t *testing.T) {
	truncateTables(t)
	seedTradeUser(t, 6103, 0)
	require.NoError(t, DB.Create(&GameCoinAccount{UserId: 6103, Balance: coin(2)}).Error)
	_, err := TransferGameCoinsToTrade(6103, 3, tradeTestDay, tradeCashEquity(t, 6103))
	require.ErrorIs(t, err, ErrGameCoinInsufficient)
	for _, amount := range []int{-1, 0, common.MaxQuota} {
		_, err = TransferGameCoinsToTrade(6103, amount, tradeTestDay, tradeCashEquity(t, 6103))
		require.ErrorIs(t, err, ErrGameCoinAmountInvalid)
		_, _, err = TransferGameCoinsFromTrade(6103, GameCoins(amount), tradeTestDay, tradeUsd(100), nil, tradeCashEquity(t, 6103))
		require.ErrorIs(t, err, ErrGameCoinAmountInvalid)
	}
	account := tradeTestAccount(t, 6103)
	assert.Zero(t, account.Cash)
	assert.Zero(t, account.TotalIn)
	assert.Equal(t, coin(2), gameBalance(t, 6103))
	_, total, err := GetTradeLedgers(6103, "", 0, 10)
	require.NoError(t, err)
	assert.Zero(t, total)
	var logCount int64
	require.NoError(t, DB.Model(&GameCoinLog{}).Where("user_id = ?", 6103).Count(&logCount).Error)
	assert.Zero(t, logCount)
}

func TestTradeCoinWithdrawalPreservesCrossMarginAndExcludesFloatingProfit(t *testing.T) {
	truncateTables(t)
	seedTradeFuturesAccount(t, 6104, 100)
	// 这 100 美元本金按游戏币转入记，只看全仓保证金与浮动盈利的限制。
	require.NoError(t, DB.Model(&TradeAccount{}).Where("user_id = ?", 6104).Update("coin_principal", tradeUsd(100)).Error)
	market := tradeTestMarket{tradeFuturesSymbol: "500"}
	openTradeFutures(t, 6104, tradeFuturesSymbol, TradeFuturesCross, TradeFuturesLong, tradeFuturesCoin, "500", 10, 0, market)
	equity := TradeEquity{Value: tradeUsd(100), Known: true, Account: tradeTestAccount(t, 6104)}
	_, _, err := TransferGameCoinsFromTrade(6104, coin(51), tradeTestDay, tradeUsd(100), market, equity)
	require.ErrorIs(t, err, ErrTradeWithdrawExceeded)
	account, profit, err := TransferGameCoinsFromTrade(6104, coin(50), tradeTestDay, tradeUsd(100), market, equity)
	require.NoError(t, err)
	assert.Zero(t, profit)
	assert.Equal(t, tradeUsd(50), account.Cash)
	// 标记价格涨到 550：50 资金加 50 浮动盈利，总资产还是 100。
	equity = TradeEquity{Value: tradeUsd(100), Known: true, Account: tradeTestAccount(t, 6104)}
	_, _, err = TransferGameCoinsFromTrade(6104, coin(1), tradeTestDay, tradeUsd(100), tradeTestMarket{tradeFuturesSymbol: "550"}, equity)
	require.ErrorIs(t, err, ErrTradeWithdrawExceeded)
	_, _, err = TransferGameCoinsFromTrade(6104, coin(1), tradeTestDay, tradeUsd(100), nil, equity)
	require.ErrorIs(t, err, ErrTradeMarkUnavailable)
}

func TestTradeCoinFundingCanRepayNegativeSiteQuota(t *testing.T) {
	truncateTables(t)
	seedTradeFuturesAccount(t, 6105, 10)
	openTradeFutures(t, 6105, tradeFuturesSymbol, TradeFuturesIsolated, TradeFuturesLong, tradeFuturesCoin, "100", 10, 0, nil)
	liquidated, err := LiquidateTradeFuturesPosition(6105, tradeFuturesSymbol, TradeFuturesLong, decimal.NewFromInt(80),
		tradeTestMarket{}.Brackets(tradeFuturesSymbol), 0)
	require.NoError(t, err)
	require.True(t, liquidated)
	quota, err := GetUserQuota(6105, true)
	require.NoError(t, err)
	assert.Equal(t, -tradeUsd(10), quota, "without game coins the deficit is charged to the site quota")
	require.NoError(t, DB.Create(&GameCoinAccount{UserId: 6105, Balance: coin(5)}).Error)
	_, err = TransferGameCoinsToTrade(6105, 5, tradeTestDay, tradeCashEquity(t, 6105))
	require.NoError(t, err)
	_, profit, err := TransferQuotaFromTrade(6105, tradeUsd(5), tradeTestDay, tradeUsd(100), nil, tradeCashEquity(t, 6105))
	require.NoError(t, err, "negative site quota must not block a transfer that repays it")
	assert.Zero(t, profit)
	quota, err = GetUserQuota(6105, true)
	require.NoError(t, err)
	assert.Equal(t, -tradeUsd(5), quota)
	assert.Zero(t, gameBalance(t, 6105))
}

// 亏损抵扣顺序决定亏损先记到哪一种本金上：先扣游戏币时总资产先还额度本金，游戏币本金只拿剩下的；先扣平台额度时游戏币本金先拿。
// 盈利不会让转回的游戏币变多。账户先转入 10 额度和 10 个币，再买入 buyUsd、卖出 sellUsd。
func TestTradeLossOrderDecidesWhichPrincipalTakesTheLoss(t *testing.T) {
	for i, test := range []struct {
		name      string
		order     string
		buyUsd    float64
		sellUsd   float64
		wantCoins GameCoins
	}{
		{"coins take the loss first", TradeLossCoinFirst, 10, 5, coin(5)},
		{"coins keep a fraction", TradeLossCoinFirst, 10, 0.5, coinText("0.5")},
		{"coins are wiped before quota", TradeLossCoinFirst, 15, 1, 0},
		{"profit adds no coins", TradeLossCoinFirst, 10, 25, coin(10)},
		{"quota takes the loss first", TradeLossQuotaFirst, 10, 5, coin(10)},
		{"coins take what quota cannot", TradeLossQuotaFirst, 15, 1, coin(6)},
	} {
		t.Run(test.name, func(t *testing.T) {
			truncateTables(t)
			userId := 6120 + i
			seedTradeUser(t, userId, 10)
			require.NoError(t, DB.Create(&GameCoinAccount{UserId: userId, Balance: coin(10)}).Error)
			_, err := SetTradeLossOrder(userId, test.order)
			require.NoError(t, err)
			_, err = TransferQuotaToTrade(userId, tradeUsd(10), tradeCashEquity(t, userId))
			require.NoError(t, err)
			_, err = TransferGameCoinsToTrade(userId, 10, tradeTestDay, tradeCashEquity(t, userId))
			require.NoError(t, err)
			tradeRoundTrip(t, userId, test.buyUsd, test.sellUsd)

			equity := tradeCashEquity(t, userId)
			assert.Equal(t, test.wantCoins, TradeCoinReturnable(equity.Account, equity.Value))
			_, _, err = TransferGameCoinsFromTrade(userId, test.wantCoins+coinText("0.01"), tradeTestDay, tradeUsd(100), nil, equity)
			require.ErrorIs(t, err, ErrTradeCoinPrincipal)
			if test.wantCoins == 0 {
				return
			}
			account, _, err := TransferGameCoinsFromTrade(userId, test.wantCoins, tradeTestDay, tradeUsd(100), nil, equity)
			require.NoError(t, err)
			assert.Zero(t, account.CoinPrincipal)
			assert.Equal(t, tradeUsd(10), account.QuotaPrincipal, "the coins' loss leaves with them, the quota principal stays")
			assert.Equal(t, equity.Value-int(test.wantCoins), account.Cash)
			assert.Equal(t, test.wantCoins, gameBalance(t, userId))
		})
	}
}

// 亏掉的游戏币不能用额度补回来再转回游戏币：每次转账之前先按总资产结算，结算掉的部分之后再转入额度也回不来。
func TestTradeCoinLossCannotBeRefilledWithQuota(t *testing.T) {
	for i, order := range []string{TradeLossCoinFirst, TradeLossQuotaFirst} {
		t.Run(order, func(t *testing.T) {
			truncateTables(t)
			userId := 6130 + i
			seedTradeUser(t, userId, 10)
			require.NoError(t, DB.Create(&GameCoinAccount{UserId: userId, Balance: coin(10)}).Error)
			_, err := SetTradeLossOrder(userId, order)
			require.NoError(t, err)
			_, err = TransferGameCoinsToTrade(userId, 10, tradeTestDay, tradeCashEquity(t, userId))
			require.NoError(t, err)
			tradeRoundTrip(t, userId, 10, 4)

			account, err := TransferQuotaToTrade(userId, tradeUsd(10), tradeCashEquity(t, userId))
			require.NoError(t, err)
			assert.Equal(t, coin(4), account.CoinPrincipal, "the loss is settled before the quota arrives")
			assert.Equal(t, tradeUsd(14), account.QuotaPrincipal)
			_, _, err = TransferGameCoinsFromTrade(userId, coinText("4.01"), tradeTestDay, tradeUsd(100), nil, tradeCashEquity(t, userId))
			require.ErrorIs(t, err, ErrTradeCoinPrincipal)
			_, _, err = TransferGameCoinsFromTrade(userId, coin(4), tradeTestDay, tradeUsd(100), nil, tradeCashEquity(t, userId))
			require.NoError(t, err)
			_, profit, err := TransferQuotaFromTrade(userId, tradeUsd(10), tradeTestDay, tradeUsd(100), nil, tradeCashEquity(t, userId))
			require.NoError(t, err)
			assert.Zero(t, profit)
			assert.Equal(t, coin(4), gameBalance(t, userId))
			quota, err := GetUserQuota(userId, true)
			require.NoError(t, err)
			assert.Equal(t, tradeUsd(10), quota)
		})
	}
}

// 只有额度时亏掉的是额度：之后转入的游戏币不替它买单，先扣游戏币时也能原样转回；这部分额度亏损再赚回来算作盈利。
func TestTradeCoinDepositDoesNotPayForEarlierQuotaLoss(t *testing.T) {
	truncateTables(t)
	seedTradeUser(t, 6111, 10)
	require.NoError(t, DB.Create(&GameCoinAccount{UserId: 6111, Balance: coin(5)}).Error)
	_, err := TransferQuotaToTrade(6111, tradeUsd(10), tradeCashEquity(t, 6111))
	require.NoError(t, err)
	tradeRoundTrip(t, 6111, 10, 4)
	account, err := TransferGameCoinsToTrade(6111, 5, tradeTestDay, tradeCashEquity(t, 6111))
	require.NoError(t, err)
	assert.Equal(t, tradeUsd(9), account.QuotaPrincipal, "the quota loss is written off before the coins join")
	assert.Equal(t, coin(5), account.CoinPrincipal)
	assert.Equal(t, coin(5), TradeCoinReturnable(*account, account.Cash))

	tradeRoundTrip(t, 6111, 9, 11)
	_, profit, err := TransferGameCoinsFromTrade(6111, coin(5), tradeTestDay, tradeUsd(100), nil, tradeCashEquity(t, 6111))
	require.NoError(t, err)
	assert.Zero(t, profit)
	_, profit, err = TransferQuotaFromTrade(6111, tradeUsd(6), tradeTestDay, tradeUsd(100), nil, tradeCashEquity(t, 6111))
	require.NoError(t, err)
	assert.Equal(t, tradeUsd(2), profit)
}

// 有游戏币本金、或者这次要转入游戏币时，估值不可用或估值之后账户又变了都拒绝转账，什么都不改；只有额度的账户用不到估值。
func TestTradeTransferNeedsAFreshValuationWhenCoinsAreInvolved(t *testing.T) {
	truncateTables(t)
	seedTradeUser(t, 6112, 20)
	require.NoError(t, DB.Create(&GameCoinAccount{UserId: 6112, Balance: coin(5)}).Error)
	_, err := TransferQuotaToTrade(6112, tradeUsd(5), TradeEquity{})
	require.NoError(t, err, "a quota-only account needs no valuation")
	stale := tradeCashEquity(t, 6112)
	_, err = TransferQuotaToTrade(6112, tradeUsd(5), TradeEquity{})
	require.NoError(t, err)
	for _, test := range []struct {
		equity TradeEquity
		want   error
	}{
		{TradeEquity{}, ErrTradeMarkUnavailable},
		{stale, ErrTradeValuationStale},
	} {
		_, err = TransferGameCoinsToTrade(6112, 5, tradeTestDay, test.equity)
		require.ErrorIs(t, err, test.want)
	}
	assert.Equal(t, coin(5), gameBalance(t, 6112))
	_, err = TransferGameCoinsToTrade(6112, 5, tradeTestDay, tradeCashEquity(t, 6112))
	require.NoError(t, err)

	stale = tradeCashEquity(t, 6112)
	_, err = TransferQuotaToTrade(6112, tradeUsd(1), stale)
	require.NoError(t, err)
	before := tradeTestAccount(t, 6112)
	for _, test := range []struct {
		equity TradeEquity
		want   error
	}{
		{TradeEquity{}, ErrTradeMarkUnavailable},
		{stale, ErrTradeValuationStale},
	} {
		_, _, err = TransferGameCoinsFromTrade(6112, coin(1), tradeTestDay, tradeUsd(100), nil, test.equity)
		require.ErrorIs(t, err, test.want)
		_, _, err = TransferQuotaFromTrade(6112, tradeUsd(1), tradeTestDay, tradeUsd(100), nil, test.equity)
		require.ErrorIs(t, err, test.want)
		_, err = TransferQuotaToTrade(6112, tradeUsd(1), test.equity)
		require.ErrorIs(t, err, test.want)
	}
	assert.Equal(t, before, tradeTestAccount(t, 6112))
}

// 发布猜错的结果和零派彩结算都不改变现金，却会使预测资产消失；旧估值不能把损失挪到额度本金。
func TestTradeCoinTransferRejectsValuationBeforePredictionLoss(t *testing.T) {
	for _, settle := range []bool{false, true} {
		t.Run(strconv.FormatBool(settle), func(t *testing.T) {
			round := predictionTestRound(t, common.GetTimestamp()/300*300-300)
			const userId = 6140
			seedTradeUser(t, userId, 0)
			require.NoError(t, DB.Create(&TradeAccount{UserId: userId, Cash: tradeUsd(10), QuotaPrincipal: tradeUsd(20), CoinPrincipal: coin(10), TotalIn: tradeUsd(20)}).Error)
			position := TradePredictionPosition{UserId: userId, WindowStart: round.WindowStart, Side: TradePredictionUp,
				Qty: 20 * 100_000_000, InitialQty: 20 * 100_000_000, Cost: tradeUsd(10), TotalCost: tradeUsd(10), Status: TradePredictionActive}
			require.NoError(t, DB.Create(&position).Error)
			if settle {
				require.NoError(t, ResolveTradePredictionRound(round.WindowStart, TradePredictionDown))
			}
			before := tradeTestAccount(t, userId)
			stale := TradeEquity{Value: tradeUsd(20), Known: true, Account: before}
			if settle {
				require.NoError(t, SettleTradePredictionPosition(position.Id))
			} else {
				require.NoError(t, ResolveTradePredictionRound(round.WindowStart, TradePredictionDown))
			}
			after := tradeTestAccount(t, userId)
			assert.Equal(t, before.Cash, after.Cash)
			assert.Equal(t, before.Frozen, after.Frozen)
			assert.Equal(t, before.QuotaPrincipal, after.QuotaPrincipal)
			assert.Equal(t, before.CoinPrincipal, after.CoinPrincipal)
			_, _, err := TransferGameCoinsFromTrade(userId, coin(10), tradeTestDay, tradeUsd(100), nil, stale)
			require.ErrorIs(t, err, ErrTradeValuationStale)
			_, _, err = TransferGameCoinsFromTrade(userId, coin(1), tradeTestDay, tradeUsd(100), nil, TradeEquity{Value: tradeUsd(10), Known: true, Account: after})
			require.ErrorIs(t, err, ErrTradeCoinPrincipal)
			assert.Equal(t, after, tradeTestAccount(t, userId))
			assert.Zero(t, gameBalance(t, userId))
		})
	}
}

func TestTradeCoinTransferRejectsValuationWhenBalancesReturnToSameValues(t *testing.T) {
	truncateTables(t)
	const userId = 6141
	seedTradeUser(t, userId, 1)
	require.NoError(t, DB.Create(&TradeAccount{UserId: userId, Cash: tradeUsd(20), QuotaPrincipal: tradeUsd(20), CoinPrincipal: coin(10), TotalIn: tradeUsd(20)}).Error)
	stale := tradeCashEquity(t, userId)
	_, err := TransferQuotaToTrade(userId, tradeUsd(1), stale)
	require.NoError(t, err)
	_, _, err = TransferQuotaFromTrade(userId, tradeUsd(1), tradeTestDay, tradeUsd(100), nil, tradeCashEquity(t, userId))
	require.NoError(t, err)
	after := tradeTestAccount(t, userId)
	assert.Equal(t, stale.Account.Cash, after.Cash)
	assert.Equal(t, stale.Account.QuotaPrincipal, after.QuotaPrincipal)
	assert.Equal(t, stale.Account.CoinPrincipal, after.CoinPrincipal)
	_, _, err = TransferGameCoinsFromTrade(userId, coin(1), tradeTestDay, tradeUsd(100), nil, stale)
	require.ErrorIs(t, err, ErrTradeValuationStale)
	_, _, err = TransferGameCoinsFromTrade(userId, coin(1), tradeTestDay, tradeUsd(100), nil, tradeCashEquity(t, userId))
	require.NoError(t, err)
	assert.Equal(t, coin(1), gameBalance(t, userId))
}

func TestTradeCoinTransferRejectsValuationBeforeSpotInterestAccrual(t *testing.T) {
	truncateTables(t)
	useTradeSpotRate(t, "0.24")
	const userId = 6142
	seedTradeUser(t, userId, 0)
	require.NoError(t, DB.Create(&TradeAccount{UserId: userId, Cash: tradeUsd(20), QuotaPrincipal: tradeUsd(20), CoinPrincipal: coin(10), TotalIn: tradeUsd(20)}).Error)
	require.NoError(t, DB.Create(&TradeSpotMargin{UserId: userId, Principal: tradeUsd(5), ChargedUntil: common.GetTimestamp() - 3600, DailyRate: "0.24"}).Error)
	stale := tradeCashEquity(t, userId)
	result, err := SettleTradeSpotMargin(userId, btcBid(100), nil, tradeSpotTestPricing(), 5)
	require.NoError(t, err)
	assert.False(t, result.Liquidated)
	assert.Positive(t, tradeSpotTestLoan(t, userId).Interest)
	assert.Equal(t, stale.Account.Cash, tradeTestAccount(t, userId).Cash)
	_, _, err = TransferGameCoinsFromTrade(userId, coin(1), tradeTestDay, tradeUsd(100), btcBid(100), stale)
	require.ErrorIs(t, err, ErrTradeValuationStale)
	assert.Zero(t, gameBalance(t, userId))
}

// 抵扣顺序只接受两种取值，账户还不存在时建出来；没设置过时先扣游戏币。
func TestSetTradeLossOrderValidatesTheOrder(t *testing.T) {
	truncateTables(t)
	seedTradeUser(t, 6113, 0)
	unset := tradeTestAccount(t, 6113)
	assert.True(t, unset.CoinFirst())
	_, err := SetTradeLossOrder(6113, "random")
	require.ErrorIs(t, err, ErrTradeAmountInvalid)
	account, err := SetTradeLossOrder(6113, TradeLossQuotaFirst)
	require.NoError(t, err)
	assert.False(t, account.CoinFirst())
	assert.Equal(t, TradeLossQuotaFirst, tradeTestAccount(t, 6113).LossOrder)
}

// 穿仓的亏空按抵扣顺序补：先扣游戏币时先用游戏币余额，不够的扣站内额度；先扣平台额度时先用站内额度里正的部分，不够的用游戏币，
// 再不够的仍扣站内额度(可以扣成负数)。注销了的用户照样补。补的钱算作转入，游戏币补的那部分算进游戏币本金；每个来源各记一笔账单，
// 游戏币流水指向它的账单，通知里写明两边各补了多少。账户转入 10，开 10 倍逐仓 100 的多单，跳空到 80 强平，亏空 10。
func TestTradeDeficitCoverFollowsLossOrder(t *testing.T) {
	for i, test := range []struct {
		name      string
		order     string
		quotaUsd  float64
		coins     GameCoins
		deleted   bool
		wantCover TradeDeficitCover
	}{
		{"coins first with fractional coins", TradeLossCoinFirst, 100, coinText("3.25"), false, TradeDeficitCover{Quota: tradeUsd(6.75), Coins: coinText("3.25")}},
		{"coins cover it all", TradeLossCoinFirst, 0, coin(20), false, TradeDeficitCover{Coins: coin(10)}},
		{"positive quota then coins then debt", TradeLossQuotaFirst, 4, coin(3), false, TradeDeficitCover{Quota: tradeUsd(7), Coins: coin(3)}},
		{"quota covers it all", TradeLossQuotaFirst, 100, coin(20), false, TradeDeficitCover{Quota: tradeUsd(10)}},
		{"negative quota goes to coins first", TradeLossQuotaFirst, -2, coin(5), false, TradeDeficitCover{Quota: tradeUsd(5), Coins: coin(5)}},
		{"deleted user still covers", TradeLossQuotaFirst, 4, coin(3), true, TradeDeficitCover{Quota: tradeUsd(7), Coins: coin(3)}},
	} {
		t.Run(test.name, func(t *testing.T) {
			truncateTables(t)
			userId := 6140 + i
			seedTradeFuturesAccount(t, userId, 10)
			require.NoError(t, DB.Model(&User{}).Where("id = ?", userId).Update("quota", tradeUsd(test.quotaUsd)).Error)
			require.NoError(t, DB.Create(&GameCoinAccount{UserId: userId, Balance: test.coins}).Error)
			_, err := SetTradeLossOrder(userId, test.order)
			require.NoError(t, err)
			openTradeFutures(t, userId, tradeFuturesSymbol, TradeFuturesIsolated, TradeFuturesLong, tradeFuturesCoin, "100", 10, 0, nil)
			if test.deleted {
				require.NoError(t, DB.Delete(&User{Id: userId}).Error)
			}
			var notified []TradeDeficitCover
			previous := TradeDeficitNotifier
			TradeDeficitNotifier = func(_ int, cover TradeDeficitCover) { notified = append(notified, cover) }
			t.Cleanup(func() { TradeDeficitNotifier = previous })

			liquidated, err := LiquidateTradeFuturesPosition(userId, tradeFuturesSymbol, TradeFuturesLong, decimal.NewFromInt(80),
				tradeTestMarket{}.Brackets(tradeFuturesSymbol), 0)
			require.NoError(t, err)
			require.True(t, liquidated)
			assert.Equal(t, []TradeDeficitCover{test.wantCover}, notified)

			var quotas []int
			require.NoError(t, DB.Unscoped().Model(&User{}).Where("id = ?", userId).Pluck("quota", &quotas).Error)
			assert.Equal(t, []int{tradeUsd(test.quotaUsd) - test.wantCover.Quota}, quotas)
			assert.Equal(t, test.coins-test.wantCover.Coins, gameBalance(t, userId))
			account := tradeTestAccount(t, userId)
			assert.Zero(t, account.Cash)
			assert.Equal(t, tradeUsd(20), account.QuotaPrincipal)
			assert.Equal(t, tradeUsd(20), account.TotalIn)
			assert.Equal(t, test.wantCover.Coins, account.CoinPrincipal)

			quotaLedgers, _, err := GetTradeLedgers(userId, TradeLedgerFuturesCover, 0, 10)
			require.NoError(t, err)
			coinLedgers, _, err := GetTradeLedgers(userId, TradeLedgerFuturesCoinCover, 0, 10)
			require.NoError(t, err)
			var logs []GameCoinLog
			require.NoError(t, DB.Where("user_id = ? AND type = ?", userId, GameCoinLogTypeTradeCover).Find(&logs).Error)
			if test.wantCover.Quota > 0 {
				require.Len(t, quotaLedgers, 1)
				assert.Equal(t, test.wantCover.Quota, quotaLedgers[0].Amount)
			} else {
				assert.Empty(t, quotaLedgers)
			}
			if test.wantCover.Coins > 0 {
				require.Len(t, coinLedgers, 1)
				assert.Equal(t, int(test.wantCover.Coins), coinLedgers[0].Amount)
				require.Len(t, logs, 1)
				assert.Equal(t, -test.wantCover.Coins, logs[0].Amount)
				assert.Equal(t, strconv.Itoa(coinLedgers[0].Id), logs[0].RefId)
			} else {
				assert.Empty(t, coinLedgers)
				assert.Empty(t, logs)
			}
			var notice TradeNotice
			require.NoError(t, DB.Where("user_id = ? AND kind = ?", userId, TradeNoticeCover).First(&notice).Error)
			assert.Equal(t, test.wantCover.Quota, notice.Amount)
			assert.Equal(t, test.wantCover.Coins, notice.Coins)
		})
	}
}
