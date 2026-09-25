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
	"github.com/QuantumNous/new-api/pkg/slots"
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

// gameCenterDay 是游戏中心的记账日：全站提现额度和每人每天的游戏奖励都按它计算，
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

// GetGameCenterSelf 返回当前用户的游戏币账户、兑换比例、提现规则、已开放的游戏和各自今天的奖励进度，
// 以及开局是否需要人机验证。
func GetGameCenterSelf(c *gin.Context) {
	userId := c.GetInt("id")
	day := gameCenterDay()
	account, err := model.GetGameCoinAccount(userId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	used, err := model.GetGameCoinWithdrawDayUsed(day)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	quotaPerCoin, err := model.GameCoinQuota(1)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	setting := operation_setting.GetGameSetting()
	games := []gin.H{}
	if setting.Tetris.Enabled {
		earned, err := model.GetGameRewardEarned(userId, gameTetris, day)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		best, err := model.GetGameBestScore(userId, gameTetris)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		games = append(games, gin.H{
			"key":                  gameTetris,
			"daily_reward_limit":   setting.Tetris.DailyRewardLimit,
			"score_per_coin":       setting.Tetris.ScorePerCoin,
			"min_seconds_per_coin": setting.Tetris.MinSecondsPerCoin,
			"earned_today":         earned,
			"best_score":           best,
		})
	}
	if setting.Slots.Enabled {
		earned, err := model.GetGameRewardEarned(userId, gameSlots, day)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		ipEarned, err := model.GetGameRewardIpEarned(gameRewardIpKey(c.ClientIP()), gameSlots, day)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		best, err := model.GetGameBestScore(userId, gameSlots)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		paid, combos := slots.Payback()
		// 老虎机的 earned_today、ip_earned_today 是账号、同一 IP 今天的净赢，可以为负，daily_reward_limit 为 0 表示不限制；
		// best_score 是单次最高的实际派彩。转轮和赔付表随游戏信息下发，浏览器按它们画出转轮，把转轮转到服务端给出的停点。
		games = append(games, gin.H{
			"key":                gameSlots,
			"daily_reward_limit": setting.Slots.DailyRewardLimit,
			"max_bet":            setting.Slots.MaxBet,
			"earned_today":       earned,
			"ip_earned_today":    ipEarned,
			"best_score":         best,
			"reels":              slots.Strips,
			"paytable":           slots.Paytable,
			"payback":            float64(paid) / float64(combos),
		})
	}
	_, _, walletErr := service.GameHotWalletAddress()
	common.ApiSuccess(c, gin.H{
		"account":        account,
		"quota_per_coin": quotaPerCoin,
		"games":          games,
		"play_turnstile": gin.H{
			"required": setting.PlayTurnstileRequired(),
			"site_key": common.TurnstileSiteKey,
		},
		"token": gin.H{
			"symbol":       setting.TokenSymbol,
			"address":      setting.TokenAddress,
			"decimals":     setting.TokenDecimals,
			"chain_id":     setting.ChainId,
			"explorer_url": setting.ExplorerUrl,
		},
		"withdraw": gin.H{
			"enabled":         setting.WithdrawEnabled && setting.ChainConfigured() && walletErr == nil,
			"min_amount":      setting.WithdrawMinAmount,
			"max_amount":      setting.WithdrawMaxAmount,
			"daily_limit":     setting.WithdrawDailyLimit,
			"daily_remaining": max(setting.WithdrawDailyLimit-used, 0),
		},
	})
}

// ConvertGameCoins 把游戏币单向兑换为主额度，1 币 = 1 美元额度，不设上限。
func ConvertGameCoins(c *gin.Context) {
	var req gameCoinConvertRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil || req.Amount < 1 {
		common.ApiErrorI18n(c, i18n.MsgGameCoinAmountInvalid)
		return
	}
	userId := c.GetInt("id")
	quota, balance, err := model.ConvertGameCoinsToQuota(userId, req.Amount)
	if err != nil {
		switch {
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
	var req gameCoinWithdrawRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil || req.Amount < 1 {
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
	if err != nil || address == gameZeroAddress || strings.EqualFold(address, setting.TokenAddress) || address == hotWallet {
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
	if !middleware.RequireSecurityProof(c, "game.withdraw", strings.ToLower(address)+":"+strconv.Itoa(req.Amount)) {
		return
	}

	day := gameCenterDay()
	withdrawal, balance, err := model.CreateGameCoinWithdrawal(userId, address, req.Amount, day, setting.WithdrawDailyLimit)
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
		"setting":    operation_setting.GetGameSetting(),
		"hot_wallet": gameHotWalletInfo(),
	})
}

// UpdateGameAdminSetting 保存游戏中心配置里请求带上的字段，在一个事务里写入。管理页可能是很久以前打开的，
// 整份回写会把别人在这期间改过的字段(例如紧急关闭的提现)悄悄改回去，所以只写改动过的字段；单次提现的
// 上下限这类跨字段约束按合并到当前配置之后的整份配置校验。
func UpdateGameAdminSetting(c *gin.Context) {
	var changes map[string]json.RawMessage
	if err := common.DecodeJson(c.Request.Body, &changes); err != nil || len(changes) == 0 {
		common.ApiErrorMsg(c, "无效的参数")
		return
	}
	setting := *operation_setting.GetGameSetting()
	body, err := common.Marshal(changes)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if err := common.Unmarshal(body, &setting); err != nil {
		common.ApiErrorMsg(c, "无效的参数")
		return
	}
	setting.RpcUrl = strings.TrimSpace(setting.RpcUrl)
	setting.ExplorerUrl = strings.TrimRight(strings.TrimSpace(setting.ExplorerUrl), "/")
	setting.TokenSymbol = strings.TrimSpace(setting.TokenSymbol)
	setting.TokenAddress = strings.TrimSpace(setting.TokenAddress)
	if setting.TokenAddress != "" {
		normalized, err := service.NormalizeEthereumAddress(setting.TokenAddress)
		if err != nil {
			common.ApiErrorMsg(c, "代币合约地址无效："+err.Error())
			return
		}
		setting.TokenAddress = normalized
	}
	if err := operation_setting.ValidateGameSetting(&setting); err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	values, err := config.ConfigToMap(&setting)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	options := make(map[string]string, len(changes))
	fields := make([]string, 0, len(changes))
	for key := range changes {
		value, ok := values[key]
		if !ok {
			common.ApiErrorMsg(c, "未知的游戏中心配置项："+key)
			return
		}
		options[operation_setting.GameSettingPrefix+key] = value
		fields = append(fields, key)
	}
	if err := model.UpdateOptionsBulk(options); err != nil {
		common.ApiError(c, err)
		return
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
