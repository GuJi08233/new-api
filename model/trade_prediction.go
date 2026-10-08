package model

import (
	"errors"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/tradesim"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	TradePredictionUp   = "UP"
	TradePredictionDown = "DOWN"
	// TradePredictionSplit 是 Polymarket 判为五五开的结果(结算数据出了问题时才会这样判)，两边每份都按 0.5 美元兑付。
	TradePredictionSplit  = "SPLIT"
	TradePredictionActive = "active"
	TradePredictionSold   = "sold"
	TradePredictionWon    = "won"
	TradePredictionLost   = "lost"
	// TradePredictionHalf 是五五开结算的持仓。
	TradePredictionHalf         = "half"
	TradePredictionWindow       = int64(300)
	TradeLedgerPredictionBuy    = "prediction_buy"
	TradeLedgerPredictionSell   = "prediction_sell"
	TradeLedgerPredictionSettle = "predict_settle"
)

var (
	ErrTradePredictionClosed  = errors.New("prediction round is closed")
	ErrTradePredictionQuote   = errors.New("prediction quote is unavailable or stale")
	ErrTradePredictionOutcome = errors.New("prediction outcome is not confirmed")
)

// 每轮身份与官方结果持久化。到期并不代表已经有结果，结果只能来自供应商的已决市场。
type TradePredictionRound struct {
	WindowStart   int64  `json:"window_start" gorm:"primaryKey;autoIncrement:false;type:bigint"`
	EndTime       int64  `json:"end_time" gorm:"type:bigint;index"`
	Slug          string `json:"slug" gorm:"type:varchar(80);not null"`
	Title         string `json:"title" gorm:"type:varchar(300)"`
	ConditionId   string `json:"-" gorm:"type:varchar(100);not null"`
	UpToken       string `json:"-" gorm:"type:varchar(100);not null"`
	DownToken     string `json:"-" gorm:"type:varchar(100);not null"`
	Outcome       string `json:"outcome" gorm:"type:varchar(8)"`
	SettledAt     int64  `json:"settled_at" gorm:"type:bigint"`
	LastCheckedAt int64  `json:"-" gorm:"type:bigint;index"`
}

// 每笔买入单独持有，可分批卖出。份额以 10^-8 记账，成交时限制到小数点后四位。
// Cost 是剩余成本（含费）；TotalCost 和 Payout 记录整笔生命周期，可直接计算已实现盈亏。
type TradePredictionPosition struct {
	Id          int    `json:"id"`
	UserId      int    `json:"user_id" gorm:"index:idx_prediction_user,priority:1"`
	WindowStart int64  `json:"window_start" gorm:"type:bigint;index"`
	Side        string `json:"side" gorm:"type:varchar(8)"`
	Qty         int64  `json:"-" gorm:"type:bigint"`
	InitialQty  int64  `json:"-" gorm:"type:bigint"`
	Cost        int    `json:"cost" gorm:"type:bigint"`
	TotalCost   int    `json:"total_cost" gorm:"type:bigint"`
	Payout      int    `json:"payout" gorm:"type:bigint"`
	AvgPrice    string `json:"avg_price" gorm:"type:varchar(40)"`
	Status      string `json:"status" gorm:"type:varchar(12);index:idx_prediction_user,priority:2"`
	CreatedAt   int64  `json:"created_at" gorm:"type:bigint"`
	UpdatedAt   int64  `json:"updated_at" gorm:"type:bigint"`
}

// SaveTradePredictionRound 只创建一次，禁止后续行情把已存的 token 或已决结果覆盖。
func SaveTradePredictionRound(round TradePredictionRound) error {
	if round.WindowStart <= 0 || round.WindowStart%TradePredictionWindow != 0 || round.EndTime != round.WindowStart+TradePredictionWindow ||
		round.ConditionId == "" || round.UpToken == "" || round.DownToken == "" || round.UpToken == round.DownToken || round.Outcome != "" {
		return ErrTradeAmountInvalid
	}
	return DB.Clauses(clause.OnConflict{DoNothing: true}).Create(&round).Error
}

func GetTradePredictionRound(windowStart int64) (*TradePredictionRound, error) {
	var round TradePredictionRound
	err := DB.Where("window_start = ?", windowStart).First(&round).Error
	return &round, err
}

type TradePredictionFill struct {
	UserId      int
	WindowStart int64
	Side        string
	Qty         int64
	Amount      int
	Fee         int
	Price       string
	// ExpiresAtMs 来自服务层本次 HTTP 盘口；事务等待锁后必须再次检查有效期。
	ExpiresAtMs int64
	MaxCost     int
	Market      TradeFuturesMarket
}

func BuyTradePrediction(input TradePredictionFill) (*TradePredictionPosition, error) {
	if input.UserId <= 0 || input.Qty <= 0 || input.Qty > 100_000_000_000_000 || input.Qty%10000 != 0 ||
		input.Amount <= 0 || input.Fee < 0 || input.Amount >= common.MaxQuota || input.Fee >= common.MaxQuota-input.Amount ||
		(input.Side != TradePredictionUp && input.Side != TradePredictionDown) || input.MaxCost <= 0 || input.MaxCost >= common.MaxQuota {
		return nil, ErrTradeAmountInvalid
	}
	var position TradePredictionPosition
	err := DB.Transaction(func(tx *gorm.DB) error {
		account, err := lockTradeAccountTx(tx, input.UserId)
		if err != nil {
			return err
		}
		now := time.Now()
		if now.UnixMilli() > input.ExpiresAtMs {
			return ErrTradePredictionQuote
		}
		var round TradePredictionRound
		if err := tx.Where("window_start = ?", input.WindowStart).First(&round).Error; err != nil {
			return err
		}
		if now.Unix() < round.WindowStart || now.Unix() >= round.EndTime || round.Outcome != "" {
			return ErrTradePredictionClosed
		}
		debit := input.Amount + input.Fee
		var cost int64
		if err := tx.Model(&TradePredictionPosition{}).Select("COALESCE(SUM(cost), 0)").Where("user_id = ? AND status = ?", input.UserId, TradePredictionActive).Scan(&cost).Error; err != nil {
			return err
		}
		if cost > int64(input.MaxCost) || int64(debit) > int64(input.MaxCost)-cost {
			return ErrTradePositionLimit
		}
		if err := checkTradeAffordTx(tx, account, debit, debit, input.Market); err != nil {
			return err
		}
		account.Cash -= debit
		position = TradePredictionPosition{UserId: input.UserId, WindowStart: input.WindowStart, Side: input.Side, Qty: input.Qty, InitialQty: input.Qty,
			Cost: debit, TotalCost: debit, AvgPrice: input.Price, Status: TradePredictionActive, CreatedAt: now.Unix(), UpdatedAt: now.Unix()}
		if err := tx.Create(&position).Error; err != nil {
			return err
		}
		if err := saveTradeAccountTx(tx, account); err != nil {
			return err
		}
		return tx.Create(&TradeLedger{UserId: input.UserId, Type: TradeLedgerPredictionBuy, Symbol: "BTC-PREDICTION", OrderId: position.Id,
			Qty: input.Qty, Price: input.Price, Amount: -debit, Fee: input.Fee, Balance: account.Cash + account.Frozen, CreatedAt: now.Unix()}).Error
	})
	return &position, err
}

func SellTradePrediction(positionId int, input TradePredictionFill) (*TradePredictionPosition, error) {
	if input.UserId <= 0 || input.Qty <= 0 || input.Qty%10000 != 0 || input.Amount < 0 || input.Amount >= common.MaxQuota || input.Fee < 0 || input.Fee > input.Amount {
		return nil, ErrTradeAmountInvalid
	}
	var position TradePredictionPosition
	err := DB.Transaction(func(tx *gorm.DB) error {
		account, err := lockTradeAccountTx(tx, input.UserId)
		if err != nil {
			return err
		}
		if err := tx.Where("id = ? AND user_id = ?", positionId, input.UserId).First(&position).Error; err != nil {
			return err
		}
		if position.Status != TradePredictionActive || input.Qty > position.Qty {
			return ErrTradePositionInsufficient
		}
		if input.WindowStart != position.WindowStart || input.Side != position.Side {
			return ErrTradePredictionQuote
		}
		now := time.Now()
		if now.UnixMilli() > input.ExpiresAtMs {
			return ErrTradePredictionQuote
		}
		var round TradePredictionRound
		if err := tx.Where("window_start = ?", position.WindowStart).First(&round).Error; err != nil {
			return err
		}
		if now.Unix() < round.WindowStart || now.Unix() >= round.EndTime || round.Outcome != "" {
			return ErrTradePredictionClosed
		}
		credit := input.Amount - input.Fee
		if account.Cash >= common.MaxQuota-credit || position.Payout >= common.MaxQuota-credit {
			return ErrTradeAmountInvalid
		}
		cost, err := common.QuotaFromDecimalStrict(decimal.NewFromInt(int64(position.Cost)).Mul(decimal.NewFromInt(input.Qty)).Div(decimal.NewFromInt(position.Qty)).Floor())
		if err != nil {
			return ErrTradeAmountInvalid
		}
		position.Cost -= cost
		position.Qty -= input.Qty
		position.Payout += credit
		position.UpdatedAt = now.Unix()
		if position.Qty == 0 {
			position.Status = TradePredictionSold
		}
		account.Cash += credit
		if err := tx.Save(&position).Error; err != nil {
			return err
		}
		if err := saveTradeAccountTx(tx, account); err != nil {
			return err
		}
		return tx.Create(&TradeLedger{UserId: input.UserId, Type: TradeLedgerPredictionSell, Symbol: "BTC-PREDICTION", OrderId: position.Id,
			Qty: input.Qty, Price: input.Price, Amount: credit, Fee: input.Fee, Pnl: credit - cost, Balance: account.Cash + account.Frozen, CreatedAt: now.Unix()}).Error
	})
	return &position, err
}

// ResolveTradePredictionRound 只接受服务验证过的官方结果(赢家或五五开)；结果一旦写入不允许改写。
func ResolveTradePredictionRound(windowStart int64, outcome string) error {
	if outcome != TradePredictionUp && outcome != TradePredictionDown && outcome != TradePredictionSplit {
		return ErrTradePredictionOutcome
	}
	now := common.GetTimestamp()
	result := DB.Model(&TradePredictionRound{}).Where("window_start = ? AND end_time <= ? AND outcome = ?", windowStart, now, "").
		Updates(map[string]any{"outcome": outcome, "settled_at": now, "last_checked_at": now})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected > 0 {
		return nil
	}
	round, err := GetTradePredictionRound(windowStart)
	if err != nil {
		return err
	}
	if round.Outcome != outcome {
		return ErrTradePredictionOutcome
	}
	return nil
}

// SettleTradePredictionPosition 账户行锁串行化卖出、划转与结算。已处理的持仓直接返回，崩溃重试不重复入账。
func SettleTradePredictionPosition(positionId int) error {
	var identity TradePredictionPosition
	if err := DB.Where("id = ?", positionId).First(&identity).Error; err != nil {
		return err
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		account, err := lockTradeAccountTx(tx, identity.UserId)
		if err != nil {
			return err
		}
		var position TradePredictionPosition
		if err := tx.Where("id = ?", positionId).First(&position).Error; err != nil {
			return err
		}
		if position.Status != TradePredictionActive {
			return nil
		}
		var round TradePredictionRound
		if err := tx.Where("window_start = ?", position.WindowStart).First(&round).Error; err != nil {
			return err
		}
		if round.EndTime > common.GetTimestamp() || (round.Outcome != TradePredictionUp && round.Outcome != TradePredictionDown && round.Outcome != TradePredictionSplit) {
			return ErrTradePredictionOutcome
		}
		// 猜对的每份兑付 1 美元，猜错的 0，五五开两边都是 0.5 美元。
		credit := 0
		position.Status = TradePredictionLost
		price := decimal.Zero
		switch round.Outcome {
		case position.Side:
			position.Status, price = TradePredictionWon, decimal.NewFromInt(1)
		case TradePredictionSplit:
			position.Status, price = TradePredictionHalf, decimal.New(5, -1)
		}
		if price.IsPositive() {
			perUsd, err := TradeQuotaPerUsd()
			if err != nil {
				return err
			}
			credit, err = (tradesim.Pricing{QuotaPerUsd: decimal.NewFromInt(int64(perUsd))}).CreditQuota(tradesim.QtyFromUnits(position.Qty).Mul(price))
			if err != nil {
				return ErrTradeAmountInvalid
			}
		}
		if account.Cash >= common.MaxQuota-credit || position.Payout >= common.MaxQuota-credit {
			return ErrTradeAmountInvalid
		}
		account.Cash += credit
		position.Payout += credit
		qty, cost := position.Qty, position.Cost
		position.Qty, position.Cost, position.UpdatedAt = 0, 0, common.GetTimestamp()
		if err := tx.Save(&position).Error; err != nil {
			return err
		}
		if err := saveTradeAccountTx(tx, account); err != nil {
			return err
		}
		return tx.Create(&TradeLedger{UserId: position.UserId, Type: TradeLedgerPredictionSettle, Symbol: "BTC-PREDICTION", OrderId: position.Id,
			Qty: qty, Price: price.String(), Amount: credit, Pnl: credit - cost, Balance: account.Cash + account.Frozen, CreatedAt: position.UpdatedAt}).Error
	})
}

func GetTradePredictionPosition(userId, positionId int) (*TradePredictionPosition, error) {
	var position TradePredictionPosition
	err := DB.Where("id = ? AND user_id = ?", positionId, userId).First(&position).Error
	return &position, err
}

func ListTradePredictionPositions(userId int, active bool, offset, limit int) ([]TradePredictionPosition, int64, error) {
	rows := []TradePredictionPosition{}
	query := DB.Model(&TradePredictionPosition{}).Where("user_id = ?", userId)
	if active {
		query = query.Where("status = ?", TradePredictionActive)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := query.Order("id DESC").Offset(offset).Limit(limit).Find(&rows).Error
	return rows, total, err
}

func ListTradePredictionRounds(offset, limit int) ([]TradePredictionRound, int64, error) {
	rows := []TradePredictionRound{}
	var total int64
	if err := DB.Model(&TradePredictionRound{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := DB.Order("window_start DESC").Offset(offset).Limit(limit).Find(&rows).Error
	return rows, total, err
}

// ListTradePredictionPositionsOf 用于统一账户估值，预测份额不能因为被扣了现金就在总资产里消失。
func ListTradePredictionPositionsOf(userIds []int) ([]TradePredictionPosition, error) {
	rows := []TradePredictionPosition{}
	if len(userIds) == 0 {
		return rows, nil
	}
	err := DB.Where("user_id IN ? AND status = ?", userIds, TradePredictionActive).Find(&rows).Error
	return rows, err
}
