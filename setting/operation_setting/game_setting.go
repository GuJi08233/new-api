package operation_setting

import (
	"encoding/hex"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
)

// GameSetting 是游戏中心的全部可配置项。游戏币按 1 币 = 1 美元额度兑换主钱包，
// 这个比例是产品规则而不是运营参数，所以不在这里配置。
type GameSetting struct {
	Enabled bool `json:"enabled"` // 游戏中心总开关，关闭时页面入口与用户接口一并下线
	// PlayTurnstileEnabled 要求每局开始前通过 Turnstile 人机验证，只在站点启用了 Turnstile 时生效。
	PlayTurnstileEnabled bool `json:"play_turnstile_enabled"`

	// 链上提现。每日上限按全站当日提现总量计算，保护热钱包里的周转余额。
	WithdrawEnabled    bool `json:"withdraw_enabled"`
	WithdrawDailyLimit int  `json:"withdraw_daily_limit"`
	WithdrawMinAmount  int  `json:"withdraw_min_amount"`
	WithdrawMaxAmount  int  `json:"withdraw_max_amount"`

	// 代币所在的链与合约。热钱包私钥不在这里，见 GameHotWalletPrivateKeyOption。
	ChainId       int64  `json:"chain_id"`
	RpcUrl        string `json:"rpc_url"`
	TokenAddress  string `json:"token_address"`
	TokenSymbol   string `json:"token_symbol"`
	TokenDecimals int    `json:"token_decimals"`
	ExplorerUrl   string `json:"explorer_url"`
	Confirmations int    `json:"confirmations"`

	// 每个游戏一份开关与奖励规则。
	Tetris GameRule  `json:"tetris"`
	Slots  SlotsRule `json:"slots"`
}

// GameRule 是单个游戏的开关与奖励规则。奖励按单局成绩计算，再受每人每日上限约束；
// 上限按游戏分别计算，一个游戏领满了不影响其他游戏。
type GameRule struct {
	Enabled bool `json:"enabled"`
	// DailyRewardLimit 是每人每天最多从这个游戏获得的游戏币，0 表示照常可玩但不发奖励。
	DailyRewardLimit int `json:"daily_reward_limit"`
	// ScorePerCoin 是单局每得这么多分奖励 1 个游戏币。
	ScorePerCoin int `json:"score_per_coin"`
	// MinSecondsPerCoin 是每个游戏币至少需要的游戏时长(秒)，0 表示不限制。脚本几秒就能刷够分数，
	// 奖励还要按实际玩了多久来算。
	MinSecondsPerCoin int `json:"min_seconds_per_coin"`
}

// SlotsRule 是老虎机的开关与下注规则。老虎机用游戏币下注，两个阶段的赔付表固定在 pkg/slots：每个玩家累计下注
// 满 EvenStageWager 之前理论返还率 100%，之后约 80%，玩家长期是输的。默认不限制每日输赢；设了每日上限时按净赢
// (派彩减去下注)计算，同一 IP 下所有账号合计也不超过它，到上限后当天不能再下注。
type SlotsRule struct {
	Enabled bool `json:"enabled"`
	// EvenStageWager 是均衡阶段的累计下注额：玩家累计下注满这么多游戏币之前按均衡赔付表，之后按常规赔付表；
	// 0 表示一开始就按常规赔付表。
	EvenStageWager int `json:"even_stage_wager"`
	// DailyRewardLimit 是每人每天最多从老虎机净赢的游戏币，0 表示不限制。
	DailyRewardLimit int `json:"daily_reward_limit"`
	// MaxBet 是每次旋转最多下注的游戏币。
	MaxBet int `json:"max_bet"`
}

// GameHotWalletPrivateKeyOption 是管理员在游戏管理页填写的热钱包私钥所在的选项键。私钥不放进
// GameSetting：整份配置会原样返回给管理端，私钥必须单独存放、只写不读。键名以 Key 结尾，
// 通用的选项列表接口会把它当作敏感项过滤掉。
const GameHotWalletPrivateKeyOption = "GameHotWalletPrivateKey"

const (
	GameSettingPrefix = "game_setting."

	// MetaMask 的 wallet_watchAsset 只接受 0–36 位精度、最长 11 个字符的符号，
	// 超出范围的代币用户无法一键添加到钱包。
	GameMaxTokenDecimals    = 36
	GameMaxTokenSymbolRunes = 11
	GameMaxConfirmations    = 64
	// 一局最长一小时，每个游戏币要求的时长再长就永远拿不到奖励了。
	GameMaxSecondsPerCoin = 3600
	// 老虎机单次下注的上限。派彩最多是下注的 50 倍，下注再大也远在 2^53 之内。
	GameMaxSlotsBet = 1000
)

// ValidateGameOption 校验单个游戏中心配置项，非法值在落库前拒绝。
// 游戏币数量与 quota 共用 2^53 边界：它们都会以 JSON 数字交给前端，还会按 1:1 美元换成额度。
func ValidateGameOption(key string, value string) error {
	if key == GameHotWalletPrivateKeyOption {
		// 这里只挡住格式错误；私钥是否落在曲线阶之内，由热钱包解析时判断并显示在管理端。
		raw, err := hex.DecodeString(strings.TrimPrefix(value, "0x"))
		if value != "" && (err != nil || len(raw) != 32) {
			return fmt.Errorf("热钱包私钥必须是 64 位十六进制字符")
		}
		return nil
	}
	if !strings.HasPrefix(key, GameSettingPrefix) {
		return nil
	}

	field := strings.TrimPrefix(key, GameSettingPrefix)
	switch field {
	case "enabled", "withdraw_enabled", "play_turnstile_enabled":
		if _, err := strconv.ParseBool(value); err != nil {
			return fmt.Errorf("%s 必须是 true 或 false", field)
		}
	case "withdraw_daily_limit", "withdraw_min_amount", "withdraw_max_amount":
		amount, err := strconv.Atoi(value)
		if err != nil || amount < 1 || amount >= common.MaxQuota {
			return fmt.Errorf("%s 必须是正整数", field)
		}
	case "chain_id":
		chainId, err := strconv.ParseInt(value, 10, 64)
		if err != nil || chainId < 1 || chainId >= common.MaxQuota {
			return fmt.Errorf("链 ID 必须是正整数")
		}
	case "rpc_url", "explorer_url":
		if value != "" && !isHttpUrl(value) {
			return fmt.Errorf("%s 必须是 http 或 https 地址", field)
		}
	case "token_address":
		if value != "" && !isEthereumHexAddress(value) {
			return fmt.Errorf("代币合约地址必须是 0x 开头的 40 位十六进制地址")
		}
	case "token_symbol":
		symbol := strings.TrimSpace(value)
		if symbol == "" || utf8.RuneCountInString(symbol) > GameMaxTokenSymbolRunes {
			return fmt.Errorf("代币符号必须是 1 到 %d 个字符", GameMaxTokenSymbolRunes)
		}
	case "token_decimals":
		decimals, err := strconv.Atoi(value)
		if err != nil || decimals < 0 || decimals > GameMaxTokenDecimals {
			return fmt.Errorf("代币精度必须为 0 到 %d", GameMaxTokenDecimals)
		}
	case "confirmations":
		confirmations, err := strconv.Atoi(value)
		if err != nil || confirmations < 1 || confirmations > GameMaxConfirmations {
			return fmt.Errorf("确认数必须为 1 到 %d", GameMaxConfirmations)
		}
	case "tetris":
		// 游戏规则整份以 JSON 保存，缺字段按零值校验：管理端总是提交完整的规则。
		var rule GameRule
		if err := common.UnmarshalJsonStr(value, &rule); err != nil {
			return fmt.Errorf("%s 的游戏规则格式无效", field)
		}
		if rule.DailyRewardLimit < 0 || rule.DailyRewardLimit >= common.MaxQuota {
			return fmt.Errorf("%s 的每日奖励上限必须是非负整数", field)
		}
		if rule.ScorePerCoin < 1 || rule.ScorePerCoin >= common.MaxQuota {
			return fmt.Errorf("%s 的每个游戏币所需分数必须是正整数", field)
		}
		if rule.MinSecondsPerCoin < 0 || rule.MinSecondsPerCoin > GameMaxSecondsPerCoin {
			return fmt.Errorf("%s 的每个游戏币所需时长必须为 0 到 %d 秒", field, GameMaxSecondsPerCoin)
		}
	case "slots":
		var rule SlotsRule
		if err := common.UnmarshalJsonStr(value, &rule); err != nil {
			return fmt.Errorf("%s 的游戏规则格式无效", field)
		}
		if rule.DailyRewardLimit < 0 || rule.DailyRewardLimit >= common.MaxQuota {
			return fmt.Errorf("%s 的每日净赢上限必须是非负整数", field)
		}
		if rule.EvenStageWager < 0 || rule.EvenStageWager >= common.MaxQuota {
			return fmt.Errorf("%s 的均衡阶段下注额必须是非负整数", field)
		}
		if rule.MaxBet < 1 || rule.MaxBet > GameMaxSlotsBet {
			return fmt.Errorf("%s 的单次最多下注必须为 1 到 %d", field, GameMaxSlotsBet)
		}
	default:
		return fmt.Errorf("未知的游戏中心配置项: %s", field)
	}
	return nil
}

// ValidateGameSetting 校验一份完整配置：逐项校验之外，还要求单次提现区间有意义。
// 管理端整份保存时先过这里，逐项写 option 的旧入口只能做单项校验。
func ValidateGameSetting(setting *GameSetting) error {
	values, err := config.ConfigToMap(setting)
	if err != nil {
		return err
	}
	for field, value := range values {
		if err := ValidateGameOption(GameSettingPrefix+field, value); err != nil {
			return err
		}
	}
	if setting.WithdrawMinAmount > setting.WithdrawMaxAmount {
		return fmt.Errorf("单次提现最少数量不能大于最多数量")
	}
	return nil
}

func isHttpUrl(value string) bool {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" {
		return false
	}
	return parsed.Scheme == "http" || parsed.Scheme == "https"
}

func isEthereumHexAddress(value string) bool {
	if len(value) != 42 || !strings.HasPrefix(value, "0x") {
		return false
	}
	for _, r := range value[2:] {
		if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
			return false
		}
	}
	return true
}

// gameSetting 仅作为配置框架(基于反射、无同步)就地修改的暂存区，
// 请求 goroutine 读取下方发布的只读快照。
var gameSetting = GameSetting{
	Enabled:              false,
	PlayTurnstileEnabled: true,
	WithdrawEnabled:      false,
	WithdrawDailyLimit:   1000,
	WithdrawMinAmount:    1,
	WithdrawMaxAmount:    100,
	ChainId:              11155111,
	RpcUrl:               "https://ethereum-sepolia-rpc.publicnode.com",
	TokenAddress:         "0x64bf89340562AB4fE829a481E6797B012664425B",
	TokenSymbol:          "✦",
	TokenDecimals:        0,
	ExplorerUrl:          "https://sepolia.etherscan.io",
	// 成功和失败都可能被浅层的区块重组推翻，而失败时的退款一旦发出就收不回来，默认等 3 个区块再下结论。
	Confirmations: 3,
	// 游戏默认都不开放，由管理员在游戏管理页逐个开启上线。
	Tetris: GameRule{Enabled: false, DailyRewardLimit: 3, ScorePerCoin: 5000, MinSecondsPerCoin: 60},
	Slots:  SlotsRule{Enabled: false, EvenStageWager: 200, DailyRewardLimit: 0, MaxBet: 5},
}

var gameSettingSnapshot atomic.Pointer[GameSetting]

var gameHotWalletPrivateKey atomic.Pointer[string]

func init() {
	config.GlobalConfig.Register("game_setting", &gameSetting)
	SyncGameSetting()
}

// GetGameSetting 返回只读快照，可被任意请求 goroutine 安全并发读取。
func GetGameSetting() *GameSetting {
	return gameSettingSnapshot.Load()
}

// SetGameHotWalletPrivateKey 发布管理员填写的热钱包私钥，空字符串表示没有填写。
func SetGameHotWalletPrivateKey(key string) {
	gameHotWalletPrivateKey.Store(&key)
}

// GetGameHotWalletPrivateKey 返回管理员填写的热钱包私钥，没有填写时为空字符串。
func GetGameHotWalletPrivateKey() string {
	if key := gameHotWalletPrivateKey.Load(); key != nil {
		return *key
	}
	return ""
}

// PlayTurnstileRequired 表示开局前要过 Turnstile 人机验证：游戏中心打开了这一项，且站点已启用 Turnstile。
func (s *GameSetting) PlayTurnstileRequired() bool {
	return s != nil && s.PlayTurnstileEnabled && common.TurnstileCheckEnabled
}

// ChainConfigured 表示链与合约已配好，提现 worker 才有地方可发。
func (s *GameSetting) ChainConfigured() bool {
	return s != nil && s.RpcUrl != "" && s.TokenAddress != "" && s.ChainId > 0
}

// SyncGameSetting 将暂存配置深拷贝后发布为只读快照。
// 每次更新暂存结构后都必须调用(见 model/option.go 的 handleConfigUpdate)。
func SyncGameSetting() {
	snapshot := &GameSetting{}
	data, err := common.Marshal(gameSetting)
	if err != nil || common.Unmarshal(data, snapshot) != nil {
		snapshot = &GameSetting{}
	}
	gameSettingSnapshot.Store(snapshot)
}

// SetGameSettingForTest 直接发布一个快照，仅供测试使用。
func SetGameSettingForTest(setting *GameSetting) {
	if setting == nil {
		setting = &GameSetting{}
	}
	gameSettingSnapshot.Store(setting)
}
