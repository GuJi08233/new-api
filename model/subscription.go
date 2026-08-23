package model

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/cachex"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/samber/hot"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// Subscription duration units
const (
	SubscriptionDurationYear   = "year"
	SubscriptionDurationMonth  = "month"
	SubscriptionDurationDay    = "day"
	SubscriptionDurationHour   = "hour"
	SubscriptionDurationCustom = "custom"
)

// Subscription quota reset period
const (
	SubscriptionResetNever   = "never"
	SubscriptionResetDaily   = "daily"
	SubscriptionResetWeekly  = "weekly"
	SubscriptionResetMonthly = "monthly"
	SubscriptionResetCustom  = "custom"
	SubscriptionResetActive  = "active"
)

// Subscription lifecycle status
const (
	SubscriptionStatusActive    = "active"
	SubscriptionStatusInactive  = "inactive"
	SubscriptionStatusExpired   = "expired"
	SubscriptionStatusCancelled = "cancelled"
)

// Subscription group modes: upgrade replaces the user's group on activation
// (legacy behavior, stored as ''), attach grants the bound group as an extra
// usable group for the holder without touching the user's own group.
const (
	SubscriptionGroupModeUpgrade = "upgrade"
	SubscriptionGroupModeAttach  = "attach"
)

// Quota tier period types
const (
	TierPeriodMonthly = "monthly" // Calendar-aligned: 1st of month
	TierPeriodWeekly  = "weekly"  // Calendar-aligned: Monday
	TierPeriodDaily   = "daily"   // Calendar-aligned: midnight
	TierPeriodHourly  = "hourly"  // Sliding window: N hours from first use
	TierPeriodCustom  = "custom"  // Sliding window: custom seconds from first use
	TierPeriodNone    = "none"    // Never resets, total cap only
)

// QuotaTier defines a single tier in a multi-tier quota configuration
type QuotaTier struct {
	Period        string `json:"period"`         // monthly|weekly|daily|hourly|custom|none
	Limit         int64  `json:"limit"`          // Max usage in this period (0 = no limit)
	CustomSeconds int64  `json:"custom_seconds"` // Only for period="custom"
	SortPriority  int    `json:"sort_priority"`  // Lower = shorter period, checked first
}

// TierUsageSnapshot stores the per-tier usage delta for a single pre-consume operation
type TierUsageSnapshot struct {
	TierIndex int   `json:"tier_index"`
	Delta     int64 `json:"delta"`
}

var (
	ErrSubscriptionOrderNotFound      = errors.New("subscription order not found")
	ErrSubscriptionOrderStatusInvalid = errors.New("subscription order status invalid")
	ErrSubscriptionOrderExpired       = errors.New("subscription order expired")
)

const (
	subscriptionPlanCacheNamespace            = "new-api:subscription_plan:v1"
	subscriptionPlanInfoCacheNamespace        = "new-api:subscription_plan_info:v1"
	subscriptionOrderQueryWindowSeconds int64 = 30 * 24 * 60 * 60
)

var (
	subscriptionPlanCacheOnce     sync.Once
	subscriptionPlanInfoCacheOnce sync.Once

	subscriptionPlanCache     *cachex.HybridCache[SubscriptionPlan]
	subscriptionPlanInfoCache *cachex.HybridCache[SubscriptionPlanInfo]
)

func subscriptionPlanCacheTTL() time.Duration {
	ttlSeconds := common.GetEnvOrDefault("SUBSCRIPTION_PLAN_CACHE_TTL", 300)
	if ttlSeconds <= 0 {
		ttlSeconds = 300
	}
	return time.Duration(ttlSeconds) * time.Second
}

func subscriptionPlanInfoCacheTTL() time.Duration {
	ttlSeconds := common.GetEnvOrDefault("SUBSCRIPTION_PLAN_INFO_CACHE_TTL", 120)
	if ttlSeconds <= 0 {
		ttlSeconds = 120
	}
	return time.Duration(ttlSeconds) * time.Second
}

func subscriptionPlanCacheCapacity() int {
	capacity := common.GetEnvOrDefault("SUBSCRIPTION_PLAN_CACHE_CAP", 5000)
	if capacity <= 0 {
		capacity = 5000
	}
	return capacity
}

func subscriptionPlanInfoCacheCapacity() int {
	capacity := common.GetEnvOrDefault("SUBSCRIPTION_PLAN_INFO_CACHE_CAP", 10000)
	if capacity <= 0 {
		capacity = 10000
	}
	return capacity
}

func subscriptionPendingHoldSeconds() int64 {
	seconds := int64(common.GetEnvOrDefault("SUBSCRIPTION_PENDING_HOLD_SECONDS", 300))
	if seconds <= 0 {
		seconds = 300
	}
	return seconds
}

func pendingSubscriptionOrderCutoff(now int64) int64 {
	return now - subscriptionPendingHoldSeconds()
}

func subscriptionOrderExpiresAt(createTime int64) int64 {
	if createTime <= 0 {
		return 0
	}
	return createTime + subscriptionPendingHoldSeconds()
}

func isSubscriptionOrderExpiredByTime(order *SubscriptionOrder, now int64) bool {
	if order == nil || order.Status != common.TopUpStatusPending {
		return false
	}
	return order.CreateTime > 0 && order.CreateTime <= pendingSubscriptionOrderCutoff(now)
}

func getSubscriptionPlanCache() *cachex.HybridCache[SubscriptionPlan] {
	subscriptionPlanCacheOnce.Do(func() {
		ttl := subscriptionPlanCacheTTL()
		subscriptionPlanCache = cachex.NewHybridCache[SubscriptionPlan](cachex.HybridCacheConfig[SubscriptionPlan]{
			Namespace: cachex.Namespace(subscriptionPlanCacheNamespace),
			Redis:     common.RDB,
			RedisEnabled: func() bool {
				return common.RedisEnabled && common.RDB != nil
			},
			RedisCodec: cachex.JSONCodec[SubscriptionPlan]{},
			Memory: func() *hot.HotCache[string, SubscriptionPlan] {
				return hot.NewHotCache[string, SubscriptionPlan](hot.LRU, subscriptionPlanCacheCapacity()).
					WithTTL(ttl).
					WithJanitor().
					Build()
			},
		})
	})
	return subscriptionPlanCache
}

func getSubscriptionPlanInfoCache() *cachex.HybridCache[SubscriptionPlanInfo] {
	subscriptionPlanInfoCacheOnce.Do(func() {
		ttl := subscriptionPlanInfoCacheTTL()
		subscriptionPlanInfoCache = cachex.NewHybridCache[SubscriptionPlanInfo](cachex.HybridCacheConfig[SubscriptionPlanInfo]{
			Namespace: cachex.Namespace(subscriptionPlanInfoCacheNamespace),
			Redis:     common.RDB,
			RedisEnabled: func() bool {
				return common.RedisEnabled && common.RDB != nil
			},
			RedisCodec: cachex.JSONCodec[SubscriptionPlanInfo]{},
			Memory: func() *hot.HotCache[string, SubscriptionPlanInfo] {
				return hot.NewHotCache[string, SubscriptionPlanInfo](hot.LRU, subscriptionPlanInfoCacheCapacity()).
					WithTTL(ttl).
					WithJanitor().
					Build()
			},
		})
	})
	return subscriptionPlanInfoCache
}

func subscriptionPlanCacheKey(id int) string {
	if id <= 0 {
		return ""
	}
	return strconv.Itoa(id)
}

func InvalidateSubscriptionPlanCache(planId int) {
	if planId <= 0 {
		return
	}
	cache := getSubscriptionPlanCache()
	_, _ = cache.DeleteMany([]string{subscriptionPlanCacheKey(planId)})
	infoCache := getSubscriptionPlanInfoCache()
	_ = infoCache.Purge()
}

// Subscription plan
type SubscriptionPlan struct {
	Id int `json:"id"`

	Title    string `json:"title" gorm:"type:varchar(128);not null"`
	Subtitle string `json:"subtitle" gorm:"type:varchar(255);default:''"`

	// Display money amount (follow existing code style: float64 for money)
	PriceAmount float64 `json:"price_amount" gorm:"type:decimal(10,6);not null;default:0"`
	Currency    string  `json:"currency" gorm:"type:varchar(8);not null;default:'USD'"`

	DurationUnit  string `json:"duration_unit" gorm:"type:varchar(16);not null;default:'month'"`
	DurationValue int    `json:"duration_value" gorm:"type:int;not null;default:1"`
	CustomSeconds int64  `json:"custom_seconds" gorm:"type:bigint;not null;default:0"`

	Enabled   bool `json:"enabled" gorm:"default:true"`
	SortOrder int  `json:"sort_order" gorm:"type:int;default:0"`

	AllowBalancePay *bool `json:"allow_balance_pay"`

	// Allow falling back to wallet balance after subscription quota is exhausted (empty = true)
	AllowWalletOverflow *bool `json:"allow_wallet_overflow"`

	StripePriceId         string `json:"stripe_price_id" gorm:"type:varchar(128);default:''"`
	CreemProductId        string `json:"creem_product_id" gorm:"type:varchar(128);default:''"`
	WaffoPancakeProductId string `json:"waffo_pancake_product_id" gorm:"type:varchar(128);default:''"`

	// Max purchases per user (0 = unlimited)
	MaxPurchasePerUser int `json:"max_purchase_per_user" gorm:"type:int;default:0"`

	// Max purchases for all users combined (0 = unlimited)
	MaxPurchaseTotal int `json:"max_purchase_total" gorm:"type:int;default:0"`

	// Reset period for global purchase limit (only effective when MaxPurchaseTotal > 0)
	MaxPurchaseResetPeriod        string `json:"max_purchase_reset_period" gorm:"type:varchar(16);default:'never'"`
	MaxPurchaseResetCustomSeconds int64  `json:"max_purchase_reset_custom_seconds" gorm:"type:bigint;default:0"`

	// Upgrade user group after purchase (empty = no change)
	UpgradeGroup string `json:"upgrade_group" gorm:"type:varchar(64);default:''"`

	// Downgrade user group on expiry (empty = revert to the group held before purchase)
	DowngradeGroup string `json:"downgrade_group" gorm:"type:varchar(64);default:''"`

	// Group mode: ''/'upgrade' replaces the user's group on activation;
	// 'attach' grants UpgradeGroup as an extra usable group instead.
	GroupMode string `json:"group_mode" gorm:"type:varchar(16);default:''"`

	// Total quota (amount in quota units, 0 = unlimited)
	TotalAmount int64 `json:"total_amount" gorm:"type:bigint;not null;default:0"`

	// Quota reset period for plan
	QuotaResetPeriod        string `json:"quota_reset_period" gorm:"type:varchar(16);default:'never'"`
	QuotaResetCustomSeconds int64  `json:"quota_reset_custom_seconds" gorm:"type:bigint;default:0"`

	// Multi-tier quota configuration (JSON array). When non-empty, overrides single-tier TotalAmount/QuotaResetPeriod.
	QuotaTiers string `json:"quota_tiers" gorm:"type:text"`

	// When true, users on this plan cannot use wallet balance for deduction
	DisableBalanceDeduction bool `json:"disable_balance_deduction"`

	CreatedAt int64 `json:"created_at" gorm:"bigint"`
	UpdatedAt int64 `json:"updated_at" gorm:"bigint"`

	// Derived fields for UI display
	PurchaseCount int64 `json:"purchase_count,omitempty" gorm:"-"`
}

func (p *SubscriptionPlan) BeforeCreate(tx *gorm.DB) error {
	now := common.GetTimestamp()
	p.CreatedAt = now
	p.UpdatedAt = now
	return nil
}

func (p *SubscriptionPlan) BeforeUpdate(tx *gorm.DB) error {
	p.UpdatedAt = common.GetTimestamp()
	return nil
}

func (p *SubscriptionPlan) NormalizeDefaults() {
	if p.AllowBalancePay == nil {
		p.AllowBalancePay = common.GetPointer(true)
	}
	if p.AllowWalletOverflow == nil {
		p.AllowWalletOverflow = common.GetPointer(true)
	}
}

func (p *SubscriptionPlan) IsAttachGroupMode() bool {
	return strings.TrimSpace(p.GroupMode) == SubscriptionGroupModeAttach
}

// Subscription order (payment -> webhook -> create UserSubscription)
type SubscriptionOrder struct {
	Id     int     `json:"id"`
	UserId int     `json:"user_id" gorm:"index"`
	PlanId int     `json:"plan_id" gorm:"index"`
	Money  float64 `json:"money"`

	TradeNo         string `json:"trade_no" gorm:"unique;type:varchar(255);index"`
	PaymentMethod   string `json:"payment_method" gorm:"type:varchar(50)"`
	PaymentProvider string `json:"payment_provider" gorm:"type:varchar(50);default:''"`
	Status          string `json:"status"`
	CreateTime      int64  `json:"create_time"`
	CompleteTime    int64  `json:"complete_time"`

	ExpectedPaymentToken  string `json:"expected_payment_token" gorm:"type:varchar(64);default:''"`
	ExpectedPaymentAmount string `json:"expected_payment_amount" gorm:"type:varchar(128);default:''"`
	ProviderPayload       string `json:"provider_payload" gorm:"type:text"`
}

type SubscriptionOrderResumePayload struct {
	Type    string            `json:"type"`
	URL     string            `json:"url,omitempty"`
	Params  map[string]string `json:"params,omitempty"`
	Message string            `json:"message,omitempty"`
}

func (o *SubscriptionOrder) Insert() error {
	if o.CreateTime == 0 {
		o.CreateTime = common.GetTimestamp()
	}
	return DB.Create(o).Error
}

func (o *SubscriptionOrder) Update() error {
	return DB.Save(o).Error
}

func GetSubscriptionOrderByTradeNo(tradeNo string) *SubscriptionOrder {
	if tradeNo == "" {
		return nil
	}
	var order SubscriptionOrder
	if err := ReadDB().Where("trade_no = ?", tradeNo).First(&order).Error; err != nil {
		return nil
	}
	return &order
}

func (o *SubscriptionOrder) ExpiresAt() int64 {
	if o == nil {
		return 0
	}
	return subscriptionOrderExpiresAt(o.CreateTime)
}

func (o *SubscriptionOrder) RemainingSeconds(now int64) int64 {
	if o == nil || o.Status != common.TopUpStatusPending {
		return 0
	}
	remaining := o.ExpiresAt() - now
	if remaining < 0 {
		return 0
	}
	return remaining
}

func (o *SubscriptionOrder) EffectiveStatus(now int64) string {
	if o == nil {
		return ""
	}
	if isSubscriptionOrderExpiredByTime(o, now) {
		return common.TopUpStatusExpired
	}
	return o.Status
}

func (o *SubscriptionOrder) SetResumePayload(payload *SubscriptionOrderResumePayload) error {
	if o == nil {
		return errors.New("subscription order is nil")
	}
	if payload == nil {
		o.ProviderPayload = ""
		return nil
	}
	data, err := common.Marshal(payload)
	if err != nil {
		return err
	}
	o.ProviderPayload = string(data)
	return nil
}

func (o *SubscriptionOrder) GetResumePayload() *SubscriptionOrderResumePayload {
	if o == nil {
		return nil
	}
	trimmed := strings.TrimSpace(o.ProviderPayload)
	if trimmed == "" {
		return nil
	}
	var payload SubscriptionOrderResumePayload
	if err := common.Unmarshal([]byte(trimmed), &payload); err != nil {
		return nil
	}
	if strings.TrimSpace(payload.Type) == "" {
		return nil
	}
	return &payload
}

func subscriptionOrderQueryCutoff() int64 {
	return common.GetTimestamp() - subscriptionOrderQueryWindowSeconds
}

func GetUserSubscriptionOrders(userId int, keyword string, pageInfo *common.PageInfo) (orders []*SubscriptionOrder, total int64, err error) {
	tx := DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	query := tx.Model(&SubscriptionOrder{}).Where("user_id = ? AND create_time >= ?", userId, subscriptionOrderQueryCutoff())
	if keyword != "" {
		query = query.Where("trade_no LIKE ?", "%"+keyword+"%")
	}
	if err = query.Count(&total).Error; err != nil {
		tx.Rollback()
		return nil, 0, err
	}
	if err = query.Order("id desc").Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).Find(&orders).Error; err != nil {
		tx.Rollback()
		return nil, 0, err
	}
	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}
	return orders, total, nil
}

func CancelPendingSubscriptionOrder(userId int, tradeNo string) error {
	if userId <= 0 || tradeNo == "" {
		return errors.New("invalid userId or tradeNo")
	}
	refCol := "`trade_no`"
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		refCol = `"trade_no"`
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var order SubscriptionOrder
		if err := lockForUpdate(tx).Where(refCol+" = ?", tradeNo).First(&order).Error; err != nil {
			return ErrSubscriptionOrderNotFound
		}
		if order.UserId != userId {
			return ErrSubscriptionOrderNotFound
		}
		if order.Status == common.TopUpStatusExpired {
			return nil
		}
		if order.Status != common.TopUpStatusPending {
			return ErrSubscriptionOrderStatusInvalid
		}
		order.Status = common.TopUpStatusExpired
		order.CompleteTime = getDBTimestampTx(tx)
		return tx.Save(&order).Error
	})
}

// User subscription instance
type UserSubscription struct {
	Id     int `json:"id"`
	UserId int `json:"user_id" gorm:"index;index:idx_user_sub_active,priority:1"`
	PlanId int `json:"plan_id" gorm:"index"`

	AmountTotal int64 `json:"amount_total" gorm:"type:bigint;not null;default:0"`
	AmountUsed  int64 `json:"amount_used" gorm:"type:bigint;not null;default:0"`

	StartTime int64  `json:"start_time" gorm:"bigint"`
	EndTime   int64  `json:"end_time" gorm:"bigint;index;index:idx_user_sub_active,priority:3"`
	Status    string `json:"status" gorm:"type:varchar(32);index;index:idx_user_sub_active,priority:2"` // active/inactive/expired/cancelled

	Source string `json:"source" gorm:"type:varchar(32);default:'order'"` // order/admin

	LastResetTime int64 `json:"last_reset_time" gorm:"type:bigint;default:0"`
	NextResetTime int64 `json:"next_reset_time" gorm:"type:bigint;default:0;index"`

	UpgradeGroup  string `json:"upgrade_group" gorm:"type:varchar(64);default:''"`
	PrevUserGroup string `json:"prev_user_group" gorm:"type:varchar(64);default:''"`

	// Downgrade target group on expiry (snapshot from plan; empty = revert to PrevUserGroup)
	DowngradeGroup string `json:"downgrade_group" gorm:"type:varchar(64);default:''"`

	// Snapshot of the plan's group mode at purchase ('' = legacy upgrade)
	GroupMode string `json:"group_mode" gorm:"type:varchar(16);default:''"`

	// Whether wallet fallback is allowed after this subscription's quota is exhausted (snapshot from plan).
	// Pointer keeps legacy NULL values distinguishable and avoids database-specific boolean defaults.
	AllowWalletOverflow *bool `json:"allow_wallet_overflow"`

	CreatedAt int64 `json:"created_at" gorm:"bigint"`
	UpdatedAt int64 `json:"updated_at" gorm:"bigint"`
}

func (s *UserSubscription) BeforeCreate(tx *gorm.DB) error {
	now := common.GetTimestamp()
	s.CreatedAt = now
	s.UpdatedAt = now
	return nil
}

func (s *UserSubscription) BeforeUpdate(tx *gorm.DB) error {
	s.UpdatedAt = common.GetTimestamp()
	return nil
}

func (s *UserSubscription) IsAttachGroupMode() bool {
	return strings.TrimSpace(s.GroupMode) == SubscriptionGroupModeAttach
}

type SubscriptionSummary struct {
	Subscription *UserSubscription `json:"subscription"`
}

type SubscriptionResetResult struct {
	PlanId           int    `json:"plan_id"`
	MatchedCount     int    `json:"matched_count"`
	ResetCount       int    `json:"reset_count"`
	UserCount        int    `json:"user_count"`
	AdvanceResetTime bool   `json:"advance_reset_time"`
	PlanTitle        string `json:"-"`
	AffectedUserIds  []int  `json:"-"`
}

func calcPlanEndTime(start time.Time, plan *SubscriptionPlan) (int64, error) {
	if plan == nil {
		return 0, errors.New("plan is nil")
	}
	if plan.DurationValue <= 0 && plan.DurationUnit != SubscriptionDurationCustom {
		return 0, errors.New("duration_value must be > 0")
	}
	switch plan.DurationUnit {
	case SubscriptionDurationYear:
		return start.AddDate(plan.DurationValue, 0, 0).Unix(), nil
	case SubscriptionDurationMonth:
		return start.AddDate(0, plan.DurationValue, 0).Unix(), nil
	case SubscriptionDurationDay:
		return start.Add(time.Duration(plan.DurationValue) * 24 * time.Hour).Unix(), nil
	case SubscriptionDurationHour:
		return start.Add(time.Duration(plan.DurationValue) * time.Hour).Unix(), nil
	case SubscriptionDurationCustom:
		if plan.CustomSeconds <= 0 {
			return 0, errors.New("custom_seconds must be > 0")
		}
		return start.Add(time.Duration(plan.CustomSeconds) * time.Second).Unix(), nil
	default:
		return 0, fmt.Errorf("invalid duration_unit: %s", plan.DurationUnit)
	}
}

func NormalizeResetPeriod(period string) string {
	switch strings.TrimSpace(period) {
	case SubscriptionResetDaily, SubscriptionResetWeekly, SubscriptionResetMonthly, SubscriptionResetCustom:
		return strings.TrimSpace(period)
	default:
		return SubscriptionResetNever
	}
}

func NormalizePurchaseResetPeriod(period string) string {
	switch strings.TrimSpace(period) {
	case SubscriptionResetDaily, SubscriptionResetWeekly, SubscriptionResetMonthly, SubscriptionResetCustom, SubscriptionResetActive:
		return strings.TrimSpace(period)
	default:
		return SubscriptionResetNever
	}
}

func calcResetWindowStart(now time.Time, period string, customSeconds int64) int64 {
	switch NormalizeResetPeriod(period) {
	case SubscriptionResetDaily:
		return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).Unix()
	case SubscriptionResetWeekly:
		weekday := int(now.Weekday())
		if weekday == 0 {
			weekday = 7
		}
		return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).
			AddDate(0, 0, -(weekday - 1)).
			Unix()
	case SubscriptionResetMonthly:
		return time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location()).Unix()
	case SubscriptionResetCustom:
		if customSeconds <= 0 {
			return 0
		}
		return now.Add(-time.Duration(customSeconds) * time.Second).Unix()
	default:
		return 0
	}
}

func getPlanPurchaseWindowStart(plan *SubscriptionPlan, now int64) int64 {
	if plan == nil || plan.MaxPurchaseTotal <= 0 {
		return 0
	}
	if NormalizePurchaseResetPeriod(plan.MaxPurchaseResetPeriod) == SubscriptionResetActive {
		return 0
	}
	return calcResetWindowStart(time.Unix(now, 0), plan.MaxPurchaseResetPeriod, plan.MaxPurchaseResetCustomSeconds)
}

func calcNextResetTime(base time.Time, plan *SubscriptionPlan, endUnix int64) int64 {
	if plan == nil {
		return 0
	}
	period := NormalizeResetPeriod(plan.QuotaResetPeriod)
	if period == SubscriptionResetNever {
		return 0
	}
	var next time.Time
	switch period {
	case SubscriptionResetDaily:
		next = time.Date(base.Year(), base.Month(), base.Day(), 0, 0, 0, 0, base.Location()).
			AddDate(0, 0, 1)
	case SubscriptionResetWeekly:
		// Align to next Monday 00:00
		weekday := int(base.Weekday()) // Sunday=0
		// Convert to Monday=1..Sunday=7
		if weekday == 0 {
			weekday = 7
		}
		daysUntil := 8 - weekday
		next = time.Date(base.Year(), base.Month(), base.Day(), 0, 0, 0, 0, base.Location()).
			AddDate(0, 0, daysUntil)
	case SubscriptionResetMonthly:
		// Align to first day of next month 00:00
		next = time.Date(base.Year(), base.Month(), 1, 0, 0, 0, 0, base.Location()).
			AddDate(0, 1, 0)
	case SubscriptionResetCustom:
		if plan.QuotaResetCustomSeconds <= 0 {
			return 0
		}
		next = base.Add(time.Duration(plan.QuotaResetCustomSeconds) * time.Second)
	default:
		return 0
	}
	if endUnix > 0 && next.Unix() > endUnix {
		return 0
	}
	return next.Unix()
}

func GetSubscriptionPlanById(id int) (*SubscriptionPlan, error) {
	return getSubscriptionPlanByIdTx(nil, id)
}

func getSubscriptionPlanByIdTx(tx *gorm.DB, id int) (*SubscriptionPlan, error) {
	if id <= 0 {
		return nil, errors.New("invalid plan id")
	}
	key := subscriptionPlanCacheKey(id)
	if key != "" {
		if cached, found, err := getSubscriptionPlanCache().Get(key); err == nil && found {
			cached.NormalizeDefaults()
			return &cached, nil
		}
	}
	var plan SubscriptionPlan
	query := ReadDB()
	if tx != nil {
		query = tx
	}
	if err := query.Where("id = ?", id).First(&plan).Error; err != nil {
		return nil, err
	}
	plan.NormalizeDefaults()
	_ = getSubscriptionPlanCache().SetWithTTL(key, plan, subscriptionPlanCacheTTL())
	return &plan, nil
}

func CountUserSubscriptionsByPlan(userId int, planId int) (int64, error) {
	if userId <= 0 || planId <= 0 {
		return 0, errors.New("invalid userId or planId")
	}
	var count int64
	if err := ReadDB().Model(&UserSubscription{}).
		Where("user_id = ? AND plan_id = ?", userId, planId).
		Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

func countTotalSubscriptionsByPlanWithWindow(tx *gorm.DB, plan *SubscriptionPlan, now int64) (int64, error) {
	if plan == nil || plan.Id <= 0 {
		return 0, errors.New("invalid plan")
	}
	if tx == nil {
		tx = DB
	}
	query := tx.Model(&UserSubscription{}).Where("plan_id = ?", plan.Id)
	if NormalizePurchaseResetPeriod(plan.MaxPurchaseResetPeriod) == SubscriptionResetActive {
		query = query.Where("end_time > ? AND status IN ?", now, []string{SubscriptionStatusActive, SubscriptionStatusInactive})
	} else if windowStart := getPlanPurchaseWindowStart(plan, now); windowStart > 0 {
		query = query.Where("start_time >= ?", windowStart)
	}
	var count int64
	if err := query.Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

func countPendingSubscriptionOrdersByPlanWithWindow(tx *gorm.DB, plan *SubscriptionPlan, now int64) (int64, error) {
	if plan == nil || plan.Id <= 0 {
		return 0, errors.New("invalid plan")
	}
	if tx == nil {
		tx = DB
	}
	query := tx.Model(&SubscriptionOrder{}).Where("plan_id = ? AND status = ?", plan.Id, common.TopUpStatusPending)
	query = query.Where("create_time > ?", pendingSubscriptionOrderCutoff(now))
	if windowStart := getPlanPurchaseWindowStart(plan, now); windowStart > 0 {
		query = query.Where("create_time >= ?", windowStart)
	}
	var count int64
	if err := query.Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

func CountSubscriptionPlanPurchaseCounts(plans []SubscriptionPlan, includePendingOrders bool) (map[int]int64, error) {
	counts := make(map[int]int64, len(plans))
	if len(plans) == 0 {
		return counts, nil
	}
	now := GetDBTimestamp()
	seen := make(map[int]struct{}, len(plans))
	for _, plan := range plans {
		if plan.Id <= 0 {
			continue
		}
		if _, ok := seen[plan.Id]; ok {
			continue
		}
		seen[plan.Id] = struct{}{}
		totalCount, err := countTotalSubscriptionsByPlanWithWindow(nil, &plan, now)
		if err != nil {
			return nil, err
		}
		if includePendingOrders {
			pendingCount, err := countPendingSubscriptionOrdersByPlanWithWindow(nil, &plan, now)
			if err != nil {
				return nil, err
			}
			totalCount += pendingCount
		}
		counts[plan.Id] = totalCount
	}
	return counts, nil
}

func CheckSubscriptionPlanPurchaseAllowed(userId int, plan *SubscriptionPlan, includePendingOrders bool) error {
	if plan == nil || plan.Id <= 0 {
		return errors.New("invalid plan")
	}
	if userId <= 0 {
		return errors.New("invalid userId")
	}
	// 已持有该套餐的未过期订阅且套餐无全局限购时，本次购买是续期：
	// 不新建订阅、不占新名额，限购不阻拦。全局限购套餐不提供续期，走正常限购判定。
	if plan.MaxPurchaseTotal <= 0 {
		if hasLive, err := hasLiveUserSubscriptionOfPlan(nil, userId, plan.Id); err != nil {
			return err
		} else if hasLive {
			return nil
		}
	}
	if plan.MaxPurchasePerUser > 0 {
		count, err := CountUserSubscriptionsByPlan(userId, plan.Id)
		if err != nil {
			return err
		}
		if count >= int64(plan.MaxPurchasePerUser) {
			return errors.New("已达到该套餐购买上限")
		}
	}
	if plan.MaxPurchaseTotal > 0 {
		now := GetDBTimestamp()
		totalCount, err := countTotalSubscriptionsByPlanWithWindow(nil, plan, now)
		if err != nil {
			return err
		}
		if includePendingOrders {
			pendingCount, err := countPendingSubscriptionOrdersByPlanWithWindow(nil, plan, now)
			if err != nil {
				return err
			}
			totalCount += pendingCount
		}
		if totalCount >= int64(plan.MaxPurchaseTotal) {
			return errors.New("该套餐已售罄")
		}
	}
	return nil
}

func checkSubscriptionPlanPurchaseAllowedTx(tx *gorm.DB, userId int, plan *SubscriptionPlan) error {
	if tx == nil {
		return errors.New("tx is nil")
	}
	if plan == nil || plan.Id <= 0 {
		return errors.New("invalid plan")
	}
	if userId <= 0 {
		return errors.New("invalid userId")
	}
	if plan.MaxPurchasePerUser > 0 {
		var count int64
		if err := tx.Model(&UserSubscription{}).
			Where("user_id = ? AND plan_id = ?", userId, plan.Id).
			Count(&count).Error; err != nil {
			return err
		}
		if count >= int64(plan.MaxPurchasePerUser) {
			return errors.New("已达到该套餐购买上限")
		}
	}
	if plan.MaxPurchaseTotal > 0 {
		var lockedPlan SubscriptionPlan
		if err := lockForUpdate(tx).Select("id").Where("id = ?", plan.Id).First(&lockedPlan).Error; err != nil {
			return err
		}
		count, err := countTotalSubscriptionsByPlanWithWindow(tx, plan, getDBTimestampTx(tx))
		if err != nil {
			return err
		}
		if count >= int64(plan.MaxPurchaseTotal) {
			return errors.New("该套餐已售罄")
		}
	}
	return nil
}

func getUserGroupByIdTx(tx *gorm.DB, userId int) (string, error) {
	if userId <= 0 {
		return "", errors.New("invalid userId")
	}
	if tx == nil {
		tx = DB
	}
	groupCol := commonGroupCol
	if groupCol == "" {
		groupCol = "group"
	}
	var group string
	if err := tx.Model(&User{}).Where("id = ?", userId).Select(groupCol).Find(&group).Error; err != nil {
		return "", err
	}
	return group, nil
}

func downgradeUserGroupForSubscriptionTx(tx *gorm.DB, sub *UserSubscription, now int64) (string, error) {
	if tx == nil || sub == nil {
		return "", errors.New("invalid downgrade args")
	}
	// 追加分组模式从不改动用户分组，到期/作废无需任何降级处理
	if sub.IsAttachGroupMode() {
		return "", nil
	}
	downgradeGroup := strings.TrimSpace(sub.DowngradeGroup)
	upgradeGroup := strings.TrimSpace(sub.UpgradeGroup)
	// Nothing to do if neither an explicit downgrade target nor an upgrade snapshot exists.
	if downgradeGroup == "" && upgradeGroup == "" {
		return "", nil
	}
	currentGroup, err := getUserGroupByIdTx(tx, sub.UserId)
	if err != nil {
		return "", err
	}
	// PrevUserGroup 为空且用户不在升级分组中，说明激活时并未覆盖分组
	// （当前分组已包含升级目标，或激活后被人工改组）。到期/作废时不做
	// 任何分组变更，防止显式降级分组误降用户原有的高级分组。
	if upgradeGroup != "" && strings.TrimSpace(sub.PrevUserGroup) == "" && currentGroup != upgradeGroup {
		return "", nil
	}
	// If another active upgraded subscription exists, keep the current group.
	// 追加分组订阅不改动用户分组，不算作"其他生效升级订阅"。
	var activeSub UserSubscription
	activeQuery := tx.Where("user_id = ? AND status = ? AND end_time > ? AND id <> ? AND upgrade_group <> '' AND (group_mode IS NULL OR group_mode <> ?)",
		sub.UserId, SubscriptionStatusActive, now, sub.Id, SubscriptionGroupModeAttach).
		Order("end_time desc, id desc").
		Limit(1).
		Find(&activeSub)
	if activeQuery.Error == nil && activeQuery.RowsAffected > 0 {
		return "", nil
	}
	// Determine the downgrade target: an explicit downgrade group takes precedence,
	// otherwise revert to the group held before purchase (legacy behavior).
	target := downgradeGroup
	if target == "" {
		// Legacy behavior: only revert when the subscription actually elevated the user.
		if currentGroup != upgradeGroup {
			return "", nil
		}
		target = strings.TrimSpace(sub.PrevUserGroup)
	}
	if target == "" || target == currentGroup {
		return "", nil
	}
	if err := tx.Model(&User{}).Where("id = ?", sub.UserId).
		Update("group", target).Error; err != nil {
		return "", err
	}
	return target, nil
}

func hasLiveActiveUserSubscriptionTx(tx *gorm.DB, userId int, now int64) (bool, error) {
	if tx == nil {
		return false, errors.New("tx is nil")
	}
	if userId <= 0 {
		return false, errors.New("invalid userId")
	}
	var count int64
	if err := tx.Model(&UserSubscription{}).
		Where("user_id = ? AND status = ? AND end_time > ? AND (group_mode IS NULL OR group_mode <> ?)",
			userId, SubscriptionStatusActive, now, SubscriptionGroupModeAttach).
		Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func activateUserSubscriptionTx(tx *gorm.DB, sub *UserSubscription) (string, error) {
	if tx == nil || sub == nil {
		return "", errors.New("invalid activate args")
	}
	prevGroup := ""
	upgradeGroup := strings.TrimSpace(sub.UpgradeGroup)
	// 追加分组模式只授予分组使用权，不覆盖用户分组
	if upgradeGroup != "" && !sub.IsAttachGroupMode() {
		currentGroup, err := getUserGroupByIdTx(tx, sub.UserId)
		if err != nil {
			return "", err
		}
		// 当前分组已通过特殊可选分组配置包含升级目标（例如 svip 已含 vip）时不覆盖分组，
		// 避免高级分组用户购买低级分组订阅后丢失原分组。
		if currentGroup != upgradeGroup && !ratio_setting.GroupSpecialGrantsGroup(currentGroup, upgradeGroup) {
			prevGroup = currentGroup
			if err := tx.Model(&User{}).Where("id = ?", sub.UserId).
				Update("group", upgradeGroup).Error; err != nil {
				return "", err
			}
		}
	}
	updates := map[string]interface{}{
		"status":          SubscriptionStatusActive,
		"prev_user_group": prevGroup,
		"updated_at":      common.GetTimestamp(),
	}
	if err := tx.Model(&UserSubscription{}).Where("id = ?", sub.Id).Updates(updates).Error; err != nil {
		return "", err
	}
	sub.Status = SubscriptionStatusActive
	sub.PrevUserGroup = prevGroup
	if upgradeGroup != "" && prevGroup != "" {
		return upgradeGroup, nil
	}
	return "", nil
}

// hasLiveUserSubscriptionOfPlan reports whether the user holds an unexpired
// (active or inactive) subscription of the plan, i.e. a repurchase would renew
// it instead of creating a new subscription.
func hasLiveUserSubscriptionOfPlan(db *gorm.DB, userId int, planId int) (bool, error) {
	if db == nil {
		db = ReadDB()
	}
	var count int64
	if err := db.Model(&UserSubscription{}).
		Where("user_id = ? AND plan_id = ? AND status IN ? AND end_time > ?",
			userId, planId, []string{SubscriptionStatusActive, SubscriptionStatusInactive}, GetDBTimestamp()).
		Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

// renewUserSubscriptionForPlanTx extends the user's live subscription of the
// same plan by one plan period instead of creating a duplicate row. Returns
// nil when the user holds no live subscription of the plan.
func renewUserSubscriptionForPlanTx(tx *gorm.DB, userId int, plan *SubscriptionPlan) (*UserSubscription, error) {
	// 配置了全局限购的套餐不提供持有期续期：名额必须等到期释放后重新购买，
	// 否则持有者可以无限顺延、永久占用稀缺名额。
	if plan.MaxPurchaseTotal > 0 {
		return nil, nil
	}
	now := getDBTimestampTx(tx)
	var sub UserSubscription
	query := lockForUpdate(tx).
		Where("user_id = ? AND plan_id = ? AND status IN ? AND end_time > ?",
			userId, plan.Id, []string{SubscriptionStatusActive, SubscriptionStatusInactive}, now).
		Order("end_time desc, id desc").
		Limit(1).
		Find(&sub)
	if query.Error != nil {
		return nil, query.Error
	}
	if query.RowsAffected == 0 {
		return nil, nil
	}
	// 从现有到期时间顺延一个周期，提前续期不损失剩余时长
	newEnd, err := calcPlanEndTime(time.Unix(sub.EndTime, 0), plan)
	if err != nil {
		return nil, err
	}
	sub.EndTime = newEnd
	// 无重置周期时 TotalAmount 是整个订阅期的额度池，续期一个周期就追加一份；
	// 有重置周期时它是每周期上限，随重置自动刷新，不追加。
	if plan.TotalAmount > 0 && NormalizeResetPeriod(plan.QuotaResetPeriod) == SubscriptionResetNever {
		if sub.AmountTotal > math.MaxInt64-plan.TotalAmount {
			sub.AmountTotal = math.MaxInt64
		} else {
			sub.AmountTotal += plan.TotalAmount
		}
	}
	// 原到期时间临近时重置排程可能已终止（NextResetTime=0），续期后按新到期时间重排
	if NormalizeResetPeriod(plan.QuotaResetPeriod) != SubscriptionResetNever && sub.NextResetTime == 0 {
		next := calcNextResetTime(time.Unix(now, 0), plan, newEnd)
		sub.NextResetTime = next
		if next > 0 {
			sub.LastResetTime = now
		}
	}
	if err := tx.Save(&sub).Error; err != nil {
		return nil, err
	}
	return &sub, nil
}

// CreateUserSubscriptionFromPlanTx creates a subscription from the plan, or —
// when the user already holds a live subscription of the same plan — renews it
// by one period. The second return value reports whether a renewal happened.
func CreateUserSubscriptionFromPlanTx(tx *gorm.DB, userId int, plan *SubscriptionPlan, source string) (*UserSubscription, bool, error) {
	if tx == nil {
		return nil, false, errors.New("tx is nil")
	}
	if plan == nil || plan.Id == 0 {
		return nil, false, errors.New("invalid plan")
	}
	if userId <= 0 {
		return nil, false, errors.New("invalid user id")
	}
	// 同套餐续期不占新名额，先于限购检查
	renewed, err := renewUserSubscriptionForPlanTx(tx, userId, plan)
	if err != nil {
		return nil, false, err
	}
	if renewed != nil {
		return renewed, true, nil
	}
	if err := checkSubscriptionPlanPurchaseAllowedTx(tx, userId, plan); err != nil {
		return nil, false, err
	}
	nowUnix := getDBTimestampTx(tx)
	now := time.Unix(nowUnix, 0)
	endUnix, err := calcPlanEndTime(now, plan)
	if err != nil {
		return nil, false, err
	}
	resetBase := now
	nextReset := calcNextResetTime(resetBase, plan, endUnix)
	lastReset := int64(0)
	if nextReset > 0 {
		lastReset = now.Unix()
	}
	upgradeGroup := strings.TrimSpace(plan.UpgradeGroup)
	downgradeGroup := strings.TrimSpace(plan.DowngradeGroup)
	groupMode := ""
	if plan.IsAttachGroupMode() && upgradeGroup != "" {
		groupMode = SubscriptionGroupModeAttach
		// 追加分组模式不改动用户分组，也就没有降级目标
		downgradeGroup = ""
	}
	status := SubscriptionStatusActive
	// 追加分组订阅与其他订阅并存生效，不参与"同时只有一个生效"的排队
	if groupMode != SubscriptionGroupModeAttach {
		hasActive, err := hasLiveActiveUserSubscriptionTx(tx, userId, nowUnix)
		if err != nil {
			return nil, false, err
		}
		if hasActive {
			status = SubscriptionStatusInactive
		}
	}
	allowWalletOverflow := true
	if plan.AllowWalletOverflow != nil {
		allowWalletOverflow = *plan.AllowWalletOverflow
	}
	sub := &UserSubscription{
		UserId:              userId,
		PlanId:              plan.Id,
		AmountTotal:         plan.TotalAmount,
		AmountUsed:          0,
		StartTime:           now.Unix(),
		EndTime:             endUnix,
		Status:              status,
		Source:              source,
		LastResetTime:       lastReset,
		NextResetTime:       nextReset,
		UpgradeGroup:        upgradeGroup,
		PrevUserGroup:       "",
		DowngradeGroup:      downgradeGroup,
		GroupMode:           groupMode,
		AllowWalletOverflow: common.GetPointer(allowWalletOverflow),
		CreatedAt:           common.GetTimestamp(),
		UpdatedAt:           common.GetTimestamp(),
	}
	if err := tx.Create(sub).Error; err != nil {
		return nil, false, err
	}
	// Initialize tier usage rows if multi-tier
	if tiers := plan.GetQuotaTiers(); len(tiers) > 0 {
		if err := initTierUsage(tx, sub.Id, tiers, nowUnix); err != nil {
			return nil, false, err
		}
	}
	if sub.Status == SubscriptionStatusActive {
		if _, err := activateUserSubscriptionTx(tx, sub); err != nil {
			return nil, false, err
		}
	}
	return sub, false, nil
}

// UserActiveSubscriptionsAllowWalletOverflow reports whether wallet balance may
// be used after any of the user's active subscription quotas is exhausted.
func UserActiveSubscriptionsAllowWalletOverflow(userId int) (bool, error) {
	return userActiveSubscriptionsAllowWalletOverflowForGroup(userId, "")
}

// UserActiveSubscriptionsAllowWalletOverflowForUsingGroup applies wallet
// fallback rules only to subscriptions usable by the current request group.
// Legacy NULL values are treated as allowing wallet fallback.
func UserActiveSubscriptionsAllowWalletOverflowForUsingGroup(userId int, usingGroup string) (bool, error) {
	return userActiveSubscriptionsAllowWalletOverflowForGroup(userId, usingGroup)
}

func userActiveSubscriptionsAllowWalletOverflowForGroup(userId int, usingGroup string) (bool, error) {
	if userId <= 0 {
		return false, errors.New("invalid userId")
	}
	now := common.GetTimestamp()
	var strictCount int64
	query := ReadDB().Model(&UserSubscription{}).
		Where("user_id = ? AND status = ? AND end_time > ? AND allow_wallet_overflow = ?", userId, SubscriptionStatusActive, now, false)
	if strings.TrimSpace(usingGroup) != "" {
		query = query.Where("(upgrade_group IS NULL OR upgrade_group = '' OR upgrade_group IN ?)", subscriptionUpgradeGroupsForUsingGroup(usingGroup))
	}
	if err := query.
		Count(&strictCount).Error; err != nil {
		return false, err
	}
	return strictCount == 0, nil
}

// Complete a subscription order (idempotent). Creates a UserSubscription snapshot from the plan.
// expectedPaymentProvider guards against cross-gateway callback attacks (empty skips the check).
// actualPaymentMethod updates the order's PaymentMethod to reflect the real payment type used (empty skips update).
func CompleteSubscriptionOrder(tradeNo string, providerPayload string, expectedPaymentProvider string, actualPaymentMethod string) error {
	return CompleteSubscriptionOrderWithPaymentCheck(tradeNo, providerPayload, expectedPaymentProvider, actualPaymentMethod, "", "")
}

func CompleteSubscriptionOrderWithPaymentCheck(tradeNo string, providerPayload string, expectedPaymentProvider string, actualPaymentMethod string, paidToken string, paidAmount string) error {
	if tradeNo == "" {
		return errors.New("tradeNo is empty")
	}
	refCol := "`trade_no`"
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		refCol = `"trade_no"`
	}
	var logUserId int
	var logPlanTitle string
	var logMoney float64
	var logPaymentMethod string
	var cacheGroup string
	logRenewed := false
	orderExpired := false
	err := DB.Transaction(func(tx *gorm.DB) error {
		var order SubscriptionOrder
		if err := lockForUpdate(tx).Where(refCol+" = ?", tradeNo).First(&order).Error; err != nil {
			return ErrSubscriptionOrderNotFound
		}
		if expectedPaymentProvider != "" && order.PaymentProvider != expectedPaymentProvider {
			return ErrPaymentMethodMismatch
		}
		now := getDBTimestampTx(tx)
		if order.Status == common.TopUpStatusSuccess {
			return nil
		}
		if isSubscriptionOrderExpiredByTime(&order, now) {
			order.Status = common.TopUpStatusExpired
			order.CompleteTime = now
			if err := tx.Save(&order).Error; err != nil {
				return err
			}
			orderExpired = true
			return nil
		}
		if paidToken != "" || paidAmount != "" {
			if err := validateExpectedChainPayment(order.ExpectedPaymentToken, order.ExpectedPaymentAmount, paidToken, paidAmount); err != nil {
				return err
			}
		}
		if order.Status != common.TopUpStatusPending {
			return ErrSubscriptionOrderStatusInvalid
		}
		plan, err := GetSubscriptionPlanById(order.PlanId)
		if err != nil {
			return err
		}
		if !plan.Enabled {
			// still allow completion for already purchased orders
		}
		sub, renewed, err := CreateUserSubscriptionFromPlanTx(tx, order.UserId, plan, "order")
		if err != nil {
			return err
		}
		logRenewed = renewed
		// 仅在激活确实覆盖了用户分组时刷新分组缓存（PrevUserGroup 非空），
		// 续期或分组未变更（已包含升级目标）时刷新会造成缓存与数据库不一致。
		if !renewed && sub.Status == SubscriptionStatusActive && sub.PrevUserGroup != "" {
			cacheGroup = strings.TrimSpace(sub.UpgradeGroup)
		}
		if err := upsertSubscriptionTopUpTx(tx, &order); err != nil {
			return err
		}
		order.Status = common.TopUpStatusSuccess
		order.CompleteTime = common.GetTimestamp()
		if providerPayload != "" {
			order.ProviderPayload = providerPayload
		}
		if actualPaymentMethod != "" && order.PaymentMethod != actualPaymentMethod {
			order.PaymentMethod = actualPaymentMethod
		}
		if err := tx.Save(&order).Error; err != nil {
			return err
		}
		logUserId = order.UserId
		logPlanTitle = plan.Title
		logMoney = order.Money
		logPaymentMethod = order.PaymentMethod
		return nil
	})
	if err != nil {
		return err
	}
	if orderExpired {
		return ErrSubscriptionOrderExpired
	}
	if cacheGroup != "" && logUserId > 0 {
		_ = UpdateUserGroupCache(logUserId, cacheGroup)
	}
	if logUserId > 0 {
		action := "购买"
		if logRenewed {
			action = "续期"
		}
		msg := fmt.Sprintf("订阅%s成功，套餐: %s，支付金额: %.2f，支付方式: %s", action, logPlanTitle, logMoney, logPaymentMethod)
		RecordLog(logUserId, LogTypeTopup, msg)
	}
	return nil
}

func upsertSubscriptionTopUpTx(tx *gorm.DB, order *SubscriptionOrder) error {
	if tx == nil || order == nil {
		return errors.New("invalid subscription order")
	}
	now := common.GetTimestamp()
	var topup TopUp
	if err := tx.Where("trade_no = ?", order.TradeNo).First(&topup).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			topup = TopUp{
				UserId:        order.UserId,
				Amount:        0,
				Money:         order.Money,
				TradeNo:       order.TradeNo,
				PaymentMethod: order.PaymentMethod,
				CreateTime:    order.CreateTime,
				CompleteTime:  now,
				Status:        common.TopUpStatusSuccess,
			}
			return tx.Create(&topup).Error
		}
		return err
	}
	topup.Money = order.Money
	if topup.PaymentMethod == "" {
		topup.PaymentMethod = order.PaymentMethod
	} else if topup.PaymentMethod != order.PaymentMethod {
		return ErrPaymentMethodMismatch
	}
	if topup.CreateTime == 0 {
		topup.CreateTime = order.CreateTime
	}
	topup.CompleteTime = now
	topup.Status = common.TopUpStatusSuccess
	return tx.Save(&topup).Error
}

func ExpireSubscriptionOrder(tradeNo string, expectedPaymentProvider string) error {
	if tradeNo == "" {
		return errors.New("tradeNo is empty")
	}
	refCol := "`trade_no`"
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		refCol = `"trade_no"`
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var order SubscriptionOrder
		if err := lockForUpdate(tx).Where(refCol+" = ?", tradeNo).First(&order).Error; err != nil {
			return ErrSubscriptionOrderNotFound
		}
		if expectedPaymentProvider != "" && order.PaymentProvider != expectedPaymentProvider {
			return ErrPaymentMethodMismatch
		}
		if order.Status != common.TopUpStatusPending {
			return nil
		}
		order.Status = common.TopUpStatusExpired
		order.CompleteTime = common.GetTimestamp()
		return tx.Save(&order).Error
	})
}

func ExpireStalePendingSubscriptionOrders(limit int) (int, error) {
	if limit <= 0 {
		limit = 100
	}
	now := GetDBTimestamp()
	cutoff := pendingSubscriptionOrderCutoff(now)
	if cutoff <= 0 {
		return 0, nil
	}
	var ids []int
	if err := ReadDB().Model(&SubscriptionOrder{}).
		Where("status = ? AND create_time <= ?", common.TopUpStatusPending, cutoff).
		Order("create_time asc").
		Limit(limit).
		Pluck("id", &ids).Error; err != nil {
		return 0, err
	}
	total := 0
	for _, id := range ids {
		err := DB.Transaction(func(tx *gorm.DB) error {
			var order SubscriptionOrder
			if err := lockForUpdate(tx).Where("id = ?", id).First(&order).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return nil
				}
				return err
			}
			if !isSubscriptionOrderExpiredByTime(&order, now) {
				return nil
			}
			order.Status = common.TopUpStatusExpired
			order.CompleteTime = now
			if err := tx.Save(&order).Error; err != nil {
				return err
			}
			total++
			return nil
		})
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

// Admin bind (no payment). Creates a UserSubscription from a plan.
func AdminBindSubscription(userId int, planId int, sourceNote string) (string, error) {
	if userId <= 0 || planId <= 0 {
		return "", errors.New("invalid userId or planId")
	}
	plan, err := GetSubscriptionPlanById(planId)
	if err != nil {
		return "", err
	}
	createdStatus := ""
	groupApplied := false
	wasRenewed := false
	renewedEndTime := int64(0)
	err = DB.Transaction(func(tx *gorm.DB) error {
		sub, renewed, err := CreateUserSubscriptionFromPlanTx(tx, userId, plan, "admin")
		if err != nil {
			return err
		}
		createdStatus = sub.Status
		groupApplied = !renewed && sub.PrevUserGroup != ""
		wasRenewed = renewed
		renewedEndTime = sub.EndTime
		return nil
	})
	if err != nil {
		return "", err
	}
	if wasRenewed {
		return fmt.Sprintf("用户已持有该套餐，已续期至 %s", time.Unix(renewedEndTime, 0).Format("2006-01-02 15:04")), nil
	}
	if createdStatus == SubscriptionStatusActive && strings.TrimSpace(plan.UpgradeGroup) != "" {
		// 分组未被覆盖（用户已在升级分组中，或当前分组已包含升级目标）时
		// 不刷新缓存也不提示升级。
		if !groupApplied {
			return "", nil
		}
		_ = UpdateUserGroupCache(userId, plan.UpgradeGroup)
		return fmt.Sprintf("用户分组将升级到 %s", plan.UpgradeGroup), nil
	}
	if createdStatus == SubscriptionStatusActive {
		return "", nil
	}
	if strings.TrimSpace(plan.UpgradeGroup) != "" {
		return fmt.Sprintf("当前已有激活订阅，新订阅已创建为未激活状态，切换后才会升级到 %s", plan.UpgradeGroup), nil
	}
	return "当前已有激活订阅，新订阅已创建为未激活状态", nil
}

func SwitchUserActiveSubscription(userId int, targetSubscriptionId int) (string, error) {
	if userId <= 0 || targetSubscriptionId <= 0 {
		return "", errors.New("invalid userId or targetSubscriptionId")
	}
	now := GetDBTimestamp()
	cacheGroup := ""
	err := DB.Transaction(func(tx *gorm.DB) error {
		var target UserSubscription
		if err := lockForUpdate(tx).
			Where("id = ? AND user_id = ?", targetSubscriptionId, userId).
			First(&target).Error; err != nil {
			return err
		}
		if target.EndTime > 0 && target.EndTime <= now {
			return errors.New("subscription already expired")
		}
		if target.Status == SubscriptionStatusCancelled || target.Status == SubscriptionStatusExpired {
			return errors.New("subscription is not switchable")
		}

		// 追加分组订阅与其他订阅并存生效，切换到它只需激活自身，不停用任何订阅
		if target.IsAttachGroupMode() {
			if target.Status != SubscriptionStatusActive {
				if _, err := activateUserSubscriptionTx(tx, &target); err != nil {
					return err
				}
			}
			return nil
		}

		var activeSubs []UserSubscription
		if err := lockForUpdate(tx).
			Where("user_id = ? AND status = ? AND end_time > ?", userId, SubscriptionStatusActive, now).
			Order("end_time desc, id desc").
			Find(&activeSubs).Error; err != nil {
			return err
		}

		for _, activeSub := range activeSubs {
			if activeSub.Id == target.Id {
				continue
			}
			// 追加分组订阅并存生效，切换普通订阅时不停用它们
			if activeSub.IsAttachGroupMode() {
				continue
			}
			if err := tx.Model(&UserSubscription{}).
				Where("id = ?", activeSub.Id).
				Updates(map[string]interface{}{
					"status":     SubscriptionStatusInactive,
					"updated_at": common.GetTimestamp(),
				}).Error; err != nil {
				return err
			}
			downgradedGroup, err := downgradeUserGroupForSubscriptionTx(tx, &activeSub, now)
			if err != nil {
				return err
			}
			if downgradedGroup != "" {
				cacheGroup = downgradedGroup
			}
		}

		if target.Status != SubscriptionStatusActive {
			appliedGroup, err := activateUserSubscriptionTx(tx, &target)
			if err != nil {
				return err
			}
			if appliedGroup != "" {
				cacheGroup = appliedGroup
			}
			return nil
		}

		if strings.TrimSpace(target.UpgradeGroup) != "" {
			currentGroup, err := getUserGroupByIdTx(tx, userId)
			if err != nil {
				return err
			}
			if currentGroup == target.UpgradeGroup {
				cacheGroup = currentGroup
			}
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if cacheGroup != "" {
		_ = UpdateUserGroupCache(userId, cacheGroup)
		return fmt.Sprintf("已切换到 %s 分组订阅", cacheGroup), nil
	}
	return "订阅已切换", nil
}

func calcSubscriptionBalanceQuota(priceAmount float64) (int, error) {
	if priceAmount <= 0 {
		return 0, nil
	}
	if common.QuotaPerUnit <= 0 {
		return 0, errors.New("额度单位配置错误")
	}
	quota := decimal.NewFromFloat(priceAmount).
		Mul(decimal.NewFromFloat(common.QuotaPerUnit)).
		Ceil().
		IntPart()
	return int(quota), nil
}

// PurchaseSubscriptionWithBalance creates a subscription by deducting the user's wallet quota.
func PurchaseSubscriptionWithBalance(userId int, planId int) error {
	if userId <= 0 || planId <= 0 {
		return errors.New("invalid userId or planId")
	}

	var logPlanTitle string
	var logMoney float64
	var chargedQuota int
	var upgradeGroup string
	groupApplied := false
	wasRenewed := false
	err := DB.Transaction(func(tx *gorm.DB) error {
		plan, err := getSubscriptionPlanByIdTx(tx, planId)
		if err != nil {
			return err
		}
		if !plan.Enabled {
			return errors.New("套餐未启用")
		}
		if plan.PriceAmount < 0 {
			return errors.New("套餐价格不能为负数")
		}
		if plan.AllowBalancePay != nil && !*plan.AllowBalancePay {
			return errors.New("该套餐不允许使用余额兑换")
		}

		requiredQuota, err := calcSubscriptionBalanceQuota(plan.PriceAmount)
		if err != nil {
			return err
		}

		var user User
		if err := lockForUpdate(tx).Where("id = ?", userId).First(&user).Error; err != nil {
			return err
		}
		if requiredQuota > 0 && user.Quota < requiredQuota {
			return errors.New("余额不足")
		}
		if requiredQuota > 0 {
			if err := tx.Model(&User{}).Where("id = ?", userId).
				Update("quota", gorm.Expr("quota - ?", requiredQuota)).Error; err != nil {
				return err
			}
		}

		createdSub, renewed, err := CreateUserSubscriptionFromPlanTx(tx, userId, plan, PaymentMethodBalance)
		if err != nil {
			return err
		}
		// 仅在激活确实覆盖了用户分组时刷新分组缓存：订阅可能是续期、被创建为
		// 未激活（已有生效订阅），或当前分组已包含升级目标而未变更分组。
		groupApplied = !renewed && createdSub.Status == SubscriptionStatusActive && createdSub.PrevUserGroup != ""
		wasRenewed = renewed

		now := common.GetTimestamp()
		tradeNo := fmt.Sprintf("SUBBALUSR%dNO%s%d", userId, common.GetRandomString(6), time.Now().UnixNano())
		order := &SubscriptionOrder{
			UserId:          userId,
			PlanId:          plan.Id,
			Money:           plan.PriceAmount,
			TradeNo:         tradeNo,
			PaymentMethod:   PaymentMethodBalance,
			PaymentProvider: PaymentProviderBalance,
			Status:          common.TopUpStatusSuccess,
			CreateTime:      now,
			CompleteTime:    now,
			ProviderPayload: fmt.Sprintf("charged_quota=%d", requiredQuota),
		}
		if err := tx.Create(order).Error; err != nil {
			return err
		}

		logPlanTitle = plan.Title
		logMoney = plan.PriceAmount
		chargedQuota = requiredQuota
		upgradeGroup = strings.TrimSpace(plan.UpgradeGroup)
		return nil
	})
	if err != nil {
		return err
	}

	if chargedQuota > 0 {
		if err := cacheDecrUserQuota(userId, int64(chargedQuota)); err != nil {
			common.SysLog("failed to decrease user quota cache after subscription balance purchase: " + err.Error())
		}
	}
	if upgradeGroup != "" && groupApplied {
		_ = UpdateUserGroupCache(userId, upgradeGroup)
	}
	action := "购买订阅"
	if wasRenewed {
		action = "续期订阅"
	}
	msg := fmt.Sprintf("使用余额%s成功，套餐: %s，支付金额: %.2f，扣除额度: %d", action, logPlanTitle, logMoney, chargedQuota)
	RecordLog(userId, LogTypeTopup, msg)
	return nil
}

// GetAllActiveUserSubscriptions returns all active subscriptions for a user.
func GetAllActiveUserSubscriptions(userId int) ([]SubscriptionSummary, error) {
	if userId <= 0 {
		return nil, errors.New("invalid userId")
	}
	now := common.GetTimestamp()
	var subs []UserSubscription
	err := ReadDB().Where("user_id = ? AND status = ? AND end_time > ?", userId, SubscriptionStatusActive, now).
		Order("end_time desc, id desc").
		Find(&subs).Error
	if err != nil {
		return nil, err
	}
	return buildSubscriptionSummaries(subs), nil
}

// HasActiveUserSubscription returns whether the user has any active subscription.
// This is a lightweight existence check to avoid heavy pre-consume transactions.
func HasActiveUserSubscription(userId int) (bool, error) {
	if userId <= 0 {
		return false, errors.New("invalid userId")
	}
	now := common.GetTimestamp()
	var count int64
	if err := ReadDB().Model(&UserSubscription{}).
		Where("user_id = ? AND status = ? AND end_time > ?", userId, SubscriptionStatusActive, now).
		Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

// GetAllUserSubscriptions returns all subscriptions for a user.
func GetAllUserSubscriptions(userId int) ([]SubscriptionSummary, error) {
	if userId <= 0 {
		return nil, errors.New("invalid userId")
	}
	var subs []UserSubscription
	err := ReadDB().Where("user_id = ?", userId).
		Order("end_time desc, id desc").
		Find(&subs).Error
	if err != nil {
		return nil, err
	}
	return buildSubscriptionSummaries(subs), nil
}

func buildSubscriptionSummaries(subs []UserSubscription) []SubscriptionSummary {
	if len(subs) == 0 {
		return []SubscriptionSummary{}
	}
	result := make([]SubscriptionSummary, 0, len(subs))
	for _, sub := range subs {
		subCopy := sub
		result = append(result, SubscriptionSummary{
			Subscription: &subCopy,
		})
	}
	return result
}

// AdminInvalidateUserSubscription marks a user subscription as cancelled and ends it immediately.
func AdminInvalidateUserSubscription(userSubscriptionId int) (string, error) {
	if userSubscriptionId <= 0 {
		return "", errors.New("invalid userSubscriptionId")
	}
	now := common.GetTimestamp()
	cacheGroup := ""
	downgradeGroup := ""
	var userId int
	err := DB.Transaction(func(tx *gorm.DB) error {
		var sub UserSubscription
		if err := lockForUpdate(tx).
			Where("id = ?", userSubscriptionId).First(&sub).Error; err != nil {
			return err
		}
		userId = sub.UserId
		if err := tx.Model(&sub).Updates(map[string]interface{}{
			"status":     SubscriptionStatusCancelled,
			"end_time":   now,
			"updated_at": now,
		}).Error; err != nil {
			return err
		}
		target, err := downgradeUserGroupForSubscriptionTx(tx, &sub, now)
		if err != nil {
			return err
		}
		if target != "" {
			cacheGroup = target
			downgradeGroup = target
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if cacheGroup != "" && userId > 0 {
		_ = UpdateUserGroupCache(userId, cacheGroup)
	}
	if downgradeGroup != "" {
		return fmt.Sprintf("用户分组将回退到 %s", downgradeGroup), nil
	}
	return "", nil
}

// AdminDeleteUserSubscription hard-deletes a user subscription.
func AdminDeleteUserSubscription(userSubscriptionId int) (string, error) {
	if userSubscriptionId <= 0 {
		return "", errors.New("invalid userSubscriptionId")
	}
	now := common.GetTimestamp()
	cacheGroup := ""
	downgradeGroup := ""
	var userId int
	err := DB.Transaction(func(tx *gorm.DB) error {
		var sub UserSubscription
		if err := lockForUpdate(tx).
			Where("id = ?", userSubscriptionId).First(&sub).Error; err != nil {
			return err
		}
		userId = sub.UserId
		target, err := downgradeUserGroupForSubscriptionTx(tx, &sub, now)
		if err != nil {
			return err
		}
		if target != "" {
			cacheGroup = target
			downgradeGroup = target
		}
		if err := tx.Where("id = ?", userSubscriptionId).Delete(&UserSubscription{}).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if cacheGroup != "" && userId > 0 {
		_ = UpdateUserGroupCache(userId, cacheGroup)
	}
	if downgradeGroup != "" {
		return fmt.Sprintf("用户分组将回退到 %s", downgradeGroup), nil
	}
	return "", nil
}

func resetUserSubscriptionTx(tx *gorm.DB, sub *UserSubscription, plan *SubscriptionPlan, now int64, advanceResetTime bool) error {
	if tx == nil || sub == nil || plan == nil {
		return errors.New("invalid reset args")
	}
	sub.AmountUsed = 0
	if advanceResetTime {
		nextReset := calcNextResetTime(time.Unix(now, 0), plan, sub.EndTime)
		sub.NextResetTime = nextReset
		if nextReset > 0 {
			sub.LastResetTime = now
		} else {
			sub.LastResetTime = 0
		}
	}
	return tx.Save(sub).Error
}

func buildSubscriptionResetResult(plan *SubscriptionPlan, subs []UserSubscription, advanceResetTime bool) *SubscriptionResetResult {
	userIds := make([]int, 0, len(subs))
	seenUsers := make(map[int]struct{}, len(subs))
	for _, sub := range subs {
		if _, ok := seenUsers[sub.UserId]; ok {
			continue
		}
		seenUsers[sub.UserId] = struct{}{}
		userIds = append(userIds, sub.UserId)
	}
	return &SubscriptionResetResult{
		PlanId:           plan.Id,
		MatchedCount:     len(subs),
		ResetCount:       len(subs),
		UserCount:        len(userIds),
		AdvanceResetTime: advanceResetTime,
		PlanTitle:        plan.Title,
		AffectedUserIds:  userIds,
	}
}

func adminResetUserSubscriptionsByPlanTx(tx *gorm.DB, userId int, plan *SubscriptionPlan, now int64, advanceResetTime bool) (*SubscriptionResetResult, error) {
	if tx == nil || plan == nil {
		return nil, errors.New("invalid reset args")
	}
	var subs []UserSubscription
	if err := lockForUpdate(tx).
		Where("user_id = ? AND plan_id = ? AND status = ? AND end_time > ?", userId, plan.Id, "active", now).
		Order("end_time asc, id asc").
		Find(&subs).Error; err != nil {
		return nil, err
	}
	if len(subs) == 0 {
		return nil, errors.New("该用户没有有效的此套餐订阅")
	}
	for i := range subs {
		if err := resetUserSubscriptionTx(tx, &subs[i], plan, now, advanceResetTime); err != nil {
			return nil, err
		}
	}
	return buildSubscriptionResetResult(plan, subs, advanceResetTime), nil
}

func adminResetPlanSubscriptionsTx(tx *gorm.DB, plan *SubscriptionPlan, now int64, advanceResetTime bool) (*SubscriptionResetResult, error) {
	if tx == nil || plan == nil {
		return nil, errors.New("invalid reset args")
	}
	var subs []UserSubscription
	if err := lockForUpdate(tx).
		Where("plan_id = ? AND status = ? AND end_time > ?", plan.Id, "active", now).
		Order("user_id asc, end_time asc, id asc").
		Find(&subs).Error; err != nil {
		return nil, err
	}
	for i := range subs {
		if err := resetUserSubscriptionTx(tx, &subs[i], plan, now, advanceResetTime); err != nil {
			return nil, err
		}
	}
	return buildSubscriptionResetResult(plan, subs, advanceResetTime), nil
}

func AdminResetUserSubscriptionsByPlan(userId int, planId int, advanceResetTime bool) (*SubscriptionResetResult, error) {
	if userId <= 0 || planId <= 0 {
		return nil, errors.New("invalid userId or planId")
	}
	var result *SubscriptionResetResult
	now := GetDBTimestamp()
	err := DB.Transaction(func(tx *gorm.DB) error {
		plan, err := getSubscriptionPlanByIdTx(tx, planId)
		if err != nil {
			return err
		}
		result, err = adminResetUserSubscriptionsByPlanTx(tx, userId, plan, now, advanceResetTime)
		return err
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func AdminResetPlanSubscriptions(planId int, advanceResetTime bool) (*SubscriptionResetResult, error) {
	if planId <= 0 {
		return nil, errors.New("invalid planId")
	}
	var result *SubscriptionResetResult
	now := GetDBTimestamp()
	err := DB.Transaction(func(tx *gorm.DB) error {
		plan, err := getSubscriptionPlanByIdTx(tx, planId)
		if err != nil {
			return err
		}
		result, err = adminResetPlanSubscriptionsTx(tx, plan, now, advanceResetTime)
		return err
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

type SubscriptionPreConsumeResult struct {
	UserSubscriptionId int
	PreConsumed        int64
	AmountTotal        int64
	AmountUsedBefore   int64
	AmountUsedAfter    int64
}

// ExpireDueSubscriptions marks expired subscriptions and handles group downgrade.
func ExpireDueSubscriptions(limit int) (int, error) {
	if limit <= 0 {
		limit = 200
	}
	now := GetDBTimestamp()
	var subs []UserSubscription
	if err := DB.Where("status IN ? AND end_time > 0 AND end_time <= ?",
		[]string{SubscriptionStatusActive, SubscriptionStatusInactive}, now).
		Order("end_time asc, id asc").
		Limit(limit).
		Find(&subs).Error; err != nil {
		return 0, err
	}
	if len(subs) == 0 {
		return 0, nil
	}
	expiredCount := 0
	userIds := make(map[int]struct{}, len(subs))
	for _, sub := range subs {
		if sub.UserId > 0 {
			userIds[sub.UserId] = struct{}{}
		}
	}
	for userId := range userIds {
		cacheGroup := ""
		err := DB.Transaction(func(tx *gorm.DB) error {
			var dueActive []UserSubscription
			if err := tx.Where("user_id = ? AND status = ? AND end_time > 0 AND end_time <= ?",
				userId, SubscriptionStatusActive, now).
				Order("end_time desc, id desc").
				Find(&dueActive).Error; err != nil {
				return err
			}
			if len(dueActive) > 0 {
				res := tx.Model(&UserSubscription{}).
					Where("user_id = ? AND status = ? AND end_time > 0 AND end_time <= ?", userId, SubscriptionStatusActive, now).
					Updates(map[string]interface{}{
						"status":     SubscriptionStatusExpired,
						"updated_at": common.GetTimestamp(),
					})
				if res.Error != nil {
					return res.Error
				}
				expiredCount += int(res.RowsAffected)
			}

			res := tx.Model(&UserSubscription{}).
				Where("user_id = ? AND status = ? AND end_time > 0 AND end_time <= ?", userId, SubscriptionStatusInactive, now).
				Updates(map[string]interface{}{
					"status":     SubscriptionStatusExpired,
					"updated_at": common.GetTimestamp(),
				})
			if res.Error != nil {
				return res.Error
			}
			expiredCount += int(res.RowsAffected)

			if len(dueActive) == 0 {
				return nil
			}

			for _, sub := range dueActive {
				target, err := downgradeUserGroupForSubscriptionTx(tx, &sub, now)
				if err != nil {
					return err
				}
				if target != "" {
					cacheGroup = target
					break
				}
			}
			return nil
		})
		if err != nil {
			return expiredCount, err
		}
		if cacheGroup != "" {
			_ = UpdateUserGroupCache(userId, cacheGroup)
		}
	}
	return expiredCount, nil
}

// UserSubscriptionTierUsage tracks per-tier usage for a user subscription.
type UserSubscriptionTierUsage struct {
	Id                 int `json:"id"`
	UserSubscriptionId int `json:"user_subscription_id" gorm:"index;uniqueIndex:idx_sub_tier"`
	TierIndex          int `json:"tier_index" gorm:"uniqueIndex:idx_sub_tier"`

	// Calendar-aligned tiers (monthly, weekly, daily)
	PeriodStart     int64 `json:"period_start" gorm:"type:bigint;default:0"`
	NextPeriodStart int64 `json:"next_period_start" gorm:"type:bigint;default:0;index"`
	UsageInPeriod   int64 `json:"usage_in_period" gorm:"type:bigint;default:0"`

	// Sliding window tiers (hourly, custom)
	WindowStart   int64 `json:"window_start" gorm:"type:bigint;default:0"`
	WindowSeconds int64 `json:"window_seconds" gorm:"type:bigint;default:0"`
	UsageInWindow int64 `json:"usage_in_window" gorm:"type:bigint;default:0"`

	CreatedAt int64 `json:"created_at" gorm:"bigint"`
	UpdatedAt int64 `json:"updated_at" gorm:"bigint"`
}

func (t *UserSubscriptionTierUsage) BeforeCreate(tx *gorm.DB) error {
	now := common.GetTimestamp()
	t.CreatedAt = now
	t.UpdatedAt = now
	return nil
}

func (t *UserSubscriptionTierUsage) BeforeUpdate(tx *gorm.DB) error {
	t.UpdatedAt = common.GetTimestamp()
	return nil
}

// SubscriptionPreConsumeRecord stores idempotent pre-consume operations per request.
type SubscriptionPreConsumeRecord struct {
	Id                 int    `json:"id"`
	RequestId          string `json:"request_id" gorm:"type:varchar(64);uniqueIndex"`
	UserId             int    `json:"user_id" gorm:"index"`
	UserSubscriptionId int    `json:"user_subscription_id" gorm:"index"`
	PreConsumed        int64  `json:"pre_consumed" gorm:"type:bigint;not null;default:0"`
	Status             string `json:"status" gorm:"type:varchar(32);index"` // consumed/refunded
	TierUsages         string `json:"tier_usages" gorm:"type:text"`
	CreatedAt          int64  `json:"created_at" gorm:"bigint"`
	UpdatedAt          int64  `json:"updated_at" gorm:"bigint;index"`
}

func (r *SubscriptionPreConsumeRecord) BeforeCreate(tx *gorm.DB) error {
	now := common.GetTimestamp()
	r.CreatedAt = now
	r.UpdatedAt = now
	return nil
}

func (r *SubscriptionPreConsumeRecord) BeforeUpdate(tx *gorm.DB) error {
	r.UpdatedAt = common.GetTimestamp()
	return nil
}

func maybeResetUserSubscriptionWithPlanTx(tx *gorm.DB, sub *UserSubscription, plan *SubscriptionPlan, now int64) error {
	if tx == nil || sub == nil || plan == nil {
		return errors.New("invalid reset args")
	}
	if sub.NextResetTime > 0 && sub.NextResetTime > now {
		return nil
	}
	if NormalizeResetPeriod(plan.QuotaResetPeriod) == SubscriptionResetNever {
		return nil
	}
	baseUnix := sub.LastResetTime
	if baseUnix <= 0 {
		baseUnix = sub.StartTime
	}
	base := time.Unix(baseUnix, 0)
	next := calcNextResetTime(base, plan, sub.EndTime)
	advanced := false
	for next > 0 && next <= now {
		advanced = true
		base = time.Unix(next, 0)
		next = calcNextResetTime(base, plan, sub.EndTime)
	}
	if !advanced {
		if sub.NextResetTime == 0 && next > 0 {
			sub.NextResetTime = next
			sub.LastResetTime = base.Unix()
			return tx.Save(sub).Error
		}
		return nil
	}
	sub.AmountUsed = 0
	sub.LastResetTime = base.Unix()
	sub.NextResetTime = next
	return tx.Save(sub).Error
}

func subscriptionMatchesUsingGroup(upgradeGroup string, usingGroup string) bool {
	upgradeGroup = strings.TrimSpace(upgradeGroup)
	usingGroup = strings.TrimSpace(usingGroup)
	if upgradeGroup == "" {
		return true
	}
	if usingGroup == "" {
		return false
	}
	if upgradeGroup == usingGroup {
		return true
	}
	// 分组层级：请求分组已包含订阅绑定的分组时（例如 svip 请求消耗 vip 订阅），
	// 高级分组可以使用为其覆盖范围内低级分组购买的订阅额度；反向不成立。
	return ratio_setting.GroupSpecialGrantsGroup(usingGroup, upgradeGroup)
}

// subscriptionUpgradeGroupsForUsingGroup returns the upgrade_group values a
// request in usingGroup may consume: the group itself plus every group its
// special usable-group configuration covers. SQL filters using this list stay
// consistent with subscriptionMatchesUsingGroup.
func subscriptionUpgradeGroupsForUsingGroup(usingGroup string) []string {
	usingGroup = strings.TrimSpace(usingGroup)
	groups := []string{usingGroup}
	for _, granted := range ratio_setting.GroupSpecialGrantedGroups(usingGroup) {
		if granted != usingGroup {
			groups = append(groups, granted)
		}
	}
	return groups
}

// PreConsumeUserSubscription pre-consumes from an active subscription that matches
// the current using group. Group-bound subscriptions only apply to the same group;
// subscriptions without UpgradeGroup remain globally usable.
func PreConsumeUserSubscription(requestId string, userId int, modelName string, quotaType int, amount int64, usingGroup string) (*SubscriptionPreConsumeResult, error) {
	if userId <= 0 {
		return nil, errors.New("invalid userId")
	}
	if strings.TrimSpace(requestId) == "" {
		return nil, errors.New("requestId is empty")
	}
	if amount <= 0 {
		return nil, errors.New("amount must be > 0")
	}
	now := GetDBTimestamp()

	returnValue := &SubscriptionPreConsumeResult{}

	err := DB.Transaction(func(tx *gorm.DB) error {
		var existing SubscriptionPreConsumeRecord
		query := tx.Where("request_id = ?", requestId).Limit(1).Find(&existing)
		if query.Error != nil {
			return query.Error
		}
		if query.RowsAffected > 0 {
			if existing.Status == "refunded" {
				return errors.New("subscription pre-consume already refunded")
			}
			var sub UserSubscription
			if err := tx.Where("id = ?", existing.UserSubscriptionId).First(&sub).Error; err != nil {
				return err
			}
			returnValue.UserSubscriptionId = sub.Id
			returnValue.PreConsumed = existing.PreConsumed
			returnValue.AmountTotal = sub.AmountTotal
			returnValue.AmountUsedBefore = sub.AmountUsed
			returnValue.AmountUsedAfter = sub.AmountUsed
			return nil
		}

		var subs []UserSubscription
		if err := lockForUpdate(tx).
			Where("user_id = ? AND status = ? AND end_time > ?", userId, SubscriptionStatusActive, now).
			Order("end_time asc, id asc").
			Find(&subs).Error; err != nil {
			return errors.New("no active subscription")
		}
		if len(subs) == 0 {
			return errors.New("no active subscription")
		}
		for _, candidate := range subs {
			sub := candidate
			if !subscriptionMatchesUsingGroup(sub.UpgradeGroup, usingGroup) {
				continue
			}
			plan, err := getSubscriptionPlanByIdTx(tx, sub.PlanId)
			if err != nil {
				return err
			}
			if err := maybeResetUserSubscriptionWithPlanTx(tx, &sub, plan, now); err != nil {
				return err
			}

			tiers := plan.GetQuotaTiers()
			if len(tiers) > 0 {
				usedBefore := sub.AmountUsed
				// Also check legacy total amount if configured
				if sub.AmountTotal > 0 {
					remain := sub.AmountTotal - usedBefore
					if remain < amount {
						continue
					}
				}
				// Multi-tier path: check all tier limits
				if err := checkTierLimits(tx, sub.Id, tiers, amount, now); err != nil {
					continue // tier limits exceeded, try next subscription
				}
				record := &SubscriptionPreConsumeRecord{
					RequestId:          requestId,
					UserId:             userId,
					UserSubscriptionId: sub.Id,
					PreConsumed:        amount,
					Status:             "consumed",
				}
				if err := tx.Create(record).Error; err != nil {
					var dup SubscriptionPreConsumeRecord
					if err2 := tx.Where("request_id = ?", requestId).First(&dup).Error; err2 == nil {
						if dup.Status == "refunded" {
							return errors.New("subscription pre-consume already refunded")
						}
						returnValue.UserSubscriptionId = sub.Id
						returnValue.PreConsumed = dup.PreConsumed
						returnValue.AmountTotal = sub.AmountTotal
						returnValue.AmountUsedBefore = sub.AmountUsed
						returnValue.AmountUsedAfter = sub.AmountUsed
						return nil
					}
					return err
				}
				// Increment all tier usages and get snapshot after the idempotency row exists.
				tierSnapshot, err := incrementTierUsage(tx, sub.Id, tiers, amount, now)
				if err != nil {
					return err
				}
				if tierSnapshot != "" {
					record.TierUsages = tierSnapshot
					if err := tx.Model(record).Update("tier_usages", tierSnapshot).Error; err != nil {
						return err
					}
				}
				sub.AmountUsed += amount
				if err := tx.Save(&sub).Error; err != nil {
					return err
				}
				returnValue.UserSubscriptionId = sub.Id
				returnValue.PreConsumed = amount
				returnValue.AmountTotal = sub.AmountTotal
				returnValue.AmountUsedBefore = usedBefore
				returnValue.AmountUsedAfter = sub.AmountUsed
				return nil
			}

			// Legacy single-tier path
			usedBefore := sub.AmountUsed
			if sub.AmountTotal > 0 {
				remain := sub.AmountTotal - usedBefore
				if remain < amount {
					continue
				}
			}
			record := &SubscriptionPreConsumeRecord{
				RequestId:          requestId,
				UserId:             userId,
				UserSubscriptionId: sub.Id,
				PreConsumed:        amount,
				Status:             "consumed",
			}
			if err := tx.Create(record).Error; err != nil {
				var dup SubscriptionPreConsumeRecord
				if err2 := tx.Where("request_id = ?", requestId).First(&dup).Error; err2 == nil {
					if dup.Status == "refunded" {
						return errors.New("subscription pre-consume already refunded")
					}
					returnValue.UserSubscriptionId = sub.Id
					returnValue.PreConsumed = dup.PreConsumed
					returnValue.AmountTotal = sub.AmountTotal
					returnValue.AmountUsedBefore = sub.AmountUsed
					returnValue.AmountUsedAfter = sub.AmountUsed
					return nil
				}
				return err
			}
			sub.AmountUsed += amount
			if err := tx.Save(&sub).Error; err != nil {
				return err
			}
			returnValue.UserSubscriptionId = sub.Id
			returnValue.PreConsumed = amount
			returnValue.AmountTotal = sub.AmountTotal
			returnValue.AmountUsedBefore = usedBefore
			returnValue.AmountUsedAfter = sub.AmountUsed
			return nil
		}
		return fmt.Errorf("subscription quota insufficient, need=%d", amount)
	})
	if err != nil {
		return nil, err
	}
	return returnValue, nil
}

// RefundSubscriptionPreConsume is idempotent and refunds pre-consumed subscription quota by requestId.
func RefundSubscriptionPreConsume(requestId string) error {
	if strings.TrimSpace(requestId) == "" {
		return errors.New("requestId is empty")
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var record SubscriptionPreConsumeRecord
		if err := lockForUpdate(tx).
			Where("request_id = ?", requestId).First(&record).Error; err != nil {
			return err
		}
		if record.Status == "refunded" {
			return nil
		}
		if record.PreConsumed <= 0 {
			record.Status = "refunded"
			return tx.Save(&record).Error
		}
		if err := postConsumeUserSubscriptionDeltaTx(tx, record.UserSubscriptionId, -record.PreConsumed, false); err != nil {
			return err
		}
		// Refund tier usages if multi-tier was used
		if record.TierUsages != "" {
			var sub UserSubscription
			if err := tx.Where("id = ?", record.UserSubscriptionId).First(&sub).Error; err == nil {
				if plan, err := getSubscriptionPlanByIdTx(tx, sub.PlanId); err == nil && plan.HasMultiTier() {
					_ = refundTierUsage(tx, record.UserSubscriptionId, record.TierUsages)
				}
			}
		}
		record.Status = "refunded"
		return tx.Save(&record).Error
	})
}

// ResetDueSubscriptions resets subscriptions whose next_reset_time has passed.
func ResetDueSubscriptions(limit int) (int, error) {
	if limit <= 0 {
		limit = 200
	}
	now := GetDBTimestamp()
	var subs []UserSubscription
	if err := DB.Where("next_reset_time > 0 AND next_reset_time <= ? AND status IN ?", now, []string{SubscriptionStatusActive, SubscriptionStatusInactive}).
		Order("next_reset_time asc").
		Limit(limit).
		Find(&subs).Error; err != nil {
		return 0, err
	}
	if len(subs) == 0 {
		return 0, nil
	}
	resetCount := 0
	for _, sub := range subs {
		subCopy := sub
		plan, err := getSubscriptionPlanByIdTx(nil, sub.PlanId)
		if err != nil || plan == nil {
			continue
		}
		err = DB.Transaction(func(tx *gorm.DB) error {
			var locked UserSubscription
			if err := lockForUpdate(tx).
				Where("id = ? AND next_reset_time > 0 AND next_reset_time <= ? AND status IN ?", subCopy.Id, now, []string{SubscriptionStatusActive, SubscriptionStatusInactive}).
				First(&locked).Error; err != nil {
				return nil
			}
			if err := maybeResetUserSubscriptionWithPlanTx(tx, &locked, plan, now); err != nil {
				return err
			}
			resetCount++
			return nil
		})
		if err != nil {
			return resetCount, err
		}
	}
	return resetCount, nil
}

// CleanupSubscriptionPreConsumeRecords removes old idempotency records to keep table small.
func CleanupSubscriptionPreConsumeRecords(olderThanSeconds int64) (int64, error) {
	if olderThanSeconds <= 0 {
		olderThanSeconds = 7 * 24 * 3600
	}
	cutoff := GetDBTimestamp() - olderThanSeconds
	res := DB.Where("updated_at < ?", cutoff).Delete(&SubscriptionPreConsumeRecord{})
	return res.RowsAffected, res.Error
}

type SubscriptionPlanInfo struct {
	PlanId    int
	PlanTitle string
}

func GetSubscriptionPlanInfoByUserSubscriptionId(userSubscriptionId int) (*SubscriptionPlanInfo, error) {
	if userSubscriptionId <= 0 {
		return nil, errors.New("invalid userSubscriptionId")
	}
	cacheKey := fmt.Sprintf("sub:%d", userSubscriptionId)
	if cached, found, err := getSubscriptionPlanInfoCache().Get(cacheKey); err == nil && found {
		return &cached, nil
	}
	var sub UserSubscription
	if err := DB.Where("id = ?", userSubscriptionId).First(&sub).Error; err != nil {
		return nil, err
	}
	plan, err := getSubscriptionPlanByIdTx(nil, sub.PlanId)
	if err != nil {
		return nil, err
	}
	info := &SubscriptionPlanInfo{
		PlanId:    sub.PlanId,
		PlanTitle: plan.Title,
	}
	_ = getSubscriptionPlanInfoCache().SetWithTTL(cacheKey, *info, subscriptionPlanInfoCacheTTL())
	return info, nil
}

// Update subscription used amount by delta (positive consume more, negative refund).
func PostConsumeUserSubscriptionDelta(userSubscriptionId int, delta int64) error {
	if userSubscriptionId <= 0 {
		return errors.New("invalid userSubscriptionId")
	}
	if delta == 0 {
		return nil
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		return postConsumeUserSubscriptionDeltaTx(tx, userSubscriptionId, delta, true)
	})
}

func postConsumeUserSubscriptionDeltaTx(tx *gorm.DB, userSubscriptionId int, delta int64, adjustTiers bool) error {
	if tx == nil {
		return errors.New("tx is nil")
	}
	var sub UserSubscription
	if err := lockForUpdate(tx).
		Where("id = ?", userSubscriptionId).
		First(&sub).Error; err != nil {
		return err
	}
	now := getDBTimestampTx(tx)
	var plan *SubscriptionPlan
	if sub.PlanId > 0 {
		var err error
		plan, err = getSubscriptionPlanByIdTx(tx, sub.PlanId)
		if err != nil {
			return err
		}
		if err := maybeResetUserSubscriptionWithPlanTx(tx, &sub, plan, now); err != nil {
			return err
		}
	}
	newUsed := sub.AmountUsed + delta
	if newUsed < 0 {
		newUsed = 0
	}
	if sub.AmountTotal > 0 && newUsed > sub.AmountTotal {
		return fmt.Errorf("subscription used exceeds total, used=%d total=%d", newUsed, sub.AmountTotal)
	}
	if adjustTiers && plan != nil {
		tiers := plan.GetQuotaTiers()
		if len(tiers) > 0 {
			if delta > 0 {
				if err := checkTierLimits(tx, sub.Id, tiers, delta, now); err != nil {
					return err
				}
				if _, err := incrementTierUsage(tx, sub.Id, tiers, delta, now); err != nil {
					return err
				}
			} else if delta < 0 {
				if err := decrementTierUsageAmount(tx, sub.Id, tiers, -delta); err != nil {
					return err
				}
			}
		}
	}
	sub.AmountUsed = newUsed
	return tx.Save(&sub).Error
}

// ---------------------------------------------------------------------------
// Multi-Tier Quota Logic
// ---------------------------------------------------------------------------

// GetQuotaTiers parses the QuotaTiers JSON and returns sorted tiers
func (p *SubscriptionPlan) GetQuotaTiers() []QuotaTier {
	if p == nil || strings.TrimSpace(p.QuotaTiers) == "" || p.QuotaTiers == "[]" {
		return nil
	}
	var tiers []QuotaTier
	if err := common.Unmarshal([]byte(p.QuotaTiers), &tiers); err != nil {
		return nil
	}
	if len(tiers) == 0 {
		return nil
	}
	// Sort by SortPriority ascending (shortest period first)
	for i := 0; i < len(tiers)-1; i++ {
		for j := i + 1; j < len(tiers); j++ {
			if tiers[i].SortPriority > tiers[j].SortPriority {
				tiers[i], tiers[j] = tiers[j], tiers[i]
			}
		}
	}
	return tiers
}

// HasMultiTier returns true if the plan has multi-tier quota configured
func (p *SubscriptionPlan) HasMultiTier() bool {
	return len(p.GetQuotaTiers()) > 0
}

// NormalizeTierPeriod normalizes a tier period string
func NormalizeTierPeriod(period string) string {
	switch strings.TrimSpace(period) {
	case TierPeriodMonthly, TierPeriodWeekly, TierPeriodDaily,
		TierPeriodHourly, TierPeriodCustom, TierPeriodNone:
		return strings.TrimSpace(period)
	default:
		return TierPeriodNone
	}
}

// calcTierPeriodBoundaries calculates the calendar-aligned period boundaries
func calcTierPeriodBoundaries(now time.Time, period string) (start, end time.Time) {
	switch period {
	case TierPeriodMonthly:
		start = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
		end = start.AddDate(0, 1, 0)
	case TierPeriodWeekly:
		weekday := int(now.Weekday())
		if weekday == 0 {
			weekday = 7
		}
		start = time.Date(now.Year(), now.Month(), now.Day()-weekday+1, 0, 0, 0, 0, now.Location())
		end = start.AddDate(0, 0, 7)
	case TierPeriodDaily:
		start = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		end = start.AddDate(0, 0, 1)
	default:
		start = now
		end = now
	}
	return start, end
}

// getSlidingWindowSeconds returns the window duration in seconds for a sliding tier
func getSlidingWindowSeconds(tier QuotaTier) int64 {
	switch tier.Period {
	case TierPeriodHourly:
		return 3600
	case TierPeriodCustom:
		if tier.CustomSeconds > 0 {
			return tier.CustomSeconds
		}
		return 3600 // fallback
	default:
		return 0
	}
}

// isSlidingWindow returns true if the tier uses a sliding window
func isSlidingWindow(tier QuotaTier) bool {
	return tier.Period == TierPeriodHourly || tier.Period == TierPeriodCustom
}

// isCalendarAligned returns true if the tier uses calendar-aligned periods
func isCalendarAligned(tier QuotaTier) bool {
	return tier.Period == TierPeriodMonthly || tier.Period == TierPeriodWeekly || tier.Period == TierPeriodDaily
}

// initTierUsage creates UserSubscriptionTierUsage rows for a new subscription
func initTierUsage(tx *gorm.DB, subId int, tiers []QuotaTier, now int64) error {
	if tx == nil || len(tiers) == 0 {
		return nil
	}
	nowTime := time.Unix(now, 0)
	for i, tier := range tiers {
		usage := UserSubscriptionTierUsage{
			UserSubscriptionId: subId,
			TierIndex:          i,
		}
		if isCalendarAligned(tier) {
			start, end := calcTierPeriodBoundaries(nowTime, tier.Period)
			usage.PeriodStart = start.Unix()
			usage.NextPeriodStart = end.Unix()
		} else if isSlidingWindow(tier) {
			usage.WindowSeconds = getSlidingWindowSeconds(tier)
			// WindowStart = 0 until first API call
		}
		// TierPeriodNone: no period fields needed
		if err := tx.Create(&usage).Error; err != nil {
			return err
		}
	}
	return nil
}

// checkTierLimits checks all tier limits for a subscription. Returns error if any tier is exceeded.
func checkTierLimits(tx *gorm.DB, subId int, tiers []QuotaTier, amount int64, now int64) error {
	if tx == nil || len(tiers) == 0 {
		return nil
	}
	nowTime := time.Unix(now, 0)
	for i, tier := range tiers {
		if tier.Limit <= 0 {
			continue // no limit at this tier
		}
		var usage UserSubscriptionTierUsage
		if err := lockForUpdate(tx).
			Where("user_subscription_id = ? AND tier_index = ?", subId, i).
			First(&usage).Error; err != nil {
			return fmt.Errorf("tier usage not found for sub %d tier %d: %w", subId, i, err)
		}

		var currentUsage int64
		if isCalendarAligned(tier) {
			// Check if period rolled over
			if now >= usage.NextPeriodStart && usage.NextPeriodStart > 0 {
				// Period expired, reset
				start, end := calcTierPeriodBoundaries(nowTime, tier.Period)
				usage.PeriodStart = start.Unix()
				usage.NextPeriodStart = end.Unix()
				usage.UsageInPeriod = 0
				if err := tx.Save(&usage).Error; err != nil {
					return err
				}
			}
			currentUsage = usage.UsageInPeriod
		} else if isSlidingWindow(tier) {
			windowSec := getSlidingWindowSeconds(tier)
			if usage.WindowStart == 0 {
				// First use in this window — start the window
				usage.WindowStart = now
				usage.UsageInWindow = 0
				if err := tx.Save(&usage).Error; err != nil {
					return err
				}
			} else if now >= usage.WindowStart+windowSec {
				// Window expired, reset
				usage.WindowStart = now
				usage.UsageInWindow = 0
				if err := tx.Save(&usage).Error; err != nil {
					return err
				}
			}
			currentUsage = usage.UsageInWindow
		} else {
			// TierPeriodNone — no periodic reset
			currentUsage = usage.UsageInWindow + usage.UsageInPeriod
		}

		if currentUsage+amount > tier.Limit {
			return fmt.Errorf("tier %d limit exceeded: current=%d, need=%d, limit=%d", i, currentUsage, amount, tier.Limit)
		}
	}
	return nil
}

// incrementTierUsage atomically increments all tier usage counters and returns a JSON snapshot
func incrementTierUsage(tx *gorm.DB, subId int, tiers []QuotaTier, amount int64, now int64) (string, error) {
	if tx == nil || len(tiers) == 0 {
		return "", nil
	}
	snapshots := make([]TierUsageSnapshot, 0, len(tiers))
	for i, tier := range tiers {
		if tier.Limit <= 0 {
			continue
		}
		var usage UserSubscriptionTierUsage
		if err := lockForUpdate(tx).
			Where("user_subscription_id = ? AND tier_index = ?", subId, i).
			First(&usage).Error; err != nil {
			return "", err
		}
		if isCalendarAligned(tier) {
			usage.UsageInPeriod += amount
		} else if isSlidingWindow(tier) || tier.Period == TierPeriodNone {
			usage.UsageInWindow += amount
		}
		if err := tx.Save(&usage).Error; err != nil {
			return "", err
		}
		snapshots = append(snapshots, TierUsageSnapshot{TierIndex: i, Delta: amount})
	}
	if len(snapshots) == 0 {
		return "", nil
	}
	data, err := common.Marshal(snapshots)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// refundTierUsage decrements tier usage based on the snapshot from pre-consume
func refundTierUsage(tx *gorm.DB, subId int, tierUsagesJSON string) error {
	if tx == nil || tierUsagesJSON == "" {
		return nil
	}
	var snapshots []TierUsageSnapshot
	if err := common.Unmarshal([]byte(tierUsagesJSON), &snapshots); err != nil {
		return err
	}
	for _, snap := range snapshots {
		if snap.Delta <= 0 {
			continue
		}
		var usage UserSubscriptionTierUsage
		if err := lockForUpdate(tx).
			Where("user_subscription_id = ? AND tier_index = ?", subId, snap.TierIndex).
			First(&usage).Error; err != nil {
			continue // tier usage row not found, skip
		}
		// Decrement both fields (one will be zero for the non-applicable type)
		usage.UsageInPeriod -= snap.Delta
		if usage.UsageInPeriod < 0 {
			usage.UsageInPeriod = 0
		}
		usage.UsageInWindow -= snap.Delta
		if usage.UsageInWindow < 0 {
			usage.UsageInWindow = 0
		}
		if err := tx.Save(&usage).Error; err != nil {
			return err
		}
	}
	return nil
}

func decrementTierUsageAmount(tx *gorm.DB, subId int, tiers []QuotaTier, amount int64) error {
	if tx == nil || subId <= 0 || amount <= 0 || len(tiers) == 0 {
		return nil
	}
	for i, tier := range tiers {
		if tier.Limit <= 0 {
			continue
		}
		var usage UserSubscriptionTierUsage
		if err := lockForUpdate(tx).
			Where("user_subscription_id = ? AND tier_index = ?", subId, i).
			First(&usage).Error; err != nil {
			return err
		}
		if isCalendarAligned(tier) {
			usage.UsageInPeriod -= amount
			if usage.UsageInPeriod < 0 {
				usage.UsageInPeriod = 0
			}
		} else if isSlidingWindow(tier) || tier.Period == TierPeriodNone {
			usage.UsageInWindow -= amount
			if usage.UsageInWindow < 0 {
				usage.UsageInWindow = 0
			}
		}
		if err := tx.Save(&usage).Error; err != nil {
			return err
		}
	}
	return nil
}

// ResetExpiredCalendarTiers resets calendar-aligned tier usages whose periods have rolled over
func ResetExpiredCalendarTiers(limit int) (int, error) {
	if limit <= 0 {
		limit = 200
	}
	now := GetDBTimestamp()
	var usages []UserSubscriptionTierUsage
	if err := DB.Where("next_period_start > 0 AND next_period_start <= ?", now).
		Order("next_period_start asc").
		Limit(limit).
		Find(&usages).Error; err != nil {
		return 0, err
	}
	if len(usages) == 0 {
		return 0, nil
	}
	resetCount := 0
	for _, u := range usages {
		uCopy := u
		// Verify parent subscription is still active
		var sub UserSubscription
		if err := DB.Where("id = ? AND status = ? AND end_time > ?", uCopy.UserSubscriptionId, "active", now).First(&sub).Error; err != nil {
			continue
		}
		// Get the plan to know the tier period type
		plan, err := getSubscriptionPlanByIdTx(nil, sub.PlanId)
		if err != nil || plan == nil {
			continue
		}
		tiers := plan.GetQuotaTiers()
		if uCopy.TierIndex >= len(tiers) {
			continue
		}
		tier := tiers[uCopy.TierIndex]
		if !isCalendarAligned(tier) {
			continue
		}
		err = DB.Transaction(func(tx *gorm.DB) error {
			var locked UserSubscriptionTierUsage
			if err := lockForUpdate(tx).
				Where("id = ? AND next_period_start > 0 AND next_period_start <= ?", uCopy.Id, now).
				First(&locked).Error; err != nil {
				return nil
			}
			nowTime := time.Unix(now, 0)
			start, end := calcTierPeriodBoundaries(nowTime, tier.Period)
			locked.PeriodStart = start.Unix()
			locked.NextPeriodStart = end.Unix()
			locked.UsageInPeriod = 0
			return tx.Save(&locked).Error
		})
		if err == nil {
			resetCount++
		}
	}
	return resetCount, nil
}

// ResetExpiredTierWindows resets sliding window tier usages whose windows have expired
func ResetExpiredTierWindows(limit int) (int, error) {
	if limit <= 0 {
		limit = 200
	}
	now := GetDBTimestamp()
	// Find tier usages with active windows that have expired
	// window_start > 0 AND window_start + window_seconds <= now
	var usages []UserSubscriptionTierUsage
	if err := DB.Where("window_start > 0 AND window_seconds > 0 AND window_start + window_seconds <= ?", now).
		Order("window_start asc").
		Limit(limit).
		Find(&usages).Error; err != nil {
		return 0, err
	}
	if len(usages) == 0 {
		return 0, nil
	}
	resetCount := 0
	for _, u := range usages {
		uCopy := u
		// Verify parent subscription is still active
		var sub UserSubscription
		if err := DB.Where("id = ? AND status = ? AND end_time > ?", uCopy.UserSubscriptionId, "active", now).First(&sub).Error; err != nil {
			continue
		}
		err := DB.Transaction(func(tx *gorm.DB) error {
			var locked UserSubscriptionTierUsage
			if err := lockForUpdate(tx).
				Where("id = ?", uCopy.Id).
				First(&locked).Error; err != nil {
				return nil
			}
			// Re-check window expiry under lock
			if locked.WindowStart <= 0 || locked.WindowSeconds <= 0 {
				return nil
			}
			if now < locked.WindowStart+locked.WindowSeconds {
				return nil
			}
			locked.WindowStart = 0
			locked.UsageInWindow = 0
			return tx.Save(&locked).Error
		})
		if err == nil {
			resetCount++
		}
	}
	return resetCount, nil
}

// HasDisableBalanceDeductionSubscription checks if a user has any active subscription
// with DisableBalanceDeduction enabled
func HasDisableBalanceDeductionSubscription(userId int) (bool, error) {
	if userId <= 0 {
		return false, nil
	}
	now := GetDBTimestamp()
	var count int64
	err := ReadDB().Model(&UserSubscription{}).
		Joins("JOIN subscription_plans ON subscription_plans.id = user_subscriptions.plan_id").
		Where("user_subscriptions.user_id = ? AND user_subscriptions.status = ? AND user_subscriptions.end_time > ? AND subscription_plans.disable_balance_deduction = ?",
			userId, "active", now, true).
		Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// GetUserAttachedGroups returns the extra groups granted to the user by live
// attach-mode subscriptions (追加分组).
func GetUserAttachedGroups(userId int) ([]string, error) {
	if userId <= 0 {
		return nil, nil
	}
	now := GetDBTimestamp()
	var groups []string
	if err := ReadDB().Model(&UserSubscription{}).
		Distinct("upgrade_group").
		Where("user_id = ? AND status = ? AND end_time > ? AND group_mode = ? AND upgrade_group <> ''",
			userId, SubscriptionStatusActive, now, SubscriptionGroupModeAttach).
		Pluck("upgrade_group", &groups).Error; err != nil {
		return nil, err
	}
	return groups, nil
}

// UserHasAttachedGroup reports whether a live attach-mode subscription grants
// the group to the user. Query errors deny access and are logged.
func UserHasAttachedGroup(userId int, group string) bool {
	group = strings.TrimSpace(group)
	if userId <= 0 || group == "" {
		return false
	}
	now := GetDBTimestamp()
	var count int64
	if err := ReadDB().Model(&UserSubscription{}).
		Where("user_id = ? AND status = ? AND end_time > ? AND group_mode = ? AND upgrade_group = ?",
			userId, SubscriptionStatusActive, now, SubscriptionGroupModeAttach, group).
		Count(&count).Error; err != nil {
		common.SysError("failed to check attached group: " + err.Error())
		return false
	}
	return count > 0
}

// HasActiveUserSubscriptionForUsingGroup checks whether the user has any active
// subscription usable by the current request group.
func HasActiveUserSubscriptionForUsingGroup(userId int, usingGroup string) (bool, error) {
	if userId <= 0 {
		return false, nil
	}
	usingGroup = strings.TrimSpace(usingGroup)
	if usingGroup == "" {
		return HasActiveUserSubscription(userId)
	}
	now := GetDBTimestamp()
	var count int64
	err := ReadDB().Model(&UserSubscription{}).
		Where("user_id = ? AND status = ? AND end_time > ? AND (upgrade_group = '' OR upgrade_group IN ?)",
			userId, "active", now, subscriptionUpgradeGroupsForUsingGroup(usingGroup)).
		Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// HasDisableBalanceDeductionSubscriptionForUsingGroup checks whether the current
// request group is bound to a subscription that forbids wallet fallback. Generic
// subscriptions (without UpgradeGroup) still apply to all groups.
func HasDisableBalanceDeductionSubscriptionForUsingGroup(userId int, usingGroup string) (bool, error) {
	if userId <= 0 {
		return false, nil
	}
	usingGroup = strings.TrimSpace(usingGroup)
	if usingGroup == "" {
		return HasDisableBalanceDeductionSubscription(userId)
	}
	now := GetDBTimestamp()
	var count int64
	err := ReadDB().Model(&UserSubscription{}).
		Joins("JOIN subscription_plans ON subscription_plans.id = user_subscriptions.plan_id").
		Where("user_subscriptions.user_id = ? AND user_subscriptions.status = ? AND user_subscriptions.end_time > ? AND subscription_plans.disable_balance_deduction = ? AND (user_subscriptions.upgrade_group = '' OR user_subscriptions.upgrade_group IN ?)",
			userId, "active", now, true, subscriptionUpgradeGroupsForUsingGroup(usingGroup)).
		Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// HasGlobalDisableBalanceDeductionSubscription checks whether the user has an
// active subscription that forbids wallet fallback for every group.
func HasGlobalDisableBalanceDeductionSubscription(userId int) (bool, error) {
	if userId <= 0 {
		return false, nil
	}
	now := GetDBTimestamp()
	var count int64
	err := ReadDB().Model(&UserSubscription{}).
		Joins("JOIN subscription_plans ON subscription_plans.id = user_subscriptions.plan_id").
		Where("user_subscriptions.user_id = ? AND user_subscriptions.status = ? AND user_subscriptions.end_time > ? AND subscription_plans.disable_balance_deduction = ? AND user_subscriptions.upgrade_group = ''",
			userId, "active", now, true).
		Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// GetUserSubscriptionTierUsages returns tier usage info for a subscription
func GetUserSubscriptionTierUsages(subId int) ([]UserSubscriptionTierUsage, error) {
	if subId <= 0 {
		return nil, nil
	}
	var usages []UserSubscriptionTierUsage
	if err := ReadDB().Where("user_subscription_id = ?", subId).Order("tier_index asc").Find(&usages).Error; err != nil {
		return nil, err
	}
	return usages, nil
}

// MigrateExistingPlansToTiers converts old single-tier plans to QuotaTiers format (one-time migration)
func MigrateExistingPlansToTiers() error {
	var plans []SubscriptionPlan
	if err := DB.Where("quota_tiers IS NULL OR quota_tiers = '' OR quota_tiers = '[]'").Find(&plans).Error; err != nil {
		return err
	}
	for _, plan := range plans {
		if plan.TotalAmount <= 0 {
			continue
		}
		period := NormalizeResetPeriod(plan.QuotaResetPeriod)
		tierPeriod := TierPeriodNone
		switch period {
		case SubscriptionResetMonthly:
			tierPeriod = TierPeriodMonthly
		case SubscriptionResetWeekly:
			tierPeriod = TierPeriodWeekly
		case SubscriptionResetDaily:
			tierPeriod = TierPeriodDaily
		case SubscriptionResetCustom:
			tierPeriod = TierPeriodCustom
		}
		tier := QuotaTier{
			Period:       tierPeriod,
			Limit:        plan.TotalAmount,
			SortPriority: 100,
		}
		if tierPeriod == TierPeriodCustom && plan.QuotaResetCustomSeconds > 0 {
			tier.CustomSeconds = plan.QuotaResetCustomSeconds
		}
		data, err := common.Marshal([]QuotaTier{tier})
		if err != nil {
			return err
		}
		if err := DB.Model(&SubscriptionPlan{}).Where("id = ?", plan.Id).Update("quota_tiers", string(data)).Error; err != nil {
			return err
		}
	}
	return nil
}
