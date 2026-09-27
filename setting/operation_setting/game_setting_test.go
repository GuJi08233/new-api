package operation_setting

import (
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
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

	// 所有免费奖励的每日合计上限可以为 0(不限制)，不能为负。
	assert.NoError(t, ValidateGameOption(GameSettingPrefix+"free_daily_limit", "0"))
	assert.Error(t, ValidateGameOption(GameSettingPrefix+"free_daily_limit", "-1"))

	// 免费游戏的规则：每日领奖次数 0 到 GameMaxDailyRewards，至少一档奖励，成绩严格递增、奖励不减，
	// 难度类游戏(扫雷)的成绩不超过最高难度。
	for game, cases := range map[string]map[string]bool{
		"tetris": {
			`{"enabled":true,"daily_rewards":3,"tiers":[{"score":5000,"coins":1},{"score":20000,"coins":2}],"min_seconds_per_coin":60}`: true,
			`{"enabled":true,"daily_rewards":0,"tiers":[{"score":1,"coins":1}]}`:                                                        true,
			`{"enabled":true,"daily_rewards":101,"tiers":[{"score":1,"coins":1}]}`:                                                      false,
			`{"enabled":true,"daily_rewards":3,"tiers":[]}`:                                                                             false,
			`{"enabled":true,"daily_rewards":3,"tiers":[{"score":20000,"coins":1},{"score":5000,"coins":2}]}`:                           false,
			`{"enabled":true,"daily_rewards":3,"tiers":[{"score":5000,"coins":2},{"score":20000,"coins":1}]}`:                           false,
			`{"enabled":true,"daily_rewards":3,"tiers":[{"score":5000,"coins":0}]}`:                                                     false,
			`{"enabled":true,"daily_rewards":3,"tiers":[{"score":5000,"coins":101}]}`:                                                   false,
			`{"enabled":true,"daily_rewards":3,"tiers":[{"score":1,"coins":1}],"min_seconds_per_coin":3601}`:                            false,
			`not json`: false,
		},
		"minesweeper": {
			`{"enabled":true,"daily_rewards":3,"tiers":[{"score":2,"coins":1},{"score":3,"coins":2}]}`: true,
			`{"enabled":true,"daily_rewards":3,"tiers":[{"score":2,"coins":1},{"score":4,"coins":2}]}`: false,
		},
		// 下注游戏只限制单次下注；按公式计算赔率的游戏返还率必须低于 100%，赔付表固定的游戏不能配置返还率；
		// 分档投入的游戏只能从固定的几档里选最高开放到哪一档。
		"dice": {
			`{"enabled":true,"max_bet":10,"rtp_percent":96}`:   true,
			`{"enabled":true,"max_bet":1000,"rtp_percent":99}`: true,
			`{"enabled":true,"max_bet":10,"rtp_percent":100}`:  false,
			`{"enabled":true,"max_bet":10,"rtp_percent":49}`:   false,
			`{"enabled":true,"max_bet":10}`:                    false,
			`{"enabled":true,"max_bet":1001,"rtp_percent":96}`: false,
			`{"enabled":true,"max_bet":0,"rtp_percent":96}`:    false,
		},
		"slots": {
			`{"enabled":true,"max_bet":10}`:                  true,
			`{"enabled":true,"max_bet":10,"rtp_percent":90}`: false,
		},
		"blackjack": {
			`{"enabled":true,"max_bet":2}`: true,
			`{"enabled":true,"max_bet":1}`: false,
		},
		"plinko": {
			`{"enabled":true,"max_bet":10}`: true,
			`{"enabled":true,"max_bet":50}`: false,
		},
		// 大转盘每次花费 1 到 GameMaxWheelSpinCost，免费次数 0 到 GameMaxWheelFreeSpins。
		"wheel": {
			`{"enabled":true,"free_spins":1,"spin_cost":1}`:   true,
			`{"enabled":true,"free_spins":0,"spin_cost":100}`: true,
			`{"enabled":true,"free_spins":101,"spin_cost":1}`: false,
			`{"enabled":true,"free_spins":1,"spin_cost":0}`:   false,
		},
		// 对战的押注上限可以为 0(只能友谊赛)，抽成不超过 GameMaxRakePercent，每步限时有上下限；
		// 人机对战的领奖规则按免费游戏校验，不支持人机对战的游戏不能打开它。
		"gomoku": {
			`{"enabled":true,"max_stake":100,"rake_percent":5,"move_seconds":60,"ai":{"enabled":true,"daily_rewards":2,"tiers":[{"score":2,"coins":1},{"score":3,"coins":2}]}}`: true,
			`{"enabled":true,"max_stake":0,"rake_percent":0,"move_seconds":10,"ai":{"enabled":false,"tiers":[{"score":1,"coins":1}]}}`:                                          true,
			`{"enabled":true,"max_stake":100,"rake_percent":21,"move_seconds":60,"ai":{"tiers":[{"score":1,"coins":1}]}}`:                                                       false,
			`{"enabled":true,"max_stake":100,"rake_percent":5,"move_seconds":9,"ai":{"tiers":[{"score":1,"coins":1}]}}`:                                                         false,
			`{"enabled":true,"max_stake":100,"rake_percent":5,"move_seconds":60,"ai":{"tiers":[{"score":4,"coins":1}]}}`:                                                        false,
		},
		"rps": {
			`{"enabled":true,"max_stake":100,"rake_percent":5,"move_seconds":20}`:                       true,
			`{"enabled":true,"max_stake":100,"rake_percent":5,"move_seconds":20,"ai":{"enabled":true}}`: false,
		},
		"jackpot": {
			`{"enabled":true,"max_bet":100,"round_seconds":60,"rake_percent":5}`: true,
			`{"enabled":true,"max_bet":0,"round_seconds":60,"rake_percent":5}`:   false,
			`{"enabled":true,"max_bet":100,"round_seconds":10,"rake_percent":5}`: false,
		},
	} {
		for value, valid := range cases {
			err := ValidateGameOption(GameSettingPrefix+game, value)
			if valid {
				assert.NoError(t, err, "%s %s", game, value)
			} else {
				assert.Error(t, err, "%s %s", game, value)
			}
		}
	}

	// 热钱包私钥也能经通用选项接口写入，同样要在落库前挡住格式错误；空值表示清除。
	assert.NoError(t, ValidateGameOption(GameHotWalletPrivateKeyOption, ""))
	assert.NoError(t, ValidateGameOption(GameHotWalletPrivateKeyOption, "4c0883a69102937d6231471b5dbb6204fe5129617082792ae468d01a3f362318"))
	assert.Error(t, ValidateGameOption(GameHotWalletPrivateKeyOption, "0x1234"))
	assert.Error(t, ValidateGameOption(GameHotWalletPrivateKeyOption, "not-a-private-key"))
}

// TestValidateGameSettingChecksWithdrawRange: the defaults must pass their own
// validation, and saving the whole setting must also reject a single-withdrawal
// range that no amount could satisfy.
func TestValidateGameSettingChecksWithdrawRange(t *testing.T) {
	setting := gameSetting.Clone()
	require.NoError(t, ValidateGameSetting(setting))

	setting.WithdrawMinAmount = 50
	setting.WithdrawMaxAmount = 10
	assert.Error(t, ValidateGameSetting(setting))
}

// 每个游戏的配置字段都登记了规则类型，按游戏标识读出的规则与字段一致，未知的游戏视为没开放。
func TestGameRulesAreRegisteredByKey(t *testing.T) {
	values, err := config.ConfigToMap(gameSetting.Clone())
	require.NoError(t, err)
	for game := range gameRuleSpecs {
		_, ok := values[game]
		assert.True(t, ok, "%s 在 GameSetting 里有字段", game)
	}
	setting := gameSetting.Clone()
	setting.Game2048.Enabled = true
	setting.Mines.Enabled = true
	setting.Xiangqi.Enabled = true
	for game := range gameRuleSpecs {
		want := game == "2048" || game == "mines" || game == "xiangqi"
		assert.Equal(t, want, setting.GameEnabled(game), game)
	}
	assert.False(t, setting.GameEnabled("unknown"))
}

// 快照是只读的：Clone 出来的配置改奖励档位不会写进原来的快照。
func TestGameSettingCloneIsDeep(t *testing.T) {
	original := gameSetting.Clone()
	clone := original.Clone()
	require.NoError(t, common.UnmarshalJsonStr(`{"tetris":{"tiers":[{"score":1,"coins":5}]}}`, clone))
	assert.Equal(t, 5000, original.Tetris.Tiers[0].Score)
	assert.Equal(t, 1, clone.Tetris.Tiers[0].Score)
}
