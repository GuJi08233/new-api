package model

import (
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/tradesim"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// TradePredictionSeriesRound 支持多币种(BTC/ETH/SOL/XRP) × 多周期(5m/15m)的 Polymarket 预测轮次。
// 默认的 BTC 5m 仍沿用原 TradePredictionRound 表保持 100% 向后兼容；其他组合写入本表。
type TradePredictionSeriesRound struct {
	Slug          string `json:"slug" gorm:"type:varchar(80);primaryKey"`
	MarketKey     string `json:"market_key" gorm:"type:varchar(24);index:idx_pred_series_mkt_win,priority:1;not null"`
	Asset         string `json:"asset" gorm:"type:varchar(16);not null"`
	IntervalSec   int64  `json:"interval_sec" gorm:"type:bigint;not null"`
	WindowStart   int64  `json:"window_start" gorm:"type:bigint;index:idx_pred_series_mkt_win,priority:2;not null"`
	EndTime       int64  `json:"end_time" gorm:"type:bigint;index"`
	Title         string `json:"title" gorm:"type:varchar(300)"`
	ConditionId   string `json:"-" gorm:"type:varchar(100);not null"`
	UpToken       string `json:"-" gorm:"type:varchar(100);not null"`
	DownToken     string `json:"-" gorm:"type:varchar(100);not null"`
	Outcome       string `json:"outcome" gorm:"type:varchar(8)"`
	SettledAt     int64  `json:"settled_at" gorm:"type:bigint"`
	LastCheckedAt int64  `json:"-" gorm:"type:bigint;index"`
}

// TradePredictionSeriesPosition 记录用户在多币种/多周期预测中的持仓。
type TradePredictionSeriesPosition struct {
	Id          int    `json:"id"`
	UserId      int    `json:"user_id" gorm:"index:idx_pred_series_pos_user,priority:1"`
	MarketKey   string `json:"market_key" gorm:"type:varchar(24);index"`
	Asset       string `json:"asset" gorm:"type:varchar(16)"`
	IntervalSec int64  `json:"interval_sec" gorm:"type:bigint"`
	Slug        string `json:"slug" gorm:"type:varchar(80);index"`
	WindowStart int64  `json:"window_start" gorm:"type:bigint;index"`
	Side        string `json:"side" gorm:"type:varchar(8)"`
	Qty         int64  `json:"-" gorm:"type:bigint"`
	InitialQty  int64  `json:"-" gorm:"type:bigint"`
	Cost        int    `json:"cost" gorm:"type:bigint"`
	TotalCost   int    `json:"total_cost" gorm:"type:bigint"`
	Payout      int    `json:"payout" gorm:"type:bigint"`
	AvgPrice    string `json:"avg_price" gorm:"type:varchar(40)"`
	Status      string `json:"status" gorm:"type:varchar(12);index:idx_pred_series_pos_user,priority:2"`
	CreatedAt   int64  `json:"created_at" gorm:"type:bigint"`
	UpdatedAt   int64  `json:"updated_at" gorm:"type:bigint"`
}

func SaveTradePredictionSeriesRound(round TradePredictionSeriesRound) error {
	if round.Slug == "" || round.MarketKey == "" || round.Asset == "" || round.IntervalSec <= 0 ||
		round.WindowStart <= 0 || round.WindowStart%round.IntervalSec != 0 || round.EndTime != round.WindowStart+round.IntervalSec ||
		round.ConditionId == "" || round.UpToken == "" || round.DownToken == "" || round.UpToken == round.DownToken || round.Outcome != "" {
		return ErrTradeAmountInvalid
	}
	return DB.Clauses(clause.OnConflict{DoNothing: true}).Create(&round).Error
}

func GetTradePredictionSeriesRound(slug string) (*TradePredictionSeriesRound, error) {
	var round TradePredictionSeriesRound
	err := DB.Where("slug = ?", slug).First(&round).Error
	return &round, err
}

func BuyTradePredictionSeries(slug, marketKey, asset string, intervalSec int64, input TradePredictionFill) (*TradePredictionSeriesPosition, error) {
	if slug == "" || marketKey == "" || asset == "" || intervalSec <= 0 ||
		input.UserId <= 0 || input.Qty <= 0 || input.Qty > 100_000_000_000_000 || input.Qty%10000 != 0 ||
		input.Amount <= 0 || input.Fee < 0 || input.Amount >= common.MaxQuota || input.Fee >= common.MaxQuota-input.Amount ||
		(input.Side != TradePredictionUp && input.Side != TradePredictionDown) || input.MaxCost <= 0 || input.MaxCost >= common.MaxQuota {
		return nil, ErrTradeAmountInvalid
	}
	var position TradePredictionSeriesPosition
	err := DB.Transaction(func(tx *gorm.DB) error {
		account, err := lockTradeAccountTx(tx, input.UserId)
		if err != nil {
			return err
		}
		now := time.Now()
		if now.UnixMilli() > input.ExpiresAtMs {
			return ErrTradePredictionQuote
		}
		var round TradePredictionSeriesRound
		if err := tx.Where("slug = ?", slug).First(&round).Error; err != nil {
			return err
		}
		if now.Unix() < round.WindowStart || now.Unix() >= round.EndTime || round.Outcome != "" {
			return ErrTradePredictionClosed
		}
		debit := input.Amount + input.Fee
		// 预测总持仓上限同时计入 BTC 5m 与多币种系列，避免多开绕过上限。
		var btcCost, seriesCost int64
		if tx.Migrator().HasTable(&TradePredictionPosition{}) {
			if err := tx.Model(&TradePredictionPosition{}).Select("COALESCE(SUM(cost), 0)").Where("user_id = ? AND status = ?", input.UserId, TradePredictionActive).Scan(&btcCost).Error; err != nil {
				return err
			}
		}
		if err := tx.Model(&TradePredictionSeriesPosition{}).Select("COALESCE(SUM(cost), 0)").Where("user_id = ? AND status = ?", input.UserId, TradePredictionActive).Scan(&seriesCost).Error; err != nil {
			return err
		}
		totalCost := btcCost + seriesCost
		if totalCost > int64(input.MaxCost) || int64(debit) > int64(input.MaxCost)-totalCost {
			return ErrTradePositionLimit
		}
		if err := checkTradeAffordTx(tx, account, debit, debit, input.Market); err != nil {
			return err
		}
		account.Cash -= debit
		position = TradePredictionSeriesPosition{
			UserId:      input.UserId,
			MarketKey:   marketKey,
			Asset:       strings.ToUpper(asset),
			IntervalSec: intervalSec,
			Slug:        slug,
			WindowStart: round.WindowStart,
			Side:        input.Side,
			Qty:         input.Qty,
			InitialQty:  input.Qty,
			Cost:        debit,
			TotalCost:   debit,
			AvgPrice:    input.Price,
			Status:      TradePredictionActive,
			CreatedAt:   now.Unix(),
			UpdatedAt:   now.Unix(),
		}
		if err := tx.Create(&position).Error; err != nil {
			return err
		}
		if err := saveTradeAccountTx(tx, account); err != nil {
			return err
		}
		if err := creditGameCoinWagerFromTradeTx(tx, account, input.Amount, operation_setting.GetTradeSetting().TradeWagerSpotBps); err != nil {
			return err
		}
		ledgerSymbol := strings.ToUpper(asset) + "-PRED"
		return tx.Create(&TradeLedger{
			UserId:    input.UserId,
			Type:      TradeLedgerPredictionBuy,
			Symbol:    ledgerSymbol,
			OrderId:   position.Id,
			Qty:       input.Qty,
			Price:     input.Price,
			Amount:    -debit,
			Fee:       input.Fee,
			Balance:   account.Cash + account.Frozen,
			CreatedAt: now.Unix(),
		}).Error
	})
	return &position, err
}

func SellTradePredictionSeries(positionId int, input TradePredictionFill) (*TradePredictionSeriesPosition, error) {
	if input.UserId <= 0 || input.Qty <= 0 || input.Qty%10000 != 0 || input.Amount < 0 || input.Amount >= common.MaxQuota || input.Fee < 0 || input.Fee > input.Amount {
		return nil, ErrTradeAmountInvalid
	}
	var position TradePredictionSeriesPosition
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
		var round TradePredictionSeriesRound
		if err := tx.Where("slug = ?", position.Slug).First(&round).Error; err != nil {
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
		if err := creditGameCoinWagerFromTradeTx(tx, account, input.Amount, operation_setting.GetTradeSetting().TradeWagerSpotBps); err != nil {
			return err
		}
		ledgerSymbol := strings.ToUpper(position.Asset) + "-PRED"
		return tx.Create(&TradeLedger{
			UserId:    input.UserId,
			Type:      TradeLedgerPredictionSell,
			Symbol:    ledgerSymbol,
			OrderId:   position.Id,
			Qty:       input.Qty,
			Price:     input.Price,
			Amount:    credit,
			Fee:       input.Fee,
			Pnl:       credit - cost,
			Balance:   account.Cash + account.Frozen,
			CreatedAt: now.Unix(),
		}).Error
	})
	return &position, err
}

func ResolveTradePredictionSeriesRound(slug, outcome string) error {
	if outcome != TradePredictionUp && outcome != TradePredictionDown && outcome != TradePredictionSplit {
		return ErrTradePredictionOutcome
	}
	now := common.GetTimestamp()
	return DB.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&TradePredictionSeriesRound{}).Where("slug = ? AND end_time <= ? AND outcome = ?", slug, now, "").
			Updates(map[string]any{"outcome": outcome, "settled_at": now, "last_checked_at": now})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected > 0 {
			users := tx.Model(&TradePredictionSeriesPosition{}).Select("user_id").
				Where("slug = ? AND status = ?", slug, TradePredictionActive)
			return tx.Model(&TradeAccount{}).Where("user_id IN (?)", users).
				Update("asset_version", gorm.Expr("asset_version + ?", 1)).Error
		}
		var round TradePredictionSeriesRound
		if err := tx.Where("slug = ?", slug).First(&round).Error; err != nil {
			return err
		}
		if round.Outcome != outcome {
			return ErrTradePredictionOutcome
		}
		return nil
	})
}

func SettleTradePredictionSeriesPosition(positionId int) error {
	var identity TradePredictionSeriesPosition
	if err := DB.Where("id = ?", positionId).First(&identity).Error; err != nil {
		return err
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		account, err := lockTradeAccountTx(tx, identity.UserId)
		if err != nil {
			return err
		}
		var position TradePredictionSeriesPosition
		if err := tx.Where("id = ?", positionId).First(&position).Error; err != nil {
			return err
		}
		if position.Status != TradePredictionActive {
			return nil
		}
		var round TradePredictionSeriesRound
		if err := tx.Where("slug = ?", position.Slug).First(&round).Error; err != nil {
			return err
		}
		if round.EndTime > common.GetTimestamp() || (round.Outcome != TradePredictionUp && round.Outcome != TradePredictionDown && round.Outcome != TradePredictionSplit) {
			return ErrTradePredictionOutcome
		}
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
		ledgerSymbol := strings.ToUpper(position.Asset) + "-PRED"
		return tx.Create(&TradeLedger{
			UserId:    position.UserId,
			Type:      TradeLedgerPredictionSettle,
			Symbol:    ledgerSymbol,
			OrderId:   position.Id,
			Qty:       qty,
			Price:     price.String(),
			Amount:    credit,
			Pnl:       credit - cost,
			Balance:   account.Cash + account.Frozen,
			CreatedAt: position.UpdatedAt,
		}).Error
	})
}

func GetTradePredictionSeriesPosition(userId, positionId int) (*TradePredictionSeriesPosition, error) {
	var position TradePredictionSeriesPosition
	err := DB.Where("id = ? AND user_id = ?", positionId, userId).First(&position).Error
	return &position, err
}

func ListTradePredictionSeriesPositions(userId int, marketKey string, active bool, offset, limit int) ([]TradePredictionSeriesPosition, int64, error) {
	rows := []TradePredictionSeriesPosition{}
	query := DB.Model(&TradePredictionSeriesPosition{}).Where("user_id = ?", userId)
	if marketKey != "" {
		query = query.Where("market_key = ?", marketKey)
	}
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

func ListTradePredictionSeriesRounds(marketKey string, offset, limit int) ([]TradePredictionSeriesRound, int64, error) {
	rows := []TradePredictionSeriesRound{}
	query := DB.Model(&TradePredictionSeriesRound{})
	if marketKey != "" {
		query = query.Where("market_key = ?", marketKey)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := query.Order("window_start DESC").Offset(offset).Limit(limit).Find(&rows).Error
	return rows, total, err
}

func ListTradePredictionSeriesPositionsOf(userIds []int) ([]TradePredictionSeriesPosition, error) {
	rows := []TradePredictionSeriesPosition{}
	if len(userIds) == 0 || !DB.Migrator().HasTable(&TradePredictionSeriesPosition{}) {
		return rows, nil
	}
	err := DB.Where("user_id IN ? AND status = ?", userIds, TradePredictionActive).Find(&rows).Error
	return rows, err
}
