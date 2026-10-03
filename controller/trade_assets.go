package controller

import (
	"context"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
)

// GetTradeAssetHistory 返回最近 days 天(7 到 90，默认 30)每天结束时的总资产与累计净转入，最后加上此刻的一点。
func GetTradeAssetHistory(c *gin.Context) {
	days, _ := strconv.Atoi(c.DefaultQuery("days", "30"))
	days = min(max(days, 7), 90)
	userId := c.GetInt("id")
	now := time.Now()
	snapshots, err := model.GetTradeSnapshots(userId, tradeDay(now.AddDate(0, 0, -days)), tradeDay(now))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	account, valuation, err := service.ValueTradeUser(userId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	points := make([]gin.H, 0, len(snapshots)+1)
	for _, snapshot := range snapshots {
		points = append(points, gin.H{"day": snapshot.Day, "equity": snapshot.Equity, "net_in": snapshot.NetIn})
	}
	points = append(points, gin.H{"day": tradeDay(now), "equity": valuation.Equity, "net_in": account.TotalIn - account.TotalOut, "live": true})
	common.ApiSuccess(c, points)
}

// GetTradeAssetDaily 返回 month(YYYY-MM)里每天的盈亏，以及加密货币、美股代币现货与合约各自的部分。某一天的盈亏是总资产的
// 变化减去当天的净转入；分类盈亏是这一类的市值变化加上当天的资金流。本月包含今天时，今天按此刻的估值算。
func GetTradeAssetDaily(c *gin.Context) {
	start, err := time.ParseInLocation("2006-01", c.Query("month"), time.Local)
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	end := start.AddDate(0, 1, 0)
	userId := c.GetInt("id")
	snapshots, err := model.GetTradeSnapshots(userId, tradeDay(start), tradeDay(end.AddDate(0, 0, -1)))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	previous, err := model.GetLatestTradeSnapshotBefore(userId, tradeDay(start))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if previous == nil {
		previous = &model.TradeSnapshot{}
	}
	now := time.Now()
	today := tradeDay(now)
	if !now.Before(start) && now.Before(end) && (len(snapshots) == 0 || snapshots[len(snapshots)-1].Day < today) {
		dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
		live, err := service.LiveTradeSnapshot(userId, today, dayStart.Unix(), now.Unix())
		if err != nil {
			common.ApiError(c, err)
			return
		}
		snapshots = append(snapshots, live)
	}
	days := make([]gin.H, 0, len(snapshots))
	for i := range snapshots {
		snapshot := &snapshots[i]
		days = append(days, gin.H{
			"day":         snapshot.Day,
			"equity":      snapshot.Equity,
			"pnl":         snapshot.Equity - snapshot.NetIn - (previous.Equity - previous.NetIn),
			"crypto_pnl":  snapshot.CryptoValue - previous.CryptoValue + snapshot.CryptoFlow,
			"stock_pnl":   snapshot.StockValue - previous.StockValue + snapshot.StockFlow,
			"futures_pnl": snapshot.FuturesValue - previous.FuturesValue + snapshot.FuturesFlow,
			"live":        snapshot.Day == today,
		})
		previous = snapshot
	}
	common.ApiSuccess(c, days)
}

// GetTradeAdminStatus 给管理员看模拟盘的行情连接、全站汇总与配置(含默认值和各项的取值范围)。配置通过通用的配置接口保存。
func GetTradeAdminStatus(c *gin.Context) {
	stats, err := model.GetTradeStats()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	perUsd, err := model.TradeQuotaPerUsd()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{
		"market":         service.GetTradeMarket().Status(),
		"futures_market": service.GetFuturesMarket().Status(),
		"stats":          stats,
		"quota_per_unit": perUsd,
		"setting":        operation_setting.GetTradeSetting(),
		"defaults":       operation_setting.DefaultTradeSetting(),
		"symbols":        operation_setting.TradeSymbols,
		"limits": gin.H{
			"max_fee_bps":      operation_setting.TradeMaxFeeBps,
			"max_order_usd":    operation_setting.TradeMaxOrderUsd,
			"max_position_usd": operation_setting.TradeMaxPositionUsd,
			"max_profit_out":   operation_setting.TradeMaxProfitOut,
			"min_stale_ms":     operation_setting.TradeMinStaleMs,
			"max_stale_ms":     operation_setting.TradeMaxStaleMs,
			"max_leverage":     operation_setting.TradeMaxLeverage,
		},
	})
}

// tradeSnapshotHandler 每天给模拟盘账户拍一次资产快照：0 点过后第一次运行时拍前一天的，拍完记下那一天，当天不再运行。
// 行情没连上时不拍(合约有连接时也要连上)，持仓没有价格只能按成本估值，那天的盈亏会失真；等连上再拍。
type tradeSnapshotHandler struct{}

func (tradeSnapshotHandler) Type() string { return model.SystemTaskTypeTradeSnapshot }

func (tradeSnapshotHandler) Enabled() bool {
	if !operation_setting.GetTradeSetting().Enabled || !service.GetTradeMarket().Status().Connected {
		return false
	}
	if futures := service.GetFuturesMarket().Status(); futures.Running && (!futures.Connected || !futures.DataConnected) {
		return false
	}
	done, err := model.HasTradeSnapshotDay(tradeDay(time.Now().AddDate(0, 0, -1)))
	return err == nil && !done
}

func (tradeSnapshotHandler) Interval() time.Duration { return 10 * time.Minute }

func (tradeSnapshotHandler) NewPayload() any { return nil }

func (tradeSnapshotHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	now := time.Now()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	dayStart := todayStart.AddDate(0, 0, -1)
	day := tradeDay(dayStart)
	accounts, err := service.TakeTradeSnapshots(ctx, day, dayStart.Unix(), todayStart.Unix())
	result := map[string]any{"day": day, "accounts": accounts}
	if err != nil {
		finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusFailed, result, err)
		return
	}
	finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusSucceeded, result, nil)
}
