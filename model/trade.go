package model

import (
	"errors"
	"math/big"

	"github.com/QuantumNous/new-api/common"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// 模拟盘账户按 Binance 现货的实时盘口买卖，以额度单位记账(1 美元 = common.QuotaPerUnit)，数量按 10^-8 记成整数
// (见 pkg/tradesim)。账户里的钱来自用户转入的额度，也能转出成额度，所以每一笔资金变动都在一个事务里完成，并且先锁住
// 账户行：同一用户的转入转出、下单、成交与撤单依次进行，持仓与委托不再单独加锁。
//
// 转出规则：可用资金都能转出，持仓要先卖出；超过转入额度的部分算盈利，每人每天转出的盈利有上限。

const (
	TradeSideBuy  = "buy"
	TradeSideSell = "sell"

	TradeOrderTypeMarket = "market"
	TradeOrderTypeLimit  = "limit"

	TradeOrderStatusOpen     = "open"     // 限价单挂着，可能已部分成交
	TradeOrderStatusFilled   = "filled"   // 全部成交
	TradeOrderStatusCanceled = "canceled" // 没成交完就结束了，FilledQty 可能大于 0

	// 委托没成交完就结束的原因。
	TradeCancelByUser    = "user"    // 用户撤单
	TradeCancelByDepth   = "depth"   // 市价单吃完可接受的盘口还没成交完，剩余部分撤销
	TradeCancelBySymbol  = "symbol"  // 交易对已下架
	TradeCancelByBalance = "balance" // 限价买单冻结的资金不够付这次成交，剩余部分撤销
)

// 模拟盘账单的类型。
const (
	TradeLedgerQuotaIn  = "quota_in"
	TradeLedgerQuotaOut = "quota_out"
	TradeLedgerBuy      = "buy"
	TradeLedgerSell     = "sell"
)

// TradeMaxOpenOrders 是每人同时挂着的限价委托上限，撮合每次盘口变化都要检查挂单，不能无限增长。
const TradeMaxOpenOrders = 50

var (
	ErrTradeAmountInvalid        = errors.New("trade amount is out of range")
	ErrTradeCashInsufficient     = errors.New("trade cash is insufficient")
	ErrTradeQuotaInsufficient    = errors.New("user quota is insufficient for the transfer")
	ErrTradeWithdrawExceeded     = errors.New("trade withdrawal exceeds the withdrawable amount")
	ErrTradeProfitOutLimit       = errors.New("trade daily profit withdrawal limit reached")
	ErrTradePositionInsufficient = errors.New("trade position is insufficient")
	ErrTradePositionLimit        = errors.New("trade position limit reached")
	ErrTradeOpenOrderLimit       = errors.New("too many open trade orders")
	ErrTradeOrderNotOpen         = errors.New("trade order is not open")
	ErrTradeNoFill               = errors.New("trade order has nothing to fill")
)

// TradeAccount 是模拟盘账户，以 user_id 为主键，第一次转入时创建。
type TradeAccount struct {
	UserId int `json:"user_id" gorm:"primaryKey;autoIncrement:false"`
	// Cash 是可用资金，Frozen 是挂着的限价买单冻结的资金，单位都是额度。
	Cash   int `json:"cash" gorm:"type:bigint;not null;default:0"`
	Frozen int `json:"frozen" gorm:"type:bigint;not null;default:0"`
	// QuotaPrincipal 是转入后还没转回的额度。转出的额度超过它的部分是交易盈利，受每天的盈利转出上限约束。
	QuotaPrincipal int `json:"quota_principal" gorm:"type:bigint;not null;default:0"`
	// TotalIn、TotalOut 是累计转入、转出的资金，总资产减去两者之差就是累计盈亏。
	TotalIn   int   `json:"total_in" gorm:"type:bigint;not null;default:0"`
	TotalOut  int   `json:"total_out" gorm:"type:bigint;not null;default:0"`
	CreatedAt int64 `json:"created_at" gorm:"bigint"`
	UpdatedAt int64 `json:"updated_at" gorm:"bigint"`
}

// TradePosition 是一个交易对的持仓。只能做多：数量不会小于 0，卖出不能超过可用数量。
type TradePosition struct {
	UserId int    `json:"user_id" gorm:"primaryKey;autoIncrement:false"`
	Symbol string `json:"symbol" gorm:"type:varchar(20);primaryKey"`
	// Qty 是持有数量(10^-8)，含 FrozenQty；FrozenQty 是挂着的限价卖单冻结的数量。
	Qty       int64 `json:"qty" gorm:"bigint;not null;default:0"`
	FrozenQty int64 `json:"frozen_qty" gorm:"bigint;not null;default:0"`
	// Cost 是持仓成本(额度单位)，含买入手续费；卖出时按数量比例结转，卖完归零。
	Cost      int   `json:"cost" gorm:"type:bigint;not null;default:0"`
	UpdatedAt int64 `json:"updated_at" gorm:"bigint"`
}

// TradeLedger 是模拟盘账单，资金每变动一次记一条：转入、转出、每一次成交。冻结与解冻不改变账户的资金合计，不记账单。
type TradeLedger struct {
	Id      int    `json:"id" gorm:"index:idx_trade_ledger_user,priority:2"`
	UserId  int    `json:"user_id" gorm:"index:idx_trade_ledger_user,priority:1"`
	Type    string `json:"type" gorm:"type:varchar(16);index"`
	Symbol  string `json:"symbol" gorm:"type:varchar(20)"`
	OrderId int    `json:"order_id" gorm:"index"`
	// Qty 与 Price 是这次成交的数量(10^-8)与均价，转入转出时为空。
	Qty   int64  `json:"qty" gorm:"bigint"`
	Price string `json:"price" gorm:"type:varchar(40)"`
	// Amount 是资金变动(额度单位)，入账为正；买入是成交金额加手续费，卖出是成交金额减手续费。
	Amount int `json:"amount" gorm:"type:bigint"`
	Fee    int `json:"fee" gorm:"type:bigint"`
	// Balance 是变动后的资金合计(可用 + 冻结)。
	Balance   int   `json:"balance" gorm:"type:bigint"`
	CreatedAt int64 `json:"created_at" gorm:"bigint;index"`
}

// TradeProfitOutDay 记录每人每天已经转出的盈利(额度单位)。
type TradeProfitOutDay struct {
	UserId int    `json:"user_id" gorm:"primaryKey;autoIncrement:false"`
	Day    string `json:"day" gorm:"type:varchar(10);primaryKey"`
	Used   int    `json:"used" gorm:"type:bigint;not null;default:0"`
}

// TradeFill 是一次成交：数量(10^-8)、成交金额与手续费(额度单位)和成交均价。
type TradeFill struct {
	Qty    int64
	Amount int
	Fee    int
	Price  string
}

// TradeQuotaPerUsd 是 1 USDT 折成的额度单位：1 USDT = 1 美元额度。
func TradeQuotaPerUsd() (int, error) {
	if common.QuotaPerUnit <= 0 {
		return 0, errors.New("QuotaPerUnit must be positive")
	}
	return common.QuotaFromDecimalStrict(decimal.NewFromFloat(common.QuotaPerUnit))
}

// GetTradeAccount 读出用户的模拟盘账户，还没有账户时返回空账户。
func GetTradeAccount(userId int) (TradeAccount, error) {
	var accounts []TradeAccount
	if err := DB.Where("user_id = ?", userId).Limit(1).Find(&accounts).Error; err != nil {
		return TradeAccount{}, err
	}
	if len(accounts) == 0 {
		return TradeAccount{UserId: userId}, nil
	}
	return accounts[0], nil
}

// GetTradePositions 返回用户还持有的仓位。
func GetTradePositions(userId int) ([]TradePosition, error) {
	var positions []TradePosition
	err := DB.Where("user_id = ? AND qty > 0", userId).Order("symbol").Find(&positions).Error
	return positions, err
}

// GetTradeProfitOutUsed 返回用户这一天已经转出的盈利。
func GetTradeProfitOutUsed(userId int, day string) (int, error) {
	var rows []TradeProfitOutDay
	if err := DB.Where("user_id = ? AND day = ?", userId, day).Limit(1).Find(&rows).Error; err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return 0, nil
	}
	return rows[0].Used, nil
}

// lockTradeAccountTx 锁住用户的模拟盘账户并读出来。账户还不存在时先建一个空的：不存在的行锁不住，新用户同时发来的
// 两个请求会各自通过检查。SQLite 没有 FOR UPDATE，建行的 INSERT 已经拿到了整库的写锁。
func lockTradeAccountTx(tx *gorm.DB, userId int) (*TradeAccount, error) {
	now := common.GetTimestamp()
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&TradeAccount{UserId: userId, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		return nil, err
	}
	var account TradeAccount
	if err := lockForUpdate(tx).Where("user_id = ?", userId).First(&account).Error; err != nil {
		return nil, err
	}
	return &account, nil
}

// saveTradeAccountTx 写回锁住后改过的账户。金额都必须在 [0, 2^53) 里，越界说明上游算错了，整笔事务回滚，不写出夹过的数字。
func saveTradeAccountTx(tx *gorm.DB, account *TradeAccount) error {
	for _, value := range []int{account.Cash, account.Frozen, account.QuotaPrincipal, account.TotalIn, account.TotalOut} {
		if value < 0 || value >= common.MaxQuota {
			return ErrTradeAmountInvalid
		}
	}
	account.UpdatedAt = common.GetTimestamp()
	return tx.Model(&TradeAccount{}).Where("user_id = ?", account.UserId).Updates(map[string]interface{}{
		"cash":            account.Cash,
		"frozen":          account.Frozen,
		"quota_principal": account.QuotaPrincipal,
		"total_in":        account.TotalIn,
		"total_out":       account.TotalOut,
		"updated_at":      account.UpdatedAt,
	}).Error
}

// tradeLedgerTx 写一条账单，Balance 取账户当前的资金合计。
func tradeLedgerTx(tx *gorm.DB, account *TradeAccount, entry TradeLedger) error {
	entry.UserId = account.UserId
	entry.Balance = account.Cash + account.Frozen
	entry.CreatedAt = common.GetTimestamp()
	return tx.Create(&entry).Error
}

// tradeCostShare 按数量比例算出卖掉 qty 要结转的持仓成本，向上取整：结转多了，剩下的成本就少，记下的盈利也跟着少，
// 取整的零头留在站点这边。全部卖完时结转全部成本。
func tradeCostShare(cost int, qty int64, held int64) int {
	if qty >= held {
		return cost
	}
	product := new(big.Int).Mul(big.NewInt(int64(cost)), big.NewInt(qty))
	share, remainder := new(big.Int).QuoRem(product, big.NewInt(held), new(big.Int))
	if remainder.Sign() > 0 {
		share.Add(share, big.NewInt(1))
	}
	return int(share.Int64())
}

// TradeStats 是全站模拟盘的汇总，给管理员看：资金与持仓成本都是额度单位。
type TradeStats struct {
	Accounts     int64 `json:"accounts"`
	Cash         int   `json:"cash"`
	Frozen       int   `json:"frozen"`
	PositionCost int   `json:"position_cost"`
	NetIn        int   `json:"net_in"`
	OpenOrders   int64 `json:"open_orders"`
}

// GetTradeStats 汇总全站的模拟盘账户、资金、持仓成本与挂单。
func GetTradeStats() (TradeStats, error) {
	var stats TradeStats
	if err := DB.Model(&TradeAccount{}).Count(&stats.Accounts).Error; err != nil {
		return stats, err
	}
	var sums struct {
		Cash   int
		Frozen int
		NetIn  int
	}
	if err := DB.Model(&TradeAccount{}).
		Select("COALESCE(SUM(cash), 0) AS cash, COALESCE(SUM(frozen), 0) AS frozen, COALESCE(SUM(total_in - total_out), 0) AS net_in").
		Scan(&sums).Error; err != nil {
		return stats, err
	}
	stats.Cash, stats.Frozen, stats.NetIn = sums.Cash, sums.Frozen, sums.NetIn
	if err := DB.Model(&TradePosition{}).Select("COALESCE(SUM(cost), 0)").Scan(&stats.PositionCost).Error; err != nil {
		return stats, err
	}
	err := DB.Model(&TradeOrder{}).Where("status = ?", TradeOrderStatusOpen).Count(&stats.OpenOrders).Error
	return stats, err
}
