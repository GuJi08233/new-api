package service

import (
	"context"
	_ "embed"
	"fmt"
	"maps"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/binance"
	"github.com/QuantumNous/new-api/pkg/tradesim"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/shopspring/decimal"
)

// 合约的风险限额档位(每档的最高杠杆、维持保证金率与速算数)来自 Binance 网页用的公开接口，它不在 fapi 上，只在 www.binance.com。
// trade_brackets.json 是 2026-10-04 取的快照：启动时先用它，之后每 12 小时从 Binance 刷新一次，刷新失败时继续用手上的。
const (
	tradeBracketsBaseURL = "https://www.binance.com"
	tradeBracketsRefresh = 12 * time.Hour
	tradeBracketsRetry   = 10 * time.Minute
)

//go:embed trade_brackets.json
var tradeBracketsSnapshot []byte

var tradeBrackets atomic.Pointer[map[string]tradesim.Brackets]

func init() {
	var raw map[string][]struct {
		Floor       string `json:"floor"`
		Cap         string `json:"cap"`
		MaxLeverage int    `json:"max_leverage"`
		Mmr         string `json:"mmr"`
		Maint       string `json:"maint"`
	}
	if err := common.Unmarshal(tradeBracketsSnapshot, &raw); err != nil {
		panic("trade brackets snapshot: " + err.Error())
	}
	tables := make(map[string]tradesim.Brackets, len(raw))
	for symbol, rows := range raw {
		brackets := make(tradesim.Brackets, len(rows))
		for i, row := range rows {
			brackets[i] = tradesim.Bracket{
				Floor:       decimal.RequireFromString(row.Floor),
				Cap:         decimal.RequireFromString(row.Cap),
				MaxLeverage: row.MaxLeverage,
				Mmr:         decimal.RequireFromString(row.Mmr),
				MaintAmount: decimal.RequireFromString(row.Maint),
			}
		}
		tables[symbol] = brackets
	}
	tradeBrackets.Store(&tables)
}

// TradeFuturesBrackets 返回合约的风险限额档位，从低到高。支持的合约都有快照，不会是空的。
func TradeFuturesBrackets(symbol string) tradesim.Brackets {
	return (*tradeBrackets.Load())[symbol]
}

// refreshBrackets 从 Binance 拉一次支持的合约的档位，拉到的替换手上的，没拉到的合约保持不变。
func (m *TradeMarket) refreshBrackets() {
	m.mu.RLock()
	proxyURL := m.config.ProxyURL
	m.mu.RUnlock()
	httpClient, _, err := tradeMarketTransport(proxyURL)
	if err != nil {
		m.recordError(err)
		return
	}
	symbols := make([]string, 0, len(operation_setting.TradeSymbols))
	for _, info := range operation_setting.TradeSymbols {
		if info.Futures != "" {
			symbols = append(symbols, info.Futures)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), tradeRestTimeout)
	defer cancel()
	fetched, err := binance.NewFuturesClient(tradeBracketsBaseURL, httpClient).RiskBrackets(ctx, symbols)
	if err != nil {
		m.recordError(fmt.Errorf("binance risk brackets: %w", err))
		return
	}
	tables := maps.Clone(*tradeBrackets.Load())
	for symbol, rows := range fetched {
		brackets := make(tradesim.Brackets, len(rows))
		for i, row := range rows {
			brackets[i] = tradesim.Bracket{Floor: row.Floor, Cap: row.Cap, MaxLeverage: row.MaxLeverage, Mmr: row.Mmr, MaintAmount: row.MaintAmount}
		}
		tables[symbol] = brackets
	}
	tradeBrackets.Store(&tables)
	m.mu.Lock()
	m.bracketsNext = time.Now().Add(tradeBracketsRefresh)
	m.mu.Unlock()
}
