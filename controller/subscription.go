package controller

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// ---- Shared types ----

type SubscriptionPlanDTO struct {
	Plan model.SubscriptionPlan `json:"plan"`
}

type BillingPreferenceRequest struct {
	BillingPreference string `json:"billing_preference"`
}

type SwitchSelfSubscriptionRequest struct {
	SubscriptionId int `json:"subscription_id"`
}

type SubscriptionOrderSelfDTO struct {
	Id               int               `json:"id"`
	TradeNo          string            `json:"trade_no"`
	PlanId           int               `json:"plan_id"`
	PlanTitle        string            `json:"plan_title"`
	Money            float64           `json:"money"`
	PaymentMethod    string            `json:"payment_method"`
	PaymentProvider  string            `json:"payment_provider"`
	Status           string            `json:"status"`
	CreateTime       int64             `json:"create_time"`
	CompleteTime     int64             `json:"complete_time"`
	ExpiresAt        int64             `json:"expires_at"`
	RemainingSeconds int64             `json:"remaining_seconds"`
	ResumeType       string            `json:"resume_type,omitempty"`
	ResumeURL        string            `json:"resume_url,omitempty"`
	ResumeParams     map[string]string `json:"resume_params,omitempty"`
	ResumeMessage    string            `json:"resume_message,omitempty"`
}

func getTierPeriodSeconds(tier model.QuotaTier) int64 {
	switch tier.Period {
	case model.TierPeriodHourly:
		return 3600
	case model.TierPeriodDaily:
		return 86400
	case model.TierPeriodWeekly:
		return 7 * 86400
	case model.TierPeriodMonthly:
		return 30 * 86400
	case model.TierPeriodCustom:
		return tier.CustomSeconds
	default:
		return math.MaxInt64
	}
}

// validateQuotaTiers validates the QuotaTiers JSON string
func validateQuotaTiers(quotaTiers string) (string, string) {
	trimmed := strings.TrimSpace(quotaTiers)
	if trimmed == "" || trimmed == "[]" {
		return "[]", ""
	}
	var tiers []model.QuotaTier
	if err := common.Unmarshal([]byte(trimmed), &tiers); err != nil {
		return "", "配额层级JSON格式错误"
	}
	if len(tiers) == 0 {
		return "[]", ""
	}
	validPeriods := map[string]bool{
		model.TierPeriodMonthly: true,
		model.TierPeriodWeekly:  true,
		model.TierPeriodDaily:   true,
		model.TierPeriodHourly:  true,
		model.TierPeriodCustom:  true,
		model.TierPeriodNone:    true,
	}
	periods := make(map[string]bool, len(tiers))
	for i := range tiers {
		tier := &tiers[i]
		if !validPeriods[tier.Period] {
			return "", "配额层级 #" + strconv.Itoa(i+1) + " 周期类型无效: " + tier.Period
		}
		if tier.Limit <= 0 || tier.Limit >= common.MaxQuota {
			return "", "多周期限额需大于0且小于最大安全额度"
		}
		if tier.Period == model.TierPeriodCustom && tier.CustomSeconds <= 0 {
			return "", "配额层级 #" + strconv.Itoa(i+1) + " 自定义周期秒数需大于0"
		}
		key := tier.Period
		if tier.Period == model.TierPeriodHourly || tier.Period == model.TierPeriodCustom {
			key = "window:" + strconv.FormatInt(getTierPeriodSeconds(*tier), 10)
		}
		if tier.Period != model.TierPeriodCustom {
			tier.CustomSeconds = 0
		}
		if periods[key] {
			return "", "多周期限额不能重复配置相同周期"
		}
		periods[key] = true
	}

	// Custom windows are ordered by their actual length, alongside calendar periods.
	sort.SliceStable(tiers, func(i, j int) bool {
		return getTierPeriodSeconds(tiers[i]) < getTierPeriodSeconds(tiers[j])
	})

	// Update sort_priority to match sorted order
	for i := range tiers {
		tiers[i].SortPriority = (i + 1) * 10
	}

	// Larger periods must not have a smaller allowance.
	for i := 1; i < len(tiers); i++ {
		if tiers[i].Limit < tiers[i-1].Limit {
			return "", fmt.Sprintf("配额层级 #%d 的限额(%d)不能小于层级 #%d 的限额(%d)", i+1, tiers[i].Limit, i, tiers[i-1].Limit)
		}
	}

	data, err := common.Marshal(tiers)
	if err != nil {
		return "", "配额层级序列化失败"
	}
	return string(data), ""
}

func validateSubscriptionPlanQuota(plan, existing *model.SubscriptionPlan) string {
	if existing != nil && plan.TotalAmount == existing.TotalAmount &&
		model.NormalizeResetPeriod(plan.QuotaResetPeriod) == model.NormalizeResetPeriod(existing.QuotaResetPeriod) &&
		plan.QuotaResetCustomSeconds == existing.QuotaResetCustomSeconds {
		var requestedTiers, existingTiers []model.QuotaTier
		requestedErr := common.UnmarshalJsonStr(plan.QuotaTiers, &requestedTiers)
		existingErr := common.UnmarshalJsonStr(existing.QuotaTiers, &existingTiers)
		if plan.QuotaTiers == existing.QuotaTiers || requestedErr == nil && existingErr == nil && reflect.DeepEqual(requestedTiers, existingTiers) {
			// Keep historical mixed configurations and tier indices intact when the
			// administrator only edits unrelated fields such as the plan title.
			plan.QuotaTiers = existing.QuotaTiers
			plan.QuotaResetPeriod = existing.QuotaResetPeriod
			return ""
		}
	}
	if plan.TotalAmount < 0 || plan.TotalAmount >= common.MaxQuota {
		return "额度需大于等于0且小于最大安全额度"
	}
	plan.QuotaResetPeriod = model.NormalizeResetPeriod(plan.QuotaResetPeriod)
	if plan.QuotaResetPeriod == model.SubscriptionResetCustom && plan.QuotaResetCustomSeconds <= 0 {
		return "自定义重置周期需大于0秒"
	}
	if plan.QuotaResetPeriod != model.SubscriptionResetCustom {
		plan.QuotaResetCustomSeconds = 0
	}
	normalizedTiers, errMsg := validateQuotaTiers(plan.QuotaTiers)
	if errMsg != "" {
		return errMsg
	}
	if normalizedTiers != "[]" && (plan.TotalAmount > 0 || plan.QuotaResetPeriod != model.SubscriptionResetNever) {
		return "多周期限额与单一额度配置不能同时设置，请在多周期限额中配置总额度或周期限额"
	}
	if normalizedTiers == "[]" && plan.TotalAmount == 0 && plan.QuotaResetPeriod != model.SubscriptionResetNever {
		return "周期额度需大于0，不限额度时请关闭周期重置"
	}
	plan.QuotaTiers = normalizedTiers
	return ""
}

// ---- User APIs ----

func GetSubscriptionPlans(c *gin.Context) {
	var plans []model.SubscriptionPlan
	if err := model.ReadDB().Where("enabled = ?", true).Order("sort_order desc, id desc").Find(&plans).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	purchaseCounts, err := model.CountSubscriptionPlanPurchaseCounts(plans, true)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	result := make([]SubscriptionPlanDTO, 0, len(plans))
	for _, p := range plans {
		p.PurchaseCount = purchaseCounts[p.Id]
		result = append(result, SubscriptionPlanDTO{
			Plan: p,
		})
	}
	common.ApiSuccess(c, result)
}

func GetSubscriptionSelf(c *gin.Context) {
	userId := c.GetInt("id")
	settingMap, _ := model.GetUserSetting(userId, false)
	pref := common.NormalizeBillingPreference(settingMap.BillingPreference)

	// Get all subscriptions (including expired)
	allSubscriptions, err := model.GetAllUserSubscriptions(userId)
	if err != nil {
		allSubscriptions = []model.SubscriptionSummary{}
	}

	// Get active subscriptions for backward compatibility
	activeSubscriptions, err := model.GetAllActiveUserSubscriptions(userId)
	if err != nil {
		activeSubscriptions = []model.SubscriptionSummary{}
	}

	common.ApiSuccess(c, gin.H{
		"billing_preference": pref,
		"subscriptions":      activeSubscriptions, // all active subscriptions
		"all_subscriptions":  allSubscriptions,    // all subscriptions including expired
	})
}

func UpdateSubscriptionPreference(c *gin.Context) {
	userId := c.GetInt("id")
	var req BillingPreferenceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorMsg(c, "参数错误")
		return
	}
	pref := common.NormalizeBillingPreference(req.BillingPreference)
	if pref == "wallet_only" || pref == "wallet_first" {
		noFallback, err := model.HasGlobalDisableBalanceDeductionSubscription(userId)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		if noFallback {
			common.ApiErrorMsg(c, "当前订阅不允许使用余额扣费")
			return
		}
	}

	user, err := model.GetUserById(userId, true)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	current := user.GetSetting()
	current.BillingPreference = pref
	if err := model.UpdateUserSetting(user.Id, current); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"billing_preference": pref})
}

func SwitchSelfSubscription(c *gin.Context) {
	userId := c.GetInt("id")
	var req SwitchSelfSubscriptionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorMsg(c, "参数错误")
		return
	}
	if req.SubscriptionId <= 0 {
		common.ApiErrorMsg(c, "订阅ID无效")
		return
	}
	msg, err := model.SwitchUserActiveSubscription(userId, req.SubscriptionId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if strings.TrimSpace(msg) == "" {
		common.ApiSuccess(c, gin.H{})
		return
	}
	common.ApiSuccess(c, gin.H{"message": msg})
}

func GetSubscriptionOrdersSelf(c *gin.Context) {
	userId := c.GetInt("id")
	pageInfo := common.GetPageQuery(c)
	keyword := strings.TrimSpace(c.Query("keyword"))

	_, _ = model.ExpireStalePendingSubscriptionOrders(100)

	orders, total, err := model.GetUserSubscriptionOrders(userId, keyword, pageInfo)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	now := common.GetTimestamp()
	planTitleMap := make(map[int]string, len(orders))
	items := make([]SubscriptionOrderSelfDTO, 0, len(orders))
	for _, order := range orders {
		planTitle, ok := planTitleMap[order.PlanId]
		if !ok {
			plan, planErr := model.GetSubscriptionPlanById(order.PlanId)
			if planErr == nil && plan != nil {
				planTitle = plan.Title
			}
			planTitleMap[order.PlanId] = planTitle
		}

		item := SubscriptionOrderSelfDTO{
			Id:               order.Id,
			TradeNo:          order.TradeNo,
			PlanId:           order.PlanId,
			PlanTitle:        planTitle,
			Money:            order.Money,
			PaymentMethod:    order.PaymentMethod,
			PaymentProvider:  order.PaymentProvider,
			Status:           order.EffectiveStatus(now),
			CreateTime:       order.CreateTime,
			CompleteTime:     order.CompleteTime,
			ExpiresAt:        order.ExpiresAt(),
			RemainingSeconds: order.RemainingSeconds(now),
		}
		if item.Status == common.TopUpStatusPending {
			if payload := order.GetResumePayload(); payload != nil {
				item.ResumeType = payload.Type
				item.ResumeURL = payload.URL
				item.ResumeParams = payload.Params
				item.ResumeMessage = payload.Message
			}
		}
		items = append(items, item)
	}

	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(items)
	common.ApiSuccess(c, pageInfo)
}

func CancelSubscriptionOrderSelf(c *gin.Context) {
	userId := c.GetInt("id")
	tradeNo := strings.TrimSpace(c.Param("tradeNo"))
	if tradeNo == "" {
		common.ApiErrorMsg(c, "订单号不能为空")
		return
	}
	if err := model.CancelPendingSubscriptionOrder(userId, tradeNo); err != nil {
		switch {
		case errors.Is(err, model.ErrSubscriptionOrderNotFound):
			common.ApiErrorMsg(c, "订单不存在")
		case errors.Is(err, model.ErrSubscriptionOrderStatusInvalid):
			common.ApiErrorMsg(c, "当前订单不可取消")
		default:
			common.ApiError(c, err)
		}
		return
	}
	common.ApiSuccess(c, gin.H{"trade_no": tradeNo})
}

// ---- Admin APIs ----

func AdminListSubscriptionPlans(c *gin.Context) {
	var plans []model.SubscriptionPlan
	if err := model.ReadDB().Order("sort_order desc, id desc").Find(&plans).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	purchaseCounts, err := model.CountSubscriptionPlanPurchaseCounts(plans, true)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	result := make([]SubscriptionPlanDTO, 0, len(plans))
	for _, p := range plans {
		p.PurchaseCount = purchaseCounts[p.Id]
		result = append(result, SubscriptionPlanDTO{
			Plan: p,
		})
	}
	common.ApiSuccess(c, result)
}

type AdminUpsertSubscriptionPlanRequest struct {
	Plan model.SubscriptionPlan `json:"plan"`
}

func AdminCreateSubscriptionPlan(c *gin.Context) {
	var req AdminUpsertSubscriptionPlanRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorMsg(c, "参数错误")
		return
	}
	req.Plan.Id = 0
	if strings.TrimSpace(req.Plan.Title) == "" {
		common.ApiErrorMsg(c, "套餐标题不能为空")
		return
	}
	if req.Plan.PriceAmount < 0 {
		common.ApiErrorMsg(c, "价格不能为负数")
		return
	}
	if req.Plan.PriceAmount > 9999 {
		common.ApiErrorMsg(c, "价格不能超过9999")
		return
	}
	if req.Plan.Currency == "" {
		req.Plan.Currency = "USD"
	}
	req.Plan.Currency = "USD"
	if req.Plan.AllowBalancePay == nil {
		req.Plan.AllowBalancePay = common.GetPointer(true)
	}
	if req.Plan.AllowWalletOverflow == nil {
		req.Plan.AllowWalletOverflow = common.GetPointer(true)
	}
	if req.Plan.DurationUnit == "" {
		req.Plan.DurationUnit = model.SubscriptionDurationMonth
	}
	if req.Plan.DurationValue <= 0 && req.Plan.DurationUnit != model.SubscriptionDurationCustom {
		req.Plan.DurationValue = 1
	}
	if req.Plan.MaxPurchasePerUser < 0 {
		common.ApiErrorMsg(c, "购买上限不能为负数")
		return
	}
	if req.Plan.MaxPurchaseTotal < 0 {
		common.ApiErrorMsg(c, "全局购买上限不能为负数")
		return
	}
	req.Plan.MaxPurchaseResetPeriod = model.NormalizePurchaseResetPeriod(req.Plan.MaxPurchaseResetPeriod)
	if req.Plan.MaxPurchaseTotal <= 0 {
		req.Plan.MaxPurchaseResetPeriod = model.SubscriptionResetNever
		req.Plan.MaxPurchaseResetCustomSeconds = 0
	} else {
		if req.Plan.MaxPurchaseResetPeriod == model.SubscriptionResetCustom && req.Plan.MaxPurchaseResetCustomSeconds <= 0 {
			common.ApiErrorMsg(c, "全局限购自定义刷新周期需大于0秒")
			return
		}
		if req.Plan.MaxPurchaseResetPeriod != model.SubscriptionResetCustom {
			req.Plan.MaxPurchaseResetCustomSeconds = 0
		}
	}
	req.Plan.UpgradeGroup = strings.TrimSpace(req.Plan.UpgradeGroup)
	if req.Plan.UpgradeGroup != "" {
		if _, ok := ratio_setting.GetGroupRatioCopy()[req.Plan.UpgradeGroup]; !ok {
			common.ApiErrorMsg(c, "升级分组不存在")
			return
		}
	}
	req.Plan.DowngradeGroup = strings.TrimSpace(req.Plan.DowngradeGroup)
	if req.Plan.DowngradeGroup != "" {
		if _, ok := ratio_setting.GetGroupRatioCopy()[req.Plan.DowngradeGroup]; !ok {
			common.ApiErrorMsg(c, "降级分组不存在")
			return
		}
	}
	req.Plan.GroupMode = strings.TrimSpace(req.Plan.GroupMode)
	if req.Plan.GroupMode == model.SubscriptionGroupModeUpgrade {
		req.Plan.GroupMode = ""
	}
	if req.Plan.GroupMode != "" && req.Plan.GroupMode != model.SubscriptionGroupModeAttach {
		common.ApiErrorMsg(c, "分组模式无效")
		return
	}
	if req.Plan.GroupMode == model.SubscriptionGroupModeAttach {
		if req.Plan.UpgradeGroup == "" {
			common.ApiErrorMsg(c, "追加分组模式必须配置分组")
			return
		}
		if req.Plan.DowngradeGroup != "" {
			common.ApiErrorMsg(c, "追加分组模式不支持降级分组")
			return
		}
	}
	if errMsg := validateSubscriptionPlanQuota(&req.Plan, nil); errMsg != "" {
		common.ApiErrorMsg(c, errMsg)
		return
	}
	req.Plan.AllowedPaymentMethods = model.NormalizeSubscriptionPayMethods(req.Plan.AllowedPaymentMethods)
	err := model.DB.Create(&req.Plan).Error
	if err != nil {
		common.ApiError(c, err)
		return
	}
	model.InvalidateSubscriptionPlanCache(req.Plan.Id)
	common.ApiSuccess(c, req.Plan)
}

func AdminUpdateSubscriptionPlan(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	if id <= 0 {
		common.ApiErrorMsg(c, "无效的ID")
		return
	}
	var req AdminUpsertSubscriptionPlanRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorMsg(c, "参数错误")
		return
	}
	if strings.TrimSpace(req.Plan.Title) == "" {
		common.ApiErrorMsg(c, "套餐标题不能为空")
		return
	}
	if req.Plan.PriceAmount < 0 {
		common.ApiErrorMsg(c, "价格不能为负数")
		return
	}
	if req.Plan.PriceAmount > 9999 {
		common.ApiErrorMsg(c, "价格不能超过9999")
		return
	}
	req.Plan.Id = id
	if req.Plan.Currency == "" {
		req.Plan.Currency = "USD"
	}
	req.Plan.Currency = "USD"
	if req.Plan.DurationUnit == "" {
		req.Plan.DurationUnit = model.SubscriptionDurationMonth
	}
	if req.Plan.DurationValue <= 0 && req.Plan.DurationUnit != model.SubscriptionDurationCustom {
		req.Plan.DurationValue = 1
	}
	if req.Plan.MaxPurchasePerUser < 0 {
		common.ApiErrorMsg(c, "购买上限不能为负数")
		return
	}
	if req.Plan.MaxPurchaseTotal < 0 {
		common.ApiErrorMsg(c, "全局购买上限不能为负数")
		return
	}
	req.Plan.MaxPurchaseResetPeriod = model.NormalizePurchaseResetPeriod(req.Plan.MaxPurchaseResetPeriod)
	if req.Plan.MaxPurchaseTotal <= 0 {
		req.Plan.MaxPurchaseResetPeriod = model.SubscriptionResetNever
		req.Plan.MaxPurchaseResetCustomSeconds = 0
	} else {
		if req.Plan.MaxPurchaseResetPeriod == model.SubscriptionResetCustom && req.Plan.MaxPurchaseResetCustomSeconds <= 0 {
			common.ApiErrorMsg(c, "全局限购自定义刷新周期需大于0秒")
			return
		}
		if req.Plan.MaxPurchaseResetPeriod != model.SubscriptionResetCustom {
			req.Plan.MaxPurchaseResetCustomSeconds = 0
		}
	}
	req.Plan.UpgradeGroup = strings.TrimSpace(req.Plan.UpgradeGroup)
	if req.Plan.UpgradeGroup != "" {
		if _, ok := ratio_setting.GetGroupRatioCopy()[req.Plan.UpgradeGroup]; !ok {
			common.ApiErrorMsg(c, "升级分组不存在")
			return
		}
	}
	req.Plan.DowngradeGroup = strings.TrimSpace(req.Plan.DowngradeGroup)
	if req.Plan.DowngradeGroup != "" {
		if _, ok := ratio_setting.GetGroupRatioCopy()[req.Plan.DowngradeGroup]; !ok {
			common.ApiErrorMsg(c, "降级分组不存在")
			return
		}
	}
	req.Plan.GroupMode = strings.TrimSpace(req.Plan.GroupMode)
	if req.Plan.GroupMode == model.SubscriptionGroupModeUpgrade {
		req.Plan.GroupMode = ""
	}
	if req.Plan.GroupMode != "" && req.Plan.GroupMode != model.SubscriptionGroupModeAttach {
		common.ApiErrorMsg(c, "分组模式无效")
		return
	}
	if req.Plan.GroupMode == model.SubscriptionGroupModeAttach {
		if req.Plan.UpgradeGroup == "" {
			common.ApiErrorMsg(c, "追加分组模式必须配置分组")
			return
		}
		if req.Plan.DowngradeGroup != "" {
			common.ApiErrorMsg(c, "追加分组模式不支持降级分组")
			return
		}
	}
	req.Plan.AllowedPaymentMethods = model.NormalizeSubscriptionPayMethods(req.Plan.AllowedPaymentMethods)

	err := model.DB.Transaction(func(tx *gorm.DB) error {
		var existing model.SubscriptionPlan
		if err := tx.First(&existing, id).Error; err != nil {
			return err
		}
		if errMsg := validateSubscriptionPlanQuota(&req.Plan, &existing); errMsg != "" {
			return errors.New(errMsg)
		}
		// update plan (allow zero values updates with map)
		updateMap := map[string]interface{}{
			"title":                             req.Plan.Title,
			"subtitle":                          req.Plan.Subtitle,
			"price_amount":                      req.Plan.PriceAmount,
			"currency":                          req.Plan.Currency,
			"duration_unit":                     req.Plan.DurationUnit,
			"duration_value":                    req.Plan.DurationValue,
			"custom_seconds":                    req.Plan.CustomSeconds,
			"enabled":                           req.Plan.Enabled,
			"sort_order":                        req.Plan.SortOrder,
			"stripe_price_id":                   req.Plan.StripePriceId,
			"creem_product_id":                  req.Plan.CreemProductId,
			"max_purchase_per_user":             req.Plan.MaxPurchasePerUser,
			"max_purchase_total":                req.Plan.MaxPurchaseTotal,
			"max_purchase_reset_period":         req.Plan.MaxPurchaseResetPeriod,
			"max_purchase_reset_custom_seconds": req.Plan.MaxPurchaseResetCustomSeconds,
			"total_amount":                      req.Plan.TotalAmount,
			"upgrade_group":                     req.Plan.UpgradeGroup,
			"group_mode":                        req.Plan.GroupMode,
			"quota_reset_period":                req.Plan.QuotaResetPeriod,
			"quota_reset_custom_seconds":        req.Plan.QuotaResetCustomSeconds,
			"quota_tiers":                       req.Plan.QuotaTiers,
			"disable_balance_deduction":         req.Plan.DisableBalanceDeduction,
			"allowed_payment_methods":           req.Plan.AllowedPaymentMethods,
			"updated_at":                        common.GetTimestamp(),
		}
		if req.Plan.AllowBalancePay != nil {
			updateMap["allow_balance_pay"] = *req.Plan.AllowBalancePay
		}
		if req.Plan.AllowWalletOverflow != nil {
			updateMap["allow_wallet_overflow"] = *req.Plan.AllowWalletOverflow
		}
		if err := tx.Model(&model.SubscriptionPlan{}).Where("id = ?", id).Updates(updateMap).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	model.InvalidateSubscriptionPlanCache(id)
	common.ApiSuccess(c, nil)
}

type AdminUpdateSubscriptionPlanStatusRequest struct {
	Enabled *bool `json:"enabled"`
}

func AdminUpdateSubscriptionPlanStatus(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	if id <= 0 {
		common.ApiErrorMsg(c, "无效的ID")
		return
	}
	var req AdminUpdateSubscriptionPlanStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.Enabled == nil {
		common.ApiErrorMsg(c, "参数错误")
		return
	}
	if err := model.DB.Model(&model.SubscriptionPlan{}).Where("id = ?", id).Update("enabled", *req.Enabled).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	model.InvalidateSubscriptionPlanCache(id)
	common.ApiSuccess(c, nil)
}

type AdminBindSubscriptionRequest struct {
	UserId int `json:"user_id"`
	PlanId int `json:"plan_id"`
}

func AdminBindSubscription(c *gin.Context) {
	var req AdminBindSubscriptionRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.UserId <= 0 || req.PlanId <= 0 {
		common.ApiErrorMsg(c, "参数错误")
		return
	}
	msg, err := model.AdminBindSubscription(req.UserId, req.PlanId, "")
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if msg != "" {
		common.ApiSuccess(c, gin.H{"message": msg})
		return
	}
	common.ApiSuccess(c, nil)
}

// ---- Admin: user subscription management ----

func AdminListUserSubscriptions(c *gin.Context) {
	userId, _ := strconv.Atoi(c.Param("id"))
	if userId <= 0 {
		common.ApiErrorMsg(c, "无效的用户ID")
		return
	}
	subs, err := model.GetAllUserSubscriptions(userId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, subs)
}

type AdminCreateUserSubscriptionRequest struct {
	PlanId int `json:"plan_id"`
}

type AdminResetSubscriptionRequest struct {
	PlanId           int   `json:"plan_id"`
	AdvanceResetTime *bool `json:"advance_reset_time"`
}

func resolveAdvanceResetTime(value *bool) bool {
	return value != nil && *value
}

func recordSubscriptionResetUserLogs(source model.LogSource, result *model.SubscriptionResetResult, adminInfo map[string]interface{}) {
	if result == nil || result.ResetCount == 0 {
		return
	}
	content := fmt.Sprintf("管理员重置订阅套餐 %s（ID: %d）额度", result.PlanTitle, result.PlanId)
	for _, userId := range result.AffectedUserIds {
		model.RecordLogWithAdminInfo(source, userId, model.LogTypeManage, content, adminInfo)
	}
}

// AdminCreateUserSubscription creates a new user subscription from a plan (no payment).
func AdminCreateUserSubscription(c *gin.Context) {
	userId, _ := strconv.Atoi(c.Param("id"))
	if userId <= 0 {
		common.ApiErrorMsg(c, "无效的用户ID")
		return
	}
	var req AdminCreateUserSubscriptionRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.PlanId <= 0 {
		common.ApiErrorMsg(c, "参数错误")
		return
	}
	msg, err := model.AdminBindSubscription(userId, req.PlanId, "")
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if msg != "" {
		common.ApiSuccess(c, gin.H{"message": msg})
		return
	}
	common.ApiSuccess(c, nil)
}

func AdminResetUserSubscriptionsByPlan(c *gin.Context) {
	userId, _ := strconv.Atoi(c.Param("id"))
	if userId <= 0 {
		common.ApiErrorMsg(c, "无效的用户ID")
		return
	}
	var req AdminResetSubscriptionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorMsg(c, "参数错误")
		return
	}
	if req.PlanId <= 0 {
		common.ApiErrorMsg(c, "参数错误")
		return
	}
	advanceResetTime := resolveAdvanceResetTime(req.AdvanceResetTime)
	result, err := model.AdminResetUserSubscriptionsByPlan(userId, req.PlanId, advanceResetTime)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	recordSubscriptionResetUserLogs(model.ClientLogSource(c), result, auditOperatorInfo(c))
	recordManageAuditFor(c, userId, "subscription.user_plan_reset", map[string]interface{}{
		"target_user_id":     userId,
		"plan_id":            result.PlanId,
		"plan_title":         result.PlanTitle,
		"reset_count":        result.ResetCount,
		"user_count":         result.UserCount,
		"advance_reset_time": result.AdvanceResetTime,
	})
	common.ApiSuccess(c, result)
}

func AdminResetPlanSubscriptions(c *gin.Context) {
	planId, _ := strconv.Atoi(c.Param("id"))
	if planId <= 0 {
		common.ApiErrorMsg(c, "无效的ID")
		return
	}
	var req AdminResetSubscriptionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorMsg(c, "参数错误")
		return
	}
	advanceResetTime := resolveAdvanceResetTime(req.AdvanceResetTime)
	result, err := model.AdminResetPlanSubscriptions(planId, advanceResetTime)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	recordSubscriptionResetUserLogs(model.ClientLogSource(c), result, auditOperatorInfo(c))
	common.SysLog(fmt.Sprintf("admin reset subscription plan %d quota: reset_count=%d user_count=%d advance_reset_time=%t",
		result.PlanId, result.ResetCount, result.UserCount, result.AdvanceResetTime))
	recordManageAudit(c, "subscription.plan_reset", map[string]interface{}{
		"plan_id":            result.PlanId,
		"plan_title":         result.PlanTitle,
		"reset_count":        result.ResetCount,
		"user_count":         result.UserCount,
		"advance_reset_time": result.AdvanceResetTime,
	})
	common.ApiSuccess(c, result)
}

// AdminInvalidateUserSubscription cancels a user subscription immediately.
func AdminInvalidateUserSubscription(c *gin.Context) {
	subId, _ := strconv.Atoi(c.Param("id"))
	if subId <= 0 {
		common.ApiErrorMsg(c, "无效的订阅ID")
		return
	}
	msg, err := model.AdminInvalidateUserSubscription(subId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if msg != "" {
		common.ApiSuccess(c, gin.H{"message": msg})
		return
	}
	common.ApiSuccess(c, nil)
}

// AdminDeleteUserSubscription hard-deletes a user subscription.
func AdminDeleteUserSubscription(c *gin.Context) {
	subId, _ := strconv.Atoi(c.Param("id"))
	if subId <= 0 {
		common.ApiErrorMsg(c, "无效的订阅ID")
		return
	}
	msg, err := model.AdminDeleteUserSubscription(subId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if msg != "" {
		common.ApiSuccess(c, gin.H{"message": msg})
		return
	}
	common.ApiSuccess(c, nil)
}
