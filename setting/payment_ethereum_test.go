package setting

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestEthereumTokenPayAmountKeepsFractionalSubscriptionPrice(t *testing.T) {
	token := EthereumToken{Symbol: "USDT", Address: "0x1", Decimals: 6, Price: "1"}
	amount, err := token.PayAmount(decimal.NewFromFloat(9.99))
	require.NoError(t, err)
	require.Equal(t, "9990000", amount)
}

func TestEthereumTokenPayAmountRejectsNonPositivePrice(t *testing.T) {
	token := EthereumToken{Symbol: "ETH", Address: "0x0", Decimals: 18, Price: "0"}
	_, err := token.PayAmount(decimal.NewFromInt(1))
	require.Error(t, err)
}
