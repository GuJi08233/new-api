package model

import (
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/tradesim"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// TradeFuturesFundingState 跟随 PositionId 生命周期保留资金费游标，平仓后也不会被下一次开仓覆盖。
// 时间都是毫秒；ClosedAt 为 0 表示仍持仓。已平仓的游标超过平仓时点后，才不再需要查询官方资金费。
type TradeFuturesFundingState struct {
	PositionId int    `gorm:"primaryKey;autoIncrement:false"`
	UserId     int    `gorm:"index"`
	Symbol     string `gorm:"type:varchar(20);index"`
	Side       string `gorm:"type:varchar(5)"`
	MarginMode string `gorm:"type:varchar(8)"`
	OpenedAt   int64  `gorm:"type:bigint"`
	ClosedAt   int64  `gorm:"type:bigint"`
	FundingAt  int64  `gorm:"type:bigint"`
}

// TradeFuturesExposure 保存成交之后的仓位数量。Id 沿用成交账单编号，同毫秒内按 Id 确定先后。
// 资金费取官方时点及之前的最后一个数量，开仓恰好发生在该时点时从下一期开始计费。
type TradeFuturesExposure struct {
	Id         int   `gorm:"primaryKey;autoIncrement:false"`
	PositionId int   `gorm:"index:idx_trade_exposure_time,priority:1"`
	OccurredAt int64 `gorm:"type:bigint;index:idx_trade_exposure_time,priority:2"`
	Qty        int64 `gorm:"type:bigint"`
}

// ensureTradeFuturesFundingStateTx 为升级前仍持有的仓位补建记录。旧账单只有秒级精度，保留原资金费游标，
// 不重新收费；已经在升级前平完的历史没有可靠的资金费期次，不能据此追溯改账。调用方持有账户行锁。
func ensureTradeFuturesFundingStateTx(tx *gorm.DB, position *TradeFuturesPosition) error {
	if position.PositionId <= 0 || position.Qty <= 0 {
		return nil
	}
	var count int64
	if err := tx.Model(&TradeFuturesFundingState{}).Where("position_id = ?", position.PositionId).Count(&count).Error; err != nil || count > 0 {
		return err
	}
	var entries []TradeLedger
	if err := tx.Where("position_id = ? AND type IN ?", position.PositionId,
		[]string{TradeLedgerFuturesOpen, TradeLedgerFuturesClose, TradeLedgerLiquidation}).Order("id").Find(&entries).Error; err != nil {
		return err
	}
	if len(entries) == 0 {
		return ErrTradeAmountInvalid
	}
	qty := int64(0)
	for _, entry := range entries {
		if entry.Type == TradeLedgerFuturesOpen {
			qty += entry.Qty
		} else {
			qty -= entry.Qty
		}
		if qty < 0 {
			return ErrTradeAmountInvalid
		}
		if err := tx.Create(&TradeFuturesExposure{Id: entry.Id, PositionId: position.PositionId, OccurredAt: entry.CreatedAt * 1000, Qty: qty}).Error; err != nil {
			return err
		}
	}
	if qty != position.Qty {
		return ErrTradeAmountInvalid
	}
	return tx.Create(&TradeFuturesFundingState{PositionId: position.PositionId, UserId: position.UserId, Symbol: position.Symbol,
		Side: position.Side, MarginMode: position.MarginMode, OpenedAt: position.OpenedAt * 1000, FundingAt: position.FundingAt}).Error
}

// recoverTradeFuturesFunding 为升级前尚未建立游标的在持仓位恢复数量历史。新仓位在成交事务内创建，无需恢复。
func recoverTradeFuturesFunding(symbol string) error {
	var positions []TradeFuturesPosition
	if err := DB.Where("symbol = ? AND qty > 0 AND position_id NOT IN (?)", symbol,
		DB.Model(&TradeFuturesFundingState{}).Select("position_id")).Find(&positions).Error; err != nil {
		return err
	}
	for _, identity := range positions {
		if err := DB.Transaction(func(tx *gorm.DB) error {
			if _, err := lockTradeAccountTx(tx, identity.UserId); err != nil {
				return err
			}
			position, err := loadTradeFuturesPositionTx(tx, identity.UserId, identity.Symbol, identity.Side)
			if err != nil {
				return err
			}
			return ensureTradeFuturesFundingStateTx(tx, position)
		}); err != nil {
			return err
		}
	}
	return nil
}

// ApplyTradeFuturesFunding 按官方时点的持仓数量结算这个用户/合约/方向上尚未处理的生命周期。
// 未平完的逐仓费用进出原仓位保证金；已平完的费用进出现金并修正原历史，不影响同方向重开的保证金。
func ApplyTradeFuturesFunding(userId int, symbol string, side string, fundingTime int64, rate decimal.Decimal, mark decimal.Decimal) (bool, error) {
	if fundingTime <= 0 || !mark.IsPositive() || (side != TradeFuturesLong && side != TradeFuturesShort) {
		return false, ErrTradeAmountInvalid
	}
	pricing, err := tradeFuturesPricing()
	if err != nil {
		return false, err
	}
	applied, covered := false, TradeDeficitCover{}
	err = DB.Transaction(func(tx *gorm.DB) error {
		account, err := lockTradeAccountTx(tx, userId)
		if err != nil {
			return err
		}
		position, err := loadTradeFuturesPositionTx(tx, userId, symbol, side)
		if err != nil {
			return err
		}
		if err = ensureTradeFuturesFundingStateTx(tx, position); err != nil {
			return err
		}
		var states []TradeFuturesFundingState
		if err = tx.Where("user_id = ? AND symbol = ? AND side = ? AND funding_at < ? AND (closed_at = 0 OR funding_at < closed_at)",
			userId, symbol, side, fundingTime).Order("position_id").Find(&states).Error; err != nil {
			return err
		}
		for _, state := range states {
			var exposures []TradeFuturesExposure
			if err = tx.Where("position_id = ? AND occurred_at <= ?", state.PositionId, fundingTime).
				Order("occurred_at DESC, id DESC").Limit(1).Find(&exposures).Error; err != nil {
				return err
			}
			if len(exposures) == 0 || exposures[0].Qty < 0 {
				return ErrTradeAmountInvalid
			}
			qty := exposures[0].Qty
			if qty > 0 {
				amount, err := pricing.Funding(tradesim.PositionSide(side), mark.Mul(tradesim.QtyFromUnits(qty)), rate)
				if err != nil {
					return ErrTradeAmountInvalid
				}
				entry := TradeLedger{Type: TradeLedgerFuturesFunding, Symbol: symbol, PositionId: state.PositionId, Qty: qty,
					Price: rate.String(), Pnl: amount, FundingTime: fundingTime}
				if position.PositionId == state.PositionId && position.Qty > 0 {
					if position.MarginMode == TradeFuturesCross {
						account.Cash += amount
						entry.Amount = amount
					} else {
						fromMargin := max(amount, -position.Margin)
						position.Margin += fromMargin
						account.Cash += amount - fromMargin
						entry.Amount = amount - fromMargin
					}
					position.Funding += amount
					if position.Funding <= -common.MaxQuota || position.Funding >= common.MaxQuota {
						return ErrTradeAmountInvalid
					}
					position.FundingAt = fundingTime
					if err = saveTradeFuturesPositionTx(tx, position); err != nil {
						return err
					}
				} else {
					account.Cash += amount
					entry.Amount = amount
					var history TradeFuturesHistory
					if err = tx.Where("position_id = ? AND user_id = ?", state.PositionId, userId).First(&history).Error; err != nil {
						return err
					}
					history.Funding += amount
					history.Pnl += amount
					if history.Funding <= -common.MaxQuota || history.Funding >= common.MaxQuota || history.Pnl <= -common.MaxQuota || history.Pnl >= common.MaxQuota {
						return ErrTradeAmountInvalid
					}
					if err = tx.Save(&history).Error; err != nil {
						return err
					}
				}
				if err = tradeLedgerTx(tx, account, &entry); err != nil {
					return err
				}
				applied = true
			}
			if err = tx.Model(&TradeFuturesFundingState{}).Where("position_id = ?", state.PositionId).Update("funding_at", fundingTime).Error; err != nil {
				return err
			}
		}
		if len(states) == 0 {
			return nil
		}
		if covered, err = settleTradeDeficitTx(tx, account); err != nil {
			return err
		}
		return saveTradeAccountTx(tx, account)
	})
	if err != nil {
		return false, err
	}
	afterTradeDeficitCovered(userId, covered)
	return applied, nil
}

// recordTradeFuturesExposureTx 与成交账单在同一事务内记录数量。调用方已经取得账户锁；时间不能取自 HTTP 请求到达时。
func recordTradeFuturesExposureTx(tx *gorm.DB, position *TradeFuturesPosition, entryId int) error {
	now := time.Now().UnixMilli()
	if position.PositionId == entryId {
		position.FundingAt = now
		if err := tx.Create(&TradeFuturesFundingState{PositionId: position.PositionId, UserId: position.UserId, Symbol: position.Symbol,
			Side: position.Side, MarginMode: position.MarginMode, OpenedAt: now, FundingAt: now}).Error; err != nil {
			return err
		}
	}
	if err := tx.Create(&TradeFuturesExposure{Id: entryId, PositionId: position.PositionId, OccurredAt: now, Qty: position.Qty}).Error; err != nil {
		return err
	}
	if position.Qty == 0 {
		return tx.Model(&TradeFuturesFundingState{}).Where("position_id = ?", position.PositionId).Update("closed_at", now).Error
	}
	return nil
}

// ListTradeFuturesFundingDue 包含平仓后仍需补结的生命周期；同一用户方向只需调用一次结算。
func ListTradeFuturesFundingDue(symbol string, fundingTime int64) ([]TradeFuturesFundingState, error) {
	var positions []TradeFuturesFundingState
	err := DB.Model(&TradeFuturesFundingState{}).Select("user_id, symbol, side").
		Where("symbol = ? AND funding_at < ? AND (closed_at = 0 OR funding_at < closed_at)", symbol, fundingTime).
		Group("user_id, symbol, side").Find(&positions).Error
	return positions, err
}

// GetTradeFuturesFundingFrom 给出仍需官方期次确认的最早游标。平仓后的第一期也会被读取，用来确认该生命周期已经没有欠费。
func GetTradeFuturesFundingFrom(symbol string) (int64, bool, error) {
	if err := recoverTradeFuturesFunding(symbol); err != nil {
		return 0, false, err
	}
	var row struct {
		Positions int64
		Earliest  int64
	}
	err := DB.Model(&TradeFuturesFundingState{}).Where("symbol = ? AND (closed_at = 0 OR funding_at < closed_at)", symbol).
		Select("COUNT(*) AS positions, COALESCE(MIN(funding_at), 0) AS earliest").Scan(&row).Error
	return row.Earliest, row.Positions > 0, err
}
