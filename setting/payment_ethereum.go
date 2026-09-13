package setting

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/shopspring/decimal"
)

// EthereumToken represents a single accepted token configuration.
// Address "0x0000000000000000000000000000000000000000" means native ETH.
type EthereumToken struct {
	Symbol   string `json:"symbol"`
	Address  string `json:"address"`  // checksummed hex address or zero address for ETH
	Decimals int    `json:"decimals"` // 18 for ETH, 6 for USDT/USDC, etc.
	// Price is the token amount per ONE top-up unit (i.e. per "1 USD equivalent").
	// E.g. for ETH at $3000: "0.000333"  |  for USDT: "1.0"
	Price string `json:"price"`
}

// EthereumMaxConfirmations caps EthereumConfirmations. Ethereum finalises after
// two epochs (64 slots); waiting longer buys nothing.
const EthereumMaxConfirmations = 64

var (
	EthereumEnabled                        bool
	EthereumChainId                        int64  = 11155111 // Sepolia testnet default
	EthereumContractAddress                string            // deployed contract checksummed address
	EthereumAlchemyWebhookSigningKey       string            // signing key from Alchemy dashboard
	EthereumMinTopUp                       int    = 1
	EthereumRpcUrl                         string // JSON-RPC endpoint used to re-verify payment receipts; empty skips on-chain verification
	EthereumConfirmations                  int    // block confirmations a payment needs before it settles (0 = as soon as the receipt exists)
	EthereumWalletConnectProjectID         string
	EthereumWalletConnectAppName           string
	EthereumWalletConnectAppDescription    string
	EthereumWalletConnectAppURL            string
	EthereumWalletConnectAppIcon           string
	EthereumWalletConnectRelayProxyEnabled bool
	EthereumWalletConnectPrimaryRelayURL   string
	EthereumWalletConnectBackupRelayURL    string
)

// DefaultEthereumTokens is the factory default (ETH only on Sepolia).
var DefaultEthereumTokens = []EthereumToken{
	{
		Symbol:   "ETH",
		Address:  "0x0000000000000000000000000000000000000000",
		Decimals: 18,
		Price:    "0.001",
	},
}

// GetEthereumTokens reads the current token list from OptionMap (thread-safe).
func GetEthereumTokens() []EthereumToken {
	common.OptionMapRWMutex.RLock()
	jsonStr := common.OptionMap["EthereumSupportedTokens"]
	common.OptionMapRWMutex.RUnlock()

	if jsonStr == "" {
		return copyDefaultEthereumTokens()
	}
	var tokens []EthereumToken
	if err := common.UnmarshalJsonStr(jsonStr, &tokens); err != nil {
		return copyDefaultEthereumTokens()
	}
	return tokens
}

// GetEthereumToken looks up the configured token for an address, case-insensitively.
func GetEthereumToken(address string) (EthereumToken, bool) {
	for _, token := range GetEthereumTokens() {
		if strings.EqualFold(token.Address, address) {
			return token, true
		}
	}
	return EthereumToken{}, false
}

// PayAmount prices a number of top-up units in the token's smallest unit (wei
// for ETH) and returns it as a decimal string. Order creation and late-payment
// settlement both go through here so a payment is always judged against the
// same rule that quoted it.
func (t EthereumToken) PayAmount(units decimal.Decimal) (string, error) {
	price, err := decimal.NewFromString(t.Price)
	if err != nil || price.Sign() <= 0 {
		return "", fmt.Errorf("invalid pricePerUnit: %s", t.Price)
	}
	if units.Sign() <= 0 || t.Decimals < 0 {
		return "0", nil
	}
	resultInt := units.Mul(price).Mul(decimal.New(1, int32(t.Decimals))).Truncate(0)
	if resultInt.Sign() <= 0 {
		return "0", nil
	}
	return resultInt.StringFixed(0), nil
}

func copyDefaultEthereumTokens() []EthereumToken {
	cp := make([]EthereumToken, len(DefaultEthereumTokens))
	copy(cp, DefaultEthereumTokens)
	return cp
}

// EthereumTokens2JsonString serialises DefaultEthereumTokens for InitOptionMap.
func EthereumTokens2JsonString() string {
	b, err := common.Marshal(DefaultEthereumTokens)
	if err != nil {
		return "[]"
	}
	return string(b)
}
