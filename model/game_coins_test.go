package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// coin 返回 n 个整币的游戏币数量，测试里用它写出预期的余额与流水。
func coin(n int) GameCoins {
	coins, err := WholeGameCoins(n)
	if err != nil {
		panic(err)
	}
	return coins
}

// coins 把一组整币数换成游戏币数量，测试里用它写出对局的输赢。
func coins(values ...int) []GameCoins {
	amounts := make([]GameCoins, len(values))
	for i, value := range values {
		amounts[i] = coin(value)
	}
	return amounts
}

// coinText 解析十进制币数，测试里用它写出带小数的数量。
func coinText(text string) GameCoins {
	coins, err := ParseGameCoins(text)
	if err != nil {
		panic(err)
	}
	return coins
}

// 游戏币按额度单位记账：1 个币就是 1 美元额度，小数按同一个单位换算，接口收发的都是币数。
func TestGameCoinsUseQuotaUnits(t *testing.T) {
	unit := int(common.QuotaPerUnit)
	require.Equal(t, GameCoins(unit), coin(1))
	assert.Equal(t, GameCoins(unit/2), coinText("0.5"))
	assert.Equal(t, GameCoins(-unit*3/2), coinText("-1.5"))
	assert.Equal(t, GameCoins(1), coinText("0.000002"))

	data, err := common.Marshal(map[string]GameCoins{"a": coinText("12.34"), "b": coin(3), "c": coinText("-0.000002")})
	require.NoError(t, err)
	assert.JSONEq(t, `{"a":12.34,"b":3,"c":-0.000002}`, string(data))

	var decoded struct {
		Number GameCoins `json:"number"`
		Text   GameCoins `json:"text"`
		Null   GameCoins `json:"null"`
	}
	require.NoError(t, common.UnmarshalJsonStr(`{"number":1.25,"text":"2.5","null":null}`, &decoded))
	assert.Equal(t, coinText("1.25"), decoded.Number)
	assert.Equal(t, coinText("2.5"), decoded.Text)
	assert.Zero(t, decoded.Null)

	whole, ok := coin(7).Whole()
	assert.True(t, ok)
	assert.Equal(t, 7, whole)
	_, ok = coinText("7.5").Whole()
	assert.False(t, ok)
	assert.True(t, coinText("1.23").HasDecimals(2))
	assert.False(t, coinText("1.234").HasDecimals(2))
}

// 不足一个额度单位的零头、科学计数法、超过 2^53 的数量都拒绝，不会被截断或夹紧。
func TestParseGameCoinsRejectsAmountsThatCannotBeBookedExactly(t *testing.T) {
	for _, text := range []string{"", "-", ".5", "1.", "1e3", "0x10", "1.2.3", "0.0000001", "1 000", "18014398509.481984", "99999999999999999999999999999"} {
		_, err := ParseGameCoins(text)
		assert.ErrorIs(t, err, ErrGameCoinAmountInvalid, text)
	}
	var amount struct {
		Value GameCoins `json:"value"`
	}
	assert.Error(t, common.UnmarshalJsonStr(`{"value":1e2}`, &amount))
	assert.Error(t, common.UnmarshalJsonStr(`{"value":true}`, &amount))
}
