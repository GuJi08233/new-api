package model

import (
	"bytes"
	"math"
	"strings"

	"github.com/QuantumNous/new-api/common"

	"github.com/shopspring/decimal"
)

// GameCoins 是游戏币数量。游戏币按额度单位记账：1 个游戏币 = 1 美元额度 = GameCoinUnit 个单位，所以可以有小数，并且与站内额度、
// 模拟盘资金按同一个数记账，三者之间划转不用换算，也不会留下取整的零头。接口里写成币数(十进制数，例如 1.5)，收发的都是币数；
// 只有链上提现和转入模拟盘要求整币(见 Whole)。
type GameCoins int

// gameCoinTextMaxLength 是接口传来的币数最多几个字符，足够表示 2^53 个单位折成的币数。
const gameCoinTextMaxLength = 32

// GameCoinUnit 是 1 个游戏币折成的额度单位数，与模拟盘的 1 USDT 相同。QuotaPerUnit 折不成正整数个单位时返回
// ErrGameCoinAmountInvalid。
func GameCoinUnit() (int, error) {
	if math.IsNaN(common.QuotaPerUnit) || math.IsInf(common.QuotaPerUnit, 0) {
		return 0, ErrGameCoinAmountInvalid
	}
	unit, err := TradeQuotaPerUsd()
	if err != nil {
		return 0, err
	}
	if unit < 1 {
		return 0, ErrGameCoinAmountInvalid
	}
	return unit, nil
}

// WholeGameCoins 返回 coins 个整币，coins 可以是 0 或负数；越过 2^53 边界时返回 ErrGameCoinAmountInvalid。
func WholeGameCoins(coins int) (GameCoins, error) {
	unit, err := GameCoinUnit()
	if err != nil {
		return 0, err
	}
	if coins > (common.MaxQuota-1)/unit || coins < -(common.MaxQuota-1)/unit {
		return 0, ErrGameCoinAmountInvalid
	}
	return GameCoins(coins * unit), nil
}

// GameCoinsFromDecimal 把币数换成额度单位，不足一个单位的零头舍去：配置里的奖励档位按两位小数填写，只在 QuotaPerUnit 被改成
// 不是 100 的倍数时才会有零头。越过 2^53 边界时返回 ErrGameCoinAmountInvalid。
func GameCoinsFromDecimal(coins decimal.Decimal) (GameCoins, error) {
	unit, err := GameCoinUnit()
	if err != nil {
		return 0, err
	}
	units, err := common.QuotaFromDecimalStrict(coins.Mul(decimal.NewFromInt(int64(unit))).Truncate(0))
	if err != nil || units <= -common.MaxQuota || units >= common.MaxQuota {
		return 0, ErrGameCoinAmountInvalid
	}
	return GameCoins(units), nil
}

// ParseGameCoins 解析十进制的币数，可以有小数和开头的负号，必须正好是整数个额度单位、在 2^53 之内，否则返回
// ErrGameCoinAmountInvalid。只接受数字、一个小数点和负号：decimal 能解析 1e1000000 这样的科学计数法，拿去运算会占用大量内存。
func ParseGameCoins(text string) (GameCoins, error) {
	text = strings.TrimSpace(text)
	digits := strings.TrimPrefix(text, "-")
	if digits == "" || len(text) > gameCoinTextMaxLength || strings.Count(digits, ".") > 1 ||
		strings.Trim(digits, "0123456789.") != "" || strings.HasPrefix(digits, ".") || strings.HasSuffix(digits, ".") {
		return 0, ErrGameCoinAmountInvalid
	}
	coins, err := decimal.NewFromString(text)
	if err != nil {
		return 0, ErrGameCoinAmountInvalid
	}
	unit, err := GameCoinUnit()
	if err != nil {
		return 0, err
	}
	units := coins.Mul(decimal.NewFromInt(int64(unit)))
	if !units.Equal(units.Truncate(0)) {
		return 0, ErrGameCoinAmountInvalid
	}
	value, err := common.QuotaFromDecimalStrict(units)
	if err != nil || value <= -common.MaxQuota || value >= common.MaxQuota {
		return 0, ErrGameCoinAmountInvalid
	}
	return GameCoins(value), nil
}

// Decimal 返回币数。
func (c GameCoins) Decimal() decimal.Decimal {
	unit, err := GameCoinUnit()
	if err != nil {
		return decimal.Zero
	}
	return decimal.NewFromInt(int64(c)).Div(decimal.NewFromInt(int64(unit)))
}

// Whole 返回整币数，不是整币时 ok 为假。
func (c GameCoins) Whole() (coins int, ok bool) {
	unit, err := GameCoinUnit()
	if err != nil || int(c)%unit != 0 {
		return 0, false
	}
	return int(c) / unit, true
}

// HasDecimals 表示币数最多有 places 位小数，例如下注只能精确到 0.01 个游戏币。
func (c GameCoins) HasDecimals(places int32) bool {
	coins := c.Decimal()
	return coins.Equal(coins.Truncate(places))
}

// MarshalJSON 把游戏币写成币数。
func (c GameCoins) MarshalJSON() ([]byte, error) {
	unit, err := GameCoinUnit()
	if err != nil {
		return nil, err
	}
	return []byte(decimal.NewFromInt(int64(c)).Div(decimal.NewFromInt(int64(unit))).String()), nil
}

// UnmarshalJSON 读入币数，数字和字符串都可以，见 ParseGameCoins；null 是 0。
func (c *GameCoins) UnmarshalJSON(data []byte) error {
	text := string(bytes.TrimSpace(data))
	if text == "null" {
		*c = 0
		return nil
	}
	if strings.HasPrefix(text, `"`) {
		var quoted string
		if err := common.Unmarshal(data, &quoted); err != nil {
			return ErrGameCoinAmountInvalid
		}
		text = quoted
	}
	coins, err := ParseGameCoins(text)
	if err != nil {
		return err
	}
	*c = coins
	return nil
}
