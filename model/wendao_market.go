package model

import (
	"errors"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

// 问道的万宝阁：掌门之间用灵石买卖宗门的货物。货物与价格的规则在 pkg/wendao(market.go)，这里只管挂单表与两位掌门的存档在
// 同一个事务里改对。加锁顺序固定为挂单、存档(两份存档按玩家编号从小到大)，挂单时只锁卖家存档再插入新行，几种操作同时发生也
// 不会互相等待。万宝阁只流通宗门的东西，不涉及游戏币。

const (
	WendaoListingOpen      = "open"
	WendaoListingSold      = "sold"
	WendaoListingCancelled = "cancelled"
)

var (
	// ErrWendaoListingTooMany 表示同时挂的单已经满了。
	ErrWendaoListingTooMany = errors.New("too many open listings")
	// ErrWendaoListingGone 表示这一单已经卖掉、撤下或过期，或者不是卖给自己的。
	ErrWendaoListingGone = errors.New("listing is no longer available")
	// ErrWendaoListingOwn 表示不能买自己挂的单。
	ErrWendaoListingOwn = errors.New("cannot buy your own listing")
)

// WendaoListing 是万宝阁的一单：SellerId 挂出 Count 份 Goods，总价 Price 灵石。TargetId 不为 0 时只有这位掌门能买(指名交易、
// 赠送)。UnitPrice 是每份千分之一灵石的单价，按它从便宜到贵排列。成交后 BuyerId 是买家、Tax 是抽走的灵石。
type WendaoListing struct {
	Id        int    `json:"id"`
	SellerId  int    `json:"seller_id" gorm:"index"`
	BuyerId   int    `json:"buyer_id" gorm:"index;not null;default:0"`
	TargetId  int    `json:"target_id" gorm:"index;not null;default:0"`
	Goods     string `json:"goods" gorm:"type:varchar(32);index"`
	Count     int    `json:"count" gorm:"not null;default:0"`
	Price     int64  `json:"price" gorm:"type:bigint;not null;default:0"`
	UnitPrice int64  `json:"unit_price" gorm:"type:bigint;index;not null;default:0"`
	Tax       int64  `json:"tax" gorm:"type:bigint;not null;default:0"`
	Status    string `json:"status" gorm:"type:varchar(16);index"`
	CreatedAt int64  `json:"created_at" gorm:"bigint"`
	ExpiresAt int64  `json:"expires_at" gorm:"bigint;index"`
	ClosedAt  int64  `json:"closed_at" gorm:"bigint"`
}

// CreateWendaoListing 挂一单：take 从卖家存档里取出货物(与挂单在同一个事务里)，卖家同时挂着的单(含过期没取回的)不能超过 maxOpen。
func CreateWendaoListing(listing *WendaoListing, maxOpen int, take func(data string) (string, error)) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		if _, err := updateGameSaveTx(tx, listing.SellerId, sectGameKey, take); err != nil {
			return err
		}
		// 卖家存档已经锁住：同一位卖家的几次挂单在这里依次进行，数到的单数是准的。
		var open int64
		if err := tx.Model(&WendaoListing{}).Where("seller_id = ? AND status = ?", listing.SellerId, WendaoListingOpen).
			Count(&open).Error; err != nil {
			return err
		}
		if open >= int64(maxOpen) {
			return ErrWendaoListingTooMany
		}
		listing.Status = WendaoListingOpen
		return tx.Create(listing).Error
	})
}

func lockWendaoListing(tx *gorm.DB, id int) (*WendaoListing, error) {
	var listing WendaoListing
	err := lockForUpdate(tx).Where("id = ?", id).First(&listing).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrWendaoListingGone
	}
	return &listing, err
}

// CancelWendaoListing 撤下自己的一单(过期的也是这样取回)：put 把货物放回卖家存档。
func CancelWendaoListing(id int, sellerId int, put func(data string, listing *WendaoListing) (string, error)) (*WendaoListing, error) {
	var listing *WendaoListing
	err := DB.Transaction(func(tx *gorm.DB) error {
		var err error
		if listing, err = lockWendaoListing(tx, id); err != nil {
			return err
		}
		if listing.SellerId != sellerId || listing.Status != WendaoListingOpen {
			return ErrWendaoListingGone
		}
		if _, err := updateGameSaveTx(tx, sellerId, sectGameKey, func(data string) (string, error) {
			return put(data, listing)
		}); err != nil {
			return err
		}
		listing.Status, listing.ClosedAt = WendaoListingCancelled, common.GetTimestamp()
		return tx.Model(&WendaoListing{}).Where("id = ?", id).
			Updates(map[string]any{"status": listing.Status, "closed_at": listing.ClosedAt}).Error
	})
	return listing, err
}

// BuyWendaoListing 买下一单：trade 拿到卖家与买家的存档，扣买家的灵石、给买家货物、给卖家灵石，返回两份新存档与抽走的灵石。
// 已经卖掉、撤下、过期或者指名给别人的单返回 ErrWendaoListingGone，自己的单返回 ErrWendaoListingOwn。
func BuyWendaoListing(id int, buyerId int, trade func(sellerData string, buyerData string, listing *WendaoListing) (string, string, int64, error)) (*WendaoListing, error) {
	var listing *WendaoListing
	err := DB.Transaction(func(tx *gorm.DB) error {
		var err error
		if listing, err = lockWendaoListing(tx, id); err != nil {
			return err
		}
		now := common.GetTimestamp()
		if listing.Status != WendaoListingOpen || listing.ExpiresAt <= now || (listing.TargetId != 0 && listing.TargetId != buyerId) {
			return ErrWendaoListingGone
		}
		if listing.SellerId == buyerId {
			return ErrWendaoListingOwn
		}
		var tax int64
		err = updateTwoGameSavesTx(tx, listing.SellerId, buyerId, sectGameKey, func(sellerData string, buyerData string) (string, string, error) {
			var seller, buyer string
			var err error
			seller, buyer, tax, err = trade(sellerData, buyerData, listing)
			return seller, buyer, err
		})
		if err != nil {
			return err
		}
		listing.Status, listing.BuyerId, listing.Tax, listing.ClosedAt = WendaoListingSold, buyerId, tax, now
		return tx.Model(&WendaoListing{}).Where("id = ?", id).
			Updates(map[string]any{"status": listing.Status, "buyer_id": buyerId, "tax": tax, "closed_at": now}).Error
	})
	return listing, err
}

// ListWendaoListings 列出能买的单：还在挂、没过期、不是自己挂的。targeted 为真时只列指名给 viewer 的单，否则只列公开的单；
// goods 不为空时只列这种货物。按单价从便宜到贵排列。
func ListWendaoListings(viewer int, goods string, targeted bool, offset int, limit int) ([]WendaoListing, error) {
	query := DB.Where("status = ? AND expires_at > ? AND seller_id <> ?", WendaoListingOpen, common.GetTimestamp(), viewer)
	if targeted {
		query = query.Where("target_id = ?", viewer)
	} else {
		query = query.Where("target_id = 0")
	}
	if goods != "" {
		query = query.Where("goods = ?", goods)
	}
	var rows []WendaoListing
	err := query.Order("unit_price asc").Order("id asc").Offset(offset).Limit(limit).Find(&rows).Error
	return rows, err
}

// ListWendaoOwnListings 列出自己还挂着的单(含已经过期、等着取回的)。
func ListWendaoOwnListings(sellerId int) ([]WendaoListing, error) {
	var rows []WendaoListing
	err := DB.Where("seller_id = ? AND status = ?", sellerId, WendaoListingOpen).Order("id desc").Find(&rows).Error
	return rows, err
}

// ListWendaoTrades 列出自己最近的成交(卖出与买到)。
func ListWendaoTrades(userId int, limit int) ([]WendaoListing, error) {
	var rows []WendaoListing
	err := DB.Where("status = ? AND (seller_id = ? OR buyer_id = ?)", WendaoListingSold, userId, userId).
		Order("closed_at desc").Order("id desc").Limit(limit).Find(&rows).Error
	return rows, err
}
