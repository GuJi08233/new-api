package service

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/shopspring/decimal"
)

type TradeSprintStanding struct {
	Rank       int     `json:"rank"`
	UserId     int     `json:"user_id"`
	Name       string  `json:"name"`
	ReturnRate float64 `json:"return_rate"`
	Profit     int     `json:"profit"`
	Prize      int     `json:"prize"`
	Me         bool    `json:"me"`
}

type TradeSprintView struct {
	Sprint    model.TradeSprint     `json:"sprint"`
	Joined    bool                  `json:"joined"`
	CanJoin   bool                  `json:"can_join"`
	SplitBps  []int                 `json:"split_bps"`
	Standings []TradeSprintStanding `json:"standings"`
	Me        *TradeSprintStanding  `json:"me"`
}

func sprintWindowFor(kind string, now time.Time) (periodKey string, durationSec, startSec, deadlineSec, endSec int64) {
	if kind == model.TradeSprintKindDaily {
		start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		end := start.AddDate(0, 0, 1)
		return fmt.Sprintf("daily-%s", start.Format("2006-01-02")), 86400, start.Unix(), end.Unix() - 7200, end.Unix()
	}
	blockHour := (now.Hour() / 4) * 4
	start := time.Date(now.Year(), now.Month(), now.Day(), blockHour, 0, 0, 0, now.Location())
	end := start.Add(4 * time.Hour)
	return fmt.Sprintf("4h-%s-%02d", start.Format("2006-01-02"), blockHour), 14400, start.Unix(), end.Unix() - 1800, end.Unix()
}

func EnsureCurrentTradeSprint(kind string) (*model.TradeSprint, error) {
	if kind != model.TradeSprintKind4H && kind != model.TradeSprintKindDaily {
		kind = model.TradeSprintKind4H
	}
	setting := operation_setting.GetTradeSetting()
	perUsd, err := model.TradeQuotaPerUsd()
	if err != nil {
		return nil, err
	}
	entryFee, err := common.QuotaFromDecimalStrict(decimal.NewFromInt(int64(setting.SprintEntryFeeUsd)).Mul(decimal.NewFromInt(int64(perUsd))))
	if err != nil {
		return nil, err
	}
	periodKey, durationSec, startSec, deadlineSec, endSec := sprintWindowFor(kind, time.Now())
	return model.EnsureTradeSprint(model.TradeSprint{
		PeriodKey:        periodKey,
		Kind:             kind,
		DurationSec:      durationSec,
		StartTime:        startSec,
		RegisterDeadline: deadlineSec,
		EndTime:          endSec,
		EntryFee:         entryFee,
		FeeBps:           setting.SprintFeeBps,
		Status:           model.TradeSprintStatusOpen,
	})
}

func buildSprintStandings(sprint model.TradeSprint, entries []model.TradeSprintEntry, viewerId int) ([]TradeSprintStanding, *TradeSprintStanding, error) {
	if len(entries) == 0 {
		return []TradeSprintStanding{}, nil, nil
	}
	userIds := make([]int, len(entries))
	for i, e := range entries {
		userIds[i] = e.UserId
	}
	names, err := model.ListTradeUserNames(userIds)
	if err != nil {
		return nil, nil, err
	}
	perUsd, err := model.TradeQuotaPerUsd()
	if err != nil {
		return nil, nil, err
	}
	minBase, err := common.QuotaFromDecimalStrict(decimal.NewFromInt(model.TradeSprintMinEquityUsd).Mul(decimal.NewFromInt(int64(perUsd))))
	if err != nil {
		return nil, nil, err
	}

	type scored struct {
		entry  model.TradeSprintEntry
		bps    int64
		profit int
	}
	list := make([]scored, 0, len(entries))

	if sprint.Status == model.TradeSprintStatusSettled {
		for _, e := range entries {
			profit := (e.EndEquity - e.EndNetIn) - (e.StartEquity - e.StartNetIn)
			list = append(list, scored{entry: e, bps: e.ReturnBps, profit: profit})
		}
		slices.SortFunc(list, func(a, b scored) int {
			return cmp.Or(cmp.Compare(a.entry.FinalRank, b.entry.FinalRank), cmp.Compare(b.bps, a.bps), cmp.Compare(a.entry.UserId, b.entry.UserId))
		})
	} else {
		var accounts []model.TradeAccount
		if err := model.DB.Where("user_id IN ?", userIds).Find(&accounts).Error; err != nil {
			return nil, nil, err
		}
		valuations, err := valueTradeAccounts(accounts)
		if err != nil {
			return nil, nil, err
		}
		byUserAcc := make(map[int]model.TradeAccount, len(accounts))
		byUserVal := make(map[int]TradeValuation, len(accounts))
		for i, acc := range accounts {
			byUserAcc[acc.UserId] = acc
			byUserVal[acc.UserId] = valuations[i]
		}
		for _, e := range entries {
			acc := byUserAcc[e.UserId]
			val := byUserVal[e.UserId]
			curNetIn := acc.TotalIn - acc.TotalOut
			// 扣除体验金本金，杜绝赛中领取体验金被误算为赛段交易盈利。
			ownEquity := max(0, val.Equity-acc.BonusPrincipal)
			bps, profit := model.ComputeSprintReturnBps(e.StartEquity, e.StartNetIn, ownEquity, curNetIn, minBase)
			list = append(list, scored{entry: e, bps: bps, profit: profit})
		}
		slices.SortFunc(list, func(a, b scored) int {
			return cmp.Or(cmp.Compare(b.bps, a.bps), cmp.Compare(b.profit, a.profit), cmp.Compare(a.entry.UserId, b.entry.UserId))
		})
	}

	split := model.SprintPrizeSplitBps(len(list))
	distributable := 0
	if sprint.PrizePool > 0 && len(split) > 0 {
		fee, _ := common.QuotaFromDecimalStrict(decimal.NewFromInt(int64(sprint.PrizePool)).Mul(decimal.NewFromInt(int64(sprint.FeeBps))).Div(decimal.NewFromInt(10000)).Floor())
		distributable = max(0, sprint.PrizePool-fee)
	}

	standings := make([]TradeSprintStanding, 0, len(list))
	var me *TradeSprintStanding
	for i, item := range list {
		rank := i + 1
		prize := item.entry.Prize
		if sprint.Status == model.TradeSprintStatusOpen && i < len(split) && distributable > 0 {
			prize, _ = common.QuotaFromDecimalStrict(decimal.NewFromInt(int64(distributable)).Mul(decimal.NewFromInt(int64(split[i]))).Div(decimal.NewFromInt(10000)).Floor())
		}
		name := names[item.entry.UserId]
		if name == "" {
			name = fmt.Sprintf("Trader #%d", item.entry.UserId)
		}
		row := TradeSprintStanding{
			Rank:       rank,
			UserId:     item.entry.UserId,
			Name:       name,
			ReturnRate: float64(item.bps) / 10000.0,
			Profit:     item.profit,
			Prize:      prize,
			Me:         item.entry.UserId == viewerId,
		}
		standings = append(standings, row)
		if row.Me {
			copyRow := row
			me = &copyRow
		}
	}
	return standings, me, nil
}

func GetTradeSprintBoard(viewerId int) ([]TradeSprintView, []model.TradeSprint, error) {
	setting := operation_setting.GetTradeSetting()
	now := common.GetTimestamp()
	active := make([]TradeSprintView, 0, 2)
	for _, kind := range []string{model.TradeSprintKind4H, model.TradeSprintKindDaily} {
		sprint, err := EnsureCurrentTradeSprint(kind)
		if err != nil {
			return nil, nil, err
		}
		entries, err := model.ListTradeSprintEntries(sprint.Id)
		if err != nil {
			return nil, nil, err
		}
		standings, me, err := buildSprintStandings(*sprint, entries, viewerId)
		if err != nil {
			return nil, nil, err
		}
		joined := me != nil
		canJoin := setting.Enabled && setting.SprintEnabled && !joined && sprint.Status == model.TradeSprintStatusOpen && now < sprint.RegisterDeadline
		active = append(active, TradeSprintView{
			Sprint:    *sprint,
			Joined:    joined,
			CanJoin:   canJoin,
			SplitBps:  model.SprintPrizeSplitBps(max(3, len(entries))),
			Standings: standings,
			Me:        me,
		})
	}
	recent, err := model.ListRecentTradeSprints("", 10)
	if err != nil {
		return nil, nil, err
	}
	return active, recent, nil
}

func JoinCurrentTradeSprint(ctx context.Context, userId int, kind string) (*model.TradeSprintEntry, error) {
	setting := operation_setting.GetTradeSetting()
	if !setting.Enabled || !setting.SprintEnabled {
		return nil, model.ErrTradeSprintDisabled
	}
	sprint, err := EnsureCurrentTradeSprint(kind)
	if err != nil {
		return nil, err
	}
	for range 3 {
		account, valuation, valErr := ValueTradeUserForTransfer(ctx, userId)
		if valErr != nil {
			return nil, valErr
		}
		equity := model.TradeEquity{Value: valuation.Equity, Known: true, Account: account}
		entry, joinErr := model.JoinTradeSprint(sprint.Id, userId, equity, TradeAccountingMarket())
		if errors.Is(joinErr, model.ErrTradeValuationStale) {
			continue
		}
		return entry, joinErr
	}
	return nil, model.ErrTradeValuationStale
}

var tradeSprintStartOnce sync.Once

func StartTradeSprints() {
	if !common.IsMasterNode {
		return
	}
	tradeSprintStartOnce.Do(func() {
		go func() {
			ticker := time.NewTicker(15 * time.Second)
			defer ticker.Stop()
			for range ticker.C {
				settleEndedSprints()
			}
		}()
	})
}

func settleEndedSprints() {
	if !model.DB.Migrator().HasTable(&model.TradeSprint{}) {
		return
	}
	if err := model.RecoverPendingTradeSprintEntries(50); err != nil {
		common.SysError("sprint settlement recovery: " + err.Error())
	}
	now := common.GetTimestamp()
	var ended []model.TradeSprint
	if err := model.DB.Where("status = ? AND end_time <= ?", model.TradeSprintStatusOpen, now).Limit(20).Find(&ended).Error; err != nil {
		common.SysError("sprint settlement scan: " + err.Error())
		return
	}
	perUsd, err := model.TradeQuotaPerUsd()
	if err != nil {
		return
	}
	minBase, err := common.QuotaFromDecimalStrict(decimal.NewFromInt(model.TradeSprintMinEquityUsd).Mul(decimal.NewFromInt(int64(perUsd))))
	if err != nil {
		return
	}
	for _, sprint := range ended {
		entries, err := model.ListTradeSprintEntries(sprint.Id)
		if err != nil {
			continue
		}
		if len(entries) == 0 {
			_ = model.SettleTradeSprint(sprint.Id, nil)
			continue
		}
		// 人数不足 2 人：100% 退还报名费，不扣抽成。
		if len(entries) < 2 {
			rows := make([]model.TradeSprintSettlementRow, len(entries))
			for i, e := range entries {
				rows[i] = model.TradeSprintSettlementRow{
					EntryId:   e.Id,
					UserId:    e.UserId,
					EndEquity: e.StartEquity,
					EndNetIn:  e.StartNetIn,
					FinalRank: 1,
					Prize:     e.EntryFee,
					Refund:    true,
				}
			}
			if err := model.SettleTradeSprint(sprint.Id, rows); err != nil {
				common.SysError(fmt.Sprintf("sprint refund %d: %v", sprint.Id, err))
			}
			continue
		}

		userIds := make([]int, len(entries))
		for i, e := range entries {
			userIds[i] = e.UserId
		}
		var accounts []model.TradeAccount
		if err := model.DB.Where("user_id IN ?", userIds).Find(&accounts).Error; err != nil {
			continue
		}
		valuations, err := valueTradeAccounts(accounts)
		if err != nil {
			continue
		}
		byUserAcc := make(map[int]model.TradeAccount, len(accounts))
		byUserVal := make(map[int]TradeValuation, len(accounts))
		for i, acc := range accounts {
			byUserAcc[acc.UserId] = acc
			byUserVal[acc.UserId] = valuations[i]
		}
		type scored struct {
			entry     model.TradeSprintEntry
			endEquity int
			endNetIn  int
			bps       int64
			profit    int
		}
		list := make([]scored, 0, len(entries))
		for _, e := range entries {
			acc := byUserAcc[e.UserId]
			val := byUserVal[e.UserId]
			curNetIn := acc.TotalIn - acc.TotalOut
			ownEquity := max(0, val.Equity-acc.BonusPrincipal)
			bps, profit := model.ComputeSprintReturnBps(e.StartEquity, e.StartNetIn, ownEquity, curNetIn, minBase)
			list = append(list, scored{entry: e, endEquity: ownEquity, endNetIn: curNetIn, bps: bps, profit: profit})
		}
		slices.SortFunc(list, func(a, b scored) int {
			return cmp.Or(cmp.Compare(b.bps, a.bps), cmp.Compare(b.profit, a.profit), cmp.Compare(a.entry.UserId, b.entry.UserId))
		})

		split := model.SprintPrizeSplitBps(len(list))
		fee, _ := common.QuotaFromDecimalStrict(decimal.NewFromInt(int64(sprint.PrizePool)).Mul(decimal.NewFromInt(int64(sprint.FeeBps))).Div(decimal.NewFromInt(10000)).Floor())
		distributable := max(0, sprint.PrizePool-fee)
		rows := make([]model.TradeSprintSettlementRow, len(list))
		for i, item := range list {
			prize := 0
			if i < len(split) && distributable > 0 {
				prize, _ = common.QuotaFromDecimalStrict(decimal.NewFromInt(int64(distributable)).Mul(decimal.NewFromInt(int64(split[i]))).Div(decimal.NewFromInt(10000)).Floor())
			}
			rows[i] = model.TradeSprintSettlementRow{
				EntryId:   item.entry.Id,
				UserId:    item.entry.UserId,
				EndEquity: item.endEquity,
				EndNetIn:  item.endNetIn,
				ReturnBps: item.bps,
				FinalRank: i + 1,
				Prize:     prize,
			}
		}
		if err := model.SettleTradeSprint(sprint.Id, rows); err != nil {
			common.SysError(fmt.Sprintf("sprint settle %d: %v", sprint.Id, err))
		}
	}
}
