package controller

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	"github.com/thanhpk/randstr"
)

// ── Keccak256 topic hash for PaymentReceived event ─────────────────────────
// PaymentReceived(bytes32 indexed orderId, address indexed payer, address token, uint256 amount)
// Pre-computed with: cast keccak "PaymentReceived(bytes32,address,address,uint256)"
const paymentReceivedTopicHex = "0x1c517e85acdede9b6dbdaab4925d20d3551f2961e9a860e72658e1769f150322"

// ── Request / response types ───────────────────────────────────────────────

// EthereumPayRequest is the JSON body for POST /api/user/ethereum/pay
type EthereumPayRequest struct {
	Amount       int64  `json:"amount"`        // top-up units
	TokenAddress string `json:"token_address"` // "0x000...000" for ETH, or ERC-20 address
}

// EthereumSubscriptionPayRequest is the JSON body for POST /api/subscription/ethereum/pay
type EthereumSubscriptionPayRequest struct {
	PlanId       int    `json:"plan_id"`
	TokenAddress string `json:"token_address"`
}

// EthereumPayResponse is returned to the frontend so it can call the contract.
type EthereumPayResponse struct {
	OrderId         string `json:"order_id"` // bytes32 hex string (0x-prefixed, 66 chars)
	ContractAddress string `json:"contract_address"`
	ChainId         int64  `json:"chain_id"`
	TokenAddress    string `json:"token_address"` // address(0) or ERC-20
	PayAmount       string `json:"pay_amount"`    // in smallest unit (wei / token decimals) as decimal string
	Symbol          string `json:"symbol"`
	Decimals        int    `json:"decimals"`
}

// ── Alchemy webhook types ──────────────────────────────────────────────────

// alchemyWebhookPayload is a subset of the Alchemy Custom Webhook JSON.
type alchemyWebhookPayload struct {
	WebhookID string `json:"webhookId"`
	ID        string `json:"id"`
	Event     struct {
		Network string `json:"network"`
		Data    struct {
			Block struct {
				Logs []alchemyLog `json:"logs"`
			} `json:"block"`
		} `json:"data"`
	} `json:"event"`
}

type alchemyLog struct {
	Topics  []string `json:"topics"`
	Data    string   `json:"data"`
	Account struct {
		Address string `json:"address"`
	} `json:"account"`
	Transaction struct {
		Hash string `json:"hash"`
	} `json:"transaction"`
}

// alchemyNetworkChainIds maps Alchemy's network identifiers to their EVM chain id.
//
// A contract address alone does not identify a chain: CREATE derives it from
// keccak(deployer, nonce), so the same deploying wallet reaches the same address
// on every EVM chain. Without this check, a webhook for a testnet deployment is
// indistinguishable from the mainnet one and free testnet funds would settle
// real orders.
var alchemyNetworkChainIds = map[string]int64{
	"ETH_MAINNET":    1,
	"ETH_SEPOLIA":    11155111,
	"ETH_HOLESKY":    17000,
	"ETH_HOODI":      560048,
	"MATIC_MAINNET":  137,
	"MATIC_AMOY":     80002,
	"ARB_MAINNET":    42161,
	"ARB_SEPOLIA":    421614,
	"OPT_MAINNET":    10,
	"OPT_SEPOLIA":    11155420,
	"BASE_MAINNET":   8453,
	"BASE_SEPOLIA":   84532,
	"BNB_MAINNET":    56,
	"BNB_TESTNET":    97,
	"AVAX_MAINNET":   43114,
	"AVAX_FUJI":      43113,
	"LINEA_MAINNET":  59144,
	"LINEA_SEPOLIA":  59141,
	"SCROLL_MAINNET": 534352,
	"SCROLL_SEPOLIA": 534351,
	"GNOSIS_MAINNET": 100,
	"ZKSYNC_MAINNET": 324,
	"ZKSYNC_SEPOLIA": 300,
}

// isConfiguredChainNetwork reports whether a webhook's network matches the
// configured chain. An unrecognised network fails closed; an absent one is
// allowed through with a warning, because older webhook versions omit the field
// and rejecting it would silently break every payment.
func isConfiguredChainNetwork(network string) bool {
	network = strings.ToUpper(strings.TrimSpace(network))
	if network == "" {
		common.SysLog(fmt.Sprintf("Ethereum Webhook: payload 未携带 network 字段，跳过链校验（期望 chainId=%d）",
			setting.EthereumChainId))
		return true
	}
	chainId, known := alchemyNetworkChainIds[network]
	if !known {
		common.SysLog(fmt.Sprintf("Ethereum Webhook: 未知网络 %s，拒绝处理（如需支持请补充 alchemyNetworkChainIds 映射）", network))
		return false
	}
	if chainId != setting.EthereumChainId {
		common.SysLog(fmt.Sprintf("Ethereum Webhook: 网络不匹配，拒绝处理 - payload=%s(chainId=%d), 配置 chainId=%d",
			network, chainId, setting.EthereumChainId))
		return false
	}
	return true
}

// ── RequestEthereumPay ─────────────────────────────────────────────────────

// RequestEthereumPay handles POST /api/user/ethereum/pay (UserAuth required).
func RequestEthereumPay(c *gin.Context) {
	if !setting.EthereumEnabled {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "Ethereum 支付未启用"})
		return
	}
	if setting.EthereumContractAddress == "" {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "合约地址未配置"})
		return
	}

	var req EthereumPayRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "参数错误"})
		return
	}
	if req.Amount <= 0 {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "参数错误"})
		return
	}

	// req.Amount arrives in whatever unit the frontend displays: top-up units in
	// currency mode, raw tokens in token mode. Token prices and TopUp.Amount are
	// both denominated in top-up units, so normalise once here and derive both
	// the charge and the credited amount from the same value — pricing the raw
	// token count would overcharge the payer by a factor of QuotaPerUnit.
	units := decimal.NewFromInt(req.Amount)
	if operation_setting.GetQuotaDisplayType() == operation_setting.QuotaDisplayTypeTokens {
		units = units.Div(decimal.NewFromFloat(common.QuotaPerUnit))
	}
	// Settlement re-multiplies the stored whole-unit amount by QuotaPerUnit, so a
	// fractional remainder can never be credited. Truncate before pricing rather
	// than rounding a sub-unit request up to a full unit.
	amount := units.Truncate(0).IntPart()
	if amount < int64(setting.EthereumMinTopUp) {
		c.JSON(http.StatusOK, gin.H{
			"message": "error",
			"data":    fmt.Sprintf("充值数量不能小于 %d", setting.EthereumMinTopUp),
		})
		return
	}

	// Find matching token config
	tokens := setting.GetEthereumTokens()
	var tokenCfg *setting.EthereumToken
	for i := range tokens {
		if strings.EqualFold(tokens[i].Address, req.TokenAddress) {
			tokenCfg = &tokens[i]
			break
		}
	}
	if tokenCfg == nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "不支持的代币"})
		return
	}

	// Calculate pay amount in smallest unit
	payAmountStr, err := calcPayAmountDecimal(decimal.NewFromInt(amount), tokenCfg.Price, tokenCfg.Decimals)
	if err != nil || payAmountStr == "0" {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "金额计算失败"})
		return
	}

	userId := c.GetInt("id")
	user, err := model.GetUserById(userId, false)
	if err != nil || user == nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "用户不存在"})
		return
	}

	// Generate trade number and derive the on-chain orderId (bytes32).
	// TradeNo format: ETH-{userId}-{unixMilli}-{6randChars} (always < 32 bytes)
	tradeNo := fmt.Sprintf("ETH-%d-%d-%s", userId, time.Now().UnixMilli(), randstr.String(6))
	orderId := tradeNoToOrderId(tradeNo)

	// Parse pay amount as float for Money field
	payMoneyFloat, _ := strconv.ParseFloat(tokenCfg.Price, 64)
	payMoney := payMoneyFloat * float64(amount)

	// Persist pending order
	topUp := &model.TopUp{
		UserId:                userId,
		Amount:                amount,
		Money:                 payMoney,
		TradeNo:               tradeNo,
		PaymentMethod:         "ethereum",
		PaymentProvider:       model.PaymentProviderEthereum,
		ExpectedPaymentToken:  strings.ToLower(tokenCfg.Address),
		ExpectedPaymentAmount: payAmountStr,
		CreateTime:            time.Now().Unix(),
		Status:                common.TopUpStatusPending,
	}
	if err := topUp.Insert(); err != nil {
		common.SysLog(fmt.Sprintf("Ethereum: 创建本地订单失败: %v", err))
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "创建订单失败"})
		return
	}

	common.SysLog(fmt.Sprintf("Ethereum: 订单创建 - userId=%d, tradeNo=%s, token=%s, payAmount=%s",
		userId, tradeNo, tokenCfg.Symbol, payAmountStr))

	c.JSON(http.StatusOK, gin.H{
		"message": "success",
		"data": EthereumPayResponse{
			OrderId:         orderId,
			ContractAddress: setting.EthereumContractAddress,
			ChainId:         setting.EthereumChainId,
			TokenAddress:    tokenCfg.Address,
			PayAmount:       payAmountStr,
			Symbol:          tokenCfg.Symbol,
			Decimals:        tokenCfg.Decimals,
		},
	})
}

// ── EthereumWebhook ────────────────────────────────────────────────────────

// EthereumWebhook handles POST /api/ethereum/webhook (no auth — verified by HMAC).
func EthereumWebhook(c *gin.Context) {
	bodyBytes, err := io.ReadAll(c.Request.Body)
	if err != nil {
		common.SysLog(fmt.Sprintf("Ethereum Webhook: 读取 body 失败: %v", err))
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}

	// Verify Alchemy HMAC-SHA256 signature.
	// This endpoint is unauthenticated by design, so the failure log must never
	// echo the signing key or the expected digest: anyone could otherwise POST a
	// bad signature and harvest the credentials needed to forge paid orders.
	sigHex := c.GetHeader("X-Alchemy-Signature")
	signingKey := strings.TrimSpace(setting.EthereumAlchemyWebhookSigningKey)
	if !verifyAlchemySignature(bodyBytes, sigHex, signingKey) {
		common.SysLog(fmt.Sprintf("Ethereum Webhook: 签名验证失败 - has_signature=%t, key_configured=%t, body_len=%d",
			strings.TrimSpace(sigHex) != "", signingKey != "", len(bodyBytes)))
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}

	common.SysLog(fmt.Sprintf("Ethereum Webhook: 签名验证通过, body_len=%d", len(bodyBytes)))

	var payload alchemyWebhookPayload
	if err := common.Unmarshal(bodyBytes, &payload); err != nil {
		common.SysLog(fmt.Sprintf("Ethereum Webhook: JSON 解析失败: %v", err))
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}

	if !isConfiguredChainNetwork(payload.Event.Network) {
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}

	logs := payload.Event.Data.Block.Logs
	common.SysLog(fmt.Sprintf("Ethereum Webhook: webhookId=%s, network=%s, log_count=%d",
		payload.WebhookID, payload.Event.Network, len(logs)))

	contractAddrLower := strings.ToLower(setting.EthereumContractAddress)
	matched := 0
	for _, logEntry := range logs {
		if strings.ToLower(logEntry.Account.Address) != contractAddrLower {
			common.SysLog(fmt.Sprintf("Ethereum Webhook: 跳过不匹配合约 - log_addr=%s, expect=%s",
				logEntry.Account.Address, contractAddrLower))
			continue
		}
		if len(logEntry.Topics) < 2 {
			common.SysLog(fmt.Sprintf("Ethereum Webhook: 跳过 topics 不足 - topics=%v", logEntry.Topics))
			continue
		}
		// topics[0] = event signature hash
		if !strings.EqualFold(logEntry.Topics[0], paymentReceivedTopicHex) {
			common.SysLog(fmt.Sprintf("Ethereum Webhook: 跳过不匹配事件 - topic0=%s, expect=%s",
				logEntry.Topics[0], paymentReceivedTopicHex))
			continue
		}
		matched++
		handlePaymentReceivedLog(logEntry, model.ClientLogSource(c))
	}

	common.SysLog(fmt.Sprintf("Ethereum Webhook: 处理完成 - matched=%d/%d", matched, len(logs)))
	c.JSON(http.StatusOK, gin.H{"message": "ok"})
}

// handlePaymentReceivedLog processes a single matched log entry.
// It dispatches to the correct completion logic based on the tradeNo prefix:
//   - "ETHSUB-" → subscription order → CompleteSubscriptionOrder
//   - "ETH-"    → top-up order       → RechargeEthereum
func handlePaymentReceivedLog(entry alchemyLog, source model.LogSource) {
	// PaymentReceived(bytes32 indexed orderId, address indexed payer, address token, uint256 amount)
	//
	// Indexed params appear in topics:
	//   topics[1] = orderId (bytes32)
	//   topics[2] = payer   (address, 32 bytes)
	//
	// Non-indexed params are ABI-encoded in data:
	//   data[0:32]  = token  (address)
	//   data[32:64] = amount (uint256)

	if len(entry.Topics) < 2 {
		return
	}

	orderIdHex := entry.Topics[1] // 0x + 64 hex chars
	tradeNo := orderIdToTradeNo(orderIdHex)
	if tradeNo == "" {
		common.SysLog(fmt.Sprintf("Ethereum Webhook: 无法从 orderId 还原 tradeNo: %s", orderIdHex))
		return
	}

	common.SysLog(fmt.Sprintf("Ethereum Webhook: 收到支付事件 - tradeNo=%s, txHash=%s", tradeNo, entry.Transaction.Hash))
	paidToken, paidAmount, err := parsePaymentReceivedData(entry.Data)
	if err != nil {
		common.SysLog(fmt.Sprintf("Ethereum Webhook: 支付事件解析失败 - tradeNo=%s, err=%v", tradeNo, err))
		return
	}

	LockOrder(tradeNo)
	defer UnlockOrder(tradeNo)

	if strings.HasPrefix(tradeNo, "ETHSUB-") {
		// Subscription purchase order
		err = model.CompleteSubscriptionOrderWithPaymentCheck(source, tradeNo, "", model.PaymentProviderEthereum, "ethereum", paidToken, paidAmount)
		if err != nil {
			common.SysLog(fmt.Sprintf("Ethereum Webhook: 订阅订单完成失败 - tradeNo=%s, err=%v", tradeNo, err))
		} else {
			common.SysLog(fmt.Sprintf("Ethereum Webhook: 订阅订单完成成功 - tradeNo=%s", tradeNo))
		}
	} else {
		// Top-up (balance recharge) order
		err = model.RechargeEthereumWithPaymentCheck(source, tradeNo, paidToken, paidAmount)
		if err != nil {
			common.SysLog(fmt.Sprintf("Ethereum Webhook: 充值失败 - tradeNo=%s, err=%v", tradeNo, err))
		} else {
			common.SysLog(fmt.Sprintf("Ethereum Webhook: 充值成功 - tradeNo=%s", tradeNo))
		}
	}
}

// ── RequestEthereumSubscriptionPay ──────────────────────────────────────────

// RequestEthereumSubscriptionPay handles POST /api/subscription/ethereum/pay (UserAuth required).
// It creates a pending SubscriptionOrder with an "ETHSUB-" prefixed tradeNo,
// so that the unified webhook callback can dispatch it to CompleteSubscriptionOrder.
func RequestEthereumSubscriptionPay(c *gin.Context) {
	if !setting.EthereumEnabled {
		common.ApiErrorMsg(c, "Ethereum 支付未启用")
		return
	}
	if setting.EthereumContractAddress == "" {
		common.ApiErrorMsg(c, "合约地址未配置")
		return
	}

	var req EthereumSubscriptionPayRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.PlanId <= 0 {
		common.ApiErrorMsg(c, "参数错误")
		return
	}

	plan, err := model.GetSubscriptionPlanById(req.PlanId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if !plan.Enabled {
		common.ApiErrorMsg(c, "套餐未启用")
		return
	}
	if plan.PriceAmount < 0.01 {
		common.ApiErrorMsg(c, "套餐金额过低")
		return
	}
	// Each token is its own entry in the purchase dialog, so the whitelist is
	// keyed per token address rather than by "ethereum" as a whole.
	if !plan.AllowsPaymentMethod(model.SubscriptionEthereumPayMethod(req.TokenAddress)) {
		common.ApiErrorMsg(c, "该套餐不支持此支付方式")
		return
	}

	userId := c.GetInt("id")

	// Check purchase limit
	if err := model.CheckSubscriptionPlanPurchaseAllowed(userId, plan, true); err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}

	// Find matching token config
	tokens := setting.GetEthereumTokens()
	var tokenCfg *setting.EthereumToken
	for i := range tokens {
		if strings.EqualFold(tokens[i].Address, req.TokenAddress) {
			tokenCfg = &tokens[i]
			break
		}
	}
	if tokenCfg == nil {
		common.ApiErrorMsg(c, "不支持的代币")
		return
	}

	// Calculate pay amount: PriceAmount is the fiat price of the plan.
	// Token price is "how many tokens per 1 top-up unit".
	payAmountStr, err := calcPayAmountDecimal(decimal.NewFromFloat(plan.PriceAmount), tokenCfg.Price, tokenCfg.Decimals)
	if err != nil || payAmountStr == "0" {
		common.ApiErrorMsg(c, "金额计算失败")
		return
	}

	// Generate trade number with ETHSUB- prefix (must fit in 32 bytes)
	// Format: ETHSUB-{userId}-{unixMilli}-{4rand} (always < 32 bytes for reasonable userId)
	tradeNo := fmt.Sprintf("ETHSUB-%d-%d-%s", userId, time.Now().UnixMilli(), randstr.String(4))
	orderId := tradeNoToOrderId(tradeNo)

	// Create pending subscription order
	order := &model.SubscriptionOrder{
		UserId:                userId,
		PlanId:                plan.Id,
		Money:                 plan.PriceAmount,
		TradeNo:               tradeNo,
		PaymentMethod:         "ethereum",
		PaymentProvider:       model.PaymentProviderEthereum,
		ExpectedPaymentToken:  strings.ToLower(tokenCfg.Address),
		ExpectedPaymentAmount: payAmountStr,
		CreateTime:            time.Now().Unix(),
		Status:                common.TopUpStatusPending,
	}
	if err := order.SetResumePayload(&model.SubscriptionOrderResumePayload{
		Type:    "recreate",
		Message: "链上支付需要重新拉起钱包签名，请重新发起支付",
	}); err != nil {
		common.SysLog(fmt.Sprintf("Ethereum: 保存订阅订单恢复支付信息失败: %v", err))
		common.ApiErrorMsg(c, "创建订单失败")
		return
	}
	if err := order.Insert(); err != nil {
		common.SysLog(fmt.Sprintf("Ethereum: 创建订阅订单失败: %v", err))
		common.ApiErrorMsg(c, "创建订单失败")
		return
	}

	common.SysLog(fmt.Sprintf("Ethereum: 订阅订单创建 - userId=%d, tradeNo=%s, planId=%d, token=%s, payAmount=%s",
		userId, tradeNo, plan.Id, tokenCfg.Symbol, payAmountStr))

	c.JSON(http.StatusOK, gin.H{
		"message": "success",
		"data": EthereumPayResponse{
			OrderId:         orderId,
			ContractAddress: setting.EthereumContractAddress,
			ChainId:         setting.EthereumChainId,
			TokenAddress:    tokenCfg.Address,
			PayAmount:       payAmountStr,
			Symbol:          tokenCfg.Symbol,
			Decimals:        tokenCfg.Decimals,
		},
	})
}

// ── Helper functions ───────────────────────────────────────────────────────

// verifyAlchemySignature verifies the Alchemy webhook HMAC-SHA256 signature.
// Alchemy docs: HMAC-SHA256(signingKey, body) → hex digest, compared against X-Alchemy-Signature.
// The signingKey (e.g. "whsec_xxx") is used as-is (raw UTF-8 bytes) per Alchemy's own JS example.
func verifyAlchemySignature(body []byte, sigHex, signingKey string) bool {
	signingKey = strings.TrimSpace(signingKey)
	sigHex = strings.TrimSpace(sigHex)
	if signingKey == "" || sigHex == "" {
		return false
	}
	mac := hmac.New(sha256.New, []byte(signingKey))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(strings.ToLower(sigHex)))
}

// tradeNoToOrderId converts a TradeNo string to a 0x-prefixed bytes32 hex string.
// Uses left-aligned UTF-8 zero-padding (lossless for <32 byte strings).
// Frontend equivalent: ethers.zeroPadBytes(ethers.toUtf8Bytes(tradeNo), 32)
func tradeNoToOrderId(tradeNo string) string {
	b := []byte(tradeNo)
	if len(b) > 32 {
		b = b[:32]
	}
	var padded [32]byte
	copy(padded[:], b) // left-aligned, zero-padded on the right
	return "0x" + hex.EncodeToString(padded[:])
}

// orderIdToTradeNo is the inverse of tradeNoToOrderId.
func orderIdToTradeNo(orderIdHex string) string {
	clean := strings.TrimPrefix(orderIdHex, "0x")
	b, err := hex.DecodeString(clean)
	if err != nil || len(b) != 32 {
		return ""
	}
	// Strip trailing zero bytes
	end := len(b)
	for end > 0 && b[end-1] == 0 {
		end--
	}
	return string(b[:end])
}

func parsePaymentReceivedData(data string) (token string, amount string, err error) {
	clean := strings.TrimPrefix(strings.TrimSpace(data), "0x")
	if len(clean) < 128 {
		return "", "", fmt.Errorf("invalid event data length: %d", len(clean))
	}
	tokenWord := clean[:64]
	amountWord := clean[64:128]
	tokenBytes, err := hex.DecodeString(tokenWord)
	if err != nil || len(tokenBytes) != 32 {
		return "", "", fmt.Errorf("invalid token word")
	}
	amountBytes, err := hex.DecodeString(amountWord)
	if err != nil || len(amountBytes) != 32 {
		return "", "", fmt.Errorf("invalid amount word")
	}
	token = "0x" + strings.ToLower(hex.EncodeToString(tokenBytes[12:]))
	amountInt := new(big.Int).SetBytes(amountBytes)
	if amountInt.Sign() <= 0 {
		return "", "", fmt.Errorf("invalid paid amount")
	}
	return token, amountInt.String(), nil
}

func calcPayAmountDecimal(units decimal.Decimal, pricePerUnit string, decimals int) (string, error) {
	price, err := decimal.NewFromString(pricePerUnit)
	if err != nil || price.Sign() <= 0 {
		return "", fmt.Errorf("invalid pricePerUnit: %s", pricePerUnit)
	}
	if units.Sign() <= 0 || decimals < 0 {
		return "0", nil
	}
	result := units.Mul(price).Mul(decimal.New(1, int32(decimals)))
	resultInt := result.Truncate(0)
	if resultInt.Sign() <= 0 {
		return "0", nil
	}
	return resultInt.StringFixed(0), nil
}

// getEthereumTopUpInfo returns the fields added to GetTopUpInfo response.
func getEthereumTopUpInfo() (enabled bool, info map[string]interface{}) {
	enabled = setting.EthereumEnabled && setting.EthereumContractAddress != ""
	if !enabled {
		return false, nil
	}
	relayProxyEnabled := setting.EthereumWalletConnectRelayProxyEnabled
	primaryRelayURL := strings.TrimSpace(setting.EthereumWalletConnectPrimaryRelayURL)
	backupRelayURL := strings.TrimSpace(setting.EthereumWalletConnectBackupRelayURL)
	if relayProxyEnabled {
		primaryRelayURL = ""
		backupRelayURL = ""
	}
	walletConnect := map[string]interface{}{
		"enabled":             strings.TrimSpace(setting.EthereumWalletConnectProjectID) != "",
		"project_id":          strings.TrimSpace(setting.EthereumWalletConnectProjectID),
		"app_name":            strings.TrimSpace(setting.EthereumWalletConnectAppName),
		"description":         strings.TrimSpace(setting.EthereumWalletConnectAppDescription),
		"url":                 strings.TrimSpace(setting.EthereumWalletConnectAppURL),
		"icon":                strings.TrimSpace(setting.EthereumWalletConnectAppIcon),
		"relay_proxy_enabled": relayProxyEnabled,
		"relay_proxy_url":     "/api/walletconnect/relay",
		"primary_relay_url":   primaryRelayURL,
		"backup_relay_url":    backupRelayURL,
	}
	info = map[string]interface{}{
		"chain_id":         setting.EthereumChainId,
		"contract_address": setting.EthereumContractAddress,
		"min_topup":        setting.EthereumMinTopUp,
		"tokens":           setting.GetEthereumTokens(),
		"wallet_connect":   walletConnect,
	}
	return true, info
}
