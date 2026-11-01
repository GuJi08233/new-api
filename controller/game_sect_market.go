package controller

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/wendao"
	"github.com/gin-gonic/gin"
)

// 问道的万宝阁：掌门之间用灵石买卖宗门的货物。挂单时货物从卖家宗门里取出来押着，成交时在同一个事务里扣买家灵石、给买家
// 货物、给卖家灵石(扣掉抽成)；撤单或过期取回时货物放回卖家宗门。规则在 pkg/wendao/market.go，挂单表在 model/wendao_market.go。
// 万宝阁只流通宗门的东西，不发也不收游戏币。

const (
	// wendaoMarketPage 是一页列出几单，wendaoMarketTrades 是成交记录列几条。
	wendaoMarketPage   = 30
	wendaoMarketTrades = 20
)

func registerSectMarketRoutes(group *gin.RouterGroup) {
	group.GET("", getSectMarket)
	group.POST("/list", middleware.GamePlayRateLimit("sect-market"), listSectMarket)
	group.POST("/cancel", middleware.GamePlayRateLimit("sect-market"), cancelSectMarket)
	group.POST("/buy", middleware.GamePlayRateLimit("sect-market"), buySectMarket)
}

func respondMarketError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, model.ErrWendaoListingTooMany):
		common.ApiErrorI18n(c, i18n.MsgGameMarketTooMany)
	case errors.Is(err, model.ErrWendaoListingGone):
		common.ApiErrorI18n(c, i18n.MsgGameMarketGone)
	case errors.Is(err, model.ErrWendaoListingOwn):
		common.ApiErrorI18n(c, i18n.MsgGameMarketOwn)
	case errors.Is(err, wendao.ErrMarketHall):
		common.ApiErrorI18n(c, i18n.MsgGameMarketHall)
	default:
		respondSectError(c, err)
	}
}

// sectMarketView 是万宝阁页：公开的单(goods 不为空时只列这种货物，page 从 0 起)、指名给自己的单、自己挂着的单与最近的成交，
// 附上这些单里出现的掌门名字。
func sectMarketView(userId int, goods string, page int) (gin.H, error) {
	listings, err := model.ListWendaoListings(userId, goods, false, page*wendaoMarketPage, wendaoMarketPage+1)
	if err != nil {
		return nil, err
	}
	more := len(listings) > wendaoMarketPage
	if more {
		listings = listings[:wendaoMarketPage]
	}
	offers, err := model.ListWendaoListings(userId, "", true, 0, wendaoMarketPage)
	if err != nil {
		return nil, err
	}
	mine, err := model.ListWendaoOwnListings(userId)
	if err != nil {
		return nil, err
	}
	trades, err := model.ListWendaoTrades(userId, wendaoMarketTrades)
	if err != nil {
		return nil, err
	}
	var ids []int
	for _, group := range [][]model.WendaoListing{listings, offers, mine, trades} {
		for _, row := range group {
			ids = append(ids, row.SellerId, row.BuyerId, row.TargetId)
		}
	}
	names, err := model.GetGameDisplayNames(ids)
	if err != nil {
		return nil, err
	}
	rules := gin.H{
		"min_hall":     wendao.MarketMinHall,
		"max_listings": wendao.MarketMaxListings,
		"tax_pct":      wendao.MarketTaxPct,
		"ttl":          wendao.MarketListingSeconds,
		"max_price":    wendao.MarketMaxPrice,
		"goods":        wendao.MarketGoods,
	}
	return gin.H{"rules": rules, "self": userId, "now": common.GetTimestamp(), "page": page, "more": more,
		"listings": listings, "offers": offers, "mine": mine, "trades": trades, "names": names}, nil
}

func getSectMarket(c *gin.Context) {
	if !requireGameEnabled(c, wendaoGame) {
		return
	}
	var query struct {
		Goods string `form:"goods"`
		Page  int    `form:"page"`
	}
	if err := c.ShouldBindQuery(&query); err != nil || query.Page < 0 || query.Page > 1000 {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if _, ok := wendao.MarketGoodOf(query.Goods); query.Goods != "" && !ok {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	view, err := sectMarketView(c.GetInt("id"), query.Goods, query.Page)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, view)
}

// respondSectMarket 在一次万宝阁操作之后回应第一页的万宝阁与自己最新的宗门。
func respondSectMarket(c *gin.Context) {
	userId := c.GetInt("id")
	view, err := sectMarketView(userId, "", 0)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	data, err := model.GetGameSave(userId, wendao.SaveKey)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if s, err := wendao.ParseSect(data); err == nil && s != nil {
		view["sect"] = s.View(common.GetTimestamp())
	}
	common.ApiSuccess(c, view)
}

// tradingSect 读出存档里的宗门，先把产出与主角修为结算到 now(卖的、付的都是现在库房里的)，主殿不够时不能买卖。
func tradingSect(save *wendao.Save, now int64) (*wendao.Sect, error) {
	if save.Sect == nil {
		return nil, wendao.ErrUnavailable
	}
	save.TickHero(now)
	save.Sect.Tick(now)
	return save.Sect, save.Sect.CanTrade()
}

// listSectMarket 挂一单：货物从宗门里取出来押在万宝阁。target 不为 0 时只卖给这位掌门，可以标 0 灵石赠送。
func listSectMarket(c *gin.Context) {
	if !requireGameEnabled(c, wendaoGame) {
		return
	}
	var req struct {
		Goods  string `json:"goods"`
		Count  int    `json:"count"`
		Price  int64  `json:"price"`
		Target int    `json:"target"`
	}
	userId := c.GetInt("id")
	if decodeGameRequest(c, &req) != nil || req.Target < 0 || req.Target == userId ||
		!wendao.ValidListing(req.Goods, req.Count, req.Price, req.Target > 0) {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if req.Target > 0 {
		names, err := model.GetGameDisplayNames([]int{req.Target})
		if err != nil {
			common.ApiError(c, err)
			return
		}
		if _, ok := names[req.Target]; !ok {
			common.ApiErrorI18n(c, i18n.MsgInvalidParams)
			return
		}
	}
	now := common.GetTimestamp()
	listing := &model.WendaoListing{SellerId: userId, TargetId: req.Target, Goods: req.Goods, Count: req.Count, Price: req.Price,
		UnitPrice: req.Price * 1000 / int64(req.Count), CreatedAt: now, ExpiresAt: now + wendao.MarketListingSeconds}
	err := model.CreateWendaoListing(listing, wendao.MarketMaxListings, func(data string) (string, error) {
		return wendao.UpdateSave(data, func(save *wendao.Save) error {
			s, err := tradingSect(save, now)
			if err != nil {
				return err
			}
			return s.TakeGoods(req.Goods, req.Count)
		})
	})
	if err != nil {
		respondMarketError(c, err)
		return
	}
	respondSectMarket(c)
}

// cancelSectMarket 撤下自己的一单，过期的单也这样取回；货物放回宗门。
func cancelSectMarket(c *gin.Context) {
	if !requireGameEnabled(c, wendaoGame) {
		return
	}
	var req struct {
		Id int `json:"id"`
	}
	if decodeGameRequest(c, &req) != nil || req.Id <= 0 {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	_, err := model.CancelWendaoListing(req.Id, c.GetInt("id"), func(data string, listing *model.WendaoListing) (string, error) {
		return wendao.UpdateSave(data, func(save *wendao.Save) error {
			if save.Sect == nil {
				return wendao.ErrUnavailable
			}
			return save.Sect.PutGoods(listing.Goods, listing.Count)
		})
	})
	if err != nil {
		respondMarketError(c, err)
		return
	}
	respondSectMarket(c)
}

// buySectMarket 买下一单：买家付灵石拿货，卖家收灵石(扣掉抽成)，两边的大事记都记一笔。
func buySectMarket(c *gin.Context) {
	if !requireGameEnabled(c, wendaoGame) {
		return
	}
	var req struct {
		Id int `json:"id"`
	}
	if decodeGameRequest(c, &req) != nil || req.Id <= 0 {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	now := common.GetTimestamp()
	_, err := model.BuyWendaoListing(req.Id, c.GetInt("id"), func(sellerData string, buyerData string, listing *model.WendaoListing) (string, string, int64, error) {
		sellerSave, err := wendao.ParseSave(sellerData)
		if err != nil {
			return "", "", 0, err
		}
		buyerSave, err := wendao.ParseSave(buyerData)
		if err != nil {
			return "", "", 0, err
		}
		buyer, err := tradingSect(buyerSave, now)
		if err != nil {
			return "", "", 0, err
		}
		if sellerSave.Sect == nil {
			return "", "", 0, wendao.ErrUnavailable
		}
		tax, err := wendao.SettleTrade(sellerSave.Sect, buyer, listing.Goods, listing.Count, listing.Price, now)
		if err != nil {
			return "", "", 0, err
		}
		sellerEncoded, err := sellerSave.Encode()
		if err != nil {
			return "", "", 0, err
		}
		buyerEncoded, err := buyerSave.Encode()
		return sellerEncoded, buyerEncoded, tax, err
	})
	if err != nil {
		respondMarketError(c, err)
		return
	}
	respondSectMarket(c)
}
