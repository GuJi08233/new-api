package controller

import (
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/wendao"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

// 长期游戏(深渊远征、问道宗门)的道具商店：花游戏币买一次性道具放进游戏存档的背包，在游戏里用掉一件少一件。花掉的币直接
// 销毁，道具换不回游戏币。各游戏自己定义道具、原价(单位 0.01 游戏币)与效果，这里只管定价、扣币与放进背包。

// gameShopMaxCount 是一次最多买几件同一种道具。
const gameShopMaxCount = 20

// gameShopItem 是商店里的一种道具在页面上的样子，Price 是乘上价格比例之后的单价。
type gameShopItem struct {
	Id    string          `json:"id"`
	Price model.GameCoins `json:"price"`
}

// gameShopPrice 把原价(0.01 游戏币)乘上管理员设的价格比例，换成游戏币。
func gameShopPrice(setting *operation_setting.GameSetting, cents int) (model.GameCoins, error) {
	coins := decimal.NewFromInt(int64(cents)).Mul(decimal.NewFromInt(int64(setting.ShopPricePercent))).Div(decimal.NewFromInt(10000))
	return model.GameCoinsFromDecimal(coins)
}

// gameShopInfo 是游戏信息里的商店部分：是否开放与每种道具的单价。ids 与 cents 是道具标识与原价，按页面上的顺序。
func gameShopInfo(setting *operation_setting.GameSetting, ids []string, cents []int) (gin.H, error) {
	items := make([]gameShopItem, 0, len(ids))
	for i, id := range ids {
		price, err := gameShopPrice(setting, cents[i])
		if err != nil {
			return nil, err
		}
		items = append(items, gameShopItem{Id: id, Price: price})
	}
	return gin.H{"enabled": setting.ShopEnabled, "items": items}, nil
}

// buyGameShopItem 处理一次购买：请求是 {item, count, request_id}。priceOf 返回道具的原价(不在商店里时 ok 为 false)，add 把
// count 件道具放进问道存档(背包放不下时返回 limitErr)。成功时返回新的存档内容与余额；request_id 是做过的那一次购买时
// 不再扣币、不再给道具，replayed 为真，返回现在的存档与余额(见 wendao.Save.Admit)。返回 false 时已写好错误响应。
func buyGameShopItem(c *gin.Context, game string, saveKey string, priceOf func(id string) (int, bool), add func(data string, id string, count int) (string, error), limitErr error) (string, model.GameCoins, bool, bool) {
	setting := operation_setting.GetGameSetting()
	if !setting.ShopEnabled {
		common.ApiErrorI18n(c, i18n.MsgGameShopDisabled)
		return "", 0, false, false
	}
	var req struct {
		Item      string `json:"item"`
		Count     int    `json:"count"`
		RequestId string `json:"request_id,omitempty"`
	}
	if decodeGameRequest(c, &req) != nil || req.Count < 1 || req.Count > gameShopMaxCount {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return "", 0, false, false
	}
	sum, err := wendao.RequestSum(struct {
		Game  string `json:"game"`
		Item  string `json:"item"`
		Count int    `json:"count"`
	}{game, req.Item, req.Count})
	if err != nil {
		common.ApiError(c, err)
		return "", 0, false, false
	}
	cents, ok := priceOf(req.Item)
	if !ok {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return "", 0, false, false
	}
	price, err := gameShopPrice(setting, cents)
	if err != nil || price < 1 {
		common.ApiErrorI18n(c, i18n.MsgGameCoinAmountInvalid)
		return "", 0, false, false
	}
	// 单价已经限制在合理范围(原价最多几个币、比例最多十倍)，乘上最多 20 件也远小于 2^53，仍按上限核对一次。
	if int64(price) > (common.MaxQuota-1)/int64(req.Count) {
		common.ApiErrorI18n(c, i18n.MsgGameCoinAmountInvalid)
		return "", 0, false, false
	}
	cost := price * model.GameCoins(req.Count)
	remark := fmt.Sprintf("%s:%s×%d", game, req.Item, req.Count)
	now := common.GetTimestamp()
	balance, data, err := model.BuyGameItem(c.GetInt("id"), saveKey, cost, remark, func(data string) (string, error) {
		// 收据与扣币、放道具在同一个事务里：做过的这一次购买在这里返回 ErrReplayed，整个事务回滚，不扣币。
		admitted, err := wendao.UpdateSave(data, func(save *wendao.Save) error { return save.Admit(req.RequestId, sum, now) })
		if err != nil {
			return "", err
		}
		return add(admitted, req.Item, req.Count)
	})
	switch {
	case err == nil:
		return data, balance, false, true
	case errors.Is(err, wendao.ErrReplayed):
		data, err := model.GetGameSave(c.GetInt("id"), saveKey)
		if err != nil {
			common.ApiError(c, err)
			return "", 0, false, false
		}
		account, err := model.GetGameCoinAccount(c.GetInt("id"))
		if err != nil {
			common.ApiError(c, err)
			return "", 0, false, false
		}
		return data, account.Balance, true, true
	case errors.Is(err, wendao.ErrRequestReused):
		common.ApiErrorI18n(c, i18n.MsgGameWendaoRequestReused)
	case errors.Is(err, model.ErrGameCoinInsufficient):
		common.ApiErrorI18n(c, i18n.MsgGameCoinInsufficient)
	case errors.Is(err, limitErr):
		common.ApiErrorI18n(c, i18n.MsgGameShopItemLimit)
	case errors.Is(err, model.ErrGameCoinUserNotFound):
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
	default:
		common.SysError(fmt.Sprintf("failed to buy %s for user %d: %v", remark, c.GetInt("id"), err))
		common.ApiErrorI18n(c, i18n.MsgGamePlayFailed)
	}
	return "", 0, false, false
}
