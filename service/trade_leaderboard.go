package service

import (
	"cmp"
	"slices"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

// 排行榜的排序方式：累计盈亏(默认)、收益率、总资产。
const (
	TradeLeaderboardByProfit = "profit"
	TradeLeaderboardByReturn = "return"
	TradeLeaderboardByEquity = "equity"
)

// TradeLeaderboardTTL 是排行榜的缓存时间：给所有成交过的账户估值要读出全部持仓，过期以后有人来看才重新算。
const TradeLeaderboardTTL = 5 * time.Minute

// TradeLeaderboardEntry 是排行榜上的一个人，金额都是额度单位。Profit 是累计盈亏：总资产减去净转入(累计转入减累计转出)；
// ReturnRate 是累计盈亏除以累计转入，资金进出过几次的人按转入的总数算。
type TradeLeaderboardEntry struct {
	UserId     int
	Name       string
	Equity     int
	Profit     int
	ReturnRate float64
}

// TradeLeaderboard 是某一刻所有成交过的人(买入过现货或开过合约)的排名，三种排序各排一份。
type TradeLeaderboard struct {
	UpdatedAt int64
	ranked    map[string][]TradeLeaderboardEntry
}

// Ranked 按 sort 排好的全部名单，第 i 个是第 i+1 名；认不出的 sort 按累计盈亏。
func (b *TradeLeaderboard) Ranked(sort string) []TradeLeaderboardEntry {
	if entries, ok := b.ranked[sort]; ok {
		return entries
	}
	return b.ranked[TradeLeaderboardByProfit]
}

var tradeLeaderboardCache struct {
	sync.Mutex
	board *TradeLeaderboard
}

// GetTradeLeaderboard 返回排行榜，超过 TradeLeaderboardTTL 才重新算；同时来的请求等同一次计算。
func GetTradeLeaderboard() (*TradeLeaderboard, error) {
	tradeLeaderboardCache.Lock()
	defer tradeLeaderboardCache.Unlock()
	board := tradeLeaderboardCache.board
	if board != nil && common.GetTimestamp()-board.UpdatedAt < int64(TradeLeaderboardTTL/time.Second) {
		return board, nil
	}
	board, err := buildTradeLeaderboard()
	if err != nil {
		return nil, err
	}
	tradeLeaderboardCache.board = board
	return board, nil
}

// buildTradeLeaderboard 按当前行情给成交过的人估值并排名。名次相同的比较依次看累计盈亏、总资产，最后按用户 id，排名稳定。
// 已删除的用户不上榜。
func buildTradeLeaderboard() (*TradeLeaderboard, error) {
	var entries []TradeLeaderboardEntry
	after := 0
	for {
		accounts, err := model.ListTradeAccounts(after, tradeSnapshotBatch)
		if err != nil {
			return nil, err
		}
		if len(accounts) == 0 {
			break
		}
		after = accounts[len(accounts)-1].UserId
		userIds := make([]int, len(accounts))
		for i, account := range accounts {
			userIds[i] = account.UserId
		}
		traders, err := model.ListTradeTraders(userIds)
		if err != nil {
			return nil, err
		}
		names, err := model.ListTradeUserNames(traders)
		if err != nil {
			return nil, err
		}
		accounts = slices.DeleteFunc(accounts, func(account model.TradeAccount) bool {
			_, ok := names[account.UserId]
			return !ok
		})
		valuations, err := valueTradeAccounts(accounts)
		if err != nil {
			return nil, err
		}
		for i, account := range accounts {
			entry := TradeLeaderboardEntry{
				UserId: account.UserId,
				Name:   names[account.UserId],
				Equity: valuations[i].Equity,
				Profit: valuations[i].Equity - (account.TotalIn - account.TotalOut),
			}
			if account.TotalIn > 0 {
				entry.ReturnRate = float64(entry.Profit) / float64(account.TotalIn)
			}
			entries = append(entries, entry)
		}
	}
	tieBreak := func(a, b TradeLeaderboardEntry) int {
		return cmp.Or(cmp.Compare(b.Profit, a.Profit), cmp.Compare(b.Equity, a.Equity), cmp.Compare(a.UserId, b.UserId))
	}
	board := &TradeLeaderboard{UpdatedAt: common.GetTimestamp(), ranked: map[string][]TradeLeaderboardEntry{}}
	orders := map[string]func(a, b TradeLeaderboardEntry) int{
		TradeLeaderboardByProfit: tieBreak,
		TradeLeaderboardByReturn: func(a, b TradeLeaderboardEntry) int {
			return cmp.Or(cmp.Compare(b.ReturnRate, a.ReturnRate), tieBreak(a, b))
		},
		TradeLeaderboardByEquity: func(a, b TradeLeaderboardEntry) int {
			return cmp.Or(cmp.Compare(b.Equity, a.Equity), tieBreak(a, b))
		},
	}
	for sort, order := range orders {
		board.ranked[sort] = slices.SortedStableFunc(slices.Values(entries), order)
	}
	return board, nil
}
