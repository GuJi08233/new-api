package model

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/blackjack"
	"github.com/QuantumNous/new-api/pkg/roulette"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// 游戏币原来按整币记账，改成按额度单位记账(见 GameCoins)之后，库里已有的游戏币数量都要乘以 1 个游戏币的额度单位数。迁移只做一次：
// 在同一个事务里先写下标记再换算，几个主节点同时启动时只有一个能写进标记、做换算，其余的看到标记就跳过；换算出错时标记随事务撤销，
// 下次启动再做。新装的站点没有数据，只写标记。
const gameCoinUnitsOption = "GameCoinUnits"

// gameCoinUnitColumns 是按游戏币记账的列。提现单的 amount 是链上发放的整币数，不换算，另把扣下的游戏币补进 units。
var gameCoinUnitColumns = []struct {
	model   any
	columns []string
}{
	{&GameCoinAccount{}, []string{"balance", "total_earned", "total_converted", "total_withdrawn", "total_wager_net", "total_wagered"}},
	{&GameCoinLog{}, []string{"amount", "balance"}},
	{&GameCoinConvertDay{}, []string{"used"}},
	{&GameSession{}, []string{"bet", "reward"}},
	{&GameRewardDay{}, []string{"coins", "pending_reward"}},
	{&GameRewardIpDay{}, []string{"coins"}},
	{&GameRoom{}, []string{"stake", "escrow", "rake"}},
	{&GameRoomSeat{}, []string{"net"}},
	{&GameJackpotRound{}, []string{"pot", "payout", "rake", "ticket", "winner_stake"}},
	{&GameJackpotEntry{}, []string{"amount"}},
	{&TradeAccount{}, []string{"coin_principal"}},
}

// gameCoinWagerGames 是成绩记的是派彩的下注游戏，它们的成绩跟着换算；技巧游戏与人机对战的成绩是分数或难度，不动。
var gameCoinWagerGames = []string{"slots", "wheel", "plinko", "dice", "roulette", "blackjack", "mines", "hilo", "crash", "scratch"}

func migrateGameCoinUnits() error {
	return DB.Transaction(func(tx *gorm.DB) error {
		claim := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&Option{Key: gameCoinUnitsOption, Value: "1"})
		if claim.Error != nil {
			return claim.Error
		}
		if claim.RowsAffected == 0 {
			return nil
		}
		unit, err := gameCoinMigrationUnit(tx)
		if err != nil {
			return err
		}
		if unit == 1 {
			return nil
		}
		limit := (common.MaxQuota - 1) / unit
		for _, table := range gameCoinUnitColumns {
			for _, column := range table.columns {
				if err := scaleGameCoinColumn(tx, table.model, column, "", nil, unit, limit); err != nil {
					return err
				}
			}
		}
		if err := scaleGameCoinColumn(tx, &GameSession{}, "score", "game IN ?", gameCoinWagerGames, unit, limit); err != nil {
			return err
		}
		if err := tx.Model(&GameCoinWithdrawal{}).Where("units = ?", 0).
			Update("units", gorm.Expr("amount * ?", unit)).Error; err != nil {
			return err
		}
		if err := scaleGameCoinStates(tx, unit); err != nil {
			return err
		}
		common.SysLog(fmt.Sprintf("game coins migrated to quota units, 1 coin = %d", unit))
		return nil
	})
}

// gameCoinMigrationUnit 是迁移时 1 个游戏币的额度单位数。迁移在加载配置之前运行，QuotaPerUnit 要从配置表里直接读，没改过时用默认值。
func gameCoinMigrationUnit(tx *gorm.DB) (int, error) {
	perUnit := common.QuotaPerUnit
	var values []string
	if err := tx.Model(&Option{}).Where(commonKeyCol+" = ?", "QuotaPerUnit").Pluck("value", &values).Error; err != nil {
		return 0, err
	}
	if len(values) > 0 && values[0] != "" {
		parsed, err := strconv.ParseFloat(values[0], 64)
		if err != nil {
			return 0, fmt.Errorf("invalid QuotaPerUnit %q: %w", values[0], err)
		}
		perUnit = parsed
	}
	unit := int(perUnit)
	if perUnit < 1 || perUnit >= common.MaxQuota || float64(unit) != perUnit {
		return 0, fmt.Errorf("QuotaPerUnit %v must be a positive integer for game coins", perUnit)
	}
	return unit, nil
}

// scaleGameCoinColumn 把一列乘以 unit，where 不为空时只换算这些行。换算前先确认最大最小值乘出来不越过 2^53，越界说明数据有问题，
// 整个迁移撤销，不写出夹过的数字。
func scaleGameCoinColumn(tx *gorm.DB, model any, column string, where string, arg any, unit int, limit int) error {
	rows := tx.Model(model)
	if where != "" {
		rows = rows.Where(where, arg)
	}
	// 新会话让同一个查询条件能接着用两次，第二次不会带上第一次的 Select。
	rows = rows.Session(&gorm.Session{})
	var bounds struct {
		High int
		Low  int
	}
	if err := rows.Select(fmt.Sprintf("COALESCE(MAX(%s), 0) AS high, COALESCE(MIN(%s), 0) AS low", column, column)).Scan(&bounds).Error; err != nil {
		return err
	}
	if bounds.High > limit || bounds.Low < -limit {
		return fmt.Errorf("game coin column %s holds %d..%d, too large to convert to quota units", column, bounds.Low, bounds.High)
	}
	return rows.Where(column+" <> ?", 0).Update(column, gorm.Expr(column+" * ?", unit)).Error
}

// scaleGameCoinStates 换算存在对局状态里的下注：还没结束的 21 点每一手的下注，以及轮盘整张注单的金额(结束的对局只供核查，
// 换算之后与对局的下注一致)。挖宝、高低牌与火箭的状态里没有金额。
func scaleGameCoinStates(tx *gorm.DB, unit int) error {
	var sessions []GameSession
	if err := tx.Select("id", "game", "state").
		Where("(game = ? AND status = ?) OR game = ?", "blackjack", GameSessionStatusPlaying, "roulette").
		Find(&sessions).Error; err != nil {
		return err
	}
	for _, session := range sessions {
		if session.State == "" {
			continue
		}
		var state []byte
		var err error
		switch session.Game {
		case "blackjack":
			var game blackjack.Game
			if err = common.UnmarshalJsonStr(session.State, &game); err != nil {
				return fmt.Errorf("blackjack session %d: %w", session.Id, err)
			}
			for i := range game.Hands {
				game.Hands[i].Bet *= unit
			}
			state, err = common.Marshal(game)
		case "roulette":
			var bets []roulette.Bet
			if err = common.UnmarshalJsonStr(session.State, &bets); err != nil {
				return fmt.Errorf("roulette session %d: %w", session.Id, err)
			}
			for i := range bets {
				bets[i].Amount *= unit
			}
			state, err = common.Marshal(bets)
		default:
			err = errors.New("unexpected game " + session.Game)
		}
		if err != nil {
			return err
		}
		if err := tx.Model(&GameSession{}).Where("id = ?", session.Id).Update("state", string(state)).Error; err != nil {
			return err
		}
	}
	return nil
}
