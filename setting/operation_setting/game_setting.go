package operation_setting

import (
	"encoding/hex"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"

	"github.com/shopspring/decimal"
)

// GameSetting 是游戏中心的全部可配置项。游戏币按 1 币 = 1 美元额度兑换主钱包，
// 这个比例是产品规则而不是运营参数，所以不在这里配置。
//
// 游戏按结算方式分三类，奖励规则各不相同：
//   - 技巧游戏与人机对战不花游戏币、只会赚，每人每个游戏每天只有前几次达标能领奖，同一 IP 下所有账号合计也只有这么多次，
//     每个游戏币还要当天在这个游戏里累计玩够一定时长，另可设这些游戏奖励每天的合计上限；大转盘免费旋转只限次数，奖品全额发放；
//   - 下注游戏花游戏币下注、会亏，理论返还率低于 100%，不限次数、不设每日上限，只限制单次下注；
//   - 对战与奖池是玩家之间的输赢，平台按比例抽成，同样不限次数。
type GameSetting struct {
	Enabled bool `json:"enabled"` // 游戏中心总开关，关闭时页面入口与用户接口一并下线
	// PlayTurnstileEnabled 要求每次领免费奖励的对局开始前通过 Turnstile 人机验证，只在站点启用了 Turnstile 时生效。
	PlayTurnstileEnabled bool `json:"play_turnstile_enabled"`
	// PlayTurnstileExemptGames 免除指定免费玩法的开局验证，不影响其他游戏或全站验证开关。
	PlayTurnstileExemptGames []string `json:"play_turnstile_exempt_games"`
	// FreeDailyLimit 是每人每天从技巧游戏与人机对战合计最多获得的游戏币，同一 IP 下所有账号合计也不超过它；
	// 大转盘免费旋转不受此上限影响，也不占用它。0 表示不限制，只按各游戏的每日领奖次数计算。
	FreeDailyLimit int `json:"free_daily_limit"`
	// RecordRetentionDays 是对局、对战房间、奖池、每日计数与游戏币明细里的游戏流水保留的天数，过了就由系统任务清理；
	// 每个玩家在每个游戏里的最高成绩留着，兑换、提现与管理员调整的流水不清理。0 表示一直保留。
	RecordRetentionDays int `json:"record_retention_days"`

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

	// 每个游戏一份规则，字段名(json 标签)就是游戏标识，也是 gameRuleSpecs 的键。
	Tetris      SkillRule `json:"tetris"`
	Game2048    SkillRule `json:"2048"`
	Snake       SkillRule `json:"snake"`
	Flappy      SkillRule `json:"flappy"`
	Minesweeper SkillRule `json:"minesweeper"`
	Sudoku      SkillRule `json:"sudoku"`
	Memory      SkillRule `json:"memory"`
	Puzzle15    SkillRule `json:"puzzle15"`
	Pairlink    SkillRule `json:"pairlink"`
	Breakout    SkillRule `json:"breakout"`
	Sokoban     SkillRule `json:"sokoban"`
	Lightsout   SkillRule `json:"lightsout"`
	Hanoi       SkillRule `json:"hanoi"`
	Twentyfour  SkillRule `json:"twentyfour"`
	Bulls       SkillRule `json:"bulls"`
	Nonogram    SkillRule `json:"nonogram"`

	Slots     WagerRule `json:"slots"`
	Wheel     WheelRule `json:"wheel"`
	Plinko    WagerRule `json:"plinko"`
	Dice      WagerRule `json:"dice"`
	Roulette  WagerRule `json:"roulette"`
	Blackjack WagerRule `json:"blackjack"`
	Mines     WagerRule `json:"mines"`
	Hilo      WagerRule `json:"hilo"`
	Crash     WagerRule `json:"crash"`
	Scratch   WagerRule `json:"scratch"`
	Baccarat  WagerRule `json:"baccarat"`
	Keno      WagerRule `json:"keno"`

	Gomoku   PvpRule `json:"gomoku"`
	Connect4 PvpRule `json:"connect4"`
	Reversi  PvpRule `json:"reversi"`
	Xiangqi  PvpRule `json:"xiangqi"`
	Rps      PvpRule `json:"rps"`
	Landlord PvpRule `json:"landlord"`
	Dots     PvpRule `json:"dots"`

	Jackpot JackpotRule `json:"jackpot"`
}

// RewardTier 是免费游戏的一档奖励：单局成绩达到 Score 奖励 Coins 个游戏币，Coins 可以有两位小数。成绩的含义由各游戏决定，
// 例如俄罗斯方块是分数，扫雷、人机对战是赢下的难度(1、2、3)。
type RewardTier struct {
	Score int     `json:"score"`
	Coins float64 `json:"coins"`
}

// SkillRule 是免费游戏的开关与奖励规则。玩家每天想玩多少局都可以，但只有成绩达到某一档的对局才发奖，发的是达到的最高一档；
// 每人每天最多领 DailyRewards 次，同一 IP 下所有账号合计也只有这么多次，领满之后照常可玩、不再发奖。
type SkillRule struct {
	Enabled bool `json:"enabled"`
	// DailyRewards 是每人每天最多领几次奖励，0 表示照常可玩但不发奖励。
	DailyRewards int `json:"daily_rewards"`
	// Tiers 按成绩从低到高排列，成绩与奖励都逐档递增。
	Tiers []RewardTier `json:"tiers"`
	// MinSecondsPerCoin 是每个游戏币需要玩家当天在这个游戏里累计玩够的时长(秒)，0 表示不限制。时长按当天结算过的每一局累加，
	// 不要求一局玩这么久；成绩先达标、时长还不够时奖励先记着(这一次领奖已经算数)，之后的对局把时长补够再发，只够几个就先发
	// 几个。脚本几秒就能打出好成绩，奖励还要按实际玩了多久来算。
	MinSecondsPerCoin int `json:"min_seconds_per_coin"`
}

// Reward 返回单局成绩应得的游戏币：达到的最高一档的奖励，一档都没达到时为 0。还没按游戏时长与每日次数扣减。
func (r SkillRule) Reward(score int) float64 {
	coins := 0.0
	for _, tier := range r.Tiers {
		if score >= tier.Score {
			coins = tier.Coins
		}
	}
	return coins
}

// MaxReward 返回单局最多能领的游戏币，即最高一档的奖励。
func (r SkillRule) MaxReward() float64 {
	if len(r.Tiers) == 0 {
		return 0
	}
	return r.Tiers[len(r.Tiers)-1].Coins
}

// WagerRule 是下注游戏的开关与下注规则。赔付表或赔率公式固定在各游戏的 pkg 包里，理论返还率低于 100%，
// 玩家长期是输的，所以不限次数、不设每日上限，只限制单次下注。
type WagerRule struct {
	Enabled bool `json:"enabled"`
	// MaxBet 是单次最多下注的游戏币。分档投入的游戏(Plinko、刮刮乐)只能从固定的几档里选，MaxBet 表示开放到哪一档。
	MaxBet int `json:"max_bet"`
	// RtpPercent 是按公式计算赔率的游戏(骰子、挖宝、高低牌、火箭)的理论返还率(百分比)，其他游戏不使用，为 0。
	RtpPercent int `json:"rtp_percent,omitempty"`
}

// WheelRule 是大转盘的开关与次数规则。每人每天前 FreeSpins 次免费，同一 IP 下所有账号合计也只有这么多次，奖品全额发放，
// 不受技巧游戏与人机对战的奖励合计上限影响，也不占用它。之后每次花 SpinCost 个游戏币，按下注游戏结算，不限次数。
type WheelRule struct {
	Enabled bool `json:"enabled"`
	// FreeSpins 是每人每天的免费次数，0 表示没有免费次数。
	FreeSpins int `json:"free_spins"`
	// SpinCost 是付费旋转每次花费的游戏币。免费旋转不花币，奖品同样按它的倍数计算。
	SpinCost int `json:"spin_cost"`
}

// PvpRule 是对战游戏的开关与规则。每位玩家押同样多的游戏币：双人对局赢家拿走两份押注，平台从中抽走 RakePercent%，多人对局
// 获胜的一方分输方的押注，平台从净赢额抽成(都向下取整)；平局各自退回，押注为 0 的是友谊赛。同一 IP 下的账号不能互相押注对战。
// Ai 是和服务端 AI 对战的领奖规则。
type PvpRule struct {
	Enabled bool `json:"enabled"`
	// MaxStake 是每人最多押注的游戏币，0 表示只能开友谊赛。
	MaxStake int `json:"max_stake"`
	// RakePercent 是押注对局里平台从奖池抽走的百分比，按奖池向下取整；平局不抽成。
	RakePercent int `json:"rake_percent"`
	// MoveSeconds 是每一步的限时(秒)，双人对局超时判负，多人对局超时由 AI 托管代打。
	MoveSeconds int `json:"move_seconds"`
	// Ai 是人机对战的开关与领奖规则，成绩是赢下的 AI 难度：1 简单、2 普通、3 困难，输了为 0。
	Ai SkillRule `json:"ai"`
}

// JackpotRule 是夺宝奖池的规则。玩家往当前一轮的奖池里投游戏币，第二位玩家投入后开始倒计时，到点按投入比例抽出一位赢家
// 拿走奖池，平台从中抽走 RakePercent%(向下取整)。
type JackpotRule struct {
	Enabled bool `json:"enabled"`
	// MaxBet 是每人每轮最多投入的游戏币。
	MaxBet int `json:"max_bet"`
	// RoundSeconds 是第二位玩家投入后到开奖的秒数。
	RoundSeconds int `json:"round_seconds"`
	// RakePercent 是平台从奖池抽走的百分比，按奖池向下取整。
	RakePercent int `json:"rake_percent"`
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
	// 每个游戏币要求的累计时长最多一小时，再长就几乎没人拿得到奖励了。
	GameMaxSecondsPerCoin = 3600
	// 免费游戏每天最多的领奖次数、奖励档数与单档奖励。单档奖励最少 0.01 个游戏币，最多两位小数。
	GameMaxDailyRewards = 100
	GameMaxRewardTiers  = 6
	GameMaxTierCoins    = 100
	GameMinTierCoins    = 0.01
	// 按公式计算赔率的下注游戏，理论返还率可以配置的范围(百分比)。必须低于 100，下注游戏不能成为提款机。
	GameMinRtpPercent = 50
	GameMaxRtpPercent = 99
	// 大转盘每次花费与每天免费次数的上限。派彩最多是花费的 50 倍，远在 2^53 之内。
	GameMaxWheelSpinCost  = 100
	GameMaxWheelFreeSpins = 100
	// 对战押注、抽成与每步限时的范围。
	GameMaxPvpStake       = 10000
	GameMaxRakePercent    = 20
	GameMinMoveSeconds    = 10
	GameMaxMoveSeconds    = 600
	GameMaxJackpotBet     = 10000
	GameMinJackpotSeconds = 15
	GameMaxJackpotSeconds = 600
	// 游戏记录保留天数的范围(0 表示一直保留)：至少一周，页面上"今天""最近几局"用到的记录不会被清理掉。
	GameMinRecordRetentionDays = 7
	GameMaxRecordRetentionDays = 3650
)

// 游戏的结算方式，决定配置项的规则类型和管理页怎么编辑。
const (
	GameKindSkill = "skill" // 免费游戏：按成绩领奖，限制每日次数
	GameKindWager = "wager" // 下注游戏：不限次数，限制单次下注
	GameKindPvp   = "pvp"   // 对战：玩家之间押注，附带人机对战
	GameKindParty = "party" // 多人奖池
)

// gameRuleSpec 是一个游戏的规则类型与取值范围。
type gameRuleSpec struct {
	Kind string
	// MaxTierScore 是免费游戏奖励档位的成绩上限，难度类游戏是难度编号的上限；0 表示只要求成绩为正。
	MaxTierScore int
	// MinBet、MaxBet 是下注游戏单次最多下注这一项的取值范围；BetSteps 不为空时只能从这几档里选。
	MinBet   int
	MaxBet   int
	BetSteps []int
	// Rtp 表示赔率按返还率公式计算，返还率可以配置。
	Rtp bool
	// Ai 表示对战游戏支持人机对战。
	Ai bool
}

// gameRuleSpecs 是所有游戏的规则类型，键是游戏标识，与 GameSetting 的字段名一致。
var gameRuleSpecs = map[string]gameRuleSpec{
	"tetris":      {Kind: GameKindSkill},
	"2048":        {Kind: GameKindSkill},
	"snake":       {Kind: GameKindSkill},
	"flappy":      {Kind: GameKindSkill},
	"minesweeper": {Kind: GameKindSkill, MaxTierScore: 3},
	"sudoku":      {Kind: GameKindSkill, MaxTierScore: 3},
	"memory":      {Kind: GameKindSkill, MaxTierScore: 3},
	"puzzle15":    {Kind: GameKindSkill, MaxTierScore: 3},
	"pairlink":    {Kind: GameKindSkill, MaxTierScore: 3},
	"breakout":    {Kind: GameKindSkill},
	"sokoban":     {Kind: GameKindSkill, MaxTierScore: 1},
	"lightsout":   {Kind: GameKindSkill, MaxTierScore: 3},
	"hanoi":       {Kind: GameKindSkill, MaxTierScore: 3},
	"twentyfour":  {Kind: GameKindSkill, MaxTierScore: 3},
	"bulls":       {Kind: GameKindSkill, MaxTierScore: 3},
	"nonogram":    {Kind: GameKindSkill, MaxTierScore: 3},

	"slots":     {Kind: GameKindWager, MinBet: 1, MaxBet: 1000},
	"wheel":     {Kind: GameKindWager},
	"plinko":    {Kind: GameKindWager, BetSteps: []int{1, 10, 100}},
	"dice":      {Kind: GameKindWager, MinBet: 1, MaxBet: 1000, Rtp: true},
	"roulette":  {Kind: GameKindWager, MinBet: 1, MaxBet: 1000},
	"blackjack": {Kind: GameKindWager, MinBet: 1, MaxBet: 1000},
	"mines":     {Kind: GameKindWager, MinBet: 1, MaxBet: 1000, Rtp: true},
	"hilo":      {Kind: GameKindWager, MinBet: 1, MaxBet: 1000, Rtp: true},
	"crash":     {Kind: GameKindWager, MinBet: 1, MaxBet: 1000, Rtp: true},
	"scratch":   {Kind: GameKindWager, BetSteps: []int{1, 5, 20}},
	"baccarat":  {Kind: GameKindWager, MinBet: 1, MaxBet: 1000},
	"keno":      {Kind: GameKindWager, MinBet: 1, MaxBet: 1000},

	"gomoku":   {Kind: GameKindPvp, Ai: true},
	"connect4": {Kind: GameKindPvp, Ai: true},
	"reversi":  {Kind: GameKindPvp, Ai: true},
	"xiangqi":  {Kind: GameKindPvp, Ai: true},
	"rps":      {Kind: GameKindPvp},
	"landlord": {Kind: GameKindPvp, Ai: true},
	"dots":     {Kind: GameKindPvp, Ai: true},

	"jackpot": {Kind: GameKindParty},
}

// GameMaxTierScores 返回奖励档位成绩有上限的免费游戏(按难度领奖的游戏，以及只有过关一种成绩的推箱子)各自的上限，
// 管理端照着它限制能填的成绩，不必再在页面里抄一份。
func GameMaxTierScores() map[string]int {
	scores := make(map[string]int)
	for game, spec := range gameRuleSpecs {
		if spec.MaxTierScore > 0 {
			scores[game] = spec.MaxTierScore
		}
	}
	return scores
}

// GameBetSteps 返回分档投入的游戏可选的几档，其他游戏返回 nil。
func GameBetSteps(game string) []int {
	return slices.Clone(gameRuleSpecs[game].BetSteps)
}

// NormalizeGameOption 整理免验证游戏列表；只接受支持免费玩法的游戏，不把未知标识静默忽略。
func NormalizeGameOption(key string, value string) (string, error) {
	if key != GameSettingPrefix+"play_turnstile_exempt_games" {
		return value, nil
	}
	var games []string
	if err := common.UnmarshalJsonStr(value, &games); err != nil || games == nil {
		return "", fmt.Errorf("免验证游戏必须是游戏标识数组")
	}
	for _, game := range games {
		spec, ok := gameRuleSpecs[game]
		if !ok || !(spec.Kind == GameKindSkill || spec.Ai || game == "wheel") {
			return "", fmt.Errorf("游戏 %q 不支持免费玩法的验证设置", game)
		}
	}
	slices.Sort(games)
	data, err := common.Marshal(slices.Compact(games))
	return string(data), err
}

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
	if spec, ok := gameRuleSpecs[field]; ok {
		return validateGameRule(field, spec, value)
	}
	switch field {
	case "play_turnstile_exempt_games":
		_, err := NormalizeGameOption(key, value)
		return err
	case "enabled", "withdraw_enabled", "play_turnstile_enabled":
		if _, err := strconv.ParseBool(value); err != nil {
			return fmt.Errorf("%s 必须是 true 或 false", field)
		}
	case "free_daily_limit":
		amount, err := strconv.Atoi(value)
		if err != nil || amount < 0 || amount >= common.MaxQuota {
			return fmt.Errorf("免费奖励每日合计上限必须是非负整数")
		}
	case "record_retention_days":
		days, err := strconv.Atoi(value)
		if err != nil || (days != 0 && (days < GameMinRecordRetentionDays || days > GameMaxRecordRetentionDays)) {
			return fmt.Errorf("游戏记录保留天数必须为 0(一直保留)或 %d 到 %d", GameMinRecordRetentionDays, GameMaxRecordRetentionDays)
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
	default:
		return fmt.Errorf("未知的游戏中心配置项: %s", field)
	}
	return nil
}

// validateGameRule 校验一个游戏的规则。规则整份以 JSON 保存，缺字段按零值校验：管理端提交的改动先合并进当前规则再整份保存。
func validateGameRule(game string, spec gameRuleSpec, value string) error {
	switch {
	case game == "wheel":
		var rule WheelRule
		if err := common.UnmarshalJsonStr(value, &rule); err != nil {
			return fmt.Errorf("%s 的游戏规则格式无效", game)
		}
		if rule.FreeSpins < 0 || rule.FreeSpins > GameMaxWheelFreeSpins {
			return fmt.Errorf("%s 的每日免费次数必须为 0 到 %d", game, GameMaxWheelFreeSpins)
		}
		if rule.SpinCost < 1 || rule.SpinCost > GameMaxWheelSpinCost {
			return fmt.Errorf("%s 的每次花费必须为 1 到 %d", game, GameMaxWheelSpinCost)
		}
	case spec.Kind == GameKindSkill:
		var rule SkillRule
		if err := common.UnmarshalJsonStr(value, &rule); err != nil {
			return fmt.Errorf("%s 的游戏规则格式无效", game)
		}
		return validateSkillRule(game, rule, spec.MaxTierScore)
	case spec.Kind == GameKindWager:
		var rule WagerRule
		if err := common.UnmarshalJsonStr(value, &rule); err != nil {
			return fmt.Errorf("%s 的游戏规则格式无效", game)
		}
		if len(spec.BetSteps) > 0 {
			if !slices.Contains(spec.BetSteps, rule.MaxBet) {
				return fmt.Errorf("%s 的最高开放档位必须是 %v 之一", game, spec.BetSteps)
			}
		} else if rule.MaxBet < spec.MinBet || rule.MaxBet > spec.MaxBet {
			return fmt.Errorf("%s 的单次最多下注必须为 %d 到 %d", game, spec.MinBet, spec.MaxBet)
		}
		if spec.Rtp && (rule.RtpPercent < GameMinRtpPercent || rule.RtpPercent > GameMaxRtpPercent) {
			return fmt.Errorf("%s 的理论返还率必须为 %d%% 到 %d%%", game, GameMinRtpPercent, GameMaxRtpPercent)
		}
		if !spec.Rtp && rule.RtpPercent != 0 {
			return fmt.Errorf("%s 的赔付表固定，不能配置返还率", game)
		}
	case spec.Kind == GameKindPvp:
		var rule PvpRule
		if err := common.UnmarshalJsonStr(value, &rule); err != nil {
			return fmt.Errorf("%s 的游戏规则格式无效", game)
		}
		if rule.MaxStake < 0 || rule.MaxStake > GameMaxPvpStake {
			return fmt.Errorf("%s 的押注上限必须为 0 到 %d", game, GameMaxPvpStake)
		}
		if rule.RakePercent < 0 || rule.RakePercent > GameMaxRakePercent {
			return fmt.Errorf("%s 的平台抽成必须为 0%% 到 %d%%", game, GameMaxRakePercent)
		}
		if rule.MoveSeconds < GameMinMoveSeconds || rule.MoveSeconds > GameMaxMoveSeconds {
			return fmt.Errorf("%s 的每步限时必须为 %d 到 %d 秒", game, GameMinMoveSeconds, GameMaxMoveSeconds)
		}
		if !spec.Ai {
			if rule.Ai.Enabled {
				return fmt.Errorf("%s 不支持人机对战", game)
			}
			return nil
		}
		return validateSkillRule(game+" 人机对战", rule.Ai, 3)
	case spec.Kind == GameKindParty:
		var rule JackpotRule
		if err := common.UnmarshalJsonStr(value, &rule); err != nil {
			return fmt.Errorf("%s 的游戏规则格式无效", game)
		}
		if rule.MaxBet < 1 || rule.MaxBet > GameMaxJackpotBet {
			return fmt.Errorf("%s 的每轮最多投入必须为 1 到 %d", game, GameMaxJackpotBet)
		}
		if rule.RoundSeconds < GameMinJackpotSeconds || rule.RoundSeconds > GameMaxJackpotSeconds {
			return fmt.Errorf("%s 的开奖倒计时必须为 %d 到 %d 秒", game, GameMinJackpotSeconds, GameMaxJackpotSeconds)
		}
		if rule.RakePercent < 0 || rule.RakePercent > GameMaxRakePercent {
			return fmt.Errorf("%s 的平台抽成必须为 0%% 到 %d%%", game, GameMaxRakePercent)
		}
	}
	return nil
}

// validateSkillRule 校验免费游戏的领奖规则：至少一档，成绩逐档严格递增、奖励逐档不减，成绩不超过 maxScore(0 表示只要求为正)。
func validateSkillRule(name string, rule SkillRule, maxScore int) error {
	if rule.DailyRewards < 0 || rule.DailyRewards > GameMaxDailyRewards {
		return fmt.Errorf("%s 的每日领奖次数必须为 0 到 %d", name, GameMaxDailyRewards)
	}
	if rule.MinSecondsPerCoin < 0 || rule.MinSecondsPerCoin > GameMaxSecondsPerCoin {
		return fmt.Errorf("%s 的每个游戏币所需时长必须为 0 到 %d 秒", name, GameMaxSecondsPerCoin)
	}
	if len(rule.Tiers) < 1 || len(rule.Tiers) > GameMaxRewardTiers {
		return fmt.Errorf("%s 的奖励档位必须有 1 到 %d 档", name, GameMaxRewardTiers)
	}
	if maxScore == 0 {
		maxScore = common.MaxQuota - 1
	}
	for i, tier := range rule.Tiers {
		if tier.Score < 1 || tier.Score > maxScore {
			return fmt.Errorf("%s 第 %d 档的成绩必须为 1 到 %d", name, i+1, maxScore)
		}
		coins := decimal.NewFromFloat(tier.Coins)
		if tier.Coins < GameMinTierCoins || tier.Coins > GameMaxTierCoins || !coins.Equal(coins.Truncate(2)) {
			return fmt.Errorf("%s 第 %d 档的奖励必须为 %v 到 %d 个游戏币，最多两位小数", name, i+1, GameMinTierCoins, GameMaxTierCoins)
		}
		if i > 0 && (tier.Score <= rule.Tiers[i-1].Score || tier.Coins < rule.Tiers[i-1].Coins) {
			return fmt.Errorf("%s 的奖励档位要按成绩从低到高排列，奖励不能比前一档少", name)
		}
	}
	return nil
}

// ValidateGameSetting 校验一份完整配置：逐项校验之外，还要求单次提现区间有意义。
// 管理端与通用 option 写入入口都在数据库事务中合并最新配置，再校验整份配置。
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

// dailyCoinRule 是免费游戏的默认领奖规则：每天领 1 次、每次 1 个游戏币，要求成绩达到 score，并且当天在这个游戏里累计玩满
// seconds 秒。
func dailyCoinRule(score int, seconds int) SkillRule {
	return SkillRule{DailyRewards: 1, Tiers: []RewardTier{{Score: score, Coins: 1}}, MinSecondsPerCoin: seconds}
}

// aiRule 是人机对战的默认领奖规则：赢下普通或困难难度领 1 个游戏币，简单难度只用来练手。
func aiRule(seconds int) SkillRule {
	rule := dailyCoinRule(2, seconds)
	rule.Enabled = true
	return rule
}

// gameSetting 仅作为配置框架(基于反射、无同步)就地修改的暂存区，
// 请求 goroutine 读取下方发布的只读快照。
var gameSetting = *DefaultGameSetting()

// DefaultGameSetting 返回一份默认配置。数据库里只存管理员改过的项，其余都是默认值，从数据库重建完整配置时以它为底。
func DefaultGameSetting() *GameSetting {
	return &GameSetting{
		Enabled:                  false,
		PlayTurnstileEnabled:     true,
		PlayTurnstileExemptGames: []string{},
		// 1 个游戏币就是 1 美元额度：技巧游戏与人机对战再多，每人每天合计也只发这么多；大转盘免费旋转独立发奖。
		FreeDailyLimit: 10,
		// 对局与游戏流水每人每天几十条，保留一个季度。
		RecordRetentionDays: 90,
		WithdrawEnabled:     false,
		WithdrawDailyLimit:  1000,
		WithdrawMinAmount:   1,
		WithdrawMaxAmount:   100,
		ChainId:             11155111,
		RpcUrl:              "https://ethereum-sepolia-rpc.publicnode.com",
		TokenAddress:        "0x64bf89340562AB4fE829a481E6797B012664425B",
		TokenSymbol:         "✦",
		TokenDecimals:       0,
		ExplorerUrl:         "https://sepolia.etherscan.io",
		// 成功和失败都可能被浅层的区块重组推翻，而失败时的退款一旦发出就收不回来，默认等 3 个区块再下结论。
		Confirmations: 3,

		// 游戏默认都不开放，由管理员在游戏管理页逐个开启上线。技巧游戏每个每天只发 1 个游戏币，当天在这个游戏里累计玩满
		// 2 到 5 分钟(按游戏节奏定)才能领；难度类游戏(扫雷、数独、记忆翻牌、数字华容道)的成绩是赢下的难度。
		Tetris:      dailyCoinRule(5000, 180),
		Game2048:    dailyCoinRule(6000, 180),
		Snake:       dailyCoinRule(20, 120),
		Flappy:      dailyCoinRule(15, 120),
		Minesweeper: dailyCoinRule(2, 180),
		Sudoku:      dailyCoinRule(1, 300),
		Memory:      dailyCoinRule(2, 120),
		Puzzle15:    dailyCoinRule(2, 180),
		Pairlink:    dailyCoinRule(2, 120),
		Breakout:    dailyCoinRule(300, 120),
		Sokoban:     dailyCoinRule(1, 180),
		Lightsout:   dailyCoinRule(2, 180),
		Hanoi:       dailyCoinRule(2, 180),
		Twentyfour:  dailyCoinRule(2, 180),
		Bulls:       dailyCoinRule(2, 120),
		Nonogram:    dailyCoinRule(2, 240),

		Slots:     WagerRule{MaxBet: 10},
		Wheel:     WheelRule{FreeSpins: 1, SpinCost: 1},
		Plinko:    WagerRule{MaxBet: 100},
		Dice:      WagerRule{MaxBet: 10, RtpPercent: 96},
		Roulette:  WagerRule{MaxBet: 10},
		Blackjack: WagerRule{MaxBet: 10},
		Mines:     WagerRule{MaxBet: 10, RtpPercent: 96},
		Hilo:      WagerRule{MaxBet: 10, RtpPercent: 96},
		Crash:     WagerRule{MaxBet: 10, RtpPercent: 96},
		Scratch:   WagerRule{MaxBet: 20},
		Baccarat:  WagerRule{MaxBet: 10},
		Keno:      WagerRule{MaxBet: 10},

		Gomoku:   PvpRule{MaxStake: 100, RakePercent: 5, MoveSeconds: 60, Ai: aiRule(180)},
		Connect4: PvpRule{MaxStake: 100, RakePercent: 5, MoveSeconds: 30, Ai: aiRule(120)},
		Reversi:  PvpRule{MaxStake: 100, RakePercent: 5, MoveSeconds: 60, Ai: aiRule(180)},
		Xiangqi:  PvpRule{MaxStake: 100, RakePercent: 5, MoveSeconds: 120, Ai: aiRule(300)},
		Rps:      PvpRule{MaxStake: 100, RakePercent: 5, MoveSeconds: 20},
		Landlord: PvpRule{MaxStake: 100, RakePercent: 5, MoveSeconds: 45, Ai: aiRule(180)},
		Dots:     PvpRule{MaxStake: 100, RakePercent: 5, MoveSeconds: 30, Ai: aiRule(150)},

		Jackpot: JackpotRule{MaxBet: 100, RoundSeconds: 60, RakePercent: 5},
	}
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

// Clone 深拷贝一份配置。快照是只读的，而规则里的奖励档位是切片，浅拷贝后再解析 JSON 会写进快照共用的底层数组，
// 要改动配置必须先 Clone。
func (s *GameSetting) Clone() *GameSetting {
	clone := &GameSetting{}
	data, err := common.Marshal(s)
	if err != nil || common.Unmarshal(data, clone) != nil {
		return &GameSetting{}
	}
	return clone
}

// SkillRuleOf 返回免费游戏的规则，game 不是免费游戏时 ok 为假。
func (s *GameSetting) SkillRuleOf(game string) (SkillRule, bool) {
	switch game {
	case "tetris":
		return s.Tetris, true
	case "2048":
		return s.Game2048, true
	case "snake":
		return s.Snake, true
	case "flappy":
		return s.Flappy, true
	case "minesweeper":
		return s.Minesweeper, true
	case "sudoku":
		return s.Sudoku, true
	case "memory":
		return s.Memory, true
	case "puzzle15":
		return s.Puzzle15, true
	case "pairlink":
		return s.Pairlink, true
	case "breakout":
		return s.Breakout, true
	case "sokoban":
		return s.Sokoban, true
	case "lightsout":
		return s.Lightsout, true
	case "hanoi":
		return s.Hanoi, true
	case "twentyfour":
		return s.Twentyfour, true
	case "bulls":
		return s.Bulls, true
	case "nonogram":
		return s.Nonogram, true
	}
	return SkillRule{}, false
}

// WagerRuleOf 返回下注游戏的规则，game 不是用 WagerRule 配置的下注游戏时 ok 为假(大转盘用 WheelRule)。
func (s *GameSetting) WagerRuleOf(game string) (WagerRule, bool) {
	switch game {
	case "slots":
		return s.Slots, true
	case "plinko":
		return s.Plinko, true
	case "dice":
		return s.Dice, true
	case "roulette":
		return s.Roulette, true
	case "blackjack":
		return s.Blackjack, true
	case "mines":
		return s.Mines, true
	case "hilo":
		return s.Hilo, true
	case "crash":
		return s.Crash, true
	case "scratch":
		return s.Scratch, true
	case "baccarat":
		return s.Baccarat, true
	case "keno":
		return s.Keno, true
	}
	return WagerRule{}, false
}

// PvpRuleOf 返回对战游戏的规则，game 不是对战游戏时 ok 为假。
func (s *GameSetting) PvpRuleOf(game string) (PvpRule, bool) {
	switch game {
	case "gomoku":
		return s.Gomoku, true
	case "connect4":
		return s.Connect4, true
	case "reversi":
		return s.Reversi, true
	case "xiangqi":
		return s.Xiangqi, true
	case "rps":
		return s.Rps, true
	case "landlord":
		return s.Landlord, true
	case "dots":
		return s.Dots, true
	}
	return PvpRule{}, false
}

// GameEnabled 表示这个游戏已开放；游戏中心总开关关闭时所有游戏都算关闭，未知的游戏返回 false。
func (s *GameSetting) GameEnabled(game string) bool {
	if !s.Enabled {
		return false
	}
	switch game {
	case "wheel":
		return s.Wheel.Enabled
	case "jackpot":
		return s.Jackpot.Enabled
	}
	if rule, ok := s.SkillRuleOf(game); ok {
		return rule.Enabled
	}
	if rule, ok := s.WagerRuleOf(game); ok {
		return rule.Enabled
	}
	if rule, ok := s.PvpRuleOf(game); ok {
		return rule.Enabled
	}
	return false
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

// PlayTurnstileRequired 表示指定免费玩法开局前需要验证：游戏验证与站点开关都打开，且该玩法未被豁免。
func (s *GameSetting) PlayTurnstileRequired(game string) bool {
	if s == nil || !s.PlayTurnstileEnabled || !common.TurnstileCheckEnabled || slices.Contains(s.PlayTurnstileExemptGames, game) {
		return false
	}
	spec, ok := gameRuleSpecs[game]
	return ok && (spec.Kind == GameKindSkill || spec.Ai || game == "wheel")
}

// ChainConfigured 表示链与合约已配好，提现 worker 才有地方可发。
func (s *GameSetting) ChainConfigured() bool {
	return s != nil && s.RpcUrl != "" && s.TokenAddress != "" && s.ChainId > 0
}

// SyncGameSetting 将暂存配置深拷贝后发布为只读快照。
// 每次更新暂存结构后都必须调用(见 model/option.go 的 handleConfigUpdate)。
func SyncGameSetting() {
	gameSettingSnapshot.Store(gameSetting.Clone())
}

// SetGameSettingForTest 直接发布一个快照，仅供测试使用。
func SetGameSettingForTest(setting *GameSetting) {
	if setting == nil {
		setting = &GameSetting{}
	}
	gameSettingSnapshot.Store(setting)
}

// DefaultGameSettingForTest 返回默认配置的一份拷贝，测试在它的基础上打开要测的游戏。
func DefaultGameSettingForTest() *GameSetting {
	return gameSetting.Clone()
}
