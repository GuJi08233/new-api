package model

import (
	"errors"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// 短期闪电冲榜赛：4 小时赛 / 每日收益率赛。
// 报名费汇入奖池；赛段内按「扣除存取款干扰后的真实收益率」排名；结束时按人数自动分奖(不足 2 人 100% 退费)。

const (
	TradeSprintKind4H    = "4h"
	TradeSprintKindDaily = "daily"

	TradeSprintStatusOpen    = "open"
	TradeSprintStatusSettled = "settled"

	TradeSprintEntryActive    = "active"
	TradeSprintEntryPaying    = "paying"
	TradeSprintEntryRefunding = "refunding"
	TradeSprintEntrySettled   = "settled"

	TradeLedgerSprintJoin   = "sprint_join"
	TradeLedgerSprintPrize  = "sprint_prize"
	TradeLedgerSprintRefund = "sprint_refund"

	// 参赛后需留存至少 10 USDT 资产，防止 0.01 USDT 微小本金刷出虚假极高收益率。
	TradeSprintMinEquityUsd = 10
)

var (
	ErrTradeSprintDisabled        = errors.New("sprint contest is disabled")
	ErrTradeSprintAlreadyJoined   = errors.New("already joined this sprint contest")
	ErrTradeSprintClosed          = errors.New("sprint registration is closed")
	ErrTradeSprintEquityTooLow    = errors.New("insufficient equity after entry fee to join sprint")
	ErrTradeSprintBonusNotAllowed = errors.New("trade bonus cannot be used to join sprint contests")
)

type TradeSprint struct {
	Id               int    `json:"id"`
	PeriodKey        string `json:"period_key" gorm:"type:varchar(48);uniqueIndex;not null"`
	Kind             string `json:"kind" gorm:"type:varchar(16);index;not null"`
	DurationSec      int64  `json:"duration_sec" gorm:"type:bigint;not null"`
	StartTime        int64  `json:"start_time" gorm:"type:bigint;not null"`
	RegisterDeadline int64  `json:"register_deadline" gorm:"type:bigint;not null"`
	EndTime          int64  `json:"end_time" gorm:"type:bigint;index;not null"`
	EntryFee         int    `json:"entry_fee" gorm:"type:bigint;not null;default:0"`
	PrizePool        int    `json:"prize_pool" gorm:"type:bigint;not null;default:0"`
	FeeBps           int    `json:"fee_bps" gorm:"type:int;not null;default:500"`
	Participants     int    `json:"participants" gorm:"type:int;not null;default:0"`
	Status           string `json:"status" gorm:"type:varchar(16);index;not null"`
	SettledAt        int64  `json:"settled_at" gorm:"type:bigint"`
	CreatedAt        int64  `json:"created_at" gorm:"type:bigint"`
}

type TradeSprintEntry struct {
	Id          int    `json:"id"`
	SprintId    int    `json:"sprint_id" gorm:"uniqueIndex:idx_trade_sprint_user,priority:1;not null"`
	UserId      int    `json:"user_id" gorm:"uniqueIndex:idx_trade_sprint_user,priority:2;index;not null"`
	StartEquity int    `json:"start_equity" gorm:"type:bigint;not null"`
	StartNetIn  int    `json:"start_net_in" gorm:"type:bigint;not null"`
	EntryFee    int    `json:"entry_fee" gorm:"type:bigint;not null"`
	EndEquity   int    `json:"end_equity" gorm:"type:bigint;not null;default:0"`
	EndNetIn    int    `json:"end_net_in" gorm:"type:bigint;not null;default:0"`
	ReturnBps   int64  `json:"return_bps" gorm:"type:bigint;not null;default:0"`
	FinalRank   int    `json:"final_rank" gorm:"type:int;not null;default:0"`
	Prize       int    `json:"prize" gorm:"type:bigint;not null;default:0"`
	Status      string `json:"status" gorm:"type:varchar(16);index;not null"`
	CreatedAt   int64  `json:"created_at" gorm:"type:bigint"`
	UpdatedAt   int64  `json:"updated_at" gorm:"type:bigint"`
}

// ComputeSprintReturnBps 计算扣除存取款干扰后的真实赛段收益率(万分比，10000 = +100%)。
// 分母同时取初始本金、10 USDT 最低基数、以及初始本金加上赛段内净转入增量中的最大值，杜绝盘中大额加金刷收益率。
func ComputeSprintReturnBps(startEquity, startNetIn, curEquity, curNetIn, minBaseQuota int) (int64, int) {
	profit := (curEquity - curNetIn) - (startEquity - startNetIn)
	netDelta := curNetIn - startNetIn
	base := max(startEquity, minBaseQuota)
	if netDelta > 0 {
		base = max(base, startEquity+netDelta)
	}
	if base <= 0 {
		return 0, profit
	}
	bps := decimal.NewFromInt(int64(profit)).
		Mul(decimal.NewFromInt(10000)).
		Div(decimal.NewFromInt(int64(base))).
		Round(0).
		IntPart()
	return bps, profit
}

// SprintPrizeSplitBps 按参赛人数返回各名次瓜分可分配奖池的万分比(总和恒 <= 10000)。
func SprintPrizeSplitBps(participants int) []int {
	switch {
	case participants < 2:
		return nil
	case participants == 2:
		return []int{7000, 3000}
	case participants < 10:
		return []int{5000, 3000, 2000}
	default:
		return []int{4000, 2500, 1500, 1200, 800}
	}
}

func EnsureTradeSprint(sprint TradeSprint) (*TradeSprint, error) {
	if sprint.PeriodKey == "" || sprint.Kind == "" || sprint.DurationSec <= 0 || sprint.EndTime <= sprint.StartTime {
		return nil, ErrTradeAmountInvalid
	}
	if sprint.Status == "" {
		sprint.Status = TradeSprintStatusOpen
	}
	if sprint.CreatedAt == 0 {
		sprint.CreatedAt = common.GetTimestamp()
	}
	if err := DB.Clauses(clause.OnConflict{DoNothing: true}).Create(&sprint).Error; err != nil {
		return nil, err
	}
	var saved TradeSprint
	err := DB.Where("period_key = ?", sprint.PeriodKey).First(&saved).Error
	return &saved, err
}

func JoinTradeSprint(sprintId, userId int, equity TradeEquity, market TradeFuturesMarket) (*TradeSprintEntry, error) {
	if sprintId <= 0 || userId <= 0 {
		return nil, ErrTradeAmountInvalid
	}
	perUsd, err := TradeQuotaPerUsd()
	if err != nil {
		return nil, err
	}
	minRemain, err := common.QuotaFromDecimalStrict(decimal.NewFromInt(TradeSprintMinEquityUsd).Mul(decimal.NewFromInt(int64(perUsd))))
	if err != nil {
		return nil, err
	}
	var entry TradeSprintEntry
	err = DB.Transaction(func(tx *gorm.DB) error {
		account, err := lockTradeAccountTx(tx, userId)
		if err != nil {
			return err
		}
		// 持有未结清体验金的账户禁止参赛，杜绝体验金洗钱进冲榜奖池或靠体验金刷假收益率。
		if account.BonusPrincipal > 0 {
			return ErrTradeSprintBonusNotAllowed
		}
		// 在账户锁内严格校验估值对应的资产版本与净转入，彻底消除并发充提 TOCTOU 竞态刷收益率漏洞。
		if err := checkTradeEquity(account, equity); err != nil {
			return err
		}
		if equity.Account.TotalIn != account.TotalIn || equity.Account.TotalOut != account.TotalOut {
			return ErrTradeValuationStale
		}
		var sprint TradeSprint
		if err := lockForUpdate(tx).Where("id = ?", sprintId).First(&sprint).Error; err != nil {
			return err
		}
		now := time.Now().Unix()
		if sprint.Status != TradeSprintStatusOpen || now >= sprint.RegisterDeadline || now >= sprint.EndTime {
			return ErrTradeSprintClosed
		}
		var existing int64
		if err := tx.Model(&TradeSprintEntry{}).Where("sprint_id = ? AND user_id = ?", sprint.Id, userId).Count(&existing).Error; err != nil {
			return err
		}
		if existing > 0 {
			return ErrTradeSprintAlreadyJoined
		}
		fee := sprint.EntryFee
		if fee < 0 || fee >= common.MaxQuota || sprint.PrizePool >= common.MaxQuota-fee {
			return ErrTradeAmountInvalid
		}
		if fee > 0 {
			if err := checkTradeAffordTx(tx, account, fee, fee, market); err != nil {
				return err
			}
		}
		ownEquity := max(0, equity.Value-account.BonusPrincipal)
		postJoinEquity := ownEquity - fee
		if postJoinEquity < minRemain {
			return ErrTradeSprintEquityTooLow
		}
		account.Cash -= fee
		entry = TradeSprintEntry{
			SprintId:    sprint.Id,
			UserId:      userId,
			StartEquity: postJoinEquity,
			StartNetIn:  account.TotalIn - account.TotalOut,
			EntryFee:    fee,
			Status:      TradeSprintEntryActive,
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		if err := tx.Create(&entry).Error; err != nil {
			return err
		}
		if err := tx.Model(&TradeSprint{}).Where("id = ?", sprint.Id).Updates(map[string]any{
			"prize_pool":   gorm.Expr("prize_pool + ?", fee),
			"participants": gorm.Expr("participants + ?", 1),
		}).Error; err != nil {
			return err
		}
		if err := saveTradeAccountTx(tx, account); err != nil {
			return err
		}
		if fee > 0 {
			return tx.Create(&TradeLedger{
				UserId:    userId,
				Type:      TradeLedgerSprintJoin,
				Symbol:    sprint.Kind + "-SPRINT",
				OrderId:   entry.Id,
				Amount:    -fee,
				Balance:   account.Cash + account.Frozen,
				CreatedAt: now,
			}).Error
		}
		return nil
	})
	return &entry, err
}

type TradeSprintSettlementRow struct {
	EntryId   int
	UserId    int
	EndEquity int
	EndNetIn  int
	ReturnBps int64
	FinalRank int
	Prize     int
	Refund    bool
}

// SettleTradeSprint 幂等完成闪电冲榜赛结算与奖金发放。
// 两阶段崩溃安全：先在事务内持久化每条参赛记录的排名、奖金与 paying/refunding 状态并关闭赛事，再逐户加账户锁安全入账；
// 若中途崩溃，下次轮询由 RecoverPendingTradeSprintEntries 自动补发。
func SettleTradeSprint(sprintId int, rows []TradeSprintSettlementRow) error {
	now := common.GetTimestamp()
	var sprint TradeSprint
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := lockForUpdate(tx).Where("id = ?", sprintId).First(&sprint).Error; err != nil {
			return err
		}
		if sprint.Status != TradeSprintStatusOpen {
			return nil
		}
		for _, row := range rows {
			nextStatus := TradeSprintEntryPaying
			if row.Refund {
				nextStatus = TradeSprintEntryRefunding
			}
			if err := tx.Model(&TradeSprintEntry{}).
				Where("id = ? AND sprint_id = ? AND status = ?", row.EntryId, sprint.Id, TradeSprintEntryActive).
				Updates(map[string]any{
					"end_equity": row.EndEquity,
					"end_net_in": row.EndNetIn,
					"return_bps": row.ReturnBps,
					"final_rank": row.FinalRank,
					"prize":      row.Prize,
					"status":     nextStatus,
					"updated_at": now,
				}).Error; err != nil {
				return err
			}
		}
		return tx.Model(&TradeSprint{}).Where("id = ? AND status = ?", sprint.Id, TradeSprintStatusOpen).
			Updates(map[string]any{"status": TradeSprintStatusSettled, "settled_at": now}).Error
	})
	if err != nil {
		return err
	}
	var pending []TradeSprintEntry
	if err := DB.Where("sprint_id = ? AND status IN ?", sprintId, []string{TradeSprintEntryPaying, TradeSprintEntryRefunding}).Order("id ASC").Find(&pending).Error; err != nil {
		return err
	}
	for _, item := range pending {
		if err := settleSingleSprintEntry(sprint.Kind, item.Id); err != nil {
			return err
		}
	}
	return nil
}

// RecoverPendingTradeSprintEntries 补发因进程崩溃停留在 paying/refunding 状态的冲榜赛奖金或退费。
func RecoverPendingTradeSprintEntries(limit int) error {
	if limit <= 0 {
		limit = 50
	}
	var pending []TradeSprintEntry
	if err := DB.Where("status IN ?", []string{TradeSprintEntryPaying, TradeSprintEntryRefunding}).Order("id ASC").Limit(limit).Find(&pending).Error; err != nil {
		return err
	}
	if len(pending) == 0 {
		return nil
	}
	sprintIds := make([]int, 0, len(pending))
	seen := map[int]bool{}
	for _, e := range pending {
		if !seen[e.SprintId] {
			seen[e.SprintId] = true
			sprintIds = append(sprintIds, e.SprintId)
		}
	}
	var sprints []TradeSprint
	if err := DB.Where("id IN ?", sprintIds).Find(&sprints).Error; err != nil {
		return err
	}
	kindById := make(map[int]string, len(sprints))
	for _, s := range sprints {
		kindById[s.Id] = s.Kind
	}
	for _, item := range pending {
		kind := kindById[item.SprintId]
		if kind == "" {
			kind = TradeSprintKind4H
		}
		if err := settleSingleSprintEntry(kind, item.Id); err != nil {
			return err
		}
	}
	return nil
}

func settleSingleSprintEntry(kind string, entryId int) error {
	var identity TradeSprintEntry
	if err := DB.Where("id = ?", entryId).First(&identity).Error; err != nil {
		return err
	}
	if identity.Status != TradeSprintEntryPaying && identity.Status != TradeSprintEntryRefunding {
		return nil
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		account, err := lockTradeAccountTx(tx, identity.UserId)
		if err != nil {
			return err
		}
		var entry TradeSprintEntry
		if err := tx.Where("id = ?", entryId).First(&entry).Error; err != nil {
			return err
		}
		if entry.Status != TradeSprintEntryPaying && entry.Status != TradeSprintEntryRefunding {
			return nil
		}
		isRefund := entry.Status == TradeSprintEntryRefunding
		now := common.GetTimestamp()
		credit := entry.Prize
		if credit < 0 || account.Cash >= common.MaxQuota-credit {
			return ErrTradeAmountInvalid
		}
		account.Cash += credit
		entry.Status = TradeSprintEntrySettled
		entry.UpdatedAt = now
		if err := tx.Save(&entry).Error; err != nil {
			return err
		}
		if err := saveTradeAccountTx(tx, account); err != nil {
			return err
		}
		if credit > 0 {
			ledgerType := TradeLedgerSprintPrize
			if isRefund {
				ledgerType = TradeLedgerSprintRefund
			}
			return tx.Create(&TradeLedger{
				UserId:    entry.UserId,
				Type:      ledgerType,
				Symbol:    kind + "-SPRINT",
				OrderId:   entry.Id,
				Amount:    credit,
				Pnl:       credit - entry.EntryFee,
				Balance:   account.Cash + account.Frozen,
				CreatedAt: now,
			}).Error
		}
		return nil
	})
}

func ListTradeSprintEntries(sprintId int) ([]TradeSprintEntry, error) {
	rows := []TradeSprintEntry{}
	err := DB.Where("sprint_id = ?", sprintId).Order("id ASC").Find(&rows).Error
	return rows, err
}

func ListRecentTradeSprints(kind string, limit int) ([]TradeSprint, error) {
	rows := []TradeSprint{}
	query := DB.Model(&TradeSprint{})
	if kind != "" {
		query = query.Where("kind = ?", kind)
	}
	err := query.Order("end_time DESC").Limit(limit).Find(&rows).Error
	return rows, err
}
