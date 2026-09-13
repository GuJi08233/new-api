package model

import (
	"errors"
	"fmt"
	"math/big"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/setting"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

type TopUp struct {
	Id                    int     `json:"id"`
	UserId                int     `json:"user_id" gorm:"index"`
	Amount                int64   `json:"amount"`
	Money                 float64 `json:"money"`
	TradeNo               string  `json:"trade_no" gorm:"unique;type:varchar(255);index"`
	PaymentMethod         string  `json:"payment_method" gorm:"type:varchar(50)"`
	PaymentProvider       string  `json:"payment_provider" gorm:"type:varchar(50);default:''"`
	ExpectedPaymentToken  string  `json:"expected_payment_token" gorm:"type:varchar(64);default:''"`
	ExpectedPaymentAmount string  `json:"expected_payment_amount" gorm:"type:varchar(128);default:''"`
	CreateTime            int64   `json:"create_time"`
	CompleteTime          int64   `json:"complete_time"`
	Status                string  `json:"status"`
}

const (
	PaymentMethodStripe       = "stripe"
	PaymentMethodCreem        = "creem"
	PaymentMethodWaffo        = "waffo"
	PaymentMethodWaffoPancake = "waffo_pancake"
	PaymentMethodBalance      = "balance"
)

const (
	PaymentProviderEpay         = "epay"
	PaymentProviderStripe       = "stripe"
	PaymentProviderCreem        = "creem"
	PaymentProviderWaffo        = "waffo"
	PaymentProviderWaffoPancake = "waffo_pancake"
	PaymentProviderEthereum     = "ethereum"
	PaymentProviderBalance      = "balance"
)

var (
	ErrPaymentMethodMismatch = errors.New("payment method mismatch")
	ErrTopUpNotFound         = errors.New("topup not found")
	ErrTopUpStatusInvalid    = errors.New("topup status invalid")
	ErrPaymentAmountMismatch = errors.New("payment amount mismatch")
	ErrTopUpExpired          = errors.New("topup order expired")
	// ErrPaymentSettlementRetryable wraps failures that a later redelivery of the
	// same payment callback may resolve, such as a database outage. Callers use
	// it to fail the webhook so the provider retries instead of dropping a
	// payment that already happened.
	ErrPaymentSettlementRetryable = errors.New("payment settlement failed, retry later")
)

// ChainPayment is what an on-chain PaymentReceived event proved was paid.
type ChainPayment struct {
	Token  string
	Amount string
	TxHash string
}

func (p *ChainPayment) present() bool {
	return p != nil && (strings.TrimSpace(p.Token) != "" || strings.TrimSpace(p.Amount) != "")
}

// ChainOrderTTLSeconds bounds how long an on-chain order's quote stays valid.
//
// ExpectedPaymentAmount is frozen at order creation from the operator's manually
// configured token price. Without an expiry, a payer can stockpile unpaid orders
// at a favourable rate and settle them after the operator repricing, buying the
// same quota for a fraction of its current value. The window must still cover a
// realistic wallet round trip: signature, block inclusion, and webhook delivery.
func ChainOrderTTLSeconds() int64 {
	seconds := int64(common.GetEnvOrDefault("ETHEREUM_ORDER_TTL_SECONDS", 1800))
	if seconds <= 0 {
		seconds = 1800
	}
	return seconds
}

// ExpiresAt is when the order's quote lapses; zero for orders without a quote.
func (topUp *TopUp) ExpiresAt() int64 {
	if topUp == nil || topUp.CreateTime <= 0 || topUp.PaymentProvider != PaymentProviderEthereum {
		return 0
	}
	return topUp.CreateTime + ChainOrderTTLSeconds()
}

func isChainTopUpExpired(topUp *TopUp, now int64) bool {
	expiresAt := topUp.ExpiresAt()
	return expiresAt > 0 && now > expiresAt
}

// RecordStrandedChainPayment leaves an admin-visible trail for an on-chain
// payment that reached the recipient wallet but could not be credited: the
// gateway cannot refund on-chain money, so every such case needs a human. The
// order is resolved by trade number so the entry lands on the payer's account;
// an order that does not exist at all is recorded against user 0. A repeated
// delivery of the same transaction does not add a second entry.
func RecordStrandedChainPayment(source LogSource, tradeNo string, payment *ChainPayment, reason string) {
	if !payment.present() {
		return
	}
	userId := 0
	if topUp := GetTopUpByTradeNo(tradeNo); topUp != nil {
		userId = topUp.UserId
	} else if order := GetSubscriptionOrderByTradeNo(tradeNo); order != nil {
		userId = order.UserId
	}
	txHash := strings.TrimSpace(payment.TxHash)
	if txHash != "" {
		var existing int64
		if err := LOG_DB.Model(&Log{}).Where("user_id = ? AND type = ? AND content LIKE ?", userId, LogTypeTopup, "%"+txHash+"%").
			Count(&existing).Error; err == nil && existing > 0 {
			return
		}
	}
	RecordLog(source, userId, LogTypeTopup, fmt.Sprintf("Ethereum 付款已到账但未能入账（%s），需人工核对：单号 %s，token %s，实付 %s，交易 %s",
		reason, tradeNo, payment.Token, payment.Amount, txHash))
}

// chainPaymentCoversCurrentPrice reports whether a payment that landed after
// its quote expired still buys the order's units at today's token price.
//
// On-chain money cannot be sent back by the gateway, so a late payment is not
// simply discarded. Honouring it at the current price removes the repricing
// arbitrage the expiry exists for while sparing an honest payer whose block
// confirmation merely crossed the deadline.
func chainPaymentCoversCurrentPrice(payment *ChainPayment, units decimal.Decimal) bool {
	if !payment.present() {
		return false
	}
	token, ok := setting.GetEthereumToken(payment.Token)
	if !ok {
		return false
	}
	expected, err := token.PayAmount(units)
	if err != nil {
		return false
	}
	return validateExpectedChainPayment(token.Address, expected, payment.Token, payment.Amount) == nil
}

func validateExpectedChainPayment(expectedToken, expectedAmount, paidToken, paidAmount string) error {
	expectedToken = strings.TrimSpace(expectedToken)
	expectedAmount = strings.TrimSpace(expectedAmount)
	paidToken = strings.TrimSpace(paidToken)
	paidAmount = strings.TrimSpace(paidAmount)
	if expectedToken == "" || expectedAmount == "" || paidToken == "" || paidAmount == "" {
		return ErrPaymentAmountMismatch
	}
	if !strings.EqualFold(expectedToken, paidToken) {
		return ErrPaymentAmountMismatch
	}
	expected := new(big.Int)
	if _, ok := expected.SetString(expectedAmount, 10); !ok || expected.Sign() <= 0 {
		return ErrPaymentAmountMismatch
	}
	paid := new(big.Int)
	if _, ok := paid.SetString(paidAmount, 10); !ok || paid.Sign() <= 0 {
		return ErrPaymentAmountMismatch
	}
	if paid.Cmp(expected) < 0 {
		return ErrPaymentAmountMismatch
	}
	return nil
}

func (topUp *TopUp) Insert() error {
	var err error
	err = DB.Create(topUp).Error
	return err
}

func (topUp *TopUp) Update() error {
	var err error
	err = DB.Save(topUp).Error
	return err
}

func GetTopUpById(id int) *TopUp {
	var topUp *TopUp
	var err error
	err = ReadDB().Where("id = ?", id).First(&topUp).Error
	if err != nil {
		return nil
	}
	return topUp
}

// GetTopUpByTradeNo must read the primary: payment callbacks (epay/stripe/
// creem/waffo) use this lookup for idempotency decisions before crediting
// quota, and a stale replica read widens the duplicate-credit window.
func GetTopUpByTradeNo(tradeNo string) *TopUp {
	var topUp *TopUp
	var err error
	err = DB.Where("trade_no = ?", tradeNo).First(&topUp).Error
	if err != nil {
		return nil
	}
	return topUp
}

func UpdatePendingTopUpStatus(tradeNo string, expectedPaymentProvider string, targetStatus string) error {
	if tradeNo == "" {
		return errors.New("未提供支付单号")
	}

	refCol := "`trade_no`"
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		refCol = `"trade_no"`
	}

	return DB.Transaction(func(tx *gorm.DB) error {
		topUp := &TopUp{}
		if err := lockForUpdate(tx).Where(refCol+" = ?", tradeNo).First(topUp).Error; err != nil {
			return ErrTopUpNotFound
		}
		if expectedPaymentProvider != "" && topUp.PaymentProvider != expectedPaymentProvider {
			return ErrPaymentMethodMismatch
		}
		if topUp.Status != common.TopUpStatusPending {
			return ErrTopUpStatusInvalid
		}

		topUp.Status = targetStatus
		return tx.Save(topUp).Error
	})
}

func Recharge(source LogSource, referenceId string, customerId string) (err error) {
	if referenceId == "" {
		return errors.New("未提供支付单号")
	}

	var quota float64
	topUp := &TopUp{}

	refCol := "`trade_no`"
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		refCol = `"trade_no"`
	}

	err = DB.Transaction(func(tx *gorm.DB) error {
		err := lockForUpdate(tx).Where(refCol+" = ?", referenceId).First(topUp).Error
		if err != nil {
			return errors.New("充值订单不存在")
		}

		if topUp.PaymentProvider != PaymentProviderStripe {
			return ErrPaymentMethodMismatch
		}

		if topUp.Status != common.TopUpStatusPending {
			return errors.New("充值订单状态错误")
		}

		topUp.CompleteTime = common.GetTimestamp()
		topUp.Status = common.TopUpStatusSuccess
		err = tx.Save(topUp).Error
		if err != nil {
			return err
		}

		quota = topUp.Money * common.QuotaPerUnit
		err = tx.Model(&User{}).Where("id = ?", topUp.UserId).Updates(map[string]interface{}{"stripe_customer": customerId, "quota": gorm.Expr("quota + ?", quota)}).Error
		if err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		common.SysError("topup failed: " + err.Error())
		return errors.New("充值失败，请稍后重试")
	}

	RecordTopupLog(source, topUp.UserId, fmt.Sprintf("使用在线充值成功，充值金额: %v，支付金额：%d", logger.FormatQuota(int(quota)), topUp.Amount), topUp.PaymentMethod, PaymentMethodStripe)

	return nil
}

// topUpQueryWindowSeconds 限制充值记录查询的时间窗口（秒）。
const topUpQueryWindowSeconds int64 = 30 * 24 * 60 * 60

// topUpQueryCutoff 返回允许查询的最早 create_time（秒级 Unix 时间戳）。
func topUpQueryCutoff() int64 {
	return common.GetTimestamp() - topUpQueryWindowSeconds
}

func GetUserTopUps(userId int, pageInfo *common.PageInfo) (topups []*TopUp, total int64, err error) {
	// Start transaction
	tx := DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	cutoff := topUpQueryCutoff()

	// Get total count within transaction
	err = tx.Model(&TopUp{}).Where("user_id = ? AND create_time >= ?", userId, cutoff).Count(&total).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	// Get paginated topups within same transaction
	err = tx.Where("user_id = ? AND create_time >= ?", userId, cutoff).Order("id desc").Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).Find(&topups).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	// Commit transaction
	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}

	return topups, total, nil
}

// GetAllTopUps 获取全平台的充值记录（管理员使用，不限制时间窗口）
func GetAllTopUps(pageInfo *common.PageInfo) (topups []*TopUp, total int64, err error) {
	tx := DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	if err = tx.Model(&TopUp{}).Count(&total).Error; err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	if err = tx.Order("id desc").Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).Find(&topups).Error; err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}

	return topups, total, nil
}

// searchTopUpCountHardLimit 搜索充值记录时 COUNT 的安全上限，
// 防止对超大表执行无界 COUNT 触发 DoS。
const searchTopUpCountHardLimit = 10000

// SearchUserTopUps 按订单号搜索某用户的充值记录
func SearchUserTopUps(userId int, keyword string, pageInfo *common.PageInfo) (topups []*TopUp, total int64, err error) {
	tx := DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	query := tx.Model(&TopUp{}).Where("user_id = ? AND create_time >= ?", userId, topUpQueryCutoff())
	if keyword != "" {
		pattern, perr := sanitizeLikePattern(keyword)
		if perr != nil {
			tx.Rollback()
			return nil, 0, perr
		}
		query = query.Where("trade_no LIKE ? ESCAPE '!'", pattern)
	}

	if err = query.Limit(searchTopUpCountHardLimit).Count(&total).Error; err != nil {
		tx.Rollback()
		common.SysError("failed to count search topups: " + err.Error())
		return nil, 0, errors.New("搜索充值记录失败")
	}

	if err = query.Order("id desc").Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).Find(&topups).Error; err != nil {
		tx.Rollback()
		common.SysError("failed to search topups: " + err.Error())
		return nil, 0, errors.New("搜索充值记录失败")
	}

	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}
	return topups, total, nil
}

// SearchAllTopUps 按订单号搜索全平台充值记录（管理员使用，不限制时间窗口）
func SearchAllTopUps(keyword string, pageInfo *common.PageInfo) (topups []*TopUp, total int64, err error) {
	tx := DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	query := tx.Model(&TopUp{})
	if keyword != "" {
		pattern, perr := sanitizeLikePattern(keyword)
		if perr != nil {
			tx.Rollback()
			return nil, 0, perr
		}
		query = query.Where("trade_no LIKE ? ESCAPE '!'", pattern)
	}

	if err = query.Limit(searchTopUpCountHardLimit).Count(&total).Error; err != nil {
		tx.Rollback()
		common.SysError("failed to count search topups: " + err.Error())
		return nil, 0, errors.New("搜索充值记录失败")
	}

	if err = query.Order("id desc").Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).Find(&topups).Error; err != nil {
		tx.Rollback()
		common.SysError("failed to search topups: " + err.Error())
		return nil, 0, errors.New("搜索充值记录失败")
	}

	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}
	return topups, total, nil
}

// ManualCompleteTopUp 管理员手动完成订单并给用户充值
func ManualCompleteTopUp(source LogSource, tradeNo string) error {
	if tradeNo == "" {
		return errors.New("未提供订单号")
	}

	refCol := "`trade_no`"
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		refCol = `"trade_no"`
	}

	var userId int
	var quotaToAdd int
	var payMoney float64
	var paymentMethod string

	err := DB.Transaction(func(tx *gorm.DB) error {
		topUp := &TopUp{}
		// 行级锁，避免并发补单
		if err := lockForUpdate(tx).Where(refCol+" = ?", tradeNo).First(topUp).Error; err != nil {
			return errors.New("充值订单不存在")
		}

		// 幂等处理：已成功直接返回
		if topUp.Status == common.TopUpStatusSuccess {
			return nil
		}

		if topUp.Status != common.TopUpStatusPending {
			return errors.New("订单状态不是待支付，无法补单")
		}

		// 计算应充值额度：
		// - Stripe 订单：Money 代表经分组倍率换算后的美元数量，直接 * QuotaPerUnit
		// - 其他订单（如易支付）：Amount 为美元数量，* QuotaPerUnit
		if topUp.PaymentProvider == PaymentProviderStripe {
			dQuotaPerUnit := decimal.NewFromFloat(common.QuotaPerUnit)
			quotaToAdd = int(decimal.NewFromFloat(topUp.Money).Mul(dQuotaPerUnit).IntPart())
		} else {
			dAmount := decimal.NewFromInt(topUp.Amount)
			dQuotaPerUnit := decimal.NewFromFloat(common.QuotaPerUnit)
			quotaToAdd = int(dAmount.Mul(dQuotaPerUnit).IntPart())
		}
		if quotaToAdd <= 0 {
			return errors.New("无效的充值额度")
		}

		// 标记完成
		topUp.CompleteTime = common.GetTimestamp()
		topUp.Status = common.TopUpStatusSuccess
		if err := tx.Save(topUp).Error; err != nil {
			return err
		}

		// 增加用户额度（立即写库，保持一致性）
		if err := tx.Model(&User{}).Where("id = ?", topUp.UserId).Update("quota", gorm.Expr("quota + ?", quotaToAdd)).Error; err != nil {
			return err
		}

		userId = topUp.UserId
		payMoney = topUp.Money
		paymentMethod = topUp.PaymentMethod
		return nil
	})

	if err != nil {
		return err
	}

	// 事务外记录日志，避免阻塞
	RecordTopupLog(source, userId, fmt.Sprintf("管理员补单成功，充值金额: %v，支付金额：%f", logger.FormatQuota(quotaToAdd), payMoney), paymentMethod, "admin")
	return nil
}
func RechargeCreem(source LogSource, referenceId string, customerEmail string, customerName string) (err error) {
	if referenceId == "" {
		return errors.New("未提供支付单号")
	}

	var quota int64
	topUp := &TopUp{}

	refCol := "`trade_no`"
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		refCol = `"trade_no"`
	}

	err = DB.Transaction(func(tx *gorm.DB) error {
		err := lockForUpdate(tx).Where(refCol+" = ?", referenceId).First(topUp).Error
		if err != nil {
			return errors.New("充值订单不存在")
		}

		if topUp.PaymentProvider != PaymentProviderCreem {
			return ErrPaymentMethodMismatch
		}

		if topUp.Status != common.TopUpStatusPending {
			return errors.New("充值订单状态错误")
		}

		topUp.CompleteTime = common.GetTimestamp()
		topUp.Status = common.TopUpStatusSuccess
		err = tx.Save(topUp).Error
		if err != nil {
			return err
		}

		// Creem 直接使用 Amount 作为充值额度（整数）
		quota = topUp.Amount

		// 构建更新字段，优先使用邮箱，如果邮箱为空则使用用户名
		updateFields := map[string]interface{}{
			"quota": gorm.Expr("quota + ?", quota),
		}

		// 如果有客户邮箱，尝试更新用户邮箱（仅当用户邮箱为空时）
		if customerEmail != "" {
			// 先检查用户当前邮箱是否为空
			var user User
			err = tx.Where("id = ?", topUp.UserId).First(&user).Error
			if err != nil {
				return err
			}

			// 如果用户邮箱为空，则更新为支付时使用的邮箱
			if user.Email == "" {
				updateFields["email"] = customerEmail
			}
		}

		err = tx.Model(&User{}).Where("id = ?", topUp.UserId).Updates(updateFields).Error
		if err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		common.SysError("creem topup failed: " + err.Error())
		return errors.New("充值失败，请稍后重试")
	}

	RecordTopupLog(source, topUp.UserId, fmt.Sprintf("使用Creem充值成功，充值额度: %v，支付金额：%.2f", quota, topUp.Money), topUp.PaymentMethod, PaymentMethodCreem)

	return nil
}

func RechargeEthereumWithPaymentCheck(source LogSource, tradeNo string, payment *ChainPayment) (err error) {
	if tradeNo == "" {
		return errors.New("未提供支付单号")
	}

	var quotaToAdd int
	orderExpired := false
	latePayment := false
	topUp := &TopUp{}

	refCol := "`trade_no`"
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		refCol = `"trade_no"`
	}

	err = DB.Transaction(func(tx *gorm.DB) error {
		err := lockForUpdate(tx).Where(refCol+" = ?", tradeNo).First(topUp).Error
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrTopUpNotFound
			}
			return err
		}
		if topUp.PaymentProvider != PaymentProviderEthereum {
			return ErrPaymentMethodMismatch
		}
		if topUp.Status == common.TopUpStatusSuccess {
			return nil
		}
		// An order already closed as expired (by the user or a sweeper) is still
		// a valid target for a payment that meets the current price: the money
		// moved regardless of what the status row says.
		if topUp.Status != common.TopUpStatusPending && !(topUp.Status == common.TopUpStatusExpired && payment.present()) {
			return ErrTopUpStatusInvalid
		}
		if payment.present() {
			if err := validateExpectedChainPayment(topUp.ExpectedPaymentToken, topUp.ExpectedPaymentAmount, payment.Token, payment.Amount); err != nil {
				return err
			}
		}

		// CreateTime and the expires_at handed to the wallet come from the app
		// clock, so the expiry decision must use the same clock.
		now := common.GetTimestamp()
		if topUp.Status == common.TopUpStatusExpired || isChainTopUpExpired(topUp, now) {
			if !chainPaymentCoversCurrentPrice(payment, decimal.NewFromInt(topUp.Amount)) {
				if topUp.Status == common.TopUpStatusExpired {
					return ErrTopUpExpired
				}
				topUp.Status = common.TopUpStatusExpired
				topUp.CompleteTime = now
				if err := tx.Save(topUp).Error; err != nil {
					return err
				}
				// Commit the expiry, then surface it to the caller: returning the
				// error here would roll the status write back.
				orderExpired = true
				return nil
			}
			latePayment = true
		}

		dAmount := decimal.NewFromInt(topUp.Amount)
		dQuotaPerUnit := decimal.NewFromFloat(common.QuotaPerUnit)
		quotaToAdd = int(dAmount.Mul(dQuotaPerUnit).IntPart())
		if quotaToAdd <= 0 {
			return ErrTopUpStatusInvalid
		}

		topUp.CompleteTime = now
		topUp.Status = common.TopUpStatusSuccess
		if err := tx.Save(topUp).Error; err != nil {
			return err
		}

		if err := tx.Model(&User{}).Where("id = ?", topUp.UserId).Update("quota", gorm.Expr("quota + ?", quotaToAdd)).Error; err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		common.SysError("ethereum topup failed: " + err.Error())
		if errors.Is(err, ErrTopUpNotFound) || errors.Is(err, ErrTopUpStatusInvalid) || errors.Is(err, ErrTopUpExpired) ||
			errors.Is(err, ErrPaymentAmountMismatch) || errors.Is(err, ErrPaymentMethodMismatch) {
			return err
		}
		return fmt.Errorf("%w: %v", ErrPaymentSettlementRetryable, err)
	}

	if orderExpired {
		return ErrTopUpExpired
	}

	if quotaToAdd > 0 {
		note := ""
		if latePayment {
			note = "（报价过期后按当前价格入账）"
		}
		RecordTopupLog(source, topUp.UserId, fmt.Sprintf("Ethereum充值成功%s，充值额度: %v，支付金额: %.6f", note, logger.FormatQuota(quotaToAdd), topUp.Money), "ethereum", "ethereum")
	}

	return nil
}

func RechargeWaffo(source LogSource, tradeNo string) (err error) {
	if tradeNo == "" {
		return errors.New("未提供支付单号")
	}

	var quotaToAdd int
	topUp := &TopUp{}

	refCol := "`trade_no`"
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		refCol = `"trade_no"`
	}

	err = DB.Transaction(func(tx *gorm.DB) error {
		err := lockForUpdate(tx).Where(refCol+" = ?", tradeNo).First(topUp).Error
		if err != nil {
			return errors.New("充值订单不存在")
		}

		if topUp.PaymentProvider != PaymentProviderWaffo {
			return ErrPaymentMethodMismatch
		}

		if topUp.Status == common.TopUpStatusSuccess {
			return nil // 幂等：已成功直接返回
		}

		if topUp.Status != common.TopUpStatusPending {
			return errors.New("充值订单状态错误")
		}

		dAmount := decimal.NewFromInt(topUp.Amount)
		dQuotaPerUnit := decimal.NewFromFloat(common.QuotaPerUnit)
		quotaToAdd = int(dAmount.Mul(dQuotaPerUnit).IntPart())
		if quotaToAdd <= 0 {
			return errors.New("无效的充值额度")
		}

		topUp.CompleteTime = common.GetTimestamp()
		topUp.Status = common.TopUpStatusSuccess
		if err := tx.Save(topUp).Error; err != nil {
			return err
		}

		if err := tx.Model(&User{}).Where("id = ?", topUp.UserId).Update("quota", gorm.Expr("quota + ?", quotaToAdd)).Error; err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		common.SysError("waffo topup failed: " + err.Error())
		return errors.New("充值失败，请稍后重试")
	}

	if quotaToAdd > 0 {
		RecordTopupLog(source, topUp.UserId, fmt.Sprintf("Waffo充值成功，充值额度: %v，支付金额: %.2f", logger.FormatQuota(quotaToAdd), topUp.Money), topUp.PaymentMethod, PaymentMethodWaffo)
	}

	return nil
}

func RechargeWaffoPancake(source LogSource, tradeNo string) (err error) {
	if tradeNo == "" {
		return errors.New("未提供支付单号")
	}

	var quotaToAdd int
	topUp := &TopUp{}

	refCol := "`trade_no`"
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		refCol = `"trade_no"`
	}

	err = DB.Transaction(func(tx *gorm.DB) error {
		err := lockForUpdate(tx).Where(refCol+" = ?", tradeNo).First(topUp).Error
		if err != nil {
			return errors.New("充值订单不存在")
		}

		if topUp.PaymentProvider != PaymentProviderWaffoPancake {
			return ErrPaymentMethodMismatch
		}

		if topUp.Status == common.TopUpStatusSuccess {
			return nil
		}

		if topUp.Status != common.TopUpStatusPending {
			return errors.New("充值订单状态错误")
		}

		quotaToAdd = int(decimal.NewFromInt(topUp.Amount).Mul(decimal.NewFromFloat(common.QuotaPerUnit)).IntPart())
		if quotaToAdd <= 0 {
			return errors.New("无效的充值额度")
		}

		topUp.CompleteTime = common.GetTimestamp()
		topUp.Status = common.TopUpStatusSuccess
		if err := tx.Save(topUp).Error; err != nil {
			return err
		}

		if err := tx.Model(&User{}).Where("id = ?", topUp.UserId).Update("quota", gorm.Expr("quota + ?", quotaToAdd)).Error; err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		common.SysError("waffo pancake topup failed: " + err.Error())
		return errors.New("充值失败，请稍后重试")
	}

	if quotaToAdd > 0 {
		RecordTopupLog(source, topUp.UserId, fmt.Sprintf("Waffo Pancake充值成功，充值额度: %v，支付金额: %.2f", logger.FormatQuota(quotaToAdd), topUp.Money), topUp.PaymentMethod, PaymentMethodWaffoPancake)
	}

	return nil
}
