package model

import (
	"errors"
	"slices"
	"sync/atomic"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/tradesim"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// 现货杠杆照 Binance 全仓杠杆的经典模式：杠杆买入借 USDT，借币卖出借币(见 trade_spot_asset_loan.go)，账户里的全部现货和不被
// 全仓合约占用的资金一起作抵押。
//
// 风险率 = (不被全仓合约占用的资金 + 现货按买一的市值) / (借 USDT 的本金 + 利息 + 借着的币按卖一的价值)。风险率不高于 1.1 时强平；借款之后不能低于开仓线
// (3 倍账户 1.5，5 倍账户 1.25)；把钱转出去(转回额度、给合约或预测用)之后不能低于 2；跌到追加保证金线(3 倍 1.3，5 倍 1.16)
// 时提醒一次。
//
// 利息按整点收：借款时先收到下一个整点为止的，之后每到整点按当时的利率预收下一小时，提前还款不退。利率是 Binance 普通用户
// 借 USDT 的日利率，由服务层定时更新(见 SetTradeSpotDailyRate)。
const (
	// TradeSpotMaxLeverage 是现货杠杆最高的倍数，Binance 全仓经典模式的账户最高 5 倍。
	TradeSpotMaxLeverage = 5
	// TradeSpotLiquidationFeeBps 是强平费，按强平时还掉的借款收 2%(Binance 全仓与逐仓杠杆都是 2%)，从账户的资金里扣。
	TradeSpotLiquidationFeeBps = 200

	TradeLedgerSpotLoan           = "spot_loan"
	TradeLedgerSpotInterest       = "spot_interest"
	TradeLedgerSpotRepay          = "spot_repay"
	TradeLedgerSpotLiquidationFee = "spot_liq_fee"
	TradeLedgerSpotCover          = "spot_cover"

	// tradeSpotDefaultDailyRate 是 2026-10-08 Binance 普通用户借 USDT 的日利率，取到实时的之前先用它。
	tradeSpotDefaultDailyRate = "0.00013778"
)

var (
	TradeSpotLiquidationLevel = decimal.RequireFromString("1.1")
	TradeSpotTransferLevel    = decimal.NewFromInt(2)

	ErrTradeSpotFinancingInvalid = errors.New("spot leverage is only available on market buys by quantity")
	// ErrTradeSpotMarginLevel 表示这笔操作会让风险率低于允许的线：借款后低于开仓线，或者买入后不高于强平线。
	ErrTradeSpotMarginLevel = errors.New("spot margin level is too low")
)

var tradeSpotDailyRate atomic.Pointer[decimal.Decimal]

func init() {
	rate := decimal.RequireFromString(tradeSpotDefaultDailyRate)
	tradeSpotDailyRate.Store(&rate)
}

// TradeSpotDailyRate 是现在借 USDT 的日利率(小数，0.00013778 即每天 0.013778%)。
func TradeSpotDailyRate() decimal.Decimal {
	return *tradeSpotDailyRate.Load()
}

// SetTradeSpotDailyRate 换上从 Binance 取到的日利率，不在 [0, 1%] 里的值不用。
func SetTradeSpotDailyRate(rate decimal.Decimal) {
	if rate.IsNegative() || rate.GreaterThan(decimal.New(1, -2)) {
		return
	}
	tradeSpotDailyRate.Store(&rate)
}

// TradeSpotInitialLevel 是借款之后风险率不能低于的开仓线，账户最高杠杆是 L 倍时为 L/(L−1)：3 倍 1.5，5 倍 1.25。
func TradeSpotInitialLevel(maxLeverage int) decimal.Decimal {
	if maxLeverage <= 1 {
		return decimal.Zero
	}
	leverage := decimal.NewFromInt(int64(maxLeverage))
	return leverage.Div(leverage.Sub(decimal.NewFromInt(1)))
}

// TradeSpotMarginCallLevel 是追加保证金的提醒线：5 倍账户 1.16，其余按 3 倍账户 1.3。
func TradeSpotMarginCallLevel(maxLeverage int) decimal.Decimal {
	if maxLeverage >= 5 {
		return decimal.RequireFromString("1.16")
	}
	return decimal.RequireFromString("1.3")
}

// TradeSpotMargin 是账户的现货借款。卖出任何现货的钱先还利息、再还本金；借款不算转入的本金。
type TradeSpotMargin struct {
	UserId    int `json:"user_id" gorm:"primaryKey;autoIncrement:false"`
	Principal int `json:"principal" gorm:"type:bigint;not null;default:0"`
	// Interest 是已经收了、还没还的利息。
	Interest int `json:"interest" gorm:"type:bigint;not null;default:0"`
	// ChargedUntil 是利息已经预收到的整点，没有借款时为 0。
	ChargedUntil int64 `json:"charged_until" gorm:"bigint"`
	// InterestRemainder 是不足一个额度单位的利息零头，留到下次再收。
	InterestRemainder string `json:"-" gorm:"type:varchar(64)"`
	// DailyRate 是最近一次收利息用的日利率(小数)。
	DailyRate string `json:"daily_rate" gorm:"type:varchar(32)"`
	// MarginCallAt 是风险率跌到追加保证金线、发出提醒的时间；回到线上之后清零，同一次下跌只提醒一次。
	MarginCallAt int64 `json:"margin_call_at" gorm:"bigint"`
}

// TradeSpotPriceMarket 给现货估值：SpotPrice 是买一价(持仓的抵押价值)，SpotAsk 是卖一价(借着的币买回来的价钱)，以及它们是否
// 新鲜。过期的价格不能用来放款、转出或强平。
type TradeSpotPriceMarket interface {
	SpotPrice(symbol string) (decimal.Decimal, bool)
	SpotAsk(symbol string) (decimal.Decimal, bool)
}

func GetTradeSpotMargin(userId int) (TradeSpotMargin, error) {
	var rows []TradeSpotMargin
	if err := DB.Where("user_id = ?", userId).Limit(1).Find(&rows).Error; err != nil {
		return TradeSpotMargin{}, err
	}
	if len(rows) == 0 {
		return TradeSpotMargin{UserId: userId}, nil
	}
	return rows[0], nil
}

func ListTradeSpotMarginsOf(userIds []int) ([]TradeSpotMargin, error) {
	var loans []TradeSpotMargin
	if len(userIds) == 0 {
		return loans, nil
	}
	err := DB.Where("user_id IN ?", userIds).Find(&loans).Error
	return loans, err
}

// ListTradeSpotMarginUsers 是借着 USDT 或者借着币的用户，按 user_id 升序。
func ListTradeSpotMarginUsers() ([]int, error) {
	var users, borrowers []int
	if err := DB.Model(&TradeSpotMargin{}).Where("principal > 0 OR interest > 0").Pluck("user_id", &users).Error; err != nil {
		return nil, err
	}
	if err := DB.Model(&TradeSpotAssetLoan{}).Where("principal > 0 OR interest > 0").Distinct("user_id").Pluck("user_id", &borrowers).Error; err != nil {
		return nil, err
	}
	users = append(users, borrowers...)
	slices.Sort(users)
	return slices.Compact(users), nil
}

// ListTradeSpotPositionSymbols 是有人持有或者借着币的现货交易对：交易对关了也要给他们行情，好让他们卖出、买回还币、还款，也让
// 强平能按盘口进行。
func ListTradeSpotPositionSymbols() ([]string, error) {
	var symbols, borrowed []string
	if err := DB.Model(&TradePosition{}).Where("qty > 0").Distinct("symbol").Pluck("symbol", &symbols).Error; err != nil {
		return nil, err
	}
	if err := DB.Model(&TradeSpotAssetLoan{}).Where("principal > 0 OR interest > 0").Distinct("symbol").Pluck("symbol", &borrowed).Error; err != nil {
		return nil, err
	}
	symbols = append(symbols, borrowed...)
	slices.Sort(symbols)
	return slices.Compact(symbols), nil
}

// tradeSpotNextHour 是 now 之后的第一个整点。
func tradeSpotNextHour(now int64) int64 {
	return now/3600*3600 + 3600
}

// addTradeSpotInterest 收 principal 在 seconds 秒里的利息，利息记到 until 为止，不足一个额度单位的零头留到下次。返回收了多少。
func addTradeSpotInterest(loan *TradeSpotMargin, principal int, seconds int64, rate decimal.Decimal, until int64) (int, error) {
	remainder, err := parseTradeValue(loan.InterestRemainder)
	if err != nil || remainder.IsNegative() || remainder.GreaterThanOrEqual(decimal.NewFromInt(1)) || principal < 0 || seconds < 0 {
		return 0, ErrTradeAmountInvalid
	}
	interest := decimal.NewFromInt(int64(principal)).Mul(rate).Mul(decimal.NewFromInt(seconds)).Div(decimal.NewFromInt(86400)).Add(remainder)
	charged, err := common.QuotaFromDecimalStrict(interest.Floor())
	if err != nil || charged < 0 || charged >= common.MaxQuota-loan.Principal-loan.Interest {
		return 0, ErrTradeAmountInvalid
	}
	loan.Interest += charged
	loan.InterestRemainder = interest.Sub(interest.Floor()).String()
	loan.ChargedUntil = until
	loan.DailyRate = rate.String()
	return charged, nil
}

// chargeTradeSpotInterest 把利息收到 now 之后的第一个整点：还没到已经收到的整点时什么也不做；节点停过、漏收的整点按现在的
// 利率一起补上。返回这次收了多少。
func chargeTradeSpotInterest(loan *TradeSpotMargin, now int64, rate decimal.Decimal) (int, error) {
	if loan.Principal <= 0 || now < loan.ChargedUntil {
		return 0, nil
	}
	from := loan.ChargedUntil
	if from <= 0 {
		from = now
	}
	until := tradeSpotNextHour(now)
	return addTradeSpotInterest(loan, loan.Principal, until-from, rate, until)
}

// AccruedTradeSpotMargin 是只读的估值：算上到 now 该收还没收的利息，算法与真正收利息时相同。
func AccruedTradeSpotMargin(loan TradeSpotMargin, now int64) (TradeSpotMargin, error) {
	if loan.Principal < 0 || loan.Interest < 0 || loan.Principal >= common.MaxQuota-loan.Interest {
		return loan, ErrTradeAmountInvalid
	}
	_, err := chargeTradeSpotInterest(&loan, now, TradeSpotDailyRate())
	return loan, err
}

func saveTradeSpotMarginTx(tx *gorm.DB, loan *TradeSpotMargin) error {
	if loan.Principal < 0 || loan.Interest < 0 || loan.Principal >= common.MaxQuota-loan.Interest {
		return ErrTradeAmountInvalid
	}
	return tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "user_id"}}, UpdateAll: true}).Create(loan).Error
}

// loadTradeSpotMarginTx 在资金行锁内读出账户的借款，把到期的利息收掉(写回借款、记一条账单)。没有借款时返回一份空的。
func loadTradeSpotMarginTx(tx *gorm.DB, account *TradeAccount) (*TradeSpotMargin, error) {
	var rows []TradeSpotMargin
	if err := tx.Where("user_id = ?", account.UserId).Limit(1).Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return &TradeSpotMargin{UserId: account.UserId}, nil
	}
	loan := &rows[0]
	chargedUntil := loan.ChargedUntil
	charged, err := chargeTradeSpotInterest(loan, common.GetTimestamp(), TradeSpotDailyRate())
	if err != nil {
		return nil, err
	}
	if loan.ChargedUntil == chargedUntil {
		return loan, nil
	}
	if err = saveTradeSpotMarginTx(tx, loan); err != nil {
		return nil, err
	}
	if charged > 0 {
		err = tradeLedgerTx(tx, account, &TradeLedger{Type: TradeLedgerSpotInterest, Fee: charged, Pnl: -charged, Price: loan.DailyRate})
	}
	return loan, err
}

// borrowTradeSpotMarginTx 给杠杆买入借款：借来的钱进资金，这笔先收到下一个整点为止的利息。调用方已经锁住账户、检查过风险率。
func borrowTradeSpotMarginTx(tx *gorm.DB, account *TradeAccount, order *TradeOrder) error {
	loan, err := loadTradeSpotMarginTx(tx, account)
	if err != nil {
		return err
	}
	if order.Borrowed <= 0 || order.Borrowed >= common.MaxQuota-loan.Principal-loan.Interest || account.Cash >= common.MaxQuota-order.Borrowed {
		return ErrTradeAmountInvalid
	}
	now := common.GetTimestamp()
	until := tradeSpotNextHour(now)
	charged, err := addTradeSpotInterest(loan, order.Borrowed, until-now, TradeSpotDailyRate(), until)
	if err != nil {
		return err
	}
	loan.Principal += order.Borrowed
	account.Cash += order.Borrowed
	if err = saveTradeSpotMarginTx(tx, loan); err != nil {
		return err
	}
	if err = tradeLedgerTx(tx, account, &TradeLedger{Type: TradeLedgerSpotLoan, Symbol: order.Symbol, OrderId: order.Id, Amount: order.Borrowed}); err != nil {
		return err
	}
	if charged > 0 {
		return tradeLedgerTx(tx, account, &TradeLedger{Type: TradeLedgerSpotInterest, Fee: charged, Pnl: -charged, Price: loan.DailyRate})
	}
	return nil
}

// repayTradeSpotMarginTx 用资金还借款，先还利息再还本金，最多还 amount，返回还了多少。本金还清之后利息从头算，这一小时已经
// 收的不退。资金可以因此变成负的，调用方负责(卖出到账的钱只会还掉不超过到账的数)。
func repayTradeSpotMarginTx(tx *gorm.DB, account *TradeAccount, amount int, orderId int) (int, error) {
	loan, err := loadTradeSpotMarginTx(tx, account)
	if err != nil {
		return 0, err
	}
	if amount <= 0 || loan.Principal+loan.Interest == 0 {
		return 0, nil
	}
	paidInterest := min(amount, loan.Interest)
	paidPrincipal := min(amount-paidInterest, loan.Principal)
	paid := paidInterest + paidPrincipal
	loan.Interest -= paidInterest
	loan.Principal -= paidPrincipal
	account.Cash -= paid
	if loan.Principal == 0 {
		loan.ChargedUntil, loan.InterestRemainder, loan.MarginCallAt = 0, "", 0
	}
	if err = saveTradeSpotMarginTx(tx, loan); err != nil {
		return 0, err
	}
	return paid, tradeLedgerTx(tx, account, &TradeLedger{Type: TradeLedgerSpotRepay, OrderId: orderId, Amount: -paid})
}

// TradeSpotRisk 是现货借款的风险(额度单位)：Debt 是借 USDT 的本金加利息再加上借着的币按卖一的价值(AssetDebt)，Cash 是不被全仓
// 合约占用的资金，Assets 是现货按买一的市值。
type TradeSpotRisk struct {
	Debt      int `json:"debt"`
	AssetDebt int `json:"asset_debt"`
	Cash      int `json:"cash"`
	Assets    int `json:"assets"`
}

// Level 是风险率 (Cash + Assets) / Debt，没有借款时为 0。
func (r TradeSpotRisk) Level() decimal.Decimal {
	if r.Debt <= 0 {
		return decimal.Zero
	}
	return decimal.NewFromInt(int64(r.Cash) + int64(r.Assets)).Div(decimal.NewFromInt(int64(r.Debt)))
}

// AtOrBelow 表示有借款而且风险率不高于 level。
func (r TradeSpotRisk) AtOrBelow(level decimal.Decimal) bool {
	return r.Debt > 0 && decimal.NewFromInt(int64(r.Cash)+int64(r.Assets)).LessThanOrEqual(level.Mul(decimal.NewFromInt(int64(r.Debt))))
}

// Reserve 是风险率要不低于 level 时资金至少要留多少：level × Debt(向上取整)减去现货市值，最少为 0。
func (r TradeSpotRisk) Reserve(level decimal.Decimal) int {
	if r.Debt <= 0 {
		return 0
	}
	return max(0, common.QuotaFromDecimal(level.Mul(decimal.NewFromInt(int64(r.Debt))).Ceil())-r.Assets)
}

// tradeSpotAssetsOf 是现货按新鲜买一的市值(额度单位，向下取整)。有一个持仓拿不到新鲜买一就返回 ErrTradeMarkUnavailable。
func tradeSpotAssetsOf(positions []TradePosition, market TradeSpotPriceMarket) (int, error) {
	pricing, err := tradeFuturesPricing()
	if err != nil {
		return 0, err
	}
	value := decimal.Zero
	for _, position := range positions {
		if position.Qty <= 0 {
			continue
		}
		if market == nil {
			return 0, ErrTradeMarkUnavailable
		}
		price, fresh := market.SpotPrice(position.Symbol)
		if !fresh || !price.IsPositive() {
			return 0, ErrTradeMarkUnavailable
		}
		value = value.Add(price.Mul(tradesim.QtyFromUnits(position.Qty)))
	}
	assets, err := pricing.CreditQuota(value)
	if err != nil {
		return 0, ErrTradeAmountInvalid
	}
	return assets, nil
}

// TradeSpotRiskOf 算出现货借款的风险，loans 是借着的币，cash 是不被全仓合约占用的资金。既没借 USDT 也没借币时不给现货估值，
// 也就不需要行情。
func TradeSpotRiskOf(loan TradeSpotMargin, loans []TradeSpotAssetLoan, cash int, positions []TradePosition, market TradeSpotPriceMarket) (TradeSpotRisk, error) {
	risk := TradeSpotRisk{Debt: loan.Principal + loan.Interest, Cash: max(0, cash)}
	if loan.Principal < 0 || loan.Interest < 0 || risk.Debt >= common.MaxQuota {
		return risk, ErrTradeAmountInvalid
	}
	borrowing := slices.ContainsFunc(loans, func(loan TradeSpotAssetLoan) bool { return loan.Debt() > 0 })
	if risk.Debt == 0 && !borrowing {
		return risk, nil
	}
	assetDebt, err := tradeSpotAssetDebtOf(loans, market)
	if err != nil {
		return risk, err
	}
	if assetDebt >= common.MaxQuota-risk.Debt {
		return risk, ErrTradeAmountInvalid
	}
	risk.AssetDebt = assetDebt
	risk.Debt += assetDebt
	risk.Assets, err = tradeSpotAssetsOf(positions, market)
	return risk, err
}

// tradeSpotRiskTx 在资金行锁内收掉到期的利息(USDT 与借着的币)，算出现货借款的风险。没有借款时不需要行情。
func tradeSpotRiskTx(tx *gorm.DB, account *TradeAccount, market TradeFuturesMarket) (TradeSpotRisk, *TradeSpotMargin, error) {
	loan, err := loadTradeSpotMarginTx(tx, account)
	if err != nil {
		return TradeSpotRisk{}, nil, err
	}
	loans, err := loadTradeSpotAssetLoansTx(tx, account)
	if err != nil {
		return TradeSpotRisk{}, loan, err
	}
	if loan.Principal+loan.Interest == 0 && len(loans) == 0 {
		return TradeSpotRisk{}, loan, nil
	}
	cross, err := tradeCrossStateTx(tx, account.UserId, market)
	if err != nil {
		return TradeSpotRisk{}, loan, err
	}
	var positions []TradePosition
	if err = tx.Where("user_id = ? AND qty > 0", account.UserId).Find(&positions).Error; err != nil {
		return TradeSpotRisk{}, loan, err
	}
	spotMarket, _ := market.(TradeSpotPriceMarket)
	risk, err := TradeSpotRiskOf(*loan, loans, cross.Withdrawable(account.Cash), positions, spotMarket)
	return risk, loan, err
}

// TradeSpotRiskRead 是不加锁读出的现货借款情况，给定时检查先筛一遍。Loan 与 Loans 是库里原样的，ChargedUntil 还是上次收利息
// 时的；Risk 算上了到期还没收的利息。
type TradeSpotRiskRead struct {
	Risk      TradeSpotRisk
	Loan      TradeSpotMargin
	Loans     []TradeSpotAssetLoan
	Positions []TradePosition
}

// NextCharge 是下一次要收利息的时间：借 USDT 与借着的币里最早的那个整点，没有借款时为 0。
func (r TradeSpotRiskRead) NextCharge() int64 {
	next := int64(0)
	if r.Loan.Principal > 0 {
		next = r.Loan.ChargedUntil
	}
	for _, loan := range r.Loans {
		if loan.Principal > 0 && (next == 0 || loan.ChargedUntil < next) {
			next = loan.ChargedUntil
		}
	}
	return next
}

// ReadTradeSpotRisk 不加锁地读出借款、借着的币与现货持仓，估出现货借款的风险(算上到期还没收的利息)。
func ReadTradeSpotRisk(userId int, market TradeFuturesMarket) (TradeSpotRiskRead, error) {
	var read TradeSpotRiskRead
	loan, err := GetTradeSpotMargin(userId)
	if err != nil {
		return read, err
	}
	read.Loan = loan
	if read.Loans, err = GetTradeSpotAssetLoans([]int{userId}); err != nil {
		return read, err
	}
	now := common.GetTimestamp()
	accrued, err := AccruedTradeSpotMargin(loan, now)
	if err != nil {
		return read, err
	}
	accruedLoans, err := AccruedTradeSpotAssetLoans(read.Loans, now)
	if err != nil {
		return read, err
	}
	account, err := GetTradeAccount(userId)
	if err != nil {
		return read, err
	}
	cross, err := tradeCrossStateTx(DB, userId, market)
	if err != nil {
		return read, err
	}
	if err = DB.Where("user_id = ? AND qty > 0", userId).Find(&read.Positions).Error; err != nil {
		return read, err
	}
	spotMarket, _ := market.(TradeSpotPriceMarket)
	read.Risk, err = TradeSpotRiskOf(accrued, accruedLoans, cross.Withdrawable(account.Cash), read.Positions, spotMarket)
	return read, err
}

// tradeSpotTransferReserveTx 是有现货借款时把钱转出去(转回额度、给合约或预测用)必须留下的资金：转出之后风险率不能低于 2。
func tradeSpotTransferReserveTx(tx *gorm.DB, account *TradeAccount, market TradeFuturesMarket) (int, error) {
	risk, _, err := tradeSpotRiskTx(tx, account, market)
	if err != nil {
		return 0, err
	}
	return risk.Reserve(TradeSpotTransferLevel), nil
}

// checkTradeSpotBorrowTx 检查杠杆买入能不能借 borrowed：照 Binance，借款之后的风险率(借来的钱先算进资金)不能低于账户最高
// 杠杆的开仓线。没有借款的账户只要保证金是自己出的就总能借；已经借过的账户风险率低于开仓线时不能再借。
func checkTradeSpotBorrowTx(tx *gorm.DB, account *TradeAccount, borrowed int, maxLeverage int, market TradeFuturesMarket) error {
	if maxLeverage <= 1 {
		return ErrTradeSpotFinancingInvalid
	}
	loan, err := loadTradeSpotMarginTx(tx, account)
	if err != nil {
		return err
	}
	cross, err := tradeCrossStateTx(tx, account.UserId, market)
	if err != nil {
		return err
	}
	var positions []TradePosition
	if err = tx.Where("user_id = ? AND qty > 0", account.UserId).Find(&positions).Error; err != nil {
		return err
	}
	loans, err := loadTradeSpotAssetLoansTx(tx, account)
	if err != nil {
		return err
	}
	spotMarket, _ := market.(TradeSpotPriceMarket)
	assets, err := tradeSpotAssetsOf(positions, spotMarket)
	if err != nil {
		return err
	}
	assetDebt, err := tradeSpotAssetDebtOf(loans, spotMarket)
	if err != nil {
		return err
	}
	after := TradeSpotRisk{Debt: loan.Principal + loan.Interest + assetDebt + borrowed, Cash: cross.Withdrawable(account.Cash) + borrowed, Assets: assets}
	if after.Level().LessThan(TradeSpotInitialLevel(maxLeverage)) {
		return ErrTradeSpotMarginLevel
	}
	return nil
}

func RepayTradeSpotMargin(userId int, amount int, market TradeFuturesMarket) (*TradeSpotMargin, error) {
	if amount <= 0 || amount >= common.MaxQuota {
		return nil, ErrTradeAmountInvalid
	}
	var loan *TradeSpotMargin
	err := DB.Transaction(func(tx *gorm.DB) error {
		account, err := lockTradeAccountTx(tx, userId)
		if err != nil {
			return err
		}
		loan, err = loadTradeSpotMarginTx(tx, account)
		if err != nil {
			return err
		}
		amount = min(amount, loan.Principal+loan.Interest)
		if amount == 0 {
			return nil
		}
		// 还款只用不被全仓合约占用的资金。
		cross, err := tradeCrossStateTx(tx, userId, market)
		if err != nil {
			return err
		}
		if amount > cross.Withdrawable(account.Cash) {
			return ErrTradeCashInsufficient
		}
		if _, err = repayTradeSpotMarginTx(tx, account, amount, 0); err != nil {
			return err
		}
		if err = saveTradeAccountTx(tx, account); err != nil {
			return err
		}
		loan, err = loadTradeSpotMarginTx(tx, account)
		return err
	})
	return loan, err
}

// TradeSpotBook 是强平时一个交易对能吃的盘口：卖出现货吃买盘 Bids，买回借着的币吃卖盘 Asks，最优价在前，已经扣掉同一份盘口上
// 别的委托吃掉的数量。Step 是数量步长。
type TradeSpotBook struct {
	Symbol string
	Bids   []tradesim.Level
	Asks   []tradesim.Level
	Step   decimal.Decimal
}

// TradeSpotSettlement 是一次定时检查做的事：Canceled 是强平时撤掉的委托，Fills 与 BuyFills 是强平卖出现货、买回借着的币在各
// 交易对盘口上的成交(给行情中心记下吃掉的数量)，Covered 是从站内额度补的亏空。
type TradeSpotSettlement struct {
	Canceled   []TradeOrder
	Fills      map[string]tradesim.Fill
	BuyFills   map[string]tradesim.Fill
	Liquidated bool
	Covered    int
}

// SettleTradeSpotMargin 是现货借款的定时检查，在资金行锁内做：收掉到期的利息(USDT 与借着的币)；风险率不高于追加保证金线时提醒
// 一次，回到线上之后清掉记号；不高于 1.1 时强平。强平先撤掉现货挂单，风险率还是不高于 1.1 就要还清全部借款、再按还掉的价值付
// 2% 的强平费：先用不被全仓合约占用的资金，不够的按 books 的顺序(流动性好的在前)在买盘上逐档卖出现货凑钱，再在卖盘上把借着的
// 币买回来还掉(买回不看资金够不够)，最后用资金还 USDT 借款。books 为空时只检查不强平。盘口吃不下、还剩现货卖不掉或者币买不回来
// 的，借款还到哪算哪，下一次接着来；现货卖光还不够的从资金里扣，资金扣成负数又没有全仓仓位撑着时从站内额度补。
func SettleTradeSpotMargin(userId int, market TradeFuturesMarket, books []TradeSpotBook, pricing tradesim.Pricing, maxLeverage int) (TradeSpotSettlement, error) {
	var result TradeSpotSettlement
	err := DB.Transaction(func(tx *gorm.DB) error {
		result = TradeSpotSettlement{}
		account, err := lockTradeAccountTx(tx, userId)
		if err != nil {
			return err
		}
		risk, loan, err := tradeSpotRiskTx(tx, account, market)
		if err != nil {
			return err
		}
		if risk.Debt == 0 {
			return nil
		}
		if !risk.AtOrBelow(TradeSpotLiquidationLevel) || len(books) == 0 {
			return noteTradeSpotMarginCallTx(tx, loan, risk, maxLeverage)
		}
		var orders []TradeOrder
		if err = tx.Where("user_id = ? AND status = ?", userId, TradeOrderStatusOpen).Find(&orders).Error; err != nil {
			return err
		}
		for i := range orders {
			finishTradeOrder(account, &orders[i], TradeOrderStatusCanceled, TradeCancelByLiquidation)
			if err = saveTradeOrderTx(tx, &orders[i]); err != nil {
				return err
			}
		}
		result.Canceled = orders
		var positions []TradePosition
		if err = tx.Where("user_id = ? AND qty > 0", userId).Find(&positions).Error; err != nil {
			return err
		}
		for i := range positions {
			if positions[i].FrozenQty > 0 {
				positions[i].FrozenQty = 0
				if err = saveTradePositionTx(tx, &positions[i]); err != nil {
					return err
				}
			}
		}
		// 撤单退回的资金可能已经够了。
		if risk, loan, err = tradeSpotRiskTx(tx, account, market); err != nil {
			return err
		}
		if !risk.AtOrBelow(TradeSpotLiquidationLevel) {
			if err = noteTradeSpotMarginCallTx(tx, loan, risk, maxLeverage); err != nil {
				return err
			}
			return saveTradeAccountTx(tx, account)
		}
		// 买回借着的币要花的钱按现在的卖盘估：欠的数量向上取到步长，加上手续费。
		usdtDebt := loan.Principal + loan.Interest
		loans, err := loadTradeSpotAssetLoansTx(tx, account)
		if err != nil {
			return err
		}
		booksBySymbol := make(map[string]TradeSpotBook, len(books))
		for _, book := range books {
			booksBySymbol[book.Symbol] = book
		}
		buybacks := make(map[string]tradesim.Fill, len(loans))
		buybackAmount, buybackCost := 0, 0
		for _, assetLoan := range loans {
			book, ok := booksBySymbol[assetLoan.Symbol]
			if !ok || assetLoan.Debt() <= 0 {
				continue
			}
			qty := tradesim.QtyFromUnits(assetLoan.Debt())
			if book.Step.IsPositive() {
				qty = qty.Div(book.Step).Ceil().Mul(book.Step)
			}
			fill := tradesim.Walk(book.Asks, tradesim.Buy, qty, decimal.Zero, book.Step)
			if !fill.Qty.IsPositive() {
				continue
			}
			amount, fee, err := pricing.BuyCost(fill)
			if err != nil {
				return ErrTradeAmountInvalid
			}
			buybacks[assetLoan.Symbol] = fill
			buybackAmount += amount
			buybackCost += amount + fee
		}
		shortfall := usdtDebt + buybackCost + tradeSpotLiquidationFee(usdtDebt+buybackAmount) - risk.Cash
		bySymbol := make(map[string]*TradePosition, len(positions))
		for i := range positions {
			bySymbol[positions[i].Symbol] = &positions[i]
		}
		result.Fills = map[string]tradesim.Fill{}
		for _, book := range books {
			position := bySymbol[book.Symbol]
			if shortfall <= 0 || position == nil || position.Qty <= 0 {
				continue
			}
			fill, amount, fee, err := pricing.SellForProceeds(book.Bids, tradesim.QtyFromUnits(position.Qty), shortfall, book.Step)
			if err != nil {
				return ErrTradeAmountInvalid
			}
			if !fill.Qty.IsPositive() {
				continue
			}
			order := TradeOrder{UserId: userId, Symbol: book.Symbol, Side: TradeSideSell, Type: TradeOrderTypeMarket, Qty: tradesim.QtyUnits(fill.Qty),
				Leverage: 1, Status: TradeOrderStatusOpen, CreatedAt: common.GetTimestamp()}
			if err = tx.Create(&order).Error; err != nil {
				return err
			}
			price := fill.AvgPrice().Round(tradesim.QtyDecimals).String()
			if err = applyTradeFillTx(tx, account, position, &order, TradeFill{Qty: order.Qty, Value: fill.Notional, Amount: amount, Fee: fee, Price: price}); err != nil {
				return err
			}
			finishTradeOrder(account, &order, TradeOrderStatusFilled, TradeCancelByLiquidation)
			if err = saveTradeOrderTx(tx, &order); err != nil {
				return err
			}
			if err = saveTradePositionTx(tx, position); err != nil {
				return err
			}
			if err = addTradeNoticeTx(tx, TradeNotice{UserId: userId, Kind: TradeNoticeLiquidation, Market: TradeNoticeSpot,
				Symbol: book.Symbol, Side: TradeSideSell, OrderId: order.Id, Qty: order.Qty, Price: price}); err != nil {
				return err
			}
			result.Fills[book.Symbol] = fill
			shortfall -= amount - fee
		}
		// 买回借着的币：成交时先还这个币的借币，多买的零头进持仓。
		result.BuyFills = map[string]tradesim.Fill{}
		bought := 0
		for _, book := range books {
			fill, ok := buybacks[book.Symbol]
			if !ok {
				continue
			}
			amount, fee, err := pricing.BuyCost(fill)
			if err != nil {
				return ErrTradeAmountInvalid
			}
			position, err := loadTradePositionTx(tx, userId, book.Symbol)
			if err != nil {
				return err
			}
			order := TradeOrder{UserId: userId, Symbol: book.Symbol, Side: TradeSideBuy, Type: TradeOrderTypeMarket, Qty: tradesim.QtyUnits(fill.Qty),
				Leverage: 1, Status: TradeOrderStatusOpen, CreatedAt: common.GetTimestamp()}
			if err = tx.Create(&order).Error; err != nil {
				return err
			}
			price := fill.AvgPrice().Round(tradesim.QtyDecimals).String()
			if err = applyTradeFill(tx, account, position, &order, TradeFill{Qty: order.Qty, Value: fill.Notional, Amount: amount, Fee: fee, Price: price}, true); err != nil {
				return err
			}
			finishTradeOrder(account, &order, TradeOrderStatusFilled, TradeCancelByLiquidation)
			if err = saveTradeOrderTx(tx, &order); err != nil {
				return err
			}
			if err = saveTradePositionTx(tx, position); err != nil {
				return err
			}
			if err = addTradeNoticeTx(tx, TradeNotice{UserId: userId, Kind: TradeNoticeLiquidation, Market: TradeNoticeSpot,
				Symbol: book.Symbol, Side: TradeSideBuy, OrderId: order.Id, Qty: order.Qty, Price: price}); err != nil {
				return err
			}
			result.BuyFills[book.Symbol] = fill
			bought += amount
		}
		// 卖出到账的钱已经自动还了借款，剩下的用资金还：现货卖光了就全还(资金可能扣成负的)，盘口吃不下、还有现货的只用空闲资金。
		if loan, err = loadTradeSpotMarginTx(tx, account); err != nil {
			return err
		}
		rest := loan.Principal + loan.Interest
		soldOut := true
		for _, position := range bySymbol {
			if position.Qty > 0 {
				soldOut = false
			}
		}
		if !soldOut {
			cross, err := tradeCrossStateTx(tx, userId, market)
			if err != nil {
				return err
			}
			rest = min(rest, cross.Withdrawable(account.Cash))
		}
		if _, err = repayTradeSpotMarginTx(tx, account, rest, 0); err != nil {
			return err
		}
		if loan, err = loadTradeSpotMarginTx(tx, account); err != nil {
			return err
		}
		if fee := tradeSpotLiquidationFee(usdtDebt - loan.Principal - loan.Interest + bought); fee > 0 {
			account.Cash -= fee
			if err = tradeLedgerTx(tx, account, &TradeLedger{Type: TradeLedgerSpotLiquidationFee, Amount: -fee, Fee: fee, Pnl: -fee}); err != nil {
				return err
			}
		}
		if result.Covered, err = coverTradeDeficitTx(tx, account, TradeLedgerSpotCover, TradeNoticeSpot); err != nil {
			return err
		}
		result.Liquidated = true
		return saveTradeAccountTx(tx, account)
	})
	if err != nil {
		return TradeSpotSettlement{}, err
	}
	afterTradeDeficitCovered(userId, result.Covered)
	return result, nil
}

// tradeSpotLiquidationFee 是还掉 repaid 的借款要付的强平费(向上取整)。
func tradeSpotLiquidationFee(repaid int) int {
	if repaid <= 0 {
		return 0
	}
	return common.QuotaFromDecimal(decimal.NewFromInt(int64(repaid)).Mul(decimal.NewFromInt(TradeSpotLiquidationFeeBps)).Div(decimal.NewFromInt(10000)).Ceil())
}

// noteTradeSpotMarginCallTx 在风险率跌到追加保证金线时记下时间、发一条提醒；回到线上之后清掉记号，下次跌到时再提醒。
func noteTradeSpotMarginCallTx(tx *gorm.DB, loan *TradeSpotMargin, risk TradeSpotRisk, maxLeverage int) error {
	below := risk.AtOrBelow(TradeSpotMarginCallLevel(maxLeverage))
	switch {
	case below && loan.MarginCallAt == 0:
		loan.MarginCallAt = common.GetTimestamp()
		if err := saveTradeSpotMarginTx(tx, loan); err != nil {
			return err
		}
		return addTradeNoticeTx(tx, TradeNotice{UserId: loan.UserId, Kind: TradeNoticeMarginCall, Market: TradeNoticeSpot,
			Price: risk.Level().StringFixed(4), Amount: risk.Debt})
	case !below && loan.MarginCallAt != 0:
		loan.MarginCallAt = 0
		return saveTradeSpotMarginTx(tx, loan)
	}
	return nil
}
