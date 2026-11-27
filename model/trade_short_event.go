package model

import (
	"errors"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// 短线事件玩法统一模型：覆盖 AI 擂台 PK(pk)、全品种极速涨跌(flash)、价格区间竞猜(range)与极速触碰(touch)。
// 支持两种结算模型：
// - pool：共享多人奖池分账(Pari-Mutuel)，零平台对赌风险，单边押注或平局 100% 退本；
// - instant：下单瞬间按新鲜实时价定锚，到期/触碰按配置固定赔率结算，单笔与在途受配置上限严格约束。

const (
	TradeShortKindPK    = "pk"
	TradeShortKindFlash = "flash"
	TradeShortKindRange = "range"
	TradeShortKindTouch = "touch"

	TradeShortModePool    = "pool"
	TradeShortModeInstant = "instant"

	TradeShortStatusActive   = "active"
	TradeShortStatusWon      = "won"
	TradeShortStatusLost     = "lost"
	TradeShortStatusRefunded = "refunded"

	TradeShortRoundOpen     = "open"
	TradeShortRoundSettled  = "settled"
	TradeShortRoundRefunded = "refunded"

	TradeShortOutcomeRefund = "REFUND"

	TradeLedgerShortBet    = "short_bet"
	TradeLedgerShortPayout = "short_payout"
	TradeLedgerShortRefund = "short_refund"
)

var (
	ErrTradeShortDisabled     = errors.New("short-term trade events are disabled")
	ErrTradeShortRoundClosed  = errors.New("short-term event round is closed")
	ErrTradeShortLimitReached = errors.New("short-term event stake limit reached")
	ErrTradeShortInvalid      = errors.New("short-term event order is invalid")
)

// TradeShortRound 是共享奖池局(PK 擂台奖池场 / 区间竞猜奖池场)。
// 投注期在 StartTime 之前结束(或前 20% 窗口)，防止临期偷跑；总奖池按胜方本金占比分账，单边押注或平局全额退本。
type TradeShortRound struct {
	Id           int    `json:"id"`
	RoundKey     string `json:"round_key" gorm:"type:varchar(96);uniqueIndex;not null"`
	Kind         string `json:"kind" gorm:"type:varchar(16);index;not null"`
	Title        string `json:"title" gorm:"type:varchar(120)"`
	SymbolA      string `json:"symbol_a" gorm:"type:varchar(24);not null"`
	SymbolB      string `json:"symbol_b" gorm:"type:varchar(24)"`
	DurationSec  int64  `json:"duration_sec" gorm:"type:bigint;not null"`
	LockTime     int64  `json:"lock_time" gorm:"type:bigint;index;not null"`
	StartTime    int64  `json:"start_time" gorm:"type:bigint;not null"`
	EndTime      int64  `json:"end_time" gorm:"type:bigint;index;not null"`
	ThresholdBps int    `json:"threshold_bps" gorm:"type:int;not null;default:0"`
	StartPriceA  string `json:"start_price_a" gorm:"type:varchar(40)"`
	StartPriceB  string `json:"start_price_b" gorm:"type:varchar(40)"`
	EndPriceA    string `json:"end_price_a" gorm:"type:varchar(40)"`
	EndPriceB    string `json:"end_price_b" gorm:"type:varchar(40)"`
	TotalPool    int    `json:"total_pool" gorm:"type:bigint;not null;default:0"`
	FeeBps       int    `json:"fee_bps" gorm:"type:int;not null;default:300"`
	Outcome      string `json:"outcome" gorm:"type:varchar(24)"`
	Status       string `json:"status" gorm:"type:varchar(16);index;not null"`
	SettledAt    int64  `json:"settled_at" gorm:"type:bigint"`
	CreatedAt    int64  `json:"created_at" gorm:"type:bigint"`
}

// TradeShortOrder 记录用户在短线事件中的每一笔投入(既支持关联 RoundId 的奖池局，也支持独立到期结算的即时单)。
type TradeShortOrder struct {
	Id           int    `json:"id"`
	UserId       int    `json:"user_id" gorm:"index:idx_trade_short_user,priority:1;not null"`
	RoundId      int    `json:"round_id" gorm:"index;not null;default:0"`
	Kind         string `json:"kind" gorm:"type:varchar(16);index;not null"`
	Mode         string `json:"mode" gorm:"type:varchar(16);not null"`
	SymbolA      string `json:"symbol_a" gorm:"type:varchar(24);not null"`
	SymbolB      string `json:"symbol_b" gorm:"type:varchar(24)"`
	Choice       string `json:"choice" gorm:"type:varchar(24);not null"`
	DurationSec  int64  `json:"duration_sec" gorm:"type:bigint;not null"`
	ThresholdBps int    `json:"threshold_bps" gorm:"type:int;not null;default:0"`
	EntryPriceA  string `json:"entry_price_a" gorm:"type:varchar(40)"`
	EntryPriceB  string `json:"entry_price_b" gorm:"type:varchar(40)"`
	SettlePriceA string `json:"settle_price_a" gorm:"type:varchar(40)"`
	SettlePriceB string `json:"settle_price_b" gorm:"type:varchar(40)"`
	Stake        int    `json:"stake" gorm:"type:bigint;not null"`
	PayoutBps    int    `json:"payout_bps" gorm:"type:int;not null;default:0"`
	Payout       int    `json:"payout" gorm:"type:bigint;not null;default:0"`
	Outcome      string `json:"outcome" gorm:"type:varchar(24)"`
	Status       string `json:"status" gorm:"type:varchar(16);index:idx_trade_short_user,priority:2;not null"`
	ExpiresAt    int64  `json:"expires_at" gorm:"type:bigint;index;not null"`
	SettledAt    int64  `json:"settled_at" gorm:"type:bigint"`
	CreatedAt    int64  `json:"created_at" gorm:"type:bigint"`
}

// EnsureTradeShortRound 按唯一 RoundKey 幂等创建共享奖池局。
func EnsureTradeShortRound(round TradeShortRound) (*TradeShortRound, error) {
	if round.RoundKey == "" || round.Kind == "" || round.SymbolA == "" || round.DurationSec <= 0 || round.EndTime <= round.StartTime {
		return nil, ErrTradeShortInvalid
	}
	if round.Status == "" {
		round.Status = TradeShortRoundOpen
	}
	if round.CreatedAt == 0 {
		round.CreatedAt = common.GetTimestamp()
	}
	if err := DB.Clauses(clause.OnConflict{DoNothing: true}).Create(&round).Error; err != nil {
		return nil, err
	}
	var saved TradeShortRound
	err := DB.Where("round_key = ?", round.RoundKey).First(&saved).Error
	return &saved, err
}

func GetTradeShortRound(roundId int) (*TradeShortRound, error) {
	var round TradeShortRound
	err := DB.Where("id = ?", roundId).First(&round).Error
	return &round, err
}

// GetTradeShortRoundPools 汇总指定奖池局每个选项的当前总下注额。
func GetTradeShortRoundPools(roundIds []int) (map[int]map[string]int, error) {
	result := make(map[int]map[string]int, len(roundIds))
	if len(roundIds) == 0 {
		return result, nil
	}
	var rows []struct {
		RoundId int
		Choice  string
		Total   int
	}
	err := DB.Model(&TradeShortOrder{}).
		Select("round_id, choice, COALESCE(SUM(stake), 0) AS total").
		Where("round_id IN ? AND mode = ?", roundIds, TradeShortModePool).
		Group("round_id, choice").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		if result[row.RoundId] == nil {
			result[row.RoundId] = map[string]int{}
		}
		result[row.RoundId][row.Choice] = row.Total
	}
	return result, nil
}

type TradeShortOrderInput struct {
	UserId       int
	RoundId      int
	Kind         string
	Mode         string
	SymbolA      string
	SymbolB      string
	Choice       string
	DurationSec  int64
	ThresholdBps int
	EntryPriceA  string
	EntryPriceB  string
	Stake        int
	PayoutBps    int
	MaxActive    int
	ExpiresAt    int64
	Market       TradeFuturesMarket
}

// PlaceTradeShortOrder 在账户锁内校验可用资金、在途上限并扣款建单；奖池模式同步递增局总奖池。
func PlaceTradeShortOrder(input TradeShortOrderInput) (*TradeShortOrder, error) {
	if input.UserId <= 0 || input.Stake <= 0 || input.Stake >= common.MaxQuota ||
		input.MaxActive <= 0 || input.MaxActive >= common.MaxQuota ||
		input.SymbolA == "" || input.Choice == "" || input.DurationSec <= 0 || input.ExpiresAt <= 0 {
		return nil, ErrTradeShortInvalid
	}
	if input.Mode != TradeShortModePool && input.Mode != TradeShortModeInstant {
		return nil, ErrTradeShortInvalid
	}
	var order TradeShortOrder
	err := DB.Transaction(func(tx *gorm.DB) error {
		account, err := lockTradeAccountTx(tx, input.UserId)
		if err != nil {
			return err
		}
		now := time.Now().Unix()
		if input.Mode == TradeShortModePool {
			if input.RoundId <= 0 {
				return ErrTradeShortInvalid
			}
			var round TradeShortRound
			if err := lockForUpdate(tx).Where("id = ?", input.RoundId).First(&round).Error; err != nil {
				return err
			}
			if round.Status != TradeShortRoundOpen || now >= round.LockTime || now >= round.EndTime {
				return ErrTradeShortRoundClosed
			}
			if round.TotalPool >= common.MaxQuota-input.Stake {
				return ErrTradeAmountInvalid
			}
			if err := tx.Model(&TradeShortRound{}).Where("id = ?", round.Id).
				Update("total_pool", gorm.Expr("total_pool + ?", input.Stake)).Error; err != nil {
				return err
			}
			input.ExpiresAt = round.EndTime
			input.DurationSec = round.DurationSec
			input.ThresholdBps = round.ThresholdBps
			input.SymbolA = round.SymbolA
			input.SymbolB = round.SymbolB
			input.Kind = round.Kind
		} else if now >= input.ExpiresAt {
			return ErrTradeShortRoundClosed
		}

		var activeStake int64
		if err := tx.Model(&TradeShortOrder{}).
			Select("COALESCE(SUM(stake), 0)").
			Where("user_id = ? AND status = ?", input.UserId, TradeShortStatusActive).
			Scan(&activeStake).Error; err != nil {
			return err
		}
		if activeStake > int64(input.MaxActive) || int64(input.Stake) > int64(input.MaxActive)-activeStake {
			return ErrTradeShortLimitReached
		}
		if err := checkTradeAffordTx(tx, account, input.Stake, input.Stake, input.Market); err != nil {
			return err
		}
		account.Cash -= input.Stake
		order = TradeShortOrder{
			UserId:       input.UserId,
			RoundId:      input.RoundId,
			Kind:         input.Kind,
			Mode:         input.Mode,
			SymbolA:      input.SymbolA,
			SymbolB:      input.SymbolB,
			Choice:       input.Choice,
			DurationSec:  input.DurationSec,
			ThresholdBps: input.ThresholdBps,
			EntryPriceA:  input.EntryPriceA,
			EntryPriceB:  input.EntryPriceB,
			Stake:        input.Stake,
			PayoutBps:    input.PayoutBps,
			Status:       TradeShortStatusActive,
			ExpiresAt:    input.ExpiresAt,
			CreatedAt:    now,
		}
		if err := tx.Create(&order).Error; err != nil {
			return err
		}
		if err := saveTradeAccountTx(tx, account); err != nil {
			return err
		}
		if err := creditGameCoinWagerFromTradeTx(tx, account, input.Stake, operation_setting.GetTradeSetting().TradeWagerSpotBps); err != nil {
			return err
		}
		ledgerSymbol := shortOrderLedgerSymbol(order.Kind, order.SymbolA, order.SymbolB)
		return tx.Create(&TradeLedger{
			UserId:    input.UserId,
			Type:      TradeLedgerShortBet,
			Symbol:    ledgerSymbol,
			OrderId:   order.Id,
			Price:     order.EntryPriceA,
			Amount:    -input.Stake,
			Balance:   account.Cash + account.Frozen,
			CreatedAt: now,
		}).Error
	})
	return &order, err
}

func shortOrderLedgerSymbol(kind, symbolA, symbolB string) string {
	switch kind {
	case TradeShortKindPK:
		if symbolB != "" {
			s := symbolA + "/" + symbolB
			if len(s) > 20 {
				return s[:20]
			}
			return s
		}
	}
	if len(symbolA) > 20 {
		return symbolA[:20]
	}
	return symbolA
}

// SettleTradeShortInstantOrder 幂等结算一笔即时短线单(涨跌/即时PK/区间/触碰)。
func SettleTradeShortInstantOrder(orderId int, outcome, settlePriceA, settlePriceB string) error {
	var identity TradeShortOrder
	if err := DB.Where("id = ?", orderId).First(&identity).Error; err != nil {
		return err
	}
	if identity.Status != TradeShortStatusActive || identity.Mode != TradeShortModeInstant {
		return nil
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		account, err := lockTradeAccountTx(tx, identity.UserId)
		if err != nil {
			return err
		}
		var order TradeShortOrder
		if err := tx.Where("id = ?", orderId).First(&order).Error; err != nil {
			return err
		}
		if order.Status != TradeShortStatusActive {
			return nil
		}
		now := common.GetTimestamp()
		payout := 0
		status := TradeShortStatusLost
		ledgerType := TradeLedgerShortPayout
		switch {
		case outcome == TradeShortOutcomeRefund:
			payout = order.Stake
			status = TradeShortStatusRefunded
			ledgerType = TradeLedgerShortRefund
		case outcome == order.Choice:
			status = TradeShortStatusWon
			payout, err = common.QuotaFromDecimalStrict(
				decimal.NewFromInt(int64(order.Stake)).
					Mul(decimal.NewFromInt(int64(order.PayoutBps))).
					Div(decimal.NewFromInt(10000)).
					Floor(),
			)
			if err != nil || payout < 0 {
				return ErrTradeAmountInvalid
			}
		}
		if payout > 0 && account.Cash >= common.MaxQuota-payout {
			return ErrTradeAmountInvalid
		}
		account.Cash += payout
		order.Payout = payout
		order.Outcome = outcome
		order.SettlePriceA = settlePriceA
		order.SettlePriceB = settlePriceB
		order.Status = status
		order.SettledAt = now
		if err := tx.Save(&order).Error; err != nil {
			return err
		}
		if err := saveTradeAccountTx(tx, account); err != nil {
			return err
		}
		return tx.Create(&TradeLedger{
			UserId:    order.UserId,
			Type:      ledgerType,
			Symbol:    shortOrderLedgerSymbol(order.Kind, order.SymbolA, order.SymbolB),
			OrderId:   order.Id,
			Price:     settlePriceA,
			Amount:    payout,
			Pnl:       payout - order.Stake,
			Balance:   account.Cash + account.Frozen,
			CreatedAt: now,
		}).Error
	})
}

// SettleTradeShortPoolRound 按 Pari-Mutuel 奖池规则结算一整局。
// 守恒不变量：
// - 单边押注 / 胜方奖池为 0 / 平局(REFUND) -> 100% 退还所有玩家本金，0 手续费；
// - 正常对决 -> 扣除 fee_bps 后按胜方本金占比向下取整派彩，总派彩恒 <= 总奖池。
func SettleTradeShortPoolRound(roundId int, outcome, startPriceA, startPriceB, endPriceA, endPriceB string) error {
	now := common.GetTimestamp()
	var orders []TradeShortOrder
	var round TradeShortRound
	var distributable, winPool int
	refundAll := outcome == TradeShortOutcomeRefund

	// 第一步：锁定该局并标记最终结果，同时读取所有待结算注单。
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := lockForUpdate(tx).Where("id = ?", roundId).First(&round).Error; err != nil {
			return err
		}
		if round.Status != TradeShortRoundOpen {
			return nil
		}
		if err := tx.Where("round_id = ? AND status = ?", round.Id, TradeShortStatusActive).Order("id ASC").Find(&orders).Error; err != nil {
			return err
		}
		totalPool := 0
		sides := map[string]int{}
		for _, ord := range orders {
			totalPool += ord.Stake
			sides[ord.Choice] += ord.Stake
		}
		winPool = sides[outcome]
		// 仅有一方下注或无人猜中时全额退本，绝不黑箱没收资金，也绝不穿仓。
		if len(sides) < 2 || winPool <= 0 {
			refundAll = true
		}
		if !refundAll {
			fee, feeErr := common.QuotaFromDecimalStrict(
				decimal.NewFromInt(int64(totalPool)).
					Mul(decimal.NewFromInt(int64(round.FeeBps))).
					Div(decimal.NewFromInt(10000)).
					Floor(),
			)
			if feeErr != nil {
				return ErrTradeAmountInvalid
			}
			distributable = max(0, totalPool-fee)
		}
		finalStatus := TradeShortRoundSettled
		finalOutcome := outcome
		if refundAll {
			finalStatus = TradeShortRoundRefunded
			finalOutcome = TradeShortOutcomeRefund
		}
		updates := map[string]any{
			"outcome":     finalOutcome,
			"status":      finalStatus,
			"settled_at":  now,
			"end_price_a": endPriceA,
			"end_price_b": endPriceB,
			"total_pool":  totalPool,
		}
		if startPriceA != "" && round.StartPriceA == "" {
			updates["start_price_a"] = startPriceA
		}
		if startPriceB != "" && round.StartPriceB == "" {
			updates["start_price_b"] = startPriceB
		}
		return tx.Model(&TradeShortRound{}).Where("id = ? AND status = ?", round.Id, TradeShortRoundOpen).Updates(updates).Error
	})
	if err != nil {
		return err
	}
	if round.Status != TradeShortRoundOpen {
		// 即使局状态已落库，仍补扫未结清注单以保证崩溃可恢复。
		if err := DB.Where("round_id = ? AND status = ?", roundId, TradeShortStatusActive).Order("id ASC").Find(&orders).Error; err != nil || len(orders) == 0 {
			return err
		}
		refundAll = round.Status == TradeShortRoundRefunded || round.Outcome == TradeShortOutcomeRefund
		outcome = round.Outcome
		totalPool := 0
		sides := map[string]int{}
		for _, ord := range orders {
			totalPool += ord.Stake
			sides[ord.Choice] += ord.Stake
		}
		winPool = sides[outcome]
		if len(sides) < 2 || winPool <= 0 {
			refundAll = true
		}
		if !refundAll {
			fee, _ := common.QuotaFromDecimalStrict(decimal.NewFromInt(int64(totalPool)).Mul(decimal.NewFromInt(int64(round.FeeBps))).Div(decimal.NewFromInt(10000)).Floor())
			distributable = max(0, totalPool-fee)
		}
	}

	// 第二步：逐单加账户行锁安全入账，严格幂等。
	for _, item := range orders {
		if err := settleSinglePoolOrder(item.Id, outcome, refundAll, distributable, winPool, endPriceA, endPriceB); err != nil {
			return err
		}
	}
	return nil
}

func settleSinglePoolOrder(orderId int, outcome string, refundAll bool, distributable, winPool int, endPriceA, endPriceB string) error {
	var identity TradeShortOrder
	if err := DB.Where("id = ?", orderId).First(&identity).Error; err != nil {
		return err
	}
	if identity.Status != TradeShortStatusActive {
		return nil
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		account, err := lockTradeAccountTx(tx, identity.UserId)
		if err != nil {
			return err
		}
		var order TradeShortOrder
		if err := tx.Where("id = ?", orderId).First(&order).Error; err != nil {
			return err
		}
		if order.Status != TradeShortStatusActive {
			return nil
		}
		now := common.GetTimestamp()
		payout := 0
		status := TradeShortStatusLost
		ledgerType := TradeLedgerShortPayout
		finalOutcome := outcome
		if refundAll {
			payout = order.Stake
			status = TradeShortStatusRefunded
			ledgerType = TradeLedgerShortRefund
			finalOutcome = TradeShortOutcomeRefund
		} else if order.Choice == outcome && winPool > 0 {
			status = TradeShortStatusWon
			payout, err = common.QuotaFromDecimalStrict(
				decimal.NewFromInt(int64(distributable)).
					Mul(decimal.NewFromInt(int64(order.Stake))).
					Div(decimal.NewFromInt(int64(winPool))).
					Floor(),
			)
			if err != nil || payout < 0 {
				return ErrTradeAmountInvalid
			}
		}
		if payout > 0 && account.Cash >= common.MaxQuota-payout {
			return ErrTradeAmountInvalid
		}
		account.Cash += payout
		order.Payout = payout
		order.Outcome = finalOutcome
		order.SettlePriceA = endPriceA
		order.SettlePriceB = endPriceB
		order.Status = status
		order.SettledAt = now
		if err := tx.Save(&order).Error; err != nil {
			return err
		}
		if err := saveTradeAccountTx(tx, account); err != nil {
			return err
		}
		return tx.Create(&TradeLedger{
			UserId:    order.UserId,
			Type:      ledgerType,
			Symbol:    shortOrderLedgerSymbol(order.Kind, order.SymbolA, order.SymbolB),
			OrderId:   order.Id,
			Price:     endPriceA,
			Amount:    payout,
			Pnl:       payout - order.Stake,
			Balance:   account.Cash + account.Frozen,
			CreatedAt: now,
		}).Error
	})
}

func ListTradeShortOrders(userId int, kind string, activeOnly bool, offset, limit int) ([]TradeShortOrder, int64, error) {
	rows := []TradeShortOrder{}
	query := DB.Model(&TradeShortOrder{}).Where("user_id = ?", userId)
	if kind != "" {
		query = query.Where("kind = ?", kind)
	}
	if activeOnly {
		query = query.Where("status = ?", TradeShortStatusActive)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := query.Order("id DESC").Offset(offset).Limit(limit).Find(&rows).Error
	return rows, total, err
}

func ListActiveTradeShortOrdersOf(userIds []int) ([]TradeShortOrder, error) {
	rows := []TradeShortOrder{}
	if len(userIds) == 0 || !DB.Migrator().HasTable(&TradeShortOrder{}) {
		return rows, nil
	}
	err := DB.Where("user_id IN ? AND status = ?", userIds, TradeShortStatusActive).Find(&rows).Error
	return rows, err
}
