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
		"unrelated_key":                              "anything",
		GameSettingPrefix + "enabled":                "true",
		GameSettingPrefix + "withdraw_enabled":       "false",
		GameSettingPrefix + "play_turnstile_enabled": "true",
		GameSettingPrefix + "withdraw_min_amount":    "1",
		GameSettingPrefix + "withdraw_daily_limit":   strconv.FormatInt(common.MaxQuota-1, 10),
		GameSettingPrefix + "chain_id":               "11155111",
		GameSettingPrefix + "rpc_url":                "",
		GameSettingPrefix + "explorer_url":           "https://sepolia.etherscan.io",
		GameSettingPrefix + "token_address":          "0x64bf89340562AB4fE829a481E6797B012664425B",
		GameSettingPrefix + "token_symbol":           "✦",
		GameSettingPrefix + "token_decimals":         "18",
		GameSettingPrefix + "confirmations":          "64",
	}
	for key, value := range valid {
		assert.NoError(t, ValidateGameOption(key, value), key)
	}

	invalid := map[string]string{
		GameSettingPrefix + "enabled":                "yes",
		GameSettingPrefix + "play_turnstile_enabled": "on",
		GameSettingPrefix + "withdraw_min_amount":    "0",
		GameSettingPrefix + "withdraw_max_amount":    "1.5",
		GameSettingPrefix + "withdraw_daily_limit":   strconv.FormatInt(common.MaxQuota, 10),
		GameSettingPrefix + "chain_id":               "-1",
		GameSettingPrefix + "rpc_url":                "ftp://node.example",
		GameSettingPrefix + "token_address":          "0x64bf89340562ab4fe829a481e6797b01266442",
		GameSettingPrefix + "token_symbol":           "   ",
		GameSettingPrefix + "token_decimals":         "37",
		GameSettingPrefix + "confirmations":          "0",
		GameSettingPrefix + "unknown_field":          "x",
	}
	for key, value := range invalid {
		assert.Error(t, ValidateGameOption(key, value), key)
	}

	// 游戏规则整份以 JSON 保存：每日奖励上限可以为 0(只玩不发奖励)，每个游戏币所需分数必须为正，缺字段按零值校验；
	// 每个游戏币所需时长可以为 0(不限制)，最长不超过一局的时长上限。
	for value, valid := range map[string]bool{
		`{"enabled":true,"daily_reward_limit":0,"score_per_coin":1}`:                                true,
		`{"enabled":false,"daily_reward_limit":3,"score_per_coin":5000}`:                            true,
		`{"enabled":true,"daily_reward_limit":3,"score_per_coin":5000,"min_seconds_per_coin":3600}`: true,
		`{"enabled":true,"daily_reward_limit":3,"score_per_coin":5000,"min_seconds_per_coin":3601}`: false,
		`{"enabled":true,"daily_reward_limit":3,"score_per_coin":5000,"min_seconds_per_coin":-1}`:   false,
		`{"enabled":true,"daily_reward_limit":-1,"score_per_coin":5000}`:                            false,
		`{"enabled":true,"daily_reward_limit":9007199254740992,"score_per_coin":5000}`:              false,
		`{"enabled":true,"daily_reward_limit":3,"score_per_coin":0}`:                                false,
		`{"enabled":true,"daily_reward_limit":3}`:                                                   false,
		`not json`: false,
	} {
		err := ValidateGameOption(GameSettingPrefix+"tetris", value)
		if valid {
			assert.NoError(t, err, value)
		} else {
			assert.Error(t, err, value)
		}
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
