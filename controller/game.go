package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

const gameZeroAddress = "0x0000000000000000000000000000000000000000"

type gameCoinConvertRequest struct {
	Amount int `json:"amount"`
}

type gameCoinWithdrawRequest struct {
	Address string `json:"address"`
	Amount  int    `json:"amount"`
}

type gameCoinAdjustRequest struct {
	UserId int    `json:"user_id"`
	Amount int    `json:"amount"`
	Remark string `json:"remark"`
}

type gameHotWalletRequest struct {
	PrivateKey string `json:"private_key"`
}

// gameCenterDay 是游戏中心的记账日：个人兑换限额、全站提现额度和每日游戏奖励都按它计算，
// 与签到一样按服务器本地时间 0 点切换。
func gameCenterDay() string {
	return time.Now().Format("2006-01-02")
}

// gameRewardIpKey 是每日上限按 IP 计数时用的键。IPv6 客户端能在运营商分配的整段前缀里随意换地址，
// 与自动封禁一样归并到风控设置的前缀。
func gameRewardIpKey(ip string) string {
	key, err := model.NormalizeAutoBanTarget(ip, operation_setting.GetRiskControlSetting().ResolvedIpBanIpv6PrefixLength())
	if err != nil {
		return ip
	}
	return key
}

// passGamePlayTurnstile 在要求人机验证时校验请求带的 Turnstile 令牌(查询参数 turnstile)，没通过时写好错误响应并返回 false。
// 白送游戏币的操作(俄罗斯方块开局、大转盘的免费旋转)每次都要带一个新令牌，不经浏览器直接调接口的脚本领不到。
func passGamePlayTurnstile(c *gin.Context, setting *operation_setting.GameSetting, userId int) bool {
	if !setting.PlayTurnstileRequired() {
		return true
	}
	token := c.Query("turnstile")
	if token == "" {
		common.ApiErrorI18n(c, i18n.MsgGameTurnstileRequired)
		return false
	}
	if err := service.VerifyTurnstile(token, c.ClientIP()); err != nil {
		if !errors.Is(err, service.ErrTurnstileRejected) {
			common.SysError(fmt.Sprintf("failed to verify turnstile for user %d: %v", userId, err))
		}
		common.ApiErrorI18n(c, i18n.MsgGameTurnstileFailed)
		return false
	}
	return true
}

// GetGameCenterSelf 返回当前用户的游戏币账户、个人兑换限额、全站提现规则、技巧与人机奖励合计，以及每个开放游戏的今日进度。
// 各游戏的规则与详细进度由游戏页自己的接口 GET /api/game/<key> 返回。游戏中心关闭时，只给还押着游戏币、没结束一局的玩家
// 列出那几个游戏(closed 为 true)，让他回去把它玩完；兑换与提现这时都不开放。
func GetGameCenterSelf(c *gin.Context) {
	ctx := newGameContext(c)
	games, counts, err := gameSummaries(ctx)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if !ctx.Setting.Enabled && len(games) == 0 {
		common.ApiErrorI18n(c, i18n.MsgGameCenterDisabled)
		return
	}
	account, err := model.GetGameCoinAccount(ctx.UserId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	used, err := model.GetGameCoinWithdrawDayUsed(ctx.Day)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	converted, err := model.GetGameCoinConvertDayUsed(ctx.UserId, ctx.Day)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	quotaPerCoin, err := model.GameCoinQuota(1)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	setting := ctx.Setting
	_, _, walletErr := service.GameHotWalletAddress()
	common.ApiSuccess(c, gin.H{
		"closed":         !setting.Enabled,
		"account":        account,
		"quota_per_coin": quotaPerCoin,
		"games":          games,
		// 兑换按个人每日累计，1 币对应 1 美元 API 额度，与全站提现池互不占用。
		"convert": gin.H{
			"daily_limit":     model.GameCoinConvertDailyLimit,
			"daily_used":      converted,
			"daily_remaining": max(model.GameCoinConvertDailyLimit-converted, 0),
		},
		// 技巧与人机奖励的每日合计上限(0 表示不限制)，不包含大转盘奖金。
		"free": gin.H{
			"daily_limit":     setting.FreeDailyLimit,
			"earned_today":    counts.user[model.GameFreeCounter].Coins,
			"ip_earned_today": counts.ip[model.GameFreeCounter].Coins,
		},
		"token": gin.H{
			"symbol":       setting.TokenSymbol,
			"address":      setting.TokenAddress,
			"decimals":     setting.TokenDecimals,
			"chain_id":     setting.ChainId,
			"explorer_url": setting.ExplorerUrl,
		},
		"withdraw": gin.H{
			"enabled":         setting.Enabled && setting.WithdrawEnabled && setting.ChainConfigured() && walletErr == nil,
			"min_amount":      setting.WithdrawMinAmount,
			"max_amount":      setting.WithdrawMaxAmount,
			"daily_limit":     setting.WithdrawDailyLimit,
			"daily_remaining": max(setting.WithdrawDailyLimit-used, 0),
		},
	})
}

// ConvertGameCoins 把游戏币单向兑换为主额度，1 币 = 1 美元额度，每人每天最多兑换 15 美元额度。
func ConvertGameCoins(c *gin.Context) {
	var req gameCoinConvertRequest
	if err := decodeGameRequest(c, &req); err != nil || req.Amount < 1 {
		common.ApiErrorI18n(c, i18n.MsgGameCoinAmountInvalid)
		return
	}
	userId := c.GetInt("id")
	quota, balance, err := model.ConvertGameCoinsToQuota(userId, req.Amount)
	if err != nil {
		switch {
		case errors.Is(err, model.ErrGameCoinConvertDailyLimit):
			common.ApiErrorI18n(c, i18n.MsgGameCoinConvertDailyLimit, map[string]any{"Limit": model.GameCoinConvertDailyLimit})
		case errors.Is(err, model.ErrGameCoinInsufficient):
			common.ApiErrorI18n(c, i18n.MsgGameCoinInsufficient)
		case errors.Is(err, model.ErrGameCoinAmountInvalid):
			common.ApiErrorI18n(c, i18n.MsgGameCoinAmountInvalid)
		case errors.Is(err, model.ErrQuotaOutOfRange):
			common.ApiErrorI18n(c, i18n.MsgQuotaExceedMax)
		default:
			common.SysError(fmt.Sprintf("game coin convert failed for user %d: %v", userId, err))
			common.ApiErrorI18n(c, i18n.MsgGameCoinConvertFailed)
		}
		return
	}
	model.RecordAuditLog(model.ClientLogSource(c), userId, "", model.LogTypeSystem,
		fmt.Sprintf("游戏币兑换额度：%d 个游戏币，获得额度 %s", req.Amount, logger.LogQuota(quota)),
		map[string]interface{}{"game_coins": req.Amount, "quota": quota})
	common.ApiSuccess(c, gin.H{"quota": quota, "balance": balance})
}

// WithdrawGameCoins 创建一笔链上提现：扣币并占用当日额度后交给提现 worker 发送。
// 游戏币要离开站点，必须先通过安全验证，验证绑定收款地址与数量。
func WithdrawGameCoins(c *gin.Context) {
	setting := operation_setting.GetGameSetting()
	if !setting.WithdrawEnabled {
		common.ApiErrorI18n(c, i18n.MsgGameWithdrawDisabled)
		return
	}
	hotWallet, _, err := service.GameHotWalletAddress()
	if err != nil || !setting.ChainConfigured() {
		common.ApiErrorI18n(c, i18n.MsgGameWithdrawUnavailable)
		return
	}
	tokenAddress, err := service.NormalizeEthereumAddress(setting.TokenAddress)
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgGameWithdrawUnavailable)
		return
	}
	var req gameCoinWithdrawRequest
	if err := decodeGameRequest(c, &req); err != nil || req.Amount < 1 {
		common.ApiErrorI18n(c, i18n.MsgGameCoinAmountInvalid)
		return
	}
	if req.Amount < setting.WithdrawMinAmount {
		common.ApiErrorI18n(c, i18n.MsgGameWithdrawBelowMin, map[string]any{"Min": setting.WithdrawMinAmount})
		return
	}
	if req.Amount > setting.WithdrawMaxAmount {
		common.ApiErrorI18n(c, i18n.MsgGameWithdrawAboveMax, map[string]any{"Max": setting.WithdrawMaxAmount})
		return
	}
	// 转给零地址或代币合约自身等于销毁，转回热钱包没有意义，都当作无效地址。
	address, err := service.NormalizeEthereumAddress(req.Address)
	if err != nil || address == gameZeroAddress || address == tokenAddress || address == hotWallet {
		common.ApiErrorI18n(c, i18n.MsgGameWithdrawAddressInvalid)
		return
	}

	userId := c.GetInt("id")
	// 先挡掉余额不够的请求，免得白白消耗一次安全验证；真正的余额检查在扣币的条件更新里。
	account, err := model.GetGameCoinAccount(userId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if account.Balance < req.Amount {
		common.ApiErrorI18n(c, i18n.MsgGameCoinInsufficient)
		return
	}
	// 全站当日额度同样先查一次：页面上的剩余额度可能已经被别人用掉，验证之后才被拒绝的话，验证白做、重试还要再验证。
	// 真正的额度检查在占用额度的条件更新里。
	day := gameCenterDay()
	used, err := model.GetGameCoinWithdrawDayUsed(day)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if used+req.Amount > setting.WithdrawDailyLimit {
		common.ApiErrorI18n(c, i18n.MsgGameWithdrawDailyLimit, map[string]any{"Remaining": max(setting.WithdrawDailyLimit-used, 0)})
		return
	}
	if !middleware.RequireSecurityProof(c, "game.withdraw", strings.ToLower(address)+":"+strconv.Itoa(req.Amount)) {
		return
	}

	// 记下现在的链、合约与精度：之后管理员换了配置，这笔提现也不会按另一种代币或另一个数量发出去。
	withdrawal := &model.GameCoinWithdrawal{
		UserId:        userId,
		ToAddress:     address,
		Amount:        req.Amount,
		Day:           day,
		ChainId:       setting.ChainId,
		TokenAddress:  tokenAddress,
		TokenDecimals: setting.TokenDecimals,
	}
	balance, err := model.CreateGameCoinWithdrawal(withdrawal, setting.WithdrawDailyLimit)
	if err != nil {
		switch {
		case errors.Is(err, model.ErrGameCoinInsufficient):
			common.ApiErrorI18n(c, i18n.MsgGameCoinInsufficient)
		case errors.Is(err, model.ErrGameCoinDailyLimit):
			used, _ := model.GetGameCoinWithdrawDayUsed(day)
			common.ApiErrorI18n(c, i18n.MsgGameWithdrawDailyLimit, map[string]any{"Remaining": max(setting.WithdrawDailyLimit-used, 0)})
		case errors.Is(err, model.ErrGameCoinAmountInvalid):
			common.ApiErrorI18n(c, i18n.MsgGameCoinAmountInvalid)
		default:
			common.SysError(fmt.Sprintf("game coin withdrawal failed for user %d: %v", userId, err))
			common.ApiErrorI18n(c, i18n.MsgGameWithdrawFailed)
		}
		return
	}
	common.ApiSuccess(c, gin.H{"withdrawal": withdrawal, "balance": balance})
}

func GetGameCoinLogs(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	logs, total, err := model.GetUserGameCoinLogs(c.GetInt("id"), c.Query("type"), pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(logs)
	common.ApiSuccess(c, pageInfo)
}

func GetGameCoinWithdrawals(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	withdrawals, total, err := model.GetUserGameCoinWithdrawals(c.GetInt("id"), pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(withdrawals)
	common.ApiSuccess(c, pageInfo)
}

// gameHotWalletInfo 描述热钱包是否可用。私钥只写不读，这里只给出地址、私钥来源与错误原因。
func gameHotWalletInfo() gin.H {
	address, source, err := service.GameHotWalletAddress()
	if err != nil {
		return gin.H{"configured": false, "source": source, "env": service.GameHotWalletPrivateKeyEnv, "error": err.Error()}
	}
	return gin.H{"configured": true, "source": source, "env": service.GameHotWalletPrivateKeyEnv, "address": address}
}

func GetGameAdminSetting(c *gin.Context) {
	common.ApiSuccess(c, gin.H{
		"setting":         operation_setting.GetGameSetting(),
		"hot_wallet":      gameHotWalletInfo(),
		"max_tier_scores": operation_setting.GameMaxTierScores(),
	})
}

// UpdateGameAdminSetting 保存管理员改动的几项配置。请求只带改动过的项，游戏规则也可以只带改动过的子字段：它们合并进数据库里
// 现在的配置，整份校验之后只写回这几项，别人刚改过的其他配置不会被这次保存改回去。
func UpdateGameAdminSetting(c *gin.Context) {
	var changes map[string]json.RawMessage
	if err := common.DecodeJson(c.Request.Body, &changes); err != nil || len(changes) == 0 {
		common.ApiErrorMsg(c, "无效的参数")
		return
	}
	body, err := common.Marshal(changes)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	// rejected 是给管理员看的校验失败原因；回调返回错误时什么都不写。
	rejected := ""
	errRejected := errors.New("game setting rejected")
	err = model.UpdateGameSettingFields(func(setting *operation_setting.GameSetting) (map[string]string, error) {
		if err := common.Unmarshal(body, setting); err != nil {
			rejected = "无效的参数"
			return nil, errRejected
		}
		setting.RpcUrl = strings.TrimSpace(setting.RpcUrl)
		setting.ExplorerUrl = strings.TrimRight(strings.TrimSpace(setting.ExplorerUrl), "/")
		setting.TokenSymbol = strings.TrimSpace(setting.TokenSymbol)
		setting.TokenAddress = strings.TrimSpace(setting.TokenAddress)
		if setting.TokenAddress != "" {
			normalized, err := service.NormalizeEthereumAddress(setting.TokenAddress)
			if err != nil {
				rejected = "代币合约地址无效：" + err.Error()
				return nil, errRejected
			}
			setting.TokenAddress = normalized
		}
		if err := operation_setting.ValidateGameSetting(setting); err != nil {
			rejected = err.Error()
			return nil, errRejected
		}
		values, err := config.ConfigToMap(setting)
		if err != nil {
			return nil, err
		}
		options := make(map[string]string, len(changes))
		for key := range changes {
			value, ok := values[key]
			if !ok {
				rejected = "未知的游戏中心配置项：" + key
				return nil, errRejected
			}
			options[operation_setting.GameSettingPrefix+key] = value
		}
		return options, nil
	})
	if rejected != "" {
		common.ApiErrorMsg(c, rejected)
		return
	}
	if err != nil {
		common.ApiError(c, err)
		return
	}
	fields := make([]string, 0, len(changes))
	for key := range changes {
		fields = append(fields, key)
	}
	sort.Strings(fields)
	recordManageAudit(c, "game.setting_update", map[string]interface{}{"fields": strings.Join(fields, ", ")})
	common.ApiSuccess(c, operation_setting.GetGameSetting())
}

// UpdateGameAdminHotWallet 保存管理员填写的热钱包私钥，之后优先于环境变量生效。私钥只写不读：
// 响应里只有它对应的地址，管理员据此核对填的是哪个钱包、该往哪里补代币和 gas。
// 已签名的提现带着原发送地址和原始交易，换钱包不影响它们继续对账。
func UpdateGameAdminHotWallet(c *gin.Context) {
	var req gameHotWalletRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorMsg(c, "无效的参数")
		return
	}
	key := strings.TrimSpace(req.PrivateKey)
	address, err := service.GameHotWalletAddressOf(key)
	if err != nil {
		common.ApiErrorMsg(c, "私钥无效，请填写 64 位十六进制私钥，可带 0x 前缀")
		return
	}
	if err := model.UpdateOption(operation_setting.GameHotWalletPrivateKeyOption, strings.ToLower(strings.TrimPrefix(key, "0x"))); err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAudit(c, "game.hot_wallet_update", map[string]interface{}{"address": address})
	common.ApiSuccess(c, gameHotWalletInfo())
}

// ClearGameAdminHotWallet 清除后台填写的私钥。设置了环境变量时改用环境变量里的私钥，
// 否则提现 worker 暂停，直到重新填写。
func ClearGameAdminHotWallet(c *gin.Context) {
	if err := model.UpdateOption(operation_setting.GameHotWalletPrivateKeyOption, ""); err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAudit(c, "game.hot_wallet_clear", nil)
	common.ApiSuccess(c, gameHotWalletInfo())
}

// GetGameAdminOverview 汇总热钱包余额、今日提现额度、未完成的提现单与今日各游戏发出的奖励，
// 供管理员判断是否需要补币、补 gas 或调整奖励规则。
func GetGameAdminOverview(c *gin.Context) {
	setting := operation_setting.GetGameSetting()
	day := gameCenterDay()
	used, err := model.GetGameCoinWithdrawDayUsed(day)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	rewards, err := model.SumGameRewardsByGame(day)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	statusCounts, err := model.CountGameCoinWithdrawalsByStatus()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	hotWallet := gameHotWalletInfo()
	if hotWallet["configured"] == true && setting.ChainConfigured() {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
		defer cancel()
		token, eth, err := service.GameHotWalletBalances(ctx, setting)
		if err != nil {
			hotWallet["balance_error"] = err.Error()
		} else {
			hotWallet["token_balance"] = decimal.NewFromBigInt(token, -int32(setting.TokenDecimals)).String()
			hotWallet["eth_balance"] = decimal.NewFromBigInt(eth, -18).StringFixed(6)
		}
	}
	common.ApiSuccess(c, gin.H{
		"day":            day,
		"withdraw_used":  used,
		"withdraw_limit": setting.WithdrawDailyLimit,
		"status_counts":  statusCounts,
		"hot_wallet":     hotWallet,
		"game_rewards":   rewards,
	})
}

func GetGameAdminWithdrawals(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	userId, _ := strconv.Atoi(c.Query("user_id"))
	withdrawals, total, err := model.GetAllGameCoinWithdrawals(c.Query("status"), userId, pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(withdrawals)
	common.ApiSuccess(c, pageInfo)
}

func GetGameAdminAccounts(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	userId, _ := strconv.Atoi(c.Query("user_id"))
	accounts, total, err := model.GetGameCoinAccounts(userId, pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(accounts)
	common.ApiSuccess(c, pageInfo)
}

// AdjustGameAdminCoins 让管理员给用户加减游戏币，用于补偿、活动发放或测试提现。
func AdjustGameAdminCoins(c *gin.Context) {
	var req gameCoinAdjustRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil || req.UserId < 1 || req.Amount == 0 {
		common.ApiErrorI18n(c, i18n.MsgGameCoinAmountInvalid)
		return
	}
	remark := strings.TrimSpace(req.Remark)
	if utf8.RuneCountInString(remark) > 255 {
		common.ApiErrorMsg(c, "备注最多 255 个字符")
		return
	}
	balance, err := model.AdjustGameCoins(req.UserId, req.Amount, remark)
	if err != nil {
		switch {
		case errors.Is(err, model.ErrGameCoinUserNotFound):
			common.ApiErrorI18n(c, i18n.MsgGameUserNotFound)
		case errors.Is(err, model.ErrGameCoinInsufficient):
			common.ApiErrorI18n(c, i18n.MsgGameCoinInsufficient)
		case errors.Is(err, model.ErrGameCoinAmountInvalid):
			common.ApiErrorI18n(c, i18n.MsgGameCoinAmountInvalid)
		default:
			common.SysError(fmt.Sprintf("game coin adjust failed for user %d: %v", req.UserId, err))
			common.ApiErrorI18n(c, i18n.MsgGameAdjustFailed)
		}
		return
	}
	recordManageAuditFor(c, req.UserId, "game.coin_adjust", map[string]interface{}{
		"amount": req.Amount,
		"remark": remark,
	})
	common.ApiSuccess(c, gin.H{"balance": balance})
}
