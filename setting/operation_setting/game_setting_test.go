package operation_setting

import (
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestValidateGameOption pins the per-field contract enforced before a game
// setting reaches the database: coin quantities are positive integers below the
// shared 2^53 wallet bound, chain fields are well-formed, unknown fields fail.
func TestValidateGameOption(t *testing.T) {
	valid := map[string]string{
		"unrelated_key":                            "anything",
		GameSettingPrefix + "enabled":              "true",
		GameSettingPrefix + "withdraw_enabled":     "false",
		GameSettingPrefix + "withdraw_min_amount":  "1",
		GameSettingPrefix + "withdraw_daily_limit": strconv.FormatInt(common.MaxQuota-1, 10),
		GameSettingPrefix + "chain_id":             "11155111",
		GameSettingPrefix + "rpc_url":              "",
		GameSettingPrefix + "explorer_url":         "https://sepolia.etherscan.io",
		GameSettingPrefix + "token_address":        "0x64bf89340562AB4fE829a481E6797B012664425B",
		GameSettingPrefix + "token_symbol":         "✦",
		GameSettingPrefix + "token_decimals":       "18",
		GameSettingPrefix + "confirmations":        "64",
	}
	for key, value := range valid {
		assert.NoError(t, ValidateGameOption(key, value), key)
	}

	invalid := map[string]string{
		GameSettingPrefix + "enabled":              "yes",
		GameSettingPrefix + "withdraw_min_amount":  "0",
		GameSettingPrefix + "withdraw_max_amount":  "1.5",
		GameSettingPrefix + "withdraw_daily_limit": strconv.FormatInt(common.MaxQuota, 10),
		GameSettingPrefix + "chain_id":             "-1",
		GameSettingPrefix + "rpc_url":              "ftp://node.example",
		GameSettingPrefix + "token_address":        "0x64bf89340562ab4fe829a481e6797b01266442",
		GameSettingPrefix + "token_symbol":         "   ",
		GameSettingPrefix + "token_decimals":       "37",
		GameSettingPrefix + "confirmations":        "0",
		GameSettingPrefix + "unknown_field":        "x",
	}
	for key, value := range invalid {
		assert.Error(t, ValidateGameOption(key, value), key)
	}

	// 热钱包私钥也能经通用选项接口写入，同样要在落库前挡住格式错误；空值表示清除。
	assert.NoError(t, ValidateGameOption(GameHotWalletPrivateKeyOption, ""))
	assert.NoError(t, ValidateGameOption(GameHotWalletPrivateKeyOption, "4c0883a69102937d6231471b5dbb6204fe5129617082792ae468d01a3f362318"))
	assert.Error(t, ValidateGameOption(GameHotWalletPrivateKeyOption, "0x1234"))
	assert.Error(t, ValidateGameOption(GameHotWalletPrivateKeyOption, "not-a-private-key"))
}

// TestValidateGameSettingChecksWithdrawRange: saving the whole setting must
// also reject a single-withdrawal range that no amount could satisfy.
func TestValidateGameSettingChecksWithdrawRange(t *testing.T) {
	setting := gameSetting
	require.NoError(t, ValidateGameSetting(&setting))

	setting.WithdrawMinAmount = 50
	setting.WithdrawMaxAmount = 10
	assert.Error(t, ValidateGameSetting(&setting))
}
