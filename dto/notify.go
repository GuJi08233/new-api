package dto

type Notify struct {
	Type    string        `json:"type"`
	Title   string        `json:"title"`
	Content string        `json:"content"`
	Values  []interface{} `json:"values"`
}

const ContentValueParam = "{{value}}"

const (
	NotifyTypeQuotaExceed      = "quota_exceed"
	NotifyTypeChannelUpdate    = "channel_update"
	NotifyTypeChannelTest      = "channel_test"
	NotifyTypeTradeDeficit     = "trade_deficit"
	NotifyTypeTradeTakeProfit  = "trade_take_profit"
	NotifyTypeTradeStopLoss    = "trade_stop_loss"
	NotifyTypeTradeLiquidation = "trade_liquidation"
	NotifyTypeTradeFill        = "trade_fill"
)

func NewNotify(t string, title string, content string, values []interface{}) Notify {
	return Notify{
		Type:    t,
		Title:   title,
		Content: content,
		Values:  values,
	}
}
