package model

import (
	"slices"
	"strings"
	"sync/atomic"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/tradesim"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// 借币卖出(做空)照 Binance 全仓杠杆的经典模式：借的是交易对的基础资产(BTC、NVDAB 等)，借来马上按盘口卖掉，到账的钱进资金；
// 欠的是币，利息也按币收：借的时候先收到下一个整点为止的，之后每到整点按这个币当时的日利率预收下一小时，提前还不退。买入这个币
// 时先还它的借币(先还利息再还本金)，剩下的才进持仓。借币按卖一估值，算进风险率的分母(见 TradeSpotRisk)，强平时按盘口买回来还掉。
//
// 借币卖出先卖可卖的持仓，不够的数量才借(Binance 的自动借款)。
const (
	TradeLedgerSpotAssetLoan     = "spot_coin_loan"
	TradeLedgerSpotAssetInterest = "spot_coin_int"
	TradeLedgerSpotAssetRepay    = "spot_coin_repay"
)

// tradeSpotMaxAssetQty 是一笔借币的本金加利息最多多少(10^-8)，挡住溢出。
const tradeSpotMaxAssetQty = int64(1) << 53

// tradeSpotDefaultAssetRates 是 2026-10-09 Binance 普通用户借各币种的日利率，取到实时的之前先用它们。
var tradeSpotDefaultAssetRates = map[string]string{
	"BTC": "0.00001165", "ETH": "0.00005316", "SOL": "0.00014976", "BNB": "0.00008044", "XRP": "0.00009793",
	"DOGE": "0.00009816", "ZEC": "0.00043448", "NVDAB": "0.0002", "TSLAB": "0.0002", "AMDB": "0.0002", "MUB": "0.0002",
	"SNDKB": "0.0002", "CRCLB": "0.0002", "MSTRB": "0.0002", "SPCXB": "0.0002", "QQQB": "0.0002", "SOXLB": "0.0002",
}

var tradeSpotAssetRates atomic.Pointer[map[string]decimal.Decimal]

func init() {
	rates := make(map[string]decimal.Decimal, len(tradeSpotDefaultAssetRates))
	for asset, rate := range tradeSpotDefaultAssetRates {
		rates[asset] = decimal.RequireFromString(rate)
	}
	tradeSpotAssetRates.Store(&rates)
}

// TradeSpotBaseAsset 是现货交易对借币时借的币：交易对都以 USDT 计价，去掉结尾的 USDT。
func TradeSpotBaseAsset(symbol string) string {
	return strings.TrimSuffix(symbol, "USDT")
}

// TradeSpotAssetDailyRate 是现在借 asset 的日利率(小数)。没有这个币的利率时不能借。
func TradeSpotAssetDailyRate(asset string) (decimal.Decimal, bool) {
	rate, ok := (*tradeSpotAssetRates.Load())[asset]
	return rate, ok
}

// SetTradeSpotAssetDailyRates 换上从 Binance 取到的各币种日利率，不在 [0, 1%] 里的值不用；这次没取到的币种沿用原来的。
func SetTradeSpotAssetDailyRates(rates map[string]decimal.Decimal) {
	next := make(map[string]decimal.Decimal, len(rates))
	for asset, rate := range *tradeSpotAssetRates.Load() {
		next[asset] = rate
	}
	for asset, rate := range rates {
		if !rate.IsNegative() && rate.LessThanOrEqual(decimal.New(1, -2)) {
			next[asset] = rate
		}
	}
	tradeSpotAssetRates.Store(&next)
}

// TradeSpotAssetLoan 是账户在一个现货交易对上借的币。
type TradeSpotAssetLoan struct {
	UserId int    `json:"user_id" gorm:"primaryKey;autoIncrement:false"`
	Symbol string `json:"symbol" gorm:"type:varchar(20);primaryKey"`
	// Principal 是借着的币，Interest 是已经收了、还没还的利息，都是数量(10^-8)。
	Principal int64 `json:"principal" gorm:"type:bigint;not null;default:0"`
	Interest  int64 `json:"interest" gorm:"type:bigint;not null;default:0"`
	// Proceeds 是借着的本金当初卖出到账的钱(额度单位)，还本金时按比例结转，只用来算开空均价与浮动盈亏，不参与记账。
	Proceeds int `json:"proceeds" gorm:"type:bigint;not null;default:0"`
	// ChargedUntil 是利息已经预收到的整点，没有借币时为 0；InterestRemainder 是不足 10^-8 的利息零头，留到下次再收。
	ChargedUntil      int64  `json:"charged_until" gorm:"bigint"`
	InterestRemainder string `json:"-" gorm:"type:varchar(64)"`
	// DailyRate 是最近一次收利息用的日利率(小数)。
	DailyRate string `json:"daily_rate" gorm:"type:varchar(32)"`
}

// Debt 是要还的币：本金加利息。
func (loan TradeSpotAssetLoan) Debt() int64 {
	return loan.Principal + loan.Interest
}

// GetTradeSpotAssetLoans 是这些用户还借着的币。
func GetTradeSpotAssetLoans(userIds []int) ([]TradeSpotAssetLoan, error) {
	var loans []TradeSpotAssetLoan
	if len(userIds) == 0 {
		return loans, nil
	}
	err := DB.Where("user_id IN ? AND (principal > 0 OR interest > 0)", userIds).Order("user_id, symbol").Find(&loans).Error
	return loans, err
}

// addTradeSpotAssetInterest 收 principal(10^-8)在 seconds 秒里的利息(按币)，利息记到 until 为止，不足 10^-8 的零头留到下次。
func addTradeSpotAssetInterest(loan *TradeSpotAssetLoan, principal int64, seconds int64, rate decimal.Decimal, until int64) (int64, error) {
	remainder, err := parseTradeValue(loan.InterestRemainder)
	if err != nil || remainder.IsNegative() || remainder.GreaterThanOrEqual(decimal.NewFromInt(1)) || principal < 0 || seconds < 0 || rate.IsNegative() {
		return 0, ErrTradeAmountInvalid
	}
	interest := decimal.NewFromInt(principal).Mul(rate).Mul(decimal.NewFromInt(seconds)).Div(decimal.NewFromInt(86400)).Add(remainder)
	charged := interest.Floor()
	if charged.GreaterThanOrEqual(decimal.NewFromInt(tradeSpotMaxAssetQty - loan.Principal - loan.Interest - principal)) {
		return 0, ErrTradeAmountInvalid
	}
	loan.Interest += charged.IntPart()
	loan.InterestRemainder = interest.Sub(charged).String()
	loan.ChargedUntil = until
	loan.DailyRate = rate.String()
	return charged.IntPart(), nil
}

// chargeTradeSpotAssetInterest 把利息收到 now 之后的第一个整点，用这个币现在的日利率；拿不到时沿用上次的。返回这次收了多少。
func chargeTradeSpotAssetInterest(loan *TradeSpotAssetLoan, now int64) (int64, error) {
	if loan.Principal <= 0 || now < loan.ChargedUntil {
		return 0, nil
	}
	rate, ok := TradeSpotAssetDailyRate(TradeSpotBaseAsset(loan.Symbol))
	if !ok {
		parsed, err := decimal.NewFromString(loan.DailyRate)
		if err != nil {
			return 0, ErrTradeAmountInvalid
		}
		rate = parsed
	}
	from := loan.ChargedUntil
	if from <= 0 {
		from = now
	}
	until := tradeSpotNextHour(now)
	return addTradeSpotAssetInterest(loan, loan.Principal, until-from, rate, until)
}

// AccruedTradeSpotAssetLoans 是只读的估值：算上到 now 该收还没收的利息，算法与真正收利息时相同。
func AccruedTradeSpotAssetLoans(loans []TradeSpotAssetLoan, now int64) ([]TradeSpotAssetLoan, error) {
	accrued := slices.Clone(loans)
	for i := range accrued {
		if accrued[i].Principal < 0 || accrued[i].Interest < 0 {
			return nil, ErrTradeAmountInvalid
		}
		if _, err := chargeTradeSpotAssetInterest(&accrued[i], now); err != nil {
			return nil, err
		}
	}
	return accrued, nil
}

func saveTradeSpotAssetLoanTx(tx *gorm.DB, loan *TradeSpotAssetLoan) error {
	if loan.Principal < 0 || loan.Interest < 0 || loan.Principal >= tradeSpotMaxAssetQty-loan.Interest || loan.Proceeds < 0 || loan.Proceeds >= common.MaxQuota {
		return ErrTradeAmountInvalid
	}
	return tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "user_id"}, {Name: "symbol"}}, UpdateAll: true}).Create(loan).Error
}

// chargeTradeSpotAssetLoanTx 在资金行锁内把一笔借币到期的利息收掉(写回、记一条账单)。
func chargeTradeSpotAssetLoanTx(tx *gorm.DB, account *TradeAccount, loan *TradeSpotAssetLoan) error {
	chargedUntil := loan.ChargedUntil
	charged, err := chargeTradeSpotAssetInterest(loan, common.GetTimestamp())
	if err != nil || loan.ChargedUntil == chargedUntil {
		return err
	}
	if err = saveTradeSpotAssetLoanTx(tx, loan); err != nil {
		return err
	}
	if charged > 0 {
		return tradeLedgerTx(tx, account, &TradeLedger{Type: TradeLedgerSpotAssetInterest, Symbol: loan.Symbol, Qty: charged, Price: loan.DailyRate})
	}
	return nil
}

// loadTradeSpotAssetLoansTx 在资金行锁内读出账户借着的全部币，把到期的利息收掉。
func loadTradeSpotAssetLoansTx(tx *gorm.DB, account *TradeAccount) ([]TradeSpotAssetLoan, error) {
	var loans []TradeSpotAssetLoan
	if err := tx.Where("user_id = ? AND (principal > 0 OR interest > 0)", account.UserId).Order("symbol").Find(&loans).Error; err != nil {
		return nil, err
	}
	for i := range loans {
		if err := chargeTradeSpotAssetLoanTx(tx, account, &loans[i]); err != nil {
			return nil, err
		}
	}
	return loans, nil
}

// loadTradeSpotAssetLoanTx 在资金行锁内读出账户在 symbol 上借的币并收掉到期的利息，没有时返回一份空的。
func loadTradeSpotAssetLoanTx(tx *gorm.DB, account *TradeAccount, symbol string) (*TradeSpotAssetLoan, error) {
	var loans []TradeSpotAssetLoan
	if err := tx.Where("user_id = ? AND symbol = ?", account.UserId, symbol).Limit(1).Find(&loans).Error; err != nil {
		return nil, err
	}
	if len(loans) == 0 {
		return &TradeSpotAssetLoan{UserId: account.UserId, Symbol: symbol}, nil
	}
	loan := &loans[0]
	return loan, chargeTradeSpotAssetLoanTx(tx, account, loan)
}

// borrowTradeSpotAssetTx 给借币卖出借 qty 个币(卖出 fill 里可卖持仓不够的部分)，这笔先收到下一个整点为止的利息(按币)，记一条借币
// 账单，开空到账按数量比例记下。借着的币按这笔的成交均价算，加上这笔不能超过单个交易对的持仓上限 maxCost(额度单位，0 表示不限)。
// 调用方已经锁住账户，卖出另外记账。
func borrowTradeSpotAssetTx(tx *gorm.DB, account *TradeAccount, order *TradeOrder, fill TradeFill, qty int64, maxCost int) error {
	rate, ok := TradeSpotAssetDailyRate(TradeSpotBaseAsset(order.Symbol))
	if !ok || qty <= 0 || qty > fill.Qty || fill.Fee > fill.Amount {
		return ErrTradeSpotFinancingInvalid
	}
	loan, err := loadTradeSpotAssetLoanTx(tx, account, order.Symbol)
	if err != nil {
		return err
	}
	if maxCost > 0 {
		exposure := decimal.NewFromInt(int64(fill.Amount)).Mul(decimal.NewFromInt(loan.Debt() + qty)).Div(decimal.NewFromInt(fill.Qty))
		if exposure.GreaterThan(decimal.NewFromInt(int64(maxCost))) {
			return ErrTradePositionLimit
		}
	}
	now := common.GetTimestamp()
	until := tradeSpotNextHour(now)
	charged, err := addTradeSpotAssetInterest(loan, qty, until-now, rate, until)
	if err != nil {
		return err
	}
	loan.Principal += qty
	loan.Proceeds += tradesim.ShareQuota(fill.Amount-fill.Fee, qty, fill.Qty)
	if err = saveTradeSpotAssetLoanTx(tx, loan); err != nil {
		return err
	}
	if err = tradeLedgerTx(tx, account, &TradeLedger{Type: TradeLedgerSpotAssetLoan, Symbol: order.Symbol, OrderId: order.Id, Qty: qty}); err != nil {
		return err
	}
	if charged > 0 {
		return tradeLedgerTx(tx, account, &TradeLedger{Type: TradeLedgerSpotAssetInterest, Symbol: order.Symbol, Qty: charged, Price: loan.DailyRate})
	}
	return nil
}

// repayTradeSpotAssetTx 用买到的 qty 个币先还这个交易对的借币，先还利息再还本金，返回还完剩下、可以进持仓的数量。本金还清之后
// 利息从头算，这一小时已经收的不退。
func repayTradeSpotAssetTx(tx *gorm.DB, account *TradeAccount, order *TradeOrder, qty int64) (int64, error) {
	loan, err := loadTradeSpotAssetLoanTx(tx, account, order.Symbol)
	if err != nil {
		return 0, err
	}
	paid := min(qty, loan.Debt())
	if paid <= 0 {
		return qty, nil
	}
	paidInterest := min(paid, loan.Interest)
	paidPrincipal := paid - paidInterest
	loan.Proceeds -= tradesim.ShareQuota(loan.Proceeds, paidPrincipal, loan.Principal)
	loan.Interest -= paidInterest
	loan.Principal -= paidPrincipal
	if loan.Principal == 0 {
		loan.ChargedUntil, loan.InterestRemainder, loan.Proceeds = 0, "", 0
	}
	if err = saveTradeSpotAssetLoanTx(tx, loan); err != nil {
		return 0, err
	}
	if err = tradeLedgerTx(tx, account, &TradeLedger{Type: TradeLedgerSpotAssetRepay, Symbol: order.Symbol, OrderId: order.Id, Qty: paid}); err != nil {
		return 0, err
	}
	return qty - paid, nil
}

// tradeSpotAssetDebtOf 是借着的币按新鲜卖一的价值(额度单位，向上取整)：买回来还掉要花的钱。有一个拿不到新鲜卖一就返回
// ErrTradeMarkUnavailable。
func tradeSpotAssetDebtOf(loans []TradeSpotAssetLoan, market TradeSpotPriceMarket) (int, error) {
	pricing, err := tradeFuturesPricing()
	if err != nil {
		return 0, err
	}
	value := decimal.Zero
	for _, loan := range loans {
		if loan.Debt() <= 0 {
			continue
		}
		if market == nil {
			return 0, ErrTradeMarkUnavailable
		}
		price, fresh := market.SpotAsk(loan.Symbol)
		if !fresh || !price.IsPositive() {
			return 0, ErrTradeMarkUnavailable
		}
		value = value.Add(price.Mul(tradesim.QtyFromUnits(loan.Debt())))
	}
	debt, err := pricing.DebitQuota(value)
	if err != nil {
		return 0, ErrTradeAmountInvalid
	}
	return debt, nil
}
